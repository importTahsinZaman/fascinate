#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
ARTIFACT_ROOT="$(cd -- "${SCRIPT_DIR}/../.." && pwd)"
source "${ARTIFACT_ROOT}/ops/release/lib.sh"

INSTALL_DIR="${FASCINATE_INSTALL_DIR:-/opt/fascinate}"
RELEASES_DIR="${INSTALL_DIR}/releases"
RELEASE_STATE_PATH="${FASCINATE_RELEASE_MANIFEST_PATH:-${INSTALL_DIR}/release-manifest.json}"
PAYLOAD_WEB_DIST_DIR="${ARTIFACT_ROOT}/payload/web/dist"
WEB_DIST_TARGET_DIR="${INSTALL_DIR}/web/dist"
CONFIG_DIR="${FASCINATE_CONFIG_DIR:-/etc/fascinate}"
ENV_FILE="${FASCINATE_ENV_FILE:-${CONFIG_DIR}/fascinate.env}"
FASCINATE_HTTP_ADDR_DEFAULT="${FASCINATE_HTTP_ADDR:-127.0.0.1:8080}"
ARTIFACT_MANIFEST_PATH="${ARTIFACT_ROOT}/manifest.json"

assert_artifact_layout() {
  if [[ ! -f "${ARTIFACT_MANIFEST_PATH}" || ! -f "${PAYLOAD_WEB_DIST_DIR}/index.html" ]]; then
    echo "this installer must run from an unpacked web release artifact" >&2
    echo "use ops/release/deploy-web-artifact.sh from a workstation or CI runner" >&2
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

install_web_dist() {
  local source_dir="$1"
  mkdir -p "${WEB_DIST_TARGET_DIR}"

  copy_tree_contents "${source_dir}/assets" "${WEB_DIST_TARGET_DIR}/assets"

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
  done < <(find "${source_dir}" -mindepth 1 -maxdepth 1 -print0)

  if [[ ! -f "${source_dir}/index.html" ]]; then
    echo "web dist is missing index.html" >&2
    exit 1
  fi

  install -m 0644 "${source_dir}/index.html" "${WEB_DIST_TARGET_DIR}/index.html"
}

load_runtime_env() {
  if [[ -f "${ENV_FILE}" ]]; then
    set -a
    # shellcheck disable=SC1090
    source "${ENV_FILE}"
    set +a
  fi
}

persist_release_state() {
  local release_dir="$1"
  local installed_at

  installed_at="$(utc_now)"
  update_installed_release_state "${RELEASE_STATE_PATH}" "web" "${release_dir}/manifest.json" "${release_dir}" "${installed_at}"
}

main() {
  require_root
  require_command jq
  assert_artifact_layout
  bash "${ARTIFACT_ROOT}/ops/release/verify-artifact.sh" --expect-type web "${ARTIFACT_ROOT}" >/dev/null

  local release_id
  local release_dir
  release_id="$(jq -r '.releaseID' "${ARTIFACT_MANIFEST_PATH}")"
  if [[ -z "${release_id}" || "${release_id}" == "null" ]]; then
    echo "artifact manifest is missing releaseID" >&2
    exit 1
  fi

  release_dir="${RELEASES_DIR}/${release_id}"
  stage_release_dir "${release_dir}"
  install_web_dist "${release_dir}/payload/web/dist"
  load_runtime_env

  echo "fascinate web bundle deployed from artifact ${release_id} without restarting fascinate"
  echo "service: $(systemctl is-active fascinate)"
  echo "caddy: $(systemctl is-active caddy)"
  curl -fsS "http://${FASCINATE_HTTP_ADDR:-${FASCINATE_HTTP_ADDR_DEFAULT}}/healthz"
  persist_release_state "${release_dir}"
}

main "$@"
