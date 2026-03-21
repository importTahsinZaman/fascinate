## Context

Fascinate now combines several cross-cutting subsystems on one live host: async VM provisioning, shell access through host-side forwarders, public app routing, persisted tool auth, full-VM snapshots, and true cloning. The current automated coverage is useful but still fragmented: basic lifecycle smoke, tool-auth smoke, and snapshot smoke each validate a narrow happy path, while the combined user expectation is broader. A user should be able to create a VM, run real workloads inside it, snapshot that workload, clone it, and continue working without hidden platform breakage.

This change is not just about writing more tests. Stress work will likely expose gaps in diagnostics, cleanup visibility, and state reporting. Because there are no live users or compatibility obligations, the implementation can harden or replace brittle behavior directly instead of preserving legacy shortcuts.

## Goals / Non-Goals

**Goals:**
- Define an explicit matrix of current Fascinate expectations and validate each one against the real OVH host.
- Add operator-usable diagnostics for machine lifecycle, snapshot lifecycle, routing/forwarder health, and persisted tool-auth capture/restore.
- Expand smoke and regression coverage to include realistic in-guest workloads: app servers, local databases, Docker containers, snapshots, create-from-snapshot, and true clone independence.
- Use findings from the stress pass to harden brittle control-plane/runtime behaviors and add targeted automated tests for them.

**Non-Goals:**
- Building a multi-host scheduler or distributed observability stack.
- Turning Fascinate into a full metrics platform before feature validation is complete.
- Preserving any implementation shortcut that fails the stress expectations.
- Guaranteeing infinite-scale load behavior; this change is about correctness and robustness of the current single-host product.

## Decisions

### 1. Treat the stress pass as a product-contract exercise, not just ad hoc testing
The first artifact of the work will be an explicit expectation matrix covering what Fascinate currently promises: create, readiness, shell entry, public routing, Docker and local DB workloads, snapshot save, create-from-snapshot, true clone, tool-auth persistence, cleanup, and restart/reboot survival where applicable.

Why:
- It prevents the stress work from becoming an unstructured collection of one-off checks.
- It creates a durable checklist for future regression smokes and operator validation.

Alternative considered:
- Only add more shell scripts as failures are discovered. Rejected because it produces uneven coverage and no shared definition of what “Fascinate works” means.

### 2. Add diagnostics through the existing control plane and host tooling instead of introducing a separate observability stack
Operator-visible diagnostics should land in the current surfaces first: HTTP endpoints, admin CLI output, structured logs, and host smoke helpers. The design should expose enough state to answer questions like “what stage is this VM in?”, “why is this snapshot stuck?”, and “what forwarders/runtime artifacts currently exist for this machine?” without requiring external infrastructure.

Why:
- The fastest path to debuggable failures is to extend the existing control plane and host tooling.
- It keeps the system understandable on a single host and avoids premature observability infrastructure.

Alternative considered:
- Introduce Prometheus/Grafana/OpenTelemetry as part of this change. Rejected because it adds operational weight before the underlying correctness contract is stable.

### 3. Validate realistic guest workloads, not synthetic no-op processes
The stress pass should use actual developer-like workloads:
- public app servers bound to `0.0.0.0`
- local database processes with persisted on-disk data
- Docker containers
- snapshots and clones taken while those workloads are alive

Why:
- The current product claim is about usable developer VMs, not just bootable guests.
- Snapshot and clone correctness matters most when the VM is doing real work.

Alternative considered:
- Keep all smokes on trivial Python HTTP servers only. Rejected because it misses the Docker, database, and multi-process workload classes that users will actually depend on.

### 4. Make stress findings drive targeted hardening in code and tests immediately
When the live pass reveals a failure, the fix should land with the smallest durable regression net that covers it. That usually means:
- a package test for the state transition or parser/command issue
- a host smoke extension when the bug only manifests on the live substrate

Why:
- This keeps the stress pass from becoming a manual certification exercise that has to be re-run from memory.
- It turns production-learned bugs into durable coverage.

Alternative considered:
- Record failures in docs only and defer automated coverage. Rejected because the same classes of regressions have already reappeared when only live fixes were applied.

### 5. Tighten tool-auth semantics under stress instead of treating them as best-effort
Persisted tool auth is now part of the expected user experience. Under stress, Fascinate must distinguish between authoritative capture points and opportunistic background syncs, and it must leave a diagnosable trace when capture or restore fails.

Why:
- Stress-created VMs and cloned VMs will exercise more edge cases in capture/restore timing.
- Background sync is useful, but it must not silently clobber valid auth state during noisy workload activity.

Alternative considered:
- Treat auth persistence as out of scope for stress because it is “secondary” to VM runtime. Rejected because it is already part of the supported product contract.

## Risks / Trade-offs

- **[Live stress consumes real host resources]** → Run the matrix from disposable users/VM names, clean up aggressively, and keep the host wipe/reset path ready before and after the pass.
- **[Diagnostics can accidentally become user-facing attack surface]** → Keep new diagnostics operator-oriented, bound to existing trusted surfaces, or behind admin/local access as appropriate.
- **[Stress scripts can become flaky if they overfit exact timing]** → Use state polling and behavioral checks instead of brittle fixed sleeps.
- **[The matrix can sprawl into an endless project]** → Keep the first pass anchored to the current supported product surface and only add coverage for features Fascinate already claims to support.
- **[Hardening changes may require direct product behavior changes]** → Accept that trade-off and document it; there are no live users and no compatibility burden.

## Migration Plan

1. Write the expectation matrix and map each expectation to the current test or smoke coverage, highlighting gaps.
2. Add the minimum diagnostics needed to observe lifecycle, routing, snapshot, clone, and tool-auth failures on the live host.
3. Build or extend host smoke scripts to exercise the missing workload combinations.
4. Run the matrix against the real OVH host, fixing failures immediately with targeted code/test changes.
5. Re-run the expanded matrix until the supported expectations pass from a clean host state.

Rollback:
- Revert the specific diagnostic or hardening change from git if it destabilizes the control plane.
- Clean up disposable VMs, snapshots, and tool-auth bundles after each failed run so the next pass starts from known state.

## Open Questions

- Which diagnostics belong in HTTP/CLI surfaces versus logs only?
- How much reboot-level coverage is worth automating in this pass versus documenting as a manual operator check?
- Do we want a single umbrella stress script, or a small set of focused scripts that share helper functions?
