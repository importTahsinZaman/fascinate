## Context

Fascinate currently runs as one co-located system: the control plane, SSH frontdoor, HTTP API, routing logic, metadata store, and Cloud Hypervisor runtime all assume they live on the same box and can call one another directly. That assumption leaks through the entire product surface: machine records do not have an owning host, snapshots are implicitly local, shell entry assumes the local runtime owns the target VM, and app routing assumes every machine lives behind the same local forwarders.

That is the wrong place to wait for a second server. If Fascinate adds a new VM host later without introducing a host model first, the follow-on work becomes a cross-cutting migration under live load: every create/delete/clone/snapshot path, every diagnostic, every shell handoff, and every route lookup must change at the same time. There are no users or compatibility constraints right now, so this is the right moment to split the architecture conceptually while still deploying on one physical box.

## Goals / Non-Goals

**Goals:**
- Introduce hosts as first-class entities in the control plane, with durable identity, role, region, advertised capabilities, health state, and capacity metadata.
- Make machines and snapshots explicitly belong to a host.
- Introduce a host-aware execution boundary so control-plane code asks a host-scoped executor to perform runtime work instead of assuming a single local runtime.
- Keep the current OVH server working as a combined control-plane host and VM host, but do so through the same host model that future VM worker hosts will use.
- Keep users, tool auth, and other non-VM platform state centralized so later VM worker boxes can remain focused on compute/storage.
- Make future additions straightforward: add a host record and host agent, then let placement choose it.

**Non-Goals:**
- Full remote-worker support in this change. The initial implementation only needs a local host adapter and host-aware internal boundaries.
- User-visible region selection in the product UI or API.
- Cross-host snapshot movement or snapshot replication.
- Moving the control plane off the current OVH machine in this change.

## Decisions

### 1. Add a durable host registry now, even with one host

The control plane will add first-class host records with fields for:
- stable host ID
- human/operator name
- role (`combined`, later `worker` or `control-plane`)
- region and labels
- lifecycle status (`ACTIVE`, `DRAINING`, `UNHEALTHY`, etc.)
- advertised runtime version and capabilities
- capacity and usage metrics
- last heartbeat timestamp

The current OVH box will self-register as the first host during startup/bootstrap.

Why:
- It lets the rest of the product refer to an owning host instead of “the runtime on this process.”
- It makes adding another box later a registration problem, not a schema redesign.

Alternative considered:
- Delay host records until there are multiple hosts.
  - Rejected because every later machine/snapshot migration would have to be backfilled while the rest of the product is already relying on single-host assumptions.

### 2. Introduce a host executor boundary, but implement only a local adapter now

The control plane will stop calling the VM runtime directly for lifecycle work. Instead, it will call a host-aware executor interface keyed by host ID. In this change there will be exactly one concrete implementation:
- a local host adapter that wraps the existing Cloud Hypervisor runtime on the current box

The interface must cover at least:
- machine lifecycle
- snapshot lifecycle
- machine/snapshot diagnostics
- shell/session target resolution
- host capacity and health reporting

Why:
- This is the seam that later remote VM worker hosts will plug into.
- A local adapter preserves today’s behavior without adding remote RPC complexity prematurely.

Alternative considered:
- Add host IDs in the DB but keep runtime calls directly wired to the local runtime.
  - Rejected because it preserves the hardest part of the later migration: the control plane would still be architecturally local-only.

### 3. Machines and snapshots become explicitly host-owned

Machine and snapshot records will gain `host_id`. For the single-host transition:
- brand new machines are placed onto the one active local host
- snapshots inherit the host of their source machine
- create-from-snapshot and clone must execute on the snapshot’s owning host

Why:
- Snapshot locality is fundamental. A snapshot artifact exists on a specific VM host.
- Later scheduling decisions need a concrete owner for every runtime-bound resource.

Alternative considered:
- Only store `host_id` on machines and infer snapshot ownership indirectly.
  - Rejected because snapshots are first-class artifacts and will later need their own locality and capacity decisions.

### 4. Placement becomes an explicit control-plane concern immediately

