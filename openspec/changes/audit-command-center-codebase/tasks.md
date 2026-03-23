## 1. Audit Setup and Review Framework

- [x] 1.1 Inventory the current browser-first Fascinate subsystems, active specs, and existing validation commands that the audit must cover.
- [x] 1.2 Define and use a consistent findings format that records cleanup opportunities, behavioral bugs, and test-hardening gaps with code references and recommended actions.

## 2. Frontend and API Audit

- [x] 2.1 Review `web/` workspace, sidebar, modal, and terminal-window code for removable complexity, inconsistent state handling, and UI regressions.
- [x] 2.2 Review browser auth, HTTP API, workspace persistence, and browser-terminal issuance flows in `internal/httpapi/`, `internal/browserauth/`, and related control-plane code for correctness and simplification opportunities.

## 3. Runtime, Control Plane, and Ops Audit

- [x] 3.1 Review `internal/browserterm/`, `internal/controlplane/`, and `internal/runtime/cloudhypervisor/` for session-lifecycle bugs, dead paths, and code that can be simplified or removed.
- [x] 3.2 Review `ops/host/` and supporting runtime/deploy scripts for stale assumptions, duplicate behavior, and missing validation around deploy and host lifecycle flows.

## 4. Test Hardening Review and Remediation Plan

- [x] 4.1 Audit Go and web test coverage to identify brittle tests, missing regression coverage, and validation gaps around browser auth, terminal sessions, machine/snapshot/env-var flows, and deploy/runtime behavior.
- [x] 4.2 Produce a prioritized remediation plan that groups accepted findings into focused follow-up work for code removal, simplification, bug fixes, and stronger automated coverage.
