## Why

Fascinate currently clones VM disks and boots a fresh machine, which preserves files but not the live development environment. To make cloning feel like a true agent workspace copy, Fascinate needs full-machine snapshots that can capture memory and device state and restore a VM exactly where it left off.

## What Changes

- Add first-class VM snapshots that capture the full machine state, including disk, memory, and device state.
- Allow users to create snapshots of any VM on demand and keep them as saved reusable artifacts.
- Allow new VM creation to start from one of the user’s saved snapshots instead of only from the base image.
- Change VM cloning to automatically create a snapshot-backed copy so the cloned VM resumes with dev servers, Docker containers, and in-memory state intact.
- **BREAKING** Replace the current disk-only clone semantics with snapshot-based true cloning.
- **BREAKING** Replace the current Cloud Hypervisor machine lifecycle assumptions with snapshot-aware VM restore semantics.

## Capabilities

### New Capabilities
- `vm-snapshots`: Full-VM snapshot creation, storage, restore, snapshot-based machine creation, and true cloning behavior.

### Modified Capabilities

## Impact

- Cloud Hypervisor runtime, machine metadata, and VM lifecycle orchestration.
- Control-plane APIs and TUI flows for snapshot creation, snapshot selection during create, and clone behavior.
- Host storage layout and ops tooling for snapshot assets, retention, and cleanup.
- End-to-end smoke tests and regression coverage for create, clone, restore, and delete flows.
