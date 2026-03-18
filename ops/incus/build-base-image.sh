#!/usr/bin/env bash
set -euo pipefail

IMAGE_ALIAS="${FASCINATE_BASE_IMAGE_ALIAS:-fascinate-base}"
SOURCE_IMAGE="${FASCINATE_BASE_SOURCE_IMAGE:-images:ubuntu/24.04}"
TEMP_NAME="${FASCINATE_BASE_BUILD_NAME:-fascinate-base-build}"
INCUS_BINARY="${FASCINATE_INCUS_BINARY:-incus}"

PACKAGES=(
  build-essential
  ca-certificates
  curl
  docker.io
  file
  fzf
  git
  gnupg
  golang-go
  jq
  lsb-release
  make
  nodejs
  npm
  openssh-client
  procps
  python-is-python3
  python3
  python3-pip
  python3-venv
  ripgrep
  rsync
  sqlite3
  tmux
  unzip
  wget
  xz-utils
  zip
)

cleanup() {
  "${INCUS_BINARY}" delete --force "${TEMP_NAME}" >/dev/null 2>&1 || true
}

wait_for_systemd() {
  "${INCUS_BINARY}" exec "${TEMP_NAME}" -- bash -lc '
    for _ in $(seq 1 60); do
      state="$(systemctl is-system-running 2>/dev/null || true)"
      case "${state}" in
        running|degraded) exit 0 ;;
      esac
      sleep 2
    done
    exit 1
  '
}

main() {
  trap cleanup EXIT

  cleanup
  "${INCUS_BINARY}" launch "${SOURCE_IMAGE}" "${TEMP_NAME}"
  wait_for_systemd

  "${INCUS_BINARY}" exec "${TEMP_NAME}" -- bash -lc "
    export DEBIAN_FRONTEND=noninteractive
    apt-get update
    apt-get install -y ${PACKAGES[*]}
    touch /.dockerenv
    systemctl enable docker
    cat >/root/AGENTS.md <<'EOF'
The fascinate platform handles public HTTPS for this machine.

Rules:
- Bind application servers to 0.0.0.0.
- Port 3000 is the default public application port right now.
- Do not configure TLS certificates inside this machine for public app traffic.
- Docker is available.
- Data on disk persists across restarts.
EOF
    cp /root/AGENTS.md /etc/skel/AGENTS.md
    apt-get clean
    rm -rf /var/lib/apt/lists/*
  "

  "${INCUS_BINARY}" stop "${TEMP_NAME}" --force
  "${INCUS_BINARY}" image delete "${IMAGE_ALIAS}" >/dev/null 2>&1 || true
  "${INCUS_BINARY}" publish "${TEMP_NAME}" --alias "${IMAGE_ALIAS}"

  echo "published image alias ${IMAGE_ALIAS}"
}

main "$@"
