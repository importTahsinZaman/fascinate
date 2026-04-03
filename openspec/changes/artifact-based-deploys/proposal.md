## Why

Fascinate's current deploy flow rebuilds the Go binary and `web/dist` on the target host from whatever source tree happens to be present there. That makes deploy correctness depend on host-local source hygiene and can silently roll the product backward when the host checkout is stale, even if the deploy command itself succeeds.

This change is needed now because we have already seen that failure mode in practice. The deploy path needs to move from "build whatever is on the host" to "install the exact release artifact that was built from the intended source tree" so release provenance is explicit and repeatable.

## What Changes

- Add off-host release build scripts that package the backend binary, browser bundle, deploy manifest, and required install assets into versioned artifacts.
- Add an artifact-based full deploy flow that uploads a prebuilt release bundle to the host and installs it without rebuilding Go or web assets from a host-side repo checkout.
- Add an artifact-based frontend-only deploy flow that uploads a prebuilt web bundle and swaps it into place without restarting `fascinate`, while preserving older hashed assets for already-open tabs.
- Add release metadata recording so the installed host can report which artifact was deployed and the deploy workflow can verify that the live install matches the intended build.
- Update host bootstrap, verification, and deployment docs so product hosts are treated as artifact consumers rather than trusted build machines.

## Capabilities

### New Capabilities
- `deploy-artifacts`: off-host release bundle creation, artifact-only full deploys, artifact-only web deploys, and installed release metadata verification

### Modified Capabilities

## Impact

- Affected code includes host deploy scripts, release packaging scripts, web build/release staging, systemd/Caddy install assets, and deployment documentation
- Product hosts should no longer require Go, Node, `pnpm`, or a synchronized repo checkout to perform standard deploys
- Deploy verification gains explicit release-manifest metadata so operators can confirm what artifact is live after install
- No database migration or persisted user data format change is required
