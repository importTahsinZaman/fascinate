## ADDED Requirements

### Requirement: Fascinate SHALL let users open a shell-scoped git diff sidebar from the workspace
Fascinate SHALL provide a git diff action in each browser shell window so a user can inspect repository changes for that shell without leaving the browser workspace or changing the canvas layout.

#### Scenario: User opens the diff sidebar for a shell
- **WHEN** a user activates the git diff action for a shell window with an active browser terminal session
- **THEN** Fascinate opens a git diff overlay above the control sidebar bound to that shell
- **AND** the workspace canvas and terminal window geometry remain unchanged

#### Scenario: User switches the sidebar to another shell
- **WHEN** a user activates the git diff action for a different shell window while the sidebar is already open
- **THEN** Fascinate updates the sidebar to the newly selected shell's git context
- **AND** the user does not need to reload the page or reopen the workspace

### Requirement: Fascinate SHALL resolve repository context from the selected shell's current working directory
Fascinate SHALL determine whether the selected shell is inside a git repository by using that shell's latest known working directory and SHALL resolve the repository root even when the shell is nested below it.

#### Scenario: Shell is in a nested repository path
- **WHEN** the selected shell's current working directory is a subdirectory inside a git repository
- **THEN** Fascinate resolves the repository root for that working directory
- **AND** the sidebar lists file changes for the repository rather than only the current subdirectory name

#### Scenario: Shell is not in a git repository
- **WHEN** the selected shell's current working directory is not inside a git repository
- **THEN** Fascinate shows an explicit non-repository sidebar state
- **AND** it does not present stale file changes from a previous repository context

### Requirement: Fascinate SHALL keep sidebar repo status current while the sidebar is active
Fascinate SHALL refresh repository status for the selected shell while its git diff sidebar is open so that the stacked diff stream reflects recent working-tree changes without requiring a full page reload.

#### Scenario: Working tree changes while the sidebar is open
- **WHEN** files are added, removed, or modified in the selected shell's repository while the sidebar is open
- **THEN** Fascinate refreshes the visible file stream for that shell
- **AND** the update occurs without interrupting terminal interaction in the workspace

#### Scenario: Selected file diff becomes stale after a repo update
- **WHEN** the user is viewing a file diff and that file's repository status changes during sidebar refresh
- **THEN** Fascinate refreshes the active shell's diff stream for files currently visible or newly paged into view
- **AND** the sidebar remains open on the same shell context while preserving the user's scroll position as closely as practical

### Requirement: Fascinate SHALL render changed files in a split diff review layout
Fascinate SHALL present changed files in a split diff layout suitable for browser review, including per-file metadata, synchronized left/right line presentation, collapsed unchanged context, and a scrollable stacked file stream instead of a separate changed-file list.

#### Scenario: User opens a changed file diff
- **WHEN** a user selects a changed file in the git diff sidebar
- **THEN** Fascinate renders that file in a split left/right diff view
- **AND** the file view includes a sticky file header with file identity and change counts
- **AND** the diff includes line gutters and distinct visual treatment for additions and removals

#### Scenario: Diff contains unchanged regions between edits
- **WHEN** a changed file has long unchanged sections between modified hunks
- **THEN** Fascinate collapses those unchanged regions behind explicit expand controls
- **AND** the user can expand the hidden context without leaving the file diff view

### Requirement: Fascinate SHALL surface recoverable git diff sidebar states
Fascinate SHALL show explicit loading, empty, and error states for shell-scoped git diff inspection instead of failing silently or leaving stale content visible.

#### Scenario: Git inspection request fails
- **WHEN** Fascinate cannot load git status or diff data for the selected shell because the session is unavailable or the git command fails
- **THEN** the sidebar shows an explicit error state for that shell
- **AND** the user can retry git inspection without reloading the entire workspace

#### Scenario: File diff cannot be rendered as normal text
- **WHEN** a selected changed file is binary or exceeds the supported diff size for inline rendering
- **THEN** Fascinate shows an explicit non-renderable file state in the sidebar
- **AND** the rest of the sidebar remains usable for other changed files in the same repository
