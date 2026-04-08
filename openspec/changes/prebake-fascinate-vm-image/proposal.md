## Why

Fresh Fascinate VM creation is slow and brittle because each new machine boots a mostly stock Ubuntu image and then installs the default toolchain during first boot. That puts user-facing create latency on external package registries, network availability, and whatever `latest` resolves to at that moment. Since there are no existing users to preserve, this is the right time to move that work into a platform-managed image pipeline and make fresh machine creation fast, deterministic, and semantically fresh.

## What Changes

- Build and publish a versioned Fascinate VM base image that already contains the default guest toolchain, including Python, Node, Go, Docker, GitHub CLI, Claude Code, and Codex.
- Add image sealing and validation so published images boot as fresh machines without carrying over machine-specific identity or auth state.
- Reduce boot-time cloud-init to machine-specific finalization such as hostname, network, guest SSH access, managed env files, and injected `AGENTS.md`.
- Change fresh machine creation to boot from a validated Fascinate image instead of relying on live first-boot package installation.
- Remove the current first-boot toolchain provisioning path once the prebaked image flow is authoritative.

## Capabilities

### New Capabilities
- `prebaked-vm-images`: Build, validate, publish, and consume platform-managed Fascinate VM images that ship the default guest toolchain before a user creates a machine.
- `fresh-vm-bootstrap`: Create fresh Fascinate VMs from a promoted image whose default toolchain is already present, with boot-time work limited to machine-specific finalization.

### Modified Capabilities
<!-- None. This change introduces new capabilities rather than modifying an archived capability. -->

## Impact

- Affected code: `ops/cloudhypervisor/build-base-image.sh`, `internal/runtime/cloudhypervisor/`, readiness checks, and host smoke/verification scripts.
- Affected systems: base-image build pipeline, VM bootstrap flow, fresh machine creation latency, and image promotion/rollback operations.
- Dependencies: image build host tooling, version-pinned package acquisition during image build, and smoke validation for promoted images.
