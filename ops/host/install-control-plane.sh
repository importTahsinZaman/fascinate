#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/../.." && pwd)"

INSTALL_DIR="${FASCINATE_INSTALL_DIR:-/opt/fascinate}"
BIN_DIR="${INSTALL_DIR}/bin"
BINARY_PATH="${BIN_DIR}/fascinate"
CONFIG_DIR="${FASCINATE_CONFIG_DIR:-/etc/fascinate}"
ENV_FILE="${FASCINATE_ENV_FILE:-${CONFIG_DIR}/fascinate.env}"
DATA_DIR="${FASCINATE_DATA_DIR:-/var/lib/fascinate}"
SERVICE_PATH="${FASCINATE_SERVICE_PATH:-/etc/systemd/system/fascinate.service}"
OVERWRITE_ENV="${FASCINATE_OVERWRITE_ENV:-0}"

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
  mkdir -p "${CONFIG_DIR}" "${DATA_DIR}"

  cat >"${ENV_FILE}" <<EOF
FASCINATE_HTTP_ADDR=$(quote_env_value "${FASCINATE_HTTP_ADDR:-127.0.0.1:8080}")
FASCINATE_SSH_ADDR=$(quote_env_value "${FASCINATE_SSH_ADDR:-0.0.0.0:2222}")
FASCINATE_DATA_DIR=$(quote_env_value "${DATA_DIR}")
FASCINATE_DB_PATH=$(quote_env_value "${FASCINATE_DB_PATH:-${DATA_DIR}/fascinate.db}")
FASCINATE_BASE_DOMAIN=$(quote_env_value "${FASCINATE_BASE_DOMAIN:-}")
FASCINATE_ADMIN_EMAILS=$(quote_env_value "${FASCINATE_ADMIN_EMAILS:-}")
FASCINATE_INCUS_BINARY=$(quote_env_value "${FASCINATE_INCUS_BINARY:-incus}")
FASCINATE_INCUS_STORAGE_POOL=$(quote_env_value "${FASCINATE_INCUS_STORAGE_POOL:-machines}")
FASCINATE_DEFAULT_IMAGE=$(quote_env_value "${FASCINATE_DEFAULT_IMAGE:-fascinate-base}")
FASCINATE_DEFAULT_MACHINE_CPU=$(quote_env_value "${FASCINATE_DEFAULT_MACHINE_CPU:-1}")
FASCINATE_DEFAULT_MACHINE_RAM=$(quote_env_value "${FASCINATE_DEFAULT_MACHINE_RAM:-2GiB}")
FASCINATE_DEFAULT_MACHINE_DISK=$(quote_env_value "${FASCINATE_DEFAULT_MACHINE_DISK:-20GiB}")
FASCINATE_MAX_MACHINES_PER_USER=$(quote_env_value "${FASCINATE_MAX_MACHINES_PER_USER:-3}")
FASCINATE_MAX_MACHINE_CPU=$(quote_env_value "${FASCINATE_MAX_MACHINE_CPU:-2}")
FASCINATE_MAX_MACHINE_RAM=$(quote_env_value "${FASCINATE_MAX_MACHINE_RAM:-4GiB}")
FASCINATE_MAX_MACHINE_DISK=$(quote_env_value "${FASCINATE_MAX_MACHINE_DISK:-20GiB}")
FASCINATE_DEFAULT_PRIMARY_PORT=$(quote_env_value "${FASCINATE_DEFAULT_PRIMARY_PORT:-3000}")
FASCINATE_SSH_HOST_KEY_PATH=$(quote_env_value "${FASCINATE_SSH_HOST_KEY_PATH:-${DATA_DIR}/ssh_host_ed25519_key}")
FASCINATE_RESEND_API_KEY=$(quote_env_value "${FASCINATE_RESEND_API_KEY:-}")
FASCINATE_EMAIL_FROM=$(quote_env_value "${FASCINATE_EMAIL_FROM:-}")
FASCINATE_RESEND_BASE_URL=$(quote_env_value "${FASCINATE_RESEND_BASE_URL:-https://api.resend.com}")
FASCINATE_SIGNUP_CODE_TTL=$(quote_env_value "${FASCINATE_SIGNUP_CODE_TTL:-15m}")
FASCINATE_ACME_EMAIL=$(quote_env_value "${FASCINATE_ACME_EMAIL:-}")
EOF
}

install_binary() {
  mkdir -p "${BIN_DIR}" "${DATA_DIR}"
  (cd "${REPO_ROOT}" && go build -o "${BINARY_PATH}" ./cmd/fascinate)
  chmod 0755 "${BINARY_PATH}"
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

maybe_allow_incus_bridge() {
  if ! command -v ufw >/dev/null 2>&1; then
    return 0
  fi

  if ip link show incusbr0 >/dev/null 2>&1; then
    ufw allow in on incusbr0 >/dev/null
  fi
}

maybe_allow_incus_bridge_routing() {
  local uplink=""

  if ! command -v ufw >/dev/null 2>&1; then
    return 0
  fi

  if ! ip link show incusbr0 >/dev/null 2>&1; then
    return 0
  fi

  uplink="$(ip route get 1.1.1.1 2>/dev/null | awk '{for (i = 1; i <= NF; i++) if ($i == "dev") {print $(i + 1); exit}}')"
  if [[ -z "${uplink}" ]]; then
    return 0
  fi

  ufw route allow in on incusbr0 out on "${uplink}" >/dev/null
}

main() {
  require_root
  install_binary
  install_systemd_unit

  if [[ ! -f "${ENV_FILE}" || "${OVERWRITE_ENV}" == "1" ]]; then
    write_env_file
  fi

  set -a
  # shellcheck disable=SC1090
  source "${ENV_FILE}"
  set +a

  maybe_open_firewall_port "${FASCINATE_SSH_ADDR}"
  maybe_allow_incus_bridge
  maybe_allow_incus_bridge_routing
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
