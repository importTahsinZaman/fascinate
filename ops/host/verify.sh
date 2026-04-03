#!/usr/bin/env bash
set -euo pipefail

VM_GUEST_CIDR="${FASCINATE_VM_GUEST_CIDR:-10.42.0.0/24}"
VM_NAMESPACE_CIDR="${FASCINATE_VM_NAMESPACE_CIDR:-100.96.0.0/16}"
RELEASE_STATE_PATH="${FASCINATE_RELEASE_MANIFEST_PATH:-/opt/fascinate/release-manifest.json}"

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

for command_name in curl git ip jq sqlite3 cloud-hypervisor qemu-img cloud-localds caddy ufw; do
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

echo "local build toolchains:"
if command -v go >/dev/null 2>&1; then
  echo "go $(go version | awk '{print $3}')"
else
  echo "go not installed (expected on artifact-consumer hosts)"
fi
echo

echo "installed release:"
if [[ -f "${RELEASE_STATE_PATH}" ]]; then
  jq . "${RELEASE_STATE_PATH}"
else
  echo "release manifest not present yet"
fi
echo

echo "namespace network:"
echo "  guest subnet: ${VM_GUEST_CIDR}"
echo "  uplink subnet: ${VM_NAMESPACE_CIDR}"
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

if iptables -t nat -C POSTROUTING -s "${VM_NAMESPACE_CIDR}" -o "${uplink}" -j MASQUERADE >/dev/null 2>&1; then
  echo "masquerade present for ${VM_NAMESPACE_CIDR} -> ${uplink}"
else
  echo "masquerade rule for ${VM_NAMESPACE_CIDR} -> ${uplink} not present yet (expected before the first VM boots)"
fi
echo

echo "control plane:"
if curl -fsS http://127.0.0.1:8080/healthz >/dev/null 2>&1; then
  curl -fsS http://127.0.0.1:8080/healthz
else
  echo "fascinate control plane is not running yet"
fi
