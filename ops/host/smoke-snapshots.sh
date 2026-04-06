#!/usr/bin/env bash
set -euo pipefail

ENV_FILE="${FASCINATE_ENV_FILE:-/etc/fascinate/fascinate.env}"
SMOKE_EMAIL="${FASCINATE_SMOKE_EMAIL:-smoke@example.com}"
SOURCE_NAME="${FASCINATE_SNAPSHOT_SOURCE_NAME:-snapshot-source-$(date +%s)}"
SNAPSHOT_NAME="${FASCINATE_SNAPSHOT_NAME:-snapshot-$(date +%s)}"
RESTORE_NAME="${FASCINATE_SNAPSHOT_RESTORE_NAME:-snapshot-restore-$(date +%s)}"
FORK_NAME="${FASCINATE_SNAPSHOT_FORK_NAME:-snapshot-fork-$(date +%s)}"
SMOKE_MAX_CPU="${FASCINATE_SMOKE_MAX_CPU:-5}"
SMOKE_MAX_MEMORY_BYTES="${FASCINATE_SMOKE_MAX_MEMORY_BYTES:-10737418240}"
SMOKE_MAX_DISK_BYTES="${FASCINATE_SMOKE_MAX_DISK_BYTES:-85899345920}"
SMOKE_MAX_MACHINE_COUNT="${FASCINATE_SMOKE_MAX_MACHINE_COUNT:-25}"
SMOKE_MAX_SNAPSHOT_COUNT="${FASCINATE_SMOKE_MAX_SNAPSHOT_COUNT:-5}"

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
  local name="$1"
  printf '%s.%s' "${name}" "${FASCINATE_BASE_DOMAIN}"
}

delete_machine() {
  local name="$1"
  curl -fsS -X DELETE "$(api_url "/v1/machines/${name}?owner_email=${SMOKE_EMAIL}")" >/dev/null 2>&1 || true
}

delete_snapshot() {
  local name="$1"
  curl -fsS -X DELETE "$(api_url "/v1/snapshots/${name}?owner_email=${SMOKE_EMAIL}")" >/dev/null 2>&1 || true
}

cleanup() {
  delete_machine "${FORK_NAME}"
  delete_machine "${RESTORE_NAME}"
  delete_machine "${SOURCE_NAME}"
  delete_snapshot "${SNAPSHOT_NAME}"
}

machine_state() {
  local name="$1"
  curl -fsS "$(api_url "/v1/machines/${name}?owner_email=${SMOKE_EMAIL}")" | jq -r '.state // empty'
}

machine_ssh_host() {
  local name="$1"
  curl -fsS "$(api_url "/v1/machines/${name}?owner_email=${SMOKE_EMAIL}")" | jq -r '.runtime.ssh_host // empty'
}

machine_ssh_port() {
  local name="$1"
  curl -fsS "$(api_url "/v1/machines/${name}?owner_email=${SMOKE_EMAIL}")" | jq -r '.runtime.ssh_port // 0'
}

snapshot_state() {
  local name="$1"
  curl -fsS "$(api_url "/v1/snapshots?owner_email=${SMOKE_EMAIL}")" | jq -r --arg name "${name}" '.snapshots[] | select(.name == $name) | .state'
}

run_guest_command() {
  local host="$1"
  local port="$2"
  shift
  shift
  "${FASCINATE_SSH_CLIENT_BINARY}" \
    -i "${FASCINATE_GUEST_SSH_KEY_PATH}" \
    -o BatchMode=yes \
    -o StrictHostKeyChecking=no \
    -o UserKnownHostsFile=/dev/null \
    -p "${port}" \
    "${FASCINATE_GUEST_SSH_USER}@${host}" \
    "$@"
}

wait_for_guest_ready() {
  local host="$1"
  local port="$2"
  local attempts=60

  while (( attempts > 0 )); do
    if run_guest_command "${host}" "${port}" "python3 --version >/dev/null 2>&1 && cat /proc/sys/kernel/random/boot_id >/dev/null 2>&1"; then
      return 0
    fi
    attempts=$((attempts - 1))
    sleep 2
  done

  echo "guest never became ready on ${host}:${port}" >&2
  exit 1
}

