#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/../.." && pwd)"

INSTALL_DIR="${FASCINATE_INSTALL_DIR:-/opt/fascinate}"
BIN_DIR="${INSTALL_DIR}/bin"
BINARY_PATH="${BIN_DIR}/fascinate"
WEB_SOURCE_DIR="${REPO_ROOT}/web"
WEB_DIST_SOURCE_DIR="${WEB_SOURCE_DIR}/dist"
WEB_DIST_TARGET_DIR="${INSTALL_DIR}/web/dist"
CONFIG_DIR="${FASCINATE_CONFIG_DIR:-/etc/fascinate}"
ENV_FILE="${FASCINATE_ENV_FILE:-${CONFIG_DIR}/fascinate.env}"
DATA_DIR="${FASCINATE_DATA_DIR:-/var/lib/fascinate}"
SERVICE_PATH="${FASCINATE_SERVICE_PATH:-/etc/systemd/system/fascinate.service}"
OVERWRITE_ENV="${FASCINATE_OVERWRITE_ENV:-0}"

resolve_pnpm() {
  if command -v pnpm >/dev/null 2>&1; then
    printf 'pnpm'
    return
  fi
  if command -v corepack >/dev/null 2>&1; then
    printf 'corepack pnpm'
    return
  fi
  echo "pnpm or corepack is required to build the web app" >&2
  exit 1
}

default_host_id() {
  if [[ -n "${FASCINATE_HOST_ID:-}" ]]; then
    printf '%s' "${FASCINATE_HOST_ID}"
    return
  fi
  hostname -s
}

default_host_name() {
  if [[ -n "${FASCINATE_HOST_NAME:-}" ]]; then
    printf '%s' "${FASCINATE_HOST_NAME}"
    return
  fi
  hostname -s
}

require_root() {
  if [[ "${EUID}" -ne 0 ]]; then
    echo "run as root" >&2
    exit 1
  fi
}

quote_env_value() {
  local value="${1//\\/\\\\}"
  value="${value//\"/\\\"}"
  printf '"%s"' "${value}"
}

write_env_file() {
  mkdir -p "${CONFIG_DIR}" "${DATA_DIR}" "${DATA_DIR}/images" "${DATA_DIR}/machines" "${DATA_DIR}/snapshots"

  cat >"${ENV_FILE}" <<EOF_ENV
FASCINATE_HTTP_ADDR=$(quote_env_value "${FASCINATE_HTTP_ADDR:-127.0.0.1:8080}")
FASCINATE_SSH_ADDR=$(quote_env_value "${FASCINATE_SSH_ADDR:-0.0.0.0:2222}")
FASCINATE_DATA_DIR=$(quote_env_value "${DATA_DIR}")
FASCINATE_DB_PATH=$(quote_env_value "${FASCINATE_DB_PATH:-${DATA_DIR}/fascinate.db}")
FASCINATE_BASE_DOMAIN=$(quote_env_value "${FASCINATE_BASE_DOMAIN:-}")
FASCINATE_ADMIN_EMAILS=$(quote_env_value "${FASCINATE_ADMIN_EMAILS:-}")
FASCINATE_RUNTIME_BINARY=$(quote_env_value "${FASCINATE_RUNTIME_BINARY:-cloud-hypervisor}")
FASCINATE_RUNTIME_STATE_DIR=$(quote_env_value "${FASCINATE_RUNTIME_STATE_DIR:-${DATA_DIR}/machines}")
FASCINATE_RUNTIME_SNAPSHOT_DIR=$(quote_env_value "${FASCINATE_RUNTIME_SNAPSHOT_DIR:-${DATA_DIR}/snapshots}")
FASCINATE_VM_BRIDGE_NAME=$(quote_env_value "${FASCINATE_VM_BRIDGE_NAME:-fascbr0}")
FASCINATE_VM_BRIDGE_CIDR=$(quote_env_value "${FASCINATE_VM_BRIDGE_CIDR:-10.42.0.1/24}")
FASCINATE_VM_GUEST_CIDR=$(quote_env_value "${FASCINATE_VM_GUEST_CIDR:-10.42.0.0/24}")
FASCINATE_VM_NAMESPACE_CIDR=$(quote_env_value "${FASCINATE_VM_NAMESPACE_CIDR:-100.96.0.0/16}")
FASCINATE_VM_FIRMWARE_PATH=$(quote_env_value "${FASCINATE_VM_FIRMWARE_PATH:-/usr/local/share/cloud-hypervisor/CLOUDHV.fd}")
FASCINATE_QEMU_IMG_BINARY=$(quote_env_value "${FASCINATE_QEMU_IMG_BINARY:-qemu-img}")
FASCINATE_CLOUD_LOCALDS_BINARY=$(quote_env_value "${FASCINATE_CLOUD_LOCALDS_BINARY:-cloud-localds}")
FASCINATE_SSH_CLIENT_BINARY=$(quote_env_value "${FASCINATE_SSH_CLIENT_BINARY:-ssh}")
FASCINATE_GUEST_SSH_KEY_PATH=$(quote_env_value "${FASCINATE_GUEST_SSH_KEY_PATH:-${DATA_DIR}/guest_ssh_ed25519}")
FASCINATE_GUEST_SSH_USER=$(quote_env_value "${FASCINATE_GUEST_SSH_USER:-ubuntu}")
FASCINATE_DEFAULT_IMAGE=$(quote_env_value "${FASCINATE_DEFAULT_IMAGE:-${DATA_DIR}/images/fascinate-base.raw}")
FASCINATE_DEFAULT_MACHINE_CPU=$(quote_env_value "${FASCINATE_DEFAULT_MACHINE_CPU:-1}")
FASCINATE_DEFAULT_MACHINE_RAM=$(quote_env_value "${FASCINATE_DEFAULT_MACHINE_RAM:-2GiB}")
FASCINATE_DEFAULT_MACHINE_DISK=$(quote_env_value "${FASCINATE_DEFAULT_MACHINE_DISK:-20GiB}")
FASCINATE_MAX_MACHINES_PER_USER=$(quote_env_value "${FASCINATE_MAX_MACHINES_PER_USER:-6}")
FASCINATE_MAX_MACHINE_CPU=$(quote_env_value "${FASCINATE_MAX_MACHINE_CPU:-2}")
FASCINATE_MAX_MACHINE_RAM=$(quote_env_value "${FASCINATE_MAX_MACHINE_RAM:-4GiB}")
FASCINATE_MAX_MACHINE_DISK=$(quote_env_value "${FASCINATE_MAX_MACHINE_DISK:-20GiB}")
FASCINATE_DEFAULT_PRIMARY_PORT=$(quote_env_value "${FASCINATE_DEFAULT_PRIMARY_PORT:-3000}")
FASCINATE_TOOL_AUTH_DIR=$(quote_env_value "${FASCINATE_TOOL_AUTH_DIR:-${DATA_DIR}/tool-auth}")
FASCINATE_TOOL_AUTH_KEY_PATH=$(quote_env_value "${FASCINATE_TOOL_AUTH_KEY_PATH:-${DATA_DIR}/tool_auth.key}")
FASCINATE_TOOL_AUTH_SYNC_INTERVAL=$(quote_env_value "${FASCINATE_TOOL_AUTH_SYNC_INTERVAL:-2m}")
FASCINATE_SSH_HOST_KEY_PATH=$(quote_env_value "${FASCINATE_SSH_HOST_KEY_PATH:-${DATA_DIR}/ssh_host_ed25519_key}")
FASCINATE_HOST_ID=$(quote_env_value "$(default_host_id)")
FASCINATE_HOST_NAME=$(quote_env_value "$(default_host_name)")
FASCINATE_HOST_REGION=$(quote_env_value "${FASCINATE_HOST_REGION:-local}")
FASCINATE_HOST_ROLE=$(quote_env_value "${FASCINATE_HOST_ROLE:-combined}")
FASCINATE_HOST_HEARTBEAT_INTERVAL=$(quote_env_value "${FASCINATE_HOST_HEARTBEAT_INTERVAL:-30s}")
FASCINATE_RESEND_API_KEY=$(quote_env_value "${FASCINATE_RESEND_API_KEY:-}")
FASCINATE_EMAIL_FROM=$(quote_env_value "${FASCINATE_EMAIL_FROM:-}")
FASCINATE_RESEND_BASE_URL=$(quote_env_value "${FASCINATE_RESEND_BASE_URL:-https://api.resend.com}")
FASCINATE_SIGNUP_CODE_TTL=$(quote_env_value "${FASCINATE_SIGNUP_CODE_TTL:-15m}")
FASCINATE_ACME_EMAIL=$(quote_env_value "${FASCINATE_ACME_EMAIL:-}")
FASCINATE_WEB_DIST_DIR=$(quote_env_value "${FASCINATE_WEB_DIST_DIR:-${WEB_DIST_TARGET_DIR}}")
EOF_ENV
}

