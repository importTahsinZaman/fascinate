## ADDED Requirements

### Requirement: Fascinate SHALL define a validation matrix for its supported platform behaviors
Fascinate SHALL maintain an explicit validation matrix covering the platform behaviors it currently claims to support.

#### Scenario: Supported product surface is enumerated
- **WHEN** an operator reviews the stress-validation artifacts for Fascinate
- **THEN** they can identify the expected behaviors for VM lifecycle, shell access, public app routing, guest workloads, snapshots, create-from-snapshot, true cloning, cleanup, and persisted tool auth
- **AND** each behavior maps to at least one automated test, host smoke, or clearly identified validation path

### Requirement: Fascinate SHALL validate realistic guest workloads under stress
Fascinate SHALL validate the current product surface using realistic in-guest workloads instead of only synthetic idle guests.

#### Scenario: Application workload survives normal validation flow
- **WHEN** Fascinate validates a machine running a public application workload
- **THEN** the validation confirms the workload is reachable from the public machine URL
- **AND** the validation confirms the machine remains shell-accessible and routable through the normal control-plane and frontdoor path

#### Scenario: Local database and Docker workloads are exercised
- **WHEN** Fascinate validates a machine running a local database process and one or more Docker containers
- **THEN** the validation confirms those workloads start successfully inside the guest
- **AND** the validation confirms they remain correct across the supported lifecycle steps being tested

### Requirement: Fascinate SHALL validate snapshots and true clones against live workloads
Fascinate SHALL validate snapshot save, create-from-snapshot, and clone behavior using VMs that already contain active workloads.

#### Scenario: Snapshot restore preserves running workload state
- **WHEN** Fascinate restores a new VM from a snapshot taken from a running workload
- **THEN** the restored VM comes up with the expected workload state already present
- **AND** the validation confirms the workload behaves as a restored environment rather than a fresh boot from source files alone

#### Scenario: True clone remains independent after restore
- **WHEN** Fascinate clones a running VM and both source and clone continue running
- **THEN** the validation confirms source and clone can diverge after the clone completes
- **AND** changes or workload shutdown in one VM do not implicitly change the other

