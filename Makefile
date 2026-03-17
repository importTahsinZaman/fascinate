GO ?= go
BINARY ?= fascinate

.PHONY: fmt test build run

fmt:
	gofmt -w cmd internal

test:
	$(GO) test ./...

build:
	mkdir -p bin
	$(GO) build -o bin/$(BINARY) ./cmd/fascinate

run:
	$(GO) run ./cmd/fascinate serve
