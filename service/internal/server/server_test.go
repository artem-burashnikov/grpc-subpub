package server

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	pb "github.com/artem-burashnikov/grpc-subpub/service/api/pb"
	"github.com/artem-burashnikov/grpc-subpub/service/internal/config"
	"github.com/artem-burashnikov/grpc-subpub/service/pkg/subpub"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

// defaultConfig provides a default configuration for the server.
// This is used in tests to ensure consistent server behavior.
func defaultConfig() config.Config {
	return config.Config{
		App: config.AppConfig{
			Environment: "dev",
			Name:        "app",
			Version:     "1.0.0",
		},
		GRPCServer: config.GRPCServerConfig{
			Port:                 "50051",
			MaxIdle:              30 * time.Second,
			Timeout:              5 * time.Second,
			StartupDelay:         1 * time.Second,
			ShutdownPeriod:       7 * time.Second,
			PublishRetryAttempts: 3,
			PublishRetryBackoff:  100 * time.Millisecond,
		},
	}
}

type mockLogger struct{}

func (m mockLogger) Debug(msg string, keysAndValues ...any) {}
func (m mockLogger) Info(msg string, keysAndValues ...any)  {}
func (m mockLogger) Warn(msg string, keysAndValues ...any)  {}
func (m mockLogger) Error(msg string, keysAndValues ...any) {}
func (m mockLogger) Fatal(msg string, keysAndValues ...any) {
	log.Fatalf(msg, keysAndValues...)
}
func (m mockLogger) Sync() {}

// bufSize defines the buffer size for the in-memory gRPC listener (bufconn).
const bufSize = 1024 * 1024

var (
	lis    *bufconn.Listener // in-memory gRPC listener used for testing.
	server *Server           // instance of the gRPC server being tested.
)

// bufDialer is a custom dialer for bufconn that allows gRPC clients to connect
// to the in-memory gRPC server.
func bufDialer(context.Context, string) (net.Conn, error) {
	return lis.Dial()
}

