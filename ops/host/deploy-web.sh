#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/../.." && pwd)"

INSTALL_DIR="${FASCINATE_INSTALL_DIR:-/opt/fascinate}"
WEB_SOURCE_DIR="${REPO_ROOT}/web"
WEB_DIST_SOURCE_DIR="${WEB_SOURCE_DIR}/dist"
WEB_DIST_TARGET_DIR="${INSTALL_DIR}/web/dist"
CONFIG_DIR="${FASCINATE_CONFIG_DIR:-/etc/fascinate}"
ENV_FILE="${FASCINATE_ENV_FILE:-${CONFIG_DIR}/fascinate.env}"
FASCINATE_HTTP_ADDR_DEFAULT="${FASCINATE_HTTP_ADDR:-127.0.0.1:8080}"
STAGING_DIR=""

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

require_root() {
  if [[ "${EUID}" -ne 0 ]]; then
    echo "run as root" >&2
    exit 1
  fi
}

cleanup() {
  if [[ -n "${STAGING_DIR}" && -d "${STAGING_DIR}" ]]; then
    rm -rf "${STAGING_DIR}"
  fi
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

stage_web_dist() {
  STAGING_DIR="$(mktemp -d /tmp/fascinate-web-dist.XXXXXX)"
  cp -R "${WEB_DIST_SOURCE_DIR}/." "${STAGING_DIR}/"
}

copy_tree_contents() {
  local source_dir="${1}"
  local target_dir="${2}"

  if [[ ! -d "${source_dir}" ]]; then
    return
  fi

  mkdir -p "${target_dir}"
  cp -R "${source_dir}/." "${target_dir}/"
}

install_web_dist() {
  mkdir -p "${WEB_DIST_TARGET_DIR}"

  copy_tree_contents "${STAGING_DIR}/assets" "${WEB_DIST_TARGET_DIR}/assets"

  while IFS= read -r -d '' entry; do
    local name="${entry##*/}"
    local destination="${WEB_DIST_TARGET_DIR}/${name}"

    if [[ "${name}" == "assets" || "${name}" == "index.html" ]]; then
      continue
    fi

    if [[ -d "${entry}" ]]; then
      copy_tree_contents "${entry}" "${destination}"
      continue
    fi

    install -m 0644 "${entry}" "${destination}"
  done < <(find "${STAGING_DIR}" -mindepth 1 -maxdepth 1 -print0)

  if [[ ! -f "${STAGING_DIR}/index.html" ]]; then
    echo "web dist is missing index.html" >&2
    exit 1
  fi

  install -m 0644 "${STAGING_DIR}/index.html" "${WEB_DIST_TARGET_DIR}/index.html"
}

load_runtime_env() {
  if [[ -f "${ENV_FILE}" ]]; then
    set -a
    # shellcheck disable=SC1090
    source "${ENV_FILE}"
    set +a
  fi
}

main() {
  trap cleanup EXIT
  require_root
  build_web_dist
  stage_web_dist
  install_web_dist
  load_runtime_env

  echo "fascinate web bundle deployed without restarting fascinate"
  echo "service: $(systemctl is-active fascinate)"
  echo "caddy: $(systemctl is-active caddy)"
  curl -fsS "http://${FASCINATE_HTTP_ADDR:-${FASCINATE_HTTP_ADDR_DEFAULT}}/healthz"
}

main "$@"
