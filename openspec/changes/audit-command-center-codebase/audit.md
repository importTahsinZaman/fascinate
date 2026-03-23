# Command Center Audit

## Scope Inventory

This audit covered the browser-first Fascinate stack that currently powers the web command center:

- Frontend workspace and terminal UI in `web/src/`
- Browser auth and REST API handling in `internal/httpapi/` and `internal/browserauth/`
- Browser terminal session lifecycle in `internal/browserterm/`
- Control-plane orchestration in `internal/controlplane/`
- Cloud Hypervisor runtime code in `internal/runtime/cloudhypervisor/`
- Host bootstrap/deploy/smoke scripts in `ops/host/`
- Active OpenSpec contracts in:
  - `openspec/specs/fascinate-env-vars/spec.md`
  - `openspec/specs/host-aware-vm-operations/spec.md`
  - `openspec/specs/host-registry/spec.md`
  - `openspec/specs/persistent-tool-auth/spec.md`
  - `openspec/specs/platform-diagnostics/spec.md`
  - `openspec/specs/platform-stress-validation/spec.md`
  - `openspec/specs/vm-snapshots/spec.md`

Existing validation commands reviewed as part of the audit baseline:

- `go test ./...`
- `make web-test`
- `make verify-ops`

Audit baseline on March 22, 2026:

- `go test ./...` passed
- `make web-test` passed
- `make verify-ops` passed

## Findings Format

Each accepted finding is recorded with:

- `Priority`: `P0` through `P2`
- `Type`: `bug`, `simplification`, `removal`, or `test-gap`
- `Subsystem`
- `Evidence`
- `Impact`
- `Recommended action`

Priority guidance used in this report:

- `P0`: likely correctness or security boundary issue
- `P1`: important reliability or regression risk
- `P2`: worthwhile cleanup or coverage hardening with lower immediate risk

## Findings

### F1. [P0][bug][httpapi/auth] Browser-owned API routes trust explicit `owner_email` before authenticating the browser session

**Evidence**

- `internal/httpapi/auth.go:47-59` returns the explicit owner email immediately:
  - `if explicit != "" { return explicit, nil }`
- `internal/httpapi/server.go` uses `ownerEmailForRequest(...)` for machines, forks, env vars, snapshots, and diagnostics routes.
- The current server tests encode this behavior as the standard request shape:
  - `internal/httpapi/server_test.go:326-455`
  - `internal/httpapi/server_test.go:458-535`
  - `internal/httpapi/server_test.go:739-819`

**Impact**

- Any browser-facing route that accepts `owner_email` can operate on an arbitrary owner identity without proving that the current session belongs to that owner.
- This is a cross-user authorization risk, not just a cleanup issue.

**Recommended action**

- Remove `owner_email` from browser-facing machine, snapshot, env-var, and diagnostics routes, or gate explicit owner override behind a separate trusted/admin-only path.
- Make browser-authenticated routes derive ownership from the session by default.
- Add explicit negative tests for:
  - unauthenticated requests with `owner_email`
  - authenticated requests whose cookie user does not match the supplied `owner_email`

### F2. [P1][test-gap][httpapi] API tests currently reinforce the wrong auth model and do not cover cross-user authorization failures

**Evidence**

- `internal/httpapi/server_test.go:326-455` treats query/body `owner_email` as the expected happy path for machine list/create/fork/delete.
- `internal/httpapi/server_test.go:458-535` does the same for diagnostics.
- `internal/httpapi/server_test.go:739-819` does the same for env vars and machine env.
- Browser-session tests exist for workspace persistence and terminal issuance, but not for the broader machine/env/snapshot routes:
  - `internal/httpapi/server_test.go:864-1059`

**Impact**

- Even if the auth boundary is fixed, the current test suite is positioned to miss a regression or actively push future changes back toward insecure `owner_email` handling.

**Recommended action**

- Add a focused authorization test matrix for all browser-owned routes.
- Rework handler tests so browser-session cookies are the default access pattern for browser APIs.
- Add cross-user denial assertions before changing any production auth code.

### F3. [P1][bug][ops/deploy] Host deploy relies on root-local `pnpm` or `corepack`, making frontend publishes brittle and environment-dependent

