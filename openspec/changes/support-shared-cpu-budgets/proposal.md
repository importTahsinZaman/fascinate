## Why

Fascinate's current hidden-sizing model still reserves full CPU for every active VM, which makes the product feel much tighter than the UX we want. On the current OVH box, that means a user hits the `2 vCPU` limit after two default VMs even though the intended experience is closer to "many dev VMs sharing one pool" with comfortable limits and healthy margins.

We want the current single-host deployment to comfortably support about `8` users with up to `5` active VMs each by making CPU a softer shared resource while keeping RAM and retained storage honest. This is the next step from hidden sizing: preserve usable developer machines, improve sellable concurrency, and avoid exposing manual VM sizing.

## What Changes

- Introduce a shared CPU budgeting model where active VMs no longer reserve their full configured CPU count against a hard per-user CPU cap.
- Keep RAM and retained storage as hard admission boundaries for create, start, fork, restore, snapshot, and host placement decisions.
- Replace the current per-user CPU budget semantics with a softer entitlement-and-fairness model sized for the current OVH host target of roughly `8` users and `5` active VMs each.
- Preserve hidden machine sizing and the existing per-user machine-count cap, but retune default machine and user budget defaults so the out-of-box economics match the new shared-pool model.
- Add operator-visible diagnostics for soft CPU entitlements, observed active VM counts, hard RAM/disk budget usage, and host-level CPU overcommit posture.
- Update browser and API rejection behavior so users are blocked for hard limits like RAM, disk, machine count, snapshot count, or host health, but not simply because they already have two `CREATING` or `RUNNING` default VMs.
- Update live validation and stress tooling to cover the new capacity target, including multi-user active-VM scenarios that would be rejected under the current hard CPU reservation model.
- **BREAKING** change per-user CPU quota behavior from hard reserved-per-active-VM accounting to a soft shared-pool policy.
- **BREAKING** change the practical meaning of "active VM capacity" on the platform from fixed per-VM CPU reservation to host-shared CPU contention under operator-defined guardrails.

## Capabilities

### New Capabilities
- `shared-cpu-budgets`: soft shared CPU entitlements for active VMs, including admission policy, defaults, fairness guardrails, and operator-visible accounting

### Modified Capabilities
- `host-aware-vm-operations`: create, start, fork, restore, stop, and delete flows now apply soft CPU policy alongside hard RAM/disk and host-capacity checks
- `platform-diagnostics`: diagnostics now report soft CPU entitlements, hard-budget usage, active VM counts, and host overcommit posture clearly
- `platform-stress-validation`: validation now covers the target capacity shape of roughly `8` users with up to `5` active VMs each on the current OVH host

## Impact

- Affected code includes `internal/controlplane/`, `internal/config/`, `internal/httpapi/`, `internal/database/`, `web/`, and operator tooling under `ops/host/`
- The change will alter default budget configuration and admission semantics for new and existing machine lifecycle flows
- Diagnostics and operator scripts will need to distinguish soft CPU policy from hard RAM/disk policy so capacity decisions are understandable during live operations
- Live-host stress and smoke tooling will need new scenarios that validate multi-user shared CPU behavior instead of the current reserved-CPU expectations