wait_for_machine_ready() {
  local name="$1"
  local attempts=180

  while (( attempts > 0 )); do
    local state
    state="$(machine_state "${name}")"
    if [[ "${state}" == "RUNNING" ]]; then
      local host
      local port
      host="$(machine_ssh_host "${name}")"
      port="$(machine_ssh_port "${name}")"
      if [[ -n "${host}" && "${port}" != "0" ]]; then
        printf '%s %s\n' "${host}" "${port}"
        return 0
      fi
    fi
    attempts=$((attempts - 1))
    sleep 2
  done

  echo "machine ${name} never became ready" >&2
  exit 1
}

wait_for_snapshot_ready() {
  local name="$1"
  local attempts=180

  while (( attempts > 0 )); do
    local state
    state="$(snapshot_state "${name}" 2>/dev/null || true)"
    if [[ "${state}" == "READY" ]]; then
      return 0
    fi
    if [[ "${state}" == "FAILED" ]]; then
      echo "snapshot ${name} failed" >&2
      exit 1
    fi
    attempts=$((attempts - 1))
    sleep 2
  done

  echo "snapshot ${name} never became ready" >&2
  exit 1
}

wait_for_route_body() {
  local name="$1"
  local expected="$2"
  local attempts=60

  while (( attempts > 0 )); do
    local host_name
    local body
    host_name="$(machine_url "${name}")"
    if body="$(curl -kfsS --resolve "${host_name}:443:127.0.0.1" "https://${host_name}/")" && grep -q "${expected}" <<<"${body}"; then
      return 0
    fi
    attempts=$((attempts - 1))
    sleep 2
  done

  echo "machine ${name} route never served expected body" >&2
  exit 1
}

machine_runtime_dir() {
  local name="$1"
  printf '/var/lib/fascinate/machines/%s' "${name}"
}

snapshot_runtime_dir() {
  local name="$1"
  local runtime_name
  runtime_name="$(sqlite3 "${FASCINATE_DB_PATH}" "select runtime_name from snapshots where name='${name}' order by created_at desc limit 1;")"
  if [[ -z "${runtime_name}" ]]; then
    runtime_name="${name}"
  fi
  printf '/var/lib/fascinate/snapshots/%s' "${runtime_name}"
}

guest_boot_id() {
  local host="$1"
  local port="$2"
  run_guest_command "${host}" "${port}" "cat /proc/sys/kernel/random/boot_id"
}

guest_http_server_signature() {
  local host="$1"
  local port="$2"
  run_guest_command "${host}" "${port}" "ps -eo pid=,args= --sort=pid | grep '[p]ython3 -m http.server 3000 --bind 0.0.0.0 --directory /home/ubuntu/fascinate-snapshot-smoke' | tr -s ' '"
}

wait_for_machine_deleted() {
  local name="$1"
  local attempts=30

  while (( attempts > 0 )); do
    if ! curl -fsS "$(api_url "/v1/machines/${name}?owner_email=${SMOKE_EMAIL}")" >/dev/null 2>&1; then
      if [[ ! -d "$(machine_runtime_dir "${name}")" ]]; then
        return 0
      fi
    fi
    attempts=$((attempts - 1))
    sleep 2
  done

  echo "machine ${name} was not deleted cleanly" >&2
  exit 1
}

wait_for_snapshot_deleted() {
  local name="$1"
  local attempts=30
  local artifact_dir
  artifact_dir="$(snapshot_runtime_dir "${name}")"

  while (( attempts > 0 )); do
    local state
    state="$(snapshot_state "${name}" 2>/dev/null || true)"
    if [[ -z "${state}" && ! -d "${artifact_dir}" ]]; then
      return 0
    fi
    attempts=$((attempts - 1))
    sleep 2
  done

  echo "snapshot ${name} was not deleted cleanly" >&2
  exit 1
}

