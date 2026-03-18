# fascinate

`fascinate` is a terminal-first control plane for persistent developer machines.

This repo now contains:
- a reproducible host bootstrap path under [`ops/host/bootstrap.sh`](/Users/tahsin/Desktop/vmcloud/ops/host/bootstrap.sh)
- a minimal Go control plane under [`cmd/fascinate/main.go`](/Users/tahsin/Desktop/vmcloud/cmd/fascinate/main.go)
- SQLite migrations for the first platform tables
- an Incus runtime wrapper and health endpoints
- a minimal SSH frontdoor backed by SQLite-stored public keys

## Current Scope

This is the first real scaffold, not the full product yet. It gives us:
- repeatable host setup for Ubuntu 24.04
- a baseline Incus + Caddy + firewall install
- a Go service that can:
  - load config from env
  - initialize SQLite
  - run migrations
  - expose `/healthz`, `/readyz`, and `/v1/runtime/machines`
  - talk to the local `incus` CLI

It does not yet include:
- SSH auth flow
- Bubble Tea TUI
- dynamic Caddy routing from the control plane

It now includes a first machine API slice:
- `GET /v1/machines`
- `POST /v1/machines`
- `GET /v1/machines/{name}`
- `DELETE /v1/machines/{name}`
- `POST /v1/machines/{name}/clone`

It also includes a first SSH slice:
- `fascinate seed-ssh-key --email ... --name ... --public-key-file ...`
- a DB-backed SSH server on `FASCINATE_SSH_ADDR`
- command handling for `help`, `whoami`, `machines`, `create`, `clone`, and `delete`
- an interactive line-based shell loop for `ssh fascinate.dev`

For now, machine ownership is bootstrapped by passing `owner_email` in the API request. This is temporary until the SSH auth flow is wired in.

## Repo Layout

- [`ops/host/bootstrap.sh`](/Users/tahsin/Desktop/vmcloud/ops/host/bootstrap.sh): installs host dependencies and baseline Incus config
- [`ops/host/verify.sh`](/Users/tahsin/Desktop/vmcloud/ops/host/verify.sh): checks the host after bootstrap
- [`ops/systemd/fascinate.service`](/Users/tahsin/Desktop/vmcloud/ops/systemd/fascinate.service): example systemd unit
- [`cmd/fascinate/main.go`](/Users/tahsin/Desktop/vmcloud/cmd/fascinate/main.go): entrypoint
- [`internal/config/config.go`](/Users/tahsin/Desktop/vmcloud/internal/config/config.go): env-backed config
- [`internal/database/migrations/0001_init.sql`](/Users/tahsin/Desktop/vmcloud/internal/database/migrations/0001_init.sql): initial SQLite schema
- [`internal/runtime/incus/runtime.go`](/Users/tahsin/Desktop/vmcloud/internal/runtime/incus/runtime.go): Incus CLI wrapper

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
- it installs `Incus` from Zabbly's stable repo
- it creates an Incus storage pool named `machines` by default
- it does not manage DNS or Cloudflare for you

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
export FASCINATE_INCUS_BINARY=incus
export FASCINATE_INCUS_STORAGE_POOL=machines
export FASCINATE_DEFAULT_IMAGE=images:ubuntu/24.04
export FASCINATE_DEFAULT_MACHINE_CPU=1
export FASCINATE_DEFAULT_MACHINE_RAM=2GiB
export FASCINATE_DEFAULT_PRIMARY_PORT=3000
export FASCINATE_SSH_HOST_KEY_PATH=./data/ssh_host_ed25519_key
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

Available SSH commands:

```bash
machines
create habits
clone habits habits-v2
delete habits --confirm habits
whoami
help
exit
```

## Next Milestones

1. Add terminal signup and unknown-key verification flow.
2. Replace the line-based SSH shell with the real Bubble Tea dashboard and machine detail screens.
3. Add terminal signup and unknown-key verification flow.
4. Replace the static host Caddy config with control-plane-managed routing.
5. Enforce per-user quotas and approval rules.
