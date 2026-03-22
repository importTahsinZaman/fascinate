## Why

Fascinate now has a meaningful product surface: async VM creation, live app routing, persisted tool auth, full-VM snapshots, and true cloning. That surface has been proven through focused smokes, but not yet through a broad stress pass that exercises the features together under realistic developer workloads such as app servers, databases, Docker containers, repeated snapshot/restore, and operator-debuggable failures.

## What Changes

- Define an explicit validation matrix for Fascinate's current expectations, including VM lifecycle, guest readiness, app serving, Docker, local databases, tool-auth persistence, snapshots, create-from-snapshot, and true clone independence.
- Add or improve operator-facing diagnostics so failures during VM provisioning, routing, snapshotting, restore, clone, and persisted tool-auth sync can be observed and debugged quickly.
- Expand live smoke coverage beyond the current narrow happy paths to include real guest workloads: public app servers, local database processes, Docker containers, snapshot-backed restores, and true clones of running environments.
- Harden automated tests where stress findings expose brittle assumptions, especially around state transitions, cleanup, runtime isolation, and restore correctness.
- **BREAKING** Remove or replace any implementation shortcuts that cannot satisfy the documented Fascinate expectations under stress, since there are no live users or compatibility constraints to preserve.

## Capabilities

### New Capabilities
- `platform-diagnostics`: Operator-visible diagnostics and observability for VM lifecycle, routing, snapshots, cloning, and tool-auth persistence.
- `platform-stress-validation`: A durable validation matrix and smoke coverage for Fascinate's supported platform behaviors under realistic VM workloads.

### Modified Capabilities
- `persistent-tool-auth`: Tighten reliability expectations for capture, restore, and diagnostics when auth persistence is exercised across stressed VMs and cloned/snapshotted environments.

## Impact

- Affected code includes the control plane, Cloud Hypervisor runtime, host smoke scripts, shell/frontdoor paths, HTTP diagnostics, and persisted tool-auth adapters.
- Likely changes touch live validation tooling under `ops/host/`, runtime and control-plane state reporting under `internal/runtime/` and `internal/controlplane/`, and possibly new diagnostic endpoints or CLI surfaces.
- This change will create and destroy real VMs on the live Fascinate host during validation and may add new host-visible logs, events, or debug surfaces to support that work.
