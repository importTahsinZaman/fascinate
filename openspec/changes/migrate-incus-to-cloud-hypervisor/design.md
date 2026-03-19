## Context

Fascinate currently provisions user machines through the local `incus` CLI, stores machine metadata in SQLite, shells into guests with `incus exec`, and routes public app traffic through host Caddy to private machine addresses discovered from Incus. That architecture was acceptable on Hetzner Cloud, but the runtime gap is now the main weakness: users get containers, not VMs, and the host still carries Incus-specific networking, storage, and shell assumptions across the control plane, frontdoor, and bootstrap scripts.

The new OVH host is bare metal with KVM available, which makes Cloud Hypervisor a viable machine substrate for the first time. There are no live users, no runtime state worth preserving, and no reason to pay the complexity cost of a long-lived dual-runtime bridge. The migration must preserve Fascinate’s product surface, but it does not need to preserve the Incus implementation behind it.

## Goals / Non-Goals

**Goals:**
- Replace Incus containers with Cloud Hypervisor VMs for Fascinate machines on the OVH host.
- Preserve current control-plane workflows for list, create, clone, delete, shell, tutorial, and `*.fascinate.dev` routing.
- Keep the runtime behind a narrow interface so the rest of the product changes as little as possible.
- Build a host-managed VM networking model with stable private guest IPs and outbound internet access.
- Provide a repeatable VM image pipeline that ships the same default toolchain users get today.
- Remove Incus-specific assumptions from the host bootstrap, runtime, session, and image-build paths.

**Non-Goals:**
- In-place conversion of existing Incus containers into VMs.
- Multi-host scheduling, migration, or clustering.
- Public IP assignment per machine.
- Live migration or memory snapshot/restore in the first delivery.
- Solving every abuse-protection gap; this change focuses on the machine substrate.

## Decisions

### 1. Replace the runtime in place instead of carrying a dual-runtime bridge

Fascinate will keep the existing lifecycle-facing runtime interface in [internal/controlplane/service.go](/Users/tahsin/Desktop/vmcloud/internal/controlplane/service.go), but the Incus-backed implementation will be replaced by a Cloud Hypervisor-backed implementation. The migration will also remove Incus-oriented naming and assumptions from the data model where needed, because there is no user state to preserve.

Why:
- There are no live users or machine records worth migrating.
- A dual-runtime bridge would add complexity to bootstrap, deploy, tests, and runtime selection without protecting meaningful state.
- The control plane, TUI, and routing layers can stay stable while the machine substrate is replaced underneath them.

Alternative considered:
- Build and maintain parallel Incus and Cloud Hypervisor runtimes during rollout. Rejected because the migration-only complexity is not justified when no compatibility or rollback surface is required.

### 2. Separate guest session transport from runtime lifecycle

Today [internal/sshfrontdoor/server.go](/Users/tahsin/Desktop/vmcloud/internal/sshfrontdoor/server.go) shells into machines by directly calling `incus exec`. That coupling does not survive a VM runtime. The frontdoor should instead depend on a guest-session transport abstraction that can open an interactive PTY-backed shell or tutorial session inside a machine by name.

For Cloud Hypervisor, the transport will be host-initiated SSH into the guest’s private IP using a Fascinate-managed host key pair injected into each VM at build or boot time.

Why:
- SSH into guests is simpler and more standard than adding a guest agent or vsock RPC in the first iteration.
- It preserves the current frontdoor UX with minimal user-visible change.
- It keeps shell/tutorial behavior independent of the lifecycle runtime implementation.

Alternatives considered:
- Add shell execution methods to the runtime interface. Rejected because lifecycle and interactive transport are different concerns and will evolve differently.
- Build a guest agent over vsock. Rejected for phase one because it adds a new always-on component inside every VM and increases image complexity.

### 3. Use a dedicated host bridge with static guest IP allocation

Cloud Hypervisor VMs will attach to a host bridge (for example `fascbr0`) through TAP devices. Fascinate will allocate stable private IPv4 addresses from a host-managed subnet and write them into cloud-init for each guest. Host firewall/NAT rules will allow outbound internet access and host-to-guest reachability for SSH and app proxying.

Why:
- Static IPs keep the control plane simple and remove a DHCP dependency from machine creation.
- Caddy and shell transport can rely on deterministic guest addressing.
- It is easier to reconcile VM state when IP assignment lives in the control plane.

Alternatives considered:
- Use DHCP on the bridge. Rejected because it adds another moving part and makes reconciliation slower.
- Give each machine a public IP. Rejected because it changes the product surface and is unnecessary for the current routing model.

