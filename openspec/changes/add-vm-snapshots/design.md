## Context

Fascinate currently creates VMs from a base image and clones VMs by copying the source disk, booting a fresh machine, and then restoring persisted tool auth. That preserves filesystem state, but it does not preserve running processes, RAM, open Docker containers, or the live state of a development server. The product goal for snapshots is stronger: users must be able to freeze a VM at an arbitrary moment and later restore it exactly as it was, and cloning must feel like a true fork of a live agent workspace.

The current Cloud Hypervisor runtime is not structured for that. It assumes:
- one shared guest bridge and unique per-VM guest IP allocation
- clone equals disk copy plus new seed image plus fresh boot
- tool auth is restored after create/clone because guest state is otherwise fresh

Those assumptions conflict with full-memory snapshots. A true restored clone cannot rely on rewriting guest networking from scratch after boot, because the snapshot includes the guest kernel and device state in memory. Since there are no live users or state to preserve, the right approach is a rip-and-replace migration of the runtime, storage layout, and machine lifecycle around snapshot restore semantics.

## Goals / Non-Goals

**Goals:**
- Support explicit saved VM snapshots that capture disk, memory, and device state.
- Allow users to create new VMs directly from their saved snapshots.
- Change clone to produce a true running clone by taking an implicit snapshot and restoring it into a new VM.
- Preserve live development state across restore, including running dev servers, Docker containers, shell sessions, and in-memory application state.
- Replace the current Cloud Hypervisor runtime assumptions with a snapshot-native implementation that is robust enough for future regression testing.

**Non-Goals:**
- Multi-host snapshot portability or migration across physical hosts.
- Long-term deduplicated snapshot storage optimization in the first delivery.
- Cross-version snapshot compatibility across arbitrary Cloud Hypervisor or guest image upgrades.
- Preserving active client TCP connections from before the snapshot; the restored VM only needs to resume internal process state and be reachable again through Fascinate.

## Decisions

### 1. Treat snapshot/restore as the primary machine primitive, not an add-on

Fascinate will migrate the runtime model so snapshots are first-class artifacts and clone becomes a thin wrapper around snapshot create + snapshot restore.

Why:
- The user-facing requirement is “true clone,” not “better disk copy.”
- Keeping the old clone path alongside a new snapshot path would preserve the wrong semantics and multiply edge cases.
- There are no live users, so there is no migration benefit to dual behavior.

Alternative considered:
- Keep disk-copy clone and add snapshots separately.
  - Rejected because it leaves two conflicting clone meanings in the product and preserves the current limitation for the most important workflow.

### 2. Make snapshot artifacts immutable and user-owned

Each saved snapshot will be modeled as an immutable artifact owned by a Fascinate user. A snapshot record will reference a host-side snapshot directory containing:
- snapshot metadata
- memory/device-state files produced by the hypervisor
- disk snapshot chain metadata or copied disk assets
- guest identity metadata needed for restore

The control plane will add a snapshot table keyed by snapshot ID and owner user ID, with state such as `CREATING`, `READY`, `FAILED`, and `DELETING`.

Why:
- Immutability keeps restore predictable and avoids accidental mutation of a saved point-in-time environment.
- A user-owned artifact model matches the intended UX: “saved machines I can fork from later.”

Alternative considered:
- Let snapshots be mutable machine checkpoints attached only to a VM.
  - Rejected because it makes reuse, listing, cleanup, and create-from-snapshot flows awkward.

### 3. Move from shared guest networking to per-VM isolated guest networks

The runtime will stop relying on a single shared guest bridge with globally unique guest IPs. Instead, each VM will run inside its own Linux network namespace with its own guest bridge, tap device, and outbound uplink to the host. Every VM will be allowed to keep the same guest-internal network identity, because namespace isolation prevents conflicts.

The concrete model for v1 is:
- each VM gets a dedicated Linux network namespace
- inside that namespace, the guest tap attaches to a bridge with a fixed guest-facing gateway, e.g. `10.42.0.1/24`
- the guest itself uses a fixed VM-internal IP and MAC, e.g. `10.42.0.2` plus a stable virtio NIC identity
- each namespace also gets a unique veth uplink pair connecting it to the host root namespace
- outbound internet access is provided by namespace/root NAT, not by placing every guest directly on one shared bridge

The host will still expose exactly one Fascinate URL per machine, but internal routing will change so:
- restored snapshots do not require guest memory surgery to change IP/MAC before resume
- multiple snapshot-derived machines can coexist safely even if their guest NIC/IP state is identical
- host-side shell and app routing target a per-machine namespace boundary rather than a globally unique guest IP
- the public reverse proxy dials a host-side per-machine forwarder address, not the guest IP directly

Why:
- A memory snapshot includes the guest kernel’s view of its NIC, IP, routes, and open sockets.
- The current unique-IP bridge model is compatible with fresh boot provisioning, but it is the wrong substrate for true live clone.

Alternative considered:
- Keep the shared bridge and patch guest networking after restore.
  - Rejected because it is brittle, invasive, and undermines the value of full-memory restore.

### 4. Introduce host-side per-machine forwarders for shell and app access

