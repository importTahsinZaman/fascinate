## 1. Schema and Data Model

- [x] 1.1 Add a `hosts` persistence model with migrations for durable host identity, status, region/labels, heartbeat metadata, and capacity fields.
- [x] 1.2 Add `host_id` ownership to machine and snapshot records, including indexes and backfill logic for the single existing OVH host.
- [x] 1.3 Add store-layer APIs for host create/update/list/get and for reading/updating machine and snapshot host ownership.

## 2. Host Registration and Health

- [x] 2.1 Add startup self-registration for the current OVH box so Fascinate can create or refresh its own host record on boot.
- [x] 2.2 Add a heartbeat/capacity reporting loop that updates the local host's health, runtime version, and advertised capacity/usage.
- [x] 2.3 Add operator-visible host diagnostics through HTTP/admin tooling so registered hosts and placement eligibility can be inspected.

## 3. Host Execution Boundary

- [x] 3.1 Introduce a host-aware executor interface for machine, snapshot, and diagnostics operations instead of calling the runtime directly from the control plane.
- [x] 3.2 Implement the initial local-host adapter that wraps the current Cloud Hypervisor runtime and satisfies the new host executor interface.
- [x] 3.3 Add a scheduler/placement abstraction that selects an eligible host for base-image create and enforces snapshot locality for restore and clone.

## 4. Host-Aware VM and Snapshot Flows

- [x] 4.1 Update create, delete, snapshot, restore, and clone flows so machine/snapshot ownership is persisted and every runtime action dispatches through the owning host.
- [x] 4.2 Update shell entry, machine diagnostics, snapshot diagnostics, and app-target resolution to resolve the owning host before accessing runtime state.
- [x] 4.3 Ensure snapshot ownership, clone ownership, and restore ownership remain host-local and fail clearly when the required host is unavailable or ineligible.

## 5. Bootstrap, Migration, and Validation

- [x] 5.1 Update host bootstrap/deploy config so the local host's durable identity and role/capabilities are explicit and survive redeploys.
- [x] 5.2 Add targeted tests for host registration, placement eligibility, host ownership persistence, and host-aware dispatch/routing behavior in the single-host deployment.
- [x] 5.3 Re-run `go test ./...`, `make verify-ops`, and the live smoke/stress harnesses on the one-box deployment to prove current external behavior is preserved after the host-model transition.
- [x] 5.4 Update README, AGENTS.md, and any relevant operator docs to describe the new first-class host model and the single-host-to-multi-host migration path.
