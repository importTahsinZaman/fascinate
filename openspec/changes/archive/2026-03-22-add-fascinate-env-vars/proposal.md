## Why

Fascinate needs a first-class environment-variable system so agents can configure apps against stable machine metadata instead of hardcoding per-VM hostnames into repo files. This matters now because snapshot and clone correctness depends on machine-specific values like the public URL being regenerated for the target VM, not copied forward from the source VM.

## What Changes

- Add a first-class user env-var object managed by the control plane.
- Add built-in read-only Fascinate machine vars, including the machine name, public URL, primary port, base domain, host ID, and host region.
- Inject rendered env vars into every VM through canonical guest files under `/etc/fascinate/` plus shell/profile integration.
- Re-render and rewrite env files on machine create, snapshot restore, and clone so target machines get fresh machine-specific values.
- Expose CRUD and inspection surfaces for env vars over the API and SSH/frontdoor.
- Update VM-injected `AGENTS.md` and `CLAUDE.md` guidance so coding agents know to use Fascinate env vars instead of hardcoded machine hostnames.
- **BREAKING**: guest env files under `/etc/fascinate/` become control-plane-managed artifacts that Fascinate may rewrite on create, restore, clone, and env-var updates.

## Capabilities

### New Capabilities
- `fascinate-env-vars`: user-defined and machine-scoped Fascinate environment variables, guest injection, rendering, and inspection behavior

### Modified Capabilities
- `persistent-tool-auth`: tool-auth and VM instructions should explain and coexist with the new Fascinate env-var files and machine metadata inside guests

## Impact

- Affected code: `internal/controlplane/`, `internal/database/`, `internal/httpapi/`, `internal/sshfrontdoor/`, `internal/runtime/cloudhypervisor/`, `internal/tui/`, `internal/toolauth/`, `ops/host/`, and guest bootstrap/restore paths
- New API and SSH/frontdoor surfaces for env-var CRUD and inspection
- New SQLite tables or records for persisted env vars
- VM bootstrap and restore logic will gain new `/etc/fascinate/env*` managed files and profile hooks
