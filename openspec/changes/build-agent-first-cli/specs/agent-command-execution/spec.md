## ADDED Requirements

### Requirement: Fascinate SHALL provide structured machine-scoped command execution
Fascinate SHALL provide a non-interactive command execution surface that runs commands inside a user's machine and returns structured execution results.

#### Scenario: Command returns structured success result
- **WHEN** a user or agent runs a non-interactive command against a running machine
- **THEN** Fascinate executes that command inside the target machine
- **AND** it returns the command's exit status, stdout, stderr, and execution metadata in a structured result

#### Scenario: Command failure remains explicit
- **WHEN** a non-interactive command exits non-zero
- **THEN** Fascinate returns the non-zero exit status without treating the request as transport failure
- **AND** the caller can distinguish command failure from control-plane or routing failure

### Requirement: Fascinate SHALL support explicit timeout and cancellation semantics for exec
Fascinate SHALL let callers bound command execution time and SHALL report timeout or cancellation as explicit command outcomes.

#### Scenario: Timed-out command reports timeout outcome
- **WHEN** a command exceeds the caller's supported execution timeout
- **THEN** Fascinate stops that command according to the exec contract
- **AND** it reports that the command timed out rather than reporting a generic transport error

#### Scenario: Cancelled command reports cancellation outcome
- **WHEN** a caller cancels an in-flight command through the supported exec workflow
- **THEN** Fascinate stops or detaches from that command according to the exec contract
- **AND** it reports the final outcome as cancelled

### Requirement: Fascinate SHALL make exec output safe for automation and streaming
Fascinate SHALL support streaming or machine-readable exec output modes that remain safe for automation and AI-agent use.

#### Scenario: Agent follows command output incrementally
- **WHEN** an agent runs a supported exec command in streaming mode
- **THEN** Fascinate emits ordered execution output events as the command runs
- **AND** it finishes with a final execution result event carrying the terminal status

#### Scenario: Agent sends multiline stdin safely
- **WHEN** an agent runs a supported exec command with local stdin attached
- **THEN** Fascinate forwards that stdin into the remote command without requiring shell-escaped heredocs
- **AND** the remote command still reports structured exit status, stdout, stderr, and timeout metadata

#### Scenario: Exec does not require a preexisting shared shell
- **WHEN** a user runs a non-interactive command against a machine that has no existing shared shell
- **THEN** Fascinate executes that command successfully through the exec surface
- **AND** it does not require the user to create or manage a shared shell first
