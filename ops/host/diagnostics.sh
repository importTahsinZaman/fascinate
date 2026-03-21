#!/usr/bin/env bash
set -euo pipefail

ENV_FILE="${FASCINATE_ENV_FILE:-/etc/fascinate/fascinate.env}"

usage() {
  cat <<'EOF'
usage:
  diagnostics.sh machine <owner_email> <machine_name>
  diagnostics.sh snapshot <owner_email> <snapshot_name>
  diagnostics.sh tool-auth <owner_email>
  diagnostics.sh events <owner_email> [limit]
EOF
}

require_command() {
  local name="$1"
  if ! command -v "${name}" >/dev/null 2>&1; then
    echo "missing required command: ${name}" >&2
    exit 1
  fi
}

api_url() {
  printf 'http://%s%s' "${FASCINATE_HTTP_ADDR}" "$1"
}

main() {
  require_command curl
  require_command jq

  if [[ ! -f "${ENV_FILE}" ]]; then
    echo "missing env file: ${ENV_FILE}" >&2
    exit 1
  fi

  set -a
  # shellcheck disable=SC1090
  source "${ENV_FILE}"
  set +a

  local command="${1:-}"
  case "${command}" in
    machine)
      local owner_email="${2:-}"
      local machine_name="${3:-}"
      if [[ -z "${owner_email}" || -z "${machine_name}" ]]; then
        usage
        exit 1
      fi
      curl -fsS "$(api_url "/v1/diagnostics/machines/${machine_name}?owner_email=${owner_email}")" | jq .
      ;;
    snapshot)
      local owner_email="${2:-}"
      local snapshot_name="${3:-}"
      if [[ -z "${owner_email}" || -z "${snapshot_name}" ]]; then
        usage
        exit 1
      fi
      curl -fsS "$(api_url "/v1/diagnostics/snapshots/${snapshot_name}?owner_email=${owner_email}")" | jq .
      ;;
    tool-auth)
      local owner_email="${2:-}"
      if [[ -z "${owner_email}" ]]; then
        usage
        exit 1
      fi
      curl -fsS "$(api_url "/v1/diagnostics/tool-auth?owner_email=${owner_email}")" | jq .
      ;;
    events)
      local owner_email="${2:-}"
      local limit="${3:-50}"
      if [[ -z "${owner_email}" ]]; then
        usage
        exit 1
      fi
      curl -fsS "$(api_url "/v1/diagnostics/events?owner_email=${owner_email}&limit=${limit}")" | jq .
      ;;
    *)
      usage
      exit 1
      ;;
  esac
}

main "$@"
