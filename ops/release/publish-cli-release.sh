#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/../.." && pwd)"
source "${SCRIPT_DIR}/lib.sh"

DEPLOY_HOST="${FASCINATE_DEPLOY_HOST:-}"
DEPLOY_USER="${FASCINATE_DEPLOY_USER:-ubuntu}"
DEPLOY_PORT="${FASCINATE_DEPLOY_PORT:-22}"
PUBLIC_DIR="${FASCINATE_CLI_PUBLIC_DIR:-/opt/fascinate/public}"
PUBLIC_BASE_URL="${FASCINATE_CLI_PUBLIC_BASE_URL:-https://downloads.fascinate.dev/cli}"
LOCAL_DIR=""
OUTPUT_DIR="${FASCINATE_RELEASE_OUTPUT_DIR:-${REPO_ROOT}/.tmp/cli-publish}"
VERSION=""
LATEST_VERSION=""
TARGETS=()
WORK_DIR=""

cleanup() {
  if [[ -n "${WORK_DIR}" && -d "${WORK_DIR}" ]]; then
    rm -rf "${WORK_DIR}"
  fi
}

usage() {
  cat <<'EOF'
usage: publish-cli-release.sh --version <version> [--latest <version>] [--target <os/arch>]... [--local-dir <path>]

Environment:
  FASCINATE_DEPLOY_HOST           remote host for publish
  FASCINATE_DEPLOY_USER           ssh user (default: ubuntu)
  FASCINATE_DEPLOY_PORT           ssh port (default: 22)
  FASCINATE_CLI_PUBLIC_DIR        remote public assets dir (default: /opt/fascinate/public)
  FASCINATE_CLI_PUBLIC_BASE_URL   public CLI base URL used in index.json (default: https://downloads.fascinate.dev/cli)
  FASCINATE_RELEASE_OUTPUT_DIR    local build staging dir

Examples:
  bash ops/release/publish-cli-release.sh --version 0.1.0
  bash ops/release/publish-cli-release.sh --version 0.1.0 --latest 0.1.0
  bash ops/release/publish-cli-release.sh --version 0.1.0 --local-dir .tmp/public
EOF
}

parse_args() {
  while [[ "$#" -gt 0 ]]; do
    case "$1" in
      --version)
        VERSION="${2:-}"
        shift 2
        ;;
      --latest)
        LATEST_VERSION="${2:-}"
        shift 2
        ;;
      --local-dir)
        LOCAL_DIR="${2:-}"
        shift 2
        ;;
      --output-dir)
        OUTPUT_DIR="${2:-}"
        shift 2
        ;;
      --public-dir)
        PUBLIC_DIR="${2:-}"
        shift 2
        ;;
      --base-url)
        PUBLIC_BASE_URL="${2:-}"
        shift 2
        ;;
      --target)
        TARGETS+=("${2:-}")
        shift 2
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      *)
        echo "unknown argument: $1" >&2
        usage
        exit 1
        ;;
    esac
  done

  if [[ -z "${VERSION}" ]]; then
    echo "--version is required" >&2
    usage
    exit 1
  fi
  if [[ -z "${LATEST_VERSION}" ]]; then
    LATEST_VERSION="${VERSION}"
  fi
  if [[ -z "${LOCAL_DIR}" && -z "${DEPLOY_HOST}" ]]; then
    echo "either --local-dir or FASCINATE_DEPLOY_HOST is required" >&2
    usage
    exit 1
  fi
  if [[ -n "${LOCAL_DIR}" && -n "${DEPLOY_HOST}" ]]; then
    echo "use either --local-dir or remote deploy environment, not both" >&2
    exit 1
  fi
  if [[ "${#TARGETS[@]}" -eq 0 ]]; then
    TARGETS=("linux/amd64" "linux/arm64" "darwin/amd64" "darwin/arm64")
  fi
}

assert_targets() {
  local target
  for target in "${TARGETS[@]}"; do
    if [[ "${target}" != */* ]]; then
      echo "invalid target ${target}; expected os/arch" >&2
      exit 1
    fi
  done
}

copy_existing_index() {
  local destination="$1"

  if [[ -n "${LOCAL_DIR}" ]]; then
    if [[ -f "${LOCAL_DIR%/}/cli/index.json" ]]; then
      cp "${LOCAL_DIR%/}/cli/index.json" "${destination}"
    fi
    return
  fi

  ssh -p "${DEPLOY_PORT}" "${DEPLOY_USER}@${DEPLOY_HOST}" "if sudo test -f '${PUBLIC_DIR%/}/cli/index.json'; then sudo cat '${PUBLIC_DIR%/}/cli/index.json'; else true; fi" >"${destination}"
  if [[ ! -s "${destination}" ]]; then
    rm -f "${destination}"
  fi
}

build_artifacts() {
  local artifact_dir="$1"
  local artifacts_output="$2"
  local target
  local target_os
  local target_arch
  local artifact_path

  : >"${artifacts_output}"
  for target in "${TARGETS[@]}"; do
    target_os="${target%%/*}"
    target_arch="${target#*/}"
    artifact_path="$(
      cd "${REPO_ROOT}"
      FASCINATE_RELEASE_OUTPUT_DIR="${artifact_dir}" \
      FASCINATE_TARGET_OS="${target_os}" \
      FASCINATE_TARGET_ARCH="${target_arch}" \
      bash ops/release/build-cli-artifact.sh | tail -n 1
    )"
    printf '%s\n' "${artifact_path}" >>"${artifacts_output}"
  done
}