Even though there is only one host now, create and clone flows will go through a scheduler/placer abstraction. The initial policy is simple:
- choose the only active eligible host for brand new machines
- require snapshot restore and clone to run on the host that owns the source snapshot

If there is no eligible host, create fails clearly.

Why:
- This turns “placement” into a small, isolated policy layer instead of something spread across handlers and runtime methods.
- It allows later region-aware and capacity-aware placement to extend the same path.

Alternative considered:
- Keep direct single-host placement now and add a scheduler later.
  - Rejected because the point of this change is to convert placement from an assumption into an explicit decision.

### 5. Keep the control plane central; keep VM state host-local

This change will formalize the long-term split:
- centralized:
  - users
  - tool auth
  - machine metadata
  - snapshot metadata
  - scheduler/placement
  - frontdoor authentication
  - host inventory and diagnostics
- host-local:
  - VM disks
  - VM processes
  - snapshot artifacts
  - host-side forwarders
  - runtime logs

Why:
- It matches the product’s likely future: one smaller control-plane box and multiple larger VM workers.
- It avoids trying to centralize high-churn VM artifacts too early.

Alternative considered:
- Move snapshot and VM artifacts into centralized shared storage immediately.
  - Rejected because it introduces a much larger storage and replication problem before host-local execution is even abstracted correctly.

### 6. Host-aware routing and shell resolution must exist now, even if the answer is “local host”

Machine lookup, shell entry, diagnostics, and public route resolution will use `host_id` to resolve the owning host before they dispatch to runtime state. In the initial implementation, resolution lands back on the local host adapter, but the call path must no longer assume “local by default.”

Why:
- Shell entry and public routing are the most user-visible paths that would otherwise remain single-host hardcoded.
- This makes later remote forwarding a contained change, not a redesign of the UX entrypoints.

Alternative considered:
- Limit host-awareness to create/delete first and defer shell/routing.
  - Rejected because that leaves the most important external surfaces coupled to one box.

## Risks / Trade-offs

- **[Higher internal complexity now]** → The product will carry host abstractions before there is a second host.
  - **Mitigation:** Keep the first version narrow: one host registry, one local adapter, one simple scheduler.

- **[Schema churn across many core tables]** → Adding `host_id` touches machines, snapshots, diagnostics, and migrations.
  - **Mitigation:** Do the transition while there are no users; backfill all existing rows to the single registered host in one migration.

- **[Partial abstraction risk]** → If any direct local-runtime shortcuts remain, later remote-host support will still be painful.
  - **Mitigation:** Make host ownership and host-dispatch mandatory for all runtime-bound control-plane paths in this change.

- **[Local deploy remains physically co-located]** → This change does not by itself increase total capacity.
  - **Mitigation:** Treat success as architectural readiness, not immediate scaling. The next server buy becomes useful only after this foundation exists.

- **[Routing contract may be over-specified too early]** → Later multi-host proxying might want a different internal transport.
  - **Mitigation:** Specify host-aware resolution now, but keep the exact future remote transport behind host executors and host metadata.

## Migration Plan

1. Add host schema and host references on runtime-owned records.
2. Introduce startup self-registration for the current OVH box and mark it as the only active eligible host.
3. Backfill all existing machine and snapshot rows to that host.
4. Introduce the host executor and scheduler abstractions, with a local-host implementation only.
5. Convert machine, snapshot, diagnostics, shell, and route lookup paths to use host ownership and host dispatch.
6. Update host bootstrap/deploy tooling so the local host identity is explicit and durable.
7. Re-run the existing live smokes and stress harnesses to prove that the one-box deployment still behaves the same externally.

Rollback:
- Revert the code and DB migration before adding any second host.
- Since there are no users or preserved state requirements, rollback can treat the current OVH box as the only runtime again if needed.

## Open Questions

- What exact host identity should be durable across reprovisioning: configured `host_id`, hostname, or both?
- Should the current box register as role `combined`, or should role be orthogonal labels such as `runs_control_plane=true` and `accepts_vm_placements=true`?
- How much of future remote-host routing metadata should be introduced now versus left implicit until the first real worker host exists?
- Should drained/unhealthy hosts immediately block shell and diagnostics resolution, or only block new placement in this first step?
