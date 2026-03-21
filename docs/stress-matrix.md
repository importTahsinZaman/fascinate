# Fascinate Stress Validation Matrix

This document defines the current Fascinate product expectations and maps each expectation to automated coverage. It is the operator checklist for answering "does Fascinate work as advertised on the live host?"

## Expectations

| Area | Expectation | Coverage |
| --- | --- | --- |
| VM create | `POST /v1/machines` returns quickly and the VM reaches `RUNNING` only after guest access is actually ready | `internal/controlplane/service_test.go`, `ops/host/smoke.sh`, `ops/host/stress.sh` |
| Guest readiness | A `RUNNING` machine has usable SSH access, expected guest tooling, and stable forwarders | `internal/sshfrontdoor/server_test.go`, `ops/host/smoke.sh`, `ops/host/stress.sh` |
| Shell entry | Entering a VM through the frontdoor works without malformed shell handoff or early-boot race failures | `internal/sshfrontdoor/server_test.go`, `ops/host/smoke.sh`, `ops/host/stress.sh` |
| Public app routing | `https://<machine>.<base-domain>` reaches the current primary-port workload or the fallback "No services detected" page | `internal/httpapi/server_test.go`, `ops/host/smoke.sh`, `ops/host/smoke-snapshots.sh`, `ops/host/stress.sh` |
| Local workloads | A VM can run a public app server, a local database process, and Docker containers at the same time | `ops/host/stress.sh` |
| Service restart | Restarting `fascinate` does not kill running VMs or break routing/forwarders | `ops/host/smoke.sh`, `ops/host/stress.sh` |
| Snapshot save | Saving a snapshot from a running VM succeeds and records snapshot artifacts durably | `ops/host/smoke-snapshots.sh`, `ops/host/stress.sh` |
| Create from snapshot | A VM created from a saved snapshot restores the captured machine state instead of booting from source files alone | `ops/host/smoke-snapshots.sh`, `ops/host/stress.sh` |
| True clone | Cloning a running VM produces a live independent copy with the captured app/process/container state already running | `ops/host/smoke-snapshots.sh`, `ops/host/stress.sh` |
| Clone independence | After clone completes, source and clone can diverge independently without shared runtime side effects | `ops/host/smoke-snapshots.sh`, `ops/host/stress.sh` |
| Cleanup | Deleting machines and snapshots removes runtime dirs, forwarders, netns/veth artifacts, and snapshot artifact dirs | `ops/host/smoke-snapshots.sh`, `ops/host/stress.sh` |
| Tool auth persistence | Supported tool auth persists across later VMs for the same user and is not silently clobbered by opportunistic empty syncs | `internal/toolauth/manager_test.go`, `internal/controlplane/service_test.go`, `ops/host/smoke-tool-auth.sh` |
| Tool auth diagnostics | Capture/restore failures for Claude, Codex, or GitHub auth are visible to operators | `internal/httpapi/server_test.go`, `ops/host/diagnostics.sh` |
| Lifecycle diagnostics | Operators can inspect machine/snapshot runtime handles, lifecycle failures, forwarder state, and recent events without manual host forensics | `internal/httpapi/server_test.go`, `ops/host/diagnostics.sh` |
| Host reboot survival | Expectations after a full host reboot are documented, but not yet covered by an automated smoke in this pass | Manual check only |

## Live Validation Entry Points

- Basic lifecycle smoke: `sudo ./ops/host/smoke.sh`
- Snapshot and clone smoke: `sudo ./ops/host/smoke-snapshots.sh`
- Tool-auth smoke: `sudo ./ops/host/smoke-tool-auth.sh`
- Full workload stress pass: `sudo ./ops/host/stress.sh`
- Diagnostics helper:
  - `sudo ./ops/host/diagnostics.sh machine <owner_email> <machine_name>`
  - `sudo ./ops/host/diagnostics.sh snapshot <owner_email> <snapshot_name>`
  - `sudo ./ops/host/diagnostics.sh tool-auth <owner_email>`
  - `sudo ./ops/host/diagnostics.sh events <owner_email> [limit]`

## Interpreting Failures

- If a machine is `RUNNING` but the app URL is wrong, inspect machine diagnostics first to distinguish forwarder failure from guest workload failure.
- If a snapshot or clone fails, inspect both snapshot diagnostics and owner events; failure stage and error details should be recorded there.
- If tool auth does not persist, inspect tool-auth diagnostics and owner events before touching guest files directly.
