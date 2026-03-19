## 1. Runtime and host foundations

- [x] 1.1 Replace Incus-oriented runtime wiring in the app with a Cloud Hypervisor runtime implementation and remove runtime-selection code that only exists for migration.
- [x] 1.2 Create `internal/runtime/cloudhypervisor` with shared machine models, health checks, and command execution helpers for the host.
- [x] 1.3 Extend host bootstrap and verification scripts to install Cloud Hypervisor, create the VM bridge, and validate bridge/NAT/firewall readiness on OVH.
- [x] 1.4 Replace the Incus base-image build flow with a VM image build flow that produces the Fascinate guest base image and cloud-init inputs.

## 2. VM lifecycle implementation

- [x] 2.1 Implement VM create/list/get/delete operations in the new Cloud Hypervisor runtime using qcow2-backed guest disks and stable private IP allocation.
- [x] 2.2 Rename or migrate Incus-specific runtime metadata in the control-plane data model so machine records are VM-neutral.
- [x] 2.3 Implement VM clone support with consistent guest-disk copy semantics and cleanup on failure.
- [x] 2.4 Update control-plane tests to run against the new runtime behavior and preserve current quota and ownership checks.

## 3. Guest session and routing migration

- [x] 3.1 Introduce a guest-session transport abstraction so the SSH frontdoor no longer depends on `incus exec`.
- [x] 3.2 Implement host-to-guest SSH shell sessions for VM-backed machines, including PTY resize handling and clean error propagation.
- [x] 3.3 Move the tutorial flow onto the guest-session transport so Claude launches inside the VM guest and tutorial completion behavior stays the same.
- [x] 3.4 Update machine app proxying to use VM guest private IPs and preserve the current port-3000 routing and status-page behavior.

## 4. Cleanup and verification

- [ ] 4.1 Add host and integration smoke tests that prove create, shell, tutorial, app routing, clone, and delete on the OVH bare metal host.
- [ ] 4.2 Remove Incus-specific runtime, bootstrap, image-build, and shell-session code paths once the Cloud Hypervisor replacements pass the smoke suite.
- [ ] 4.3 Document the new single-runtime host and recovery model for OVH, including what to snapshot or back up before future substrate work.
- [ ] 4.4 Validate the rewrite from a clean host state and clear any leftover test state so Fascinate is ready for normal use on the VM substrate.
