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

tools:
	go install github.com/yoheimuta/protolint/cmd/protolint@latest
	go install golang.org/x/tools/cmd/goimports@latest
	go install github.com/fullstorydev/grpcurl/cmd/grpcurl@latest
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh | sh -s -- -b $(go env GOPATH)/bin v2.1.6

test:
	go test -race -covermode=atomic -coverprofile=cover.out ./...
	go tool cover -html=cover.out -o cover.html
