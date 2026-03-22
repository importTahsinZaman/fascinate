## 1. Snapshot substrate

- [x] 1.1 Add snapshot metadata storage for user-owned immutable snapshots, including snapshot state, source machine linkage, host artifact paths, and size/version metadata.
- [x] 1.2 Replace the current shared guest networking/runtime assumptions with per-VM network namespaces and fixed guest-internal network identity.
- [x] 1.3 Add host-side per-machine shell/app forwarders so routing no longer depends on globally unique guest IPs.
- [x] 1.4 Add Cloud Hypervisor snapshot create, snapshot restore, snapshot delete, and snapshot list support in the runtime.
- [x] 1.5 Add host storage layout and cleanup helpers for snapshot artifacts, including memory/device-state files and disk-chain assets.

## 2. Machine lifecycle rewrite

- [x] 2.1 Extend the control plane with explicit snapshot create/list/delete operations and async snapshot job state handling.
- [x] 2.2 Extend machine creation so a VM can be created from either the base image or a saved snapshot.
- [x] 2.3 Replace the current clone flow with implicit snapshot + restore true cloning.
- [x] 2.4 Change readiness handling so snapshot-created and cloned VMs only become `RUNNING` after restored shell and routing paths are usable.
- [x] 2.5 Make snapshot-based machine restore authoritative over post-create tool-auth restore.

## 3. Product surface

- [x] 3.1 Add snapshot controls to the TUI for creating, listing, and deleting snapshots from a selected VM.
- [x] 3.2 Add snapshot selection during new VM creation and show clone/snapshot progress states in the dashboard.
- [x] 3.3 Update the SSH and HTTP control-plane surfaces to expose snapshot operations and snapshot-based machine creation.

## 4. Hardening and rollout

- [x] 4.1 Add runtime and control-plane tests for snapshot create, snapshot restore, clone, delete, and failed-restore cleanup.
- [x] 4.2 Add live host smoke coverage for create-from-snapshot and true clone preserving a running dev server.
- [x] 4.3 Remove the old disk-copy clone path and any runtime code that assumes fresh-boot-only semantics.
- [x] 4.4 Update docs and host bootstrap/deploy scripts for the snapshot-native runtime and perform a rip-and-replace rollout on OVH.
