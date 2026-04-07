#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/lib.sh"

BASE_URL=""
VERSION=""
LATEST_VERSION=""
OUTPUT_PATH=""
ARTIFACTS=()

usage() {
  cat <<'EOF'
usage: build-cli-release-index.sh --base-url <url> --version <version> --output <path> [--latest <version>] <artifact.tar.gz>...
EOF
}

parse_args() {
  while [[ "$#" -gt 0 ]]; do
    case "$1" in
      --base-url)
        BASE_URL="${2:-}"
        shift 2
        ;;
      --version)
        VERSION="${2:-}"
        shift 2
        ;;
      --latest)
        LATEST_VERSION="${2:-}"
        shift 2
        ;;
      --output)
        OUTPUT_PATH="${2:-}"
        shift 2
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      *)
        ARTIFACTS+=("$1")
        shift
        ;;
    esac
  done

  if [[ -z "${BASE_URL}" || -z "${VERSION}" || -z "${OUTPUT_PATH}" || "${#ARTIFACTS[@]}" -eq 0 ]]; then
    usage
    exit 1
  fi
  if [[ -z "${LATEST_VERSION}" ]]; then
    LATEST_VERSION="${VERSION}"
  fi
}

artifact_entry_json() {
  local artifact_path="$1"
  local unpack_dir
  local root
  local manifest_path
  local release_id
  local target_os
  local target_arch

  unpack_dir="$(mktemp -d)"
  trap 'rm -rf "${unpack_dir}"' RETURN

  bash "${SCRIPT_DIR}/verify-artifact.sh" --expect-type cli "${artifact_path}" >/dev/null
  tar -xzf "${artifact_path}" -C "${unpack_dir}"
  root="$(find "${unpack_dir}" -mindepth 1 -maxdepth 1 -type d | head -n 1)"
  manifest_path="${root}/manifest.json"
  release_id="$(jq -r '.releaseID' "${manifest_path}")"
  target_os="$(jq -r '.targetOS' "${manifest_path}")"
  target_arch="$(jq -r '.targetArch' "${manifest_path}")"

  jq -n \
    --arg releaseID "${release_id}" \
    --arg targetOS "${target_os}" \
    --arg targetArch "${target_arch}" \
    --arg url "${BASE_URL%/}/$(basename "${artifact_path}")" \
    --arg sha256 "$(sha256_file "${artifact_path}")" \
    --argjson size "$(file_size_bytes "${artifact_path}")" \
    '{
      releaseID: $releaseID,
      targetOS: $targetOS,
      targetArch: $targetArch,
      url: $url,
      sha256: $sha256,
      size: $size
    }'
}

main() {
  require_command jq
  require_command tar
  parse_args "$@"

  local existing_input
  local artifacts_json
  local tmp_output

  mkdir -p "$(dirname "${OUTPUT_PATH}")"

  artifacts_json="$(
    for artifact in "${ARTIFACTS[@]}"; do
      artifact_entry_json "${artifact}"
    done | jq -s '.'
  )"

  existing_input="$(mktemp)"
  tmp_output="$(mktemp)"
  if [[ -f "${OUTPUT_PATH}" ]]; then
    cp "${OUTPUT_PATH}" "${existing_input}"
  else
    printf '{}\n' >"${existing_input}"
  fi

  jq \
    --arg version "${VERSION}" \
    --arg latestVersion "${LATEST_VERSION}" \
    --arg generatedAt "$(utc_now)" \
    --argjson artifacts "${artifacts_json}" \
    '
      .schemaVersion = 1 |
      .generatedAt = $generatedAt |
      .latestVersion = $latestVersion |
      .releases = ((.releases // []) | map(select(.version != $version)) + [{version: $version, artifacts: $artifacts}])
    ' "${existing_input}" >"${tmp_output}"

  install -m 0644 "${tmp_output}" "${OUTPUT_PATH}"
  rm -f "${existing_input}" "${tmp_output}"
}

main "$@"
