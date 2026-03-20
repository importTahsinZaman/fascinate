## Why

Fascinate is supposed to let users give each coding agent its own VM, but today auth state for tools like Claude Code is trapped inside each guest. That means users repeat login flows per machine, which is friction the platform should remove.

## What Changes

- Introduce a host-managed persistent tool-auth framework that stores per-user auth state separately from any single VM.
- Support multiple auth persistence modes through tool-specific adapters:
  - session-state bundles for interactive login tools
  - centrally projected secret material for API-key tools
  - provider-credential adapters for cloud-backed auth flows
- Restore supported tool auth into a new VM before the machine is marked ready.
- Capture updated auth state from running VMs and persist it back to the user’s Fascinate account for later machines.
- Keep stored auth encrypted at rest and isolated by Fascinate user, tool, and auth method.
- Ship the first adapter for Claude Code subscription login in the initial implementation.

## Capabilities

### New Capabilities
- `persistent-tool-auth`: Persist, restore, and update per-user agent-tool auth state across Fascinate VMs using tool/method-specific adapters.

### Modified Capabilities

## Impact

- Affected code:
  - `internal/runtime/cloudhypervisor`
  - `internal/sshfrontdoor`
  - `internal/controlplane`
  - VM bootstrap and guest tool setup paths
  - new host-side storage, encryption, and sync helpers for user auth state
- Affected systems:
  - per-user state storage under Fascinate’s host data directory
  - machine readiness and guest hydration flow
  - shell/tutorial session teardown and background reconciliation
  - future tool adapters for Claude Code, Codex, OpenCode, and similar CLIs
- Security impact:
  - user tool auth becomes host-managed sensitive state and must be encrypted at rest
  - guest-to-user mapping and per-user isolation become part of the auth persistence contract
