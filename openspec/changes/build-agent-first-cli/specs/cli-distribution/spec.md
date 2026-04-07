## ADDED Requirements

### Requirement: Fascinate SHALL publish versioned CLI artifacts for supported platforms
Fascinate SHALL publish versioned CLI release artifacts for each supported OS and architecture so users do not need a source checkout to install the CLI.

#### Scenario: Latest stable CLI artifact is available
- **WHEN** a user or install script requests the latest stable Fascinate CLI release for a supported platform
- **THEN** Fascinate provides a versioned downloadable artifact for that platform
- **AND** the release metadata identifies the artifact URL, version, target platform, and checksum

#### Scenario: User pins an explicit CLI version
- **WHEN** a user or automation workflow requests a specific supported Fascinate CLI version
- **THEN** Fascinate resolves and downloads the matching versioned artifact instead of the latest release
- **AND** the install flow fails clearly if that version is unavailable for the requested platform

### Requirement: Fascinate SHALL support curl-bootstrap CLI installation
Fascinate SHALL provide a stable install script URL that downloads and installs the CLI from published release artifacts.

#### Scenario: User installs CLI via curl bootstrap
- **WHEN** a user runs the supported curl-install command for a supported platform
- **THEN** Fascinate downloads the matching CLI artifact through the install script
- **AND** the install script installs the CLI without requiring a source checkout

#### Scenario: Install script avoids host-side control-plane side effects
- **WHEN** a user installs the CLI through the curl bootstrap flow
- **THEN** the install script installs only the CLI and related completion assets
- **AND** it does not install systemd units, web assets, or control-plane host configuration as part of CLI installation

### Requirement: Fascinate SHALL verify CLI artifact integrity before installation
Fascinate SHALL verify published checksum metadata before installing a downloaded CLI artifact.

#### Scenario: Checksum verification succeeds
- **WHEN** the install script downloads a CLI artifact and its expected checksum metadata
- **THEN** the install script verifies the downloaded artifact before placing the binary into the install directory
- **AND** installation proceeds only after verification succeeds

#### Scenario: Checksum verification fails
- **WHEN** the downloaded CLI artifact does not match the expected checksum metadata
- **THEN** the install script aborts installation
- **AND** it reports the integrity failure clearly rather than installing the mismatched binary
