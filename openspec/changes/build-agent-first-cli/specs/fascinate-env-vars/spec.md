## MODIFIED Requirements

### Requirement: Fascinate SHALL expose env-var management and inspection surfaces
Fascinate SHALL expose API, web, and CLI surfaces to manage user env vars and inspect effective machine env vars.

#### Scenario: User manages env vars through Fascinate
- **WHEN** a user interacts with the env-var API, web app, or CLI commands
- **THEN** the user can list, set, and unset their saved env vars
- **AND** those actions affect only their own env-var object

#### Scenario: Operator or user inspects effective machine env
- **WHEN** Fascinate is asked through the API, web app, or CLI for the effective env of a specific machine
- **THEN** it returns the machine-specific rendered values, including built-ins
- **AND** the response is suitable for debugging clone and config issues as well as automation