func TestMain(m *testing.M) {
	// Ensures no goroutine leaks after tests.
	defer goleak.VerifyTestMain(m)

	cfg := defaultConfig()
	logger := mockLogger{}
	sp := subpub.New()

	server = New(cfg, logger, sp)
	defer server.gracefulShutdown(context.Background())

	// Creates an in-memory gRPC listener for testing.
	lis = bufconn.Listen(bufSize)

	grpcServer := grpc.NewServer()

	pb.RegisterPubSubServer(grpcServer, server)

	go func() {
		if err := grpcServer.Serve(lis); err != nil {
			logger.Fatal("Server failed to run: %v", err)
		}
	}()

	// Runs all the tests.
	code := m.Run()

	grpcServer.GracefulStop()
	lis.Close()
	server.gracefulShutdown(context.Background())

	os.Exit(code)
}
func TestServerPublish(t *testing.T) {
	require := require.New(t)
	conn, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(bufDialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.Nil(err)
	defer conn.Close()

	client := pb.NewPubSubClient(conn)

	// Create a buffered channel for receiving messages.
	msgChan := make(chan string, 2)

	// Optional sync point for waiting until subscription is confirmed.
	var wg sync.WaitGroup

	// Start a goroutine to handle subscription and receiving messages.
	wg.Add(1)
	go func(msgChan chan string) {
		defer close(msgChan)

		// Open a subscription stream to the "test" topic.
		stream, err := client.Subscribe(context.Background(), &pb.SubscribeRequest{Key: "test"})
		require.Nil(err)

		// Wait for a confirmation message that the subscription is active.
		confirmation, err := stream.Recv()
		require.Nil(err)
		require.Equal(confirmation.GetData(), pb.SubscriptionReady)

		// Signal that subscription is ready.
		wg.Done()

		// Start receiving messages from the stream and forward them to msgChan.
		for range cap(msgChan) {
			event, err := stream.Recv()
			require.Nil(err)
			msgChan <- event.GetData()
		}
	}(msgChan)

	// Wait for subscription confirmation before publishing.
	wg.Wait()

	// Start a goroutine to publish two messages to the "test" topic.
	go func() {
		_, err = client.Publish(context.Background(), &pb.PublishRequest{
			Key:  "test",
			Data: "hello 1",
		})
		require.Nil(err)

		_, err = client.Publish(context.Background(), &pb.PublishRequest{
			Key:  "test",
			Data: "hello 2",
		})
		require.Nil(err)
	}()

	// Print messages received via the subscription stream.
	// Messages are received in the order they were published.
	for i := range 2 {
		msg := <-msgChan
		assert.Equal(t, fmt.Sprintf("hello %d", i+1), msg)
	}
}

func TestServerSubscribeNoPublisher(t *testing.T) {
	require := require.New(t)

	conn, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(bufDialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(err)
	defer conn.Close()

	client := pb.NewPubSubClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Subscribes to a topic that has no publishers.
	stream, err := client.Subscribe(ctx, &pb.SubscribeRequest{Key: "empty-topic"})
	require.NoError(err)

	// Waits for a confirmation message that the subscription is active.
	confirmation, err := stream.Recv()
	require.NoError(err)
	assert.Equal(t, pb.SubscriptionReady, confirmation.GetData())

	// No messages published — wait a bit and ensure no data is received.
	waitChan := make(chan struct{})
	go func() {
		_, err := stream.Recv() // Attempts to receive a message from the stream.
		assert.Error(t, err)    // should timeout or EOF
		close(waitChan)
	}()

	select {
	case <-waitChan:
	case <-time.After(3 * time.Second):
		t.Fatal("expected stream to finish or fail gracefully")
	}
}

func TestServerMultipleSubscribers(t *testing.T) {
	require := require.New(t)

	// Creates two gRPC client connections.
	conn1, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(bufDialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(err)
	defer conn1.Close()

	conn2, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(bufDialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(err)
	defer conn2.Close()

	// Creates PubSub clients for both connections.
	client1 := pb.NewPubSubClient(conn1)
	client2 := pb.NewPubSubClient(conn2)

	// Channels for receiving messages from each subscriber.
	msgCh1 := make(chan string, 1)
	msgCh2 := make(chan string, 1)

	// WaitGroup to synchronize subscription readiness.
	var wg sync.WaitGroup

	// Function to handle subscription and receiving messages for a client.
	subscribe := func(client pb.PubSubClient, ch chan string) {
		stream, err := client.Subscribe(context.Background(), &pb.SubscribeRequest{Key: "shared-topic"})
		require.NoError(err)
		confirmation, err := stream.Recv()
		require.NoError(err)
		require.Equal(pb.SubscriptionReady, confirmation.GetData())

		wg.Done()

		event, err := stream.Recv()
		require.NoError(err)
		ch <- event.GetData()
	}

	wg.Add(2)
	go subscribe(client1, msgCh1)
	go subscribe(client2, msgCh2)

	wg.Wait() // ensure both are subscribed

	// Publishes a message to the shared topic.
	_, err = client1.Publish(context.Background(), &pb.PublishRequest{
		Key:  "shared-topic",
		Data: "broadcast",
	})
	require.NoError(err)

	// Verifies that both subscribers received the broadcast message.
	msg1 := <-msgCh1
	msg2 := <-msgCh2

	assert.Equal(t, "broadcast", msg1)
	assert.Equal(t, "broadcast", msg2)
}

func TestServerInvalidSubscribeRequest(t *testing.T) {
	require := require.New(t)

	conn, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(bufDialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(err)
	defer conn.Close()

	client := pb.NewPubSubClient(conn)

	// Sends a subscription request with an invalid key (empty string).
	stream, err := client.Subscribe(context.Background(), &pb.SubscribeRequest{Key: ""})
	require.NoError(err)

	_, err = stream.Recv()
	require.Error(err)
}

func TestServerUnsubscribe(t *testing.T) {
	require := require.New(t)

	conn, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(bufDialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(err)
	defer conn.Close()

	client := pb.NewPubSubClient(conn)

	// Creates a context with cancellation to simulate unsubscribing.
	ctx, cancel := context.WithCancel(context.Background())
	stream, err := client.Subscribe(ctx, &pb.SubscribeRequest{Key: "unsub-topic"})
	require.NoError(err)

	// Receives a confirmation message that the subscription is active.
	confirmation, err := stream.Recv()
	require.NoError(err)
	require.Equal(pb.SubscriptionReady, confirmation.GetData())

	// Cancels the subscription before any messages are sent.
	cancel()

	// Attempts to read from the closed stream — expects an error.
	_, err = stream.Recv()
	require.Error(err)

	// Publishes a message to the topic — no subscribers should receive it.
	_, err = client.Publish(context.Background(), &pb.PublishRequest{
		Key:  "unsub-topic",
		Data: "ignored message",
	})
	require.NoError(err)

	// Attempts to read again.
	_, err = stream.Recv()
	require.Error(err)
}

func TestServerPublishNoSubscribers(t *testing.T) {
	require := require.New(t)

	conn, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(bufDialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(err)
	defer conn.Close()

	client := pb.NewPubSubClient(conn)

	_, err = client.Publish(context.Background(), &pb.PublishRequest{
		Key:  "no-one-listening",
		Data: "just in the void",
	})
	require.NoError(err)
}

func TestServerSubscribeTimeout(t *testing.T) {
	require := require.New(t)

	conn, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(bufDialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(err)
	defer conn.Close()

	client := pb.NewPubSubClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Subscribes to a topic with a short timeout.
	stream, err := client.Subscribe(ctx, &pb.SubscribeRequest{Key: "timeout-topic"})
	require.NoError(err)

	confirmation, err := stream.Recv()
	require.NoError(err)
	require.Equal(pb.SubscriptionReady, confirmation.GetData())

	// Waits for the timeout to occur and ensures the stream is closed.
	_, err = stream.Recv()
	require.Error(err)
}

func TestServerMultipleTopicsIsolation(t *testing.T) {
	require := require.New(t)

	conn, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(bufDialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(err)
	defer conn.Close()

	client := pb.NewPubSubClient(conn)

	// Channels for receiving messages from two separate topics.
	topic1Chan := make(chan string, 1)
	topic2Chan := make(chan string, 1)

	// WaitGroup to synchronize subscription readiness.
	var wg sync.WaitGroup

	subscribe := func(topic string, ch chan string) {
		defer close(ch)
		stream, err := client.Subscribe(context.Background(), &pb.SubscribeRequest{Key: topic})
		require.NoError(err)

		confirmation, err := stream.Recv()
		require.NoError(err)
		require.Equal(pb.SubscriptionReady, confirmation.GetData())

		wg.Done()

		event, err := stream.Recv()
		if topic == "topic-1" {
			require.NoError(err) // Ensures no error occurred for topic-1.
		} else {
			require.Error(err) // Expects an error for topic-2 since no message is published to it.
		}
		ch <- event.GetData()
	}

	wg.Add(2)
	go subscribe("topic-1", topic1Chan)
	go subscribe("topic-2", topic2Chan)

	wg.Wait()

	// Publishes a message to topic-1.
	_, err = client.Publish(context.Background(), &pb.PublishRequest{
		Key:  "topic-1",
		Data: "only topic 1",
	})
	require.NoError(err)

	// Verifies that topic-1 received the message.
	select {
	case msg := <-topic1Chan:
		assert.Equal(t, "only topic 1", msg)
	case <-time.After(time.Second):
		t.Fatal("expected message on topic-1")
	}

	// Verifies that topic-2 did not receive any message.
	select {
	case msg := <-topic2Chan:
		t.Fatalf("unexpected message on topic-2: %s", msg)
	case <-time.After(500 * time.Millisecond):
		// ok — no message expected
	}
}

func TestServerRaceCondition(t *testing.T) {
	require := require.New(t)

	conn, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(bufDialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(err)
	defer conn.Close()

	client := pb.NewPubSubClient(conn)

	const (
		subscriberCount = 10   // Number of concurrent subscribers.
		messageCount    = 1000 // Number of messages to publish.
		topicKey        = "race-topic"
	)

	var allSubscribed sync.WaitGroup
	allSubscribed.Add(subscriberCount)

	var allMessages sync.WaitGroup
	allMessages.Add(subscriberCount * messageCount)

	// Starts multiple subscribers.
	for i := range subscriberCount {
		go func(subID int) {
			stream, err := client.Subscribe(context.Background(), &pb.SubscribeRequest{Key: topicKey})
			require.NoError(err)

			confirmation, err := stream.Recv()
			require.NoError(err)
			require.Equal(pb.SubscriptionReady, confirmation.GetData())

			// Marks this subscriber as ready.
			allSubscribed.Done()

			received := 0
			for received < messageCount {
				event, err := stream.Recv()
				if err != nil {
					t.Errorf("subscriber %d: failed to receive: %v", subID, err)
					return
				}
				if !strings.HasPrefix(event.GetData(), "msg") {
					t.Errorf("subscriber %d: unexpected message: %s", subID, event.GetData())
				}
				received++
				// Marks a message as received.
				allMessages.Done()
			}
		}(i)
	}

	allSubscribed.Wait() // Wait for subscribers to be ready

	// Publishes messages concurrently.
	for i := range messageCount {
		go func(i int) {
			_, err := client.Publish(context.Background(), &pb.PublishRequest{
				Key:  topicKey,
				Data: fmt.Sprintf("msg-%d", i),
			})
			require.NoError(err)
		}(i)
	}

	// Waits for all messages to be received.
	allMessages.Wait()
}

func TestServerPublishThrottle(t *testing.T) {
	require := require.New(t)

	conn, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(bufDialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(err)
	defer conn.Close()

	client := pb.NewPubSubClient(conn)

	const totalMessages = 1000                  // Total number of messages to publish.
	msgChan := make(chan string, totalMessages) // Channel to collect received messages.
	ready := make(chan struct{})

	// Goroutine to handle subscription and receiving messages.
	go func() {
		defer close(msgChan)

		stream, err := client.Subscribe(context.Background(), &pb.SubscribeRequest{Key: "throttle"})
		require.NoError(err)

		confirmation, err := stream.Recv()
		require.NoError(err)
		require.Equal(pb.SubscriptionReady, confirmation.GetData())

		// Signals that the subscription is ready.
		close(ready)

		for range totalMessages {
			event, err := stream.Recv()
			require.NoError(err)
			msgChan <- event.GetData()
		}
	}()

	// Waits for the subscription to be ready.
	<-ready

	// Publishes messages to the topic.
	for i := range totalMessages {
		_, err := client.Publish(context.Background(), &pb.PublishRequest{
			Key:  "throttle",
			Data: fmt.Sprintf("msg-%d", i),
		})
		require.NoError(err)
	}

	// Collects all received messages.
	received := make([]string, 0, totalMessages)
	for range totalMessages {
		received = append(received, <-msgChan)
	}

	// Verifies that all messages were received in order.
	for i := range totalMessages {
		assert.Equal(t, fmt.Sprintf("msg-%d", i), received[i])
	}
}

func TestServerWithSubscriberLatency(t *testing.T) {
	require := require.New(t)

	conn, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(bufDialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(err)
	defer conn.Close()

	client := pb.NewPubSubClient(conn)

	msgChan := make(chan string, 2) // Channel to collect received messages.
	ready := make(chan struct{})    // Channel to signal subscription readiness.

	// Goroutine to handle subscription and simulate subscriber latency.
	go func() {
		defer close(msgChan)

		stream, err := client.Subscribe(context.Background(), &pb.SubscribeRequest{Key: "slow-client"})
		require.NoError(err)

		confirmation, err := stream.Recv()
		require.NoError(err)
		require.Equal(pb.SubscriptionReady, confirmation.GetData())

		close(ready)

		// Simulates latency by adding a delay before receiving each message.
		for range 2 {
			time.Sleep(300 * time.Millisecond)
			event, err := stream.Recv()
			require.NoError(err)
			msgChan <- event.GetData()
		}
	}()

	<-ready // Waits for the subscription to be ready.

	// Publishes two messages to the topic.
	for _, data := range []string{"slow-1", "slow-2"} {
		_, err := client.Publish(context.Background(), &pb.PublishRequest{
			Key:  "slow-client",
			Data: data,
		})
		require.NoError(err)
	}

	// Verifies that the subscriber received both messages in order.
	msg1 := <-msgChan
	msg2 := <-msgChan

	assert.Equal(t, "slow-1", msg1)
	assert.Equal(t, "slow-2", msg2)
}

func TestServerSlowSubscriberDoesNotBlockFastOne(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	conn, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(bufDialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(err)
	defer conn.Close()

	client := pb.NewPubSubClient(conn)
	topic := "parallel"

	slowReceived := make(chan string, 1) // Channel for the slower subscriber.
	fastReceived := make(chan string, 1) // Channel for the faster subscriber.
	ready := make(chan struct{}, 2)

	// Faster subscriber
	go func() {
		stream, err := client.Subscribe(context.Background(), &pb.SubscribeRequest{Key: topic})
		require.NoError(err)

		msg, err := stream.Recv()
		require.NoError(err)
		assert.Equal(pb.SubscriptionReady, msg.GetData())
		ready <- struct{}{}

		msg, err = stream.Recv()
		require.NoError(err)
		fastReceived <- msg.GetData()
	}()

	// Slower subscriber.
	go func() {
		stream, err := client.Subscribe(context.Background(), &pb.SubscribeRequest{Key: topic})
		require.NoError(err)

		msg, err := stream.Recv()
		require.NoError(err)
		assert.Equal(pb.SubscriptionReady, msg.GetData())
		ready <- struct{}{}

		// Simulates latency
		time.Sleep(200 * time.Millisecond)

		msg, err = stream.Recv()
		require.NoError(err)
		slowReceived <- msg.GetData()
	}()

	// Waits for both subscribers to confirm their subscriptions.
	<-ready
	<-ready

	// Publishes a single message.
	_, err = client.Publish(context.Background(), &pb.PublishRequest{
		Key:  topic,
		Data: "message",
	})
	require.NoError(err)

	// Verifies that the faster subscriber received the message first.
	select {
	case msg := <-fastReceived:
		assert.Equal("message", msg)
	case <-slowReceived:
		t.Fatal("fast subscriber should receiver message first")
	case <-time.After(100 * time.Millisecond):
		t.Fatal("fast subscriber didn't receive message in time")
	}

	select {
	case msg := <-slowReceived:
		assert.Equal("message", msg)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("slow subscriber didn't receive message in time")
	}
}
