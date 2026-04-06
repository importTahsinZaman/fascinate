## ADDED Requirements

### Requirement: Fascinate SHALL treat per-user CPU as a soft active entitlement
Fascinate SHALL treat CPU as a shared active-resource entitlement rather than a hard reserved-per-active-VM quota. A user's active machines MUST NOT be rejected solely because their nominal active CPU demand exceeds a per-user soft CPU entitlement when hard RAM, retained-storage, machine-count, snapshot-count, and host shared CPU limits still fit.

#### Scenario: Third active default machine is allowed under soft CPU policy
- **WHEN** a user already has two active default machines and requests a third default machine
- **AND** the user's hard RAM, retained-storage, machine-count, and snapshot-count limits still fit
- **AND** the host shared CPU ceiling still has headroom
- **THEN** Fascinate admits the new machine request
- **AND** it does not reject the request only because the user already has `2` nominal active vCPU

#### Scenario: User can exceed nominal CPU entitlement while host headroom exists
- **WHEN** a user's nominal active CPU demand is already above their soft CPU entitlement
- **AND** the user's hard RAM and retained-storage limits still fit
- **AND** the host shared CPU ceiling still has headroom
- **THEN** Fascinate still admits a create or start request
- **AND** diagnostics reflect that the user is above entitlement rather than reporting a hard quota failure

### Requirement: Fascinate SHALL keep RAM and retained storage as hard limits
Fascinate SHALL continue to enforce per-user RAM and retained-storage budgets as hard admission boundaries even when CPU becomes a shared soft resource.

#### Scenario: Active RAM budget blocks another active machine
- **WHEN** creating, starting, restoring, or cloning another active machine would cause the user's active memory usage to exceed the user's hard RAM budget
- **THEN** Fascinate rejects the request before the machine becomes runnable
- **AND** the rejection identifies memory as the exhausted hard limit

#### Scenario: Retained storage budget still blocks retained artifacts
- **WHEN** creating, restoring, cloning, or snapshotting a machine would cause the user's retained machine and snapshot storage to exceed the user's hard disk budget
- **THEN** Fascinate rejects the request before the retained artifact is created
- **AND** the rejection identifies retained storage as the exhausted hard limit

### Requirement: Fascinate SHALL enforce a host-wide shared CPU ceiling
Fascinate SHALL admit active machine work against a host-wide shared CPU ceiling derived from operator-configured policy rather than against only physical CPU thread count or only per-user hard CPU sums.

#### Scenario: Host shared CPU ceiling blocks further active work
- **WHEN** creating, starting, restoring, or cloning another active machine would cause host nominal active CPU demand to exceed the host shared CPU ceiling
- **THEN** Fascinate rejects the request before the machine becomes runnable
- **AND** the rejection explains that host shared CPU capacity is exhausted

#### Scenario: Shared CPU ceiling is operator-visible
- **WHEN** an operator inspects host diagnostics
- **THEN** Fascinate exposes the host's physical CPU total, the configured shared CPU ceiling, and the current nominal active CPU demand
- **AND** the operator can determine whether the platform is within or above its intended overcommit posture

### Requirement: Fascinate SHALL tune one-box defaults for about eight users with five active default VMs each
For the current single OVH host deployment, Fascinate SHALL ship defaults that make roughly `8` concurrent users with about `5` active default VMs each a comfortable supported operating target.

#### Scenario: New user can run five default machines without a hard CPU rejection
- **WHEN** a newly created user on the default one-box configuration creates or starts up to five default machines
- **THEN** Fascinate does not reject those requests solely because of a hard per-user CPU wall
- **AND** hard RAM becomes the primary per-user active-compute guardrail

#### Scenario: Default host shared CPU ceiling matches the target shape
- **WHEN** Fascinate computes the default shared CPU ceiling for the current one-box host profile
- **THEN** that ceiling reflects an operating target of roughly forty nominal active vCPU on the live twenty-four-thread host
- **AND** the value is derived from explicit operator policy rather than implied by the old hard per-user CPU quota
