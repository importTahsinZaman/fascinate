## Context

Fascinate's browser-first command center now spans multiple layers: browser auth and workspace UX in `web/`, session issuance and PTY orchestration in `internal/browserterm/`, machine and snapshot orchestration in `internal/controlplane/`, VM/runtime behavior in `internal/runtime/cloudhypervisor/`, and deploy/runtime scripts in `ops/host/`. The product has moved quickly, with many recent UX and terminal-session changes landing directly against the live codebase.

The audit needs to create a defensible quality baseline before more feature work continues. That means reviewing the codebase as a system, not as isolated files, and producing outputs that can be acted on in small follow-up changes instead of a broad "cleanup" bucket.

Constraints:
- The audit must cover both user-facing browser flows and supporting backend/runtime/ops behavior.
- Findings must differentiate between code that is safe to delete, code that should be simplified, code that appears buggy, and code that needs stronger tests.
- The audit is review-first; it does not itself change the product contract or introduce new runtime behavior.

## Goals / Non-Goals

**Goals:**
- Review the full browser-first Fascinate stack with explicit coverage of frontend, backend, runtime, deploy, and test code.
- Produce findings that are actionable, prioritized, and supported by concrete code references.
- Separate cleanup opportunities from behavioral bugs and test-hardening gaps so follow-up work can be scoped correctly.
- Turn accepted findings into implementation-ready tasks rather than leaving them as an unstructured report.

**Non-Goals:**
- Implement every cleanup or bug fix during the audit itself.
- Redesign product requirements or introduce new user-facing features unrelated to the findings.
- Replace existing OpenSpec capabilities for VM, snapshot, env-var, or terminal behavior.
- Perform live destructive operations or broad refactors without a follow-up implementation change.

## Decisions

### Audit findings will be organized by subsystem, then by finding type

The audit will review the codebase in slices:
- frontend/workspace UX
- browser auth and API layers
- browser terminal/session lifecycle
- control plane and VM/snapshot/env-var orchestration
- runtime and host/ops scripts
- automated test coverage and validation flows

Within each slice, findings will be categorized as:
- removable code
- simplification opportunities
- behavioral bugs or regressions
- test hardening gaps

Why this approach:
- It keeps the review grounded in architecture boundaries that already exist in the repo.
- It prevents "cleanup" from swallowing bugs or missing-test findings.

Alternative considered:
- A single flat findings list for the entire repo. Rejected because it becomes hard to reason about ownership, impact, and remediation sequencing.

### Every finding will require evidence and a recommended action

Each accepted finding will identify:
- the affected file or subsystem
- the risk or waste being called out
- why the current behavior is problematic or unnecessary
- the recommended action (remove, simplify, fix, or harden tests)

Why this approach:
- It forces the audit to stay concrete and reviewable.
- It reduces the risk of speculative cleanup work that does not materially improve the codebase.

Alternative considered:
- A high-level narrative report with generalized concerns. Rejected because it is not implementation-ready.

### Test hardening will be treated as a first-class outcome, not a secondary note

The audit will explicitly identify:
- behaviors that are untested
- tests that are too shallow or brittle
- areas where current validation misses likely regressions

Why this approach:
- The browser-first surface now depends on coupled backend/frontend/session behavior, so missing tests are often as risky as obvious bugs.

Alternative considered:
- Folding test comments into other findings. Rejected because test debt needs to be visible as its own workstream.

### The audit will produce a prioritized remediation plan instead of a monolithic cleanup task

Follow-up implementation work will be broken into focused tasks grouped by severity and dependency:
- correctness and regression risks first
- then simplification/removal with low behavioral risk
- then test-hardening and polish items

Why this approach:
- It preserves momentum and reduces the risk of a large unstable cleanup branch.

Alternative considered:
- One broad "clean up the codebase" task. Rejected because it is too ambiguous to execute safely.

## Risks / Trade-offs

- [Large audit scope] → Mitigation: structure the review by subsystem and require prioritized findings instead of exhaustive commentary on every file.
- [Spec drift from implementation reality] → Mitigation: anchor findings in the current code and current active specs before recommending follow-up tasks.
- [Cleanup recommendations could become subjective] → Mitigation: require code references, impact explanation, and a concrete recommended action for each finding.
- [Test hardening backlog could balloon] → Mitigation: separate missing-critical-coverage findings from nice-to-have improvements and prioritize by regression risk.

## Migration Plan

1. Complete the audit using the subsystem-based review workflow described above.
2. Convert accepted findings into focused follow-up implementation work.
3. Execute remediation in small changesets with package-scoped validation and broader regression coverage where required.
4. Roll back any follow-up implementation change independently if it introduces regressions; the audit artifacts themselves are documentation-only and do not require runtime rollback.

## Open Questions

- Should the follow-up remediation be a single implementation change or split into multiple changes by subsystem/severity?
- How much of the audit output should be captured in OpenSpec versus a code-review style findings report with inline references?
