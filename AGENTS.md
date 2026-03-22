# AGENTS.md

Fascinate is a terminal-first control plane for persistent Cloud Hypervisor developer VMs, including full-VM snapshots, true cloning, and persisted tool auth.

This repo uses Go modules and Make; there is no root JavaScript package manager.

## Commands
- Format changed Go files: `gofmt -w path/to/file.go ...`
- Test a changed package: `go test ./path/to/package/...`
- Run the full test suite: `go test ./...`
- Build the binary: `make build`
- Verify ops scripts: `make verify-ops`
- Host lifecycle smoke (configured host only): `bash ops/host/smoke.sh`
- Host workload stress (configured host only): `bash ops/host/stress.sh`
- Host benchmark (configured host only): `bash ops/host/benchmark.sh`
- Snapshot smoke (configured host only): `bash ops/host/smoke-snapshots.sh`
- Tool-auth persistence harness (configured host only): `bash ops/host/smoke-tool-auth.sh`
- Host diagnostics helper: `bash ops/host/diagnostics.sh <hosts|machine|snapshot|tool-auth|events> ...`

## Testing
- Prefer package-scoped tests first, then `go test ./...` when changes cross packages.
- Any change under `ops/` or VM/runtime/deploy flows should also run `make verify-ops`.
- Only run the host smoke and benchmark scripts on a machine that is already bootstrapped for Fascinate, and only when the task calls for live validation.
- Treat `ops/host/smoke-tool-auth.sh` as a targeted harness for tool-auth changes, not as the default always-run host acceptance smoke.
- Add or update tests for behavioral changes, especially around control-plane state transitions, snapshots/cloning, shell entry, and tool-auth persistence.

## Project Structure
- `cmd/fascinate/main.go` — CLI entrypoint and admin commands
- `internal/app/` — app wiring, background loops, adapter registration
- `internal/config/` — environment config and env-file loading
- `internal/controlplane/` — machine and snapshot orchestration, state transitions, quotas
- `internal/controlplane/hosts.go` — first-class host registry, heartbeat, placement, and host dispatch
- `internal/runtime/cloudhypervisor/` — VM runtime, network namespaces, snapshots, restore/clone
- `internal/sshfrontdoor/` — SSH command handling, guest shell handoff, dashboard launch
- `internal/tui/` — Bubble Tea dashboard and signup flows
- `internal/toolauth/` — persisted auth adapters for Claude, Codex, and GitHub CLI
- `ops/host/` — bootstrap, deploy, verify, and live smoke scripts
- `docs/stress-matrix.md` — expectation matrix and live validation guidance
- `openspec/` — current and archived change/spec history

## Code Style
- Follow existing Go patterns: small structs, explicit errors, narrow helper functions, and `gofmt` output.
- Keep user-visible state transitions explicit (`CREATING`, `RUNNING`, `FAILED`, `DELETING`) and cover them with tests.
- Preserve the current architecture: Cloud Hypervisor VMs, per-VM network namespaces/forwarders, and host-managed persisted tool auth.
- Preserve the current host model: even on one box, machines and snapshots are host-owned and runtime work should flow through the host executor boundary.

## Boundaries

**Always:**
- Keep diffs focused; do not mix unrelated cleanup with functional changes.
- Update tests and, when user-visible behavior changes, update `README.md` and relevant active OpenSpec docs.
- Prefer changing the existing runtime/control-plane path over adding parallel legacy behavior.

**Ask first:**
- Any live-host action that changes data or availability, including deploys, prod smoke runs, or wiping users/VMs/snapshots/tool-auth data.
- Schema migrations or persisted-format changes for snapshots or tool auth.
- Adding dependencies, changing Cloudflare/Caddy/DNS behavior, or changing default machine quotas or sizes.

**Never:**
- Reintroduce Incus/container-era assumptions into runtime or docs.
- Edit files under `openspec/changes/archive/` except to read historical context.
- Commit secrets, auth bundles, host-specific keys, or machine state from `data/` or `/var/lib/fascinate/`.
- Remove tests or smoke coverage just to get a green run.
