#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
ARTIFACT_ROOT="$(cd -- "${SCRIPT_DIR}/../.." && pwd)"
source "${ARTIFACT_ROOT}/ops/release/lib.sh"

INSTALL_DIR="${FASCINATE_INSTALL_DIR:-/opt/fascinate}"
BIN_DIR="${INSTALL_DIR}/bin"
BINARY_PATH="${BIN_DIR}/fascinate"
RELEASES_DIR="${INSTALL_DIR}/releases"
RELEASE_STATE_PATH="${FASCINATE_RELEASE_MANIFEST_PATH:-${INSTALL_DIR}/release-manifest.json}"
PAYLOAD_DIR="${ARTIFACT_ROOT}/payload"
PAYLOAD_BINARY_PATH="${PAYLOAD_DIR}/bin/fascinate"
PAYLOAD_WEB_DIST_DIR="${PAYLOAD_DIR}/web/dist"
WEB_DIST_TARGET_DIR="${INSTALL_DIR}/web/dist"
CONFIG_DIR="${FASCINATE_CONFIG_DIR:-/etc/fascinate}"
ENV_FILE="${FASCINATE_ENV_FILE:-${CONFIG_DIR}/fascinate.env}"
DATA_DIR="${FASCINATE_DATA_DIR:-/var/lib/fascinate}"
SERVICE_PATH="${FASCINATE_SERVICE_PATH:-/etc/systemd/system/fascinate.service}"
OVERWRITE_ENV="${FASCINATE_OVERWRITE_ENV:-0}"
ARTIFACT_MANIFEST_PATH="${ARTIFACT_ROOT}/manifest.json"

quote_env_value() {
  local value="${1//\\/\\\\}"
  value="${value//\"/\\\"}"
  printf '"%s"' "${value}"
}

