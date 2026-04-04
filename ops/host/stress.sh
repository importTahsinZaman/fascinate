#!/usr/bin/env bash
set -euo pipefail

ENV_FILE="${FASCINATE_ENV_FILE:-/etc/fascinate/fascinate.env}"
STRESS_EMAIL="${FASCINATE_STRESS_EMAIL:-stress@example.com}"
SOURCE_NAME="${FASCINATE_STRESS_SOURCE_NAME:-stress-source-$(date +%s)}"
SNAPSHOT_NAME="${FASCINATE_STRESS_SNAPSHOT_NAME:-stress-snapshot-$(date +%s)}"
RESTORE_NAME="${FASCINATE_STRESS_RESTORE_NAME:-stress-restore-$(date +%s)}"
FORK_NAME="${FASCINATE_STRESS_FORK_NAME:-stress-fork-$(date +%s)}"
STRESS_MAX_CPU="${FASCINATE_STRESS_MAX_CPU:-4}"
STRESS_MAX_MEMORY_BYTES="${FASCINATE_STRESS_MAX_MEMORY_BYTES:-17179869184}"
STRESS_MAX_DISK_BYTES="${FASCINATE_STRESS_MAX_DISK_BYTES:-107374182400}"
STRESS_MAX_MACHINE_COUNT="${FASCINATE_STRESS_MAX_MACHINE_COUNT:-25}"
STRESS_MAX_SNAPSHOT_COUNT="${FASCINATE_STRESS_MAX_SNAPSHOT_COUNT:-5}"

declare -A MACHINE_NAMESPACE=()
declare -A MACHINE_HOST_VETH=()
declare -A MACHINE_RUNTIME_DIR=()
declare -A MACHINE_APP_PORT=()
declare -A MACHINE_SSH_PORT=()
declare -A SNAPSHOT_ARTIFACT_DIR=()

usage() {
  cat <<'EOF'
Run a full Fascinate stress pass against a configured host:
  sudo ./ops/host/stress.sh
EOF
}

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

machine_state() {
  local name="$1"
  curl -fsS "$(api_url "/v1/machines/${name}?owner_email=${STRESS_EMAIL}")" | jq -r '.state // empty'
}

machine_ssh_host() {
  local name="$1"
  curl -fsS "$(api_url "/v1/machines/${name}?owner_email=${STRESS_EMAIL}")" | jq -r '.runtime.ssh_host // empty'
}

machine_ssh_port() {
  local name="$1"
  curl -fsS "$(api_url "/v1/machines/${name}?owner_email=${STRESS_EMAIL}")" | jq -r '.runtime.ssh_port // 0'
}

snapshot_state() {
  local name="$1"
  curl -fsS "$(api_url "/v1/snapshots?owner_email=${STRESS_EMAIL}")" | jq -r --arg name "${name}" '.snapshots[] | select(.name == $name) | .state'
}

machine_diag() {
  local name="$1"
  curl -fsS "$(api_url "/v1/diagnostics/machines/${name}?owner_email=${STRESS_EMAIL}")"
}

snapshot_diag() {
  local name="$1"
  curl -fsS "$(api_url "/v1/diagnostics/snapshots/${name}?owner_email=${STRESS_EMAIL}")"
}

tool_auth_diag() {
  curl -fsS "$(api_url "/v1/diagnostics/tool-auth?owner_email=${STRESS_EMAIL}")"
}

owner_events() {
  curl -fsS "$(api_url "/v1/diagnostics/events?owner_email=${STRESS_EMAIL}&limit=100")"
}

create_machine() {
  local name="$1"
  local snapshot_name="${2:-}"

  local body
  if [[ -n "${snapshot_name}" ]]; then
    body="{\"name\":\"${name}\",\"owner_email\":\"${STRESS_EMAIL}\",\"snapshot_name\":\"${snapshot_name}\"}"
  else
    body="{\"name\":\"${name}\",\"owner_email\":\"${STRESS_EMAIL}\"}"
  fi

  curl -fsS \
    -X POST \
    -H 'Content-Type: application/json' \
    -d "${body}" \
    "$(api_url '/v1/machines')" >/dev/null
}

