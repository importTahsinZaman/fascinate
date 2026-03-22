## MODIFIED Requirements

### Requirement: Fascinate SHALL expose env-var management and inspection surfaces
Fascinate SHALL expose browser and API surfaces to manage user env vars and inspect effective machine env vars.

#### Scenario: User manages env vars through the browser command center
- **WHEN** a user interacts with env-var controls in the Fascinate web app
- **THEN** the user can list, set, edit, and unset their saved env vars
- **AND** those actions affect only their own env-var object

#### Scenario: Operator or user inspects effective machine env
- **WHEN** Fascinate is asked from the browser app or API for the effective env of a specific machine
- **THEN** it returns the machine-specific rendered values, including built-ins
- **AND** the response is suitable for debugging clone and config issues
