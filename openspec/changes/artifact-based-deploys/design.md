## Context

Fascinate's current deployment workflow assumes the target host is also a trusted build machine. `ops/host/install-control-plane.sh` runs `go build`, rebuilds `web/dist`, and reads systemd/Caddy install assets from the repo checkout on the host. `ops/host/deploy-web.sh` similarly rebuilds the frontend on the host before copying it into `/opt/fascinate`.

That model has two operational problems:
- it requires product hosts to carry build toolchains such as Go, Node, and `pnpm`
- it makes the deployed release depend on whatever source tree happens to be on the host at deploy time

We have already seen the second problem cause a broad rollback when a full deploy rebuilt from a stale host-side copy of the repo. The next deploy model needs to treat the host as an artifact consumer, not as the release authority.

Constraints:
- Full deploys still need to update the backend binary, browser bundle, systemd unit, and generated Caddyfile behavior.
- Web-only deploys must preserve the current no-restart semantics and the "keep old hashed assets, swap `index.html` last" behavior.
- Bootstrapped hosts should remain able to create or preserve `/etc/fascinate/fascinate.env`, `/opt/fascinate`, and runtime data directories.
- We should keep the runtime filesystem layout under `/opt/fascinate` stable unless a stronger change is required.
- The deploy path must work from a developer machine or CI runner and must not require a git checkout on the host.

## Goals / Non-Goals

**Goals:**
- Build versioned full-release and web-only artifacts from the intended source tree off-host.
- Make the supported full deploy path install only from uploaded artifacts, with no host-side `go build` or `pnpm build`.
- Make the supported web-only deploy path install only from uploaded web artifacts while preserving no-restart asset-swap behavior.
- Package every deploy-time asset needed for installation so the host does not read service or config helpers from a repo checkout.
- Persist release metadata on the host so post-deploy verification can report exactly what artifact is live.

**Non-Goals:**
- Designing a full CI/CD pipeline, approvals model, or release promotion system in this change.
- Reworking Fascinate's runtime service layout, data directory layout, or database schema.
- Adding automatic rollback orchestration beyond preserving enough metadata for operators to install a prior artifact again.
- Eliminating all optional developer tooling from hosts immediately if operators still want local debugging tools for unrelated reasons.

## Decisions

### 1. Build two artifact types: full release and web-only release

The change will introduce two release bundle types built from the local/CI source tree:
- a full release bundle for backend + web + packaged install assets
- a web-only bundle for frontend-only deploys

Each bundle will be versioned and include a release manifest. The full bundle should contain, at minimum:
- the Linux `fascinate` binary for the target platform
- prebuilt `web/dist`
- packaged install scripts that operate relative to the unpacked artifact
- the systemd unit and any config-generation helpers the install flow needs
- a manifest with release identity and checksums

The web-only bundle should contain:
- prebuilt `web/dist`
- the web-only installer logic
- a manifest with release identity and checksums

Why:
- Full and web-only deploys have different restart behavior and different artifact payloads.
- Packaging install assets with the bundle removes dependence on a host repo checkout.
- A release manifest lets operators confirm what was actually built and installed.

Alternatives considered:
- One monolithic bundle for every deploy.
  - Rejected because web-only deploys should remain lightweight and no-restart.
- Continue rebuilding artifacts on the host and only add a version check.
  - Rejected because it does not remove the stale-source failure mode.

### 2. Run installers from the unpacked artifact, not from a host-side repo tree

The artifact deploy flow will upload a bundle to the host, unpack it into a temporary staging directory, and execute the packaged install script from that staged artifact. The host will install from files inside the artifact only.

This means the host install step will no longer depend on:
- `/home/ubuntu/vmcloud` or any other host repo copy
- host-local Node or Go toolchains
- repo-relative service/config helper files outside the artifact staging directory

Why:
- The install logic must be versioned with the artifact it is applying.
- Host-side repo state is exactly what caused the rollback we want to prevent.
- This keeps the deploy contract simple: upload bundle, unpack, install.

Alternatives considered:
- Keep `ops/host/install-control-plane.sh` as a source-based installer and add warnings.
  - Rejected because operators can still accidentally rebuild from stale source.
- Preinstall a permanent host-wide installer outside the repo and keep all logic there.
  - Rejected for the first change because it adds another versioned component to manage; packaging the installer with the artifact is simpler.

### 3. Keep `/opt/fascinate` as the runtime target, but stage releases under a versioned release directory

