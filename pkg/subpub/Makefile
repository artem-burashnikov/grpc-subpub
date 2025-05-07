.PHONY: test coverage clean

test:
	@go test -v -race ./...

coverage:
	@go test -coverprofile=coverage.out -covermode=atomic -race ./...
	@go tool cover -func=coverage.out

clean:
	@rm coverage.out
