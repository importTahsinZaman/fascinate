# host-registry Specification

## Purpose
TBD - created by archiving change introduce-host-model. Update Purpose after archive.
## Requirements
### Requirement: Hosts are first-class registered resources
The control plane SHALL maintain first-class host records for every VM-capable Fascinate server, even when only one host exists. A host record MUST include stable identity, lifecycle status, role or capabilities, region, heartbeat freshness, and operator-visible capacity metadata.

#### Scenario: Local host self-registers on startup
- **WHEN** the current OVH box boots the Fascinate control plane with no existing record for itself
- **THEN** Fascinate SHALL create a host record for that box
- **AND** the host SHALL become eligible for VM placement once its local runtime is healthy

#### Scenario: Existing host refreshes its heartbeat
- **WHEN** a registered host reports health and capacity successfully
- **THEN** Fascinate SHALL update that host's heartbeat timestamp and operator-visible capacity metadata
- **AND** the host SHALL remain in an eligible state unless it is explicitly drained or unhealthy

### Requirement: Host health gates placement eligibility
The control plane SHALL treat host availability as an explicit placement concern. Hosts that are missing, unhealthy, or draining MUST NOT receive new machine placements.

#### Scenario: Single healthy host remains eligible
- **WHEN** exactly one registered host is active and healthy
- **THEN** create requests for new base-image machines SHALL place onto that host

#### Scenario: Draining host is excluded from new placement
- **WHEN** a host is marked draining
- **THEN** Fascinate SHALL exclude it from new machine placement
- **AND** Fascinate SHALL keep the host record visible to operators for existing machine ownership and diagnostics

### Requirement: Operators can inspect host registry state
Fascinate SHALL expose operator-visible host diagnostics so a human can determine which hosts exist, whether they are healthy, and what capacity they are advertising.

#### Scenario: Operator lists hosts
- **WHEN** an operator requests host diagnostics
- **THEN** Fascinate SHALL return the known hosts with identity, status, heartbeat freshness, and capacity metadata
- **AND** the response SHALL distinguish placement eligibility from mere registration