create_machine() {
  local name="$1"
  local snapshot_name="${2:-}"

  local body
  if [[ -n "${snapshot_name}" ]]; then
    body="{\"name\":\"${name}\",\"owner_email\":\"${SMOKE_EMAIL}\",\"snapshot_name\":\"${snapshot_name}\"}"
  else
    body="{\"name\":\"${name}\",\"owner_email\":\"${SMOKE_EMAIL}\"}"
  fi

  curl -fsS \
    -X POST \
    -H 'Content-Type: application/json' \
    -d "${body}" \
    "$(api_url '/v1/machines')" >/dev/null
}

ensure_smoke_user_budget() {
  sqlite3 "${FASCINATE_DB_PATH}" <<EOF_SQL
UPDATE users
SET
  max_cpu = '${SMOKE_MAX_CPU}',
  max_memory_bytes = ${SMOKE_MAX_MEMORY_BYTES},
  max_disk_bytes = ${SMOKE_MAX_DISK_BYTES},
  max_machine_count = ${SMOKE_MAX_MACHINE_COUNT},
  max_snapshot_count = ${SMOKE_MAX_SNAPSHOT_COUNT}
WHERE email = '${SMOKE_EMAIL}';
EOF_SQL
}

main() {
  require_root

  for command_name in curl jq grep sqlite3; do
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
    echo "FASCINATE_BASE_DOMAIN must be set for snapshot smoke runs" >&2
    exit 1
  fi

  require_command "${FASCINATE_SSH_CLIENT_BINARY}"

  trap cleanup EXIT
  cleanup

  echo "creating ${SOURCE_NAME}"
  create_machine "${SOURCE_NAME}"
  ensure_smoke_user_budget
  local source_host source_port
  read -r source_host source_port <<<"$(wait_for_machine_ready "${SOURCE_NAME}")"
  wait_for_guest_ready "${source_host}" "${source_port}"

  echo "starting long-lived app on ${SOURCE_NAME}"
  run_guest_command "${source_host}" "${source_port}" "python3 -c \"import pathlib, subprocess; root=pathlib.Path('/home/ubuntu/fascinate-snapshot-smoke'); root.mkdir(parents=True, exist_ok=True); (root / 'index.html').write_text('snapshot-smoke-${SOURCE_NAME}'); log=open('/tmp/fascinate-snapshot-smoke.log','ab', buffering=0); proc=subprocess.Popen(['python3','-m','http.server','3000','--bind','0.0.0.0','--directory',str(root)], stdin=subprocess.DEVNULL, stdout=log, stderr=subprocess.STDOUT, start_new_session=True); pathlib.Path('/tmp/fascinate-snapshot-smoke.pid').write_text(str(proc.pid)); print('started')\""
  wait_for_route_body "${SOURCE_NAME}" "snapshot-smoke-${SOURCE_NAME}"

  echo "saving snapshot ${SNAPSHOT_NAME}"
  curl -fsS \
    -X POST \
    -H 'Content-Type: application/json' \
    -d "{\"machine_name\":\"${SOURCE_NAME}\",\"snapshot_name\":\"${SNAPSHOT_NAME}\",\"owner_email\":\"${SMOKE_EMAIL}\"}" \
    "$(api_url '/v1/snapshots')" >/dev/null
  wait_for_snapshot_ready "${SNAPSHOT_NAME}"

  echo "restoring ${RESTORE_NAME} from ${SNAPSHOT_NAME}"
  create_machine "${RESTORE_NAME}" "${SNAPSHOT_NAME}"
  local restore_host restore_port
  read -r restore_host restore_port <<<"$(wait_for_machine_ready "${RESTORE_NAME}")"
  wait_for_guest_ready "${restore_host}" "${restore_port}"
  wait_for_route_body "${RESTORE_NAME}" "snapshot-smoke-${SOURCE_NAME}"

  echo "forking ${SOURCE_NAME} to ${FORK_NAME}"
  curl -fsS \
    -X POST \
    -H 'Content-Type: application/json' \
    -d "{\"target_name\":\"${FORK_NAME}\",\"owner_email\":\"${SMOKE_EMAIL}\"}" \
    "$(api_url "/v1/machines/${SOURCE_NAME}/fork")" >/dev/null
  local fork_host fork_port
  read -r fork_host fork_port <<<"$(wait_for_machine_ready "${FORK_NAME}")"
  wait_for_guest_ready "${fork_host}" "${fork_port}"
  wait_for_route_body "${FORK_NAME}" "snapshot-smoke-${SOURCE_NAME}"

  echo "verifying restored runtime identity"
  local source_boot_id restore_boot_id fork_boot_id
  source_boot_id="$(guest_boot_id "${source_host}" "${source_port}")"
  restore_boot_id="$(guest_boot_id "${restore_host}" "${restore_port}")"
  fork_boot_id="$(guest_boot_id "${fork_host}" "${fork_port}")"
  if [[ "${source_boot_id}" != "${restore_boot_id}" || "${source_boot_id}" != "${fork_boot_id}" ]]; then
    echo "snapshot boot IDs diverged: source=${source_boot_id} restore=${restore_boot_id} fork=${fork_boot_id}" >&2
    exit 1
  fi

  local source_sig restore_sig fork_sig
  source_sig="$(guest_http_server_signature "${source_host}" "${source_port}")"
  restore_sig="$(guest_http_server_signature "${restore_host}" "${restore_port}")"
  fork_sig="$(guest_http_server_signature "${fork_host}" "${fork_port}")"
  if [[ -z "${source_sig}" || "${source_sig}" != "${restore_sig}" || "${source_sig}" != "${fork_sig}" ]]; then
    echo "snapshot process signatures diverged" >&2
    printf 'source=%s\nrestore=%s\nfork=%s\n' "${source_sig}" "${restore_sig}" "${fork_sig}" >&2
    exit 1
  fi

  echo "verifying fork independence after source mutation"
  run_guest_command "${source_host}" "${source_port}" "printf 'snapshot-mutated-${SOURCE_NAME}\n' >/home/ubuntu/fascinate-snapshot-smoke/index.html"
  wait_for_route_body "${SOURCE_NAME}" "snapshot-mutated-${SOURCE_NAME}"
  wait_for_route_body "${RESTORE_NAME}" "snapshot-smoke-${SOURCE_NAME}"
  wait_for_route_body "${FORK_NAME}" "snapshot-smoke-${SOURCE_NAME}"

  echo "restarting fascinate"
  systemctl restart fascinate
  sleep 2
  curl -fsS "http://${FASCINATE_HTTP_ADDR}/healthz" >/dev/null
  wait_for_route_body "${SOURCE_NAME}" "snapshot-mutated-${SOURCE_NAME}"
  wait_for_route_body "${RESTORE_NAME}" "snapshot-smoke-${SOURCE_NAME}"
  wait_for_route_body "${FORK_NAME}" "snapshot-smoke-${SOURCE_NAME}"

  echo "verifying fork independence after source shutdown"
  run_guest_command "${source_host}" "${source_port}" "kill \$(cat /tmp/fascinate-snapshot-smoke.pid)"
  wait_for_route_body "${SOURCE_NAME}" "No services detected"
  wait_for_route_body "${RESTORE_NAME}" "snapshot-smoke-${SOURCE_NAME}"
  wait_for_route_body "${FORK_NAME}" "snapshot-smoke-${SOURCE_NAME}"

  echo "cleaning up snapshot smoke artifacts"
  cleanup
  trap - EXIT
  wait_for_machine_deleted "${FORK_NAME}"
  wait_for_machine_deleted "${RESTORE_NAME}"
  wait_for_machine_deleted "${SOURCE_NAME}"
  wait_for_snapshot_deleted "${SNAPSHOT_NAME}"

  echo "snapshot smoke passed"
}

main "$@"
