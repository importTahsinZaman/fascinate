## ADDED Requirements

### Requirement: Fascinate SHALL expose shared shell and exec diagnostics
Fascinate SHALL provide operator-visible diagnostics for shared shell state, attachment health, event delivery, and non-interactive command execution.

#### Scenario: Shared shell synchronization failure is diagnosable
- **WHEN** a user reports that CLI and web shell state are out of sync or a shared shell cannot be attached
- **THEN** Fascinate exposes enough shell and attachment diagnostics to determine the shell's owner, machine, host, lifecycle state, and recent attachment failures
- **AND** the operator can distinguish metadata-sync problems from guest-shell reachability problems

#### Scenario: Exec failure is diagnosable
- **WHEN** a non-interactive command fails, times out, or is cancelled
- **THEN** Fascinate exposes enough diagnostics to determine the target machine, owning host, execution outcome, and failure class
- **AND** the operator can distinguish command-exit failure from routing or control-plane failure
