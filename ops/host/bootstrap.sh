#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
HOSTNAME_VALUE="${FASCINATE_HOSTNAME:-fascinate-01}"
VM_BRIDGE_NAME="${FASCINATE_VM_BRIDGE_NAME:-fascbr0}"
VM_BRIDGE_CIDR="${FASCINATE_VM_BRIDGE_CIDR:-10.42.0.1/24}"
VM_GUEST_CIDR="${FASCINATE_VM_GUEST_CIDR:-10.42.0.0/24}"
INSTALL_GO="${FASCINATE_INSTALL_GO:-1}"
HOST_ADMIN_SSH_PORT="${FASCINATE_HOST_ADMIN_SSH_PORT:-}"
CLOUD_HYPERVISOR_VERSION="${FASCINATE_CLOUD_HYPERVISOR_VERSION:-v51.0}"
CLOUD_HYPERVISOR_URL="${FASCINATE_CLOUD_HYPERVISOR_URL:-https://github.com/cloud-hypervisor/cloud-hypervisor/releases/download/${CLOUD_HYPERVISOR_VERSION}/cloud-hypervisor-static}"

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

configure_bridge() {
  cat >/etc/netplan/60-fascinate-vm-bridge.yaml <<EOF
network:
  version: 2
  renderer: networkd
  bridges:
    ${VM_BRIDGE_NAME}:
      dhcp4: false
      dhcp6: false
      addresses:
        - ${VM_BRIDGE_CIDR}
      parameters:
        stp: false
        forward-delay: 0
EOF
  chmod 0600 /etc/netplan/60-fascinate-vm-bridge.yaml

  netplan generate
  netplan apply
}

install_vm_network_service() {
  cat >/usr/local/sbin/fascinate-vm-network-up <<EOF
#!/usr/bin/env bash
set -euo pipefail

BRIDGE_NAME=${VM_BRIDGE_NAME@Q}
GUEST_CIDR=${VM_GUEST_CIDR@Q}

uplink="\$(ip route get 1.1.1.1 2>/dev/null | awk '{for (i = 1; i <= NF; i++) if (\$i == "dev") {print \$(i + 1); exit}}')"
if [[ -z "\${uplink}" ]]; then
  echo "could not determine uplink interface" >&2
  exit 1
fi

sysctl -w net.ipv4.ip_forward=1 >/dev/null

if command -v ufw >/dev/null 2>&1; then
  ufw allow in on "\${BRIDGE_NAME}" >/dev/null 2>&1 || true
  ufw route allow in on "\${BRIDGE_NAME}" out on "\${uplink}" >/dev/null 2>&1 || true
fi

iptables -t nat -C POSTROUTING -s "\${GUEST_CIDR}" -o "\${uplink}" -j MASQUERADE >/dev/null 2>&1 || \
  iptables -t nat -A POSTROUTING -s "\${GUEST_CIDR}" -o "\${uplink}" -j MASQUERADE
EOF
  chmod 0755 /usr/local/sbin/fascinate-vm-network-up

  cat >/etc/systemd/system/fascinate-vm-network.service <<EOF
[Unit]
Description=Configure Fascinate VM bridge NAT
After=network-online.target ufw.service
Wants=network-online.target

[Service]
Type=oneshot
ExecStart=/usr/local/sbin/fascinate-vm-network-up
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
EOF

  mkdir -p /etc/sysctl.d
  cat >/etc/sysctl.d/60-fascinate-vm-network.conf <<EOF
net.ipv4.ip_forward=1
EOF

  sysctl --system >/dev/null
  systemctl daemon-reload
  systemctl enable --now fascinate-vm-network.service
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
  echo "bridge:"
  ip addr show "${VM_BRIDGE_NAME}"
  echo
  echo "firewall:"
  ufw status verbose
}

require_root
install_packages
install_cloud_hypervisor
configure_hostname
configure_firewall
configure_bridge
install_vm_network_service
ensure_services
configure_admin_ssh
print_summary
