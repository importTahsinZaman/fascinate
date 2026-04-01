## ADDED Requirements

### Requirement: Fascinate SHALL provide an ordered horizontal terminal workspace
Fascinate SHALL provide a browser workspace where a user can keep multiple terminal windows visible at once in a rigid left-to-right strip and reorder them without using a freeform canvas.

#### Scenario: User keeps multiple terminal windows visible
- **WHEN** a user opens terminals for multiple machines or multiple shells of the same machine
- **THEN** Fascinate shows those terminals as separate windows in the workspace
- **AND** the user can keep them visible at the same time

#### Scenario: User reorders terminal windows
- **WHEN** a user drags a shell header to change the order of terminal windows in the workspace
- **THEN** the terminal remains usable during that interaction
- **AND** Fascinate preserves the updated shell order for later use

#### Scenario: User scans open shells separately from machine inventory
- **WHEN** a user opens the browser workspace with one or more live shells
- **THEN** Fascinate shows those shells in a dedicated shell list ordered the same way as the horizontal workspace
- **AND** machine management actions remain available in a separate machine inventory section rather than nesting open shells inside each machine card

### Requirement: Fascinate SHALL support multiple browser terminal sessions per machine
Fascinate SHALL allow a user to open more than one browser terminal session into the same machine at the same time.

#### Scenario: User opens two shells into one machine
- **WHEN** a user opens a second browser terminal for a machine that already has an active terminal window
- **THEN** Fascinate creates a distinct session for that second shell
- **AND** both shells remain independently usable

#### Scenario: Busy terminal does not block another terminal
- **WHEN** one browser terminal session is producing heavy output
- **THEN** another active terminal session for the same user remains interactive
- **AND** its input handling does not wait for the busy terminal to become quiet

### Requirement: Fascinate SHALL keep browser terminal streaming low-latency and interactive
Fascinate SHALL stream browser terminal input and output through a dedicated interactive session path suitable for continuous typing and live agent output.

#### Scenario: Terminal attach produces a usable shell quickly
- **WHEN** a user opens a browser terminal window
- **THEN** Fascinate establishes an interactive terminal session without requiring a full page reload
- **AND** the terminal becomes usable as part of the workspace flow rather than a detached download or polling experience

#### Scenario: Terminal input is not coupled to general page renders
- **WHEN** terminal output is streaming into the workspace
- **THEN** Fascinate does not require whole-page rerenders to display that output
- **AND** the rest of the workspace remains responsive to reorder, focus, scroll, and other interactions

### Requirement: Fascinate SHALL support standard copy shortcuts for terminal selections
Fascinate SHALL copy the active xterm selection to the local browser clipboard when the user presses the platform copy shortcut inside the browser workspace.

#### Scenario: User copies a selected terminal region
- **WHEN** a user highlights text inside a terminal window and presses `Cmd-C` on macOS or `Ctrl-C` on other platforms
- **THEN** Fascinate copies the selected terminal text to the local clipboard
- **AND** Fascinate does not send an interrupt into the shell for that shortcut while the selection is active

### Requirement: Fascinate SHALL persist workspace layout independently from live sessions
Fascinate SHALL persist the user's workspace layout separately from the live terminal session state.

#### Scenario: User reloads the app and keeps their workspace shape
- **WHEN** a user returns to the browser app after leaving or refreshing it
- **THEN** Fascinate restores the saved workspace layout for that user
- **AND** terminal windows reopen in their previously saved arrangement even if some live sessions need to reconnect or restart

#### Scenario: Session failure does not destroy layout
- **WHEN** a terminal session exits or fails
- **THEN** Fascinate preserves the surrounding workspace layout
- **AND** the user can reopen a terminal in that workspace without rebuilding the shell strip manually
