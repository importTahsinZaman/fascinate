GO ?= go
BINARY ?= fascinate

.PHONY: fmt test build run verify-ops smoke-host

fmt:
	gofmt -w cmd internal

test:
	$(GO) test ./...

build:
	mkdir -p bin
	$(GO) build -o bin/$(BINARY) ./cmd/fascinate

run:
	$(GO) run ./cmd/fascinate serve

verify-ops:
	bash -n ops/host/bootstrap.sh
	bash -n ops/host/configure-admin-ssh.sh
	bash -n ops/host/smoke.sh
	bash -n ops/host/verify.sh
	bash -n ops/host/write-caddyfile.sh
	bash -n ops/host/install-control-plane.sh
	bash -n ops/cloudhypervisor/build-base-image.sh

smoke-host:
	bash ops/host/smoke.sh
