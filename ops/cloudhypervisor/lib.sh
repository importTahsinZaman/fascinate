#!/usr/bin/env bash
set -euo pipefail

resolve_fascinate_binary() {
  if [[ -n "${FASCINATE_IMAGE_BINARY:-}" ]]; then
    if [[ ! -x "${FASCINATE_IMAGE_BINARY}" ]]; then
      echo "FASCINATE_IMAGE_BINARY is not executable: ${FASCINATE_IMAGE_BINARY}" >&2
      exit 1
    fi
    printf '%s\n' "${FASCINATE_IMAGE_BINARY}"
    return
  fi

  if command -v fascinate >/dev/null 2>&1; then
    command -v fascinate
    return
  fi

  if [[ -x /opt/fascinate/bin/fascinate ]]; then
    printf '%s\n' "/opt/fascinate/bin/fascinate"
    return
  fi

  echo "unable to locate fascinate binary; set FASCINATE_IMAGE_BINARY" >&2
  exit 1
}

run_image_command() {
  local subcommand="$1"
  shift

  local binary
  binary="$(resolve_fascinate_binary)"
  exec "${binary}" image "${subcommand}" "$@"
}