The installer will unpack into a versioned staging/release directory under `/opt/fascinate/releases/<release-id>` and then install into the live runtime paths under `/opt/fascinate`. The live service can continue using `/opt/fascinate/bin/fascinate` and `/opt/fascinate/web/dist`, while the release directory preserves the artifact contents and manifest that produced those live files.

For the web-only flow, the installer will preserve the existing hashed-asset copy behavior:
- copy new hashed assets into `/opt/fascinate/web/dist/assets`
- preserve older hashed assets still referenced by open tabs
- swap `index.html` last

Why:
- Keeping the existing runtime paths limits service-file churn and reduces risk.
- Storing a release directory gives us a durable place for manifests and future rollback inputs.
- Preserving the current web asset swap semantics avoids regressions for already-open browser sessions.

Alternatives considered:
- Switch the service to a `/opt/fascinate/current` symlink.
  - Rejected for now because it widens the runtime change surface without being strictly required for artifact installs.
- Replace the web install with an atomic directory swap that deletes old hashed assets.
  - Rejected because it breaks the current "older tabs can still lazy-load old chunks" behavior.

### 4. Persist and surface installed release metadata as a host-level manifest

Each artifact will carry a manifest with:
- release ID
- source revision if available
- dirty-state flag if applicable
- build timestamp
- target platform
- per-payload checksums

After a successful install, the host will persist that manifest to a stable location such as `/opt/fascinate/release-manifest.json` and also keep the manifest inside the versioned release directory.

Deploy tooling and host verification can then read that manifest to confirm what is live. This avoids needing the backend or web app to infer their version from a repo checkout.

Why:
- We need a direct way to prove which artifact is installed after deploy.
- A stable installed manifest makes post-deploy checks scriptable.
- This decouples release verification from git availability on the host.

Alternatives considered:
- Only print the source revision during build time.
  - Rejected because the live host still needs a persistent record after the deploy completes.
- Add a backend API endpoint as the only source of build metadata.
  - Rejected for the first step because the install flow itself should be verifiable even before application-level endpoints are expanded.

### 5. Treat host bootstrap and verification as runtime prerequisites, not build prerequisites

Once artifact install is the supported path, host bootstrap and host verification should stop treating Node, `pnpm`, and Go as required deploy prerequisites. Verification should instead focus on runtime prerequisites plus installed release metadata.

Why:
- Hosts are no longer expected to compile the product.
- Host verification should validate the production contract we actually support.

Alternatives considered:
- Leave the extra build toolchains installed "just in case."
  - Rejected because it preserves confusion about whether host-side builds are still supported.

## Risks / Trade-offs

- **[Artifact build logic becomes more complex than the current host-build scripts]** -> Keep bundle structure explicit, keep full and web-only installers small, and cover bundle shape with focused script verification tests.
- **[Platform targeting mistakes could produce a valid bundle for the wrong architecture]** -> Record target platform in the manifest and require build scripts to name it explicitly in the artifact identity.
- **[Operators may keep using stale host-side source scripts out of habit]** -> Update the docs, make artifact deploy wrappers the default entry point, and convert legacy host scripts into artifact installers or hard failures instead of source builders.
- **[Bundled install assets can drift from runtime expectations]** -> Package the exact systemd/Caddy helper files with the build and verify their presence/checksums in the bundle manifest.
- **[Web-only deploys could regress the current asset preservation behavior]** -> Reuse the existing hashed-asset copy semantics in the artifact installer and add verification coverage for repeated web deploys.

## Migration Plan

1. Add off-host bundle build scripts for full and web-only releases.
2. Add packaged artifact install scripts and local/CI deploy wrappers that upload bundles and execute the staged installers remotely.
3. Update the current host install scripts so the supported path consumes artifacts rather than source.
4. Persist installed release manifests and surface them through host verification tooling.
5. Update bootstrap, verification, README, and deployment docs to describe artifact-consumer hosts rather than host-local builds.
6. Validate on a bootstrapped host by running a full artifact deploy, a web-only artifact deploy, and post-deploy verification.

Rollback:
- Reinstall the previous release artifact from its preserved bundle or release directory.
- Because no database or runtime data format changes are required, rollback is an application/package rollback only.

## Open Questions

- Should the first implementation keep legacy host-local build scripts as explicit dev-only escape hatches, or should it fail hard on any attempt to deploy from source?
- Do we want the initial verification surface to be only a persisted host manifest, or also an application-level build-info endpoint in the same change?
