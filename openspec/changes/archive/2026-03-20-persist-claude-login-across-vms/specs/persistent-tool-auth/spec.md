## ADDED Requirements

### Requirement: Fascinate SHALL persist tool auth by user, tool, and auth method
Fascinate SHALL model persisted agent-tool auth as a per-user profile keyed by Fascinate user identity, tool identity, and auth method identity.

#### Scenario: One user has multiple auth profiles
- **WHEN** a user uses more than one supported agent tool or more than one supported auth method
- **THEN** Fascinate stores separate auth profiles for each user/tool/auth-method combination
- **AND** one profile does not overwrite another unrelated profile

#### Scenario: Different users do not share a profile
- **WHEN** two different Fascinate users use the same supported tool and auth method
- **THEN** Fascinate stores separate auth profiles for them
- **AND** one user’s profile is never restored into the other user’s VM

### Requirement: Fascinate SHALL support multiple auth persistence modes
Fascinate SHALL support tool-auth adapters that use session-state bundles, secret-material projection, or provider-credential handling under one framework.

#### Scenario: Session-state adapter restores opaque tool state
- **WHEN** a supported tool/auth-method adapter uses session-state persistence
- **THEN** Fascinate restores the adapter’s managed guest paths as opaque state without requiring Fascinate to parse the tool’s private credential format

#### Scenario: Unsupported storage mode is not guessed
- **WHEN** a tool/auth-method combination does not have a supported Fascinate adapter
- **THEN** Fascinate does not invent a fallback persistence strategy
- **AND** the guest behaves as an ordinary logged-out tool environment for that method

### Requirement: Fascinate SHALL restore supported tool auth before a machine is ready
Fascinate SHALL restore any supported persisted tool-auth profiles for a VM owner before that machine transitions to `RUNNING`.

#### Scenario: Supported auth profile is hydrated into a new VM
- **WHEN** a user creates a new Fascinate VM and has a persisted supported auth profile for a tool available in that VM
- **THEN** Fascinate restores that auth profile into the guest before the machine is marked `RUNNING`
- **AND** the tool is available in its restored auth state when the user first enters the VM

#### Scenario: Restore failure falls back cleanly
- **WHEN** Fascinate cannot restore a stored auth profile during provisioning
- **THEN** Fascinate records the restore failure
- **AND** it falls back to a usable logged-out guest state instead of leaving the machine permanently stuck

### Requirement: Fascinate SHALL capture updated session-state auth from running VMs
For supported session-state adapters, Fascinate SHALL capture updated auth state from a running VM and replace the user’s canonical stored profile with the captured state.

#### Scenario: First login becomes available to a later VM
- **WHEN** a user logs into a supported session-state tool in one Fascinate VM and Fascinate reaches a persistence checkpoint
- **THEN** Fascinate stores the updated auth profile for that user/tool/auth-method
- **AND** a later VM for the same user restores that login state automatically

#### Scenario: Logout propagates to later VMs
- **WHEN** a user logs out of a supported session-state tool in one Fascinate VM and Fascinate captures the updated state
- **THEN** Fascinate replaces the stored auth profile with the logged-out state
- **AND** later VMs for that user no longer restore the previous login

### Requirement: Fascinate SHALL persist tool auth securely
Fascinate SHALL store persisted tool-auth profiles encrypted at rest on the host and SHALL treat them as sensitive user credential material.

#### Scenario: Stored auth profile is encrypted at rest
- **WHEN** Fascinate writes a persisted tool-auth profile to host storage
- **THEN** the stored representation is encrypted at rest
- **AND** Fascinate can decrypt it only with configured host-side key material

#### Scenario: Missing profile never falls through to another user or method
- **WHEN** Fascinate cannot find a valid persisted auth profile for a given user/tool/auth-method
- **THEN** it provisions the guest without that restored auth state
- **AND** it does not substitute a different user’s or a different method’s stored profile

### Requirement: Fascinate SHALL ship Claude subscription auth as the first adapter
The first delivered persistent-tool-auth adapter SHALL support Claude Code subscription login as a session-state profile.

#### Scenario: Claude subscription login persists across later VMs
- **WHEN** a user logs into Claude Code with their Claude subscription in one Fascinate VM and Fascinate captures that state
- **THEN** a later VM for the same user restores that Claude login automatically
- **AND** the user does not need to run Claude login again in the later VM