stage_publish_tree() {
  local public_stage="$1"
  local artifacts_output="$2"
  local artifact_path
  local artifact_paths=()

  mkdir -p "${public_stage}/cli"
  install -m 0644 "${REPO_ROOT}/install.sh" "${public_stage}/install.sh"

  while IFS= read -r artifact_path; do
    [[ -z "${artifact_path}" ]] && continue
    artifact_paths+=("${artifact_path}")
    install -m 0644 "${artifact_path}" "${public_stage}/cli/$(basename "${artifact_path}")"
  done <"${artifacts_output}"

  bash "${SCRIPT_DIR}/build-cli-release-index.sh" \
    --base-url "${PUBLIC_BASE_URL}" \
    --version "${VERSION}" \
    --latest "${LATEST_VERSION}" \
    --output "${public_stage}/cli/index.json" \
    "${artifact_paths[@]}"
}

publish_local() {
  local public_stage="$1"

  mkdir -p "${LOCAL_DIR%/}/cli"
  install -m 0644 "${public_stage}/install.sh" "${LOCAL_DIR%/}/install.sh"
  find "${public_stage}/cli" -mindepth 1 -maxdepth 1 -type f -print0 | while IFS= read -r -d '' path; do
    install -m 0644 "${path}" "${LOCAL_DIR%/}/cli/$(basename "${path}")"
  done
  printf 'Published CLI install assets to %s\n' "${LOCAL_DIR%/}"
}

publish_remote() {
  local public_stage="$1"
  local remote_target
  local remote_script

  require_command ssh

  remote_target="${DEPLOY_USER}@${DEPLOY_HOST}"
  remote_script=$(cat <<EOF
set -euo pipefail
staging_dir=\$(mktemp -d /tmp/fascinate-cli-publish.XXXXXX)
cleanup() {
  rm -rf "\${staging_dir}"
}
trap cleanup EXIT
cat >"\${staging_dir}/publish.tar.gz"
tar -xzf "\${staging_dir}/publish.tar.gz" -C "\${staging_dir}"
sudo mkdir -p "${PUBLIC_DIR%/}" "${PUBLIC_DIR%/}/cli"
sudo install -m 0644 "\${staging_dir}/install.sh" "${PUBLIC_DIR%/}/install.sh"
find "\${staging_dir}/cli" -mindepth 1 -maxdepth 1 -type f -print0 | while IFS= read -r -d '' file_path; do
  sudo install -m 0644 "\${file_path}" "${PUBLIC_DIR%/}/cli/\$(basename "\${file_path}")"
done
sudo find "${PUBLIC_DIR%/}" -name '._*' -delete
EOF
)

  COPYFILE_DISABLE=1 LC_ALL=C LANG=C tar -C "${public_stage}" -czf - install.sh cli | ssh -p "${DEPLOY_PORT}" "${remote_target}" "bash -lc $(printf '%q' "${remote_script}")"
  printf 'Published CLI install assets to %s:%s\n' "${remote_target}" "${PUBLIC_DIR%/}"
}

print_summary() {
  local artifacts_output="$1"
  local artifact_path
  local install_location

  if [[ -n "${LOCAL_DIR}" ]]; then
    install_location="${LOCAL_DIR%/}/install.sh"
  else
    install_location="https://fascinate.dev/install.sh"
  fi

  printf 'Install entry: %s\n' "${install_location}"
  printf 'CLI index URL: %s/index.json\n' "${PUBLIC_BASE_URL%/}"
  printf 'Published version: %s\n' "${VERSION}"
  printf 'Latest version pointer: %s\n' "${LATEST_VERSION}"
  printf 'Artifacts:\n'
  while IFS= read -r artifact_path; do
    [[ -z "${artifact_path}" ]] && continue
    printf '  - %s\n' "$(basename "${artifact_path}")"
  done <"${artifacts_output}"
}

main() {
  trap cleanup EXIT
  require_command go
  require_command jq
  require_command tar
  parse_args "$@"
  assert_targets

  local artifact_dir
  local public_stage
  local artifacts_output

  mkdir -p "${OUTPUT_DIR}"
  WORK_DIR="$(mktemp -d "${OUTPUT_DIR%/}/publish-cli.${VERSION}.XXXXXX")"
  artifact_dir="${WORK_DIR}/artifacts"
  public_stage="${WORK_DIR}/public"
  artifacts_output="${WORK_DIR}/artifacts.txt"
  mkdir -p "${artifact_dir}" "${public_stage}/cli"

  copy_existing_index "${public_stage}/cli/index.json"
  build_artifacts "${artifact_dir}" "${artifacts_output}"
  stage_publish_tree "${public_stage}" "${artifacts_output}"

  if [[ -n "${LOCAL_DIR}" ]]; then
    publish_local "${public_stage}"
  else
    publish_remote "${public_stage}"
  fi
  print_summary "${artifacts_output}"
}

main "$@"
