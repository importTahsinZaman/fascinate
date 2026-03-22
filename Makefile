GO ?= go
BINARY ?= fascinate
PNPM ?= pnpm

.PHONY: fmt test build run web-install web-build web-test verify-ops smoke-host smoke-snapshots smoke-tool-auth stress-host benchmark-host

fmt:
	gofmt -w cmd internal

test:
	$(GO) test ./...
	$(MAKE) web-test

build:
	mkdir -p bin
	$(GO) build -o bin/$(BINARY) ./cmd/fascinate
	$(MAKE) web-build

run:
	$(GO) run ./cmd/fascinate serve

web-install:
	cd web && $(PNPM) install

web-build: web-install
	cd web && $(PNPM) build

web-test: web-install
	cd web && $(PNPM) test

verify-ops:
	bash -n ops/host/bootstrap.sh
	bash -n ops/host/configure-admin-ssh.sh
	bash -n ops/host/diagnostics.sh
	bash -n ops/host/smoke.sh
	bash -n ops/host/benchmark.sh
	bash -n ops/host/stress.sh
	bash -n ops/host/smoke-snapshots.sh
	bash -n ops/host/smoke-tool-auth.sh
	bash -n ops/host/verify.sh
	bash -n ops/host/write-caddyfile.sh
	bash -n ops/host/install-control-plane.sh
	bash -n ops/cloudhypervisor/build-base-image.sh

smoke-host:
	bash ops/host/smoke.sh

smoke-snapshots:
	bash ops/host/smoke-snapshots.sh

smoke-tool-auth:
	bash ops/host/smoke-tool-auth.sh

stress-host:
	bash ops/host/stress.sh

benchmark-host:
	bash ops/host/benchmark.sh
