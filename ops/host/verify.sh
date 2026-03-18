#!/usr/bin/env bash
set -euo pipefail

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

for command_name in curl git jq sqlite3 incus caddy ufw; do
  require_command "${command_name}"
done

echo "host: $(hostname)"
echo "kernel: $(uname -r)"
echo

echo "caddy:"
systemctl is-active caddy
echo

echo "incus:"
incus version
echo

echo "incus storage:"
incus storage list
echo

echo "incus networks:"
incus network list
echo

echo "firewall:"
ufw status verbose
echo

echo "guest egress:"
uplink="$(ip route get 1.1.1.1 2>/dev/null | awk '{for (i = 1; i <= NF; i++) if ($i == \"dev\") {print $(i + 1); exit}}')"
if [[ -n "${uplink}" ]]; then
  if ufw status numbered | grep -F "ALLOW FWD" | grep -F "incusbr0" | grep -F "${uplink}" >/dev/null 2>&1; then
    echo "ufw route allow present for incusbr0 -> ${uplink}"
  else
    echo "missing ufw route allow for incusbr0 -> ${uplink}" >&2
    exit 1
  fi
else
  echo "could not determine uplink interface" >&2
  exit 1
fi
echo

echo "control plane:"
if curl -fsS http://127.0.0.1:8080/healthz >/dev/null 2>&1; then
  curl -fsS http://127.0.0.1:8080/healthz
else
  echo "fascinate control plane is not running yet"
fi
