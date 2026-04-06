## ADDED Requirements

### Requirement: Fascinate SHALL expose soft-CPU and hard-budget posture distinctly
Fascinate SHALL provide diagnostics that clearly distinguish soft shared CPU posture from hard RAM, retained-storage, machine-count, and snapshot-count limits for both users and hosts.

#### Scenario: User diagnostics show entitlement versus hard limits
- **WHEN** an authorized caller requests per-user budget diagnostics
- **THEN** Fascinate returns the user's soft CPU entitlement, nominal active CPU demand, hard RAM budget and active RAM usage, retained-storage usage, machine count, and snapshot count
- **AND** the response distinguishes advisory CPU posture from hard limit exhaustion

#### Scenario: Host diagnostics show shared CPU overcommit posture
- **WHEN** an operator requests host diagnostics
- **THEN** Fascinate returns physical CPU total, configured shared CPU ceiling, current nominal active CPU demand, and remaining shared CPU headroom
- **AND** the response makes clear that CPU is a shared pool rather than a strict per-user reserved quota

### Requirement: Fascinate SHALL expose clear admission-failure diagnostics for the new policy
Fascinate SHALL expose admission-failure information that tells operators and users whether a request was blocked by host shared CPU pressure or by a hard RAM, retained-storage, machine-count, snapshot-count, or host-health limit.

#### Scenario: CPU rejection is reported as shared host capacity
- **WHEN** a machine create, start, clone, or restore request is rejected because host shared CPU capacity is exhausted
- **THEN** Fascinate surfaces a rejection reason that identifies shared host CPU pressure
- **AND** it does not describe the failure as a hard per-user CPU quota overrun

#### Scenario: Hard-limit rejection remains explicit
- **WHEN** a machine create, start, clone, restore, or snapshot request is rejected because of hard RAM, retained storage, machine count, or snapshot count
- **THEN** Fascinate surfaces the specific hard limit that was exceeded
- **AND** diagnostics do not conflate that rejection with the soft CPU entitlement model
