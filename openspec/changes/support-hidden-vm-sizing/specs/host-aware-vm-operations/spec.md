## MODIFIED Requirements

### Requirement: Runtime operations dispatch through a host boundary
The control plane MUST execute machine and snapshot runtime operations through a host-aware execution boundary rather than assuming the local runtime is globally authoritative. This includes create, start, stop, delete, fork, snapshot, and restore operations.

#### Scenario: Create uses the owning host executor
- **WHEN** Fascinate provisions a new machine
- **THEN** the control plane SHALL resolve an owning host first
- **AND** it SHALL invoke that host's runtime executor to perform the create

#### Scenario: Stopped machine start uses the owning host executor
- **WHEN** a user starts a stopped machine
- **THEN** the control plane SHALL dispatch that start to the machine's owning host
- **AND** it SHALL reject the request clearly if that host cannot start the machine

#### Scenario: Stop uses the owning host executor
- **WHEN** a user stops a running machine
- **THEN** the control plane SHALL dispatch that stop to the machine's owning host
- **AND** it SHALL preserve the machine's host ownership for later restart

#### Scenario: Snapshot restore stays on the snapshot host
- **WHEN** Fascinate creates a machine from a saved snapshot
- **THEN** the control plane SHALL dispatch the restore to the host that owns the snapshot
- **AND** it SHALL reject the request clearly if that host is not available for restore

#### Scenario: Clone uses host-local snapshot locality
- **WHEN** a user clones one of their VMs
- **THEN** Fascinate SHALL execute the implicit snapshot and restore on the source machine's host
- **AND** the resulting clone SHALL persist that host ownership

### Requirement: Shell, routing, and diagnostics resolve through host ownership
Fascinate MUST resolve a machine's owning host and current power state before it performs shell entry, app routing resolution, or runtime diagnostics lookup, even in a single-host deployment.

#### Scenario: Shell entry resolves owning host for a running machine
- **WHEN** a user requests a shell for a running machine
- **THEN** Fascinate SHALL resolve the machine's owning host before it asks for a shell target
- **AND** the shell path SHALL fail explicitly if the owning host cannot serve that machine

#### Scenario: Stopped machine shell request is rejected clearly
- **WHEN** a user requests a shell for a stopped machine
- **THEN** Fascinate rejects the request
- **AND** the rejection explains that the machine must be started before shell entry is available

#### Scenario: Public route resolution uses machine host ownership only for running machines
- **WHEN** Fascinate resolves the backend target for a machine's public app URL
- **THEN** it SHALL use the machine's owning host as the authority for that machine's routing target
- **AND** it SHALL not treat a stopped machine as route-ready
