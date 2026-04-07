## Context

Fascinate already has most of the backend primitives needed for a serious CLI: machines, snapshots, env vars, diagnostics, host-aware routing, and a VM-backed shell path. The missing piece is that the current shell implementation is still browser-centric. Terminal sessions live in process, the web workspace owns shell inventory locally, and the API/auth model is centered on browser cookies rather than durable automation-friendly credentials.

The desired outcome is stronger than “add some subcommands.” The CLI must be able to act as a full product surface for humans and AI agents, and it must stay synchronized with the web app. That means Fascinate needs a shared control-plane contract for machines, snapshots, env vars, diagnostics, shell lifecycle, and command execution. It also needs real-time metadata synchronization and a shell model that survives client disconnects and control-plane restarts better than the current browser-only session manager.

There are no existing users and no compatibility constraints that justify preserving the current browser-only shell/session contract. The design should therefore optimize for a shared multi-surface architecture, not for temporary shims.

## Goals / Non-Goals

**Goals:**
- Make the `fascinate` CLI a first-class way to use Fascinate end-to-end, not an admin/debug sidecar.
- Use one shared backend contract for CLI and web so machine, snapshot, env-var, shell, and exec state remain authoritative and synchronized.
- Replace browser-only shell session ownership with durable backend-owned shell resources that can be attached from multiple surfaces.
- Provide AI-agent-friendly command execution with structured output, stable exit semantics, and non-interactive auth.
- Make the CLI easy to install from a stable public curl-install flow with versioned release artifacts and checksum verification.
- Improve reliability over the current browser shell model, especially around restart recovery, shell discovery, and event visibility.
- Add thorough automated coverage for CLI/API/web interoperability and shell/exec failure handling.

**Non-Goals:**
- Preserving the current browser-only terminal session endpoints, workspace data model, or auth assumptions.
- Supporting direct local-runtime CLI shortcuts that bypass the control plane when pointed at a Fascinate deployment.
- Storing unbounded terminal byte streams centrally in SQLite.
- Cross-host live migration of active shell attachments in the first implementation.
- Building every possible CLI ergonomic extra in v1; the first release focuses on correctness, machine readability, and core operator UX.

## Decisions

### 1. Use one product API for both web and CLI, and keep `fascinate` as both server and client binary

The user CLI will be implemented as a network client against the same control-plane API the web app uses. The existing `fascinate` binary will continue to contain server/admin commands, but it will gain a first-class user command tree for login, machine, snapshot, env-var, diagnostics, shell, and exec operations.

The current unconditional host-env bootstrap in `cmd/fascinate/main.go` will be split by mode:
- server/admin commands continue to load host configuration from env files
- product CLI commands load CLI config such as API base URL, token, and output preferences

Why:
- One backend contract is the only clean way to keep CLI and web synchronized.
- One binary avoids version skew between a separate server artifact and a separate user CLI artifact.
- A networked CLI exercises the real product surface and is usable from workstations and agents, not only from the control-plane host.

Alternatives considered:
- Build a separate `fascinate-cli` binary.
  - Rejected because it adds packaging and versioning complexity without changing the backend contract problem.
- Let the CLI talk directly to SQLite or the runtime when local.
  - Rejected because it creates a second control plane and breaks sync with the web app.

### 2. Add dedicated API-token authentication for the CLI and agents

The CLI will authenticate with hashed bearer tokens stored in the control-plane database, separate from browser session cookies. Interactive login will reuse the email-code verification flow to authenticate a user and mint a named API token. Non-interactive use will accept `FASCINATE_TOKEN` and equivalent config-file storage.

The API contract will distinguish:
- browser sessions presented by cookie
- CLI/API tokens presented by `Authorization: Bearer <token>`

Why:
- Cookie sessions are the wrong primitive for agents and workstation automation.
- Hashed durable API tokens fit the current database-backed session model and remain easy to revoke and inspect.
- Reusing the email-code identity flow avoids inventing a second user-identity system.

Alternatives considered:
- Reuse browser cookies for CLI.
  - Rejected because it is brittle for non-interactive clients and awkward for agents.
- Move immediately to a full OAuth/device-flow stack.
  - Rejected because the identity problem is simpler than that today and email-code login already exists.

### 3. Replace browser-only terminal sessions with durable shell resources plus ephemeral attachments

Fascinate will treat a shell as a durable user-owned resource backed by a host-local tmux session inside the VM. A shell resource will persist metadata such as owner, machine, host, lifecycle state, title, and last activity in the control-plane database. Interactive connections become ephemeral attachments to that shell rather than the shell itself.

Core shell semantics:
- `POST /v1/shells` creates a shell resource and the backing tmux session
- `GET /v1/shells` / `GET /v1/shells/{id}` list and inspect shells
- `DELETE /v1/shells/{id}` destroys the tmux session and disconnects attachments
- `POST /v1/shells/{id}/attach` creates a short-lived attach token for interactive streaming
- `POST /v1/shells/{id}/input` and line-inspection endpoints support non-interactive shell control

