#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/../.." && pwd)"
source "${SCRIPT_DIR}/lib.sh"

OUTPUT_DIR="${FASCINATE_RELEASE_OUTPUT_DIR:-${REPO_ROOT}/.tmp/releases}"
TARGET_OS="${FASCINATE_TARGET_OS:-linux}"
TARGET_ARCH="${FASCINATE_TARGET_ARCH:-amd64}"
RELEASE_ID="${FASCINATE_RELEASE_ID:-}"
WORK_DIR=""

cleanup() {
  if [[ -n "${WORK_DIR}" && -d "${WORK_DIR}" ]]; then
    rm -rf "${WORK_DIR}"
  fi
}

copy_install_assets() {
  local artifact_root="$1"

  mkdir -p \
    "${artifact_root}/ops/host" \
    "${artifact_root}/ops/release" \
    "${artifact_root}/payload/web"

  install -m 0755 "${REPO_ROOT}/ops/host/deploy-web.sh" "${artifact_root}/ops/host/deploy-web.sh"
  install -m 0644 "${REPO_ROOT}/ops/release/lib.sh" "${artifact_root}/ops/release/lib.sh"
  install -m 0755 "${REPO_ROOT}/ops/release/verify-artifact.sh" "${artifact_root}/ops/release/verify-artifact.sh"
}

build_web_dist() {
  local artifact_root="$1"
  local pnpm_cmd

  pnpm_cmd="$(resolve_pnpm)"
  echo "building web/dist" >&2
  (
    cd "${REPO_ROOT}/web"
    rm -rf dist
    eval "${pnpm_cmd} install --frozen-lockfile"
    eval "${pnpm_cmd} build"
  )

  copy_tree_contents "${REPO_ROOT}/web/dist" "${artifact_root}/payload/web/dist"
}

main() {
  trap cleanup EXIT
  require_command jq
  require_command tar

  mkdir -p "${OUTPUT_DIR}"

  local source_revision
  local source_dirty
  local built_at
  local bundle_name
  local artifact_root
  local artifact_path

  source_revision="$(git_source_revision "${REPO_ROOT}")"
  source_dirty="$(git_dirty_flag "${REPO_ROOT}")"
  built_at="$(utc_now)"
  if [[ -z "${RELEASE_ID}" ]]; then
    RELEASE_ID="$(build_release_id "web" "${TARGET_OS}" "${TARGET_ARCH}" "${source_revision}" "${source_dirty}")"
  fi

  bundle_name="fascinate-${RELEASE_ID}"
  WORK_DIR="$(mktemp -d "${OUTPUT_DIR}/build-web.XXXXXX")"
  artifact_root="${WORK_DIR}/${bundle_name}"
  artifact_path="${OUTPUT_DIR}/${bundle_name}.tar.gz"

  mkdir -p "${artifact_root}"
  copy_install_assets "${artifact_root}"
  build_web_dist "${artifact_root}"
  write_manifest "${artifact_root}" "web" "${RELEASE_ID}" "${built_at}" "${TARGET_OS}" "${TARGET_ARCH}" "${source_revision}" "${source_dirty}"
  bash "${SCRIPT_DIR}/verify-artifact.sh" --expect-type web "${artifact_root}" >/dev/null

  echo "packing ${artifact_path}" >&2
  COPYFILE_DISABLE=1 LC_ALL=C LANG=C tar -C "${WORK_DIR}" -czf "${artifact_path}" "${bundle_name}"
  bash "${SCRIPT_DIR}/verify-artifact.sh" --expect-type web "${artifact_path}" >/dev/null
  printf '%s\n' "${artifact_path}"
}

main "$@"
