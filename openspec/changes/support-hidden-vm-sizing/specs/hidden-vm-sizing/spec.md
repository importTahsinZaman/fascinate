## ADDED Requirements

### Requirement: Fascinate SHALL keep machine sizing hidden from normal users
Fascinate SHALL create machines from an internal baseline size without requiring the user to choose CPU, RAM, or disk in the normal browser flow. Clone and snapshot-restore flows SHALL preserve the source machine's internal shape without exposing a size picker or boost mode.

#### Scenario: Fresh machine create does not ask for a size
- **WHEN** a user creates a fresh machine from the normal browser flow
- **THEN** Fascinate provisions the machine from an internal baseline shape
- **AND** the request does not require the user to choose CPU, RAM, or disk

#### Scenario: Clone and restore inherit hidden shape
- **WHEN** a user clones a machine or creates a machine from a snapshot
- **THEN** Fascinate assigns the target machine the same hidden resource shape as the source machine or snapshot
- **AND** it does not expose a manual size-selection step

### Requirement: Fascinate SHALL enforce shared compute budgets using only active machine states
Fascinate SHALL charge per-user CPU and RAM budgets only for machines that are actively booting or running. Retained machines that are fully stopped SHALL not consume the user's active CPU or RAM budget.

#### Scenario: Stopping a machine frees shared compute headroom
- **WHEN** a user's running machine transitions to `STOPPED`
- **THEN** that machine no longer counts toward the user's CPU or RAM budget
- **AND** the freed headroom becomes available for other machine starts or creates

#### Scenario: Start is rejected when active compute headroom is exhausted
- **WHEN** a user tries to start, create, clone, or restore a machine that would exceed the user's active CPU or RAM budget
- **THEN** Fascinate rejects the request before the machine becomes runnable
- **AND** the rejection explains that the shared compute budget is exhausted

### Requirement: Fascinate SHALL enforce retained storage budgets independently from active compute
Fascinate SHALL enforce per-user disk budgets using retained machine storage plus retained snapshot storage, independent from whether a machine is currently running or stopped.

#### Scenario: Stopped machine still counts toward retained storage
- **WHEN** a machine is stopped but still retained by the user
- **THEN** Fascinate continues to count that machine's retained storage usage toward the user's disk budget
- **AND** stopping the machine does not by itself free disk headroom

#### Scenario: Retained storage budget blocks new retained artifacts
- **WHEN** creating, cloning, restoring, or snapshotting a machine would cause the user's retained machine and snapshot storage to exceed the user's disk budget
- **THEN** Fascinate rejects the request before creating the retained artifact
- **AND** the rejection identifies retained storage as the exhausted budget dimension
