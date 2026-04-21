# Fascinate

Fascinate is a browser-first command center for persistent developer VMs.

It gives the web UI and the `fascinate` CLI a shared control plane, so humans and AI agents can work against the same machines, shells, snapshots, env vars, and live state. The current system is built around Cloud Hypervisor, a Go backend, a React/Vite frontend, and host-managed VM images.

## What Fascinate Does

- Creates persistent developer VMs on an operator-managed host
- Opens browser terminals backed by durable `tmux` sessions inside each VM
- Saves full-VM snapshots and creates true snapshot-backed forks
- Persists per-user env vars and selected tool auth across later VMs
- Keeps browser and CLI clients synchronized through the same backend APIs and event stream
- Supports non-interactive command execution for automation and agent workflows
- Routes each machine to its own public subdomain on the configured base domain

## Architecture

At a high level, the repo contains:

- A Go control plane and CLI under `cmd/` and `internal/`
- A React/Vite web app under `web/`
- Host bootstrap, image lifecycle, deploy, and smoke tooling under `ops/`
- SQLite-backed product, auth, host, and workspace state
- A Cloud Hypervisor runtime with snapshot and restore support

Key directories:

- `cmd/fascinate/` - server entrypoint and user CLI
- `internal/app/` - app wiring and background loops
- `internal/browserauth/` - email-code auth and DB-backed sessions
- `internal/browserterm/` - browser terminal session management
- `internal/controlplane/` - machines, snapshots, env vars, hosts, placement
- `internal/runtime/cloudhypervisor/` - VM runtime, networking, snapshots, restore, fork
- `internal/httpapi/` - REST APIs, event streaming, and static web serving
- `internal/toolauth/` - persisted auth adapters for supported tools
- `web/` - browser command center
- `ops/` - bootstrap, build, deploy, verify, smoke, and image scripts
- `docs/` - development and deployment notes

## Local Development

Fascinate has two useful local loops:

- Full stack: run the Go backend and the Vite dev server together
- UI-only: run the web app in seeded mock mode with no live backend

### Prerequisites

- Go `1.25+`
- Node.js and `pnpm`
- Linux with Cloud Hypervisor, `qemu-img`, and `cloud-localds` if you want to exercise real VM lifecycle locally

Notes:

- Frontend work and most CLI work can be done without a bootstrapped VM host.
- Real browser sign-in requires email delivery config. Mock mode does not.
- The production host flow currently targets Ubuntu 24.04.

### Full-Stack Dev

Run the backend:

```bash
make run
```

In a second shell, run the Vite app:

```bash
make web-dev
```

The Vite server proxies API and WebSocket traffic to `http://127.0.0.1:8080` by default. Open `http://127.0.0.1:5173/app`.

If you want the Go server to serve built assets directly, build the web app first and then open `http://127.0.0.1:8080/app`:

```bash
make web-build
make run
```

### UI-Only Mock Mode

For layout and interaction work that does not need real machines:

```bash
make web-dev-mock
```

Mock mode seeds a signed-in session, sample machines, sample snapshots, sample env vars, and browser-rendered mock terminals.

### Build And Test

Common commands:

```bash
go test ./...
make web-test
make web-build
make build
```

If you changed scripts under `ops/`, also run:

```bash
make verify-ops
```

### Auth For Local Runs

The real auth flow uses email verification codes. For that path, configure:

```bash
export FASCINATE_RESEND_API_KEY=...
export FASCINATE_EMAIL_FROM="Fascinate <hello@example.com>"
```

Other useful local overrides include:

```bash
export FASCINATE_HTTP_ADDR=127.0.0.1:8080
export FASCINATE_DATA_DIR=./data
export FASCINATE_DB_PATH=./data/fascinate.db
export FASCINATE_BASE_DOMAIN=dev.example.test
```

## CLI

Build the CLI from source with:

```bash
go build -o bin/fascinate ./cmd/fascinate
```

Example workflows:

```bash
./bin/fascinate help
./bin/fascinate login --email you@example.com
./bin/fascinate machine list
./bin/fascinate machine create devbox
./bin/fascinate shell create devbox
./bin/fascinate shell attach <shell-id>
./bin/fascinate exec devbox -- pwd
```

The repo also includes `install.sh` for published CLI artifacts. By default it expects a release index at `https://downloads.fascinate.dev/cli`, and self-hosters can override that with `FASCINATE_INSTALL_BASE_URL`.

## Self-Hosting

The current deployment model assumes an operator-managed Ubuntu 24.04 host.

### 1. Bootstrap A Fresh Host

```bash
sudo FASCINATE_HOSTNAME=fascinate-01 ./ops/host/bootstrap.sh
sudo ./ops/host/verify.sh
```

The bootstrap path installs the system packages and runtime prerequisites needed for the current Cloud Hypervisor-based stack.

### 2. Build And Promote A Guest Image

Fresh machine creation depends on a promoted base image:

```bash
sudo ./ops/cloudhypervisor/build-base-image.sh --version 20260407-01
sudo ./ops/cloudhypervisor/validate-base-image.sh --version 20260407-01
sudo ./ops/cloudhypervisor/promote-base-image.sh --version 20260407-01
sudo ./ops/cloudhypervisor/base-image-status.sh
```

### 3. Deploy The Control Plane

```bash
export FASCINATE_DEPLOY_HOST=your-host.example
export FASCINATE_DEPLOY_USER=ubuntu
export FASCINATE_DEPLOY_PORT=2220
export FASCINATE_BASE_DOMAIN=your-domain.example
export FASCINATE_ACME_EMAIL=you@example.com
export FASCINATE_ADMIN_EMAILS=you@example.com
bash ./ops/release/deploy-full-artifact.sh
```

For frontend-only changes, use the no-restart web deploy path:

```bash
bash ./ops/release/deploy-web-artifact.sh
```

Important deployment notes:

- Browser sign-in requires email delivery config such as `FASCINATE_RESEND_API_KEY` and `FASCINATE_EMAIL_FROM`.
- Wildcard routing for machine subdomains is operator-managed.
- The repo does not provision DNS, Cloudflare, or external infrastructure for you.
- Frontend-only deploys preserve active backend state; full deploys restart `fascinate`.

## Validation And Smoke Scripts

The repo includes host-side verification and live validation tooling:

- `ops/host/smoke.sh` - basic create, route, restart, and delete lifecycle
- `ops/host/stress.sh` - heavier workload, snapshot, restore, fork, and cleanup coverage
- `ops/host/benchmark.sh` - structured timing metrics
- `ops/host/smoke-snapshots.sh` - snapshot and fork flows
- `ops/host/smoke-tool-auth.sh` - persisted tool-auth coverage
- `ops/host/diagnostics.sh` - host, machine, snapshot, event, and auth diagnostics

See `docs/stress-matrix.md` for the current live validation matrix.

## Docs

- `docs/development-and-deploy.md` - local dev loop and deploy workflow
- `docs/stress-matrix.md` - live validation guidance and expectations
- `AGENTS.md` - repo-specific engineering guidance used in this codebase
