## ADDED Requirements

### Requirement: Fascinate SHALL provide an authenticated browser command center
Fascinate SHALL provide a browser application on the primary Fascinate product origin where a user can sign in, view their machines and snapshots, and reach the main workspace without depending on SSH or the Bubble Tea dashboard.

#### Scenario: User signs into the browser app
- **WHEN** a user visits the Fascinate web app and completes the supported browser login flow
- **THEN** Fascinate creates a browser-authenticated session for that user
- **AND** the user reaches the browser command center without needing terminal-first setup steps

#### Scenario: Browser session gates product access
- **WHEN** an unauthenticated browser requests machine, snapshot, env-var, or workspace pages
- **THEN** Fascinate does not expose those user-scoped surfaces
- **AND** it redirects or prompts the browser into the login flow

### Requirement: Fascinate SHALL authenticate the browser app with email-code login and web sessions
Fascinate SHALL authenticate browser users through emailed verification codes and persisted web sessions, without requiring SSH-key registration as part of browser login.

#### Scenario: Browser login can create a user without an SSH key
- **WHEN** a browser user completes email-code verification for an email address that does not yet have a Fascinate user
- **THEN** Fascinate creates the user record
- **AND** it creates a browser-authenticated session for that user
- **AND** it does not require the user to register an SSH key to enter the browser app

#### Scenario: Browser session is revocable durable server-side state
- **WHEN** Fascinate creates a browser-authenticated session
- **THEN** the session is represented as server-side durable session state
- **AND** the browser uses a session cookie to present that session on later requests

### Requirement: Fascinate SHALL let users manage machines from the browser app
Fascinate SHALL let a user create, inspect, clone, and delete their machines from the browser command center.

#### Scenario: User creates a machine from the browser
- **WHEN** a user submits a machine-create action in the web app
- **THEN** Fascinate creates the machine for that user using the existing control-plane lifecycle
- **AND** the web app shows the machine's provisioning and readiness state

#### Scenario: User clones or deletes a machine from the browser
- **WHEN** a user clones or deletes one of their machines from the web app
- **THEN** Fascinate performs the corresponding machine lifecycle action
- **AND** the browser reflects the resulting machine state change

### Requirement: Fascinate SHALL let users manage snapshots from the browser app
Fascinate SHALL let a user list, create, restore from, and delete their saved snapshots from the browser command center.

#### Scenario: User creates and later restores a snapshot from the browser
- **WHEN** a user creates a snapshot and then requests a new machine from that snapshot in the web app
- **THEN** Fascinate performs the saved-snapshot and restore flows for that user
- **AND** the browser shows both the saved snapshot and the resulting machine lifecycle state

#### Scenario: User deletes a snapshot from the browser
- **WHEN** a user deletes one of their saved snapshots from the web app
- **THEN** Fascinate removes that snapshot for that user
- **AND** the browser no longer lists it as available for restore

### Requirement: Fascinate SHALL let users manage env vars from the browser app
Fascinate SHALL let a user list, create, update, delete, and inspect their Fascinate env vars from the browser command center.

#### Scenario: User edits env vars from the browser
- **WHEN** a user creates or updates a saved env var in the web app
- **THEN** Fascinate stores the env-var change for that user
- **AND** the browser shows the saved key and effective raw value

#### Scenario: User inspects effective machine env from the browser
- **WHEN** a user requests env details for one of their machines in the web app
- **THEN** Fascinate returns the rendered machine-specific env set including built-in `FASCINATE_*` vars
- **AND** the browser can display those rendered values for debugging machine identity and clone behavior
