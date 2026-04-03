## 1. Release Bundle Build Pipeline

- [x] 1.1 Add a full-release artifact build script that stages the Linux `fascinate` binary, prebuilt `web/dist`, packaged install assets, and a release manifest into a versioned bundle.
- [x] 1.2 Add a web-only artifact build script that stages prebuilt `web/dist`, packaged web install assets, and a release manifest into a versioned bundle.
- [x] 1.3 Add verification for bundle contents and manifest fields, including target platform metadata and payload checksums.

## 2. Artifact Install And Deploy Flows

- [x] 2.1 Implement a packaged full-release installer plus deploy wrapper that upload, unpack, and install a supplied artifact without invoking host-side build steps.
- [x] 2.2 Implement a packaged web-only installer plus deploy wrapper that install a supplied web artifact, preserve older hashed assets, and avoid restarting `fascinate`.
- [x] 2.3 Update or retire the current host source-build deploy scripts so the supported deploy paths consume artifacts instead of rebuilding from a host repo checkout.

## 3. Installed Release Metadata And Operator Tooling

- [x] 3.1 Persist the installed release manifest on the host in a stable location and update host verification/diagnostics to report the live artifact identity.
- [x] 3.2 Update host bootstrap and verification expectations so production hosts are treated as artifact consumers rather than Go/Node build machines.
- [x] 3.3 Update README and deployment docs with the new artifact build, full deploy, web-only deploy, verification, and rollback workflow.

## 4. Validation

- [x] 4.1 Run script-level verification and targeted local build checks for both artifact types.
- [x] 4.2 Validate a full artifact deploy and a web-only artifact deploy on a bootstrapped host, including health/readiness checks and installed-manifest verification.
