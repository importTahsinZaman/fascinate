## MODIFIED Requirements

### Requirement: Fascinate SHALL expose operator-visible machine and snapshot diagnostics
Fascinate SHALL provide operator-visible diagnostics for VM and snapshot lifecycle work so an operator can determine what the platform is doing, what failed, what runtime artifacts currently exist for a given machine or snapshot, and whether a machine is running, stopped, starting, or stopping.

#### Scenario: Machine provisioning or power-transition failure is diagnosable
- **WHEN** a machine create, restore, start, or stop operation fails during provisioning or power transition
- **THEN** Fascinate exposes the lifecycle stage that failed and a human-usable failure reason through an operator-visible surface
- **AND** the operator can identify the affected machine and its runtime handle without logging directly into the guest

#### Scenario: Snapshot or clone failure is diagnosable
- **WHEN** snapshot save, create-from-snapshot, or clone fails
- **THEN** Fascinate exposes the failing snapshot or machine state and the relevant runtime artifact identifiers through an operator-visible surface
- **AND** the operator can determine whether cleanup completed or which artifacts remain

### Requirement: Fascinate SHALL expose runtime forwarding and reachability diagnostics
Fascinate SHALL provide operator-visible diagnostics for the host-side routing and shell-forwarding layer so routing and shell-entry failures can be distinguished from guest workload failures and from a machine simply being powered off.

#### Scenario: App route can be traced to host-side forwarding state
- **WHEN** a machine is marked `RUNNING` but its public app URL is not serving the expected workload
- **THEN** Fascinate exposes the host-side forwarding target and readiness state for that machine
- **AND** the operator can determine whether the problem is in the host forwarding layer or inside the guest workload

#### Scenario: Stopped machine diagnostics explain unavailable shell and route state
- **WHEN** a machine is `STOPPED`
- **THEN** Fascinate exposes enough diagnostics to show that shell and app-routing are unavailable because the machine is powered off
- **AND** the operator can distinguish that state from malformed forwarding or guest reachability failure

## ADDED Requirements

### Requirement: Fascinate SHALL expose power-state-aware budget diagnostics
Fascinate SHALL provide diagnostics that separate active running compute usage from retained machine and snapshot storage usage.

#### Scenario: Diagnostics show running compute versus retained storage
- **WHEN** an authorized caller requests per-user budget diagnostics
- **THEN** Fascinate returns current CPU and RAM usage for actively booting or running machines
- **AND** it separately returns retained storage usage for machines and snapshots

#### Scenario: Diagnostics show why another machine cannot start
- **WHEN** a user has retained machines but cannot start or create another active machine
- **THEN** Fascinate diagnostics show the remaining active CPU and RAM headroom
- **AND** they make clear whether the limiting factor is active compute, retained storage, machine count, or snapshot count
