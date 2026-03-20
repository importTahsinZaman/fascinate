#!/usr/bin/env bash
set -euo pipefail

ENV_FILE="${FASCINATE_ENV_FILE:-/etc/fascinate/fascinate.env}"
SMOKE_EMAIL="${FASCINATE_SMOKE_EMAIL:-smoke@example.com}"
SMOKE_NAME="${FASCINATE_SMOKE_NAME:-smoke-$(date +%s)}"

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

machine_url() {
  printf '%s.%s' "${SMOKE_NAME}" "${FASCINATE_BASE_DOMAIN}"
}

delete_machine() {
  curl -fsS -X DELETE "$(api_url "/v1/machines/${SMOKE_NAME}?owner_email=${SMOKE_EMAIL}")" >/dev/null 2>&1 || true
}

guest_ssh_host() {
  curl -fsS "$(api_url "/v1/machines/${SMOKE_NAME}?owner_email=${SMOKE_EMAIL}")" | jq -r '.runtime.ssh_host // empty'
}

guest_ssh_port() {
  curl -fsS "$(api_url "/v1/machines/${SMOKE_NAME}?owner_email=${SMOKE_EMAIL}")" | jq -r '.runtime.ssh_port // 0'
}

machine_state() {
  curl -fsS "$(api_url "/v1/machines/${SMOKE_NAME}?owner_email=${SMOKE_EMAIL}")" | jq -r '.state // empty'
}

run_guest_command() {
  local guest_host="$1"
  local guest_port="$2"
  shift
  shift
  "${FASCINATE_SSH_CLIENT_BINARY}" \
    -i "${FASCINATE_GUEST_SSH_KEY_PATH}" \
    -o BatchMode=yes \
    -o StrictHostKeyChecking=no \
    -o UserKnownHostsFile=/dev/null \
    -p "${guest_port}" \
    "${FASCINATE_GUEST_SSH_USER}@${guest_host}" \
    "$@"
}

wait_for_guest() {
  local guest_host="$1"
  local guest_port="$2"
  local attempts=60

  while (( attempts > 0 )); do
    if run_guest_command "${guest_host}" "${guest_port}" "claude --version >/dev/null 2>&1 && codex --version >/dev/null 2>&1 && node --version >/dev/null 2>&1 && go version >/dev/null 2>&1 && docker --version >/dev/null 2>&1"; then
      return 0
    fi
    attempts=$((attempts - 1))
    sleep 2
  done

  echo "guest never became ready" >&2
  exit 1
}

wait_for_machine_ready() {
  local attempts=120

  while (( attempts > 0 )); do
    local state
    state="$(machine_state)"
    if [[ "${state}" == "RUNNING" ]]; then
      local host
      local port
      host="$(guest_ssh_host)"
      port="$(guest_ssh_port)"
      if [[ -n "${host}" && "${port}" != "0" ]]; then
        printf '%s %s\n' "${host}" "${port}"
        return 0
      fi
    fi
    attempts=$((attempts - 1))
    sleep 2
  done

  echo "machine never became ready" >&2
  exit 1
}

wait_for_route() {
  local host_header
  host_header="$(machine_url)"
  local attempts=30

  while (( attempts > 0 )); do
    if curl -kfsS --resolve "${host_header}:443:127.0.0.1" "https://${host_header}/" >/dev/null 2>&1; then
      return 0
    fi
    attempts=$((attempts - 1))
    sleep 2
  done

  echo "machine route never became reachable" >&2
  exit 1
}

wait_for_health() {
  local attempts=30

  while (( attempts > 0 )); do
    if curl -fsS "$(api_url '/healthz')" >/dev/null 2>&1; then
      return 0
    fi
    attempts=$((attempts - 1))
    sleep 2
  done

  echo "control plane never became healthy" >&2
  exit 1
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

  if [[ -z "${FASCINATE_BASE_DOMAIN:-}" ]]; then
    echo "FASCINATE_BASE_DOMAIN must be set for host smoke runs" >&2
    exit 1
  fi

  require_command "${FASCINATE_SSH_CLIENT_BINARY}"

  trap delete_machine EXIT
  delete_machine

  echo "creating ${SMOKE_NAME}"
  curl -fsS \
    -X POST \
    -H 'Content-Type: application/json' \
    -d "{\"name\":\"${SMOKE_NAME}\",\"owner_email\":\"${SMOKE_EMAIL}\"}" \
    "$(api_url '/v1/machines')" >/dev/null

  local ssh_host
  local ssh_port
  read -r ssh_host ssh_port <<<"$(wait_for_machine_ready)"

  echo "waiting for guest toolchain on ${ssh_host}:${ssh_port}"
  wait_for_guest "${ssh_host}" "${ssh_port}"

  echo "starting smoke app on port 3000"
  run_guest_command "${ssh_host}" "${ssh_port}" "python3 -c \"import subprocess; log=open('/tmp/fascinate-smoke.log','ab', buffering=0); subprocess.Popen(['python3','-m','http.server','3000','--bind','0.0.0.0'], stdin=subprocess.DEVNULL, stdout=log, stderr=subprocess.STDOUT, start_new_session=True); print('started')\""

  echo "waiting for routed app response"
  wait_for_route

  echo "restarting fascinate"
  systemctl restart fascinate
  wait_for_health
  wait_for_route

  echo "deleting ${SMOKE_NAME}"
  delete_machine
  trap - EXIT

  if curl -fsS "$(api_url "/v1/machines/${SMOKE_NAME}?owner_email=${SMOKE_EMAIL}")" >/dev/null 2>&1; then
    echo "machine still present after delete" >&2
    exit 1
  fi

  echo "host smoke passed"
}

main "$@"
