#!/usr/bin/env bash
set -euo pipefail

ENV_FILE="${FASCINATE_ENV_FILE:-/etc/fascinate/fascinate.env}"
BENCH_EMAIL="${FASCINATE_BENCH_EMAIL:-bench@example.com}"
BENCH_PREFIX="${FASCINATE_BENCH_PREFIX:-bench}"

usage() {
  cat <<'EOF'
Run a repeatable Fascinate benchmark against a configured host:
  sudo ./ops/host/benchmark.sh

Output:
  JSON with timing metrics for:
  - bare create
  - idle snapshot
  - idle restore
  - loaded snapshot
  - loaded restore
  - clone
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

delete_machine() {
  local name="$1"
  curl -fsS -X DELETE "$(api_url "/v1/machines/${name}?owner_email=${BENCH_EMAIL}")" >/dev/null 2>&1 || true
}

delete_snapshot() {
  local name="$1"
  curl -fsS -X DELETE "$(api_url "/v1/snapshots/${name}?owner_email=${BENCH_EMAIL}")" >/dev/null 2>&1 || true
}

machine_json() {
  local name="$1"
  curl -fsS "$(api_url "/v1/machines/${name}?owner_email=${BENCH_EMAIL}")"
}

machine_ssh_host() {
  local name="$1"
  machine_json "${name}" | jq -r '.runtime.ssh_host // empty'
}

machine_ssh_port() {
  local name="$1"
  machine_json "${name}" | jq -r '.runtime.ssh_port // 0'
}

machine_state() {
  local name="$1"
  machine_json "${name}" | jq -r '.state // empty'
}

snapshot_state() {
  local name="$1"
  curl -fsS "$(api_url "/v1/snapshots?owner_email=${BENCH_EMAIL}")" | jq -r --arg name "${name}" '.snapshots[]? | select(.name == $name) | .state'
}

wait_for_machine_ready() {
  local name="$1"
  local attempts=300

  while (( attempts > 0 )); do
    local body state ssh_host ssh_port
    body="$(curl -fsS "$(api_url "/v1/machines/${name}?owner_email=${BENCH_EMAIL}")" 2>/dev/null || true)"
    if [[ -n "${body}" ]]; then
      state="$(jq -r '.state // empty' <<<"${body}")"
      ssh_host="$(jq -r '.runtime.ssh_host // empty' <<<"${body}")"
      ssh_port="$(jq -r '.runtime.ssh_port // 0' <<<"${body}")"
      if [[ "${state}" == "RUNNING" && -n "${ssh_host}" && "${ssh_port}" != "0" ]]; then
        printf '%s %s\n' "${ssh_host}" "${ssh_port}"
        return 0
      fi
      if [[ "${state}" == "FAILED" ]]; then
        echo "machine ${name} failed" >&2
        echo "${body}" | jq . >&2 || true
        exit 1
      fi
    fi
    attempts=$((attempts - 1))
    sleep 1
  done

  echo "machine ${name} never became ready" >&2
  exit 1
}

wait_for_snapshot_ready() {
  local name="$1"
  local attempts=300

  while (( attempts > 0 )); do
    local state
    state="$(snapshot_state "${name}" 2>/dev/null || true)"
    if [[ "${state}" == "READY" ]]; then
      return 0
    fi
    if [[ "${state}" == "FAILED" ]]; then
      echo "snapshot ${name} failed" >&2
      curl -fsS "$(api_url "/v1/diagnostics/snapshots/${name}?owner_email=${BENCH_EMAIL}")" | jq . >&2 || true
      exit 1
    fi
    attempts=$((attempts - 1))
    sleep 1
  done

  echo "snapshot ${name} never became ready" >&2
  exit 1
}

wait_for_route_body() {
  local name="$1"
  local expected="$2"
  local attempts=120
  local host_header
  host_header="${name}.${FASCINATE_BASE_DOMAIN}"

  while (( attempts > 0 )); do
    if curl -kfsS --resolve "${host_header}:443:127.0.0.1" "https://${host_header}/" 2>/dev/null | grep -q "${expected}"; then
      return 0
    fi
    attempts=$((attempts - 1))
    sleep 1
  done

  echo "route for ${name} never served expected body: ${expected}" >&2
  exit 1
}

run_guest_script() {
  local name="$1"
  local script_path="$2"
  local host port
  host="$(machine_ssh_host "${name}")"
  port="$(machine_ssh_port "${name}")"

  "${FASCINATE_SSH_CLIENT_BINARY}" \
    -i "${FASCINATE_GUEST_SSH_KEY_PATH}" \
    -o BatchMode=yes \
    -o StrictHostKeyChecking=no \
    -o UserKnownHostsFile=/dev/null \
    -p "${port}" \
    "${FASCINATE_GUEST_SSH_USER}@${host}" \
    bash -s < "${script_path}"
}

run_guest_command() {
  local name="$1"
  shift
  local host port
  host="$(machine_ssh_host "${name}")"
  port="$(machine_ssh_port "${name}")"

  "${FASCINATE_SSH_CLIENT_BINARY}" \
    -i "${FASCINATE_GUEST_SSH_KEY_PATH}" \
    -o BatchMode=yes \
    -o StrictHostKeyChecking=no \
    -o UserKnownHostsFile=/dev/null \
    -p "${port}" \
    "${FASCINATE_GUEST_SSH_USER}@${host}" \
    "$@"
}

cleanup_bench_user() {
  local user_id
  user_id="$(sqlite3 "${FASCINATE_DB_PATH}" "select id from users where email='${BENCH_EMAIL}' limit 1;")"
  if [[ -n "${user_id}" ]]; then
    rm -rf "${FASCINATE_TOOL_AUTH_DIR}/${user_id}"
  fi
  sqlite3 "${FASCINATE_DB_PATH}" "
    PRAGMA foreign_keys = ON;
    DELETE FROM events
      WHERE actor_user_id = '${user_id}'
         OR machine_id IN (SELECT id FROM machines WHERE owner_user_id = '${user_id}')
         OR machine_id IN (SELECT id FROM machines WHERE name LIKE '${BENCH_PREFIX}-%');
    DELETE FROM machine_ports
      WHERE machine_id IN (SELECT id FROM machines WHERE owner_user_id = '${user_id}')
         OR machine_id IN (SELECT id FROM machines WHERE name LIKE '${BENCH_PREFIX}-%');
    DELETE FROM snapshots
      WHERE owner_user_id = '${user_id}'
         OR name LIKE '${BENCH_PREFIX}-%';
    DELETE FROM machines
      WHERE owner_user_id = '${user_id}'
         OR name LIKE '${BENCH_PREFIX}-%';
    DELETE FROM ssh_keys WHERE user_id = '${user_id}';
    DELETE FROM email_codes WHERE email = '${BENCH_EMAIL}';
    DELETE FROM users WHERE email = '${BENCH_EMAIL}';
  " >/dev/null 2>&1 || true
}

cleanup() {
  delete_machine "${CLONE_NAME}"
  delete_machine "${LOADED_RESTORE_NAME}"
  delete_machine "${IDLE_RESTORE_NAME}"
  delete_machine "${SOURCE_NAME}"
  delete_snapshot "${LOADED_SNAPSHOT_NAME}"
  delete_snapshot "${IDLE_SNAPSHOT_NAME}"
  cleanup_bench_user
}

main() {
  require_root
  for command_name in curl jq sqlite3 date mktemp; do
    require_command "${command_name}"
  done

  if [[ "${1:-}" == "--help" || "${1:-}" == "-h" ]]; then
    usage
    exit 0
  fi

  if [[ ! -f "${ENV_FILE}" ]]; then
    echo "missing env file: ${ENV_FILE}" >&2
    exit 1
  fi

  set -a
  # shellcheck disable=SC1090
  source "${ENV_FILE}"
  set +a

  require_command "${FASCINATE_SSH_CLIENT_BINARY}"

  local stamp
  stamp="$(date +%s)"
  SOURCE_NAME="${BENCH_PREFIX}-src-${stamp}"
  IDLE_SNAPSHOT_NAME="${BENCH_PREFIX}-idle-${stamp}"
  IDLE_RESTORE_NAME="${BENCH_PREFIX}-idle-restore-${stamp}"
  LOADED_SNAPSHOT_NAME="${BENCH_PREFIX}-loaded-${stamp}"
  LOADED_RESTORE_NAME="${BENCH_PREFIX}-loaded-restore-${stamp}"
  CLONE_NAME="${BENCH_PREFIX}-clone-${stamp}"

  trap cleanup EXIT
  cleanup

  local start_ms bare_create_ms idle_snapshot_ms idle_restore_ms loaded_snapshot_ms loaded_restore_ms clone_ms
  local loaded_restore_route_ms clone_route_ms
  local idle_disk_bytes idle_memory_bytes loaded_disk_bytes loaded_memory_bytes
  local loaded_rows loaded_restore_checks clone_checks
  local tmpdir setup_script

  tmpdir="$(mktemp -d)"
  setup_script="${tmpdir}/loaded-setup.sh"
  cat > "${setup_script}" <<'EOF'
set -euo pipefail
mkdir -p /home/ubuntu/benchmark-static
printf 'benchmark-loaded-ok\n' >/home/ubuntu/benchmark-static/index.html
pkill -f 'python3 -m http.server 3000 --bind 0.0.0.0 --directory /home/ubuntu/benchmark-static' || true
nohup python3 -m http.server 3000 --bind 0.0.0.0 --directory /home/ubuntu/benchmark-static >/tmp/benchmark-static.log 2>&1 < /dev/null &

sudo systemctl start docker >/dev/null 2>&1 || true
for _ in $(seq 1 60); do
  if sudo docker info >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
if ! sudo docker info >/dev/null 2>&1; then
  echo "docker daemon did not become ready" >&2
  exit 1
fi
sudo docker rm -f benchmark-sidecar >/dev/null 2>&1 || true
sudo docker run -d --name benchmark-sidecar nginx:alpine >/dev/null

sqlite3 /home/ubuntu/benchmark.db <<'SQL'
PRAGMA journal_mode=WAL;
create table if not exists blobs (
  id integer primary key,
  payload blob not null
);
delete from blobs;
with recursive c(x) as (
  select 1
  union all
  select x + 1 from c limit 50000
)
insert into blobs(payload)
select randomblob(2048) from c;
analyze;
SQL
sqlite3 /home/ubuntu/benchmark.db 'select count(*) from blobs;'
EOF

  start_ms="$(date +%s%3N)"
  curl -fsS \
    -X POST \
    -H 'Content-Type: application/json' \
    -d "{\"name\":\"${SOURCE_NAME}\",\"owner_email\":\"${BENCH_EMAIL}\"}" \
    "$(api_url '/v1/machines')" >/dev/null
  wait_for_machine_ready "${SOURCE_NAME}" >/dev/null
  bare_create_ms="$(( $(date +%s%3N) - start_ms ))"

  start_ms="$(date +%s%3N)"
  curl -fsS \
    -X POST \
    -H 'Content-Type: application/json' \
    -d "{\"machine_name\":\"${SOURCE_NAME}\",\"snapshot_name\":\"${IDLE_SNAPSHOT_NAME}\",\"owner_email\":\"${BENCH_EMAIL}\"}" \
    "$(api_url '/v1/snapshots')" >/dev/null
  wait_for_snapshot_ready "${IDLE_SNAPSHOT_NAME}"
  idle_snapshot_ms="$(( $(date +%s%3N) - start_ms ))"
  idle_disk_bytes="$(curl -fsS "$(api_url "/v1/diagnostics/snapshots/${IDLE_SNAPSHOT_NAME}?owner_email=${BENCH_EMAIL}")" | jq -r '.snapshot.disk_size_bytes')"
  idle_memory_bytes="$(curl -fsS "$(api_url "/v1/diagnostics/snapshots/${IDLE_SNAPSHOT_NAME}?owner_email=${BENCH_EMAIL}")" | jq -r '.snapshot.memory_size_bytes')"

  start_ms="$(date +%s%3N)"
  curl -fsS \
    -X POST \
    -H 'Content-Type: application/json' \
    -d "{\"name\":\"${IDLE_RESTORE_NAME}\",\"owner_email\":\"${BENCH_EMAIL}\",\"snapshot_name\":\"${IDLE_SNAPSHOT_NAME}\"}" \
    "$(api_url '/v1/machines')" >/dev/null
  wait_for_machine_ready "${IDLE_RESTORE_NAME}" >/dev/null
  idle_restore_ms="$(( $(date +%s%3N) - start_ms ))"

  loaded_rows="$(run_guest_script "${SOURCE_NAME}" "${setup_script}" | tail -n 1 | tr -d '\r')"
  wait_for_route_body "${SOURCE_NAME}" "benchmark-loaded-ok"

  start_ms="$(date +%s%3N)"
  curl -fsS \
    -X POST \
    -H 'Content-Type: application/json' \
    -d "{\"machine_name\":\"${SOURCE_NAME}\",\"snapshot_name\":\"${LOADED_SNAPSHOT_NAME}\",\"owner_email\":\"${BENCH_EMAIL}\"}" \
    "$(api_url '/v1/snapshots')" >/dev/null
  wait_for_snapshot_ready "${LOADED_SNAPSHOT_NAME}"
  loaded_snapshot_ms="$(( $(date +%s%3N) - start_ms ))"
  loaded_disk_bytes="$(curl -fsS "$(api_url "/v1/diagnostics/snapshots/${LOADED_SNAPSHOT_NAME}?owner_email=${BENCH_EMAIL}")" | jq -r '.snapshot.disk_size_bytes')"
  loaded_memory_bytes="$(curl -fsS "$(api_url "/v1/diagnostics/snapshots/${LOADED_SNAPSHOT_NAME}?owner_email=${BENCH_EMAIL}")" | jq -r '.snapshot.memory_size_bytes')"

  start_ms="$(date +%s%3N)"
  curl -fsS \
    -X POST \
    -H 'Content-Type: application/json' \
    -d "{\"name\":\"${LOADED_RESTORE_NAME}\",\"owner_email\":\"${BENCH_EMAIL}\",\"snapshot_name\":\"${LOADED_SNAPSHOT_NAME}\"}" \
    "$(api_url '/v1/machines')" >/dev/null
  wait_for_machine_ready "${LOADED_RESTORE_NAME}" >/dev/null
  loaded_restore_ms="$(( $(date +%s%3N) - start_ms ))"

  start_ms="$(date +%s%3N)"
  wait_for_route_body "${LOADED_RESTORE_NAME}" "benchmark-loaded-ok"
  loaded_restore_route_ms="$(( $(date +%s%3N) - start_ms ))"
  loaded_restore_checks="$(run_guest_command "${LOADED_RESTORE_NAME}" "sqlite3 /home/ubuntu/benchmark.db 'select count(*) from blobs;' && echo --- && sudo docker ps --format '{{.Names}}'")"

  start_ms="$(date +%s%3N)"
  curl -fsS \
    -X POST \
    -H 'Content-Type: application/json' \
    -d "{\"target_name\":\"${CLONE_NAME}\",\"owner_email\":\"${BENCH_EMAIL}\"}" \
    "$(api_url "/v1/machines/${SOURCE_NAME}/clone")" >/dev/null
  wait_for_machine_ready "${CLONE_NAME}" >/dev/null
  clone_ms="$(( $(date +%s%3N) - start_ms ))"

  start_ms="$(date +%s%3N)"
  wait_for_route_body "${CLONE_NAME}" "benchmark-loaded-ok"
  clone_route_ms="$(( $(date +%s%3N) - start_ms ))"
  clone_checks="$(run_guest_command "${CLONE_NAME}" "sqlite3 /home/ubuntu/benchmark.db 'select count(*) from blobs;' && echo --- && sudo docker ps --format '{{.Names}}'")"

  jq -n \
    --arg benchmark_owner_email "${BENCH_EMAIL}" \
    --arg benchmark_prefix "${BENCH_PREFIX}" \
    --arg source_machine "${SOURCE_NAME}" \
    --arg idle_snapshot "${IDLE_SNAPSHOT_NAME}" \
    --arg idle_restore "${IDLE_RESTORE_NAME}" \
    --arg loaded_snapshot "${LOADED_SNAPSHOT_NAME}" \
    --arg loaded_restore "${LOADED_RESTORE_NAME}" \
    --arg clone_machine "${CLONE_NAME}" \
    --arg workload "public app on port 3000 + local sqlite payload + docker sidecar" \
    --arg loaded_restore_checks "${loaded_restore_checks}" \
    --arg clone_checks "${clone_checks}" \
    --argjson bare_create_ms "${bare_create_ms}" \
    --argjson idle_snapshot_ms "${idle_snapshot_ms}" \
    --argjson idle_snapshot_disk_bytes "${idle_disk_bytes}" \
    --argjson idle_snapshot_memory_bytes "${idle_memory_bytes}" \
    --argjson idle_restore_create_ms "${idle_restore_ms}" \
    --argjson loaded_workload_rows "${loaded_rows}" \
    --argjson loaded_snapshot_ms "${loaded_snapshot_ms}" \
    --argjson loaded_snapshot_disk_bytes "${loaded_disk_bytes}" \
    --argjson loaded_snapshot_memory_bytes "${loaded_memory_bytes}" \
    --argjson loaded_restore_create_ms "${loaded_restore_ms}" \
    --argjson loaded_restore_route_after_running_ms "${loaded_restore_route_ms}" \
    --argjson clone_create_ms "${clone_ms}" \
    --argjson clone_route_after_running_ms "${clone_route_ms}" \
    '{
      benchmark_owner_email: $benchmark_owner_email,
      benchmark_prefix: $benchmark_prefix,
      source_machine: $source_machine,
      workload: $workload,
      timings_ms: {
        bare_create: $bare_create_ms,
        idle_snapshot: $idle_snapshot_ms,
        idle_restore_create: $idle_restore_create_ms,
        loaded_snapshot: $loaded_snapshot_ms,
        loaded_restore_create: $loaded_restore_create_ms,
        loaded_restore_route_after_running: $loaded_restore_route_after_running_ms,
        clone_create: $clone_create_ms,
        clone_route_after_running: $clone_route_after_running_ms
      },
      snapshot_sizes_bytes: {
        idle: {
          disk: $idle_snapshot_disk_bytes,
          memory: $idle_snapshot_memory_bytes
        },
        loaded: {
          disk: $loaded_snapshot_disk_bytes,
          memory: $loaded_snapshot_memory_bytes
        }
      },
      loaded_workload: {
        sqlite_rows: $loaded_workload_rows,
        loaded_restore_checks: $loaded_restore_checks,
        clone_checks: $clone_checks
      },
      artifacts: {
        idle_snapshot: $idle_snapshot,
        idle_restore: $idle_restore,
        loaded_snapshot: $loaded_snapshot,
        loaded_restore: $loaded_restore,
        clone_machine: $clone_machine
      }
    }'

  rm -rf "${tmpdir}"
}

main "$@"
