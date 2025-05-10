# grpc-subpub

![LICENSE][license-shield] ![CI Status][ci-shield]

`grpc-subpub` is a gRPC-based event subscription service built on a custom in-memory pub-sub system. It supports key-based event delivery with non-blocking, FIFO message handling.

The project is designed with modern software engineering patterns, such as graceful shutdown, dependency injection, and a local running queue for efficient message processing.

---

## Features

- **Key-based event delivery**: Publish and subscribe to specific topics.
- **Non-blocking message handling**: Ensures subscribers are not blocked by slow consumers.
- **FIFO message delivery**: Guarantees message order for each subscriber.
- **Graceful shutdown**: Ensures proper cleanup of resources during termination.
- **Dependency injection**: Simplifies testing and configuration.
- **Local running queue**: Efficiently handles messages for each subscriber.

---

## Table of Contents

- [grpc-subpub](#grpc-subpub)
  - [Features](#features)
  - [Table of Contents](#table-of-contents)
  - [Getting Started](#getting-started)
    - [Supscriber-Publisher Event Bus](#supscriber-publisher-event-bus)
    - [Building the Project](#building-the-project)
      - [Server configuration](#server-configuration)
      - [Build Locally](#build-locally)
      - [Build with Docker](#build-with-docker)
    - [Running the Server](#running-the-server)
      - [Run Locally](#run-locally)
      - [Run with Docker](#run-with-docker)
      - [Run with Docker Compose](#run-with-docker-compose)
    - [Using the Test Client](#using-the-test-client)
    - [Development Patterns](#development-patterns)
      - [Graceful Shutdown](#graceful-shutdown)
      - [Dependency Injection](#dependency-injection)
      - [Local Running Queue](#local-running-queue)
    - [Makefile Targets](#makefile-targets)
    - [Docker and Docker Compose](#docker-and-docker-compose)
      - [Use Docker Compose](#use-docker-compose)
  - [Testing and Code Coverage](#testing-and-code-coverage)
  - [License](#license)

---

## Getting Started

To get started with `grpc-subpub`, clone the repository:

```bash
git clone https://github.com/artem-burashnikov/grpc-subpub.git
cd grpc-subpub
```

### Supscriber-Publisher Event Bus

gRPC server utilizes [subpub package](/service/pkg/subpub/) for subscribtion and message handling.

### Building the Project

#### Server configuration

The [config.yaml](./service/config.yaml) file provides configuration settings for the `grpc-subpub service`.

Below is an explanation of the key sections:

```yaml
app:
  environment: dev # Application environment: 'dev' for development, 'prod' for production.
  name: subpub-service # The name of the application.
  version: 0.1.0 # The version of the application.

grpc_server:
  port: 50051 # The port on which the gRPC server listens.
  max_idle: 10s # Maximum idle time for gRPC connections.
  timeout: 5s # Timeout for gRPC requests.
  startup_delay: 1s # Delay before the server starts accepting requests.
  shutdown_period: 7s # Graceful shutdown period for the server.
  publish_retry_attempts: 3 # Number of retry attempts for publishing messages.
  publish_retry_backoff: 100ms # Backoff duration between publish retries.
  ```

#### Build Locally

To build the project locally, ensure you have Go 1.24+ installed:

```bash
cd service
go build -o bin/service ./cmd/server
```

#### Build with Docker

To build the project using Docker:

```bash
docker build -t grpc-subpub:latest ./service
```

### Running the Server

#### Run Locally

To run the server locally:

```bash
cd service
go run cmd/server/main.go
```

#### Run with Docker

To run the server using Docker:

```bash
docker run -p 50051:50051 grpc-subpub:latest
```

#### Run with Docker Compose

To run the server using Docker Compose:

```bash
docker compose up --build
```

### Using the Test Client

A test client is provided in service/cmd/client/main.go. It demonstrates how to interact with the server.

Example: Subscribing and Publishing
Start the server with:

```bash
cd service
go run cmd/server/main.go
```

Run the client to subscribe to a topic and publish messages:

```bash
cd service
go run service/cmd/server/main.go
```

The client will:

1. Subscribe to the test topic.
2. Publish two messages: hello and hello again.
3. Print the received messages in order.

Below is the example client code:

```go

import (
    // necessary imports
)

func main() {
    // Start the gRPC server in a separate process on localhost:50051.
    // Create a new gRPC client connection to the server.
    const addr = "localhost:50051"

    conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
    if err != nil {
        log.Fatal(err)
    }
    defer conn.Close()

    // Create a new PubSub client using the generated gRPC client.
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
        wg.Done()

        // Start receiving messages from the stream and forward them to msgChan.
        for range 2 {
            event, err := stream.Recv()
            if err != nil {
                log.Fatal("failed to receive message:", err)
                return
            }
            msgChan <- event.GetData()
        }
    }(msgChan)

    // Wait for subscription confirmation before publishing.
    wg.Wait()

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
```

### Development Patterns

#### Graceful Shutdown

The server ensures a clean shutdown by:

- Stopping the gRPC server gracefully.
- Closing the net listener.
- Closing the in-memory pub-sub system.
- Cleaning up all active subscriptions by draining their message queue.

#### Dependency Injection

The server uses dependency injection for:

- Configuration (`config.Config`).
- Logging (`logger.Logger`).
- Pub-sub system (`subpub.SubPub`).

A `Must` pattern is also utilized in a generic form.

```go
func Must[T any](obj T, err error) T {
    if err != nil {
        panic(err)
    }
    return obj
}

func main() {
    const defaultConfigPath = "config.yaml"

    cfg := Must(config.Load(defaultConfigPath))

    log := logger.NewZap(cfg.App.Environment)
    defer log.Sync()

    sp := subpub.New()

    s := server.New(cfg, log, sp)

    if err := s.Run(); err != nil {
        panic(err)
    }
}
```

#### Local Running Queue

Each subscriber has a concurrency-safe local running queue built on channels to:

- Buffer messages.
- Ensure non-blocking message delivery.
- Handle slow subscribers without affecting others.

Take a look at the [queue.go](service/pkg/subpub/internal/queue/queue.go) for implementation details.

### Makefile Targets

The project includes a Makefile for common tasks:

Generate .pb:

```bash
make proto
```

Run unit tests:

```bash
make test
```

Run golangci-lint and protolint:

```bash
make lint
```

Start the Server with the Docker Compose:

```bash
make up
```

Stop the Server:

```bash
make down
```

### Docker and Docker Compose

Build the Docker Image:

```bash
docker build -t grpc-subpub:latest ./service
```

Run the Server with Docker:

```bash
docker run -p 50051:50051 grpc-subpub:latest
```

#### Use Docker Compose

The project includes a compose.yaml file for Docker Compose:

```bash
docker compose up --build
```

This will:

- Build the server image.
- Start the server on port 50051.

## Testing and Code Coverage

[goleak](https://github.com/uber-go/goleak) is used to ensure there are no leaking goroutines.

To run tests and collect coverage use `Makefile` target.

```bash
cd service
make test
```

This will output `coverage.out` to the `service` directory.

gRPC server test cases are available at [server_test.go](./service/internal/server/server_test.go).

## License

The project is licensed under an [MIT LICENSE](LICENSE).

<!--  -->
[ci-shield]: https://img.shields.io/github/actions/workflow/status/artem-burashnikov/grpc-subpub/.github%2Fworkflows%2Fci.yaml?style=for-the-badge&color=lightgreen
[license-shield]: https://img.shields.io/github/license/artem-burashnikov/grpc-subpub?style=for-the-badge&color=lightblue
