## ADDED Requirements

### Requirement: Fascinate SHALL boot fresh machines from the promoted Fascinate image
When a user creates a fresh machine without restoring a saved snapshot, Fascinate SHALL boot that machine from the currently promoted platform-managed Fascinate image rather than relying on a guest-specific provisioning image path.

#### Scenario: Fresh machine create uses the promoted image
- **WHEN** an authenticated user creates a new machine without specifying a snapshot
- **THEN** Fascinate boots that machine from the currently promoted Fascinate image artifact
- **AND** the machine still receives the configured default CPU, memory, disk, and primary port values

#### Scenario: Snapshot restore remains a separate flow
- **WHEN** an authenticated user creates a machine from a saved snapshot
- **THEN** Fascinate uses the snapshot restore flow for that machine
- **AND** it does not reinterpret snapshot restore as an ordinary fresh-machine boot

### Requirement: Fascinate SHALL keep fresh-machine boot independent of live toolchain installation
Fresh-machine readiness SHALL not depend on downloading or installing the default Fascinate toolchain during per-machine guest boot.

#### Scenario: Default toolchain is available when the fresh machine becomes ready
- **WHEN** a fresh machine reaches `RUNNING`
- **THEN** the default Fascinate guest toolchain is already present in that machine
- **AND** the user does not need to wait for per-machine package installation before using the guest

#### Scenario: Fresh machine create does not require package registries during guest boot
- **WHEN** the host already has a promoted Fascinate image available for fresh machine creation
- **THEN** a fresh machine can become ready without guest-boot package downloads from external registries
- **AND** default tool availability does not depend on live package installation during that boot

### Requirement: Fascinate SHALL limit fresh-machine boot work to machine-specific finalization
For fresh machines booted from the promoted Fascinate image, guest-boot setup SHALL be limited to machine-specific finalization rather than shared toolchain provisioning.

#### Scenario: Boot-time finalization applies machine-specific state
- **WHEN** a fresh machine boots from the promoted Fascinate image
- **THEN** Fascinate applies that machine's hostname, network configuration, managed env files, and injected guest instructions
- **AND** those machine-specific values reflect the created machine rather than the published image source

#### Scenario: Fresh machine instructions and env reflect the created machine
- **WHEN** the user first enters a fresh machine
- **THEN** the guest's injected `AGENTS.md` and managed Fascinate env files describe that created machine's identity
- **AND** they do not describe the builder VM or any previously published image instance

