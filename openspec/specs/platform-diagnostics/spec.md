# platform-diagnostics Specification

## Purpose
TBD - created by archiving change stress-test-fascinate. Update Purpose after archive.
## Requirements
### Requirement: Fascinate SHALL expose operator-visible machine and snapshot diagnostics
Fascinate SHALL provide operator-visible diagnostics for VM and snapshot lifecycle work so an operator can determine what the platform is doing, what failed, and what runtime artifacts currently exist for a given machine or snapshot.

#### Scenario: Machine provisioning failure is diagnosable
- **WHEN** a machine create or restore operation fails during provisioning
- **THEN** Fascinate exposes the lifecycle stage that failed and a human-usable failure reason through an operator-visible surface
- **AND** the operator can identify the affected machine and its runtime handle without logging directly into the guest

#### Scenario: Snapshot or clone failure is diagnosable
- **WHEN** snapshot save, create-from-snapshot, or clone fails
- **THEN** Fascinate exposes the failing snapshot or machine state and the relevant runtime artifact identifiers through an operator-visible surface
- **AND** the operator can determine whether cleanup completed or which artifacts remain

### Requirement: Fascinate SHALL expose runtime forwarding and reachability diagnostics
Fascinate SHALL provide operator-visible diagnostics for the host-side routing and shell-forwarding layer so routing and shell-entry failures can be distinguished from guest workload failures.

#### Scenario: App route can be traced to host-side forwarding state
- **WHEN** a machine is marked `RUNNING` but its public app URL is not serving the expected workload
- **THEN** Fascinate exposes the host-side forwarding target and readiness state for that machine
- **AND** the operator can determine whether the problem is in the host forwarding layer or inside the guest workload

#### Scenario: Shell-entry failure can be traced to guest reachability state
- **WHEN** a user or operator cannot enter a machine shell
- **THEN** Fascinate exposes enough machine reachability state to distinguish guest-SSH readiness problems from malformed shell handoff or forwarding problems

