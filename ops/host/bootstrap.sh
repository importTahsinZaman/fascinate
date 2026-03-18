#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
HOSTNAME_VALUE="${FASCINATE_HOSTNAME:-fascinate-01}"
INCUS_POOL_NAME="${FASCINATE_INCUS_POOL_NAME:-machines}"
INCUS_POOL_SIZE_GIB="${FASCINATE_INCUS_POOL_SIZE_GIB:-180}"
INSTALL_GO="${FASCINATE_INSTALL_GO:-1}"
HOST_ADMIN_SSH_PORT="${FASCINATE_HOST_ADMIN_SSH_PORT:-}"

require_root() {
  if [[ "${EUID}" -ne 0 ]]; then
    echo "run as root" >&2
    exit 1
  fi
}

ensure_zabbly_repo() {
  mkdir -p /etc/apt/keyrings

  if [[ ! -f /etc/apt/keyrings/zabbly.asc ]]; then
    curl -fsSL https://pkgs.zabbly.com/key.asc -o /etc/apt/keyrings/zabbly.asc
  fi

  cat >/etc/apt/sources.list.d/zabbly-incus-stable.sources <<EOF
Enabled: yes
Types: deb
URIs: https://pkgs.zabbly.com/incus/stable
Suites: $(. /etc/os-release && echo "${VERSION_CODENAME}")
Components: main
Architectures: $(dpkg --print-architecture)
Signed-By: /etc/apt/keyrings/zabbly.asc
EOF
}

install_packages() {
  export DEBIAN_FRONTEND=noninteractive

  local packages=(
    ca-certificates
    curl
    git
    jq
    sqlite3
    build-essential
    gnupg
    lsb-release
    ufw
    fail2ban
    caddy
    incus
  )

  if [[ "${INSTALL_GO}" == "1" ]]; then
    packages+=(golang-go)
  fi

  apt-get update
  apt-get install -y "${packages[@]}"
}

configure_hostname() {
  hostnamectl set-hostname "${HOSTNAME_VALUE}"
  if ! grep -qE "^127\\.0\\.1\\.1[[:space:]]+${HOSTNAME_VALUE}([[:space:]]|$)" /etc/hosts; then
    printf "127.0.1.1 %s\n" "${HOSTNAME_VALUE}" >>/etc/hosts
  fi
}

configure_firewall() {
  ufw allow 22/tcp
  ufw allow 80/tcp
  ufw allow 443/tcp
  ufw --force enable
}

allow_incus_bridge_firewall() {
  if ip link show incusbr0 >/dev/null 2>&1; then
    ufw allow in on incusbr0 >/dev/null
  fi
}

ensure_services() {
  systemctl enable --now fail2ban
  systemctl enable --now caddy
}

configure_admin_ssh() {
  if [[ -z "${HOST_ADMIN_SSH_PORT}" || "${HOST_ADMIN_SSH_PORT}" == "22" ]]; then
    return 0
  fi

  FASCINATE_HOST_ADMIN_SSH_PORT="${HOST_ADMIN_SSH_PORT}" "${SCRIPT_DIR}/configure-admin-ssh.sh"
}

ensure_incus_initialized() {
  if ! incus info >/dev/null 2>&1; then
    incus admin init --minimal
  fi
}

ensure_incus_network() {
  if ! incus network show incusbr0 >/dev/null 2>&1; then
    incus network create incusbr0 ipv4.address=auto ipv4.nat=true ipv6.address=none
  fi

  if incus profile device get default eth0 network >/dev/null 2>&1; then
    incus profile device set default eth0 network incusbr0
  else
    incus profile device add default eth0 nic name=eth0 network=incusbr0
  fi
}

ensure_incus_pool() {
  if ! incus storage show "${INCUS_POOL_NAME}" >/dev/null 2>&1; then
    incus storage create "${INCUS_POOL_NAME}" btrfs "size=${INCUS_POOL_SIZE_GIB}GiB"
  fi

  if incus profile device get default root pool >/dev/null 2>&1; then
    incus profile device set default root pool "${INCUS_POOL_NAME}"
  else
    incus profile device add default root disk path=/ pool="${INCUS_POOL_NAME}"
  fi
}

print_summary() {
  echo "fascinate host bootstrap complete"
  echo
  echo "hostname: $(hostname)"
  echo "incus version: $(incus version | tr '\n' ' ' | sed 's/[[:space:]]\+/ /g')"
  echo
  incus storage list
  echo
  incus network list
}

require_root
ensure_zabbly_repo
install_packages
configure_hostname
configure_firewall
ensure_services
configure_admin_ssh
ensure_incus_initialized
ensure_incus_network
allow_incus_bridge_firewall
ensure_incus_pool
print_summary