**Evidence**

- `ops/host/install-control-plane.sh:19-29` resolves `pnpm` only from the current execution environment.
- `ops/host/install-control-plane.sh:117-125` always rebuilds the frontend inside the root-run install script.
- The same script requires root via `ops/host/install-control-plane.sh:48-53`.

**Impact**

- Deploy success depends on the root user's PATH and toolchain availability, not just the repo state.
- This is exactly the failure mode that leads to "backend updated, frontend stale" deploys and manual dist copies.

**Recommended action**

- Stop rebuilding the frontend inside the privileged install step, or pass an explicit build artifact into the root-owned install flow.
- If host-side builds remain necessary, make the script accept an explicit `pnpm` path and fail with a more actionable preflight.
- Add one execution-level test/smoke that exercises the install path instead of only parsing shell syntax.

### F4. [P1][test-gap][ops] `make verify-ops` only syntax-checks host scripts, so deploy/runtime regressions can pass CI unnoticed

**Evidence**

- `Makefile:31-43` defines `verify-ops` entirely as `bash -n ...`.
- That validates shell syntax only; it does not exercise tool discovery, generated files, systemd behavior, or publish/install steps.

**Impact**

- Regressions in deploy preconditions, Caddy generation, or install-time behavior can still produce a green `make verify-ops`.
- The audit baseline passing `make verify-ops` should not be interpreted as high confidence in deploy correctness.

**Recommended action**

- Keep the current syntax pass, but add at least one non-destructive execution harness for:
  - `ops/host/install-control-plane.sh`
  - `ops/host/write-caddyfile.sh`
- Treat those as host-script contract tests, not as optional manual verification.

### F5. [P2][simplification][ops] The control-plane installer starts Fascinate twice on every deploy

**Evidence**

- `ops/host/install-control-plane.sh:171-174` runs:
  - `systemctl enable --now fascinate`
  - `systemctl restart fascinate`

**Impact**

- This is unnecessary churn during deploy and obscures the exact lifecycle semantics of the installer.
- It also makes troubleshooting startup behavior noisier than it needs to be.

**Recommended action**

- Collapse this to one deliberate start/restart path.
- If the intent is "enable if needed, then restart with the new binary", make that explicit and document it.

### F6. [P2][simplification][frontend/store] Workspace layout persists fixed-size window dimensions that are immediately discarded on hydrate

**Evidence**

- Fixed shell size is hard-coded in `web/src/store.ts:23-25`.
- New windows always use that fixed size in `web/src/store.ts:63-77`.
- Hydration ignores persisted window sizes and replaces them with the fixed size in `web/src/store.ts:38-50`.
- Overlap detection also assumes the fixed size globally in `web/src/store.ts:196-240`.

**Impact**

- Width and height are currently dead persisted state for shells.
- The layout shape is more complex than the UI behavior actually supports.

**Recommended action**

- Either stop persisting per-window width/height for fixed-size shell windows, or make those fields real again.
- Prefer the simpler option unless resizable windows are coming back.

### F7. [P2][simplification][frontend/terminal] The terminal WebGL fallback path still mutates a removed `note` field

**Evidence**

- `web/src/terminal.tsx:106-109` still does:
  - `setStats((current) => ({ ...current, note: "renderer fallback enabled" }))`
- `TerminalStats` no longer defines `note`; the visible status strip that used to consume it has already been removed.

**Impact**

- This is stale state plumbing left behind after the shell overlay redesign.
- It is harmless today, but it is exactly the kind of residue that makes terminal state harder to reason about later.

**Recommended action**

- Remove the dead `note` write and keep the renderer fallback behavior limited to terminal output plus any intentionally supported UI state.

### F8. [P1][bug][frontend/shell-lifecycle] Closing a shell removes it from the UI before the backend close succeeds, with no rollback or user feedback

**Evidence**

- `web/src/app.tsx:262-266` closes the local window first and then fires `deleteTerminalSession(...)` without awaiting it or handling errors.
- `web/src/app.tsx:1072-1074` uses the same fire-and-forget pattern from the window header close path.

