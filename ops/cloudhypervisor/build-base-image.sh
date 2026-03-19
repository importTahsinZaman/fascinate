#!/usr/bin/env bash
set -euo pipefail

OUTPUT_IMAGE="${FASCINATE_DEFAULT_IMAGE:-/var/lib/fascinate/images/fascinate-base.qcow2}"
CACHE_DIR="${FASCINATE_IMAGE_CACHE_DIR:-$(dirname -- "${OUTPUT_IMAGE}")/cache}"
SOURCE_IMAGE_URL="${FASCINATE_BASE_SOURCE_IMAGE_URL:-https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img}"
INSTALL_CLAUDE_CODE="${FASCINATE_INSTALL_CLAUDE_CODE:-1}"
CLAUDE_CODE_VERSION="${FASCINATE_CLAUDE_CODE_VERSION:-}"
APT_MIRROR_BASE_URL="${FASCINATE_APT_MIRROR_BASE_URL:-}"
NODE_VERSION="${FASCINATE_NODE_VERSION:-latest}"
GO_VERSION="${FASCINATE_GO_VERSION:-latest}"
NPM_VERSION="${FASCINATE_NPM_VERSION:-latest}"

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

require_command() {
  local name="$1"
  if ! command -v "${name}" >/dev/null 2>&1; then
    echo "missing required command: ${name}" >&2
    exit 1
  fi
}

claude_code_package() {
  if [[ -n "${CLAUDE_CODE_VERSION}" ]]; then
    printf '%s@%s' "@anthropic-ai/claude-code" "${CLAUDE_CODE_VERSION}"
    return
  fi

  printf '%s' "@anthropic-ai/claude-code"
}

main() {
  require_command curl
  require_command cp
  require_command virt-customize

  mkdir -p "$(dirname -- "${OUTPUT_IMAGE}")" "${CACHE_DIR}"

  local source_image="${CACHE_DIR}/$(basename -- "${SOURCE_IMAGE_URL}")"
  local temp_image="${OUTPUT_IMAGE}.tmp"
  local provision_script
  provision_script="$(mktemp)"
  trap "rm -f '${provision_script}' '${temp_image}'" EXIT

  curl -fsSL "${SOURCE_IMAGE_URL}" -o "${source_image}"
  cp "${source_image}" "${temp_image}"

  cat >"${provision_script}" <<'EOF'
#!/usr/bin/env bash
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

install_npm() {
  local requested="${NPM_VERSION_REQUESTED:-latest}"
  npm install -g --force "npm@${requested}"
}

if [[ -n "${APT_MIRROR_BASE_URL}" ]]; then
  cat >/etc/apt/sources.list <<EOF_MIRROR
deb ${APT_MIRROR_BASE_URL} noble main restricted universe multiverse
deb ${APT_MIRROR_BASE_URL} noble-updates main restricted universe multiverse
deb ${APT_MIRROR_BASE_URL} noble-security main restricted universe multiverse
EOF_MIRROR
fi

cat >/etc/apt/apt.conf.d/99fascinate-network <<'EOF_NETWORK'
Acquire::ForceIPv4 "true";
Acquire::Retries "3";
EOF_NETWORK

apt-get -o Acquire::ForceIPv4=true update
apt-get -o Acquire::ForceIPv4=true upgrade -y
apt-get -o Acquire::ForceIPv4=true install -y ${APT_PACKAGES}

NODE_RESOLVED_VERSION="$(resolve_node_version)"
GO_RESOLVED_VERSION="$(resolve_go_version)"
install_node "${NODE_RESOLVED_VERSION}" "$(node_arch)"
install_go "${GO_RESOLVED_VERSION}" "$(go_arch)"
install_npm

if [[ "${INSTALL_CLAUDE_CODE}" == "1" && -n "${CLAUDE_PACKAGE}" ]]; then
  npm install -g "${CLAUDE_PACKAGE}"
  command -v claude >/dev/null 2>&1
fi

cat >/etc/systemd/system/docker.service.d/10-fascinate.conf <<'EOF_DOCKER'
[Service]
Environment=DOCKER_RAMDISK=true
EOF_DOCKER

cat >/root/AGENTS.md <<'EOF_AGENTS'
The fascinate platform handles public HTTPS for this machine.

Rules:
- Bind application servers to 0.0.0.0.
- Port 3000 is the default public application port right now.
- Do not configure TLS certificates inside this machine for public app traffic.
- Docker is available.
- Data on disk persists across restarts.
- Claude Code is preinstalled as `claude`.
EOF_AGENTS

cp /root/AGENTS.md /etc/skel/AGENTS.md
apt-get clean
npm cache clean --force >/dev/null 2>&1 || true
rm -rf /var/lib/apt/lists/*
EOF
  chmod 0755 "${provision_script}"

  local claude_package=""
  if [[ "${INSTALL_CLAUDE_CODE}" == "1" ]]; then
    claude_package="$(claude_code_package)"
  fi

  virt-customize \
    -a "${temp_image}" \
    --copy-in "${provision_script}:/root" \
    --run-command "APT_PACKAGES='${PACKAGES[*]}' APT_MIRROR_BASE_URL='${APT_MIRROR_BASE_URL}' GO_VERSION_REQUESTED='${GO_VERSION}' NODE_VERSION_REQUESTED='${NODE_VERSION}' NPM_VERSION_REQUESTED='${NPM_VERSION}' INSTALL_CLAUDE_CODE='${INSTALL_CLAUDE_CODE}' CLAUDE_PACKAGE='${claude_package}' /root/$(basename -- "${provision_script}")" \
    --firstboot-command 'systemctl enable docker || true'

  mv -f "${temp_image}" "${OUTPUT_IMAGE}"
  echo "built base image ${OUTPUT_IMAGE}"
}

main "$@"
