## MODIFIED Requirements

### Requirement: Users SHALL be able to create full VM snapshots
Fascinate SHALL allow a user to create a saved snapshot of one of their VMs when the request fits within the user's retained-snapshot policy, retained-disk budget, and current host safety checks. A saved snapshot SHALL capture the VM's disk, memory, and device state as a point-in-time machine image.

#### Scenario: Snapshot of a running VM succeeds within limits
- **WHEN** a user creates a snapshot for one of their running VMs and the resulting retained snapshot count and retained disk usage remain within policy
- **THEN** Fascinate stores a saved snapshot owned by that user
- **AND** the saved snapshot contains the VM's disk, memory, and device state from that moment
- **AND** the source VM resumes normal operation after the snapshot is recorded

#### Scenario: Snapshot request is rejected at retained snapshot cap
- **WHEN** a user requests a saved snapshot and already has the maximum allowed number of retained snapshots
- **THEN** Fascinate rejects the snapshot request before snapshot creation begins
- **AND** the user's existing retained snapshots remain unchanged

#### Scenario: Snapshot failure does not destroy the source VM
- **WHEN** snapshot creation fails for a VM after admission succeeds
- **THEN** Fascinate marks the snapshot attempt as failed
- **AND** the source VM remains available to the user unless it was already broken before the attempt

### Requirement: Clone SHALL be a true live clone created from an implicit snapshot
Fascinate SHALL implement VM cloning by automatically creating a point-in-time snapshot of the source VM and restoring that snapshot into a new VM owned by the same user. The implicit clone snapshot SHALL be treated as a transient internal artifact rather than a retained user snapshot.

#### Scenario: Clone preserves live development environment
- **WHEN** a user clones one of their VMs and the resulting machine fits within the user's machine-resource budgets and host safety checks
- **THEN** Fascinate creates an implicit snapshot of the source VM
- **AND** the cloned VM resumes with the source VM's running development environment intact
- **AND** development servers, Docker containers, and other in-memory process state present at snapshot time remain available in the clone

#### Scenario: Clone implicit snapshot does not consume retained snapshot cap
- **WHEN** Fascinate creates the implicit snapshot used only to complete a clone
- **THEN** that implicit snapshot is not retained as a user-visible saved snapshot after clone completion
- **AND** it does not count against the user's retained snapshot limit

#### Scenario: Clone does not degrade to disk-only copy semantics
- **WHEN** a user clones a VM
- **THEN** the clone is not implemented as only a copied root disk plus a fresh guest boot
- **AND** the clone resumes from snapshot restore semantics instead
