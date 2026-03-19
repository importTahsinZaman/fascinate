# fascinate

`fascinate` is a terminal-first control plane for persistent developer machines.

This repo now contains:
- a reproducible host bootstrap path under [`ops/host/bootstrap.sh`](/Users/tahsin/Desktop/vmcloud/ops/host/bootstrap.sh)
- a host redeploy path under [`ops/host/install-control-plane.sh`](/Users/tahsin/Desktop/vmcloud/ops/host/install-control-plane.sh)
- a Caddy config writer under [`ops/host/write-caddyfile.sh`](/Users/tahsin/Desktop/vmcloud/ops/host/write-caddyfile.sh)
- a VM base image builder under [`ops/cloudhypervisor/build-base-image.sh`](/Users/tahsin/Desktop/vmcloud/ops/cloudhypervisor/build-base-image.sh)
- a minimal Go control plane under [`cmd/fascinate/main.go`](/Users/tahsin/Desktop/vmcloud/cmd/fascinate/main.go)
- SQLite migrations for the first platform tables
- a Cloud Hypervisor runtime wrapper and health endpoints
- a minimal SSH frontdoor backed by SQLite-stored public keys

## Current Scope

This is the first real scaffold, not the full product yet. It gives us:
- repeatable host setup for Ubuntu 24.04
- a baseline Cloud Hypervisor + Caddy + firewall install
- a Go service that can:
  - load config from env
  - initialize SQLite
  - run migrations
  - expose `/healthz`, `/readyz`, and `/v1/runtime/machines`
  - talk to the local `cloud-hypervisor` runtime

It does not yet include:
- recovery and account-management flows for additional SSH keys

It now includes a first machine API slice:
- `GET /v1/machines`
- `POST /v1/machines`
- `GET /v1/machines/{name}`
- `DELETE /v1/machines/{name}`
- `POST /v1/machines/{name}/clone`

It also includes a first SSH slice:
- `fascinate seed-ssh-key --email ... --name ... --public-key-file ...`
- a DB-backed SSH server on `FASCINATE_SSH_ADDR`
- command handling for `help`, `whoami`, `machines`, `create`, `clone`, `delete`, and `shell`
- a Bubble Tea dashboard for interactive `ssh fascinate.dev` sessions
- unknown-key signup with emailed 6-digit verification codes
- wildcard machine routing inside the HTTP server for `https://<machine>.<base-domain>`

For now, machine ownership is bootstrapped by passing `owner_email` in the API request. This is temporary until the SSH auth flow is wired in.

## Repo Layout

- [`ops/host/bootstrap.sh`](/Users/tahsin/Desktop/vmcloud/ops/host/bootstrap.sh): installs host dependencies and baseline VM networking/runtime config
- [`ops/host/verify.sh`](/Users/tahsin/Desktop/vmcloud/ops/host/verify.sh): checks the host after bootstrap
- [`ops/host/write-caddyfile.sh`](/Users/tahsin/Desktop/vmcloud/ops/host/write-caddyfile.sh): writes the host Caddy config for Fascinate
- [`ops/host/install-control-plane.sh`](/Users/tahsin/Desktop/vmcloud/ops/host/install-control-plane.sh): builds and installs the Fascinate service on a host
- [`ops/cloudhypervisor/build-base-image.sh`](/Users/tahsin/Desktop/vmcloud/ops/cloudhypervisor/build-base-image.sh): builds an agent-ready qcow2 guest image
- [`ops/systemd/fascinate.service`](/Users/tahsin/Desktop/vmcloud/ops/systemd/fascinate.service): example systemd unit
- [`cmd/fascinate/main.go`](/Users/tahsin/Desktop/vmcloud/cmd/fascinate/main.go): entrypoint
- [`internal/config/config.go`](/Users/tahsin/Desktop/vmcloud/internal/config/config.go): env-backed config
- [`internal/database/migrations/0001_init.sql`](/Users/tahsin/Desktop/vmcloud/internal/database/migrations/0001_init.sql): initial SQLite schema
- [`internal/runtime/cloudhypervisor/runtime.go`](/Users/tahsin/Desktop/vmcloud/internal/runtime/cloudhypervisor/runtime.go): Cloud Hypervisor VM runtime
- [`internal/sshfrontdoor/server.go`](/Users/tahsin/Desktop/vmcloud/internal/sshfrontdoor/server.go): SSH transport and auth
- [`internal/tui/dashboard.go`](/Users/tahsin/Desktop/vmcloud/internal/tui/dashboard.go): Bubble Tea dashboard model

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

Notes:
- the bootstrap script assumes a fresh host or a host you are willing to standardize
- it installs Cloud Hypervisor plus qcow2/cloud-init image tooling
- it creates a bridge named `fascbr0` by default
- it configures NAT and forwarding so guest VMs can reach the internet through the host
- it does not manage DNS or Cloudflare for you

Build the default agent-ready guest image:

```bash
sudo ./ops/cloudhypervisor/build-base-image.sh
```

By default, the base image builder also installs Claude Code as `claude`.
Useful image build env vars:

```bash
export FASCINATE_INSTALL_CLAUDE_CODE=1
export FASCINATE_CLAUDE_CODE_VERSION=latest
sudo ./ops/cloudhypervisor/build-base-image.sh
```

Set `FASCINATE_INSTALL_CLAUDE_CODE=0` if you want a neutral image without it.
Set `FASCINATE_APT_MIRROR_BASE_URL=...` only if you explicitly want to rewrite the guest apt sources during image build.

If you want Fascinate to own port `22`, move host admin SSH first:

