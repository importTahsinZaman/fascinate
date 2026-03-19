## ADDED Requirements

### Requirement: Fascinate machines SHALL run as Cloud Hypervisor virtual machines
Fascinate SHALL provision new machines as Cloud Hypervisor virtual machines on the host rather than as Incus containers. Each created machine SHALL preserve the configured default CPU, memory, disk, and primary-port settings already enforced by the control plane.

#### Scenario: Creating a machine provisions a VM
- **WHEN** an authenticated user creates a new machine
- **THEN** the control plane creates a Cloud Hypervisor guest with the configured default CPU, memory, disk, and primary port values
- **AND** the created machine is tracked as a running Fascinate machine with a private guest network identity

#### Scenario: Machine size policy is enforced for VM creation
- **WHEN** a machine request would exceed the configured per-machine resource limits or per-user machine count
- **THEN** Fascinate rejects the request before creating a Cloud Hypervisor guest

### Requirement: Fascinate SHALL route machine traffic to guest private networking
Fascinate SHALL proxy `https://<machine>.<base-domain>` traffic to the machine’s private guest address and configured primary port without requiring a public IP on the guest.

#### Scenario: Public traffic reaches a running guest app
- **WHEN** a machine is running an application on its primary port
- **THEN** requests to `https://<machine>.<base-domain>` are proxied to the guest’s private address and primary port

#### Scenario: Machine exists but no app is listening
- **WHEN** a machine exists but nothing is listening on its primary port
- **THEN** Fascinate returns its machine status page instead of a transport error

### Requirement: Fascinate SHALL support VM-based clone and delete lifecycle operations
Fascinate SHALL clone and delete machines by operating on VM guest disks and runtime metadata while preserving the existing control-plane workflows for clone and delete.

#### Scenario: Cloning creates an independent VM machine
- **WHEN** an authenticated owner clones an existing machine to a new name
- **THEN** Fascinate creates a new VM-backed machine with the source machine’s disk state and configured primary port
- **AND** the cloned machine starts independently of the source machine

#### Scenario: Deleting removes VM runtime resources
- **WHEN** an authenticated owner deletes a machine
- **THEN** Fascinate removes the VM process and guest disk resources for that machine
- **AND** the machine no longer appears as a live runtime instance
