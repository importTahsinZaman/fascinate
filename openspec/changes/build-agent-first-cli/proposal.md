## Why

Fascinate already has the control-plane and VM primitives needed to support a full command-line product surface, but the current shell model is still browser-centric and in-process. To let users and AI agents operate Fascinate entirely from the CLI while staying in sync with the web app, Fascinate needs backend-owned shell resources, real-time cross-surface state propagation, and a CLI contract designed for both humans and automation.

## What Changes

- Add a first-class user CLI for machine, snapshot, env-var, diagnostics, and shell management so Fascinate can be used entirely without the web UI.
- Add a public CLI distribution path with versioned platform-specific release artifacts and a curl-installable bootstrap script so users can install Fascinate without cloning the repo.
- Add durable backend-owned shell resources that can be created, listed, attached, written to, tailed, resized, and deleted from either the CLI or the web app.
- Add real-time cross-surface synchronization so shell creation, deletion, attachment state, and terminal activity become visible immediately across CLI and web clients.
- Add agent-optimized non-interactive command execution with structured machine-readable output, stable exit behavior, and predictable automation semantics.
- Replace browser-only shell/session assumptions with a shared multi-surface control-plane model; no backwards-compatibility constraints are required for the current browser-only terminal session contract or workspace coupling.
- Expand automated coverage to include CLI/API/web interoperability, restart recovery, shell durability, and agent-oriented execution reliability.

## Capabilities

### New Capabilities
- `cli-command-center`: authenticated CLI surface for managing machines, snapshots, env vars, diagnostics, and shell discovery/operations with strong human and machine UX
- `cli-distribution`: public release artifacts and curl-installable distribution flow for the Fascinate CLI
- `shared-shell-sessions`: durable backend-owned shell resources, shared attachments, and real-time synchronization between CLI and web clients
- `agent-command-execution`: structured non-interactive command execution optimized for AI agents and automation workflows

### Modified Capabilities
- `host-aware-vm-operations`: shell and exec requests must resolve through the owning host and current machine power state rather than browser-only gateway assumptions
- `fascinate-env-vars`: env-var management and effective machine-env inspection must be available through the CLI as a first-class Fascinate surface
- `platform-diagnostics`: diagnostics must expose shared shell/session state, attachment health, event delivery, and agent execution failures in addition to existing VM and snapshot diagnostics

## Impact

- Affected code includes `cmd/fascinate/`, `internal/httpapi/`, `internal/browserterm/`, `internal/controlplane/`, `internal/database/`, `internal/app/`, and `web/`
- New CLI UX, auth/token handling, shell/event APIs, durable shell/session persistence, structured agent execution flows, and public CLI release/install tooling
- Likely replacement of the current browser-only in-memory terminal session model and workspace/session coupling
- Expanded Go, web, and end-to-end interoperability tests for CLI, API, web, and restart behavior
