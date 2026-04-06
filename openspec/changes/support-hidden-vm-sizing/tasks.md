## 1. Runtime And Data Model Foundations

- [x] 1.1 Add the persisted machine fields and state handling needed for hidden sizing, including explicit power states and retained machine disk-usage tracking.
- [x] 1.2 Extend the runtime and host-executor interfaces with a first-class machine stop operation that preserves retained machine state and supports later restart.
- [x] 1.3 Update Cloud Hypervisor machine metadata and lifecycle helpers so stopped machines can be restarted cleanly without being reprovisioned from scratch.

## 2. Hidden-Sizing Budget Engine

- [x] 2.1 Replace reservation-style CPU/RAM accounting with running-state compute accounting for create, start, fork, restore, stop, and delete flows.
- [x] 2.2 Change retained storage accounting to use retained machine disk usage plus retained snapshot artifact usage for per-user disk budgets and host safety checks.
- [x] 2.3 Update lifecycle admission and rejection paths so create/start/fork/restore all evaluate shared compute headroom, retained storage headroom, machine-count limits, and host capacity under the host-scoped lock.

## 3. API, Browser, And Diagnostics

- [x] 3.1 Add start/stop machine API endpoints and client methods, including clear power-state-aware rejection errors.
- [x] 3.2 Update browser machine-card UX so stopped, starting, stopping, and running machines expose only the actions valid for that power state while keeping sizing hidden.
- [x] 3.3 Update diagnostics, operator scripts, and README content to show active compute usage, retained storage usage, power-state counts, and the hidden-sizing model.

## 4. Validation

- [x] 4.1 Add backend tests for power-state transitions, start/stop admission, running-state compute accounting, retained disk accounting, and rollback on failure.
- [x] 4.2 Add browser tests for start/stop actions, shell/action gating by power state, and honest pending states without any visible size controls.
- [x] 4.3 Run local verification for backend, web, and ops changes, then validate the full flow on the live host with create, stop, start, shell, fork, snapshot, delete, and budget-edge smoke coverage.
