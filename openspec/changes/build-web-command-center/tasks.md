## 1. Browser App Foundation

- [x] 1.1 Create a dedicated `web/` frontend package with React, TypeScript, Vite, and the initial build/test tooling.
- [x] 1.2 Wire the control plane to serve the built web app on the primary Fascinate origin and route browser app requests to the SPA entrypoint.
- [x] 1.3 Add control-plane schema and store support for browser state, including `web_sessions` and `workspace_layouts`, in the existing central database.
- [x] 1.4 Add first-class browser authentication based on emailed verification codes, including user creation without SSH-key registration.
- [x] 1.5 Add opaque DB-backed browser session handling with protected browser routes and session cookies.

## 2. Browser Management Surfaces

- [x] 2.1 Build browser machine-management views for listing, creating, inspecting, cloning, and deleting machines.
- [x] 2.2 Build browser snapshot-management views for listing, creating, deleting, and creating a machine from a saved snapshot.
- [x] 2.3 Build browser env-var views for listing, creating, editing, deleting, and inspecting effective machine env values.

## 3. Terminal Session Backend

- [x] 3.1 Add browser terminal-session APIs that authorize a session request, resolve the owning host, and issue attach details for that host.
- [x] 3.2 Implement the local-host browser terminal gateway that creates PTYs, attaches to guest shells, handles resize, and streams terminal bytes.
- [x] 3.3 Add host-aware browser terminal diagnostics and metrics for session creation, attach failure, disconnects, and active-session counts.

## 4. Canvas Workspace Frontend

- [x] 4.1 Build the main workspace route as a draggable and resizable DOM-based canvas for terminal windows.
- [x] 4.2 Integrate `xterm.js` terminals into workspace windows so users can open multiple shells, including multiple shells for one machine.
- [x] 4.3 Persist workspace layout state separately from live sessions and restore the saved layout when the user returns to the app.

## 5. Latency and Interaction Hardening

- [x] 5.1 Switch browser terminal transport to dedicated per-session binary WebSockets instead of generic request/response flows.
- [x] 5.2 Ensure terminal streaming stays off the React render path and that busy terminals do not block other active terminals.
- [x] 5.3 Add client and server instrumentation for terminal attach time, keypress-to-echo samples, and workspace responsiveness.

## 6. Validation, Docs, and Product Cutover

- [x] 6.1 Add automated coverage for browser auth, machine/snapshot/env-var browser flows, terminal session issuance, and workspace persistence.
- [x] 6.2 Run live-host validation for the browser command center, including VM create, snapshot create/restore, env-var management, and multiple simultaneous terminals.
- [x] 6.3 Update README, AGENTS.md, and product/operator docs to describe the browser-first command center and de-emphasize the old terminal-first UX.
