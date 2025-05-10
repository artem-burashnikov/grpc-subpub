.PHONY: lint protolint golint tools

lint: protolint golint

protobuf:
	protoc --proto_path=api/proto \
			--go_out=api/pb --go_opt=paths=source_relative \
			--go-grpc_out=api/pb --go-grpc_opt=paths=source_relative \
			api/proto/subpub.proto

protolint:
	protolint api/proto/*

golint:
	golangci-lint run -v ./...

test:
	go test -race -covermode=atomic -coverprofile=cover.out ./...
	go tool cover -html=cover.out -o cover.html
