## 1. Validation Matrix

- [x] 1.1 Write a concrete expectation matrix for the current Fascinate product surface, covering VM lifecycle, guest readiness, shell entry, public routing, Docker, local databases, persisted tool auth, snapshots, create-from-snapshot, true cloning, cleanup, and restart/reboot expectations.
- [x] 1.2 Map each expectation in that matrix to existing automated coverage, identifying which expectations already have package tests, which have host smokes, and which are currently uncovered.
- [x] 1.3 Add or update repository documentation for the stress-validation entrypoints so operators know how to run the matrix and interpret pass/fail behavior.

## 2. Diagnostics and Observability

- [x] 2.1 Add operator-visible diagnostics for machine and snapshot lifecycle stages, including enough state to identify runtime handles, lifecycle phase, and failure reason.
- [x] 2.2 Add operator-visible diagnostics for host-side shell/app forwarding and guest reachability so routing failures can be distinguished from in-guest workload failures.
- [x] 2.3 Add operator-visible diagnostics for persisted tool-auth restore and capture failures, including tool ID, auth method, and checkpoint context.
- [x] 2.4 Extend host/admin tooling or scripts so the new diagnostics can be queried during live stress runs without manual host forensics.

## 3. Stress Harness

- [x] 3.1 Extend or add host smoke helpers to provision realistic workloads inside test VMs: public apps, local database processes, and Docker containers.
- [x] 3.2 Add a stress path that validates a workload VM can be created, reached over SSH, served publicly, and remain correct after normal lifecycle transitions.
- [x] 3.3 Add a stress path that snapshots a running workload VM, restores a new VM from that snapshot, and verifies the restored app/database/container state behaves as a restored environment instead of a fresh boot.
- [x] 3.4 Add a stress path that clones a running workload VM and verifies both source and clone can coexist, then diverge independently after the clone completes.
- [x] 3.5 Add cleanup assertions to the stress harness so failed create/restore/clone/delete runs prove that VMs, forwarders, TAP/veth artifacts, snapshots, and temp directories are actually removed.

## 4. Hardening From Findings

- [x] 4.1 Run the expanded matrix against the live OVH host from a clean Fascinate state and record every failure, mismatch, or flaky behavior discovered.
- [x] 4.2 Fix VM/control-plane/runtime issues revealed by the stress pass, prioritizing correctness of state transitions, routing, snapshot/clone behavior, workload isolation, and cleanup.
- [x] 4.3 Fix tool-auth and guest-environment issues revealed by the stress pass, including persistence timing, restore/capture correctness, and operator diagnosability.
- [x] 4.4 Add or update targeted Go tests for every hardened failure mode so the new behavior is covered outside the live-host smoke scripts.

## 5. Final Validation

- [x] 5.1 Re-run `go test ./...`, `make verify-ops`, and the expanded live-host stress scripts until the documented expectation matrix passes from a clean starting state.
- [x] 5.2 Update README, AGENTS/docs, and any relevant active OpenSpec artifacts to reflect new diagnostics, stress tooling, and any product-surface changes made during hardening.
