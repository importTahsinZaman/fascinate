## ADDED Requirements

### Requirement: Host admission SHALL apply soft CPU policy with hard RAM and storage checks
Fascinate SHALL evaluate create, start, fork, and restore requests under the host-scoped admission lock using soft shared CPU policy together with hard per-user RAM, retained-storage, machine-count, snapshot-count, and host safety checks.

#### Scenario: Active machine request is admitted under soft CPU policy
- **WHEN** a user requests create or start for a machine
- **AND** the request fits the user's hard RAM, retained-storage, machine-count, and snapshot-count limits
- **AND** the request fits host shared CPU capacity and host safety checks
- **THEN** Fascinate admits the request under the host-scoped lock
- **AND** it does not reject the request only because the user's nominal active CPU demand exceeds a soft entitlement

#### Scenario: Active machine request is rejected on host shared CPU exhaustion
- **WHEN** a user requests create or start for a machine
- **AND** the request would exceed the host shared CPU ceiling even though the user's hard RAM and retained-storage limits still fit
- **THEN** Fascinate rejects the request before the machine becomes runnable
- **AND** the rejection identifies shared host CPU capacity as the exhausted dimension

### Requirement: Clone and restore SHALL use the same shared-CPU admission semantics as create and start
Fascinate SHALL apply the same soft shared CPU and hard RAM/storage admission policy to clone and create-from-snapshot flows that it applies to fresh create and restart.

#### Scenario: Clone is admitted under shared CPU headroom
- **WHEN** a user clones a machine
- **AND** the user's hard RAM, retained-storage, machine-count, and snapshot-count limits still fit
- **AND** the source host still has shared CPU headroom and other host safety checks pass
- **THEN** Fascinate admits the clone request
- **AND** it uses the same shared-CPU policy as fresh machine creation

#### Scenario: Restore is rejected when host shared CPU is saturated
- **WHEN** a user creates a machine from a snapshot
- **AND** the user's hard RAM and retained-storage limits still fit
- **AND** the snapshot's host no longer has shared CPU headroom for another active machine
- **THEN** Fascinate rejects the restore request
- **AND** it does not begin a restore that would overfill the host's shared CPU ceiling
