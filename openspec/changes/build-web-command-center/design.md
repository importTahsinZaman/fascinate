## Context

Fascinate has reached the point where the runtime substrate is materially stronger than the product surface. The platform can now create persistent VMs, save full snapshots, restore and clone live machines, persist supported tool auth, and inject machine-scoped env vars. But users still interact through an SSH-first frontdoor and a Bubble Tea dashboard that were intentionally used to postpone frontend work. That tradeoff no longer makes sense for the actual product direction.

The desired product is a browser command center for running many coding-agent sessions on VMs at once. That means the main interaction surface must support:
- creating and managing machines, snapshots, and env vars without leaving the web app
- opening multiple live shells at once, including multiple shells into the same VM
- arranging those shells in a rigid left-to-right workspace
- keeping input latency low enough that the browser terminal feels immediate rather than tunneled through a slow admin path

There are no live users and no compatibility promises to preserve. That removes the main reason to keep terminal-first assumptions alive. The design should optimize for the final browser-first architecture, even when that means de-prioritizing or replacing the TUI and SSH-specific UX.

## Goals / Non-Goals

**Goals:**
- Make the web app at `fascinate.dev` the primary Fascinate product surface.
- Use a stack that is performant for a windowed multi-terminal workspace and does not fight low-latency terminal streaming.
- Give the web app first-class workflows for machines, snapshots, clone/create-from-snapshot, and env vars.
- Support multiple browser terminal sessions per VM in an ordered horizontal strip that users can reorder.
- Keep terminal keystroke-to-echo latency off the normal control-plane path as much as possible.
- Build directly on the existing host model so the same design scales from the current single host to later VM worker hosts.
- Introduce observability for browser terminal attach time, session failures, and latency.

**Non-Goals:**
- Final visual design, branding, typography, or interaction polish beyond what is needed to establish the architecture.
- Perfect terminal session survival across full control-plane restarts in v1.
- Collaborative multi-user workspaces, shared terminals, or multiplayer cursors.
- A compatibility bridge that preserves the Bubble Tea dashboard or keeps terminal/CLI workflows first-class.
- CDN edge execution, region-aware browser routing, or cross-host session migration in the first delivery.

## Decisions

### 1. Build a React/Vite browser app served by the Go control plane

The web app will live in a dedicated `web/` package using:
- `React`
- `TypeScript`
- `Vite`
- `pnpm`

The built assets will be compiled as part of deploy/build and served from the Go control plane on the primary app origin. The control plane remains the authority for REST APIs, browser auth, and terminal-session authorization.

Why:
- A Vite SPA is a better fit than an SSR-heavy framework for a long-lived authenticated command center with terminal streams.
- Same-origin serving avoids unnecessary auth, CORS, and deployment complexity for browser sessions and API calls.
- Keeping the web bundle in the same deploy artifact as the control plane reduces version skew between frontend and backend while the product is still moving quickly.

Alternatives considered:
- Next.js or another SSR framework.
  - Rejected because SSR is not the core value here, and introducing a second application runtime would complicate deployment before it buys much product value.
- Separate hosted frontend on another platform.
  - Rejected because it complicates session auth and browser terminal handoff too early.

### 1.5 Keep one central control-plane database and extend it for browser state

Fascinate will continue using the existing central control-plane database rather than introducing a second product database for the web app. The browser rollout will add new control-plane tables for browser-specific state, including:
- `web_sessions` for browser-authenticated sessions
- `workspace_layouts` for persisted browser workspace state

Optional future tables may exist for control-plane-visible browser terminal metadata, but live PTY state remains host-local rather than becoming a central database concern.

Why:
- Users, machines, snapshots, env vars, hosts, and browser auth all belong to the same control-plane authority.
- A second database would add operational complexity without solving a real product problem at this stage.
- Keeping all durable product state in one place fits the later architecture where the control plane moves to its own smaller machine and VM hosts remain compute workers.

Alternatives considered:
- Introduce a separate frontend or auth database.
  - Rejected because it complicates deployment and data ownership too early.

### 2. Model the main surface as a horizontally scrollable DOM workspace

The main workspace will be implemented as a horizontally scrollable DOM strip with fixed shell windows arranged left-to-right. Each terminal window will host its own `xterm.js` instance. Users can drag shell headers to reorder shells within the strip, but the workspace does not support freeform dragging, freeform resizing, or zooming.

Why:
- Browser terminals need focus, text selection, IME handling, copy/paste, accessibility semantics, and independent rendering lifecycles.
- Terminal copy shortcuts should resolve from xterm selection state at the document level, rather than depending on the hidden xterm textarea to keep keyboard focus after a mouse selection.
- `xterm.js` is designed to own a DOM container; avoiding a scaled/transformed canvas removes an entire class of terminal hit-testing and selection bugs.
- A rigid strip matches the product need better than a whiteboard metaphor because users primarily switch contexts, reorder shells, and scan left-to-right.

