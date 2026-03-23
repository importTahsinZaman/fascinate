## Context

Fascinate's browser workspace already gives each terminal window a persistent session ID, a window header, and client-side current-working-directory metadata derived from the terminal stream. That is enough to anchor a shell-scoped git inspection feature without changing the VM/runtime model or inventing a second session primitive.

The current browser terminal surface stops at raw shell access. Users can keep many shells open, but they cannot inspect a working tree in a review-friendly way without dropping into manual `git status` / `git diff` commands or leaving the product for another tool. The requested UX is a dark, elevated, unified diff presentation that opens from the shell header as an overlay above the control sidebar and does not push or resize the canvas. The review surface should feel like a purpose-built hosted diff tool, with compact stacked cards, sticky split file headers, and restrained blue/red/green banding rather than generic terminal-themed panels.

Relevant constraints:
- The diff surface must stay browser-first and must not reintroduce terminal-first product assumptions.
- Terminal byte streaming must remain outside React state and outside the normal REST polling path.
- The diff sidebar should use the shell's latest known cwd, which is already tracked on the client, instead of depending on prompt hooks or server-side cwd persistence.
- No schema or persisted workspace-layout change is desirable for a shell-local review surface.
- Adding a new frontend dependency for diff rendering should be avoided unless strictly necessary.

## Goals / Non-Goals

**Goals:**
- Add a shell-header git diff action that opens a fixed overlay above the control sidebar without moving canvas windows.
- Resolve repository context from the selected shell's latest known cwd, including shells opened in nested directories inside a repo.
- Fetch git status and file diffs through explicit shell-scoped backend APIs that run outside the interactive PTY path.
- Keep the sidebar reasonably current while open through bounded polling rather than prompt hooks or long-lived file watchers.
- Render changed files in a review-style unified diff UI with a repo-summary header, branch-chip and animated panel chrome, inline copy-path affordances with visible acknowledgment, sticky file headers, change counts, gutters, full-width expandable collapsed context, quiet static collapsed-link chrome, and a continuous stacked file stream.
- Keep diff state ephemeral to the browser session so workspace layout persistence remains unchanged.

**Non-Goals:**
- Inline code review comments, "mark as viewed", file actions, or PR workflow features.
- Git write operations such as stage, unstage, checkout, commit, or discard.
- Persisting diff sidebar state in workspace layouts or the database.
- Supporting non-git version control systems.
- Building a generalized repository browser outside the context of an active shell window.

## Decisions

### 1. Use a single shell-scoped overlay panel that sits above the existing layout

The git diff UI will be implemented as a dedicated overlay rendered at the command-center level rather than inside the canvas or the terminal body. The panel is opened from a button in the shell window header, sits above the standard control sidebar, and is bound to one selected shell window at a time.

The overlay will:
- sit above the workspace/control-sidebar layout with its own z-layer
- avoid resizing, docking, or reflowing terminal windows
- switch context when the user opens diffs from another shell
- keep its open/scroll state ephemeral and local to the current browser session

Why:
- The user explicitly wants a panel that is "on top" and does not push anything.
- Embedding the diff inside the shell body would steal terminal space and make comparison harder.
- Reusing the existing right control sidebar would conflate machine-management UI with shell-local git context.

Alternatives considered:
- Dock the diff in the existing layout.
  - Rejected because it would change workspace geometry and violate the requested overlay behavior.
- Render the diff inside each terminal window.
  - Rejected because it reduces terminal usefulness and couples review UI to window size constraints.

### 2. Use the browser-tracked cwd plus session-authenticated backend endpoints

The frontend already knows the latest cwd for each shell window from the terminal stream. Diff requests will send that cwd together with the selected shell's session ID to new shell-scoped HTTP endpoints.

The backend will extend the terminal manager boundary with read-only git inspection methods, for example:
- repo/status lookup for a session + cwd
- file diff lookup for a session + cwd + file path

Authorization remains session-based:
- the browser must already own the shell session
- the terminal manager resolves the machine/user from that session
- git commands only run against that machine and cwd context

Why:
- The cwd already exists in the browser and updates quickly with prompt changes.
- This avoids inventing server-side cwd persistence or trying to infer repo state from the PTY stream.
- Session-bound APIs keep the feature aligned with the existing browser terminal trust model.

Alternatives considered:
- Parse git information out of terminal output.
  - Rejected because PTY output is lossy, presentation-specific, and unreliable for machine-readable repo state.
- Add shell prompt hooks or filesystem watchers.
  - Rejected because they add operational complexity for a problem that periodic read-only polling can solve cleanly.

### 3. Run read-only git commands out-of-band from the interactive PTY path

Git inspection commands will run as separate host-initiated guest commands through the browserterm manager rather than through the live tmux/PTY stream. The terminal session remains interactive and unaffected by diff fetches.

The manager will:
- look up the authorized session and owning machine
- connect to the guest the same way browserterm already reaches shells
- execute bounded, read-only `git` commands against the requested cwd/repo
- return parsed status metadata and per-file diff payloads as JSON

