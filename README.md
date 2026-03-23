# fascinate

`fascinate` is a browser-first command center for persistent developer machines.

This repo now contains:
- a React/Vite web app under [`web/`](/Users/tahsin/Desktop/vmcloud/web)
- a reproducible host bootstrap path under [`ops/host/bootstrap.sh`](/Users/tahsin/Desktop/vmcloud/ops/host/bootstrap.sh)
- a host redeploy path under [`ops/host/install-control-plane.sh`](/Users/tahsin/Desktop/vmcloud/ops/host/install-control-plane.sh)
- a Caddy config writer under [`ops/host/write-caddyfile.sh`](/Users/tahsin/Desktop/vmcloud/ops/host/write-caddyfile.sh)
- a VM base image builder under [`ops/cloudhypervisor/build-base-image.sh`](/Users/tahsin/Desktop/vmcloud/ops/cloudhypervisor/build-base-image.sh)
- a Go control plane under [`cmd/fascinate/main.go`](/Users/tahsin/Desktop/vmcloud/cmd/fascinate/main.go)
- SQLite migrations for product, auth, host, and workspace state
- a first-class host registry and local-host heartbeat model
- a Cloud Hypervisor runtime wrapper and health endpoints
- browser auth with emailed verification codes and DB-backed web sessions
- a browser terminal gateway with PTY-backed WebSocket shells
- first-class full-VM snapshots and snapshot-backed forking

## Current Product Surface

Fascinate currently gives us:
- repeatable host setup for Ubuntu 24.04
- a baseline Cloud Hypervisor + Caddy + firewall install
- a Go service that can:
  - load config from env
  - initialize SQLite
  - run migrations
  - expose `/healthz`, `/readyz`, and runtime/diagnostics APIs
  - register the current box as a first-class host and heartbeat its capacity
  - talk to the local `cloud-hypervisor` runtime through a host executor boundary
  - provision persistent VMs asynchronously
  - save full-VM snapshots and restore new VMs from them
  - perform true snapshot-backed forking
  - persist per-user Fascinate env vars and rewrite machine-specific built-ins on create, restore, and fork
  - persist supported tool auth across later VMs for the same user
  - serve the browser command center on the primary Fascinate origin
  - authenticate browser users through emailed verification codes and DB-backed sessions
  - issue browser terminal sessions and stream PTY traffic over dedicated WebSockets
  - inspect shell-scoped git working trees in a unified review overlay above the control sidebar with a repo-summary header, branch-chip and animated refresh chrome, sticky stacked file cards, full-width collapsed-context bars with quiet static link chrome, compact review-grade diff chrome, inline path copy affordances with visible copy feedback, scroll-batched file loading, Shiki syntax highlighting, and without resizing the workspace canvas
  - bridge terminal-driven clipboard copy requests into the browser's local clipboard when supported
  - persist per-user workspace layouts for the browser terminal canvas

Current browser HTTP API:
- `POST /v1/auth/request-code`
- `POST /v1/auth/verify`
- `GET /v1/auth/session`
- `POST /v1/auth/logout`
- `GET /v1/workspaces/default`
- `PUT /v1/workspaces/default`
- `POST /v1/terminal/sessions`
- `GET /v1/terminal/sessions/{id}/stream`
- `POST /v1/terminal/sessions/{id}/git/status`
- `POST /v1/terminal/sessions/{id}/git/diff`
- `GET /v1/machines`
- `POST /v1/machines`
- `GET /v1/machines/{name}`
- `GET /v1/machines/{name}/env`
- `DELETE /v1/machines/{name}`
- `POST /v1/machines/{name}/fork`
- `GET /v1/env-vars`
- `PUT /v1/env-vars`
- `DELETE /v1/env-vars/{key}`
- `GET /v1/snapshots`
- `POST /v1/snapshots`
- `DELETE /v1/snapshots/{name}`
- `GET /v1/diagnostics/events`
- `GET /v1/diagnostics/hosts`
- `GET /v1/diagnostics/tool-auth`
- `GET /v1/diagnostics/machines/{name}`
- `GET /v1/diagnostics/snapshots/{name}`
- `GET /v1/diagnostics/terminal-sessions`

## Repo Layout

