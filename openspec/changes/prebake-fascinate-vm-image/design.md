## Context

Fresh machine creation currently pays the full cost of guest provisioning on every VM boot. The runtime boots a base image, writes cloud-init data, starts the VM, and then waits for cloud-init to finish installing the standard Fascinate toolchain and starting Docker before the machine is considered ready. That makes user-facing create latency depend on external package registries, network stability, and whatever the current `latest` versions of Node, Go, Claude Code, and Codex happen to be at that moment.

Fascinate does not need to preserve current-user behavior or a compatibility bridge for existing machines. There are no live users, and the correct long-term model is for a fresh machine to be a truly fresh VM that boots from a platform-owned image already containing the default toolchain. The image pipeline should own the expensive provisioning work, and per-machine boot should only finalize machine-specific state.

## Goals / Non-Goals

**Goals:**
- Make fresh Fascinate VM creation substantially faster by moving toolchain installation out of per-machine first boot.
- Make fresh machine readiness deterministic and independent of live package downloads during user-facing creation.
- Keep fresh machine semantics distinct from snapshot restore semantics.
- Produce versioned, validated, rollbackable Fascinate VM images with a clear promotion path.
- Keep per-machine boot work limited to machine identity, networking, managed env files, guest instructions, and user-auth restoration.

**Non-Goals:**
- Reusing user snapshot restore as the default create path.
- Preserving the current first-boot provisioning path indefinitely as a compatibility fallback.
- Supporting mixed old and new image semantics for existing users.
- Changing the control-plane API for machine creation.
- Reworking VM sizing, quotas, or host placement as part of this change.

## Decisions

### 1. Build a real Fascinate guest image instead of provisioning every VM at first boot

Fascinate will replace the current "download Ubuntu and convert it" base-image flow with a platform-owned image build pipeline that produces a versioned guest image containing the standard toolchain before any user machine is created.

The build pipeline will:
- start from the upstream Ubuntu cloud image
- boot a disposable builder VM from that image
- run a provisioning script inside the guest to install and configure the standard Fascinate toolchain
- write an image manifest under `/etc/fascinate/image-manifest.json`
- seal the image into a reusable published artifact

Why:
- It moves slow and failure-prone work out of the user-facing create path.
- It keeps the resulting guest state close to what production fresh machines actually boot.
- It avoids conflating "fresh machine" with "restored machine state".

Alternatives considered:
- Continue provisioning on first boot. Rejected because it is exactly what causes slow and brittle fresh creates.
- Use a hidden system snapshot as the normal create path. Rejected because snapshot restore preserves resumed guest state semantics and host-local snapshot constraints that do not fit the long-term fresh-machine model.
- Customize the image entirely offline via direct disk/chroot mutation. Rejected for the first implementation because guest-boot provisioning is easier to reason about, closer to runtime reality, and simpler to validate end to end.

### 2. Treat the published image as a versioned, promoted artifact

The build output will be a versioned image artifact plus metadata, not an ad hoc file overwrite. The host will maintain:
- a versioned image file in the image store
- a stable "current" image reference consumed by machine create
- a manifest with build inputs and tool versions
- a retained previous image for rollback

Fresh VM creation will continue to use the normal create path and consume the currently promoted image via `FASCINATE_DEFAULT_IMAGE` or its successor stable reference.

Why:
- It makes promotion, rollback, and debugging explicit.
- It allows image validation without exposing half-built artifacts to users.
- It avoids binding machine create to ephemeral build outputs.

Alternatives considered:
- Overwrite one fixed image path in place. Rejected because it makes rollback and forensic debugging harder.
- Add a database-backed image registry. Rejected for now because the single-host artifact model is sufficient and avoids unnecessary control-plane complexity.

### 3. Shrink cloud-init to machine-specific finalization only

Boot-time cloud-init will stop installing system packages, downloading Node and Go, or globally installing AI CLIs. Instead, boot-time cloud-init will only:
- set machine hostname and per-machine metadata
- apply static network configuration and guest SSH access
- write managed env files and injected `AGENTS.md`
- ensure any required per-machine directories and symlinks exist

Per-user tool-auth restoration will remain a control-plane post-boot step, as it is machine-owner-specific rather than image-specific.

Why:
- Machine creation should boot a ready image, not assemble a workstation.
- Machine-specific state belongs at boot; shared toolchain state belongs in the image.
- This keeps fresh create semantics clean while preserving owner-specific hydration after the VM is reachable.

Alternatives considered:
- Keep a partial first-boot installer for "just a few" tools. Rejected because it preserves nondeterministic readiness and splits the source of truth for the default guest environment.
- Bake user auth into the image. Rejected because auth state is user-specific and must remain outside the published image artifact.

