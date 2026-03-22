## ADDED Requirements

### Requirement: Users SHALL be able to create full VM snapshots
Fascinate SHALL allow a user to create a saved snapshot of one of their VMs at any time. A saved snapshot SHALL capture the VM’s disk, memory, and device state as a point-in-time machine image.

#### Scenario: Snapshot of a running VM succeeds
- **WHEN** a user creates a snapshot for one of their running VMs
- **THEN** Fascinate stores a saved snapshot owned by that user
- **AND** the saved snapshot contains the VM’s disk, memory, and device state from that moment
- **AND** the source VM resumes normal operation after the snapshot is recorded

#### Scenario: Snapshot failure does not destroy the source VM
- **WHEN** snapshot creation fails for a VM
- **THEN** Fascinate marks the snapshot attempt as failed
- **AND** the source VM remains available to the user unless it was already broken before the attempt

### Requirement: Users SHALL be able to create a new VM from a saved snapshot
Fascinate SHALL allow a user to create a new VM from one of their saved snapshots. The restored VM SHALL resume from the captured snapshot state rather than booting as a fresh machine from the base image.

#### Scenario: Create from snapshot restores live machine state
- **WHEN** a user creates a new VM from a saved snapshot
- **THEN** the new VM resumes with the filesystem, memory contents, and device state captured in that snapshot
- **AND** running processes present in the snapshot are still running after restore

#### Scenario: Snapshot restore does not require fresh guest provisioning
- **WHEN** a VM is created from a saved snapshot
- **THEN** Fascinate does not treat it as a fresh base-image boot
- **AND** it does not require first-boot package installation or post-create tool-auth hydration to make the restored machine usable

### Requirement: Clone SHALL be a true live clone created from an implicit snapshot
Fascinate SHALL implement VM cloning by automatically creating a point-in-time snapshot of the source VM and restoring that snapshot into a new VM owned by the same user.

#### Scenario: Clone preserves live development environment
- **WHEN** a user clones one of their VMs
- **THEN** Fascinate creates an implicit snapshot of the source VM
- **AND** the cloned VM resumes with the source VM’s running development environment intact
- **AND** development servers, Docker containers, and other in-memory process state present at snapshot time remain available in the clone

#### Scenario: Clone does not degrade to disk-only copy semantics
- **WHEN** a user clones a VM
- **THEN** the clone is not implemented as only a copied root disk plus a fresh guest boot
- **AND** the clone resumes from snapshot restore semantics instead

### Requirement: Snapshot-based machines SHALL become ready only after restore is usable
Fascinate SHALL keep a snapshot-created or cloned VM in a non-ready state until the restored VM is reachable through Fascinate’s normal shell and app-routing paths.

#### Scenario: Restored VM waits for usable readiness
- **WHEN** Fascinate is restoring a VM from a saved or implicit snapshot
- **THEN** the machine remains in a provisioning state until the restored VM is reachable again
- **AND** Fascinate does not expose shell entry as ready before that point

#### Scenario: Failed restore becomes visible
- **WHEN** snapshot restore fails
- **THEN** Fascinate marks the target VM as failed
- **AND** it does not leave the user with a machine that appears ready but cannot be entered

### Requirement: Snapshot restore SHALL isolate guest network identity from other VMs
Fascinate SHALL allow snapshot-created and cloned VMs to preserve the saved guest memory and device networking state without conflicting with the source VM or other restored VMs on the same host.

#### Scenario: Source VM and clone keep identical guest IP state safely
- **WHEN** a user clones a running VM and both the source and clone remain on the same host
- **THEN** Fascinate allows both VMs to coexist without requiring the restored clone to rewrite the saved guest IP or MAC state after restore
- **AND** the source VM remains reachable through its Fascinate shell and app URL
- **AND** the clone becomes reachable through its own Fascinate shell and app URL

#### Scenario: Public routing does not depend on globally unique guest IPs
- **WHEN** Fascinate routes shell or app traffic to snapshot-created or cloned VMs
- **THEN** it does not require each VM to have a globally unique guest IP in the host root namespace
- **AND** it uses per-machine host-side routing or forwarding that isolates one VM’s guest network identity from another’s

### Requirement: Saved snapshots SHALL be immutable user-owned artifacts
Fascinate SHALL persist snapshots as immutable artifacts owned by the creating user and SHALL allow only that user to restore or delete them unless a future access-sharing feature explicitly changes that rule.

#### Scenario: Snapshot ownership is enforced
- **WHEN** one user attempts to create a VM from or delete another user’s saved snapshot
- **THEN** Fascinate rejects the request
- **AND** the other user’s snapshot remains unchanged

#### Scenario: Snapshot contents do not mutate after creation
- **WHEN** a snapshot is successfully created
- **THEN** later changes inside the source VM do not alter that saved snapshot
- **AND** later restores from that snapshot reproduce the saved point-in-time state rather than the source VM’s newer state
