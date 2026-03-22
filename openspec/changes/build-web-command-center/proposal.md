## Why

Fascinate now has the VM, snapshot, host, and env-var foundations needed to become a real browser-based command center for running many coding-agent sessions at once. The current terminal-first UX is the wrong product surface for that goal; users need a low-latency web workspace where they can create VMs, manage snapshots and env vars, and keep multiple live terminals visible at the same time.

## What Changes

- Add a first-class browser web app at `fascinate.dev` as the primary Fascinate product surface.
- Add a canvas-style workspace where users can open multiple terminal sessions, drag and resize them, and keep many VM and agent shells visible at once.
- Add browser workflows for creating and deleting machines, creating and restoring snapshots, cloning VMs, and creating and managing user env vars.
- Add browser authentication and persisted workspace/session state appropriate for a long-lived web command center.
- Add low-latency browser terminal streaming backed by host-aware terminal session routing rather than terminal/TUI-specific assumptions.
- Add terminal/session observability and latency diagnostics so the web workspace can be operated against explicit performance targets.
- **BREAKING**: replace terminal-first product assumptions with a browser-first command center. Backwards compatibility with the existing Bubble Tea dashboard and terminal-centric UX is not required.

## Capabilities

### New Capabilities
- `browser-command-center`: authenticated web application for machine, snapshot, and env-var management plus launching terminal workspaces
- `browser-terminal-workspaces`: canvas-style multi-terminal workspace, session lifecycle, layout persistence, and low-latency browser terminal behavior

### Modified Capabilities
- `host-aware-vm-operations`: machine shell/session routing should resolve through the owning host's browser terminal gateway rather than only terminal/TUI-oriented shell entry flows
- `fascinate-env-vars`: env-var management and effective machine-env inspection should be available through the browser command center, not just API or SSH/frontdoor surfaces
- `platform-diagnostics`: operators should be able to inspect browser-terminal session health, routing, and latency diagnostics in addition to VM and snapshot diagnostics

## Impact

- Affected code includes the control plane, HTTP server, host executor boundary, shell/session transport, diagnostics, config, deploy scripts, and any product surfaces that currently assume terminal-first interaction
- New frontend code, build tooling, browser authentication/session handling, and browser terminal transport
- New WebSocket or equivalent session APIs for browser terminals and host-aware terminal routing
- Removal or de-prioritization of Bubble Tea/TUI-specific assumptions is acceptable because there are no users or compatibility constraints to preserve
