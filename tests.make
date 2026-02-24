.PHONY: test test-cover fmt

test:
	go test ./tests -v

test-cover:
	go test ./tests -cover -v

fmt:
	go fmt ./...

all: fmt test