Alternatives considered:
- A real HTML canvas or whiteboard-style renderer.
  - Rejected because it is the wrong substrate for multiple interactive terminals.
- A freeform DOM canvas with draggable/resizable shells.
  - Rejected because it adds zoom, transform, and placement complexity without enough product value for a shell-first command center.

### 3. Keep terminal bytes out of React state and out of the normal control-plane request path

The frontend will use:
- `Zustand` for workspace/window state
- `TanStack Query` for REST-backed product state
- `xterm.js` with `@xterm/addon-fit` and `@xterm/addon-webgl` for terminal rendering

Terminal output will flow directly into `xterm.write()` and will never be mirrored into React component state. Layout state updates will be local and debounced before persistence. Hidden/minimized terminals will stay connected but reduce rendering work.

Why:
- The fastest way to destroy terminal responsiveness is to treat terminal output as ordinary React data.
- Workspace layout and terminal byte streams have different performance shapes and should be isolated in different state layers.
- `xterm.js` already solves the hard browser-terminal rendering problem; the surrounding app should stay out of its hot path.

Alternatives considered:
- Store terminal buffers in React state.
  - Rejected because it guarantees unnecessary rerenders and memory churn.
- Build a custom terminal renderer from scratch.
  - Rejected because it adds huge complexity without product upside.

### 4. Use one binary WebSocket per terminal session, with host-local terminal gateways

Browser terminals will attach through a dedicated terminal-session API:
1. browser asks the control plane to create or resume a terminal session for a machine
2. control plane resolves the machine’s owning host
3. control plane mints a short-lived signed attach token for that host and session
4. browser opens a dedicated WebSocket to the owning host’s terminal gateway

The session stream will use:
- binary frames for terminal input/output bytes
- small control messages for resize, exit, and heartbeats

Each visible terminal session gets its own WebSocket rather than sharing one multiplexed socket.

Why:
- One socket per terminal avoids backpressure coupling between independent shells.
- Binary frames avoid per-keystroke JSON overhead.
- Host-local gateways keep the central control plane out of the per-byte data path, which is necessary for low-latency typing as multi-host deployment arrives.

Alternatives considered:
- Route all terminal traffic through the central control plane.
  - Rejected because it becomes the latency bottleneck and scales poorly with many active shells.
- Multiplex all terminal sessions over one WebSocket.
  - Rejected because one noisy terminal can degrade the others and complicate flow control.

### 5. Reuse the host model: every host runs a terminal gateway alongside VM runtime duties

The current combined host will run:
- the central control plane
- the local VM runtime
- the browser terminal gateway

Later VM worker hosts will also run the terminal gateway for the machines they own. The control plane remains responsible for authorization and session issuance; the host remains responsible for PTY creation, guest-shell attachment, resize handling, and stream transport.

Why:
- This lines up with the host-aware architecture already introduced.
- It keeps the latency-sensitive path on the box that already owns the VM network namespace and guest reachability.
- It preserves a clean future split: central control plane plus many VM/terminal worker hosts.

Alternatives considered:
- Make the control plane the only terminal gateway forever.
  - Rejected because it fights the multi-host direction and worsens latency.

### 6. Introduce browser auth and browser-native sessions as first-class product primitives

The web app will not depend on SSH-key semantics. Fascinate will add browser auth based on emailed verification codes and HttpOnly web sessions. The control plane will issue normal browser sessions for REST access, plus short-lived terminal attach tokens for host-local WebSocket streams.

Browser login will:
- accept an email address
- send a one-time verification code using the existing email-code mechanism
- create the user record if it does not already exist
- create a browser session without requiring SSH-key registration

Browser sessions will use opaque random tokens whose hashes are stored in the central DB. The browser cookie carries the raw token; the database stores only the hash plus expiry and last-seen metadata.

Why:
- Browser UX needs a direct login/session model.
- Reusing only SSH-key-driven flows would make the web app feel like an adapter over the wrong identity surface.
- Short-lived terminal attach tokens decouple host-local stream auth from long-lived browser cookies.
- Opaque DB-backed sessions are easier to revoke, inspect, and migrate across hosts than self-contained stateless tokens.

Alternatives considered:
- Keep SSH as the primary identity mechanism and proxy browser shells through it.
  - Rejected because it preserves terminal-first product assumptions and complicates the browser flow.
- Use stateless JWTs as the main browser session mechanism.
  - Rejected because revocation and session inspection are more awkward, and the control plane already has a durable DB suited for session storage.

### 7. Persist workspace layout separately from ephemeral terminal sessions

The control plane will store browser workspace metadata per user, including:
- workspace ID/name
- the left-to-right shell order and the window-to-machine bindings
- which machine a terminal window is bound to
- whether the window should reopen a new shell or try to reattach to a known session