write_env_file() {
  local public_assets_dir
  public_assets_dir="${FASCINATE_PUBLIC_ASSETS_DIR:-${INSTALL_DIR}/public}"

  mkdir -p "${CONFIG_DIR}" "${DATA_DIR}" "${DATA_DIR}/images" "${DATA_DIR}/machines" "${DATA_DIR}/snapshots" "${public_assets_dir}" "${public_assets_dir}/cli"

  cat >"${ENV_FILE}" <<EOF_ENV
FASCINATE_HTTP_ADDR=$(quote_env_value "${FASCINATE_HTTP_ADDR:-127.0.0.1:8080}")
FASCINATE_DATA_DIR=$(quote_env_value "${DATA_DIR}")
FASCINATE_DB_PATH=$(quote_env_value "${FASCINATE_DB_PATH:-${DATA_DIR}/fascinate.db}")
FASCINATE_BASE_DOMAIN=$(quote_env_value "${FASCINATE_BASE_DOMAIN:-}")
FASCINATE_ADMIN_EMAILS=$(quote_env_value "${FASCINATE_ADMIN_EMAILS:-}")
FASCINATE_PUBLIC_ASSETS_DIR=$(quote_env_value "${public_assets_dir}")
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
FASCINATE_DEFAULT_USER_MAX_CPU=$(quote_env_value "${FASCINATE_DEFAULT_USER_MAX_CPU:-5}")
FASCINATE_DEFAULT_USER_MAX_RAM=$(quote_env_value "${FASCINATE_DEFAULT_USER_MAX_RAM:-10GiB}")
FASCINATE_DEFAULT_USER_MAX_DISK=$(quote_env_value "${FASCINATE_DEFAULT_USER_MAX_DISK:-80GiB}")
FASCINATE_DEFAULT_USER_MAX_MACHINES=$(quote_env_value "${FASCINATE_DEFAULT_USER_MAX_MACHINES:-5}")
FASCINATE_DEFAULT_USER_MAX_SNAPSHOTS=$(quote_env_value "${FASCINATE_DEFAULT_USER_MAX_SNAPSHOTS:-5}")
FASCINATE_MAX_MACHINES_PER_USER=$(quote_env_value "${FASCINATE_MAX_MACHINES_PER_USER:-5}")
FASCINATE_MAX_MACHINE_CPU=$(quote_env_value "${FASCINATE_MAX_MACHINE_CPU:-2}")
FASCINATE_MAX_MACHINE_RAM=$(quote_env_value "${FASCINATE_MAX_MACHINE_RAM:-4GiB}")
FASCINATE_MAX_MACHINE_DISK=$(quote_env_value "${FASCINATE_MAX_MACHINE_DISK:-20GiB}")
FASCINATE_HOST_SHARED_CPU_RATIO=$(quote_env_value "${FASCINATE_HOST_SHARED_CPU_RATIO:-1.67}")
FASCINATE_HOST_MIN_FREE_DISK=$(quote_env_value "${FASCINATE_HOST_MIN_FREE_DISK:-150GiB}")
FASCINATE_DEFAULT_PRIMARY_PORT=$(quote_env_value "${FASCINATE_DEFAULT_PRIMARY_PORT:-3000}")
FASCINATE_TOOL_AUTH_DIR=$(quote_env_value "${FASCINATE_TOOL_AUTH_DIR:-${DATA_DIR}/tool-auth}")
FASCINATE_TOOL_AUTH_KEY_PATH=$(quote_env_value "${FASCINATE_TOOL_AUTH_KEY_PATH:-${DATA_DIR}/tool_auth.key}")
FASCINATE_TOOL_AUTH_SYNC_INTERVAL=$(quote_env_value "${FASCINATE_TOOL_AUTH_SYNC_INTERVAL:-2m}")
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

ensure_public_assets_env() {
  local public_assets_dir
  public_assets_dir="${FASCINATE_PUBLIC_ASSETS_DIR:-${INSTALL_DIR}/public}"

  mkdir -p "${public_assets_dir}" "${public_assets_dir}/cli"
  touch "${ENV_FILE}"

  if grep -q '^FASCINATE_PUBLIC_ASSETS_DIR=' "${ENV_FILE}"; then
    sed -i.bak "s#^FASCINATE_PUBLIC_ASSETS_DIR=.*#FASCINATE_PUBLIC_ASSETS_DIR=$(quote_env_value "${public_assets_dir}")#" "${ENV_FILE}"
    rm -f "${ENV_FILE}.bak"
    return
  fi

  printf '\nFASCINATE_PUBLIC_ASSETS_DIR=%s\n' "$(quote_env_value "${public_assets_dir}")" >>"${ENV_FILE}"
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

assert_artifact_layout() {
  if [[ ! -f "${ARTIFACT_MANIFEST_PATH}" || ! -f "${PAYLOAD_BINARY_PATH}" || ! -f "${PAYLOAD_WEB_DIST_DIR}/index.html" ]]; then
    echo "this installer must run from an unpacked full release artifact" >&2
    echo "use ops/release/deploy-full-artifact.sh from a workstation or CI runner" >&2
    exit 1
  fi
}

stage_release_dir() {
  local release_dir="$1"
  local staging_dir

  mkdir -p "${RELEASES_DIR}"
  staging_dir="$(mktemp -d "${RELEASES_DIR}/.release.XXXXXX")"
  cp -R "${ARTIFACT_ROOT}/." "${staging_dir}/"
  rm -rf "${release_dir}"
  mv "${staging_dir}" "${release_dir}"
}

install_binary() {
  local release_dir="$1"
  mkdir -p "${BIN_DIR}" "${DATA_DIR}"
  install -m 0755 "${release_dir}/payload/bin/fascinate" "${BINARY_PATH}"
}

install_web_dist() {
  local release_dir="$1"
  rm -rf "${WEB_DIST_TARGET_DIR}"
  mkdir -p "${WEB_DIST_TARGET_DIR}"
  cp -R "${release_dir}/payload/web/dist/." "${WEB_DIST_TARGET_DIR}/"
}

install_systemd_unit() {
  local release_dir="$1"
  install -m 0644 "${release_dir}/ops/systemd/fascinate.service" "${SERVICE_PATH}"
}

persist_release_state() {
  local release_dir="$1"
  local installed_at

  installed_at="$(utc_now)"
  update_installed_release_state "${RELEASE_STATE_PATH}" "full" "${release_dir}/manifest.json" "${release_dir}" "${installed_at}"
}

main() {
  require_root
  require_command jq
  assert_artifact_layout
  bash "${ARTIFACT_ROOT}/ops/release/verify-artifact.sh" --expect-type full "${ARTIFACT_ROOT}" >/dev/null

  local release_id
  local release_dir
  release_id="$(jq -r '.releaseID' "${ARTIFACT_MANIFEST_PATH}")"
  if [[ -z "${release_id}" || "${release_id}" == "null" ]]; then
    echo "artifact manifest is missing releaseID" >&2
    exit 1
  fi

  release_dir="${RELEASES_DIR}/${release_id}"
  stage_release_dir "${release_dir}"
  install_binary "${release_dir}"
  install_web_dist "${release_dir}"
  install_systemd_unit "${release_dir}"

  if [[ ! -f "${ENV_FILE}" || "${OVERWRITE_ENV}" == "1" ]]; then
    write_env_file
  else
    ensure_public_assets_env
  fi

  set -a
  # shellcheck disable=SC1090
  source "${ENV_FILE}"
  set +a

  bash "${release_dir}/ops/host/write-caddyfile.sh"

  systemctl daemon-reload
  systemctl enable --now fascinate
  systemctl restart fascinate
  systemctl reload caddy

  persist_release_state "${release_dir}"

  echo "fascinate deployed from artifact ${release_id}"
  echo "service: $(systemctl is-active fascinate)"
  echo "caddy: $(systemctl is-active caddy)"
  curl -fsS "http://${FASCINATE_HTTP_ADDR}/healthz"
  persist_release_state "${release_dir}"
}

main "$@"
