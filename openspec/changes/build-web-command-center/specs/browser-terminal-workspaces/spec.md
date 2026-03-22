## ADDED Requirements

### Requirement: Fascinate SHALL provide a canvas-style terminal workspace
Fascinate SHALL provide a browser workspace where a user can keep multiple terminal windows visible at once and arrange them freely on a canvas-like surface.

#### Scenario: User keeps multiple terminal windows visible
- **WHEN** a user opens terminals for multiple machines or multiple shells of the same machine
- **THEN** Fascinate shows those terminals as separate windows in the workspace
- **AND** the user can keep them visible at the same time

#### Scenario: User repositions and resizes terminal windows
- **WHEN** a user drags or resizes a terminal window in the workspace
- **THEN** the terminal remains usable during that interaction
- **AND** Fascinate preserves the updated window position and dimensions for later use

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
- **AND** the rest of the workspace remains responsive to drag, resize, focus, and other interactions

### Requirement: Fascinate SHALL persist workspace layout independently from live sessions
Fascinate SHALL persist the user's workspace layout separately from the live terminal session state.

#### Scenario: User reloads the app and keeps their workspace shape
- **WHEN** a user returns to the browser app after leaving or refreshing it
- **THEN** Fascinate restores the saved workspace layout for that user
- **AND** terminal windows reopen in their previously saved arrangement even if some live sessions need to reconnect or restart

#### Scenario: Session failure does not destroy layout
- **WHEN** a terminal session exits or fails
- **THEN** Fascinate preserves the surrounding workspace layout
- **AND** the user can reopen a terminal in that workspace without rebuilding the whole canvas manually