Live terminal sessions themselves remain ephemeral and host-local. Session persistence across browser refreshes will be best-effort in v1, but the layout is durable.

Why:
- Layout persistence is core product value; users want their command center to reopen in the same shape.
- Terminal sessions are much more operationally sensitive and belong on the host-local side.
- Separating the two avoids trying to serialize live PTY state into the control plane DB.

Alternatives considered:
- Persist all terminal session state centrally.
  - Rejected because PTY streams are not a good match for durable central storage and would complicate restart semantics unnecessarily.

### 8. Treat machines, snapshots, and env vars as first-class web workflows, not side panels over raw APIs

The web app will ship with browser surfaces for:
- machine list/create/delete/clone
- snapshot list/create/delete/create-from-snapshot
- env-var list/create/edit/delete and effective machine-env inspection
- terminal launch from machine detail or directly into the workspace

These are not auxiliary pages around the shell strip; they are part of the main product surface and should share auth, data fetching, and optimistic UX patterns with the workspace.

The default control surface should prioritize active work before inventory. That means the sidebar should present open shells as its primary list, using the persisted shell-strip order and surfacing cwd plus machine name for fast context switching. Machine rows belong in a separate lower inventory block that keeps machine actions and shell launch available without burying currently open shells inside per-machine cards.

Why:
- The product goal is a command center, not just a browser terminal.
- Snapshot and env-var management materially affect how users prepare and reuse agent workspaces.
- Keeping these flows in the web app lets Fascinate stop depending on SSH/TUI management paths.

Alternatives considered:
- Only build a browser terminal first and leave machine/snapshot/env-var management to API or SSH.
  - Rejected because it preserves the split-brain UX the user wants to leave behind.

### 9. Add explicit browser-terminal observability and performance budgets

Fascinate will instrument:
- browser terminal attach time
- session creation failures
- WebSocket disconnect reasons
- keypress-to-echo RTT samples
- host-side queue/backpressure state
- count of active browser terminal sessions per host

The design target for same-region usage is:
- terminal attach visibly started in under 1 second
- median keypress-to-echo RTT low enough to feel local
- no full-workspace rerender on terminal output or shell reorder updates

Exact thresholds can tighten later, but the system must expose enough data to measure them from day one.

Why:
- Low latency is a product requirement here, not an implementation detail.
- Without instrumentation, the app will drift into “works on localhost” performance and regress silently.

Alternatives considered:
- Add observability after the UI exists.
  - Rejected because terminal latency problems are hard to reason about retroactively.

## Risks / Trade-offs

- **[Frontend build complexity in a previously Go-only repo]** -> Add a dedicated `web/` package and treat it as a bounded exception rather than spreading JS tooling across the repo.
- **[Two session models: browser auth and terminal attach tokens]** -> Keep browser sessions long-lived and simple; make terminal tokens short-lived and purpose-scoped.
- **[Workspace UX complexity]** -> Ship one high-value workspace model first: an ordered horizontal shell strip with persisted layout. Defer collaboration and advanced orchestration.
- **[Host-direct terminal streams complicate deployment]** -> Reuse the existing host model and Caddy routing so the current single host remains the first terminal gateway implementation.
- **[Session loss on restart in v1]** -> Persist layout durably and make session reattach best-effort; do not block the first browser release on full restart-proof session recovery.
- **[Temptation to preserve the TUI path]** -> Treat browser-first flows as authoritative and avoid designing APIs around the Bubble Tea dashboard.

## Migration Plan

1. Add the `web/` frontend package, browser build pipeline, and static asset serving from the Go control plane.
2. Add browser-auth tables and browser auth, reusing the current email-code flow but splitting browser login from SSH-key onboarding.
3. Add session cookies, opaque DB-backed web sessions, and the initial browser shell of the app.
4. Add REST-backed browser views for machines, snapshots, and env vars on top of the existing control plane.
5. Add the host-aware browser terminal session API plus the local-host terminal gateway.
6. Add the workspace shell-strip manager and `xterm.js`-based multi-terminal workspace.
7. Add workspace persistence, browser-terminal diagnostics, and latency instrumentation.
8. Remove Bubble Tea/TUI assumptions from primary product docs and stop treating terminal-first flows as the main UX.

Rollback:
- Revert the deploy to the previous control-plane build and disable the web app routes if the browser rollout proves unstable.
- Because there are no live users, rollback does not need compatibility shims for older saved layouts or sessions.

## Open Questions

- Should v1 ship with one default workspace per user, or named multiple workspaces from the start?
- What exact host URL shape should browser terminals use later in multi-host mode: host-specific subdomains or an edge-routed shared domain?
- Do we want browser-terminal reconnection to automatically reattach to the same host-local session when possible, or create a fresh shell by default?
- Should the initial management views be integrated into the workspace shell itself, or should the app start with a more conventional left-nav plus workspace route structure?
