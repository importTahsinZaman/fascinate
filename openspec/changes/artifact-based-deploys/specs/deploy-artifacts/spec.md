## ADDED Requirements

### Requirement: Fascinate SHALL produce versioned deploy artifacts off-host
Fascinate SHALL provide an artifact build workflow that runs from the intended source tree outside the target host and emits versioned release bundles for both full control-plane deploys and web-only deploys.

#### Scenario: Full release artifact is built
- **WHEN** an operator or CI job builds a full Fascinate release artifact
- **THEN** the build workflow SHALL produce a versioned bundle containing the Linux `fascinate` binary, prebuilt `web/dist`, the required install assets, and a release manifest
- **AND** the release manifest SHALL record the source revision when available, dirty-state metadata, build timestamp, target platform, and checksums for the shipped artifacts

#### Scenario: Web-only artifact is built
- **WHEN** an operator or CI job builds a web-only release artifact
- **THEN** the build workflow SHALL produce a versioned bundle containing the prebuilt `web/dist`, the required web install assets, and a release manifest
- **AND** the web artifact SHALL be installable without rebuilding frontend assets on the host

### Requirement: Fascinate SHALL install full releases from supplied artifacts only
The supported full deploy path SHALL install a supplied release artifact onto a bootstrapped host without rebuilding Go code, rebuilding web assets, or reading deploy-time assets from a host-side repo checkout.

#### Scenario: Full deploy works on an artifact consumer host
- **WHEN** an operator deploys a full release artifact to a bootstrapped Fascinate host that does not have Go, Node, `pnpm`, or a synchronized repo checkout
- **THEN** the installer SHALL unpack the supplied artifact and install the backend binary, browser bundle, and packaged service assets onto the host
- **AND** it SHALL restart `fascinate` and reload Caddy without invoking `go build`, `pnpm`, or other source-build steps on the host

#### Scenario: Stale host source cannot change the deployed release
- **WHEN** the host contains an unrelated or stale source tree
- **THEN** the full deploy path SHALL install only the contents of the supplied release artifact
- **AND** files outside that artifact SHALL NOT affect the live release

### Requirement: Fascinate SHALL install prebuilt web artifacts without restarting the control plane
Fascinate SHALL provide a web-only artifact install path that swaps a prebuilt browser bundle into place without restarting `fascinate`.

#### Scenario: Web-only deploy preserves current no-restart behavior
- **WHEN** an operator deploys a web-only release artifact
- **THEN** the installer SHALL copy the new prebuilt web assets into the install directory, preserve older hashed assets that may still be referenced by open tabs, and swap `index.html` last
- **AND** it SHALL NOT restart the `fascinate` service

### Requirement: Fascinate SHALL record installed release metadata for verification
Every successful artifact install SHALL persist release metadata on the host so operators can verify what build is currently installed.

#### Scenario: Installed release metadata is available after deploy
- **WHEN** a full or web-only artifact install completes successfully
- **THEN** the host SHALL persist the installed release manifest in a stable location outside the source tree
- **AND** the deploy workflow or host verification tooling SHALL be able to report that installed release identity after the deploy
