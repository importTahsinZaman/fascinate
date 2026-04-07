#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/../.." && pwd)"

require() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

assert_contains() {
  local haystack="$1"
  local needle="$2"
  if [[ "${haystack}" != *"${needle}"* ]]; then
    echo "expected output to contain: ${needle}" >&2
    echo "got: ${haystack}" >&2
    exit 1
  fi
}

main() {
  require go
  require jq
  require tar
  require curl

  work_dir=""
  local artifact_dir
  local install_dir
  local pinned_dir
  local bad_dir
  local artifact_path
  local output

  work_dir="$(mktemp -d)"
  artifact_dir="${work_dir}/artifacts"
  install_dir="${work_dir}/install"
  pinned_dir="${work_dir}/pinned"
  bad_dir="${work_dir}/bad"
  mkdir -p "${artifact_dir}" "${install_dir}" "${pinned_dir}" "${bad_dir}"
  trap 'rm -rf "${work_dir}"' EXIT

  (
    cd "${REPO_ROOT}"
    export FASCINATE_RELEASE_OUTPUT_DIR="${artifact_dir}"
    export FASCINATE_TARGET_OS="$(go env GOOS)"
    export FASCINATE_TARGET_ARCH="$(go env GOARCH)"
    artifact_path="$(bash ops/release/build-cli-artifact.sh)"
    bash ops/release/build-cli-release-index.sh \
      --base-url "file://${artifact_dir}" \
      --version "1.2.3" \
      --latest "1.2.3" \
      --output "${artifact_dir}/index.json" \
      "${artifact_path}"

    output="$(
      FASCINATE_INSTALL_BASE_URL="file://${artifact_dir}" \
      FASCINATE_INSTALL_DIR="${install_dir}" \
      bash ./install.sh
    )"
    assert_contains "${output}" "Installed fascinate"
    "${install_dir}/fascinate" version >/dev/null

    output="$(
      FASCINATE_INSTALL_BASE_URL="file://${artifact_dir}" \
      FASCINATE_INSTALL_DIR="${pinned_dir}" \
      FASCINATE_VERSION="1.2.3" \
      bash ./install.sh
    )"
    assert_contains "${output}" "Installed fascinate"
    "${pinned_dir}/fascinate" version >/dev/null

    cp "${artifact_path}" "${bad_dir}/$(basename "${artifact_path}")"
    cp "${artifact_dir}/index.json" "${bad_dir}/index.json"
    python3 - <<'PY' "${bad_dir}/index.json"
import json, sys
path = sys.argv[1]
with open(path, "r", encoding="utf-8") as fh:
    data = json.load(fh)
data["releases"][0]["artifacts"][0]["sha256"] = "0" * 64
with open(path, "w", encoding="utf-8") as fh:
    json.dump(data, fh)
PY
    if FASCINATE_INSTALL_BASE_URL="file://${bad_dir}" FASCINATE_INSTALL_DIR="${work_dir}/bad-install" bash ./install.sh >/dev/null 2>&1; then
      echo "expected checksum failure install to fail" >&2
      exit 1
    fi

    if FASCINATE_INSTALL_BASE_URL="file://${artifact_dir}" FASCINATE_INSTALL_DIR="${work_dir}/missing-install" FASCINATE_VERSION="9.9.9" bash ./install.sh >/dev/null 2>&1; then
      echo "expected missing version install to fail" >&2
      exit 1
    fi
  )
}

main "$@"
