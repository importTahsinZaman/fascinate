## ADDED Requirements

### Requirement: Fascinate SHALL enforce per-user aggregate machine resource budgets
Fascinate SHALL persist per-user hard limits for total CPU, total RAM, and total retained disk usage, and SHALL reject machine create, clone, or restore requests that would exceed those limits.

#### Scenario: Machine create within budget succeeds
- **WHEN** a user requests a new machine and the resulting total CPU, RAM, and retained disk usage would remain within that user's configured limits
- **THEN** Fascinate accepts the request
- **AND** the new machine's reserved resources count toward that user's usage while provisioning is in progress

#### Scenario: Machine lifecycle request exceeding budget is rejected
- **WHEN** a user requests a machine create, clone, or restore that would exceed the user's configured CPU, RAM, or retained disk limit
- **THEN** Fascinate rejects the request before starting runtime creation work
- **AND** the rejection identifies which budget dimension was exceeded

### Requirement: Fascinate SHALL keep a separate per-user machine-count cap
Fascinate SHALL enforce a per-user maximum machine count separately from aggregate CPU, RAM, and disk budgets.

#### Scenario: User remains under machine-count cap
- **WHEN** a user creates or clones a machine and the resulting machine count remains within the user's configured machine-count limit
- **THEN** Fascinate evaluates the request against the normal resource-budget and host-capacity checks

#### Scenario: User exceeds machine-count cap
- **WHEN** a user requests another machine and the resulting retained machine count would exceed the user's configured machine-count limit
- **THEN** Fascinate rejects the request before creating the machine
- **AND** the rejection explains that the machine-count cap was exceeded

### Requirement: Fascinate SHALL reserve host capacity before lifecycle work begins
Fascinate SHALL reserve host CPU, RAM, and disk capacity during machine create, clone, restore, and snapshot admission so concurrent lifecycle requests cannot over-allocate the current host.

#### Scenario: Concurrent admissions do not over-provision the host
- **WHEN** multiple users submit lifecycle requests at nearly the same time
- **THEN** Fascinate admits only the subset whose reserved resources fit within host capacity and configured safety headroom
- **AND** later requests are rejected rather than over-provisioning the host

#### Scenario: Host free-disk floor blocks lifecycle work
- **WHEN** a create, clone, restore, or snapshot request would reduce host free disk below the configured safety floor
- **THEN** Fascinate rejects that request before starting the runtime operation
- **AND** the rejection clearly indicates that host disk headroom would be violated

### Requirement: Fascinate SHALL expose per-user budget usage for diagnostics
Fascinate SHALL provide owner or operator diagnostics that report a user's configured limits and current usage for CPU, RAM, retained disk, machine count, and retained snapshots.

#### Scenario: Diagnostics show current usage versus limits
- **WHEN** an authorized caller requests per-user budget diagnostics
- **THEN** Fascinate returns the configured limits for that user
- **AND** it returns the current usage totals that the control plane is using for admission decisions
