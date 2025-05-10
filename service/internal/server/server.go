package server

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	pb "github.com/artem-burashnikov/grpc-subpub/service/api/pb"
	"github.com/artem-burashnikov/grpc-subpub/service/internal/config"
	"github.com/artem-burashnikov/grpc-subpub/service/internal/logger"
	"github.com/artem-burashnikov/grpc-subpub/service/pkg/subpub"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

// gRPC Sub/Pub server.
type Server struct {
	cfg  config.Config
	log  logger.Logger
	sp   subpub.SubPub // event bus system
	grpc *grpc.Server
	pb.UnimplementedPubSubServer
}

func New(cfg config.Config, log logger.Logger, sp subpub.SubPub) *Server {
	kp := keepalive.ServerParameters{
		MaxConnectionIdle: cfg.GRPCServer.MaxIdle,
		Timeout:           cfg.GRPCServer.Timeout,
	}

	grpcServer := grpc.NewServer(grpc.KeepaliveParams(kp))

	s := &Server{
		cfg:  cfg,
		log:  log,
		sp:   sp,
		grpc: grpcServer,
	}

	pb.RegisterPubSubServer(grpcServer, s)

	return s
}

// Run starts the gRPC server and blocks until a termination signal is received.
func (s *Server) Run() error {
	if s == nil {
		return fmt.Errorf("gRPC server is not initialized")
	}

	// Create a TCP listener.
	lis, err := net.Listen("tcp", ":"+s.cfg.GRPCServer.Port)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	serverErr := make(chan error, 1)

	// Start the gRPC server.
	go func() {
		if err := s.grpc.Serve(lis); err != nil {
			serverErr <- err
		}
	}()

	// Wait for either successful startup or failure.
	select {
	case err := <-serverErr:
		return fmt.Errorf("gRPC server failed to start: %w", err)
	case <-time.After(s.cfg.GRPCServer.StartupDelay):
		s.log.Info("server started", zap.String("port", s.cfg.GRPCServer.Port))
	}

	// Graceful shutdown.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	<-quit
	s.log.Info("shutting down gracefully...")

	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.GRPCServer.ShutdownPeriod)
	defer cancel()

	if err := s.gracefulShutdown(ctx); err != nil {
		s.log.Error("error during graceful shutdown", zap.Error(err))
		return err
	}

	s.log.Info("server stopped")
	return nil
}

// gracefulShutdown stops the gRPC server and cleans up resources.
func (s *Server) gracefulShutdown(ctx context.Context) error {
	if s == nil {
		return fmt.Errorf("gRPC server is not initialized")
	}

	done := make(chan struct{})
	errCh := make(chan error, 1)

	go func() {
		defer close(done)
		defer close(errCh)

		s.log.Info("stopping gRPC server...")
		s.grpc.GracefulStop()
		s.log.Info("gRPC server stopped")

		s.log.Info("closing SubPub system...")
		if err := s.sp.Close(ctx); err != nil {
			s.log.Error("failed to close SubPub system", zap.Error(err))
			errCh <- err
			return
		}
		s.log.Info("SubPub system closed")
	}()

	select {
	case <-done:
		s.log.Info("graceful shutdown completed successfully")
		return nil
	case err := <-errCh:
		return err
	case <-ctx.Done():
		s.log.Warn("graceful shutdown timed out, forcing stop")
		s.grpc.Stop()
		return ctx.Err()
	}
}

// Publish sends a message to a specific key/topic with retry logic.
func (s *Server) Publish(ctx context.Context, req *pb.PublishRequest) (*emptypb.Empty, error) {
	if s == nil {
		return nil, status.Error(codes.Unknown, "gRPC server is not initialized")
	}

	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	subject, message := req.GetKey(), req.GetData()

	if subject == "" {
		s.log.Debug("invalid publish argument: empty subject is not allowed")
		return nil, status.Error(codes.InvalidArgument, "empty key")
	}

	if message == "" {
		s.log.Debug("publishing empty message")
	}

	s.log.Debug("attempting to publish message", zap.String("key", req.Key), zap.String("data", req.Data))
	var lastErr error
	for i := range s.cfg.GRPCServer.PublishRetryAttempts {
		if err := s.sp.Publish(subject, message); err == nil {
			s.log.Info("published successfully")
			return &emptypb.Empty{}, nil
		} else {
			lastErr = err
			s.log.Warn("publish attempt failed, retrying",
				zap.Int("attempt", i+1),
				zap.Int("max_attempts", s.cfg.GRPCServer.PublishRetryAttempts),
				zap.Error(err),
			)
			time.Sleep(s.cfg.GRPCServer.PublishRetryBackoff)
		}
	}

	s.log.Error("failed to publish after all retries",
		zap.String("key", subject),
		zap.String("data", message),
		zap.Error(lastErr),
	)
	return nil, status.Error(codes.Internal, "internal server error")

}

// Subscribe registers a client subscription and streams messages to them via a gRPC stream.
//
// A confirmation message (SubscriptionReady) is sent to ensure the client is ready to receive.
func (s *Server) Subscribe(req *pb.SubscribeRequest, stream pb.PubSub_SubscribeServer) error {
	if s == nil {
		return status.Error(codes.Unknown, "gRPC server is not initialized")
	}

	if req == nil {
		return status.Error(codes.InvalidArgument, "request is nil")
	}

	subject := req.GetKey()
	if subject == "" {
		s.log.Error("invalid subscribe argument: empty subject is not allowed")
		return status.Error(codes.InvalidArgument, "empty key")
	}

	ctx := stream.Context()

	msgHandler := func(msg any) {
		msgStr, ok := msg.(string)
		if !ok {
			s.log.Error("failed to parse message: invalid message type")
			return
		}
		select {
		case <-ctx.Done():
			s.log.Debug("context canceled, dropping message")
		default:
			if err := stream.Send(&pb.Event{Data: msgStr}); err != nil {
				s.log.Error("failed to send message to client",
					zap.String("subject", subject),
					zap.String("message", msgStr),
					zap.Error(err),
				)
				return
			}
		}
	}

	// Subscribe to the topic.
	sub, err := s.sp.Subscribe(subject, msgHandler)
	if err != nil {
		s.log.Error("subscription failed",
			zap.String("subject", subject),
			zap.Error(err),
		)
		return status.Error(codes.Internal, "internal server error")
	}
	defer sub.Unsubscribe()

	s.log.Info("subscription established", zap.String("subject", subject))

	// Send confirmation to client (ensures they’re ready before messages are sent).
	if err := stream.Send(&pb.Event{Data: pb.SubscriptionReady}); err != nil {
		s.log.Error("failed to send subscription confirmation",
			zap.String("subject", subject),
			zap.Error(err),
		)
		return status.Error(codes.Internal, "internal server error")
	}
	s.log.Info("subscription confirmation sent", zap.String("subject", subject))

	<-ctx.Done()
	s.log.Info("client disconnected", zap.String("subject", subject))
	return nil
}
