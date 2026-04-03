#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/lib.sh"

DEPLOY_HOST="${FASCINATE_DEPLOY_HOST:-}"
DEPLOY_USER="${FASCINATE_DEPLOY_USER:-ubuntu}"
DEPLOY_PORT="${FASCINATE_DEPLOY_PORT:-22}"
REMOTE_TMP_DIR="${FASCINATE_DEPLOY_REMOTE_TMP_DIR:-/tmp}"
ARTIFACT_PATH="${1:-}"

usage() {
  cat <<'EOF'
usage: deploy-web-artifact.sh [artifact.tar.gz]

Environment:
  FASCINATE_DEPLOY_HOST   required remote host or IP
  FASCINATE_DEPLOY_USER   ssh user (default: ubuntu)
  FASCINATE_DEPLOY_PORT   ssh port (default: 22)
EOF
}

build_passthrough_env_args() {
  local args=""
  local name
  while IFS='=' read -r name _; do
    [[ "${name}" == FASCINATE_DEPLOY_* ]] && continue
    [[ "${name}" == FASCINATE_RELEASE_* ]] && continue
    [[ "${name}" == FASCINATE_TARGET_* ]] && continue
    args+=" $(printf '%q' "${name}=${!name}")"
  done < <(env | awk -F= '/^FASCINATE_/ {print $1}' | LC_ALL=C sort)
  printf '%s' "${args}"
}

remote_ssh_target() {
  printf '%s@%s' "${DEPLOY_USER}" "${DEPLOY_HOST}"
}

build_artifact_path() {
  local output
  output="$(bash "${SCRIPT_DIR}/build-web-artifact.sh")"
  printf '%s\n' "${output}" | tail -n 1
}

main() {
  require_command scp
  require_command ssh

  if [[ -z "${DEPLOY_HOST}" ]]; then
    usage
    exit 1
  fi

  if [[ -z "${ARTIFACT_PATH}" ]]; then
    ARTIFACT_PATH="$(build_artifact_path)"
  fi

  if [[ ! -f "${ARTIFACT_PATH}" ]]; then
    echo "artifact does not exist: ${ARTIFACT_PATH}" >&2
    exit 1
  fi

  bash "${SCRIPT_DIR}/verify-artifact.sh" --expect-type web "${ARTIFACT_PATH}" >/dev/null

  local target
  local remote_archive
  local remote_env_args
  local remote_script

  target="$(remote_ssh_target)"
  remote_archive="${REMOTE_TMP_DIR%/}/$(basename "${ARTIFACT_PATH}")"
  remote_env_args="$(build_passthrough_env_args)"

  echo "uploading $(basename "${ARTIFACT_PATH}") to ${target}" >&2
  scp -P "${DEPLOY_PORT}" "${ARTIFACT_PATH}" "${target}:${remote_archive}"

  remote_script=$(cat <<EOF
set -euo pipefail
staging_dir=\$(mktemp -d "${REMOTE_TMP_DIR%/}/fascinate-web.XXXXXX")
cleanup() {
  rm -rf "\${staging_dir}"
  rm -f "${remote_archive}"
}
trap cleanup EXIT
tar -xzf "${remote_archive}" -C "\${staging_dir}"
artifact_root=\$(find "\${staging_dir}" -mindepth 1 -maxdepth 1 -type d | head -n 1)
if [[ -z "\${artifact_root}" ]]; then
  echo "failed to unpack artifact" >&2
  exit 1
fi
sudo env${remote_env_args} "\${artifact_root}/ops/host/deploy-web.sh"
EOF
)

  ssh -p "${DEPLOY_PORT}" "${target}" "bash -lc $(printf '%q' "${remote_script}")"
}

main "$@"
