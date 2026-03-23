## Why

Fascinate already centers the product around persistent browser terminals, but users still have to drop into raw `git` commands to understand what changed in the repo behind a shell. That breaks the browser-first workflow at exactly the moment users need a fast, review-grade read on their working tree.

The terminal workspace already tracks shell session IDs and current working directories, which makes this the right time to add a shell-native diff surface. A dedicated diff sidebar can turn the browser workspace into a better command center for agent-driven development without pushing users back to local editors or terminal-only review habits.

## What Changes

- Add a shell-header git diff action that opens a fixed overlay above the control sidebar for the selected shell without resizing or reflowing the workspace canvas.
- Add repo-aware shell diff behavior that resolves the active repository from the shell's current working directory, even when the shell is nested below the repository root.
- Add browser-visible git status and file diff retrieval for live shell sessions through explicit backend endpoints rather than the interactive PTY stream.
- Add near-real-time refresh while the diff sidebar is open so changed files and hunks stay current without requiring prompt hooks or full page reloads.
- Add split-view file diffs with sticky file headers, per-file add/remove counts, collapsed-context expansion, line gutters, and dark elevated styling aligned with the provided review UI inspiration.
- Add explicit empty, loading, and error states for shells outside a git repo, unavailable sessions, and git command failures.

## Capabilities

### New Capabilities
- `browser-shell-git-diffs`: shell-scoped git repository detection, stacked file-stream diff browsing, and unified diff rendering inside the browser workspace

### Modified Capabilities

## Impact

- Affected code includes browser terminal workspace UI, shell window chrome, workspace-local state, API bindings, browser terminal session plumbing, and HTTP handlers that serve shell-scoped git metadata and diffs
- Affected backend systems include browser terminal session management and host-side command execution for read-only `git status` / `git diff` inspection
- No database migration or persisted format change is required if shell diff state stays ephemeral to the browser session
- The change should rely on the guest's existing `git` installation and avoid introducing a second source of truth for repository state
