## ADDED Requirements

### Requirement: Fascinate SHALL expose browser terminal session diagnostics
Fascinate SHALL provide operator-visible diagnostics for browser terminal session creation, attachment, and live stream health so terminal latency and session failures can be investigated without guessing from the UI alone.

#### Scenario: Failed browser terminal attach is diagnosable
- **WHEN** Fascinate cannot create or attach a browser terminal session for a machine
- **THEN** Fascinate exposes the affected user, machine, host, and failure stage through an operator-visible surface
- **AND** an operator can distinguish authorization failure from host reachability or PTY/session creation failure

#### Scenario: Terminal latency and load are observable
- **WHEN** browser terminal sessions are active on a host
- **THEN** Fascinate exposes operator-visible data about active session count and terminal-stream health
- **AND** an operator can inspect enough session-level information to investigate latency or backpressure problems
