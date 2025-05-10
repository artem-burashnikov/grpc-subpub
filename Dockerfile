FROM golang:1.24.3@sha256:39d9e7d9c5d9c9e4baf0d8fff579f06d5032c0f4425cdec9e86732e8e4e374dc AS cache

WORKDIR /modules

COPY go.mod go.sum ./

RUN go mod download

FROM golang:1.24.3@sha256:39d9e7d9c5d9c9e4baf0d8fff579f06d5032c0f4425cdec9e86732e8e4e374dc AS build

COPY --from=cache /go/pkg /go/pkg

RUN apt update && apt install -y protobuf-compiler && \
    go install google.golang.org/protobuf/cmd/protoc-gen-go@latest && \
    go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

ENV PATH="$PATH:$(go env GOPATH)/bin"

WORKDIR /app
COPY . .

RUN protoc --proto_path=api/proto --go_out=api/pb --go_opt=paths=source_relative \
           --go-grpc_out=api/pb --go-grpc_opt=paths=source_relative \
           api/proto/subpub.proto

ENV CGO_ENABLED=0
RUN go build -trimpath -ldflags "-s -w" -o /go/bin/app ./cmd/server

FROM scratch AS final
LABEL author="a.burashnikov"
COPY --from=build /go/bin/app /app

ENTRYPOINT [ "/app" ]
