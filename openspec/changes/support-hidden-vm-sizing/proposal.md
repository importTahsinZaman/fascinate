## Why

Fascinate's current budget model still feels like fixed-size VMs even after the per-user resource-budget refactor. A user can hit the `2 vCPU` limit after two default machines because every `RUNNING` VM reserves its full stored CPU/RAM shape, but the product UX we want is closer to "up to 25 VMs that share one pool" without exposing a size picker.

We want a hidden-sizing model where users can accumulate many machines, active machines consume the shared compute budget, retained machines continue to count against disk, and the system stays honest about readiness and capacity without adding a "boost" or advanced sizing workflow.

## What Changes

- Replace reservation-style per-VM compute admission with a hidden-sizing model where CPU and RAM are charged against the user's shared budget only while a machine is actively running.
- Introduce first-class machine power states so users can stop and start retained VMs instead of deleting them to free shared compute budget.
- Keep the separate product cap of `25` VMs per user, but make it meaningful by allowing many retained machines to coexist under one shared compute pool.
- Preserve a hidden baseline machine shape for create flows and keep sizing out of the normal UI; do not add a user-facing size picker or any boost mode.
- Update create, start, fork, and restore flows so hidden machine sizing and shared-budget admission are consistent across all machine-producing lifecycle paths.
- Keep disk and snapshot limits explicit, but align machine-retention and disk-accounting policy with the hidden-sizing UX so retained stopped VMs remain practical.
- Update browser and operator surfaces to show honest machine power state, shared-budget usage, and rejection reasons when the user's pool is exhausted.
- **BREAKING** change the current assumption that a created machine always consumes its full CPU/RAM budget until it is deleted.
- **BREAKING** add explicit stopped-machine lifecycle semantics that affect shell availability, machine actions, and quota accounting.

## Capabilities

### New Capabilities
- `hidden-vm-sizing`: hidden baseline VM sizing, shared-pool compute budgeting, and machine lifecycle behavior without a user-facing size picker
- `vm-power-states`: user-visible stop/start lifecycle for retained machines and power-state-aware UI/availability rules

### Modified Capabilities
- `host-aware-vm-operations`: machine create, start, fork, restore, and host admission rules now respect hidden sizing and stopped-machine semantics
- `platform-diagnostics`: diagnostics must report power-state-aware budget usage, retained disk usage, and admission failures under the hidden-sizing model

## Impact

- Affected code includes `internal/controlplane/`, `internal/runtime/cloudhypervisor/`, `internal/httpapi/`, `internal/database/`, `internal/config/`, `web/`, and operator tooling under `ops/host/`
- The change will introduce persisted machine power-state semantics and new API/UI behavior for starting and stopping machines
- Existing budget logic, host-admission checks, and diagnostics will need to move from reservation-style compute accounting to running-state shared-pool compute accounting
- Browser UX will need to distinguish `RUNNING`, `STOPPED`, `CREATING`, and `DELETING` behavior without reintroducing a manual machine-size workflow
