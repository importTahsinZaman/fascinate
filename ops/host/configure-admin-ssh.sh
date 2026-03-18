#!/usr/bin/env bash
set -euo pipefail

ADMIN_PORT="${FASCINATE_HOST_ADMIN_SSH_PORT:-2220}"
DROPIN_DIR="/etc/systemd/system/ssh.socket.d"
SOCKET_DROPIN="${DROPIN_DIR}/listen.conf"
SSHD_CONFIG_DIR="/etc/ssh/sshd_config.d"
SSHD_CONFIG_PATH="${SSHD_CONFIG_DIR}/10-fascinate-admin-port.conf"

require_root() {
  if [[ "${EUID}" -ne 0 ]]; then
    echo "run as root" >&2
    exit 1
  fi
}

validate_port() {
  if ! [[ "${ADMIN_PORT}" =~ ^[0-9]+$ ]] || (( ADMIN_PORT < 1 || ADMIN_PORT > 65535 )); then
    echo "FASCINATE_HOST_ADMIN_SSH_PORT must be a valid TCP port" >&2
    exit 1
  fi
}

write_sshd_config() {
  mkdir -p "${SSHD_CONFIG_DIR}"
  cat >"${SSHD_CONFIG_PATH}" <<EOF
Port ${ADMIN_PORT}
EOF
}

write_socket_dropin() {
  mkdir -p "${DROPIN_DIR}"
  cat >"${SOCKET_DROPIN}" <<EOF
[Socket]
ListenStream=
ListenStream=0.0.0.0:${ADMIN_PORT}
ListenStream=[::]:${ADMIN_PORT}
EOF
}

open_firewall() {
  if ! command -v ufw >/dev/null 2>&1; then
    return 0
  fi

  ufw allow "${ADMIN_PORT}/tcp" >/dev/null
}

reload_ssh() {
  sshd -t
  systemctl daemon-reload
  systemctl restart ssh.socket
}

print_summary() {
  ss -lntp | grep -E ":${ADMIN_PORT}\\s" || true
  echo "host admin ssh now listens on port ${ADMIN_PORT}"
}

require_root
validate_port
write_sshd_config
write_socket_dropin
open_firewall
reload_ssh
print_summary
