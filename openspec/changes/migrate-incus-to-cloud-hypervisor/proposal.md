## Why

Fascinate now runs on OVH bare metal, but user machines are still Incus system containers managed from a host-level `incus` CLI. That leaves a gap between the current product promise of persistent developer machines and the actual isolation, compatibility, and trust boundaries of the runtime.

There are no live users, no machines worth preserving, and no compatibility surface we need to maintain with the Incus runtime. That makes this the right moment to replace the machine substrate outright instead of carrying migration-only complexity through the codebase.

## What Changes

- Replace the current Incus container runtime with a new Cloud Hypervisor VM runtime while preserving the existing control-plane workflows for create, clone, delete, shell, tutorial, and app routing.
- Introduce a host-managed VM networking model so each machine gets a private guest IP that Fascinate can use for shell access and HTTP proxying.
- Replace the Incus image publishing flow with a VM base-image pipeline that produces guest disks and cloud-init data for new machines.
- Replace `incus exec` shell and tutorial execution with host-initiated guest SSH sessions that preserve the current frontdoor experience.
- Remove Incus-specific runtime, bootstrap, image-build, and machine-session paths rather than supporting dual runtimes.
- Rename or replace Incus-specific machine metadata where needed so the control plane no longer depends on container-oriented naming.
- Keep the current user-facing defaults and quotas unless a VM-specific limit needs to be tightened.
- **BREAKING**: Fascinate machines become VMs instead of containers, and the host bootstrap/deploy path no longer depends on Incus.

## Capabilities

### New Capabilities
- `virtual-machine-runtime`: Provision, clone, delete, inspect, and route Fascinate machines as Cloud Hypervisor VMs on the OVH bare metal host.
- `virtual-machine-session-access`: Open shell and tutorial sessions into VM guests through the existing Fascinate SSH frontdoor without relying on `incus exec`.

### Modified Capabilities

## Impact

- Affected code:
  - `internal/runtime/cloudhypervisor`
  - `internal/controlplane`
  - `internal/sshfrontdoor`
  - `internal/httpapi`
  - `ops/host`
  - `ops/cloudhypervisor`
  - runtime-related database fields and migrations
- Affected host systems:
  - bare-metal networking and firewall rules
  - guest image build/publish flow
  - disk and clone storage layout
  - VM process supervision and reconciliation
- Affected operational assumptions:
  - machines become KVM guests instead of containers
  - shell access shifts from host-side container exec to guest SSH
  - machine density on a single host will likely decrease due to VM overhead
  - there is no supported rollback to Incus machines once the replacement lands