### 4. Replace the Incus image alias flow with a qcow2 base-image pipeline

The current image builder in [ops/incus/build-base-image.sh](/Users/tahsin/Desktop/vmcloud/ops/incus/build-base-image.sh) will be replaced by a VM-oriented build flow that produces an immutable base qcow2 image and the metadata needed to boot new guests with cloud-init. New VM disks will be created as qcow2 overlays on top of that base image.

Why:
- qcow2 overlays preserve fast machine creation from a prebuilt image.
- The base-image concept maps cleanly from today’s `fascinate-base`.
- It keeps guest provisioning predictable and repeatable.

Alternatives considered:
- Raw disks per machine copied from scratch. Rejected because it makes creation and cloning slower and consumes more disk immediately.
- Mutating stock Ubuntu cloud images at boot only. Rejected because it pushes too much setup cost into first boot and makes user-facing create latency worse.

### 5. Support clone with disk-copy semantics, not live memory cloning

Phase one cloning will duplicate a machine’s guest disk state and boot the new VM without attempting live memory snapshotting. The design assumes a short pause window or a powered-off copy path for consistent clones if required by the implementation. The user-visible contract is preserved at the machine level, but the runtime will not promise instantaneous live clones.

Why:
- It is the lowest-risk way to keep clone semantics while moving to VMs.
- It avoids making Cloud Hypervisor snapshot/memory restore part of the initial migration.

Alternatives considered:
- Live snapshot/restore in the first migration. Rejected because it adds significant runtime complexity and rollback risk.

### 6. Rewrite runtime-oriented metadata to be VM-neutral

Machine metadata that currently assumes Incus naming or container semantics, such as `incus_name`, will be renamed or replaced with runtime-neutral fields before the Cloud Hypervisor runtime becomes the only supported machine substrate.

Why:
- It removes the last misleading container-specific assumptions from the control plane.
- It is cheaper to clean up the schema now than to carry legacy runtime naming forward forever.

Alternatives considered:
- Keep legacy field names such as `incus_name` and treat them as opaque handles. Rejected because it bakes the removed runtime into the long-term schema for no user-visible benefit.

## Risks / Trade-offs

- **[Lower machine density]** → VMs consume more CPU and RAM overhead than containers. Mitigation: keep current fixed machine sizes, measure actual host density on OVH, and revise quotas only after the VM runtime is stable.
- **[Shell path rewrite risk]** → Replacing `incus exec` touches the core interactive UX. Mitigation: introduce the guest-session abstraction early and verify shell/tutorial flows before removing the old shell path.
- **[Clone performance regression]** → Disk-copy clone semantics may be slower than Incus copy-on-write containers. Mitigation: start with qcow2 overlays and benchmark clone latency before broad use.
- **[Networking complexity]** → TAP devices, bridge rules, and NAT are easier to get wrong than Incus defaults. Mitigation: keep the bridge model simple, automate host verification, and test guest egress and port 3000 routing in scripted smoke checks.
- **[Big-bang cutover mistakes]** → A direct replacement removes the fallback of flipping back to Incus. Mitigation: keep the rewrite bounded to the runtime and session layers, snapshot the OVH host before cutover work, and require end-to-end smoke tests before considering the new runtime ready.

## Migration Plan

1. Snapshot or otherwise checkpoint the OVH host before starting the runtime replacement.
2. Rewrite the runtime, shell transport, and image pipeline to target Cloud Hypervisor on OVH.
3. Remove Incus-specific host bootstrap, deploy, and session assumptions as soon as their Cloud Hypervisor replacements are working.
4. Rename or migrate runtime-specific metadata so the control plane is no longer Incus-oriented.
5. Prove one full machine flow on OVH from a clean state: signup, create, shell, tutorial, app on port 3000, clone, delete.
6. Keep the old code in git history, not in the live runtime path, once the replacement is ready.

Rollback:
- Restore the host snapshot or revert the deployment from git if the rewrite fails before the new runtime is accepted.
- There is no planned operational rollback to a mixed Incus/Cloud Hypervisor production state.

## Open Questions

- What storage layout should back qcow2 images on OVH: ext4, XFS with reflinks, or a different filesystem?
- Should the control plane persist guest private IPs explicitly in the database, or derive them from deterministic allocation on startup?
- Is a brief pause during clone acceptable for the first VM release, or do we need a more advanced snapshot approach immediately?
- Do we want to evaluate Incus VMs as an intermediate step, or commit directly to Cloud Hypervisor as the long-term substrate?