- [`web/`](/Users/tahsin/Desktop/vmcloud/web): React/Vite browser command center and xterm workspace canvas
- [`ops/host/bootstrap.sh`](/Users/tahsin/Desktop/vmcloud/ops/host/bootstrap.sh): installs host dependencies and baseline VM networking/runtime config
- [`ops/host/verify.sh`](/Users/tahsin/Desktop/vmcloud/ops/host/verify.sh): checks the host after bootstrap
- [`ops/host/write-caddyfile.sh`](/Users/tahsin/Desktop/vmcloud/ops/host/write-caddyfile.sh): writes the host Caddy config for Fascinate
- [`ops/host/install-control-plane.sh`](/Users/tahsin/Desktop/vmcloud/ops/host/install-control-plane.sh): builds and installs the Fascinate service and web bundle on a host
- [`ops/host/smoke.sh`](/Users/tahsin/Desktop/vmcloud/ops/host/smoke.sh): validates the basic create, route, restart, and delete lifecycle
- [`ops/host/benchmark.sh`](/Users/tahsin/Desktop/vmcloud/ops/host/benchmark.sh): prints structured timing metrics for create, snapshot, restore, and fork
- [`ops/host/stress.sh`](/Users/tahsin/Desktop/vmcloud/ops/host/stress.sh): validates realistic app, local DB, Docker, restart, snapshot, restore, fork, divergence, and cleanup behavior
- [`ops/host/diagnostics.sh`](/Users/tahsin/Desktop/vmcloud/ops/host/diagnostics.sh): queries host, machine, snapshot, tool-auth, terminal-session, and event diagnostics from a configured host
- [`ops/host/smoke-tool-auth.sh`](/Users/tahsin/Desktop/vmcloud/ops/host/smoke-tool-auth.sh): targeted persistence harness for Claude, Codex, and GitHub auth across later VMs
- [`ops/host/smoke-snapshots.sh`](/Users/tahsin/Desktop/vmcloud/ops/host/smoke-snapshots.sh): validates saved snapshots, create-from-snapshot, and true fork flows
- [`docs/stress-matrix.md`](/Users/tahsin/Desktop/vmcloud/docs/stress-matrix.md): expectation matrix and operator guidance for live validation
- [`ops/cloudhypervisor/build-base-image.sh`](/Users/tahsin/Desktop/vmcloud/ops/cloudhypervisor/build-base-image.sh): builds an agent-ready qcow2 guest image
- [`ops/systemd/fascinate.service`](/Users/tahsin/Desktop/vmcloud/ops/systemd/fascinate.service): example systemd unit
- [`cmd/fascinate/main.go`](/Users/tahsin/Desktop/vmcloud/cmd/fascinate/main.go): entrypoint
- [`internal/browserauth/service.go`](/Users/tahsin/Desktop/vmcloud/internal/browserauth/service.go): browser auth and session lifecycle
- [`internal/browserterm/manager.go`](/Users/tahsin/Desktop/vmcloud/internal/browserterm/manager.go): host-local browser terminal gateway and diagnostics
- [`internal/config/config.go`](/Users/tahsin/Desktop/vmcloud/internal/config/config.go): env-backed config
- [`internal/controlplane/hosts.go`](/Users/tahsin/Desktop/vmcloud/internal/controlplane/hosts.go): host registry, heartbeat, placement, and local-host executor wiring
- [`internal/httpapi/server.go`](/Users/tahsin/Desktop/vmcloud/internal/httpapi/server.go): browser/auth/session API surface and static web serving
- [`internal/database/migrations/0001_init.sql`](/Users/tahsin/Desktop/vmcloud/internal/database/migrations/0001_init.sql): initial SQLite schema
- [`internal/runtime/cloudhypervisor/runtime.go`](/Users/tahsin/Desktop/vmcloud/internal/runtime/cloudhypervisor/runtime.go): Cloud Hypervisor VM runtime
## Quick Start

### Fresh Host

Run the bootstrap script on a fresh Ubuntu 24.04 machine:

```bash
sudo FASCINATE_HOSTNAME=fascinate-01 ./ops/host/bootstrap.sh
```

Then verify:

```bash
sudo ./ops/host/verify.sh
```

Run the host smoke path after deploys or major runtime changes:

```bash
sudo ./ops/host/smoke.sh
```

Run the workload stress path when validating app, local DB, Docker, restart, snapshot, restore, fork, and cleanup behavior together:

```bash
sudo ./ops/host/stress.sh
```

