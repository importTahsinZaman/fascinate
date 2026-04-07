## ADDED Requirements

### Requirement: Fascinate SHALL provide an authenticated CLI command center
Fascinate SHALL provide a first-class CLI that authenticates users to the same product control plane used by the web app and allows them to use Fascinate without depending on the browser UI.

#### Scenario: User signs in from the CLI
- **WHEN** a user runs the supported CLI login flow and completes email-code verification
- **THEN** Fascinate authenticates that user without requiring a browser session cookie
- **AND** it mints a durable CLI/API credential the CLI can use on later requests

#### Scenario: Agent authenticates non-interactively
- **WHEN** a CLI process is given a valid Fascinate API token through supported non-interactive configuration
- **THEN** Fascinate authorizes that process without prompting for interactive input
- **AND** the CLI can use the same product surfaces available to an interactive user

### Requirement: Fascinate SHALL expose core product workflows through the CLI
Fascinate SHALL let a user manage machines, snapshots, env vars, diagnostics, and shared shells from the CLI as first-class product workflows.

#### Scenario: User manages machines and snapshots from the CLI
- **WHEN** a user runs CLI commands to create, inspect, fork, delete, snapshot, or restore machines
- **THEN** Fascinate performs those operations through the existing control-plane lifecycle
- **AND** the CLI shows the resulting state transitions and final resource state

#### Scenario: User manages env vars and diagnostics from the CLI
- **WHEN** a user runs CLI commands to list, set, unset, inspect, or diagnose Fascinate resources
- **THEN** Fascinate returns user-scoped env-var and diagnostics information through the CLI
- **AND** those commands do not require the browser app to complete the workflow

### Requirement: Fascinate SHALL provide automation-safe CLI output and exit behavior
Fascinate SHALL make CLI commands safe for automation and AI-agent use by providing stable machine-readable output, predictable prompting rules, and consistent exit semantics.

#### Scenario: Command emits machine-readable output
- **WHEN** a user or agent runs a supported CLI command with machine-readable output enabled
- **THEN** the CLI prints only the declared structured response format to stdout
- **AND** progress, warnings, and human-oriented commentary are kept off stdout

#### Scenario: Non-interactive command avoids unexpected prompts
- **WHEN** a CLI command that needs confirmation or credentials runs without an interactive TTY
- **THEN** the CLI fails clearly instead of hanging on an unseen prompt
- **AND** it returns a non-zero exit status that automation can detect reliably
