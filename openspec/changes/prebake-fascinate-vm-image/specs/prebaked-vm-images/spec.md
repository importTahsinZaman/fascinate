## ADDED Requirements

### Requirement: Fascinate SHALL build versioned guest images with the default toolchain preinstalled
Fascinate SHALL produce platform-managed, versioned VM image artifacts that already contain the default Fascinate guest toolchain before any user machine is created.

#### Scenario: Image build produces a versioned Fascinate guest artifact
- **WHEN** an operator builds a new Fascinate guest image
- **THEN** the build outputs a versioned image artifact suitable for fresh machine creation
- **AND** the artifact includes the default Fascinate toolchain required for a standard guest environment

#### Scenario: Image build records guest image contents
- **WHEN** Fascinate completes a guest image build
- **THEN** it writes image metadata that identifies the built image version and the resolved toolchain versions
- **AND** operators can inspect that metadata to understand what the published image contains

### Requirement: Fascinate SHALL seal published images for fresh-machine reuse
Before an image artifact is promoted for fresh machine creation, Fascinate SHALL scrub machine-specific and user-specific runtime identity from that artifact so it can be safely reused as a fresh machine image.

#### Scenario: Published image does not retain builder machine identity
- **WHEN** Fascinate seals a candidate image for publication
- **THEN** the published image does not retain the builder VM's machine identity material
- **AND** a later fresh machine can boot from that image as a new machine rather than as a resumed prior machine

#### Scenario: Published image does not ship persisted tool auth
- **WHEN** Fascinate publishes a guest image
- **THEN** the image does not contain persisted Claude, Codex, GitHub CLI, or related user auth/session state
- **AND** a fresh machine created from that image starts without inherited user credential material

### Requirement: Fascinate SHALL validate candidate images before promotion
Fascinate SHALL require a boot validation pass for each candidate image before that image becomes the promoted default for fresh machine creation.

#### Scenario: Validated candidate becomes the promoted image
- **WHEN** a candidate image boots successfully in validation and satisfies the expected guest readiness checks
- **THEN** Fascinate may promote that image for fresh machine creation
- **AND** later fresh machines use the promoted image instead of the prior one

#### Scenario: Failed candidate is not promoted
- **WHEN** a candidate image fails validation
- **THEN** Fascinate does not promote that image for fresh machine creation
- **AND** the current promoted image remains the active source for new machines

### Requirement: Fascinate SHALL support rollback by switching promoted image versions
Fascinate SHALL be able to roll back fresh machine creation to a previously promoted image version without restoring the removed first-boot toolchain installer path.

#### Scenario: Rollback restores the previous promoted image
- **WHEN** operators roll back a bad promoted image
- **THEN** Fascinate switches fresh machine creation back to a previously promoted image artifact
- **AND** new machines resume creating from that earlier image version

