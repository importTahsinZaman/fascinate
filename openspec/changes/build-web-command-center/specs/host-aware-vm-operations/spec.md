## MODIFIED Requirements

### Requirement: Shell, routing, and diagnostics resolve through host ownership
Fascinate MUST resolve a machine's owning host before it performs browser terminal session issuance, app routing resolution, or runtime diagnostics lookup, even in a single-host deployment.

#### Scenario: Browser terminal session resolves owning host
- **WHEN** a user requests a browser terminal session for a machine
- **THEN** Fascinate SHALL resolve the machine's owning host before it issues terminal attach details
- **AND** the terminal session path SHALL fail explicitly if the owning host cannot serve that machine

#### Scenario: Public route resolution uses machine host ownership
- **WHEN** Fascinate resolves the backend target for a machine's public app URL
- **THEN** it SHALL use the machine's owning host as the authority for that machine's routing target
- **AND** it SHALL not assume that all machines live on the local host
