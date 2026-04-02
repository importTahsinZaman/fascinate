# AGENTS.md

Fascinate is a browser-first command center for persistent Cloud Hypervisor developer VMs, including full-VM snapshots, true forking, managed env vars, and low-latency browser terminals.

This repo uses Go modules at the root and a dedicated `web/` package that uses `pnpm`.

## Commands
- Format changed Go files: `gofmt -w path/to/file.go ...`
- Test a changed Go package: `go test ./path/to/package/...`
- Run the full backend suite: `go test ./...`
- Install web dependencies: `make web-install`
- Run the web app against a live local backend: `make web-dev`
- Run the web app in UI-only mock mode: `make web-dev-mock`
- Test the web app: `make web-test`
- Build the web app: `make web-build`
- Build the full product (Go binary + web app): `make build`
- Verify ops scripts: `make verify-ops`
- Frontend-only no-restart deploy (configured host only): `sudo ./ops/host/deploy-web.sh`
- Host lifecycle smoke (configured host only): `bash ops/host/smoke.sh`
- Host workload stress (configured host only): `bash ops/host/stress.sh`
- Host benchmark (configured host only): `bash ops/host/benchmark.sh`
- Snapshot smoke (configured host only): `bash ops/host/smoke-snapshots.sh`
- Tool-auth persistence harness (configured host only): `bash ops/host/smoke-tool-auth.sh`
- Host diagnostics helper: `bash ops/host/diagnostics.sh <hosts|machine|snapshot|tool-auth|events>`

## Testing
- Prefer package-scoped tests first, then `go test ./...` when changes cross packages.
- Any web change should also run `make web-test` and `make web-build`.
- Any change under `ops/` or VM/runtime/deploy flows should also run `make verify-ops`.
- Only run host smokes, benchmark, or deploy validation on a machine that is already bootstrapped for Fascinate, and only when the task calls for live validation.
- Treat `ops/host/smoke-tool-auth.sh` as a targeted harness for tool-auth changes, not as the default always-run host acceptance smoke.
- Add or update tests for behavioral changes around browser auth, workspace persistence, machine/snapshot/env-var management, and terminal session issuance.

## Project Structure
- `cmd/fascinate/main.go` — server entrypoint and admin commands
- `internal/app/` — app wiring, background loops, browser auth/terminal gateway registration
- `internal/browserauth/` — email-code browser auth and opaque DB-backed web sessions
- `internal/browserterm/` — browser terminal session issuance, host-local PTY gateway, and diagnostics
- `internal/config/` — environment config and env-file loading
- `internal/controlplane/` — machine/snapshot orchestration, env vars, host placement, and state transitions
- `internal/runtime/cloudhypervisor/` — VM runtime, network namespaces, snapshots, restore/fork
- `internal/httpapi/` — REST API, SPA/static serving, browser auth endpoints, terminal APIs
- `internal/toolauth/` — persisted auth adapters for Claude, Codex, and GitHub CLI
- `web/` — React/Vite browser command center, workspace canvas, and xterm-based terminal windows
- `ops/host/` — bootstrap, deploy, verify, and live smoke scripts
- `docs/development-and-deploy.md` — local frontend workflow and deploy guidance
- `docs/stress-matrix.md` — expectation matrix and live validation guidance
- `openspec/` — active and archived change/spec history

## Code Style
- Follow existing Go patterns: small structs, explicit errors, narrow helper functions, and `gofmt` output.
- Follow the existing React patterns in `web/`: keep terminal byte streams out of React state, use localized Zustand state for workspace layout, and avoid loading heavy terminal code on the initial bundle when a lazy boundary is sufficient.
- Keep user-visible state transitions explicit (`CREATING`, `RUNNING`, `FAILED`, `DELETING`) and cover them with tests.
- Preserve the current architecture: Cloud Hypervisor VMs, per-VM network namespaces/forwarders, managed env vars, host-owned machines/snapshots, and host-local browser terminal gateways.

## Boundaries

**Always:**
- Keep diffs focused; do not mix unrelated cleanup with functional changes.
- Update tests and, when user-visible behavior changes, update `README.md` and relevant active OpenSpec docs.
- Prefer changing the browser-first control-plane/runtime path over preserving old TUI-first assumptions.

**Ask first:**
- Any live-host action that changes data or availability, including deploys, prod smoke runs, or wiping users/VMs/snapshots/tool-auth data.
- Schema migrations or persisted-format changes for snapshots, tool auth, managed env vars, browser sessions, or workspace layouts.
- Adding dependencies, changing Cloudflare/Caddy/DNS behavior, or changing default machine quotas or sizes.

**Never:**
- Reintroduce terminal-first/TUI-first product assumptions into new browser-facing code or docs.
- Edit files under `openspec/changes/archive/` except to read historical context.
- Commit secrets, auth bundles, host-specific keys, or machine state from `data/` or `/var/lib/fascinate/`.
- Remove tests or smoke coverage just to get a green run.
