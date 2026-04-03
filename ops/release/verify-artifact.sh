#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/lib.sh"

EXPECT_TYPE=""
ARTIFACT_INPUT=""
UNPACK_DIR=""

cleanup() {
  if [[ -n "${UNPACK_DIR}" && -d "${UNPACK_DIR}" ]]; then
    rm -rf "${UNPACK_DIR}"
  fi
}

usage() {
  cat <<'EOF'
usage: verify-artifact.sh [--expect-type full|web] <artifact-dir-or-tar.gz>
EOF
}

parse_args() {
  while [[ "$#" -gt 0 ]]; do
    case "$1" in
      --expect-type)
        EXPECT_TYPE="${2:-}"
        shift 2
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      *)
        ARTIFACT_INPUT="$1"
        shift
        ;;
    esac
  done

  if [[ -z "${ARTIFACT_INPUT}" ]]; then
    usage
    exit 1
  fi
}

artifact_root() {
  local input_path="$1"
  if [[ -d "${input_path}" ]]; then
    (cd -- "${input_path}" && pwd)
    return
  fi

  if [[ ! -f "${input_path}" ]]; then
    echo "artifact input does not exist: ${input_path}" >&2
    exit 1
  fi

  require_command tar
  UNPACK_DIR="$(mktemp -d)"
  LC_ALL=C LANG=C tar -xzf "${input_path}" -C "${UNPACK_DIR}"

  local unpacked_root
  unpacked_root="$(find "${UNPACK_DIR}" -mindepth 1 -maxdepth 1 -type d | head -n 1)"
  if [[ -z "${unpacked_root}" ]]; then
    echo "artifact archive did not contain a top-level directory" >&2
    exit 1
  fi
  printf '%s\n' "${unpacked_root}"
}

verify_required_paths() {
  local root="$1"
  local artifact_type="$2"
  local required_paths=()

  case "${artifact_type}" in
    full)
      required_paths=(
        "manifest.json"
        "ops/host/install-control-plane.sh"
        "ops/host/write-caddyfile.sh"
        "ops/release/lib.sh"
        "ops/release/verify-artifact.sh"
        "ops/systemd/fascinate.service"
        "payload/bin/fascinate"
        "payload/web/dist/index.html"
      )
      ;;
    web)
      required_paths=(
        "manifest.json"
        "ops/host/deploy-web.sh"
        "ops/release/lib.sh"
        "ops/release/verify-artifact.sh"
        "payload/web/dist/index.html"
      )
      ;;
    *)
      echo "unsupported artifact type in manifest: ${artifact_type}" >&2
      exit 1
      ;;
  esac

  local path
  for path in "${required_paths[@]}"; do
    if [[ ! -e "${root}/${path}" ]]; then
      echo "artifact is missing required path: ${path}" >&2
      exit 1
    fi
  done
}

verify_manifest_shape() {
  local manifest_path="$1"

  jq -e '
    .schemaVersion == 1 and
    (.artifactType | type == "string" and length > 0) and
    (.releaseID | type == "string" and length > 0) and
    (.builtAt | type == "string" and length > 0) and
    ((.sourceRevision == null) or (.sourceRevision | type == "string")) and
    (.sourceDirty | type == "boolean") and
    (.targetOS | type == "string" and length > 0) and
    (.targetArch | type == "string" and length > 0) and
    (.payload | type == "array" and length > 0) and
    (all(.payload[]; (.path | type == "string" and length > 0) and (.sha256 | type == "string" and length == 64) and (.size | type == "number" and . >= 0)))
  ' "${manifest_path}" >/dev/null
}

verify_payload_checksums() {
  local root="$1"
  local manifest_path="$2"
  local entry_count

  entry_count="$(jq '.payload | length' "${manifest_path}")"
  if [[ "${entry_count}" -eq 0 ]]; then
    echo "artifact manifest has no payload entries" >&2
    exit 1
  fi

  jq -r '.payload[] | [.path, .sha256, (.size | tostring)] | @tsv' "${manifest_path}" | \
    while IFS=$'\t' read -r relative_path expected_sha expected_size; do
      if [[ "${relative_path}" == /* || "${relative_path}" == *"../"* || "${relative_path}" == ".."* ]]; then
        echo "artifact manifest path is not safe: ${relative_path}" >&2
        exit 1
      fi

      if [[ ! -f "${root}/${relative_path}" ]]; then
        echo "artifact payload file is missing: ${relative_path}" >&2
        exit 1
      fi

      local actual_sha
      actual_sha="$(sha256_file "${root}/${relative_path}")"
      if [[ "${actual_sha}" != "${expected_sha}" ]]; then
        echo "artifact checksum mismatch for ${relative_path}" >&2
        exit 1
      fi

      local actual_size
      actual_size="$(file_size_bytes "${root}/${relative_path}")"
      if [[ "${actual_size}" != "${expected_size}" ]]; then
        echo "artifact size mismatch for ${relative_path}" >&2
        exit 1
      fi
    done
}

main() {
  trap cleanup EXIT
  parse_args "$@"
  require_command jq
  local root
  local manifest_path
  local artifact_type

  root="$(artifact_root "${ARTIFACT_INPUT}")"
  manifest_path="${root}/manifest.json"

  if [[ ! -f "${manifest_path}" ]]; then
    echo "artifact is missing manifest.json" >&2
    exit 1
  fi

  verify_manifest_shape "${manifest_path}"
  artifact_type="$(jq -r '.artifactType' "${manifest_path}")"

  if [[ -n "${EXPECT_TYPE}" && "${artifact_type}" != "${EXPECT_TYPE}" ]]; then
    echo "expected artifact type ${EXPECT_TYPE}, found ${artifact_type}" >&2
    exit 1
  fi

  verify_required_paths "${root}" "${artifact_type}"
  verify_payload_checksums "${root}" "${manifest_path}"

  echo "verified ${artifact_type} artifact: ${root}"
}

main "$@"
