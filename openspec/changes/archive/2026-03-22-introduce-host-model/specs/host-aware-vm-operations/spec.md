## ADDED Requirements

### Requirement: Machines and snapshots have explicit host ownership
Every machine and snapshot SHALL belong to exactly one registered host. The control plane MUST persist that ownership and use it for lifecycle operations, diagnostics, and future placement decisions.

#### Scenario: Brand new machine is assigned to a host
- **WHEN** a user creates a new machine from the base image in a single-host deployment
- **THEN** Fascinate SHALL assign that machine to the one eligible host
- **AND** the machine record SHALL persist that host ownership

#### Scenario: Snapshot inherits source host ownership
- **WHEN** a user saves a snapshot from an existing machine
- **THEN** Fascinate SHALL assign the snapshot to the same host that owns the source machine
- **AND** the snapshot record SHALL persist that host ownership

### Requirement: Runtime operations dispatch through a host boundary
The control plane MUST execute machine and snapshot runtime operations through a host-aware execution boundary rather than assuming the local runtime is globally authoritative.

#### Scenario: Create uses the owning host executor
- **WHEN** Fascinate provisions a new machine
- **THEN** the control plane SHALL resolve an owning host first
- **AND** it SHALL invoke that host's runtime executor to perform the create

#### Scenario: Snapshot restore stays on the snapshot host
- **WHEN** Fascinate creates a machine from a saved snapshot
- **THEN** the control plane SHALL dispatch the restore to the host that owns the snapshot
- **AND** it SHALL reject the request clearly if that host is not available for restore

#### Scenario: Clone uses host-local snapshot locality
- **WHEN** a user clones a machine
- **THEN** Fascinate SHALL execute the implicit snapshot and restore on the source machine's host
- **AND** the resulting clone SHALL persist that host ownership

### Requirement: Shell, routing, and diagnostics resolve through host ownership
Fascinate MUST resolve a machine's owning host before it performs shell entry, app routing resolution, or runtime diagnostics lookup, even in a single-host deployment.

#### Scenario: Shell entry resolves owning host
- **WHEN** a user requests a shell for a machine
- **THEN** Fascinate SHALL resolve the machine's owning host before it asks for a shell target
- **AND** the shell path SHALL fail explicitly if the owning host cannot serve that machine

#### Scenario: Public route resolution uses machine host ownership
- **WHEN** Fascinate resolves the backend target for a machine's public app URL
- **THEN** it SHALL use the machine's owning host as the authority for that machine's routing target
- **AND** it SHALL not assume that all machines live on the local host

### Requirement: Single-host transition preserves current external behavior
Introducing host ownership and host-aware dispatch SHALL NOT change the user-visible behavior of the current one-box deployment except for new operator-visible host metadata and diagnostics.

#### Scenario: Existing one-box flows continue to work
- **WHEN** Fascinate runs with exactly one registered host that also runs the control plane and VM runtime
- **THEN** machine create, shell entry, snapshot, restore, clone, and app routing SHALL continue to work through that host
- **AND** the user SHALL not need to know about host IDs to use the product