Run the benchmark path when you want structured timing metrics for bare create, snapshot, restore, and fork:

```bash
sudo ./ops/host/benchmark.sh
```

Run the automated tool-auth persistence harness when you are changing tool-auth behavior or debugging auth restore/capture issues:

```bash
sudo ./ops/host/smoke-tool-auth.sh
```

Run the snapshot smoke path when validating saved snapshots and true fork semantics:

```bash
sudo ./ops/host/smoke-snapshots.sh
```

Query live diagnostics from a configured host:

```bash
sudo ./ops/host/diagnostics.sh hosts
sudo ./ops/host/diagnostics.sh machine you@example.com machine-name
sudo ./ops/host/diagnostics.sh snapshot you@example.com snapshot-name
sudo ./ops/host/diagnostics.sh tool-auth you@example.com
sudo ./ops/host/diagnostics.sh events you@example.com 100
```

The `hosts` diagnostics output includes `placement_eligible`, which currently means the host is active, has a fresh heartbeat, and can fit a default-size Fascinate machine right now.

Notes:
- the bootstrap script assumes a fresh host or a host you are willing to standardize
- it installs Cloud Hypervisor plus qcow2/cloud-init image tooling
- it enables the kernel and package prerequisites for the namespace-based VM runtime
- guest NAT/forwarding rules are created lazily when the first VM boots
- it does not manage DNS or Cloudflare for you

Build the default agent-ready guest image:

```bash
sudo ./ops/cloudhypervisor/build-base-image.sh
```

The base image builder only prepares the raw Ubuntu cloud image. Fascinate installs the developer toolchain, Claude Code, Codex CLI, and GitHub CLI during VM first boot.

If you want host admin SSH on port `2220`, move it explicitly:

```bash
export FASCINATE_HOST_ADMIN_SSH_PORT=2220
sudo ./ops/host/configure-admin-ssh.sh
```

After that:
- host admin SSH uses `ssh -p 2220 root@fascinate.dev`

Deploy or redeploy the Fascinate service:

```bash
export FASCINATE_BASE_DOMAIN=fascinate.dev
export FASCINATE_ACME_EMAIL=you@example.com
export FASCINATE_ADMIN_EMAILS=you@example.com
sudo ./ops/host/install-control-plane.sh
```

Important for Cloudflare:
- the generated wildcard Caddy block uses `tls internal`
- that means Cloudflare should use `Full` mode for proxied wildcard traffic unless you replace the wildcard TLS block with an Origin CA certificate
- the apex `fascinate.dev` site still gets a normal public cert from Caddy because it is `DNS only`

### Local Development

Build the browser app and control plane locally:

```bash
make build
```

For backend-only development you can still run:

```bash
make run
```

If you want the browser UI locally, build the web bundle first with `make web-build` (or just run `make build`) so the Go server can serve `web/dist`. 

Then check:

```bash
curl http://127.0.0.1:8080/healthz
curl http://127.0.0.1:8080/readyz
curl http://127.0.0.1:8080/v1/runtime/machines
curl http://127.0.0.1:8080/v1/machines
```

Useful env vars:

```bash
export FASCINATE_HTTP_ADDR=127.0.0.1:8080
export FASCINATE_DATA_DIR=./data
export FASCINATE_DB_PATH=./data/fascinate.db
export FASCINATE_BASE_DOMAIN=fascinate.dev
export FASCINATE_ADMIN_EMAILS=you@example.com,ops@example.com
export FASCINATE_RUNTIME_BINARY=cloud-hypervisor
export FASCINATE_RUNTIME_STATE_DIR=./data/machines
export FASCINATE_RUNTIME_SNAPSHOT_DIR=./data/snapshots
export FASCINATE_VM_BRIDGE_NAME=fascbr0
export FASCINATE_VM_BRIDGE_CIDR=10.42.0.1/24
export FASCINATE_VM_GUEST_CIDR=10.42.0.0/24
export FASCINATE_VM_NAMESPACE_CIDR=100.96.0.0/16
export FASCINATE_VM_FIRMWARE_PATH=/usr/local/share/cloud-hypervisor/CLOUDHV.fd
export FASCINATE_QEMU_IMG_BINARY=qemu-img
export FASCINATE_CLOUD_LOCALDS_BINARY=cloud-localds
export FASCINATE_SSH_CLIENT_BINARY=ssh
export FASCINATE_GUEST_SSH_KEY_PATH=./data/guest_ssh_ed25519
export FASCINATE_GUEST_SSH_USER=ubuntu
export FASCINATE_DEFAULT_IMAGE=./data/images/fascinate-base.raw
export FASCINATE_DEFAULT_MACHINE_CPU=1
export FASCINATE_DEFAULT_MACHINE_RAM=2GiB
export FASCINATE_DEFAULT_MACHINE_DISK=20GiB
export FASCINATE_MAX_MACHINES_PER_USER=6
export FASCINATE_MAX_MACHINE_CPU=2
export FASCINATE_MAX_MACHINE_RAM=4GiB
export FASCINATE_MAX_MACHINE_DISK=20GiB
export FASCINATE_DEFAULT_PRIMARY_PORT=3000
export FASCINATE_TOOL_AUTH_DIR=./data/tool-auth
export FASCINATE_TOOL_AUTH_KEY_PATH=./data/tool_auth.key
export FASCINATE_TOOL_AUTH_SYNC_INTERVAL=2m
export FASCINATE_NODE_VERSION=latest
export FASCINATE_GO_VERSION=latest
export FASCINATE_NPM_VERSION=latest
export FASCINATE_HOST_ID=local-host
export FASCINATE_HOST_NAME=local-host
export FASCINATE_HOST_REGION=local
export FASCINATE_HOST_ROLE=combined
export FASCINATE_HOST_HEARTBEAT_INTERVAL=30s
export FASCINATE_RESEND_API_KEY=...
export FASCINATE_EMAIL_FROM='Fascinate <hello@example.com>'
export FASCINATE_RESEND_BASE_URL=https://api.resend.com
export FASCINATE_SIGNUP_CODE_TTL=15m
export FASCINATE_ACME_EMAIL=you@example.com
```

For manual host smoke runs you can also override:

```bash
export FASCINATE_SMOKE_EMAIL=smoke@example.com
export FASCINATE_SMOKE_NAME=smoke-$(date +%s)
```

For the workload stress harness you can also override:

```bash
export FASCINATE_STRESS_EMAIL=stress@example.com
export FASCINATE_STRESS_SOURCE_NAME=stress-source-$(date +%s)
export FASCINATE_STRESS_SNAPSHOT_NAME=stress-snapshot-$(date +%s)
export FASCINATE_STRESS_RESTORE_NAME=stress-restore-$(date +%s)
export FASCINATE_STRESS_FORK_NAME=stress-fork-$(date +%s)
```