install_binary() {
  mkdir -p "${BIN_DIR}" "${DATA_DIR}"
  (cd "${REPO_ROOT}" && go build -o "${BINARY_PATH}" ./cmd/fascinate)
  chmod 0755 "${BINARY_PATH}"
}

build_web_dist() {
  local pnpm_cmd
  pnpm_cmd="$(resolve_pnpm)"
  (
    cd "${WEB_SOURCE_DIR}"
    rm -rf "${WEB_DIST_SOURCE_DIR}"
    eval "${pnpm_cmd} install --frozen-lockfile"
    eval "${pnpm_cmd} build"
  )
}

install_web_dist() {
  build_web_dist
  rm -rf "${WEB_DIST_TARGET_DIR}"
  mkdir -p "${WEB_DIST_TARGET_DIR}"
  cp -R "${WEB_DIST_SOURCE_DIR}/." "${WEB_DIST_TARGET_DIR}/"
}

install_systemd_unit() {
  install -m 0644 "${REPO_ROOT}/ops/systemd/fascinate.service" "${SERVICE_PATH}"
}

maybe_open_firewall_port() {
  local addr="${1}"
  local port="${addr##*:}"
  if [[ -z "${port}" || "${port}" == "${addr}" ]]; then
    return 0
  fi

  if ! command -v ufw >/dev/null 2>&1; then
    return 0
  fi

  ufw allow "${port}/tcp" >/dev/null
}

main() {
  require_root
  install_binary
  install_web_dist
  install_systemd_unit

  if [[ ! -f "${ENV_FILE}" || "${OVERWRITE_ENV}" == "1" ]]; then
    write_env_file
  fi

  set -a
  # shellcheck disable=SC1090
  source "${ENV_FILE}"
  set +a

  maybe_open_firewall_port "${FASCINATE_SSH_ADDR}"
  bash "${REPO_ROOT}/ops/host/write-caddyfile.sh"

  systemctl daemon-reload
  systemctl enable --now fascinate
  systemctl restart fascinate
  systemctl reload caddy

  echo "fascinate deployed"
  echo "service: $(systemctl is-active fascinate)"
  echo "caddy: $(systemctl is-active caddy)"
  curl -fsS "http://${FASCINATE_HTTP_ADDR}/healthz"
}

main "$@"
