#!/usr/bin/env bash
set -euo pipefail

IMAGE_ALIAS="${FASCINATE_BASE_IMAGE_ALIAS:-fascinate-base}"
SOURCE_IMAGE="${FASCINATE_BASE_SOURCE_IMAGE:-images:ubuntu/24.04}"
TEMP_NAME="${FASCINATE_BASE_BUILD_NAME:-fascinate-base-build}"
INCUS_BINARY="${FASCINATE_INCUS_BINARY:-incus}"
INSTALL_CLAUDE_CODE="${FASCINATE_INSTALL_CLAUDE_CODE:-1}"
CLAUDE_CODE_VERSION="${FASCINATE_CLAUDE_CODE_VERSION:-}"
APT_MIRROR_BASE_URL="${FASCINATE_APT_MIRROR_BASE_URL:-}"
NODE_VERSION="${FASCINATE_NODE_VERSION:-latest}"
GO_VERSION="${FASCINATE_GO_VERSION:-latest}"

PACKAGES=(
  build-essential
  ca-certificates
  curl
  docker.io
  file
  fzf
  git
  gnupg
  jq
  lsb-release
  make
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

  "${INCUS_BINARY}" exec "${TEMP_NAME}" -- env \
    APT_PACKAGES="${PACKAGES[*]}" \
    DEBIAN_FRONTEND=noninteractive \
    APT_MIRROR_BASE_URL="${APT_MIRROR_BASE_URL}" \
    CLAUDE_PACKAGE="${claude_package}" \
    GO_VERSION_REQUESTED="${GO_VERSION}" \
    INSTALL_CLAUDE_CODE="${INSTALL_CLAUDE_CODE}" \
    NODE_VERSION_REQUESTED="${NODE_VERSION}" \
    bash -se <<'EOF'
set -euo pipefail

resolve_node_version() {
  local requested="${NODE_VERSION_REQUESTED:-latest}"

  case "${requested}" in
    ""|latest)
      curl -fsSL https://nodejs.org/dist/index.json | python3 -c 'import json, sys; releases = json.load(sys.stdin); print(releases[0]["version"])'
      ;;
    latest-lts)
      curl -fsSL https://nodejs.org/dist/index.json | python3 -c 'import json, sys; releases = json.load(sys.stdin); print(next(release["version"] for release in releases if release.get("lts")))'
      ;;
    v*)
      printf "%s\n" "${requested}"
      ;;
    *)
      printf "v%s\n" "${requested}"
      ;;
  esac
}

resolve_go_version() {
  local requested="${GO_VERSION_REQUESTED:-latest}"

  case "${requested}" in
    ""|latest)
      curl -fsSL https://go.dev/dl/?mode=json | python3 -c 'import json, sys; releases = json.load(sys.stdin); print(releases[0]["version"].removeprefix("go"))'
      ;;
    go*)
      printf "%s\n" "${requested#go}"
      ;;
    *)
      printf "%s\n" "${requested}"
      ;;
  esac
}

node_arch() {
  case "$(dpkg --print-architecture)" in
    amd64) printf "%s\n" "x64" ;;
    arm64) printf "%s\n" "arm64" ;;
    *)
      printf "unsupported node architecture: %s\n" "$(dpkg --print-architecture)" >&2
      exit 1
      ;;
  esac
}

go_arch() {
  case "$(dpkg --print-architecture)" in
    amd64) printf "%s\n" "amd64" ;;
    arm64) printf "%s\n" "arm64" ;;
    *)
      printf "unsupported go architecture: %s\n" "$(dpkg --print-architecture)" >&2
      exit 1
      ;;
  esac
}

