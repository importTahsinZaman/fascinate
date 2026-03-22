## Why

Fascinate currently behaves like a single-box product all the way through the control plane, runtime, routing, and metadata model. That is workable today, but it means adding a second VM host later would require invasive rewrites across machine ownership, shell routing, snapshot placement, and operator tooling instead of being a mechanical capacity add.

## What Changes

- Add a first-class host model to Fascinate now, even while there is still only one physical server.
- Introduce durable host records, host self-registration, health/heartbeat reporting, and operator-visible host diagnostics.
- Make machines and snapshots explicitly belong to a host, and require VM lifecycle operations to execute through a host-aware boundary instead of assuming the local runtime is the only runtime.
- Introduce a host execution abstraction that keeps the current single-box deployment working through a local host adapter, while defining the internal contract needed for future remote VM worker hosts.
- Make machine lookup, shell entry, app routing, diagnostics, and cleanup use machine host ownership instead of direct local assumptions.
- Keep tool auth, users, and other non-VM platform state centralized so later remote VM hosts can remain mostly compute/storage workers.
- **BREAKING** Replace remaining single-host assumptions in the control plane and runtime wiring with host-aware metadata and dispatch. There are no live users or compatibility constraints to preserve.

## Capabilities

### New Capabilities
- `host-registry`: First-class registered hosts with identity, region/labels, heartbeat state, capacity metadata, and operator diagnostics.
- `host-aware-vm-operations`: Machine and snapshot ownership, lifecycle dispatch, and shell/routing behavior that go through an explicit host boundary instead of assuming one local runtime.

### Modified Capabilities
- None.

## Impact

- Affected code includes the control plane, database schema, app wiring, shell frontdoor, HTTP API, runtime dispatch, diagnostics, and host bootstrap/deploy scripts.
- This change will likely add host metadata tables and host references on machine/snapshot records, plus new internal interfaces for host-scoped runtime execution.
- The current OVH box will continue to run both the control plane and VM runtime, but it will do so through the same host model that later additional VM worker boxes will use.
