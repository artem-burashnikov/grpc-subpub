.PHONY: lint protolint golint tools

lint: protolint golint

protobuf:
	protoc --go_out=. --go_opt=paths=source_relative \
			--go-grpc_out=. --go-grpc_opt=paths=source_relative \
			proto/subpub.proto

protolint:
	protolint proto/*

golint:
	golangci-lint run -v ./...

tools:
	go install github.com/yoheimuta/protolint/cmd/protolint@latest
	go install golang.org/x/tools/cmd/goimports@latest
	go install github.com/fullstorydev/grpcurl/cmd/grpcurl@latest
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh | sh -s -- -b $(go env GOPATH)/bin v2.1.6
