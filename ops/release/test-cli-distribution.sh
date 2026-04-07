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
  local missing_sha_dir
  local traversal_dir
  local artifact_path
  local output

  work_dir="$(mktemp -d)"
  artifact_dir="${work_dir}/artifacts"
  install_dir="${work_dir}/install"
  pinned_dir="${work_dir}/pinned"
  bad_dir="${work_dir}/bad"
  missing_sha_dir="${work_dir}/missing-sha"
  traversal_dir="${work_dir}/traversal"
  mkdir -p "${artifact_dir}" "${install_dir}" "${pinned_dir}" "${bad_dir}" "${missing_sha_dir}" "${traversal_dir}"
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

    cp "${artifact_path}" "${missing_sha_dir}/$(basename "${artifact_path}")"
    cp "${artifact_dir}/index.json" "${missing_sha_dir}/index.json"
    python3 - <<'PY' "${missing_sha_dir}/index.json"
import json, sys
path = sys.argv[1]
with open(path, "r", encoding="utf-8") as fh:
    data = json.load(fh)
del data["releases"][0]["artifacts"][0]["sha256"]
with open(path, "w", encoding="utf-8") as fh:
    json.dump(data, fh)
PY
    if FASCINATE_INSTALL_BASE_URL="file://${missing_sha_dir}" FASCINATE_INSTALL_DIR="${work_dir}/missing-sha-install" bash ./install.sh >/dev/null 2>&1; then
      echo "expected missing checksum install to fail" >&2
      exit 1
    fi

    if FASCINATE_INSTALL_BASE_URL="http://example.com/cli" FASCINATE_INSTALL_DIR="${work_dir}/insecure-install" bash ./install.sh >/dev/null 2>&1; then
      echo "expected insecure http install base to fail" >&2
      exit 1
    fi

    cp "${artifact_dir}/index.json" "${traversal_dir}/index.json"
    python3 - <<'PY' "${traversal_dir}/evil.tar.gz" "${traversal_dir}/index.json"
import hashlib, io, json, sys, tarfile
archive_path, index_path = sys.argv[1], sys.argv[2]
with tarfile.open(archive_path, "w:gz") as tf:
    info = tarfile.TarInfo(name="../../escape.txt")
    payload = b"owned"
    info.size = len(payload)
    tf.addfile(info, io.BytesIO(payload))
with open(archive_path, "rb") as fh:
    sha = hashlib.sha256(fh.read()).hexdigest()
with open(index_path, "r", encoding="utf-8") as fh:
    data = json.load(fh)
data["releases"][0]["artifacts"][0]["url"] = "file://" + archive_path
data["releases"][0]["artifacts"][0]["sha256"] = sha
with open(index_path, "w", encoding="utf-8") as fh:
    json.dump(data, fh)
PY
    if FASCINATE_INSTALL_BASE_URL="file://${traversal_dir}" FASCINATE_INSTALL_DIR="${work_dir}/traversal-install" bash ./install.sh >/dev/null 2>&1; then
      echo "expected traversal archive install to fail" >&2
      exit 1
    fi
  )
}

main "$@"
