## 1. Backend Git Inspection APIs

- [x] 1.1 Extend the browser terminal manager with session-authorized git status and per-file diff methods that execute read-only guest `git` commands outside the interactive PTY path.
- [x] 1.2 Add HTTP API routes, request/response types, and backend tests for repo-root resolution, non-repo cwd handling, session authorization, and git command failure states.
- [x] 1.3 Add backend handling for binary or oversized file diffs so the API can return explicit non-renderable metadata instead of timing out or failing silently.

## 2. Shell Header And Sidebar State

- [x] 2.1 Extend the web API bindings and workspace-local state to track the active git diff sidebar shell, latest cwd, loading states, scroll-paged diff batches, and polling lifecycle without persisting that state in workspace layouts.
- [x] 2.2 Add a git diff action to the shell window header and render a command-center overlay container that opens on the right side without changing workspace canvas geometry.
- [x] 2.3 Wire the sidebar to shell switching behavior so opening diffs from another window rebinds the panel to that shell's session and cwd context.

## 3. Repo Status And Diff Rendering

- [x] 3.1 Implement sidebar file-list loading, non-repository empty state, recoverable error state, and open-state polling for repo status refresh.
- [x] 3.2 Implement per-file diff fetching plus a local unified-diff parser that produces split left/right hunks, line gutters, and collapsed unchanged regions.
- [x] 3.3 Implement the review-style diff presentation with sticky file headers, file path/change counts, elevated dark styling, expandable context blocks, and explicit fallbacks for non-renderable files.

## 4. Verification

- [x] 4.1 Add frontend tests for cwd-driven repo detection, sidebar shell switching, polling refresh behavior, and key split-diff rendering states.
- [x] 4.2 Run targeted Go tests for the affected browserterm/httpapi packages and run the web test/build flow for the sidebar UI changes.
