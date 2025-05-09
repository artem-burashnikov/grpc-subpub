package server

import (
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/artem-burashnikov/grpc-subpub/internal/config"
	"github.com/artem-burashnikov/grpc-subpub/internal/logger"
	"github.com/artem-burashnikov/grpc-subpub/pkg/subpub"
	spv1 "github.com/artem-burashnikov/grpc-subpub/proto"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type Server struct {
	cfg  config.Config
	log  logger.Logger
	sp   subpub.SubPub
	grpc *grpc.Server
	spv1.UnimplementedPubSubServer
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

	spv1.RegisterPubSubServer(grpcServer, s)

	return s
}

func (s *Server) Run() {
	if s == nil {
		log.Fatal("gRPC server is not initialized")
		return
	}

	lis, err := net.Listen("tcp", ":"+s.cfg.GRPCServer.Port)
	if err != nil {
		s.log.Fatal("failed to listen:", err)
	}

	if err := s.grpc.Serve(lis); err != nil {
		s.log.Fatal("gRPC server failed to start", zap.Error(err))
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	<-quit
	s.log.Info("shutting down gracefully...")
	s.GracefulStop()
	s.log.Info("server stopped")
}

func (s *Server) GracefulStop() {
	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.GRPCServer.ShutdownPeriod)
	defer cancel()

	done := make(chan struct{})
	errCh := make(chan error, 1)

	go func() {
		defer close(done)
		defer close(errCh)

		s.grpc.GracefulStop()

		if err := s.sp.Close(ctx); err != nil {
			errCh <- err
			return
		}

	}()

	select {
	case <-done:
		s.log.Info("system shut down successfully")
	case err := <-errCh:
		s.log.Error("subpub system returned an error", zap.Error(err))
	case <-ctx.Done():
		s.log.Info("graceful shutdown timed out, forcibly closing grpc server")
		s.grpc.Stop()
	}
}

func (s *Server) Publish(ctx context.Context, req *spv1.PublishRequest) (*emptypb.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	done := make(chan struct{})
	errCh := make(chan error, 1)

	go func() {
		defer close(errCh)
		defer close(done)

		subject, message := req.GetKey(), req.GetData()

		if subject == "" {
			s.log.Error("invalid publish argument: empty subject is not allowed")
			errCh <- status.Error(codes.InvalidArgument, "empty key")
			return
		}

		if message == "" {
			s.log.Debug("publishing empty message")
		}

		err := s.sp.Publish(subject, message)
		if err != nil {
			s.log.Error("failed to publish",
				zap.String("key", subject),
				zap.String("data", message),
				zap.Error(err),
			)
			errCh <- status.Error(codes.Internal, "internal server error")
			return
		}

		s.log.Info("published successfully")
	}()

	select {
	case <-done:
		return &emptypb.Empty{}, nil
	case err := <-errCh:
		return nil, err
	case <-ctx.Done():
		return nil, status.Error(codes.Canceled, "publish request was canceled by caller")
	}
}

func (s *Server) Subscribe(req *spv1.SubscribeRequest, stream spv1.PubSub_SubscribeServer) error {
	if req == nil {
		return status.Error(codes.InvalidArgument, "request is nil")
	}

	ctx := stream.Context()
	errCh := make(chan error, 1)

	go func() {
		defer close(errCh)

		subject := req.GetKey()
		if subject == "" {
			s.log.Error("invalid publish argument: empty subject is not allowed")
			errCh <- status.Error(codes.InvalidArgument, "empty key")
			return
		}

		streamHandler := func(msg any) {
			select {
			case <-ctx.Done():
				return // ignore the message if the context was canceled
			default:
				event, ok := msg.(*spv1.Event)
				if !ok {
					s.log.Error("failed to parse message: invalid message type")
					status.Error(codes.Internal, "internal server error")
					return
				}

				if err := stream.Send(event); err != nil {
					s.log.Error("failed to send event", zap.Error(err))
					errCh <- status.Error(codes.Internal, "internal server error")
					return
				}
				s.log.Debug("message sent", zap.String("data", event.GetData()))
			}
		}

		sub, err := s.sp.Subscribe(subject, streamHandler)
		if err != nil {
			s.log.Error("subscription failed",
				zap.String("subject", subject),
				zap.Error(err),
			)
			errCh <- status.Error(codes.Internal, "internal server error")
			return
		}
		defer sub.Unsubscribe()

		<-ctx.Done()
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		s.log.Info("client disconnected")
		return nil
	}
}
