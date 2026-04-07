GO ?= go
BINARY ?= fascinate
PNPM ?= pnpm
FASCINATE_DEV_PROXY_TARGET ?= http://127.0.0.1:8080

.PHONY: fmt test build run web-install web-dev web-dev-mock web-build web-test verify-ops smoke-host smoke-snapshots smoke-tool-auth stress-host benchmark-host

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

web-dev: web-install
	cd web && FASCINATE_DEV_PROXY_TARGET=$(FASCINATE_DEV_PROXY_TARGET) $(PNPM) dev

web-dev-mock: web-install
	cd web && VITE_FASCINATE_UI_MOCK=1 $(PNPM) dev

web-build: web-install
	cd web && $(PNPM) build

web-test: web-install
	cd web && $(PNPM) test

verify-ops:
	bash -n ops/host/bootstrap.sh
	bash -n ops/host/configure-admin-ssh.sh
	bash -n ops/host/diagnostics.sh
	bash -n ops/release/lib.sh
	bash -n ops/release/verify-artifact.sh
	bash -n ops/release/build-cli-artifact.sh
	bash -n ops/release/build-cli-release-index.sh
	bash -n ops/release/build-full-artifact.sh
	bash -n ops/release/build-web-artifact.sh
	bash -n ops/release/deploy-full-artifact.sh
	bash -n ops/release/deploy-web-artifact.sh
	bash -n ops/release/test-cli-distribution.sh
	bash -n ops/host/smoke.sh
	bash -n ops/host/benchmark.sh
	bash -n ops/host/stress.sh
	bash -n ops/host/smoke-snapshots.sh
	bash -n ops/host/smoke-tool-auth.sh
	bash -n ops/host/verify.sh
	bash -n ops/host/write-caddyfile.sh
	bash -n ops/host/deploy-web.sh
	bash -n ops/host/install-control-plane.sh
	bash -n ops/host/reset-runtime-state.sh
	bash -n ops/cloudhypervisor/build-base-image.sh
	bash -n install.sh

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
