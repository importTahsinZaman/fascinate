# Development and Deploy Workflow

This is the default workflow moving forward:

- use the Vite dev server for frontend work
- use mock mode for UI-only iteration
- use the frontend-only deploy path for web changes
- use the full installer only when backend/runtime changes require it

## Local Development

### Backend + frontend together

Run the Go server locally:

```bash
make run
```

In a second shell, run the Vite dev server:

```bash
make web-dev
```

The Vite server proxies these paths to `FASCINATE_DEV_PROXY_TARGET`:

- `/v1`
- `/healthz`
- `/readyz`

It also proxies terminal WebSockets under `/v1/...`, so the browser app can talk to a live local control plane without rebuilding `web/dist` after every change.

By default the proxy target is `http://127.0.0.1:8080`. Override it if your backend is elsewhere:

```bash
FASCINATE_DEV_PROXY_TARGET=http://127.0.0.1:9090 make web-dev
```

Readiness behavior:

- `/healthz` reports whether the web process is up
- `/readyz` reports whether the control plane is ready for normal traffic
- during startup VM recovery, the server now binds HTTP first, so the site can stay reachable while `/readyz` temporarily returns `startup recovery in progress`

### UI-only mock mode

For layout, styling, and interaction work that does not need a live backend or real machines:

```bash
make web-dev-mock
```

Mock mode provides:

- a signed-in browser session
- sample machines, snapshots, and env vars
- sample shell windows
- sample git status and git diffs
- mock terminal sessions rendered entirely in the browser

This is the fastest path for command-center and diff-view UI work.

### Validation before pushing

Any web change should still pass:

```bash
make web-test
make web-build
```

If you touched `ops/` scripts, also run:

```bash
make verify-ops
```

## Deploy

### Frontend-only deploy

For UI-only changes on a bootstrapped host, deploy the web bundle without restarting `fascinate`:

```bash
sudo ./ops/host/deploy-web.sh
```

What this does:

- rebuilds `web/dist`
- copies new hashed assets into `/opt/fascinate/web/dist`
- preserves old hashed assets so already-open tabs do not break while lazy-loading older chunks
- swaps `index.html` last
- does **not** restart `fascinate`

This is the default deploy path for frontend work because it avoids disconnecting existing browser shell attachments.

### Full control-plane deploy

Use the full installer only when backend, runtime, or service wiring changes require it:

```bash
sudo ./ops/host/install-control-plane.sh
```

This path:

- rebuilds the Go binary
- rebuilds `web/dist`
- installs both under `/opt/fascinate`
- rewrites `/etc/fascinate/fascinate.env` if needed
- reloads Caddy
- restarts `fascinate`

Important:

- restarting `fascinate` drops active browser shell attachments because terminal sessions live in-process today
- persistent shells inside the guest still survive because they run under `tmux`

### Verification after deploy

For either deploy path:

```bash
sudo systemctl is-active fascinate
sudo systemctl is-active caddy
curl -fsS http://127.0.0.1:8080/healthz
curl -fsS http://127.0.0.1:8080/readyz
```

Interpretation:

- `healthz=ok` and `readyz=ready` means the control plane is fully up
- `healthz=ok` and `readyz=startup recovery in progress` means the web/API layer is serving, but initial VM recovery is still running in the background
- if the public site ever returns `502`, first confirm whether `127.0.0.1:8080` is actually listening before debugging Caddy