Because the guest IP will no longer be unique in the host root namespace, Fascinate will stop routing directly to guest IPs from the host. Instead:
- interactive shell and guest-management commands will run through `ip netns exec <namespace>` and connect to the fixed guest IP inside that namespace
- app traffic will terminate at a host-side per-machine TCP forwarder bound in the root namespace
- Caddy will proxy each machine hostname to that host-side forwarder rather than to the guest IP

Why:
- Caddy and the SSH frontdoor run in the host root namespace.
- A fixed same-IP-per-VM guest model is compatible with snapshots only if the root namespace never treats guest IPs as globally unique.
- Host-side forwarders let Fascinate preserve the simple one-machine-one-public-URL product model while hiding the namespace complexity.

Alternative considered:
- Teach Caddy and every host-side caller to setns into the VM namespace per request.
  - Rejected because it complicates every caller path and makes non-Go components like Caddy awkward to integrate.

### 5. Saved snapshot restore is authoritative over post-create tool auth restore

When a VM is created from a saved snapshot or an implicit clone snapshot, Fascinate will not layer the current persisted tool-auth bundle on top afterward. The restored machine state from the snapshot is authoritative.

Why:
- The snapshot is supposed to be an exact point-in-time machine state.
- Rehydrating a newer tool-auth profile after restore would mutate the snapshot semantics and create confusing drift between “what I saved” and “what came back.”

Alternative considered:
- Restore the snapshot, then reapply the newest saved tool auth bundle.
  - Rejected because it breaks the “exact saved VM” mental model.

### 6. Snapshot creation briefly quiesces the source VM, then resumes it

User-created snapshots and clone-triggered implicit snapshots will briefly quiesce or pause the source VM while the host requests a hypervisor snapshot, then resume the source VM after the snapshot is safely recorded.

Why:
- This is the safest way to ensure the snapshot is internally consistent across memory and device state.
- It keeps the source machine usable after the snapshot without requiring the user to shut it down first.

Alternative considered:
- Require the VM to be manually stopped before snapshot.
  - Rejected because it defeats the main user value of “snapshot at any time.”

### 7. Machine readiness for snapshot restores must mean “restored and reachable,” not “process exists”

Create-from-snapshot and clone-from-snapshot will only transition to `RUNNING` after:
- the VM process is restored and alive
- the host-side routing and shell plumbing are attached
- guest SSH is reachable again (if the source snapshot had SSH available)

Why:
- Fascinate already learned that process liveness is not enough for a usable VM.
- Snapshot restore needs the same strict readiness discipline as create.

Alternative considered:
- Mark restored machines `RUNNING` as soon as the hypervisor process starts.
  - Rejected because it recreates the same early-readiness bug the product already had.

## Risks / Trade-offs

- **[Snapshot version fragility]** Snapshot artifacts may be tightly coupled to the exact Cloud Hypervisor, firmware, and guest image versions.  
  **Mitigation:** Pin versions on the host, record snapshot runtime metadata, and reject restore from incompatible versions instead of guessing.

- **[Storage growth]** Full-memory snapshots can consume substantial disk space, especially with multiple large VMs.  
  **Mitigation:** Add snapshot quota/retention limits early and store snapshot size metadata for future cleanup UX.

- **[Pause latency]** Creating a snapshot can briefly interrupt the source VM.  
  **Mitigation:** Make snapshot creation explicit in the UI, surface `SNAPSHOTTING` state, and keep clone implemented as an async job.

- **[Networking migration complexity]** Replacing the shared bridge model is a meaningful runtime rewrite.  
  **Mitigation:** Do the network model migration together with snapshot support instead of trying to preserve the old assumptions, and introduce one host-side forwarder abstraction that all routing/shell paths share.

- **[Restore exactness vs convenience]** Users may expect the latest tool auth/profile data even when restoring an old snapshot.  
  **Mitigation:** Keep snapshot restore exact in v1 and add later UX for “restore snapshot and refresh auth” only if needed.

## Migration Plan

1. Add snapshot metadata tables and host storage layout for saved snapshots.
2. Replace the current shared-network runtime design with per-VM network namespaces, fixed guest-internal identity, and host-side per-machine forwarders.
3. Implement explicit snapshot create/delete/list/restore in the Cloud Hypervisor runtime and control plane.
4. Change machine create to support `base image` or `snapshot` as the source.
5. Replace clone with async implicit snapshot + restore.
6. Update the TUI and SSH flows to surface snapshot creation, snapshot-based machine creation, and clone progress.
7. Rewrite smoke coverage around snapshot create, restore, clone, delete, and restart behavior.
8. Because there are no users or state to preserve, wipe any incompatible runtime state on the host and redeploy the new runtime model in place.

Rollback:
- Revert to the previous commit and redeploy the current non-snapshot runtime if the rewrite fails before public use resumes.
- Since there are no users, rollback does not require any compatibility bridge for existing machine state.

## Open Questions

- Which exact Cloud Hypervisor snapshot API/asset format should be treated as the stable host contract for v1?
- Do we expose saved snapshot names to users directly, or keep user-facing labels mapped to opaque snapshot IDs?
- Should snapshot restore allow optional “resume paused” versus “restore into paused state,” or always resume immediately in v1?
- How much snapshot storage should count against per-user quota in the first delivery?
