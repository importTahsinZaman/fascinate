#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
HOSTNAME_VALUE="${FASCINATE_HOSTNAME:-fascinate-01}"
VM_BRIDGE_CIDR="${FASCINATE_VM_BRIDGE_CIDR:-10.42.0.1/24}"
VM_GUEST_CIDR="${FASCINATE_VM_GUEST_CIDR:-10.42.0.0/24}"
VM_NAMESPACE_CIDR="${FASCINATE_VM_NAMESPACE_CIDR:-100.96.0.0/16}"
INSTALL_GO="${FASCINATE_INSTALL_GO:-1}"
HOST_ADMIN_SSH_PORT="${FASCINATE_HOST_ADMIN_SSH_PORT:-}"
CLOUD_HYPERVISOR_VERSION="${FASCINATE_CLOUD_HYPERVISOR_VERSION:-v51.0}"
CLOUD_HYPERVISOR_URL="${FASCINATE_CLOUD_HYPERVISOR_URL:-https://github.com/cloud-hypervisor/cloud-hypervisor/releases/download/${CLOUD_HYPERVISOR_VERSION}/cloud-hypervisor-static}"
CLOUDHV_FIRMWARE_VERSION="${FASCINATE_CLOUDHV_FIRMWARE_VERSION:-ch-a54f262b09}"
CLOUDHV_FIRMWARE_URL="${FASCINATE_CLOUDHV_FIRMWARE_URL:-https://github.com/cloud-hypervisor/edk2/releases/download/${CLOUDHV_FIRMWARE_VERSION}/CLOUDHV.fd}"

require_root() {
  if [[ "${EUID}" -ne 0 ]]; then
    echo "run as root" >&2
    exit 1
  fi
}

install_packages() {
  export DEBIAN_FRONTEND=noninteractive

  local packages=(
    ca-certificates
    cloud-image-utils
    caddy
    curl
    fail2ban
    git
    gnupg
    iproute2
    iptables
    jq
    libguestfs-tools
    lsb-release
    openssh-client
    ovmf
    qemu-utils
    sqlite3
    ufw
    unzip
    wget
  )

  if [[ "${INSTALL_GO}" == "1" ]]; then
    packages+=(golang-go build-essential)
  fi

  apt-get update
  apt-get install -y "${packages[@]}"
}

install_cloud_hypervisor() {
  if command -v cloud-hypervisor >/dev/null 2>&1; then
    return 0
  fi

  curl -fsSL "${CLOUD_HYPERVISOR_URL}" -o /usr/local/bin/cloud-hypervisor
  chmod 0755 /usr/local/bin/cloud-hypervisor
}

install_cloudhv_firmware() {
  if [[ -f /usr/local/share/cloud-hypervisor/CLOUDHV.fd ]]; then
    return 0
  fi

  mkdir -p /usr/local/share/cloud-hypervisor
  curl -fsSL "${CLOUDHV_FIRMWARE_URL}" -o /usr/local/share/cloud-hypervisor/CLOUDHV.fd
  chmod 0644 /usr/local/share/cloud-hypervisor/CLOUDHV.fd
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

configure_kernel_networking() {
  mkdir -p /etc/sysctl.d
  cat >/etc/sysctl.d/60-fascinate-vm-network.conf <<EOF
net.ipv4.ip_forward=1
EOF

  sysctl --system >/dev/null
}

ensure_services() {
  systemctl enable --now fail2ban
  systemctl enable --now caddy
}

configure_admin_ssh() {
  if [[ -z "${HOST_ADMIN_SSH_PORT}" || "${HOST_ADMIN_SSH_PORT}" == "22" ]]; then
    return 0
  fi

  FASCINATE_HOST_ADMIN_SSH_PORT="${HOST_ADMIN_SSH_PORT}" bash "${SCRIPT_DIR}/configure-admin-ssh.sh"
}

print_summary() {
  echo "fascinate host bootstrap complete"
  echo
  echo "hostname: $(hostname)"
  echo "cloud-hypervisor version: $(cloud-hypervisor --version 2>/dev/null | tr '\n' ' ' | sed 's/[[:space:]]\+/ /g')"
  echo
  echo "guest network:"
  echo "  guest subnet: ${VM_GUEST_CIDR}"
  echo "  guest gateway: ${VM_BRIDGE_CIDR}"
  echo "  namespace uplinks: ${VM_NAMESPACE_CIDR}"
  echo
  echo "firewall:"
  ufw status verbose
}

require_root
install_packages
install_cloud_hypervisor
install_cloudhv_firmware
configure_hostname
configure_firewall
configure_kernel_networking
ensure_services
configure_admin_ssh
print_summary