### 4. Seal images so every fresh VM boots with fresh identity

Before promotion, the candidate image will be scrubbed so published images never contain machine-specific or user-specific runtime identity. The sealing step will remove or reset at least:
- `cloud-init` instance state
- `/etc/machine-id`
- SSH host keys
- auth/session state for Claude, Codex, GitHub CLI, and related guest config
- shell histories, temp data, and package caches that should not ship in the base image

The build pipeline will leave the preinstalled binaries, system packages, Docker configuration, guest user setup, and any Fascinate-owned static configuration intact.

Why:
- A fresh VM must be fresh, not a hidden restore of previously booted machine state.
- Identity scrubbing is the main semantic advantage of an image pipeline over a snapshot-backed create path.

Alternatives considered:
- Publish the builder VM disk as-is. Rejected because it risks duplicate guest identity and leaked session state.
- Regenerate identity only during per-machine boot. Rejected because a sealed image is easier to validate and safer by default.

### 5. Validate candidate images before promotion and rollback by switching image versions

Fascinate will not promote an image just because the build finished. A candidate image must pass an automated validation flow that boots a temporary VM and verifies at least:
- SSH readiness
- default toolchain presence
- Docker daemon availability
- absence of persisted auth/session state in the guest
- correct machine-specific finalization for managed env and `AGENTS.md`

Only then will the system advance the stable image reference. Rollback will mean repointing that reference to the last known good image rather than re-enabling the old first-boot provisioning path.

Why:
- The user-facing create path should only ever consume images that already passed the same readiness expectations that production machines use.
- Rollback should stay in the image lifecycle, not revive obsolete provisioning behavior.

Alternatives considered:
- Trust image build success without boot validation. Rejected because a bootable artifact can still be semantically broken for Fascinate.
- Keep the old first-boot installer as runtime fallback. Rejected because it preserves the exact operational path this change is meant to remove.

### 6. Pin build inputs and record them in the image manifest

The image build must stop resolving unbounded `latest` versions during user-facing boot. The build pipeline will accept explicit version inputs for the toolchain, record the resolved versions in the manifest, and promote only the validated result.

Why:
- Reproducibility matters more than silently drifting tool versions.
- Operators need to know what image content produced a failure.
- Rebuilds and rollbacks are much easier when image contents are explicit.

Alternatives considered:
- Resolve `latest` during every image build without recording the result. Rejected because it preserves drift and weakens debugging.
- Keep resolving `latest` during machine boot. Rejected because it reintroduces the current reliability problem.

## Risks / Trade-offs

- **[Larger base images and longer image builds]** → Accept the cost in the build pipeline, keep published artifacts sparse where possible, and optimize user-facing create latency instead of build speed.
- **[Image drift between rebuilds]** → Pin versions, record a manifest, and require explicit promotion of each candidate image.
- **[A bad promoted image can break fresh create globally]** → Validate every candidate before promotion and retain the previous image version for immediate rollback.
- **[Docker and other services may still add cold-boot latency]** → Keep service startup in the image, validate readiness in smoke tests, and treat residual boot latency as a smaller second-order problem.
- **[Build logic becomes more operationally complex]** → Keep the pipeline local and file-based for the first version instead of introducing a new service or registry layer.

## Migration Plan

1. Replace the current base-image script with a versioned image build flow that provisions a disposable builder VM and seals a candidate image artifact.
2. Introduce an image manifest format, candidate/published image layout, and promotion/rollback script support under `ops/cloudhypervisor/` or `ops/host/`.
3. Split the current runtime cloud-init bootstrap into build-time provisioning and boot-time machine finalization.
4. Update fresh machine creation to consume the promoted Fascinate image artifact without invoking live package installation during guest boot.
5. Extend smoke and verification coverage so the promoted image is validated through an actual VM boot before use.
6. Cut over the host to the new image path and remove the old first-boot toolchain provisioning path instead of maintaining both flows.

Rollback:
- Repoint the stable image reference to the previously promoted image and redeploy the host configuration if the new image proves bad.
- Do not reintroduce the legacy first-boot provisioning flow as part of rollback; rollback stays within the new image-artifact model.

## Open Questions

- Should the promoted artifact remain `raw`, switch to `qcow2`, or stay format-agnostic as long as the runtime can detect and consume it?
- What cadence should rebuilds follow: manual promotion only, scheduled rebuilds, or both?
- Do we want one build/promotion script with subcommands, or separate scripts for build, validate, promote, and rollback?
