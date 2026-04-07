# Development and Deploy Workflow

This is the default workflow moving forward:

- use the Vite dev server for frontend work
- use mock mode for UI-only iteration
- use the artifact-based frontend-only deploy path for web changes
- use the artifact-based full deploy path only when backend/runtime changes require it

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

For UI-only changes on a bootstrapped host, build or upload a prebuilt web artifact and deploy it without restarting `fascinate`:

```bash
export FASCINATE_DEPLOY_HOST=fascinate.dev
export FASCINATE_DEPLOY_USER=ubuntu
export FASCINATE_DEPLOY_PORT=2220
bash ./ops/release/deploy-web-artifact.sh
```

If you only want to build the artifact without uploading it yet:

```bash
bash ./ops/release/build-web-artifact.sh
```

What this does:

- builds `web/dist` off-host
- uploads the resulting artifact to the target host
- copies new hashed assets into `/opt/fascinate/web/dist`
- preserves old hashed assets so already-open tabs do not break while lazy-loading older chunks
- swaps `index.html` last
- does **not** restart `fascinate`

This is the default deploy path for frontend work because it avoids disconnecting existing browser shell attachments.

### Full control-plane deploy

Use the full artifact deploy only when backend, runtime, or service wiring changes require it:

```bash
export FASCINATE_DEPLOY_HOST=fascinate.dev
export FASCINATE_DEPLOY_USER=ubuntu
export FASCINATE_DEPLOY_PORT=2220
export FASCINATE_BASE_DOMAIN=fascinate.dev
export FASCINATE_ACME_EMAIL=you@example.com
export FASCINATE_ADMIN_EMAILS=you@example.com
bash ./ops/release/deploy-full-artifact.sh
```

If you only want to build the bundle first:

```bash
bash ./ops/release/build-full-artifact.sh
```

This path:

- cross-builds the Linux `fascinate` binary off-host
- builds `web/dist` off-host
- uploads a versioned release bundle to the host
- installs both under `/opt/fascinate`
- stores the unpacked artifact under `/opt/fascinate/releases/<release-id>`
- updates `/opt/fascinate/release-manifest.json`
- rewrites `/etc/fascinate/fascinate.env` if needed
- reloads Caddy
- restarts `fascinate`

Important:

- restarting `fascinate` drops active browser shell attachments because terminal sessions live in-process today
- persistent shells inside the guest still survive because they run under `tmux`

### Public CLI publish

Use the CLI publish path when you need `curl -fsSL https://fascinate.dev/install.sh | bash` to install a newly released CLI version:

```bash
export FASCINATE_DEPLOY_HOST=fascinate.dev
export FASCINATE_DEPLOY_USER=ubuntu
export FASCINATE_DEPLOY_PORT=2220
bash ./ops/release/publish-cli-release.sh --version 0.1.0 --latest 0.1.0
```

This path:

- builds CLI-only artifacts for the supported Linux and macOS targets
- updates the public release index at `https://downloads.fascinate.dev/cli/index.json`
- publishes `install.sh` at `https://fascinate.dev/install.sh`
- uploads the artifacts into `FASCINATE_PUBLIC_ASSETS_DIR` on the target host so the live service can serve them immediately

If you only want to stage the publish output locally without uploading it:

```bash
bash ./ops/release/publish-cli-release.sh --version 0.1.0 --local-dir ./.tmp/public-cli
```

### Verification after deploy

For either deploy path:

```bash
sudo systemctl is-active fascinate
sudo systemctl is-active caddy
curl -fsS http://127.0.0.1:8080/healthz
curl -fsS http://127.0.0.1:8080/readyz
sudo ./ops/host/diagnostics.sh release-manifest
```

Interpretation:

- `healthz=ok` and `readyz=ready` means the control plane is fully up
- `healthz=ok` and `readyz=startup recovery in progress` means the web/API layer is serving, but initial VM recovery is still running in the background
- if the public site ever returns `502`, first confirm whether `127.0.0.1:8080` is actually listening before debugging Caddy

### Rollback

Every deployed artifact is preserved under `/opt/fascinate/releases/<release-id>`. To roll back, redeploy a previously built artifact with the matching deploy wrapper or reinstall from a preserved release directory on the host.
