# fascinate

`fascinate` is a browser-first command center for persistent developer machines.

Fascinate now has two first-class user surfaces built on one control plane:
- the web command center
- the `fascinate` CLI for humans and AI agents

Shared shells, machine lifecycle, snapshots, env vars, diagnostics, and agent-oriented non-interactive exec all flow through the same backend APIs and owner-scoped event stream. A shell is now a durable backend resource rather than a browser-local window, so CLI and web clients see the same shell inventory and can attach to the same live session.

This repo now contains:
- a React/Vite web app under [`web/`](/Users/tahsin/Desktop/vmcloud/web)
- a reproducible host bootstrap path under [`ops/host/bootstrap.sh`](/Users/tahsin/Desktop/vmcloud/ops/host/bootstrap.sh)
- an off-host full-release artifact builder under [`ops/release/build-full-artifact.sh`](/Users/tahsin/Desktop/vmcloud/ops/release/build-full-artifact.sh)
- an off-host full deploy wrapper under [`ops/release/deploy-full-artifact.sh`](/Users/tahsin/Desktop/vmcloud/ops/release/deploy-full-artifact.sh)
- an off-host web-only artifact builder under [`ops/release/build-web-artifact.sh`](/Users/tahsin/Desktop/vmcloud/ops/release/build-web-artifact.sh)
- an off-host web-only deploy wrapper under [`ops/release/deploy-web-artifact.sh`](/Users/tahsin/Desktop/vmcloud/ops/release/deploy-web-artifact.sh)
- a packaged full-release installer under [`ops/host/install-control-plane.sh`](/Users/tahsin/Desktop/vmcloud/ops/host/install-control-plane.sh)
- a packaged no-restart web installer under [`ops/host/deploy-web.sh`](/Users/tahsin/Desktop/vmcloud/ops/host/deploy-web.sh)
- a Caddy config writer under [`ops/host/write-caddyfile.sh`](/Users/tahsin/Desktop/vmcloud/ops/host/write-caddyfile.sh)
- a VM base image builder under [`ops/cloudhypervisor/build-base-image.sh`](/Users/tahsin/Desktop/vmcloud/ops/cloudhypervisor/build-base-image.sh)
- a Go control plane under [`cmd/fascinate/main.go`](/Users/tahsin/Desktop/vmcloud/cmd/fascinate/main.go)
- SQLite migrations for product, auth, host, and workspace state
- a first-class host registry and local-host heartbeat model
- a Cloud Hypervisor runtime wrapper and health endpoints
- browser auth with emailed verification codes and DB-backed web sessions
- a browser terminal gateway with PTY-backed WebSocket shells
- bearer-token CLI auth and a first-class user CLI for machine, snapshot, env-var, diagnostics, shell, and exec workflows
- durable shared shell resources backed by persistent tmux sessions inside each VM
- owner-scoped SSE synchronization for machine, snapshot, shell, and exec lifecycle updates
- agent-optimized non-interactive command execution with structured stdout, stderr, exit, timeout, and cancellation outcomes
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
  - authenticate CLI clients through durable bearer tokens minted from the same email-code identity flow
  - persist shared shell records and stream shell attachments over dedicated WebSockets
  - execute non-interactive machine-scoped commands without requiring a preexisting shell
  - publish owner-scoped real-time events over SSE so CLI and web clients stay synchronized
  - inspect shell-scoped git working trees through a live shell-header git-status chip with distinct idle/open states, repeat-toggle and `Escape` dismissal, and a unified review overlay above a shell-first control sidebar, with a repo-summary header, branch-chip and animated refresh chrome, sticky stacked file cards, full-width collapsed-context bars with quiet static link chrome, compact review-grade diff chrome with consistent diff-count accents, insertion-only inline token highlights for pure add-side expansions, inline path copy affordances with visible copy feedback, a centered clean-repo empty state, scroll-ahead batched file loading that prefetches visible cards before you reach them, backend-batched per-file patch fetches with per-machine git-command throttling, lazy-loaded Shiki syntax highlighting across the major source languages, a rigid horizontally scrollable shell strip with drag-to-reorder headers instead of a zoomable free canvas, and a separate machine inventory block for machine actions and shell launch
  - bridge terminal-driven clipboard copy requests into the browser's local clipboard when supported, keep persistent tmux sessions in normal text-selection mode, translate wheel scrolling into fine-grained tmux history navigation when local xterm scrollback is empty across pixel- and line-based wheel devices, and honor `Cmd-C`/`Ctrl-C` for active terminal text selections without sending an interrupt into the shell
  - persist per-user workspace layouts for the browser terminal shell strip

