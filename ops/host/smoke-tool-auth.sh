#!/usr/bin/env bash
set -euo pipefail

ENV_FILE="${FASCINATE_ENV_FILE:-/etc/fascinate/fascinate.env}"
SMOKE_EMAIL="${FASCINATE_SMOKE_EMAIL:-smoke@example.com}"
SMOKE_FIRST_NAME="${FASCINATE_TOOL_AUTH_FIRST_NAME:-claude-auth-1-$(date +%s)}"
SMOKE_SECOND_NAME="${FASCINATE_TOOL_AUTH_SECOND_NAME:-claude-auth-2-$(date +%s)}"
SMOKE_THIRD_NAME="${FASCINATE_TOOL_AUTH_THIRD_NAME:-claude-auth-3-$(date +%s)}"

require_root() {
  if [[ "${EUID}" -ne 0 ]]; then
    echo "run as root" >&2
    exit 1
  fi
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

delete_machine() {
  local name="$1"
  curl -fsS -X DELETE "$(api_url "/v1/machines/${name}?owner_email=${SMOKE_EMAIL}")" >/dev/null 2>&1 || true
}

cleanup() {
  delete_machine "${SMOKE_FIRST_NAME}"
  delete_machine "${SMOKE_SECOND_NAME}"
  delete_machine "${SMOKE_THIRD_NAME}"
}

wait_for_machine_state() {
  local name="$1"
  local want_state="$2"
  local attempts=120

  while (( attempts > 0 )); do
    local state
    state="$(curl -fsS "$(api_url "/v1/machines/${name}?owner_email=${SMOKE_EMAIL}")" 2>/dev/null | jq -r '.state // empty' || true)"
    if [[ "${state}" == "${want_state}" ]]; then
      return 0
    fi
    attempts=$((attempts - 1))
    sleep 2
  done

  echo "machine ${name} never reached ${want_state}" >&2
  exit 1
}

guest_ssh_host() {
  local name="$1"
  curl -fsS "$(api_url "/v1/machines/${name}?owner_email=${SMOKE_EMAIL}")" | jq -r '.runtime.ssh_host // empty'
}

guest_ssh_port() {
  local name="$1"
  curl -fsS "$(api_url "/v1/machines/${name}?owner_email=${SMOKE_EMAIL}")" | jq -r '.runtime.ssh_port // 0'
}

run_guest_shell() {
  local name="$1"
  local host
  local port
  host="$(guest_ssh_host "${name}")"
  port="$(guest_ssh_port "${name}")"
  if [[ -z "${host}" || "${port}" == "0" ]]; then
    echo "missing guest SSH target for ${name}" >&2
    exit 1
  fi

  "${FASCINATE_SSH_CLIENT_BINARY}" \
    -tt \
    -i "${FASCINATE_GUEST_SSH_KEY_PATH}" \
    -o BatchMode=yes \
    -o StrictHostKeyChecking=no \
    -o UserKnownHostsFile=/dev/null \
    -p "${port}" \
    "${FASCINATE_GUEST_SSH_USER}@${host}"
}

create_machine() {
  local name="$1"

  echo "creating ${name}"
  curl -fsS \
    -X POST \
    -H 'Content-Type: application/json' \
    -d "{\"name\":\"${name}\",\"owner_email\":\"${SMOKE_EMAIL}\"}" \
    "$(api_url '/v1/machines')" >/dev/null

  wait_for_machine_state "${name}" "RUNNING"
}

pause_for_confirmation() {
  local prompt="$1"
  printf '\n%s\n' "${prompt}"
  read -r -p "Press enter to continue..."
}

main() {
  require_root

  for command_name in curl jq systemctl; do
    require_command "${command_name}"
  done

  if [[ ! -f "${ENV_FILE}" ]]; then
    echo "missing env file: ${ENV_FILE}" >&2
    exit 1
  fi

  set -a
  # shellcheck disable=SC1090
  source "${ENV_FILE}"
  set +a

  require_command "${FASCINATE_SSH_CLIENT_BINARY}"

  trap cleanup EXIT
  cleanup

  create_machine "${SMOKE_FIRST_NAME}"
  pause_for_confirmation "Log into Claude inside ${SMOKE_FIRST_NAME}. The next command opens a direct shell to the guest."
  run_guest_shell "${SMOKE_FIRST_NAME}"

  echo "waiting for the frontdoor sync to persist the Claude session state"
  sleep 10

  create_machine "${SMOKE_SECOND_NAME}"
  pause_for_confirmation "Verify Claude is already logged in inside ${SMOKE_SECOND_NAME}. Exit the guest shell once verified."
  run_guest_shell "${SMOKE_SECOND_NAME}"

  pause_for_confirmation "Log out of Claude inside ${SMOKE_SECOND_NAME}. Exit the guest shell after the logout completes."
  run_guest_shell "${SMOKE_SECOND_NAME}"

  echo "waiting for the logout to sync back to host storage"
  sleep 10

  create_machine "${SMOKE_THIRD_NAME}"
  pause_for_confirmation "Verify Claude now requires login again inside ${SMOKE_THIRD_NAME}. Exit the guest shell once verified."
  run_guest_shell "${SMOKE_THIRD_NAME}"

  echo "tool-auth smoke flow completed"
  trap - EXIT
  cleanup
}

main "$@"
