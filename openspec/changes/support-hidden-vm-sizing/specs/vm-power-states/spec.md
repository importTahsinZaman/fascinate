## ADDED Requirements

### Requirement: Users SHALL be able to stop and start retained machines
Fascinate SHALL allow a user to stop one of their retained machines and later start it again without deleting and recreating it.

#### Scenario: User stops a running machine
- **WHEN** a user requests stop for one of their running machines
- **THEN** Fascinate transitions that machine through a stopping state to `STOPPED`
- **AND** it preserves the machine's retained disk and ownership metadata for later reuse

#### Scenario: User starts a stopped machine
- **WHEN** a user requests start for one of their stopped machines and the request fits within current shared compute and host-capacity limits
- **THEN** Fascinate transitions that machine through a starting state to `RUNNING`
- **AND** the machine becomes usable again without being recreated from the base image

### Requirement: Machine actions SHALL be power-state-aware
Fascinate SHALL allow only the actions that make sense for the machine's current power state.

#### Scenario: Stopped machine rejects live-only actions
- **WHEN** a machine is `STOPPED`
- **THEN** Fascinate rejects shell entry, live snapshot, and live clone actions for that machine
- **AND** it allows the user to start or delete the machine

#### Scenario: Transitional machine suppresses conflicting actions
- **WHEN** a machine is `CREATING`, `STARTING`, `STOPPING`, or `DELETING`
- **THEN** Fascinate does not expose conflicting lifecycle actions as ready
- **AND** it does not report the machine as shell-ready until the transition completes

### Requirement: Restarted machines SHALL preserve retained machine contents
Fascinate SHALL resume a stopped machine from its retained machine state rather than treating start as a fresh-machine provision flow.

#### Scenario: Restart preserves machine filesystem and environment
- **WHEN** a user starts a previously stopped machine
- **THEN** the machine resumes with the same retained root disk contents and managed environment state it had when stopped
- **AND** Fascinate does not replace it with a newly created machine

#### Scenario: Restart waits for honest usability before running
- **WHEN** Fascinate starts a stopped machine
- **THEN** it keeps the machine in a non-ready starting state until the machine is reachable again through Fascinate's normal shell and routing paths
- **AND** it does not expose the machine as `RUNNING` before that point
