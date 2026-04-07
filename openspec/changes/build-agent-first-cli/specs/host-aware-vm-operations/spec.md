## ADDED Requirements

### Requirement: Shared shell and exec operations SHALL dispatch through the owning host
Fascinate MUST resolve a machine's owning host and current power state before it creates a shared shell, attaches to a shell, or executes a non-interactive command for that machine.

#### Scenario: Shared shell creation resolves owning host
- **WHEN** a user requests a new shared shell for a machine
- **THEN** Fascinate resolves that machine's owning host before creating the backing shell session
- **AND** the shell request fails clearly if the owning host cannot serve that machine

#### Scenario: Exec rejects stopped machine clearly
- **WHEN** a user requests a non-interactive command for a machine that is not running
- **THEN** Fascinate rejects that command without attempting host execution
- **AND** the rejection explains that the machine must be running before shell or exec access is available
