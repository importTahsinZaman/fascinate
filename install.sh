#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${FASCINATE_INSTALL_BASE_URL:-https://downloads.fascinate.dev/cli}"
INSTALL_DIR="${FASCINATE_INSTALL_DIR:-${HOME}/.local/bin}"
REQUESTED_VERSION="${FASCINATE_VERSION:-latest}"
TMP_DIR=""

cleanup() {
  if [[ -n "${TMP_DIR}" && -d "${TMP_DIR}" ]]; then
    rm -rf "${TMP_DIR}"
  fi
}

usage() {
  cat <<'EOF'
usage: install.sh [--version <version>] [--install-dir <path>]
EOF
}

require() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

sha256_file() {
  local path="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "${path}" | awk '{print $1}'
    return
  fi
  shasum -a 256 "${path}" | awk '{print $1}'
}

detect_os() {
  case "$(uname -s)" in
    Linux) printf 'linux' ;;
    Darwin) printf 'darwin' ;;
    *) echo "unsupported operating system: $(uname -s)" >&2; exit 1 ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) printf 'amd64' ;;
    arm64|aarch64) printf 'arm64' ;;
    *) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;;
  esac
}

parse_args() {
  while [[ "$#" -gt 0 ]]; do
    case "$1" in
      --version)
        REQUESTED_VERSION="${2:-}"
        shift 2
        ;;
      --install-dir)
        INSTALL_DIR="${2:-}"
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
}

main() {
  trap cleanup EXIT
  require curl
  require jq
  require tar

  parse_args "$@"

  local target_os
  local target_arch
  local index_path
  local resolved_version
  local artifact_url
  local artifact_sha
  local artifact_path
  local unpack_dir
  local artifact_root

  target_os="$(detect_os)"
  target_arch="$(detect_arch)"
  TMP_DIR="$(mktemp -d)"
  index_path="${TMP_DIR}/index.json"

  curl -fsSL "${BASE_URL%/}/index.json" -o "${index_path}"

  resolved_version="$(jq -r --arg version "${REQUESTED_VERSION}" '
    if $version == "latest" then .latestVersion else $version end
  ' "${index_path}")"
  if [[ -z "${resolved_version}" || "${resolved_version}" == "null" ]]; then
    echo "unable to resolve requested version: ${REQUESTED_VERSION}" >&2
    exit 1
  fi

  artifact_url="$(jq -r --arg version "${resolved_version}" --arg os "${target_os}" --arg arch "${target_arch}" '
    first(.releases[] | select(.version == $version) | .artifacts[] | select(.targetOS == $os and .targetArch == $arch) | .url)
  ' "${index_path}")"
  artifact_sha="$(jq -r --arg version "${resolved_version}" --arg os "${target_os}" --arg arch "${target_arch}" '
    first(.releases[] | select(.version == $version) | .artifacts[] | select(.targetOS == $os and .targetArch == $arch) | .sha256)
  ' "${index_path}")"
  if [[ -z "${artifact_url}" || "${artifact_url}" == "null" ]]; then
    echo "no fascinate CLI artifact is published for version ${resolved_version} on ${target_os}/${target_arch}" >&2
    exit 1
  fi

  artifact_path="${TMP_DIR}/artifact.tar.gz"
  curl -fsSL "${artifact_url}" -o "${artifact_path}"
  if [[ "$(sha256_file "${artifact_path}")" != "${artifact_sha}" ]]; then
    echo "checksum verification failed for ${artifact_url}" >&2
    exit 1
  fi

  unpack_dir="${TMP_DIR}/unpack"
  mkdir -p "${unpack_dir}"
  tar -xzf "${artifact_path}" -C "${unpack_dir}"
  artifact_root="$(find "${unpack_dir}" -mindepth 1 -maxdepth 1 -type d | head -n 1)"
  if [[ ! -x "${artifact_root}/payload/bin/fascinate" ]]; then
    echo "downloaded artifact does not contain the fascinate CLI binary" >&2
    exit 1
  fi

  mkdir -p "${INSTALL_DIR}"
  install -m 0755 "${artifact_root}/payload/bin/fascinate" "${INSTALL_DIR}/fascinate"
  printf 'Installed fascinate %s to %s/fascinate\n' "${resolved_version}" "${INSTALL_DIR}"

  case ":${PATH}:" in
    *":${INSTALL_DIR}:"*) ;;
    *)
      printf 'Warning: %s is not on PATH\n' "${INSTALL_DIR}" >&2
      ;;
  esac
}

main "$@"
