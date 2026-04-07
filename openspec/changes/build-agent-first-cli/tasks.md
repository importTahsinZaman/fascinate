## 1. CLI and Auth Foundation

- [x] 1.1 Refactor the `fascinate` binary entrypoint so server/admin commands load host config while user CLI commands load CLI config without requiring `/etc/fascinate/fascinate.env`.
- [x] 1.2 Add CLI config discovery, API base URL resolution, token storage, `FASCINATE_TOKEN` override support, and non-interactive auth/prompt guardrails.
- [x] 1.3 Add database migrations, store methods, and service logic for durable CLI/API bearer tokens with hashed storage and revocation metadata.
- [x] 1.4 Add HTTP auth support for bearer tokens plus CLI login/token endpoints that reuse the email-code identity flow.

## 2. Shared Shell Resource Model

- [x] 2.1 Add database schema, store methods, and control-plane models for durable shell resources and shell lifecycle metadata.
- [x] 2.2 Refactor the current browser-only terminal manager into a surface-neutral shell manager backed by persistent tmux shell resources.
- [x] 2.3 Add host-aware shell create/list/get/delete APIs plus attach-token issuance for interactive shared shell attachments.
- [x] 2.4 Add non-interactive shell input and recent-line inspection APIs using tmux-aware host primitives instead of browser-local session state.
- [x] 2.5 Add shell restart-recovery behavior so durable shell records can be rediscovered and reattached after control-plane restart.

## 3. Realtime Sync and Diagnostics

- [x] 3.1 Add a control-plane SSE event stream that replays durable resource events and fans out live machine, snapshot, shell, and exec updates.
- [x] 3.2 Publish shell lifecycle, attachment, and deletion events through the control plane and wire host-side updates back into the shared event stream.
- [x] 3.3 Extend platform diagnostics and owner events to expose shared shell state, attachment failures, event delivery state, and exec outcomes.

## 4. Agent-Optimized Command Execution

- [x] 4.1 Implement host-aware non-PTY exec service and API flows that run commands inside a machine without requiring a preexisting shell.
- [x] 4.2 Add structured exec streaming and final-result framing with explicit stdout, stderr, exit status, timeout, and cancellation outcomes.
- [x] 4.3 Map exec transport, routing, timeout, cancellation, and command-exit failures into stable CLI-visible status and exit behavior.

## 5. Web App Refactor

- [x] 5.1 Replace the web app's local window-owned shell model with shared shell resources keyed by durable shell IDs.
- [x] 5.2 Switch shell and other fast-changing product state from polling-only refreshes to REST hydration plus SSE-driven synchronization where required.
- [x] 5.3 Update web terminal attach, delete, and workspace flows to use the new shared shell APIs and deletion semantics.
- [x] 5.4 Migrate workspace persistence and shell sidebar behavior so layout remains presentation state while shared shell existence comes from the backend.

## 6. CLI Product Surface

- [x] 6.1 Implement machine, snapshot, env-var, and diagnostics CLI commands on top of the shared product API with human-readable and `--json` outputs.
- [x] 6.2 Implement `fascinate shell` subcommands for create, list, attach, send, lines, and delete on top of the durable shared shell model.
- [x] 6.3 Implement `fascinate exec` with structured output modes, timeout control, cancellation handling, and agent-safe stdout/stderr discipline.
- [x] 6.4 Add help, usage, confirmation, and error-shaping behavior that keeps interactive UX strong without breaking non-interactive automation.

## 7. CLI Distribution

- [x] 7.1 Add a CLI-only artifact builder and manifest generation flow for supported OS/architecture targets using the existing release tooling patterns.
- [x] 7.2 Add a public release index/manifest format that lets the installer resolve latest and pinned CLI versions with checksums.
- [x] 7.3 Add a stable `install.sh` bootstrap script that downloads, verifies, and installs the CLI to a user-scoped directory by default.
- [x] 7.4 Add install-path, checksum-failure, and version-resolution tests for the CLI distribution flow.

## 8. Verification and Documentation

- [x] 8.1 Add Go unit and integration tests for bearer-token auth, shell CRUD, shared attachments, SSE synchronization, host routing, and exec behavior.
- [x] 8.2 Add web tests covering shared shell discovery, live sync with backend events, workspace migration, and shell deletion/update behavior.
- [x] 8.3 Add end-to-end interoperability coverage proving CLI-created shells and machine changes appear in the web app immediately and survive restart/reconnect cases.
- [x] 8.4 Update `README.md` and relevant active OpenSpec docs to describe the CLI-first multi-surface architecture, supported command workflows, and curl-install path.