Why:
- It prevents diff traffic from interfering with typing latency and terminal rendering.
- It keeps git inspection deterministic and independent from whatever the shell is currently printing.
- It fits the current `internal/httpapi` -> `internal/browserterm` ownership split.

Alternatives considered:
- Reuse the tmux session directly and run commands through the attached shell.
  - Rejected because it would pollute shell history/state and could race with user input.

### 4. Poll repo status while the sidebar is open, and page file patches on demand

The sidebar will use bounded polling only while it is open for an active shell. Status polling will refresh the repo/file list on a short interval such as 2 seconds. File patch requests will be fetched in small scroll-driven batches and additional file cards will load as the user nears the end of the current batch.

This splits the data flow into:
- lightweight repo status polling for freshness
- heavier per-file patch fetches only for files the user is actively reading in the stacked stream

Why:
- It gives the user the near-real-time behavior they asked for without always-on background load.
- It avoids shipping or rendering the entire repo patch set on every poll.
- It keeps the PTY and UI responsive even for repos with many modified files.

Alternatives considered:
- Poll full diffs for all files continuously.
  - Rejected because it scales poorly and would make large working trees unnecessarily expensive.
- Build a true push-based repository watcher.
  - Rejected because it adds host/guest complexity with limited product benefit for the first delivery.

### 5. Use machine-readable git status on the backend and a frontend parser for unified diff rendering

Repo status will be collected with a machine-readable git command such as `git status --porcelain=v2 -z` so the backend can return a stable file list with staged/unstaged/untracked metadata, branch information, and repo-root resolution.

Per-file patch retrieval will return raw unified diff text plus file metadata. The frontend will parse that patch into a unified diff model and render:
- sticky per-file headers
- filename/path plus add/remove counts
- left/right line gutters
- addition/removal backgrounds and paired changed lines
- collapsed unchanged regions with expand controls

The frontend will no longer show a separate changed-file list. Instead it will render file cards in a stacked scroll stream and page more cards in as the user scrolls.

Why:
- `git status --porcelain=v2` is robust for summaries and avoids scraping human-readable output.
- Returning raw patches keeps the backend close to git semantics and avoids encoding a presentation-specific split-diff model in Go.
- A local parser gives the frontend full control over the requested styling without taking a new dependency.

Alternatives considered:
- Return a fully structured split-diff model from the backend.
  - Rejected because it would move UI-specific formatting logic into the Go service boundary.
- Add a third-party React diff viewer.
  - Rejected because the user wants a very specific visual style and this repo should avoid unnecessary dependency additions.

### 6. Cap oversized and non-text diffs instead of trying to render everything

The sidebar will distinguish normal text patches from binary or oversized diffs. Backend responses should carry enough metadata for the UI to show a bounded fallback state when:
- the file is binary
- the patch exceeds a configured byte/line threshold
- git cannot produce a diff relative to the current repo state

Why:
- Large diffs can overwhelm both polling and browser rendering.
- Binary files do not map cleanly to the requested split-text presentation.
- A bounded fallback is better than freezing the workspace or timing out silently.

Alternatives considered:
- Attempt to render every diff regardless of size.
  - Rejected because it risks degraded terminal/workspace responsiveness.

## Risks / Trade-offs

- **[Client-reported cwd may lag behind the user's actual shell location]** -> Disable repo fetches until a cwd is known, refresh after each cwd update, and treat stale responses as replaceable rather than authoritative.
- **[Polling can create noticeable backend load on very active repos]** -> Poll only while the sidebar is open, keep status and patch requests separate, and cap expensive patch payloads.
- **[Raw patch parsing in the frontend adds implementation complexity]** -> Keep the parser narrowly scoped to unified diffs that Git emits for this feature and cover it with focused tests.
- **[Large, binary, or unborn-branch repos create edge cases]** -> Normalize repo-state responses on the backend and fall back to explicit informational states instead of failing the entire sidebar.
- **[Overlay layering can conflict with the existing right control sidebar]** -> Introduce an explicit overlay layer with controlled width/z-index and test interaction/focus behavior on desktop and smaller viewports.

## Migration Plan

1. Extend the browser terminal manager and HTTP API with shell-scoped read-only git inspection endpoints.
2. Add frontend API bindings and ephemeral sidebar state for the selected shell/session/cwd.
3. Add a shell-header git diff action and the command-center overlay container.
4. Implement repo status polling, per-file diff fetching, and the unified diff renderer.
5. Add tests for backend git inspection, frontend diff parsing, and sidebar interaction states.
6. Deploy as a normal web + backend release with no data migration.

Rollback:
- Remove or disable the new shell-header action and git inspection endpoints in a normal application rollback.
- Because the feature stores no new durable state, rollback does not require data cleanup or format migration.

## Open Questions

- What byte/line threshold gives the best balance between readable large diffs and safe browser performance for Fascinate's typical repos?
- Should the first release include inline word-diff highlighting for paired changed lines, or should that land immediately after the base unified diff if parser complexity proves higher than expected?
