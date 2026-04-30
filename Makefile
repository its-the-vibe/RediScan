.PHONY: build test lint ci clean

BINARY_NAME := rediscan

build:
	go build -o $(BINARY_NAME) .

test:
	go test -v ./...

lint:
	go vet ./...

ci: lint build test

clean:
	rm -f $(BINARY_NAME)
