## 1. Framework foundations

- [x] 1.1 Add host configuration and key material for encrypted per-user tool-auth profile storage.
- [x] 1.2 Introduce a generic `ToolAuthProfile` model keyed by user, tool, auth method, and storage mode.
- [x] 1.3 Implement encrypted host storage and rollback backup behavior for persisted tool-auth profiles.

## 2. Adapter model and restore/capture plumbing

- [x] 2.1 Define the adapter interface for `session_state`, `secret_material`, and `provider_credentials` persistence modes.
- [x] 2.2 Implement reusable host helpers for versioned session-state path manifests, guest restore, and guest capture.
- [x] 2.3 Integrate supported auth-profile restore into VM provisioning so hydration completes before a machine becomes `RUNNING`.
- [x] 2.4 Add guest ownership and permission handling for restored tool-auth files and directories.

## 3. Claude subscription first adapter

- [x] 3.1 Implement the Claude subscription `session_state` adapter with its initial Linux guest path manifest.
- [x] 3.2 Capture Claude auth updates on frontdoor shell/tutorial exit and on VM stop/delete paths.
- [x] 3.3 Add a background sync worker so Claude login state becomes available to later VMs without requiring the first VM to be destroyed.
- [x] 3.4 Treat captured Claude session state as exact current state so logout/reset propagates to later VMs.

## 4. Validation and operations

- [x] 4.1 Add unit and integration coverage for profile storage, adapter isolation, hydrate fallback, and capture replacement.
- [x] 4.2 Add a host smoke flow that proves: first Claude VM requires login, second Claude VM restores that login, and logout in one VM removes it for later VMs.
- [x] 4.3 Document the security and recovery model for persistent tool auth on the OVH host, including key rotation and bundle cleanup procedures.
