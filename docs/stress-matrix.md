# Fascinate Stress Validation Matrix

This document defines the current Fascinate product expectations and maps each expectation to automated coverage. It is the operator checklist for answering "does Fascinate work as advertised on the live host?"

## Expectations

| Area | Expectation | Coverage |
| --- | --- | --- |
| VM create | `POST /v1/machines` returns quickly and the VM reaches `RUNNING` only after guest access is actually ready | `internal/controlplane/service_test.go`, `ops/host/smoke.sh`, `ops/host/stress.sh` |
| Concurrent multi-user create | Simultaneous creates across different users do not collide in runtime network allocation, and per-user machine limits are enforced independently | `internal/runtime/cloudhypervisor/runtime_test.go`, manual live concurrent-load validation |
| Shared CPU target shape | On the one-box default config, users can exceed their soft CPU entitlement when host headroom exists, and the platform target is about `8` users with about `5` active default VMs each | `internal/controlplane/service_test.go`, manual live multi-user validation, `ops/host/diagnostics.sh hosts`, `ops/host/diagnostics.sh budgets <owner_email>` |
| Guest readiness | A `RUNNING` machine has usable guest access for browser terminals, expected guest tooling, and stable forwarders | `internal/browserterm/manager_test.go`, `ops/host/smoke.sh`, `ops/host/stress.sh` |
| Shell entry | Opening a browser shell works without malformed handoff or early-boot guest race failures | `internal/browserterm/manager_test.go`, `ops/host/smoke.sh`, `ops/host/stress.sh` |
| Public app routing | `https://<machine>.<base-domain>` reaches the current primary-port workload or the fallback "No services detected" page | `internal/httpapi/server_test.go`, `ops/host/smoke.sh`, `ops/host/smoke-snapshots.sh`, `ops/host/stress.sh` |
| Local workloads | A VM can run a public app server, a local database process, and Docker containers at the same time | `ops/host/stress.sh` |
| Heavier SQL workload | A VM can serve a live app backed by a Dockerized PostgreSQL workload with meaningful row volume and benchmark traffic | Manual live SQL workload validation |
| Service restart | Restarting `fascinate` does not kill running VMs or break routing/forwarders | `ops/host/smoke.sh`, `ops/host/stress.sh` |
| Snapshot save | Saving a snapshot from a running VM succeeds and records snapshot artifacts durably | `ops/host/smoke-snapshots.sh`, `ops/host/stress.sh` |
| Create from snapshot | A VM created from a saved snapshot restores the captured machine state instead of booting from source files alone | `ops/host/smoke-snapshots.sh`, `ops/host/stress.sh` |
| True fork | Forking a running VM produces a live independent copy with the captured app/process/container state already running | `ops/host/smoke-snapshots.sh`, `ops/host/stress.sh` |
| Fork independence | After fork completes, source and fork can diverge independently without shared runtime side effects | `ops/host/smoke-snapshots.sh`, `ops/host/stress.sh` |
| Cleanup | Deleting machines and snapshots removes runtime dirs, forwarders, netns/veth artifacts, and snapshot artifact dirs | `ops/host/smoke-snapshots.sh`, `ops/host/stress.sh` |
| Tool auth persistence | Supported tool auth persists across later VMs for the same user and is not silently clobbered by opportunistic empty syncs | `internal/toolauth/manager_test.go`, `internal/controlplane/service_test.go`, targeted `ops/host/smoke-tool-auth.sh` runs |
| Tool auth diagnostics | Capture/restore failures for Claude, Codex, or GitHub auth are visible to operators | `internal/httpapi/server_test.go`, `ops/host/diagnostics.sh` |
| Lifecycle diagnostics | Operators can inspect machine/snapshot runtime handles, lifecycle failures, forwarder state, and recent events without manual host forensics | `internal/httpapi/server_test.go`, `ops/host/diagnostics.sh` |
| Host diagnostics | Operators can inspect registered hosts, heartbeat freshness, and current default-machine placement eligibility | `internal/controlplane/hosts_test.go`, `internal/httpapi/server_test.go`, `ops/host/diagnostics.sh hosts` |
| Budget diagnostics | Operators can inspect per-user soft CPU entitlement, nominal active CPU demand, hard RAM/storage usage, power-state counts, retained snapshot count, and remaining headroom | `internal/controlplane/service_test.go`, `internal/httpapi/server_test.go`, `ops/host/diagnostics.sh budgets` |
| Shared CPU ceiling rejection | When the host shared CPU ceiling is saturated, create/fork/restore fail with an explicit shared host CPU pressure error instead of a stale per-user CPU quota message | `internal/controlplane/service_test.go`, manual live validation |
| Host reboot survival | After a full host reboot, the control plane restarts, running VMs are recovered, and guest workloads configured for guest boot come back without manual host repair | `internal/controlplane/service_test.go`, manual live reboot validation |

## Live Validation Entry Points

- Basic lifecycle smoke: `sudo ./ops/host/smoke.sh`
- Snapshot and fork smoke: `sudo ./ops/host/smoke-snapshots.sh`
- Tool-auth persistence harness: `sudo ./ops/host/smoke-tool-auth.sh`
- Full workload stress pass: `sudo ./ops/host/stress.sh`
- Timing benchmark: `sudo ./ops/host/benchmark.sh`
- Diagnostics helper:
  - `sudo ./ops/host/diagnostics.sh hosts`
  - `sudo ./ops/host/diagnostics.sh budgets <owner_email>`
  - `sudo ./ops/host/diagnostics.sh machine <owner_email> <machine_name>`
  - `sudo ./ops/host/diagnostics.sh snapshot <owner_email> <snapshot_name>`
  - `sudo ./ops/host/diagnostics.sh tool-auth <owner_email>`
  - `sudo ./ops/host/diagnostics.sh events <owner_email> [limit]`

## Interpreting Failures

- If a machine is `RUNNING` but the app URL is wrong, inspect machine diagnostics first to distinguish forwarder failure from guest workload failure.
- If a snapshot or fork fails, inspect both snapshot diagnostics and owner events; failure stage and error details should be recorded there.
- If tool auth does not persist, inspect tool-auth diagnostics and owner events before touching guest files directly.
- If create/fork/restore starts failing under load, check `shared_cpu_ceiling`, `nominal_active_cpu`, and `shared_cpu_remaining` on hosts before changing per-user budgets.
- For one-box capacity validation, confirm that a user can exceed their soft CPU entitlement while `host_shared_cpu.remaining` is positive, and that the eventual rejection is reported as shared host CPU pressure rather than a hard user CPU quota.