install_node() {
  local version="$1"
  local arch="$2"
  local file="node-${version}-linux-${arch}.tar.xz"
  local base_url="https://nodejs.org/dist/${version}"

  curl -fsSLO "${base_url}/${file}"
  curl -fsSL "${base_url}/SHASUMS256.txt" -o SHASUMS256.txt
  grep " ${file}\$" SHASUMS256.txt | sha256sum -c -

  rm -rf /usr/local/lib/nodejs
  mkdir -p /usr/local/lib/nodejs
  tar -xJf "${file}" -C /usr/local/lib/nodejs

  ln -sf "/usr/local/lib/nodejs/node-${version}-linux-${arch}/bin/node" /usr/local/bin/node
  ln -sf "/usr/local/lib/nodejs/node-${version}-linux-${arch}/bin/npm" /usr/local/bin/npm
  ln -sf "/usr/local/lib/nodejs/node-${version}-linux-${arch}/bin/npx" /usr/local/bin/npx
  ln -sf "/usr/local/lib/nodejs/node-${version}-linux-${arch}/bin/corepack" /usr/local/bin/corepack
  npm config set prefix /usr/local >/dev/null 2>&1 || true
  corepack enable >/dev/null 2>&1 || true

  rm -f "${file}" SHASUMS256.txt
}

install_go() {
  local version="$1"
  local arch="$2"
  local file="go${version}.linux-${arch}.tar.gz"
  local checksum=""

  curl -fsSLo "${file}" "https://dl.google.com/go/${file}"
  checksum="$(curl -fsSL "https://go.dev/dl/?mode=json&include=all" | python3 -c 'import json, sys; target = sys.argv[1]; releases = json.load(sys.stdin); print(next(entry["sha256"] for release in releases for entry in release.get("files", []) if entry.get("filename") == target))' "${file}")"
  printf "%s  %s\n" "${checksum}" "${file}" | sha256sum -c -

  rm -rf /usr/local/go
  tar -C /usr/local -xzf "${file}"
  ln -sf /usr/local/go/bin/go /usr/local/bin/go
  ln -sf /usr/local/go/bin/gofmt /usr/local/bin/gofmt

  rm -f "${file}"
}

if [[ -n "${APT_MIRROR_BASE_URL}" ]]; then
  cat >/etc/apt/sources.list <<EOF_MIRROR
deb ${APT_MIRROR_BASE_URL} noble main restricted universe multiverse
deb ${APT_MIRROR_BASE_URL} noble-updates main restricted universe multiverse
deb ${APT_MIRROR_BASE_URL} noble-security main restricted universe multiverse
EOF_MIRROR
fi

cat >/etc/apt/apt.conf.d/99fascinate-network <<'EOF_NETWORK'
Acquire::ForceIPv4 \"true\";
Acquire::Retries \"3\";
EOF_NETWORK

apt-get -o Acquire::ForceIPv4=true update
apt-get -o Acquire::ForceIPv4=true upgrade -y
apt-get -o Acquire::ForceIPv4=true install -y ${APT_PACKAGES}

NODE_RESOLVED_VERSION="$(resolve_node_version)"
GO_RESOLVED_VERSION="$(resolve_go_version)"
install_node "${NODE_RESOLVED_VERSION}" "$(node_arch)"
install_go "${GO_RESOLVED_VERSION}" "$(go_arch)"

if [[ "${INSTALL_CLAUDE_CODE}" == "1" && -n "${CLAUDE_PACKAGE}" ]]; then
  npm install -g "${CLAUDE_PACKAGE}"
  command -v claude >/dev/null 2>&1
fi

touch /.dockerenv
systemctl enable docker

cat >/root/AGENTS.md <<'EOF_AGENTS'
The fascinate platform handles public HTTPS for this machine.

Rules:
- Bind application servers to 0.0.0.0.
- Port 3000 is the default public application port right now.
- Do not configure TLS certificates inside this machine for public app traffic.
- Docker is available.
- Data on disk persists across restarts.
- Claude Code is preinstalled as \`claude\`.
EOF_AGENTS

cp /root/AGENTS.md /etc/skel/AGENTS.md
apt-get clean
npm cache clean --force >/dev/null 2>&1 || true
rm -rf /var/lib/apt/lists/*
EOF

  "${INCUS_BINARY}" stop "${TEMP_NAME}" --force
  "${INCUS_BINARY}" image delete "${IMAGE_ALIAS}" >/dev/null 2>&1 || true
  "${INCUS_BINARY}" publish "${TEMP_NAME}" --alias "${IMAGE_ALIAS}"

  echo "published image alias ${IMAGE_ALIAS}"
}

main "$@"
