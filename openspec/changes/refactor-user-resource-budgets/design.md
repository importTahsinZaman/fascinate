## Context

Fascinate currently admits VM lifecycle work using three blunt controls:
- fixed default machine size (`1` CPU, `2 GiB` RAM, `20 GiB` disk)
- per-machine max size checks
- a per-user machine-count cap

That model does not map cleanly to the current one-box OVH deployment. It treats all users as if they consume capacity only through machine count, even though the real host bottlenecks are aggregate CPU, RAM, retained disk, and snapshot growth. It also relies on host heartbeat metrics for placement eligibility, which is good for diagnostics but too stale to be the only admission source for concurrent create/fork/restore work.

The live host we are sizing for today has `24` logical CPUs, `125 GiB` RAM, and about `877 GiB` of data-disk capacity. The target operating point is roughly 10 simultaneously active users with explicit headroom, using an initial per-user budget of `2 vCPU`, `8 GiB` RAM, and `50 GiB` disk. Snapshot artifacts are real retained disk-plus-memory artifacts, so they must be part of the quota model instead of remaining effectively unbounded.

Important constraints for this change:
- there are no active users, so we do not need backward-compatible quota semantics or preserved machine/snapshot state
- the system is still one-box in practice, even though the control plane already has a host model
- clone must remain snapshot-based true fork semantics
- operators need clear diagnostics for why admission was accepted or rejected

## Goals / Non-Goals

**Goals:**
- Replace the machine-count-first quota model with per-user aggregate CPU, RAM, and disk budgets.
- Keep a separate product cap of `25` VMs per user.
- Add a temporary retained-snapshot cap of `5` per user while keeping transient fork snapshots outside that count.
- Prevent host over-admission on the one-box deployment by reserving capacity during create, restore, clone, and snapshot flows.
- Make control-plane records authoritative enough that quota checks do not depend on runtime reachability or best-effort heartbeat timing.
- Allow a destructive rollout that discards existing VM/snapshot state if that yields a materially cleaner implementation.

**Non-Goals:**
- Building a full billing/plans system or self-serve quota management UI.
- Preserving existing VMs, snapshots, or quota semantics across rollout.
- Re-architecting the multi-host scheduler beyond what is necessary to make the current one-box host safe.
- Modeling exact physical qcow2 consumption for running VM disks; the quota system will use configured machine disk allocations rather than thin-provisioned host bytes.
- Replacing the temporary snapshot-count cap with a final byte-budgeted snapshot policy in this same change.

## Decisions

### 1. Persist per-user budget limits directly in the user data model

Each user will have persisted hard limits for:
- max CPU
- max RAM bytes
- max disk bytes
- max machine count
- max retained snapshot count

The initial defaults for newly created users will be:
- `2 vCPU`
- `8 GiB` RAM
- `50 GiB` disk
- `25` VMs
- `5` retained snapshots

These values will be seeded from config defaults, then stored per user so the model is genuinely per-user even if the first rollout gives everyone the same limits.

Why:
- The product requirement is per-user budgeting, not just a new set of global defaults.
- Persisting the limits on the user record keeps the first implementation simple and avoids inventing a separate quota object when there is no need for plan/version history yet.
- It gives operators a clean later path to override a single user's budget without redesigning the data model again.

Alternatives considered:
- Global config only.
  - Rejected because it does not actually implement per-user limits.
- A separate `user_quotas` table.
  - Rejected for the first pass because it adds another object boundary without enough benefit over extending the user model.

### 2. Make machine and snapshot records the quota ledger

The control plane will stop treating runtime inspection as the source of truth for user-budget accounting.

Machine records will persist normalized resource sizes:
- CPU
- RAM bytes
- disk bytes

Snapshot records already persist disk and memory artifact sizes. The new accounting rules will be:
- CPU and RAM count against `CREATING`, `RESTORING`, and `RUNNING` machines
- stopped/failed/deleting machines stop counting against CPU and RAM
- retained machine disks always count against disk budget until the machine is deleted
- retained snapshots count against disk budget using their stored artifact sizes
- snapshots in `CREATING` reserve a conservative upper bound until the actual artifact sizes are known

Why:
- Budget checks must continue working even when the runtime is temporarily unavailable.
- In-flight creates/restores need to reserve resources before they become visible as running machines.
- Snapshot artifacts already have persisted size metadata, so we should use that instead of guessing after the fact.

Alternatives considered:
- Recompute user usage from the runtime on every request.
  - Rejected because runtime availability and control-plane quota accounting should not be coupled.
- Count only physical host bytes for all VM disks.
  - Rejected because qcow2 sparsity makes that unstable as a user-facing quota signal; configured disk allocation is the clearer product contract for live VMs.

### 3. Admit lifecycle work through host-scoped reservation, not heartbeat-only placement checks

Host heartbeats will remain useful for diagnostics, but create/fork/restore/snapshot admission will no longer trust heartbeat state alone.

For the current one-box deployment, the control plane will serialize host-capacity admission behind a host-scoped lock. While the lock is held, it will:
1. load the target user's limits and current usage from persisted machine/snapshot records
2. compute current host allocations plus in-flight reservations from persisted state
3. read fresh host free-disk state from the data filesystem
4. reject if the operation would exceed user limits, host capacity, or the configured free-disk floor
5. persist the new machine/snapshot record with reserved resources before launching async runtime work