Current API surface:
- `POST /v1/cli/auth/request-code`
- `POST /v1/cli/auth/verify`
- `GET /v1/cli/auth/session`
- `POST /v1/cli/auth/logout`
- `POST /v1/auth/request-code`
- `POST /v1/auth/verify`
- `GET /v1/auth/session`
- `POST /v1/auth/logout`
- `GET /v1/events/stream`
- `GET /v1/workspaces/default`
- `PUT /v1/workspaces/default`
- `GET /v1/shells`
- `POST /v1/shells`
- `GET /v1/shells/{id}`
- `DELETE /v1/shells/{id}`
- `POST /v1/shells/{id}/attach`
- `POST /v1/shells/{id}/input`
- `GET /v1/shells/{id}/lines`
- `GET /v1/execs`
- `POST /v1/execs`
- `GET /v1/execs/{id}`
- `POST /v1/execs/{id}/cancel`
- `GET /v1/execs/{id}/stream`
- `POST /v1/terminal/sessions`
- `GET /v1/terminal/sessions/{id}/stream`
- `POST /v1/terminal/sessions/{id}/git/status`
  - `POST /v1/terminal/sessions/{id}/git/diffs`
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
- `GET /v1/diagnostics/budgets`
- `GET /v1/diagnostics/tool-auth`
- `GET /v1/diagnostics/machines/{name}`
- `GET /v1/diagnostics/snapshots/{name}`
- `GET /v1/diagnostics/terminal-sessions`
- `GET /v1/diagnostics/execs`

Deleted machine and snapshot names are released immediately, so the same name can be reused after `DELETE /v1/machines/{name}` or `DELETE /v1/snapshots/{name}` succeeds.
Deleting a machine also closes any browser terminal sessions for that machine, removes their shell windows from the browser workspace immediately, and collapses the machine card to a right-edge spinner while the delete is in flight.
Fresh machine creation now stays pending until guest bootstrap is actually ready, and the machine card shows only a right-edge spinner until the VM reaches a usable `running` state.
Machines use hidden baseline sizing. CPU is now a soft per-user entitlement against a host-shared CPU ceiling, RAM remains a hard active-machine budget, and retained machine disk plus retained snapshot artifacts continue to count against disk limits.
There is no user-facing stop/start control. To free active compute without keeping a live VM around, save a snapshot, delete the VM, and later restore a new VM from that snapshot.

## Repo Layout

- [`web/`](/Users/tahsin/Desktop/vmcloud/web): React/Vite browser command center and ordered xterm shell strip
- [`ops/host/bootstrap.sh`](/Users/tahsin/Desktop/vmcloud/ops/host/bootstrap.sh): installs host dependencies and baseline VM networking/runtime config
- [`ops/host/verify.sh`](/Users/tahsin/Desktop/vmcloud/ops/host/verify.sh): checks the host after bootstrap
- [`ops/release/build-full-artifact.sh`](/Users/tahsin/Desktop/vmcloud/ops/release/build-full-artifact.sh): builds a versioned full release bundle off-host
- [`ops/release/build-web-artifact.sh`](/Users/tahsin/Desktop/vmcloud/ops/release/build-web-artifact.sh): builds a versioned web-only bundle off-host
- [`ops/release/deploy-full-artifact.sh`](/Users/tahsin/Desktop/vmcloud/ops/release/deploy-full-artifact.sh): uploads and installs a full release artifact over SSH
- [`ops/release/deploy-web-artifact.sh`](/Users/tahsin/Desktop/vmcloud/ops/release/deploy-web-artifact.sh): uploads and installs a web-only artifact over SSH without restarting `fascinate`
- [`ops/host/write-caddyfile.sh`](/Users/tahsin/Desktop/vmcloud/ops/host/write-caddyfile.sh): writes the host Caddy config for Fascinate
- [`ops/host/install-control-plane.sh`](/Users/tahsin/Desktop/vmcloud/ops/host/install-control-plane.sh): packaged installer executed from an unpacked full artifact on the host
- [`ops/host/reset-runtime-state.sh`](/Users/tahsin/Desktop/vmcloud/ops/host/reset-runtime-state.sh): destructive clean-state helper for wiping persisted VM/snapshot runtime state before a rollout
- [`ops/host/deploy-web.sh`](/Users/tahsin/Desktop/vmcloud/ops/host/deploy-web.sh): packaged web-only installer executed from an unpacked artifact on the host
- [`ops/host/smoke.sh`](/Users/tahsin/Desktop/vmcloud/ops/host/smoke.sh): validates the basic create, route, restart, and delete lifecycle
- [`ops/host/benchmark.sh`](/Users/tahsin/Desktop/vmcloud/ops/host/benchmark.sh): prints structured timing metrics for create, snapshot, restore, and fork
- [`ops/host/stress.sh`](/Users/tahsin/Desktop/vmcloud/ops/host/stress.sh): validates realistic app, local DB, Docker, restart, snapshot, restore, fork, divergence, and cleanup behavior
- [`ops/host/diagnostics.sh`](/Users/tahsin/Desktop/vmcloud/ops/host/diagnostics.sh): queries host, budget, machine, snapshot, tool-auth, terminal-session, event, and installed-release diagnostics from a configured host
- [`ops/host/smoke-tool-auth.sh`](/Users/tahsin/Desktop/vmcloud/ops/host/smoke-tool-auth.sh): targeted persistence harness for Claude, Codex, and GitHub auth across later VMs
- [`ops/host/smoke-snapshots.sh`](/Users/tahsin/Desktop/vmcloud/ops/host/smoke-snapshots.sh): validates saved snapshots, create-from-snapshot, and true fork flows
- [`docs/stress-matrix.md`](/Users/tahsin/Desktop/vmcloud/docs/stress-matrix.md): expectation matrix and operator guidance for live validation
- [`docs/development-and-deploy.md`](/Users/tahsin/Desktop/vmcloud/docs/development-and-deploy.md): local UI development loop and deploy workflow guidance
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

