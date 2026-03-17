# fascinate

`fascinate` is a terminal-first control plane for persistent developer machines.

This repo now contains:
- a reproducible host bootstrap path under [`ops/host/bootstrap.sh`](/Users/tahsin/Desktop/vmcloud/ops/host/bootstrap.sh)
- a minimal Go control plane under [`cmd/fascinate/main.go`](/Users/tahsin/Desktop/vmcloud/cmd/fascinate/main.go)
- SQLite migrations for the first platform tables
- an Incus runtime wrapper and health endpoints

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
- machine create/delete/clone APIs
- dynamic Caddy routing from the control plane

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
```

Useful env vars:

```bash
export FASCINATE_HTTP_ADDR=127.0.0.1:8080
export FASCINATE_DATA_DIR=./data
export FASCINATE_DB_PATH=./data/fascinate.db
export FASCINATE_BASE_DOMAIN=fascinate.dev
export FASCINATE_ADMIN_EMAILS=you@example.com,ops@example.com
export FASCINATE_INCUS_BINARY=incus
```

## Next Milestones

1. Add machine CRUD and clone operations backed by Incus and SQLite.
2. Add SSH key lookup and terminal signup flow.
3. Add the Bubble Tea dashboard and machine detail screens.
4. Replace the static host Caddy config with control-plane-managed routing.
