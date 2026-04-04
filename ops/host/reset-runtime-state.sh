#!/usr/bin/env bash
set -euo pipefail

ENV_FILE="${FASCINATE_ENV_FILE:-/etc/fascinate/fascinate.env}"
SERVICE_NAME="${FASCINATE_SERVICE_NAME:-fascinate}"
CONFIRM_TOKEN="delete-runtime-state"

usage() {
  cat <<EOF
usage:
  sudo ./ops/host/reset-runtime-state.sh --force

This deletes all persisted machine, snapshot, host, and event state from the local Fascinate control plane
and removes all runtime machine/snapshot directories. A backup copy of the sqlite database is written first.
EOF
}

require_command() {
  local name="$1"
  if ! command -v "${name}" >/dev/null 2>&1; then
    echo "missing required command: ${name}" >&2
    exit 1
  fi
}

main() {
  local force="${1:-}"
  if [[ "${force}" != "--force" ]]; then
    usage
    exit 1
  fi

  require_command python3
  require_command systemctl

  if [[ ! -f "${ENV_FILE}" ]]; then
    echo "missing env file: ${ENV_FILE}" >&2
    exit 1
  fi

  set -a
  # shellcheck disable=SC1090
  source "${ENV_FILE}"
  set +a

  : "${FASCINATE_DB_PATH:?FASCINATE_DB_PATH must be set}"
  : "${FASCINATE_RUNTIME_STATE_DIR:?FASCINATE_RUNTIME_STATE_DIR must be set}"
  : "${FASCINATE_RUNTIME_SNAPSHOT_DIR:?FASCINATE_RUNTIME_SNAPSHOT_DIR must be set}"

  if [[ "${FASCINATE_RESET_CONFIRM:-}" != "${CONFIRM_TOKEN}" ]]; then
    echo "refusing destructive reset without FASCINATE_RESET_CONFIRM=${CONFIRM_TOKEN}" >&2
    exit 1
  fi

  local backup_path="${FASCINATE_DB_PATH}.bak.$(date -u +%Y%m%dT%H%M%SZ)"

  systemctl stop "${SERVICE_NAME}"

  if [[ -f "${FASCINATE_DB_PATH}" ]]; then
    cp "${FASCINATE_DB_PATH}" "${backup_path}"
  fi

  rm -rf "${FASCINATE_RUNTIME_STATE_DIR:?}/"*
  rm -rf "${FASCINATE_RUNTIME_SNAPSHOT_DIR:?}/"*
  mkdir -p "${FASCINATE_RUNTIME_STATE_DIR}" "${FASCINATE_RUNTIME_SNAPSHOT_DIR}"

  DB_PATH="${FASCINATE_DB_PATH}" python3 <<'PY'
import os
import sqlite3

db_path = os.environ["DB_PATH"]
conn = sqlite3.connect(db_path)
try:
    conn.execute("PRAGMA foreign_keys = ON")
    conn.execute("DELETE FROM events")
    conn.execute("DELETE FROM snapshots")
    conn.execute("DELETE FROM machines")
    conn.execute("DELETE FROM hosts")
    conn.commit()
    conn.execute("VACUUM")
finally:
    conn.close()
PY

  systemctl start "${SERVICE_NAME}"

  echo "reset complete"
  if [[ -f "${backup_path}" ]]; then
    echo "database backup: ${backup_path}"
  fi
}

main "$@"
