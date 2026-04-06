## 1. Shared-CPU Policy Foundations

- [x] 1.1 Add the config and persisted-budget changes needed to represent soft per-user CPU entitlement, host shared CPU overcommit policy, and retuned one-box RAM/disk defaults for the `8 users * 5 active VMs` target.
- [x] 1.2 Update control-plane budget types and helpers so they distinguish soft CPU entitlement from hard RAM, retained-storage, machine-count, and snapshot-count limits.
- [x] 1.3 Update host diagnostics/state reporting so each host publishes physical CPU, shared CPU ceiling, and current nominal active CPU demand.

## 2. Admission And Lifecycle Refactor

- [x] 2.1 Replace hard per-user CPU admission checks with host-shared CPU ceiling checks for create, start, fork, and restore while keeping hard RAM and retained-storage enforcement unchanged.
- [x] 2.2 Apply the new shared-CPU policy consistently under the host-scoped lock across create/start/fork/restore rejection paths and lifecycle reservation logic.
- [x] 2.3 Update rejection errors and lifecycle responses so shared host CPU exhaustion is reported distinctly from hard RAM, retained-storage, machine-count, snapshot-count, and host-health failures.

## 3. Diagnostics, Operator Surfaces, And Docs

- [x] 3.1 Extend per-user and per-host diagnostics endpoints, payloads, and operator scripts to expose soft CPU entitlement, nominal active CPU demand, shared CPU headroom, and hard RAM/storage posture.
- [x] 3.2 Update README and relevant operator/stress documentation to explain the new soft-CPU, hard-RAM, hard-storage model and the target one-box capacity shape.
- [x] 3.3 Update any browser or API user-facing copy needed so CPU is described as shared platform capacity rather than a strict per-user reserved quota.

## 4. Validation

- [x] 4.1 Add backend tests for soft CPU entitlement behavior, host shared CPU ceiling enforcement, and continued hard RAM/retained-storage enforcement.
- [x] 4.2 Update stress/smoke tooling and validation docs to cover the target operating point of roughly `8` users with `5` active default VMs each and the graceful CPU-ceiling rejection path.
- [x] 4.3 Run local verification and live-host validation for the new model, including multi-user active-VM concurrency, hard RAM/disk guardrails, and shared-CPU ceiling behavior on the OVH host.