Open the browser command center at [https://fascinate.dev/app](https://fascinate.dev/app) or your local `/app` route. Browser sign-in uses email verification codes; no SSH key registration flow is required.

If your host Caddy config forwards wildcard subdomains to `FASCINATE_HTTP_ADDR`, requests for `https://<machine>.fascinate.dev` are proxied to that machine's primary port. If nothing is listening yet, Fascinate serves a status page that points users back to the browser command center.

New machines built from `fascinate-base` come with:
- Ubuntu 24.04 packages upgraded to the latest versions available in the current Ubuntu repositories during VM first boot
- Docker
- Node.js and Go installed from upstream releases during VM first boot (`FASCINATE_NODE_VERSION=latest` and `FASCINATE_GO_VERSION=latest` by default)
- npm upgraded from the upstream registry during VM first boot
- Python 3, git, jq, ripgrep, sqlite3, tmux, fzf, curl, wget, unzip, zip, rsync, and common build tooling
- Claude Code available as `claude`
- Codex CLI available as `codex`
- GitHub CLI available as `gh`

## Persistent Tool Auth

Fascinate now keeps tool auth as a per-user host asset instead of a per-VM-only state.

Current scope:
- framework supports `session_state`, `secret_material`, and `provider_credentials` storage modes
- shipped session-state adapters are Claude Code subscription login, Codex ChatGPT/device-code login, and GitHub CLI login
- running VM sync happens on shell/tutorial exit and on a background interval
- later VMs for the same user restore the stored tool auth before the machine becomes `RUNNING`

## Fascinate Env Vars

Fascinate now keeps plain user-defined env vars as a first-class per-user object and combines them with built-in machine identity vars inside every VM.

Built-in machine vars currently include:
- `FASCINATE_MACHINE_NAME`
- `FASCINATE_MACHINE_ID`
- `FASCINATE_PUBLIC_URL`
- `FASCINATE_PRIMARY_PORT`
- `FASCINATE_BASE_DOMAIN`
- `FASCINATE_HOST_ID`
- `FASCINATE_HOST_REGION`

User-defined env vars:
- are stored centrally per user
- cannot override the reserved `FASCINATE_` prefix
- support `${NAME}` interpolation across built-ins and other user vars
- are rewritten into restored and forked VMs before those machines are surfaced as ready

Inside each VM, Fascinate writes:
- `/etc/fascinate/env`
- `/etc/fascinate/env.sh`
- `/etc/fascinate/env.json`
- `/etc/profile.d/fascinate-env.sh`

Use these for fork-safe app config. For example:

```env
FRONTEND_URL=${FASCINATE_PUBLIC_URL}
```

Instead of hardcoding `https://<machine>.<base-domain>`.
- shell entry shows a GitHub hint when `gh` is installed but not connected yet: `gh auth login && gh auth setup-git`
- tool-auth operator diagnostics are available under `/v1/diagnostics/tool-auth` and `ops/host/diagnostics.sh tool-auth ...`

## Diagnostics And Stress Validation

- See [`docs/stress-matrix.md`](/Users/tahsin/Desktop/vmcloud/docs/stress-matrix.md) for the current expectation matrix and coverage map.
- Machine diagnostics surface runtime handles, forwarding ports, reachability, and recent lifecycle events.
- Snapshot diagnostics surface artifact locations, runtime metadata, and recent snapshot lifecycle events.
- Owner event diagnostics surface machine, snapshot, and tool-auth lifecycle history without needing manual host log forensics.
- Host diagnostics surface registered hosts, heartbeat freshness, placement eligibility, and advertised capacity.

Host storage:
- encrypted bundles live under `FASCINATE_TOOL_AUTH_DIR`
- the encryption key lives at `FASCINATE_TOOL_AUTH_KEY_PATH`
- the current implementation stores profiles by `user_id/tool_id/auth_method_id`

Security and recovery notes:
- rotating `FASCINATE_TOOL_AUTH_KEY_PATH` invalidates existing encrypted bundles unless you re-encrypt them first
- the safe rotation flow is: stop `fascinate`, back up `FASCINATE_TOOL_AUTH_DIR` and the current key, replace the key, remove or migrate old bundles, then restart
- per-user cleanup is done by deleting the matching subtree under `FASCINATE_TOOL_AUTH_DIR`
- if host recovery is needed, restore both the tool-auth directory and its matching key backup together

## Host Model

Fascinate now has a first-class host model even in the current one-box deployment.

- every machine and snapshot record belongs to a `host_id`
- the current OVH box self-registers as the local host on startup
- the control plane heartbeats local capacity and health into the `hosts` table
- machine, snapshot, diagnostics, shell, and routing flows resolve host ownership before touching runtime state

Today that still dispatches to the local host executor, but it means the control plane is already shaped for:

- adding more VM worker boxes later
- keeping snapshot and fork operations host-local at first
- eventually moving the control plane and DB onto a smaller dedicated machine

Available exec-style SSH commands:

```bash
machines
snapshots
create habits
create habits-v2 --from-snapshot habits-snap
fork habits habits-v2
snapshot save habits habits-snap
snapshot delete habits-snap
delete habits --confirm habits
shell habits
tutorial habits
whoami
help
exit
```

## Snapshots

Fascinate snapshots are immutable per-user full-VM artifacts.

Current behavior:
- `snapshot save <machine> <name>` captures disk, memory, and device state
- `create <name> --from-snapshot <snapshot>` restores a new VM directly from a saved snapshot
- `fork <source> <target>` performs a true fork by taking an implicit snapshot and restoring it into the target VM
- snapshot-created and forked VMs keep restored state authoritative; Fascinate does not layer fresh tool-auth restore on top afterward

Runtime notes:
- each VM runs in its own Linux network namespace
- guests keep the same internal IP and MAC identity across restores
- shell access and public app routing go through host-side per-machine forwarders instead of globally unique guest IPs

## Next Milestones

1. Add browser-auth recovery and email-code abuse guardrails such as rate limits and quotas around account access.
2. Improve snapshot and fork UX in the command center, including clearer retention and cleanup flows.
3. Add more persistent tool-auth adapters beyond the current Claude, Codex, and GitHub session-state set.