The first rollout will use a host free-disk floor sized for the current box, in the `100-150 GiB` range, to preserve operating headroom for the host OS, page cache, and snapshot/fork bursts.

Why:
- The current heartbeat model is eventually consistent and can race across multiple users.
- Persisted reservations on `CREATING`/`RESTORING` work are enough to make one-box admission safe without inventing a distributed scheduler.
- A free-disk floor is the simplest practical guardrail against snapshot-heavy disk exhaustion.

Alternatives considered:
- Keep relying on heartbeat totals and retry if a later create fails.
  - Rejected because it still allows avoidable over-admission.
- Add a new generic reservation table immediately.
  - Rejected for this change because machine/snapshot state can already carry the reservations we need.

### 4. Keep snapshot policy intentionally simple for now: byte accounting plus a vibe cap

Retained snapshots will have two policies:
- they count against the user's disk budget using stored artifact sizes
- they are capped at `5` retained snapshots per user

Explicit user snapshots count toward that cap. Transient fork snapshots do not, because clone will continue to use an internal temporary snapshot artifact that is removed after restore and never becomes a retained user snapshot object.

Snapshot creation will reserve a worst-case amount before work starts, based on the source machine's configured disk size plus memory size, then replace that reservation with the actual persisted artifact sizes after success.

Why:
- The temporary count cap is simple, understandable, and good enough while we collect real snapshot-usage data.
- Counting retained snapshot bytes keeps the cap from becoming the only control.
- Excluding transient fork snapshots preserves true-clone behavior even when a user is already at the retained snapshot cap.

Alternatives considered:
- Count limit only, with no disk accounting.
  - Rejected because five large snapshots can still consume meaningful host capacity.
- Separate live-disk and snapshot-disk budgets immediately.
  - Rejected for the first pass because a single per-user disk budget plus the temporary count cap is simpler to reason about.

### 5. Use a destructive clean-state rollout instead of backfilling old runtime state

This change will not attempt to preserve or backfill existing machine/snapshot resource state. The rollout plan can stop the service, clear machine and snapshot records, remove host runtime directories for machines and snapshots, and restart on an empty state.

Why:
- There are no active users to protect.
- Backfilling older records would add migration complexity to preserve data that does not need to survive.
- A clean-slate rollout lets the new quota ledger start from internally consistent persisted records.

Alternatives considered:
- Backfill machine resource fields from current runtime metadata and keep existing VMs.
  - Rejected because it complicates the implementation for no real user benefit.

### 6. Expose budget usage and rejection reasons through diagnostics-first surfaces

The first implementation will prioritize:
- clear error messages for rejected create/fork/restore/snapshot requests
- owner/admin diagnostics that show current usage versus limits
- host diagnostics that continue to report host totals, allocations, free disk, and placement eligibility

This change does not need a polished browser quota-management surface. Operator/diagnostic visibility is enough for the first rollout.

Why:
- The important near-term need is operational clarity, not a full admin product surface.
- Diagnostics-first visibility keeps the implementation focused on the quota engine instead of UI churn.

Alternatives considered:
- Error strings only.
  - Rejected because operators also need an inspection path for current usage and limits.
- Full browser quota CRUD in the same change.
  - Rejected because it is not required to make the host safe for the current 10-user target.

## Risks / Trade-offs

- **[Destructive rollout deletes current VM and snapshot state]** -> Accept the wipe explicitly in rollout steps and do not spend time on compatibility code for nonexistent users.
- **[Configured machine disk is more conservative than actual sparse disk bytes]** -> Use configured disk allocation for live-VM quotas on purpose, and keep actual artifact bytes only for retained snapshots.
- **[Snapshot creation reserves more disk than the final artifact may consume]** -> Reserve worst-case bytes during creation, then replace with actual persisted artifact sizes after success so temporary pessimism does not become permanent quota inflation.
- **[Host-scoped admission lock is one-box-centric]** -> Keep the design host-scoped rather than process-global so the model can later evolve to per-host admission if multi-host placement becomes active.
- **[Diagnostics and runtime accounting can diverge if lifecycle state cleanup is buggy]** -> Make machine/snapshot state transitions explicit and test admission accounting across success, failure, rollback, and delete flows.

## Migration Plan

1. Add the new user-limit fields and machine resource fields to the database schema.
2. Add the new control-plane accounting helpers for user usage, host usage, and lifecycle reservation.
3. Replace the current `MaxMachinesPerUser`-first admission path with budget-driven create/fork/restore/snapshot checks.
4. Add the retained-snapshot cap and host free-disk-floor checks.
5. Add diagnostics for per-user limits and current usage, plus clearer rejection errors.
6. During rollout, stop `fascinate`, delete existing machine/snapshot runtime state and matching database records, then restart on empty state.
7. Verify on the live OVH host that budget admission, diagnostics, clone, snapshot, and restore flows behave correctly with the new empty-state baseline.

Rollback:
- If the rollout fails before new users depend on it, revert the code and schema changes and restart with an empty runtime state.
- Because this rollout intentionally discards existing machines and snapshots, rollback is about restoring service behavior, not recovering prior user workspaces.

## Open Questions

- Should the first operator-facing budget inspection surface be a dedicated diagnostics endpoint, or should it be folded into an existing owner diagnostics response?
- Do we want the free-disk floor pinned immediately to `150 GiB`, or configurable with a default in that range?
