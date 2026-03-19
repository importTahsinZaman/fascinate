## ADDED Requirements

### Requirement: Fascinate SHALL open machine shell sessions through the VM guest
Fascinate SHALL allow an authenticated machine owner to open an interactive shell session into a VM-backed machine through the existing SSH frontdoor without relying on host-side container exec.

#### Scenario: Opening a machine shell reaches the guest
- **WHEN** an authenticated owner opens a shell for a running machine from the dashboard or `shell <name>`
- **THEN** Fascinate connects the session to the guest operating system of that machine
- **AND** terminal resizing and interactive input continue to work for the session

#### Scenario: Shell access is denied for non-owners
- **WHEN** a user attempts to open a shell for a machine they do not own
- **THEN** Fascinate rejects the request and does not connect them to the guest

### Requirement: Fascinate SHALL run tutorial sessions inside the VM guest
Fascinate SHALL launch the tutorial flow inside the guest operating system of a VM-backed machine so the tutorial uses the same tools and network path as ordinary user work.

#### Scenario: Tutorial launches Claude inside the guest
- **WHEN** an eligible user starts the tutorial for a machine
- **THEN** Fascinate opens an interactive guest session and launches the tutorial command inside that machine

#### Scenario: Tutorial completion is preserved
- **WHEN** a tutorial session exits successfully or the user creates an additional machine
- **THEN** Fascinate records tutorial completion for that user so the tutorial prompt is no longer shown as a first-machine action

### Requirement: Fascinate SHALL surface guest-session failures as machine access errors
If Fascinate cannot reach the guest shell transport for a machine, it SHALL fail the session cleanly and report that the machine is unavailable instead of hanging the user’s SSH session.

#### Scenario: Guest is unavailable for shell access
- **WHEN** a shell or tutorial session is requested for a machine whose guest transport cannot be reached
- **THEN** Fascinate returns an error to the user
- **AND** the frontdoor session remains usable after the failed attempt
