## 1. Schema and Rendering Foundation

- [x] 1.1 Add a persisted per-user env-var data model with migrations, validation rules, and store-layer CRUD helpers.
- [x] 1.2 Implement built-in Fascinate machine vars plus `${NAME}` interpolation, including rejection of reserved-key overrides, undefined references, and cycles.
- [x] 1.3 Add an effective-env renderer that combines built-ins and user-defined vars for a specific machine without storing machine-rendered values in the database.

## 2. Guest Injection and Lifecycle Sync

- [x] 2.1 Extend VM bootstrap to write `/etc/fascinate/env`, `/etc/fascinate/env.sh`, `/etc/fascinate/env.json`, and `/etc/profile.d/fascinate-env.sh`.
- [x] 2.2 Extend restore/clone identity refresh so managed env files are re-rendered and rewritten for the target machine before it is marked ready.
- [x] 2.3 Add best-effort running-machine env sync after env-var updates so future shells and newly started app processes see the latest Fascinate-managed env files.

## 3. Control Plane, API, and Frontdoor Surface

- [x] 3.1 Add control-plane APIs for listing, setting, unsetting, and inspecting effective env vars for a user and machine.
- [x] 3.2 Expose HTTP endpoints for env-var CRUD and machine-specific effective-env inspection.
- [x] 3.3 Add SSH/frontdoor commands for env-var list/set/unset and machine env inspection.

## 4. Agent Guidance and Guest UX

- [x] 4.1 Update the VM-injected `AGENTS.md` / `CLAUDE.md` content so coding agents are told to prefer Fascinate env vars like `FASCINATE_PUBLIC_URL` over hardcoded hostnames.
- [x] 4.2 Ensure guest shell/profile integration exports the managed env vars for interactive sessions.
- [x] 4.3 Update repo docs and operator guidance to explain the new env-var object, guest file locations, and clone-safe usage patterns.

## 5. Hardening and Validation

- [x] 5.1 Add tests for env-var validation, interpolation, effective rendering, and reserved-key protection.
- [x] 5.2 Add runtime/control-plane tests for create, restore, clone, and running-machine sync rewriting the managed env files correctly.
- [x] 5.3 Add API/frontdoor coverage for env-var CRUD and machine-specific env inspection.
- [x] 5.4 Re-run `go test ./...` and `make verify-ops`, plus any targeted live validation needed to prove env files are correct on create and clone.
