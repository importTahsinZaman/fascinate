## ADDED Requirements

### Requirement: Fascinate SHALL persist shells as backend-owned shared resources
Fascinate SHALL model a shell as a durable user-owned resource backed by a persistent in-guest shell session rather than as a browser-local attachment object.

#### Scenario: CLI-created shell becomes a shared resource
- **WHEN** a user creates a shell for a running machine from the CLI
- **THEN** Fascinate persists a shell record owned by that user for that machine
- **AND** it creates the backing in-guest shell session so later clients can attach to the same shell

#### Scenario: Shell survives client disconnect
- **WHEN** every active client disconnects from a shell without deleting it
- **THEN** Fascinate keeps the backing shell resource available for later reattachment
- **AND** later clients can discover and reattach to that shell without creating a new one

### Requirement: Fascinate SHALL synchronize shell lifecycle across CLI and web surfaces
Fascinate SHALL propagate shell lifecycle changes to all subscribed product surfaces quickly enough that CLI and web clients observe one shared shell inventory.

#### Scenario: Web sees shell created from the CLI
- **WHEN** a user creates a shell from the CLI while the web app is open for the same user
- **THEN** Fascinate publishes that shell creation to the web app without requiring manual refresh
- **AND** the web app can attach to the newly created shell as a shared resource

#### Scenario: CLI sees shell deleted from the web app
- **WHEN** a user deletes a shell from the web app while a CLI shell list or watch is active
- **THEN** Fascinate publishes that shell deletion to the CLI without requiring manual refresh
- **AND** the CLI no longer reports that shell as available

### Requirement: Fascinate SHALL support concurrent shared attachments to one shell
Fascinate SHALL allow more than one client attachment to the same shell at the same time so CLI and web clients can observe and interact with one shared shell session.

#### Scenario: CLI and web attach to the same shell
- **WHEN** a shell already attached in the web app is attached again from the CLI
- **THEN** both attachments connect to the same backing shell
- **AND** output produced in that shell becomes visible to both clients

#### Scenario: Deleting a shell disconnects all attachments
- **WHEN** a user deletes a shared shell that still has one or more active attachments
- **THEN** Fascinate terminates the backing shell session
- **AND** all active attachments are closed with a clear shell-deleted reason

### Requirement: Fascinate SHALL support non-interactive shell input and recent-line inspection
Fascinate SHALL let authorized clients send input to a shell and inspect recent shell lines without requiring an interactive terminal attachment.

#### Scenario: User sends a command into an existing shell
- **WHEN** a user invokes a supported CLI action to send command text to an existing shell
- **THEN** Fascinate delivers that input to the backing shell session
- **AND** later line inspection or live attachments show the resulting output

#### Scenario: User reads recent shell lines
- **WHEN** a user requests recent output from an existing shell without opening an interactive attachment
- **THEN** Fascinate returns recent shell lines from that shell's persistent history
- **AND** the response is usable for debugging, automation, or deciding whether an interactive attach is needed