ensure_stress_user_budget() {
  sqlite3 "${FASCINATE_DB_PATH}" <<EOF_SQL
UPDATE users
SET
  max_cpu = '${STRESS_MAX_CPU}',
  max_memory_bytes = ${STRESS_MAX_MEMORY_BYTES},
  max_disk_bytes = ${STRESS_MAX_DISK_BYTES},
  max_machine_count = ${STRESS_MAX_MACHINE_COUNT},
  max_snapshot_count = ${STRESS_MAX_SNAPSHOT_COUNT}
WHERE email = '${STRESS_EMAIL}';
EOF_SQL
}

delete_machine() {
  local name="$1"
  curl -fsS -X DELETE "$(api_url "/v1/machines/${name}?owner_email=${STRESS_EMAIL}")" >/dev/null 2>&1 || true
}

delete_snapshot() {
  local name="$1"
  curl -fsS -X DELETE "$(api_url "/v1/snapshots/${name}?owner_email=${STRESS_EMAIL}")" >/dev/null 2>&1 || true
}

run_guest_command() {
  local host="$1"
  local port="$2"
  shift 2
  "${FASCINATE_SSH_CLIENT_BINARY}" \
    -i "${FASCINATE_GUEST_SSH_KEY_PATH}" \
    -o BatchMode=yes \
    -o StrictHostKeyChecking=no \
    -o UserKnownHostsFile=/dev/null \
    -p "${port}" \
    "${FASCINATE_GUEST_SSH_USER}@${host}" \
    "$@"
}

wait_for_guest_toolchain() {
  local host="$1"
  local port="$2"
  local attempts=120

  while (( attempts > 0 )); do
    if run_guest_command "${host}" "${port}" "claude --version >/dev/null 2>&1 && codex --version >/dev/null 2>&1 && gh --version >/dev/null 2>&1 && node --version >/dev/null 2>&1 && go version >/dev/null 2>&1 && docker --version >/dev/null 2>&1 && systemctl is-active --quiet docker"; then
      return 0
    fi
    attempts=$((attempts - 1))
    sleep 2
  done

  echo "guest toolchain never became ready on ${host}:${port}" >&2
  exit 1
}

wait_for_machine_ready() {
  local name="$1"
  local attempts=240

  while (( attempts > 0 )); do
    local state
    state="$(machine_state "${name}" 2>/dev/null || true)"
    if [[ "${state}" == "RUNNING" ]]; then
      local host port
      host="$(machine_ssh_host "${name}")"
      port="$(machine_ssh_port "${name}")"
      if [[ -n "${host}" && "${port}" != "0" ]]; then
        printf '%s %s\n' "${host}" "${port}"
        return 0
      fi
    elif [[ "${state}" == "FAILED" ]]; then
      echo "machine ${name} failed" >&2
      machine_diag "${name}" | jq . >&2 || true
      exit 1
    fi
    attempts=$((attempts - 1))
    sleep 2
  done

  echo "machine ${name} never became ready" >&2
  machine_diag "${name}" | jq . >&2 || true
  exit 1
}

wait_for_snapshot_ready() {
  local name="$1"
  local attempts=240

  while (( attempts > 0 )); do
    local state
    state="$(snapshot_state "${name}" 2>/dev/null || true)"
    if [[ "${state}" == "READY" ]]; then
      return 0
    fi
    if [[ "${state}" == "FAILED" ]]; then
      echo "snapshot ${name} failed" >&2
      snapshot_diag "${name}" | jq . >&2 || true
      owner_events | jq . >&2 || true
      exit 1
    fi
    attempts=$((attempts - 1))
    sleep 2
  done

  echo "snapshot ${name} never became ready" >&2
  snapshot_diag "${name}" | jq . >&2 || true
  exit 1
}

