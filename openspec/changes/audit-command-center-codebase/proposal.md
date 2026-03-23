## Why

Fascinate's browser-first command center has grown quickly across the control plane, runtime, browser terminal stack, deploy scripts, and React workspace. Before adding more surface area, the codebase needs a deliberate review to remove unnecessary complexity, catch latent bugs, and harden weak tests while the architecture is still small enough to improve cheaply.

## What Changes

- Perform a full codebase audit across backend, frontend, runtime, and ops code related to the browser-first command center.
- Produce a prioritized findings set that identifies code that can be simplified or removed, behavioral bugs or regressions, and areas where automated tests are too weak or missing.
- Define a remediation workflow that turns findings into focused implementation tasks rather than broad cleanup with unclear scope.
- Require the audit to distinguish between code that should be deleted, code that should be simplified, code that is risky but acceptable, and code that needs stronger test coverage.

## Capabilities

### New Capabilities
- `codebase-quality-audit`: structured review and remediation planning for cleanup opportunities, bugs, and test hardening across the Fascinate browser-first stack

### Modified Capabilities
- None.

## Impact

- Affected areas include `internal/`, `web/`, `ops/`, and supporting tests across the command center, browser auth, browser terminals, VM orchestration, and deploy/runtime flows.
- No immediate user-facing API or schema change is required by this planning change; the output is an audit contract and implementation plan for follow-up cleanup and fixes.
- The resulting work will likely drive code deletion, simplification, bug fixes, and additional tests in both Go and React code paths.
