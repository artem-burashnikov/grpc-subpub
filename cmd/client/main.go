package main

import (
	"context"
	"fmt"
	"log"

	pb "github.com/artem-burashnikov/grpc-subpub/api/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	// Create a new gRPC client connection to the server running on localhost:50051.
	// `insecure.NewCredentials()` is used because we're not using TLS.
	conn, err := grpc.NewClient("localhost:50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	// Create a new PubSub client using the generated gRPC client.
	client := pb.NewPubSubClient(conn)

	// Create a buffered channel for receiving messages.
	msgChan := make(chan string, 2)

	// Optional sync point for waiting until subscription is confirmed.
	ready := make(chan struct{})

	// Start a goroutine to handle subscription and receiving messages.
	go func(msgChan chan string) {
		defer close(msgChan)

		// Open a subscription stream to the "test" topic.
		stream, err := client.Subscribe(context.Background(), &pb.SubscribeRequest{Key: "test"})
		if err != nil {
			log.Fatal(err)
			return
		}

		// Wait for a confirmation message that the subscription is active.
		confirmation, err := stream.Recv()
		if err != nil {
			log.Fatal("subscription confirmation was not received:", err)
			return
		}
		if confirmation.GetData() != pb.SubscriptionReady {
			log.Fatal("invalid confirmation message:", confirmation.GetData())
			return
		}

		// Signal that subscription is ready.
		close(ready)

		// Start receiving messages from the stream and forward them to msgChan.
		for range cap(msgChan) {
			event, err := stream.Recv()
			if err != nil {
				log.Fatal("failed to receive message:", err)
				return
			}
			msgChan <- event.GetData()
		}
	}(msgChan)

	// Wait for subscription confirmation before publishing.
	<-ready

	// Start a goroutine to publish two messages to the "test" topic.
	go func() {
		_, err = client.Publish(context.Background(), &pb.PublishRequest{
			Key:  "test",
			Data: "hello",
		})
		if err != nil {
			log.Fatal("failed to publish:", err)
		}

		_, err = client.Publish(context.Background(), &pb.PublishRequest{
			Key:  "test",
			Data: "hello again",
		})
		if err != nil {
			log.Fatal("failed to publish", err)
		}
	}()

	// Print messages received via the subscription stream.
	// Messages are received in the order they were published.
	for msg := range msgChan {
		fmt.Println(msg)
	}
}
