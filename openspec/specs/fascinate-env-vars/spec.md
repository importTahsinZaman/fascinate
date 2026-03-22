# fascinate-env-vars Specification

## Purpose
TBD - created by archiving change add-fascinate-env-vars. Update Purpose after archive.
## Requirements
### Requirement: Fascinate SHALL persist user-defined env vars as a first-class user object
Fascinate SHALL store plain environment variables as a first-class object owned by a Fascinate user, separate from guest-local files.

#### Scenario: User saves env vars centrally
- **WHEN** a user creates or updates env vars in Fascinate
- **THEN** Fascinate stores those raw key/value pairs centrally for that user
- **AND** later machines for the same user use the centrally stored env vars instead of requiring manual re-entry inside each VM

#### Scenario: User-global env vars apply to all machines
- **WHEN** a user has one or more saved Fascinate env vars
- **THEN** every machine created, restored, or cloned for that user receives those env vars
- **AND** the env vars are not shared with other users

### Requirement: Fascinate SHALL provide built-in machine env vars
Fascinate SHALL generate built-in machine-specific env vars for each machine and SHALL reserve the `FASCINATE_` prefix for those built-ins.

#### Scenario: Machine gets built-in Fascinate metadata
- **WHEN** Fascinate provisions, restores, or clones a machine
- **THEN** the machine receives built-in env vars including the machine name, public URL, primary port, base domain, host ID, and host region
- **AND** those values reflect the target machine rather than the source machine

#### Scenario: User cannot override reserved built-ins
- **WHEN** a user attempts to define an env var whose key begins with `FASCINATE_`
- **THEN** Fascinate rejects that write
- **AND** the built-in machine vars remain authoritative

### Requirement: Fascinate SHALL support deterministic env interpolation
Fascinate SHALL support `${NAME}` interpolation when rendering effective env vars for a machine.

#### Scenario: User var references a built-in Fascinate var
- **WHEN** a user sets `FRONTEND_URL=${FASCINATE_PUBLIC_URL}`
- **THEN** Fascinate renders `FRONTEND_URL` to the current machine’s public URL on create, restore, and clone

#### Scenario: Invalid interpolation is rejected
- **WHEN** a user env var references an undefined key or introduces a cycle
- **THEN** Fascinate rejects the invalid definition
- **AND** it does not write partially rendered guest env files

### Requirement: Fascinate SHALL render canonical env files inside the guest
Fascinate SHALL write the effective rendered env set to managed files under `/etc/fascinate/` and make those vars available in interactive shells.

#### Scenario: Machine receives canonical env files
- **WHEN** a machine becomes ready
- **THEN** the guest contains `/etc/fascinate/env`, `/etc/fascinate/env.sh`, and `/etc/fascinate/env.json`
- **AND** those files contain the effective rendered env vars for that machine

#### Scenario: Interactive shell sees Fascinate env vars
- **WHEN** a user or coding agent opens a shell inside the VM
- **THEN** the effective Fascinate env vars are exported for that shell session
- **AND** those vars are available without the user manually sourcing a custom file

### Requirement: Fascinate SHALL resync env files on restore and clone
Fascinate SHALL re-render and rewrite managed env files after snapshot restore or clone before the machine is considered ready.

#### Scenario: Clone refreshes machine-specific env values
- **WHEN** a user clones `source-vm` into `source-vm-v2`
- **THEN** the clone’s built-in env vars reflect `source-vm-v2`
- **AND** the clone does not keep the source machine’s `FASCINATE_PUBLIC_URL` or other machine-specific built-ins

#### Scenario: Snapshot restore refreshes managed env files
- **WHEN** a user creates a new machine from a snapshot
- **THEN** Fascinate rewrites the managed `/etc/fascinate/env*` files for the restored machine
- **AND** those files do not remain stale copies from the snapshot source

### Requirement: Fascinate SHALL expose env-var management and inspection surfaces
Fascinate SHALL expose API and SSH/frontdoor surfaces to manage user env vars and inspect effective machine env vars.

#### Scenario: User manages env vars through Fascinate
- **WHEN** a user interacts with the env-var API or SSH/frontdoor commands
- **THEN** the user can list, set, and unset their saved env vars
- **AND** those actions affect only their own env-var object

#### Scenario: Operator or user inspects effective machine env
- **WHEN** Fascinate is asked for the effective env of a specific machine
- **THEN** it returns the machine-specific rendered values, including built-ins
- **AND** the response is suitable for debugging clone and config issues