**Impact**

- A transient network or API failure can leave the terminal session running on the backend while the UI has already discarded it.
- The user gets no failure state and no retry path.

**Recommended action**

- Either await close success before removing the window, or keep the optimistic close but add rollback/error handling.
- Add one frontend test that covers delete-session failure and asserts the chosen recovery behavior.

### F9. [P1][test-gap][browserterm] Session detach/error/expiry lifecycle is largely untested despite a non-trivial state machine

**Evidence**

- `internal/browserterm/manager.go:507-600` contains explicit logic for:
  - expiry pruning
  - attach failure accounting
  - `CONNECTED` / `DETACHED` / `ERROR` transitions
  - TTL extension on touch
- `internal/browserterm/manager_test.go:34-181` only covers:
  - create
  - remote-host rejection
  - token rotation
  - close removal
  - tmux command generation
  - clean websocket close classification
- There are no targeted tests for:
  - expired session pruning
  - detached session reattach after disconnect
  - attach failure status/diagnostics accounting
  - TTL extension behavior

**Impact**

- Browser shell persistence is one of the most coupled parts of the product, but some of the riskiest transitions are still unguarded by direct tests.

**Recommended action**

- Add focused manager tests for detached, expired, and attach-failure paths.
- Verify diagnostics counters and session metadata, not just success-path creation.

### F10. [P2][simplification][frontend/app] `CommandCenter` is carrying too many unrelated responsibilities in one component

**Evidence**

- `web/src/app.tsx:136-686` mixes:
  - query wiring
  - modal state
  - mutation orchestration
  - machine/snapshot/env-var rendering
  - shell close/focus behavior
  - sidebar composition
- `web/src/app.tsx:688-1189` then continues with workspace canvas, focus animation, gesture routing, and window rendering in the same file.

**Impact**

- The component is still workable, but it is becoming the place where unrelated regressions converge.
- Small UX changes increasingly require touching query logic, modal logic, and canvas behavior in one file.

**Recommended action**

- Split `CommandCenter` into at least:
  - sidebar/actions
  - modal manager
  - workspace canvas/window layer
- Keep state ownership the same; this is a structural simplification, not a feature rewrite.

## Non-Findings

The audit did not find another high-confidence correctness issue in:

- `internal/controlplane/service.go` beyond the surrounding auth boundary and follow-up coverage concerns
- `internal/runtime/cloudhypervisor/runtime.go` and snapshot flows at the same severity as F1/F3
- the current web test and Go test baseline, which is passing as of this audit

That does not mean those areas are perfect; it means the strongest actionable issues in this pass clustered around authorization boundaries, deploy reliability, shell-session lifecycle coverage, and UI simplification.

## Prioritized Remediation Plan

### Follow-up 1: Lock browser ownership to the authenticated session

Scope:

- Fix `ownerEmailForRequest(...)` so browser-owned routes cannot be steered with arbitrary `owner_email`.
- Remove or isolate explicit owner override behavior.
- Add negative authz coverage for machine, snapshot, env-var, and diagnostics routes.

Why first:

- This is the highest-confidence correctness and security issue in the audit.

### Follow-up 2: Harden deploy/install correctness

Scope:

- Rework `ops/host/install-control-plane.sh` so frontend publishing does not depend on root-local Node tooling.
- Remove redundant service restart behavior.
- Add executable validation around installer and Caddyfile generation.

Why second:

- This directly affects shipping confidence and explains recent deploy flakiness.

### Follow-up 3: Tighten browser shell lifecycle behavior

Scope:

- Fix shell close behavior so backend close failures are surfaced or rolled back.
- Add manager tests for detached/error/expired terminal sessions.

Why third:

- Persistent browser terminals are a flagship workflow and deserve stronger lifecycle guarantees.

### Follow-up 4: Remove dead frontend state and simplify fixed-window layout

Scope:

- Remove stale terminal `note` state.
- Simplify fixed window layout persistence so it matches real behavior.
- Break up `CommandCenter` into smaller surfaces while preserving existing product behavior.

Why fourth:

- These are lower-risk cleanups, but they will reduce future regression pressure on the web app.
