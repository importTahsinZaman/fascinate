#!/usr/bin/env bash
set -euo pipefail

IMAGE_ALIAS="${FASCINATE_BASE_IMAGE_ALIAS:-fascinate-base}"
SOURCE_IMAGE="${FASCINATE_BASE_SOURCE_IMAGE:-images:ubuntu/24.04}"
TEMP_NAME="${FASCINATE_BASE_BUILD_NAME:-fascinate-base-build}"
INCUS_BINARY="${FASCINATE_INCUS_BINARY:-incus}"
INSTALL_CLAUDE_CODE="${FASCINATE_INSTALL_CLAUDE_CODE:-1}"
CLAUDE_CODE_VERSION="${FASCINATE_CLAUDE_CODE_VERSION:-}"
APT_MIRROR_BASE_URL="${FASCINATE_APT_MIRROR_BASE_URL:-}"

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

claude_code_package() {
  if [[ -n "${CLAUDE_CODE_VERSION}" ]]; then
    printf '%s@%s' "@anthropic-ai/claude-code" "${CLAUDE_CODE_VERSION}"
    return
  fi

  printf '%s' "@anthropic-ai/claude-code"
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

  local claude_package=""
  if [[ "${INSTALL_CLAUDE_CODE}" == "1" ]]; then
    claude_package="$(claude_code_package)"
  fi

  cleanup
  "${INCUS_BINARY}" launch "${SOURCE_IMAGE}" "${TEMP_NAME}"
  wait_for_systemd

  "${INCUS_BINARY}" exec "${TEMP_NAME}" -- bash -lc "
    set -euo pipefail
    export DEBIAN_FRONTEND=noninteractive
    if [[ -n '${APT_MIRROR_BASE_URL}' ]]; then
      cat >/etc/apt/sources.list <<'EOF'
deb ${APT_MIRROR_BASE_URL} noble main restricted universe multiverse
deb ${APT_MIRROR_BASE_URL} noble-updates main restricted universe multiverse
deb ${APT_MIRROR_BASE_URL} noble-security main restricted universe multiverse
EOF
    fi
    cat >/etc/apt/apt.conf.d/99fascinate-network <<'EOF'
Acquire::ForceIPv4 \"true\";
Acquire::Retries \"3\";
EOF
    apt-get -o Acquire::ForceIPv4=true update
    apt-get -o Acquire::ForceIPv4=true install -y ${PACKAGES[*]}
    if [[ -n '${claude_package}' ]]; then
      npm install -g '${claude_package}'
      command -v claude >/dev/null 2>&1
    fi
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
- Claude Code is preinstalled as \`claude\`.
EOF
    cp /root/AGENTS.md /etc/skel/AGENTS.md
    apt-get clean
    npm cache clean --force >/dev/null 2>&1 || true
    rm -rf /var/lib/apt/lists/*
  "

  "${INCUS_BINARY}" stop "${TEMP_NAME}" --force
  "${INCUS_BINARY}" image delete "${IMAGE_ALIAS}" >/dev/null 2>&1 || true
  "${INCUS_BINARY}" publish "${TEMP_NAME}" --alias "${IMAGE_ALIAS}"

  echo "published image alias ${IMAGE_ALIAS}"
}

main "$@"