wait_for_route_body() {
  local name="$1"
  local expected="$2"
  local attempts=60

  while (( attempts > 0 )); do
    local host_name body
    host_name="$(machine_url "${name}")"
    if body="$(curl -kfsS --resolve "${host_name}:443:127.0.0.1" "https://${host_name}/" 2>/dev/null)" && grep -q "${expected}" <<<"${body}"; then
      return 0
    fi
    attempts=$((attempts - 1))
    sleep 2
  done

  echo "machine ${name} route never served expected body: ${expected}" >&2
  machine_diag "${name}" | jq . >&2 || true
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

wait_for_machine_deleted() {
  local name="$1"
  local attempts=60

  while (( attempts > 0 )); do
    if ! curl -fsS "$(api_url "/v1/machines/${name}?owner_email=${STRESS_EMAIL}")" >/dev/null 2>&1; then
      local runtime_dir="${MACHINE_RUNTIME_DIR[${name}]:-}"
      local namespace_name="${MACHINE_NAMESPACE[${name}]:-}"
      local host_veth="${MACHINE_HOST_VETH[${name}]:-}"
      local app_port="${MACHINE_APP_PORT[${name}]:-0}"
      local ssh_port="${MACHINE_SSH_PORT[${name}]:-0}"
      if [[ -z "${runtime_dir}" || ! -d "${runtime_dir}" ]]; then
        if [[ -z "${namespace_name}" || -z "$(ip netns list | awk '{print $1}' | grep -x "${namespace_name}" || true)" ]]; then
          if [[ -z "${host_veth}" || ! -e "/sys/class/net/${host_veth}" ]]; then
            if ! nc -z 127.0.0.1 "${app_port}" >/dev/null 2>&1 && ! nc -z 127.0.0.1 "${ssh_port}" >/dev/null 2>&1; then
              return 0
            fi
          fi
        fi
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
  local attempts=60
  local artifact_dir="${SNAPSHOT_ARTIFACT_DIR[${name}]:-}"

  while (( attempts > 0 )); do
    local state
    state="$(snapshot_state "${name}" 2>/dev/null || true)"
    if [[ -z "${state}" && ( -z "${artifact_dir}" || ! -d "${artifact_dir}" ) ]]; then
      return 0
    fi
    attempts=$((attempts - 1))
    sleep 2
  done

  echo "snapshot ${name} was not deleted cleanly" >&2
  exit 1
}

record_machine_artifacts() {
  local name="$1"
  local diag
  diag="$(machine_diag "${name}")"
  MACHINE_NAMESPACE["${name}"]="$(jq -r '.runtime.namespace_name // empty' <<<"${diag}")"
  MACHINE_HOST_VETH["${name}"]="$(jq -r '.runtime.host_veth_name // empty' <<<"${diag}")"
  MACHINE_RUNTIME_DIR["${name}"]="$(dirname "$(jq -r '.runtime.log_path // empty' <<<"${diag}")")"
  MACHINE_APP_PORT["${name}"]="$(jq -r '.runtime.app_forward_port // 0' <<<"${diag}")"
  MACHINE_SSH_PORT["${name}"]="$(jq -r '.runtime.ssh_forward_port // 0' <<<"${diag}")"
}

record_snapshot_artifacts() {
  local name="$1"
  SNAPSHOT_ARTIFACT_DIR["${name}"]="$(snapshot_diag "${name}" | jq -r '.runtime.artifact_dir // empty')"
}

cleanup() {
  delete_machine "${FORK_NAME}"
  delete_machine "${RESTORE_NAME}"
  delete_machine "${SOURCE_NAME}"
  delete_snapshot "${SNAPSHOT_NAME}"
}

debug_dump() {
  echo "--- owner events ---" >&2
  owner_events | jq . >&2 || true
  echo "--- tool auth diagnostics ---" >&2
  tool_auth_diag | jq . >&2 || true
  for name in "${SOURCE_NAME}" "${RESTORE_NAME}" "${FORK_NAME}"; do
    if curl -fsS "$(api_url "/v1/machines/${name}?owner_email=${STRESS_EMAIL}")" >/dev/null 2>&1; then
      echo "--- machine diagnostics: ${name} ---" >&2
      machine_diag "${name}" | jq . >&2 || true
    fi
  done
  if curl -fsS "$(api_url "/v1/snapshots?owner_email=${STRESS_EMAIL}")" >/dev/null 2>&1; then
    echo "--- snapshots ---" >&2
    curl -fsS "$(api_url "/v1/snapshots?owner_email=${STRESS_EMAIL}")" | jq . >&2 || true
    if snapshot_state "${SNAPSHOT_NAME}" >/dev/null 2>&1; then
      echo "--- snapshot diagnostics: ${SNAPSHOT_NAME} ---" >&2
      snapshot_diag "${SNAPSHOT_NAME}" | jq . >&2 || true
    fi
  fi
}

setup_source_workloads() {
  local host="$1"
  local port="$2"
  run_guest_command "${host}" "${port}" "cat <<'EOF' >/tmp/fascinate-stress-setup.sh
set -euo pipefail
mkdir -p /home/ubuntu/fascinate-stress-app /home/ubuntu/fascinate-stress-data/redis
printf 'stress-app-${SOURCE_NAME}\n' >/home/ubuntu/fascinate-stress-app/index.html
python3 -c \"import pathlib, subprocess; root=pathlib.Path('/home/ubuntu/fascinate-stress-app'); log=open('/tmp/fascinate-stress-app.log','ab', buffering=0); proc=subprocess.Popen(['python3','-m','http.server','3000','--bind','0.0.0.0','--directory',str(root)], stdin=subprocess.DEVNULL, stdout=log, stderr=subprocess.STDOUT, start_new_session=True); pathlib.Path('/tmp/fascinate-stress-app.pid').write_text(str(proc.pid)); print('started')\"
if ! command -v redis-server >/dev/null 2>&1; then
  sudo apt-get update
  sudo apt-get install -y redis-server
fi
if pgrep -f 'redis-server .*6379' >/dev/null 2>&1; then
  pkill -f 'redis-server .*6379' || true
fi
redis-server --daemonize yes --bind 127.0.0.1 --port 6379 --dir /home/ubuntu/fascinate-stress-data/redis --dbfilename dump.rdb --pidfile /tmp/fascinate-stress-redis.pid
redis-cli -p 6379 set fascinate:stress redis-${SOURCE_NAME} >/dev/null
docker rm -f fascinate-stress-nginx >/dev/null 2>&1 || true
docker pull -q nginx:alpine >/dev/null
docker run -d --name fascinate-stress-nginx nginx:alpine >/dev/null
docker ps --format '{{.Names}}' | grep -x fascinate-stress-nginx >/dev/null
EOF
bash /tmp/fascinate-stress-setup.sh"
}

app_signature() {
  local host="$1"
  local port="$2"
  run_guest_command "${host}" "${port}" "ps -eo pid=,args= --sort=pid | grep '[p]ython3 -m http.server 3000 --bind 0.0.0.0 --directory /home/ubuntu/fascinate-stress-app' | tr -s ' '"
}

docker_signature() {
  local host="$1"
  local port="$2"
  run_guest_command "${host}" "${port}" "docker inspect -f '{{.Id}} {{.State.Status}}' fascinate-stress-nginx"
}

redis_value() {
  local host="$1"
  local port="$2"
  run_guest_command "${host}" "${port}" "redis-cli -p 6379 get fascinate:stress"
}

boot_id() {
  local host="$1"
  local port="$2"
  run_guest_command "${host}" "${port}" "cat /proc/sys/kernel/random/boot_id"
}

assert_running_workloads() {
  local name="$1"
  local host="$2"
  local port="$3"
  local expected_app="$4"
  local expected_redis="$5"

  wait_for_route_body "${name}" "${expected_app}"
  if [[ "$(redis_value "${host}" "${port}")" != "${expected_redis}" ]]; then
    echo "unexpected redis value in ${name}" >&2
    exit 1
  fi
  if [[ -z "$(docker_signature "${host}" "${port}")" ]]; then
    echo "docker workload missing in ${name}" >&2
    exit 1
  fi
}

main() {
  require_root
  for command_name in curl jq sqlite3 ip nc systemctl; do
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
    echo "FASCINATE_BASE_DOMAIN must be set for stress runs" >&2
    exit 1
  fi
  require_command "${FASCINATE_SSH_CLIENT_BINARY}"

  trap 'debug_dump' ERR
  trap cleanup EXIT
  cleanup

  echo "creating ${SOURCE_NAME}"
  create_machine "${SOURCE_NAME}"
  ensure_stress_user_budget
  local source_host source_port
  read -r source_host source_port <<<"$(wait_for_machine_ready "${SOURCE_NAME}")"
  record_machine_artifacts "${SOURCE_NAME}"
  wait_for_guest_toolchain "${source_host}" "${source_port}"

  echo "setting up source workloads"
  setup_source_workloads "${source_host}" "${source_port}"
  assert_running_workloads "${SOURCE_NAME}" "${source_host}" "${source_port}" "stress-app-${SOURCE_NAME}" "redis-${SOURCE_NAME}"

  echo "restarting fascinate with source workload alive"
  systemctl restart fascinate
  wait_for_health
  wait_for_machine_ready "${SOURCE_NAME}" >/dev/null
  assert_running_workloads "${SOURCE_NAME}" "${source_host}" "${source_port}" "stress-app-${SOURCE_NAME}" "redis-${SOURCE_NAME}"

  echo "saving snapshot ${SNAPSHOT_NAME}"
  curl -fsS \
    -X POST \
    -H 'Content-Type: application/json' \
    -d "{\"machine_name\":\"${SOURCE_NAME}\",\"snapshot_name\":\"${SNAPSHOT_NAME}\",\"owner_email\":\"${STRESS_EMAIL}\"}" \
    "$(api_url '/v1/snapshots')" >/dev/null
  wait_for_snapshot_ready "${SNAPSHOT_NAME}"
  record_snapshot_artifacts "${SNAPSHOT_NAME}"

  echo "creating ${RESTORE_NAME} from snapshot"
  create_machine "${RESTORE_NAME}" "${SNAPSHOT_NAME}"
  local restore_host restore_port
  read -r restore_host restore_port <<<"$(wait_for_machine_ready "${RESTORE_NAME}")"
  record_machine_artifacts "${RESTORE_NAME}"
  assert_running_workloads "${RESTORE_NAME}" "${restore_host}" "${restore_port}" "stress-app-${SOURCE_NAME}" "redis-${SOURCE_NAME}"

  echo "forking ${SOURCE_NAME} to ${FORK_NAME}"
  curl -fsS \
    -X POST \
    -H 'Content-Type: application/json' \
    -d "{\"target_name\":\"${FORK_NAME}\",\"owner_email\":\"${STRESS_EMAIL}\"}" \
    "$(api_url "/v1/machines/${SOURCE_NAME}/fork")" >/dev/null
  local fork_host fork_port
  read -r fork_host fork_port <<<"$(wait_for_machine_ready "${FORK_NAME}")"
  record_machine_artifacts "${FORK_NAME}"
  assert_running_workloads "${FORK_NAME}" "${fork_host}" "${fork_port}" "stress-app-${SOURCE_NAME}" "redis-${SOURCE_NAME}"

  echo "verifying snapshot-backed identity"
  local source_boot restore_boot fork_boot
  source_boot="$(boot_id "${source_host}" "${source_port}")"
  restore_boot="$(boot_id "${restore_host}" "${restore_port}")"
  fork_boot="$(boot_id "${fork_host}" "${fork_port}")"
  if [[ "${source_boot}" != "${restore_boot}" || "${source_boot}" != "${fork_boot}" ]]; then
    echo "boot IDs diverged" >&2
    exit 1
  fi

  local source_app restore_app fork_app
  source_app="$(app_signature "${source_host}" "${source_port}")"
  restore_app="$(app_signature "${restore_host}" "${restore_port}")"
  fork_app="$(app_signature "${fork_host}" "${fork_port}")"
  if [[ -z "${source_app}" || "${source_app}" != "${restore_app}" || "${source_app}" != "${fork_app}" ]]; then
    echo "application process signatures diverged" >&2
    exit 1
  fi

  local source_docker restore_docker fork_docker
  source_docker="$(docker_signature "${source_host}" "${source_port}")"
  restore_docker="$(docker_signature "${restore_host}" "${restore_port}")"
  fork_docker="$(docker_signature "${fork_host}" "${fork_port}")"
  if [[ -z "${source_docker}" || "${source_docker}" != "${restore_docker}" || "${source_docker}" != "${fork_docker}" ]]; then
    echo "docker container signatures diverged" >&2
    exit 1
  fi

  echo "restarting fascinate with source, restore, and fork alive"
  systemctl restart fascinate
  wait_for_health
  assert_running_workloads "${SOURCE_NAME}" "${source_host}" "${source_port}" "stress-app-${SOURCE_NAME}" "redis-${SOURCE_NAME}"
  assert_running_workloads "${RESTORE_NAME}" "${restore_host}" "${restore_port}" "stress-app-${SOURCE_NAME}" "redis-${SOURCE_NAME}"
  assert_running_workloads "${FORK_NAME}" "${fork_host}" "${fork_port}" "stress-app-${SOURCE_NAME}" "redis-${SOURCE_NAME}"

  echo "mutating source app, redis, and docker workloads"
  run_guest_command "${source_host}" "${source_port}" "printf 'stress-app-mutated-${SOURCE_NAME}\n' >/home/ubuntu/fascinate-stress-app/index.html"
  run_guest_command "${source_host}" "${source_port}" "redis-cli -p 6379 set fascinate:stress redis-mutated-${SOURCE_NAME} >/dev/null"
  run_guest_command "${source_host}" "${source_port}" "docker stop fascinate-stress-nginx >/dev/null"
  run_guest_command "${source_host}" "${source_port}" "kill \$(cat /tmp/fascinate-stress-app.pid)"

  wait_for_route_body "${SOURCE_NAME}" "No services detected"
  wait_for_route_body "${RESTORE_NAME}" "stress-app-${SOURCE_NAME}"
  wait_for_route_body "${FORK_NAME}" "stress-app-${SOURCE_NAME}"

  if [[ "$(redis_value "${source_host}" "${source_port}")" != "redis-mutated-${SOURCE_NAME}" ]]; then
    echo "source redis mutation did not apply" >&2
    exit 1
  fi
  if [[ "$(redis_value "${restore_host}" "${restore_port}")" != "redis-${SOURCE_NAME}" ]]; then
    echo "restore redis value changed unexpectedly" >&2
    exit 1
  fi
  if [[ "$(redis_value "${fork_host}" "${fork_port}")" != "redis-${SOURCE_NAME}" ]]; then
    echo "fork redis value changed unexpectedly" >&2
    exit 1
  fi

  if run_guest_command "${source_host}" "${source_port}" "docker ps --format '{{.Names}}' | grep -x fascinate-stress-nginx >/dev/null"; then
    echo "source docker container should be stopped" >&2
    exit 1
  fi
  run_guest_command "${restore_host}" "${restore_port}" "docker ps --format '{{.Names}}' | grep -x fascinate-stress-nginx >/dev/null"
  run_guest_command "${fork_host}" "${fork_port}" "docker ps --format '{{.Names}}' | grep -x fascinate-stress-nginx >/dev/null"

  echo "cleaning up stress artifacts"
  delete_machine "${FORK_NAME}"
  delete_machine "${RESTORE_NAME}"
  delete_machine "${SOURCE_NAME}"
  delete_snapshot "${SNAPSHOT_NAME}"
  wait_for_machine_deleted "${FORK_NAME}"
  wait_for_machine_deleted "${RESTORE_NAME}"
  wait_for_machine_deleted "${SOURCE_NAME}"
  wait_for_snapshot_deleted "${SNAPSHOT_NAME}"

  trap - ERR
  trap - EXIT
  echo "stress smoke passed"
}

main "$@"