### CLI Install

If the public install script is being served from the Fascinate origin, users can install the CLI with:

```bash
curl -fsSL https://fascinate.dev/install.sh | bash
```

The curl bootstrap installs only the user CLI. It resolves the latest or pinned CLI artifact from a public release index, verifies the archive checksum before installation, and installs `fascinate` into `~/.local/bin` by default.

To publish a new public CLI release:

```bash
export FASCINATE_DEPLOY_HOST=fascinate.dev
export FASCINATE_DEPLOY_USER=ubuntu
export FASCINATE_DEPLOY_PORT=2220
bash ./ops/release/publish-cli-release.sh --version 0.1.0 --latest 0.1.0
```

That command:
- builds CLI-only artifacts for the supported Linux and macOS targets
- updates the public CLI release index served from `https://downloads.fascinate.dev/cli/index.json`
- publishes the stable installer at `https://fascinate.dev/install.sh`
- uploads the artifacts into the host public-assets directory so they are immediately downloadable through the live Fascinate service

Useful CLI workflows:

```bash
fascinate help
fascinate help --json agents
fascinate login --email you@example.com
fascinate machine list
fascinate machine create my-machine
fascinate shell create my-machine
fascinate shell attach <shell-id>
fascinate exec my-machine -- pwd
fascinate diagnostics execs --json
```

The CLI is optimized for automation:
- `--json` prints only structured JSON to stdout
- `--jsonl` streams ordered exec events to stdout for agents
- collection-style `--json` commands use named top-level keys like `machines`, `snapshots`, `shells`, `lines`, `hosts`, and `events`
- human prompts stay off stdout and destructive commands require `--yes` when stdin/stdout are not interactive
- `fascinate help --json [topic]` returns machine-readable onboarding and command reference data

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
sudo ./ops/host/diagnostics.sh budgets you@example.com
sudo ./ops/host/diagnostics.sh machine you@example.com machine-name
sudo ./ops/host/diagnostics.sh snapshot you@example.com snapshot-name
sudo ./ops/host/diagnostics.sh tool-auth you@example.com
sudo ./ops/host/diagnostics.sh events you@example.com 100
sudo ./ops/host/diagnostics.sh release-manifest
```

The `hosts` diagnostics output includes `placement_eligible`, which currently means the host is active, has a fresh heartbeat, and can fit a default-size Fascinate machine right now.

Notes:
- the bootstrap script assumes a fresh host or a host you are willing to standardize
- it installs Cloud Hypervisor plus qcow2/cloud-init image tooling
- it bootstraps artifact-consumer hosts, not host-local build machines
- standard deploys do not require a synchronized repo checkout, Go, Node, `pnpm`, or `corepack` on the host
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
- host admin SSH typically uses `ssh -p 2220 ubuntu@fascinate.dev`

Deploy or redeploy the full Fascinate service from a workstation or CI runner:

```bash
export FASCINATE_DEPLOY_HOST=fascinate.dev
export FASCINATE_DEPLOY_USER=ubuntu
export FASCINATE_DEPLOY_PORT=2220
export FASCINATE_BASE_DOMAIN=fascinate.dev
export FASCINATE_ACME_EMAIL=you@example.com
export FASCINATE_ADMIN_EMAILS=you@example.com
bash ./ops/release/deploy-full-artifact.sh
```

For frontend-only changes, prefer the no-restart path:

```bash
bash ./ops/release/deploy-web-artifact.sh
```

Use the frontend-only deploy when you are only shipping web changes and want to avoid disconnecting active browser shell attachments. It uploads a prebuilt `web/dist`, preserves older hashed assets for already-open tabs, swaps `index.html` last, and leaves the running `fascinate` process alone.

Artifact bundles are written to `.tmp/releases/` by default. Both deploy wrappers can either consume a supplied `.tar.gz` bundle or build a fresh artifact before upload.

Each successful artifact install also records live release metadata under `/opt/fascinate/release-manifest.json` and keeps the full unpacked bundle under `/opt/fascinate/releases/<release-id>`.

Startup and readiness:

- Fascinate now binds the HTTP server before initial VM recovery runs.
- `/healthz` tells you whether the web process is serving.
- `/readyz` tells you whether startup recovery is finished.
- During startup recovery, the site can stay reachable while `/readyz` temporarily reports `startup recovery in progress`.

Important for Cloudflare:
- the generated wildcard Caddy block uses `tls internal`
- that means Cloudflare should use `Full` mode for proxied wildcard traffic unless you replace the wildcard TLS block with an Origin CA certificate
- the apex `fascinate.dev` site still gets a normal public cert from Caddy because it is `DNS only`

### Local Development

The documented local workflow lives in [`docs/development-and-deploy.md`](/Users/tahsin/Desktop/vmcloud/docs/development-and-deploy.md). The short version is:

- `make run` for the local Go server
- `make web-dev` for the Vite frontend dev server with API and WebSocket proxying to `http://127.0.0.1:8080`
- `make web-dev-mock` for UI-only work with a seeded mock control plane and browser-rendered mock terminals

