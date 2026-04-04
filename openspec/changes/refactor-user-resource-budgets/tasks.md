## 1. Schema And Rollout Baseline

- [x] 1.1 Add database schema support for persisted per-user limits and persisted machine resource sizes needed for quota accounting.
- [x] 1.2 Add config defaults for initial per-user budgets, per-user machine/snapshot caps, and the host free-disk floor.
- [x] 1.3 Define and script the destructive rollout baseline for the one-box host, including clearing existing machine and snapshot runtime state and matching database records.

## 2. Quota Ledger And Admission Engine

- [x] 2.1 Implement control-plane helpers that compute per-user usage from persisted machine and snapshot records, including in-flight reservations.
- [x] 2.2 Replace the current machine-count-first create admission path with per-user CPU, RAM, disk, and machine-count budget checks plus host-capacity reservation.
- [x] 2.3 Apply the same budget and reservation checks to clone and restore flows so all machine-producing paths share one admission model.
- [x] 2.4 Update snapshot creation to enforce retained-snapshot count limits, reserve worst-case bytes during creation, and replace reservations with actual persisted artifact sizes on success.

## 3. Diagnostics And Policy Surfaces

- [x] 3.1 Add owner or operator diagnostics that report configured per-user limits and current usage for CPU, RAM, disk, machine count, and retained snapshots.
- [x] 3.2 Update lifecycle rejection errors and event payloads so quota and host-capacity failures clearly identify the violated limit.
- [x] 3.3 Remove or downgrade the old `MaxMachinesPerUser`-centered policy flow so the new budget model is the primary capacity control.

## 4. Validation

- [x] 4.1 Add or update tests for create, clone, restore, snapshot, delete, and failure/rollback paths under the new budget ledger.
- [x] 4.2 Add tests covering concurrent admissions so one-box host capacity cannot be over-reserved across multiple users.
- [x] 4.3 Update README and relevant operator docs to describe the new per-user budget model, temporary snapshot cap, and destructive rollout assumption.
- [x] 4.4 Validate the refactor on the live OVH host after the clean-state rollout, including budget diagnostics, machine creation, clone, snapshot save, and snapshot restore.
