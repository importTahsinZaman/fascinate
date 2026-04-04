## Why

Fascinate's current limits model is built around fixed default VM sizes, per-machine max sizes, and a per-user machine-count cap. That is too blunt for the current one-box OVH deployment: it does not express the real host capacity target, it does not bound retained snapshot growth cleanly, and it does not let one user trade a few larger VMs for many smaller ones under a single predictable budget.

We want the current single-host system to support about 10 total users with explicit headroom on the live OVH box. There are no active users and no backward-compatibility burden, so this refactor can optimize for a clean end state rather than preserving current quota behavior or persisted runtime state.

## What Changes

- Replace the current per-user machine-count quota as the primary capacity control with per-user aggregate resource budgets.
- Introduce per-user hard budgets for CPU, RAM, and disk, with the initial target sized for the current OVH box at `2 vCPU`, `8 GiB RAM`, and `50 GiB disk` per user.
- Keep a separate product cap of `25` VMs per user, but treat it as a UX/product limit rather than the main host-capacity admission control.
- Count CPU and RAM usage against running VMs, and count disk usage against all retained VM disks plus retained snapshots owned by the user.
- Add a temporary retained-snapshot policy of `5` snapshots per user, excluding transient fork snapshots used only as internal implementation details.
- Add host-level safety guardrails for create, fork, restore, and snapshot operations, including a free-disk floor and host-capacity reservation behavior that prevents over-admission on the one-box host.
- Surface the new quota model through diagnostics and operator-visible configuration so the live host can report budget usage and rejection reasons clearly.
- **BREAKING** remove the old per-user machine-count-first quota model from the current create/fork/snapshot admission path.
- **BREAKING** allow destructive rollout steps such as deleting existing VMs, snapshots, or related runtime state if that produces a cleaner implementation, since there are no active users to preserve.

## Capabilities

### New Capabilities
- `user-resource-budgets`: per-user aggregate CPU, RAM, disk, VM-count, and retained-snapshot limits for VM lifecycle admission on the current one-box deployment

### Modified Capabilities
- `vm-snapshots`: snapshot creation is no longer unbounded; retained snapshots are constrained by per-user limits and host safety checks

## Impact

- Affected code includes `internal/controlplane/`, `internal/database/`, `internal/httpapi/`, `internal/config/`, `internal/runtime/cloudhypervisor/`, and host/operator diagnostics under `ops/host/`
- The change will require persisted quota/state model changes and may remove or replace the current `MaxMachinesPerUser`-centered policy flow
- Operator configuration and diagnostics will need to describe per-user budget usage, snapshot limits, and host reservation headroom
- Existing machine and snapshot state may be intentionally discarded during rollout if that makes the new quota model materially cleaner