Why:
- The CLI and web cannot share shell state if the shell only exists as an in-memory browser session.
- tmux is already the right in-guest durability primitive; the missing piece is durable control-plane metadata around it.
- Separating shell resources from attachments lets shells survive client disconnects and enables multiple simultaneous viewers/writers.

Alternatives considered:
- Keep the current in-memory browser terminal manager and add CLI-specific wrappers.
  - Rejected because it preserves the exact sync and restart problems the CLI is meant to solve.
- Persist raw terminal streams in SQLite.
  - Rejected because it adds storage and replay complexity while tmux already owns shell history.

### 4. Use SSE for metadata synchronization and WebSocket for shell I/O

Metadata updates such as machine state, snapshot state, shell lifecycle, attachment count, and exec status will be published through a control-plane event stream. REST endpoints will hydrate initial state; both the web app and CLI will subscribe to a server-sent event stream for incremental updates. Interactive shell traffic remains on a dedicated per-attachment WebSocket.

The control plane will use the existing events table as the durable event log and add an in-process fan-out hub for low-latency subscribers. Host-local shell gateways will report lifecycle changes back through the control plane so all surfaces observe one authoritative event stream.

Why:
- Polling is insufficient for “CLI creates shell, web sees it immediately.”
- SSE is a simpler and more reliable fit for metadata fan-out than a second bidirectional socket protocol.
- Keeping terminal bytes on per-shell WebSockets avoids coupling metadata subscribers to high-volume terminal streams.

Alternatives considered:
- Keep 5-second polling for lists.
  - Rejected because it breaks the immediate sync goal and creates stale UI/CLI state.
- Multiplex metadata and terminal bytes over one shared WebSocket.
  - Rejected because it couples unrelated traffic classes and complicates client logic.

### 5. Keep shell history in tmux and derive line inspection from tmux primitives

Fascinate will not attempt to make the control plane the owner of terminal scrollback. Recent shell lines will be served by host-side `tmux capture-pane`-style inspection, and live follow mode will compose:
- a recent-history snapshot from tmux
- an optional read-only live attachment for subsequent bytes

Shell command injection for CLI actions such as `shell send` will use tmux-aware input primitives rather than screen scraping.

Why:
- tmux already provides bounded history and durable shell state inside the VM.
- This avoids building a fragile centralized terminal log system.
- It gives the CLI direct “show me recent lines” behavior without requiring an interactive TTY.

Alternatives considered:
- Store recent terminal output in database tables.
  - Rejected because it duplicates tmux state and invites unbounded storage.
- Make CLI line inspection depend on attaching an interactive PTY every time.
  - Rejected because it is awkward for automation and agents.

### 6. Add a separate machine-scoped exec primitive optimized for agents

Fascinate will add a non-PTY command execution path distinct from shared interactive shells. Exec requests will resolve the owning host, run as the normal guest user with the managed Fascinate environment loaded, and return structured status:
- exit code
- timeout/cancel state
- stdout
- stderr
- cwd
- timestamps

The CLI will expose this as `fascinate exec`, with human output by default and `--json` / `--jsonl` for machine use.

Why:
- AI agents need deterministic command execution more often than they need an interactive terminal.
- Reusing interactive shells for non-interactive jobs produces unreliable parsing, prompt ambiguity, and weak exit handling.
- A dedicated exec path makes stdout/stderr separation and timeout semantics explicit.

The exec contract will also support optional caller-provided stdin so agents can pipe multiline scripts or generated command bodies directly into the remote command without relying on shell-escaped heredocs.

Alternatives considered:
- Force agents to use only interactive shells plus `shell send`.
  - Rejected because it is brittle and hard to automate correctly.
- Implement exec by creating hidden tmux shells.
  - Rejected because it blurs two different product primitives and makes exit semantics harder to reason about.

### 10. Add archive-based upload and download for file movement

Fascinate will add machine-scoped file transfer endpoints and CLI commands that move files or directories as tar archives over the same authenticated control-plane contract used by exec and shells.

Core transfer semantics:
- `fascinate upload <local-path> <machine>:<remote-path>` streams a local file or directory archive into a running machine
- `fascinate download <machine>:<remote-path> <local-path>` streams a file or directory archive back to the local workstation or agent
- transfer traffic uses the control plane for authentication and host routing, not direct workstation SSH

Why:
- Agents and humans need a first-class way to move scripts, projects, and outputs without converting file contents into shell-escaped heredocs.
- Archive transfer supports both files and directories while staying simple to route through the existing host-local SSH gateway.
- Reusing the control plane preserves the multi-surface model and keeps direct machine access optional rather than required.

### 7. Refactor the web workspace to reference shell IDs instead of owning shell existence

The web app will stop treating workspace windows as the source of truth for live shells. Instead:
- the shell list comes from the shared shell API plus event stream
- workspace layout persists presentation metadata such as order and window geometry, keyed by shell ID
- deleting a shell removes it from the shared shell list and therefore from the workspace

This is an intentional breaking refactor of the current browser-local shell model.