```bash
export FASCINATE_HOST_ADMIN_SSH_PORT=2220
sudo ./ops/host/configure-admin-ssh.sh
```

After that:
- host admin SSH uses `ssh -p 2220 root@fascinate.dev`
- Fascinate can safely bind `:22`

Deploy or redeploy the Fascinate service:

```bash
export FASCINATE_BASE_DOMAIN=fascinate.dev
export FASCINATE_ACME_EMAIL=you@example.com
export FASCINATE_ADMIN_EMAILS=you@example.com
export FASCINATE_SSH_ADDR=0.0.0.0:22
sudo ./ops/host/install-control-plane.sh
```

Important for Cloudflare:
- the generated wildcard Caddy block uses `tls internal`
- that means Cloudflare should use `Full` mode for proxied wildcard traffic unless you replace the wildcard TLS block with an Origin CA certificate
- the apex `fascinate.dev` site still gets a normal public cert from Caddy because it is `DNS only`

### Local Development

Run the control plane locally:

```bash
make run
```

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
export FASCINATE_SSH_ADDR=127.0.0.1:2222
export FASCINATE_DATA_DIR=./data
export FASCINATE_DB_PATH=./data/fascinate.db
export FASCINATE_BASE_DOMAIN=fascinate.dev
export FASCINATE_ADMIN_EMAILS=you@example.com,ops@example.com
export FASCINATE_RUNTIME_BINARY=cloud-hypervisor
export FASCINATE_RUNTIME_STATE_DIR=./data/machines
export FASCINATE_VM_BRIDGE_NAME=fascbr0
export FASCINATE_VM_BRIDGE_CIDR=10.42.0.1/24
export FASCINATE_VM_GUEST_CIDR=10.42.0.0/24
export FASCINATE_VM_FIRMWARE_PATH=/usr/share/qemu/OVMF.fd
export FASCINATE_QEMU_IMG_BINARY=qemu-img
export FASCINATE_CLOUD_LOCALDS_BINARY=cloud-localds
export FASCINATE_SSH_CLIENT_BINARY=ssh
export FASCINATE_GUEST_SSH_KEY_PATH=./data/guest_ssh_ed25519
export FASCINATE_GUEST_SSH_USER=ubuntu
export FASCINATE_DEFAULT_IMAGE=./data/images/fascinate-base.qcow2
export FASCINATE_DEFAULT_MACHINE_CPU=1
export FASCINATE_DEFAULT_MACHINE_RAM=2GiB
export FASCINATE_DEFAULT_MACHINE_DISK=20GiB
export FASCINATE_MAX_MACHINES_PER_USER=3
export FASCINATE_MAX_MACHINE_CPU=2
export FASCINATE_MAX_MACHINE_RAM=4GiB
export FASCINATE_MAX_MACHINE_DISK=20GiB
export FASCINATE_DEFAULT_PRIMARY_PORT=3000
export FASCINATE_NODE_VERSION=latest
export FASCINATE_GO_VERSION=latest
export FASCINATE_NPM_VERSION=latest
export FASCINATE_SSH_HOST_KEY_PATH=./data/ssh_host_ed25519_key
export FASCINATE_RESEND_API_KEY=...
export FASCINATE_EMAIL_FROM='Fascinate <hello@example.com>'
export FASCINATE_RESEND_BASE_URL=https://api.resend.com
export FASCINATE_SIGNUP_CODE_TTL=15m
export FASCINATE_ACME_EMAIL=you@example.com
```

Seed an SSH key into the local SQLite DB:

```bash
./bin/fascinate seed-ssh-key \
  --email you@example.com \
  --name laptop \
  --public-key-file ~/.ssh/id_ed25519.pub
```

Then connect to the local SSH frontdoor:

```bash
ssh -p 2222 localhost machines
```

Or open an interactive shell:

```bash
ssh -p 2222 localhost
```

If the SSH key is unknown and email delivery is configured, the session opens a signup flow instead of rejecting the connection. After verification, the key is persisted and the dashboard opens in the same SSH session.

If your host Caddy config forwards wildcard subdomains to `FASCINATE_HTTP_ADDR`, requests for `https://<machine>.fascinate.dev` are proxied to that machine's primary port. If nothing is listening yet, Fascinate serves a status page with the SSH shell command for that machine.

New machines built from `fascinate-base` come with:
- Ubuntu 24.04 packages upgraded to the latest versions available in the current Ubuntu repositories at image-build time
- Docker
- Node.js and Go installed from upstream releases at image-build time (`FASCINATE_NODE_VERSION=latest` and `FASCINATE_GO_VERSION=latest` by default)
- npm upgraded from the upstream registry at image-build time (`FASCINATE_NPM_VERSION=latest` by default)
- Python 3, git, jq, ripgrep, sqlite3, tmux, fzf, curl, wget, unzip, zip, rsync, and common build tooling
- Claude Code available as `claude`

Available exec-style SSH commands:

```bash
machines
create habits
clone habits habits-v2
delete habits --confirm habits
shell habits
whoami
help
exit
```

Interactive dashboard keys:

```bash
j / k or arrows   move selection
enter             open selected machine detail
s                 open a shell in the selected machine
n                 create machine
c                 clone selected machine
d                 delete selected machine (typed confirmation)
r                 refresh
q                 quit
esc               back/cancel
```

## Next Milestones

1. Add recovery and “attach another SSH key” flows for existing accounts.
2. Replace the current single-screen dashboard with fuller Bubble Tea flows for machine creation, detail, and errors.
3. Enforce per-user quotas and approval rules.
4. Add account recovery and attach-another-key flows.