If you want a local production-style build of the browser app and control plane:

```bash
make build
```

For backend-only development you can still run:

```bash
make run
```

If you want the Go server to serve local production assets directly, build the web bundle first with `make web-build` (or just run `make build`) so the server can serve `web/dist`.

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
export FASCINATE_PUBLIC_ASSETS_DIR=./public
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
export FASCINATE_DEFAULT_USER_MAX_CPU=5
export FASCINATE_DEFAULT_USER_MAX_RAM=10GiB
export FASCINATE_DEFAULT_USER_MAX_DISK=80GiB
export FASCINATE_DEFAULT_USER_MAX_MACHINES=5
export FASCINATE_DEFAULT_USER_MAX_SNAPSHOTS=5
export FASCINATE_MAX_MACHINES_PER_USER=5
export FASCINATE_MAX_MACHINE_CPU=2
export FASCINATE_MAX_MACHINE_RAM=4GiB
export FASCINATE_MAX_MACHINE_DISK=20GiB
export FASCINATE_HOST_MIN_FREE_DISK=150GiB
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

## Hidden Sizing And Limits

Fascinate now treats machine sizing as an internal baseline instead of a user-facing control.

Current behavior:
- new machines are created from one hidden baseline shape
- CPU is a soft user entitlement and a host-shared pool, not a strict reserved-per-user quota
- RAM is a hard per-user active-machine limit
- deleting a machine frees active CPU and RAM budget; snapshots let users preserve state for later restore
- retained machine disk plus retained snapshot artifacts still count against disk limits
- the separate product cap of `5` machines and `5` retained snapshots per user still applies
- browser shells, fork, snapshot, and public routes are only available when a machine is fully `RUNNING`

Current single-box defaults are tuned for the live OVH host target of about `8` users with about `5` active default VMs each:
- hidden default machine size: `1 vCPU / 2 GiB RAM / 20 GiB root disk`
- soft per-user CPU entitlement: `5`
- hard per-user RAM budget: `10 GiB`
- hard per-user retained-storage budget: `80 GiB`
- host shared CPU overcommit ratio: `1.67` (about `40` nominal active vCPU on a `24` thread host)

## Fascinate Env Vars

Fascinate now keeps plain user-defined env vars as a first-class per-user object and combines them with built-in machine identity vars inside every VM.

Built-in machine vars currently include:
- `FASCINATE_MACHINE_NAME` — name of the current VM (example: `tic-tac-toe`)
- `FASCINATE_MACHINE_ID` — stable Fascinate ID for the current VM (example: `machine-1`)
- `FASCINATE_PUBLIC_URL` — public HTTPS URL for the current VM, routed to its primary port (example: `https://tic-tac-toe.fascinate.dev`)
- `FASCINATE_PRIMARY_PORT` — primary port Fascinate exposes for the current VM (example: `3000`)
- `FASCINATE_BASE_DOMAIN` — base domain Fascinate uses to generate machine URLs (example: `fascinate.dev`)
- `FASCINATE_HOST_ID` — ID of the host currently running the VM (example: `fascinate-01`)
- `FASCINATE_HOST_REGION` — region advertised by the host currently running the VM (example: `ca-east`)

The browser env-vars modal lists these built-ins with descriptions so users can safely reference them when composing saved vars.

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
- Budget diagnostics surface each user's aggregate CPU, memory, disk, machine-count, and snapshot-count limits plus active compute usage, retained storage usage, power-state counts, and remaining headroom.

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