Why:
- The web app cannot reflect CLI-created shells if it owns shell existence locally.
- Separating shell existence from layout mirrors the split between durable control-plane resources and presentation state.
- The workspace becomes simpler to reason about once it renders shared shell resources instead of inventing them.

Alternatives considered:
- Preserve the current local window model and periodically reconcile it with backend shells.
  - Rejected because it keeps the wrong ownership model and creates harder edge cases.

### 8. Treat interoperability and failure handling as first-class test targets

The implementation will include package and integration coverage for:
- CLI auth/token flows
- REST and event-stream authorization
- shell create/list/attach/send/tail/delete behavior
- multi-attachment shared-shell behavior
- control-plane restart and reattach behavior
- exec success, non-zero exit, timeout, and cancellation
- CLI/web synchronization for shell and machine state

This change will also add end-to-end coverage that exercises the real CLI against the test server and validates the web app against event-driven shell state.

Why:
- The core risk of this change is coordination failure between surfaces, not only single-package correctness.
- Shell and exec features degrade badly when disconnect, retry, and restart paths are not tested.

Alternatives considered:
- Rely mainly on existing package tests and manual CLI checks.
  - Rejected because the change crosses too many boundaries for that to be reliable.

### 9. Add a separate CLI artifact and public install script instead of reusing the host installer

Fascinate will ship the user CLI through a dedicated release channel:
- a CLI-only artifact builder that produces platform-specific archives containing just the `fascinate` client binary and release manifest
- a published release index/manifest that maps version, channel, OS, and architecture to artifact URLs and checksums
- a stable public install script such as `https://fascinate.dev/install.sh`

The install script will:
- detect supported OS and architecture
- resolve the requested version or the latest stable release
- download the matching CLI artifact and checksum metadata
- verify the artifact checksum before install
- install to a user-writable directory by default, with explicit override for system-wide install
- print PATH guidance and avoid surprising root/service side effects

Why:
- The existing host installer is designed for unpacked full release artifacts, requires root, installs systemd and web assets, and is not appropriate for end-user CLI installation.
- A curl-install flow is a large UX improvement and matches the expectation for a modern agent-oriented CLI.
- Public versioned artifacts with checksums keep the curl bootstrap small and auditable.

Alternatives considered:
- Reuse `ops/host/install-control-plane.sh` for CLI installs.
  - Rejected because it is a host deploy installer, not a user CLI installer.
- Ask users to download binaries manually from releases.
  - Rejected because it adds friction and hurts agent/human onboarding.

## Risks / Trade-offs

- **[More durable control-plane state for shells and API tokens]** -> Keep shell metadata compact, keep terminal history in tmux, and use additive migrations with bounded indexes.
- **[SSE plus WebSocket means two realtime transports]** -> Keep responsibilities separate: SSE for metadata, WebSocket for shell bytes.
- **[tmux becomes a stricter product dependency]** -> Treat tmux availability and version compatibility as part of the shell contract and cover it in tests and diagnostics.
- **[A breaking web-workspace refactor could temporarily destabilize the UI]** -> Land the shared shell model with targeted web tests and migrate the workspace in one coherent cutover rather than a hybrid ownership model.
- **[Exec semantics may diverge from interactive shell behavior]** -> Ensure exec uses the same guest user and managed env bootstrap path as normal shells, and document the remaining non-PTY differences.
- **[Single-binary client/server mode split increases startup branching]** -> Keep configuration loading explicit per mode and test the binary entrypoint behavior directly.
- **[Curl installers can become opaque or unsafe]** -> Keep the installer minimal, publish checksums/manifests, support version pinning, and make the script download a signed or checksum-verified artifact rather than embedding install logic in the binary payload.

## Migration Plan

1. Add additive database support for CLI tokens, durable shells, and any shell metadata needed for synchronization.
2. Add bearer-token auth, shell CRUD/attach/input/lines APIs, exec APIs, and the SSE event stream on the control plane.
3. Refactor the host-local terminal manager into a surface-neutral shell manager that owns tmux-backed shells and shared attachments.
4. Refactor the web app to consume shared shell resources and event-driven state rather than browser-local shell ownership and polling.
5. Add the first-class CLI command tree and output helpers on top of the shared product API.
6. Add the CLI artifact build path, public release manifest, and curl-install script for supported platforms.
7. Expand automated coverage across Go packages, web tests, install-script checks, and end-to-end CLI/web synchronization paths.
8. Update README and active OpenSpec docs to describe the CLI-first, multi-surface architecture and install flow.

Rollback:
- Database changes should be additive so the deployment can roll back to the previous binary if needed.
- If a deployment must be reverted after shell resources are created, guest tmux sessions remain inside the VM; operators can recover them manually even if the new control-plane metadata is rolled back.
- Because there are no users, rollback priority is restoring service correctness rather than preserving transitional compatibility.

## Open Questions

- Whether shell completion generation ships in the first implementation or follows once the command surface stabilizes.
- What default TTL and rotation policy best balance convenience and safety for API tokens in agent-heavy workflows.
- What output size caps and truncation rules should apply to long-running exec jobs before a follow-up spool-to-artifact design is needed.
