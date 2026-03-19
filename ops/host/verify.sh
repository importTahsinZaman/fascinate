#!/usr/bin/env bash
set -euo pipefail

VM_BRIDGE_NAME="${FASCINATE_VM_BRIDGE_NAME:-fascbr0}"
VM_GUEST_CIDR="${FASCINATE_VM_GUEST_CIDR:-10.42.0.0/24}"

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

require_root

for command_name in curl git jq sqlite3 cloud-hypervisor qemu-img cloud-localds caddy ufw; do
  require_command "${command_name}"
done

echo "host: $(hostname)"
echo "kernel: $(uname -r)"
echo

echo "caddy:"
systemctl is-active caddy
echo

echo "cloud-hypervisor:"
cloud-hypervisor --version
echo

echo "bridge:"
ip addr show "${VM_BRIDGE_NAME}"
echo

echo "firewall:"
ufw status verbose
echo

echo "ip forwarding:"
if [[ "$(sysctl -n net.ipv4.ip_forward)" != "1" ]]; then
  echo "net.ipv4.ip_forward is not enabled" >&2
  exit 1
fi
echo "enabled"
echo

echo "guest egress:"
uplink="$(ip route get 1.1.1.1 2>/dev/null | awk '{for (i = 1; i <= NF; i++) if ($i == "dev") {print $(i + 1); exit}}')"
if [[ -z "${uplink}" ]]; then
  echo "could not determine uplink interface" >&2
  exit 1
fi

if iptables -t nat -C POSTROUTING -s "${VM_GUEST_CIDR}" -o "${uplink}" -j MASQUERADE >/dev/null 2>&1; then
  echo "masquerade present for ${VM_GUEST_CIDR} -> ${uplink}"
else
  echo "missing masquerade rule for ${VM_GUEST_CIDR} -> ${uplink}" >&2
  exit 1
fi
echo

echo "control plane:"
if curl -fsS http://127.0.0.1:8080/healthz >/dev/null 2>&1; then
  curl -fsS http://127.0.0.1:8080/healthz
else
  echo "fascinate control plane is not running yet"
fi
