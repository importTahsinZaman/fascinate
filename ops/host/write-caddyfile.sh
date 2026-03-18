#!/usr/bin/env bash
set -euo pipefail

require_root() {
  if [[ "${EUID}" -ne 0 ]]; then
    echo "run as root" >&2
    exit 1
  fi
}

BASE_DOMAIN="${FASCINATE_BASE_DOMAIN:-}"
HTTP_ADDR="${FASCINATE_HTTP_ADDR:-127.0.0.1:8080}"
ACME_EMAIL="${FASCINATE_ACME_EMAIL:-}"
OUTPUT_PATH="${FASCINATE_CADDYFILE_PATH:-/etc/caddy/Caddyfile}"

if [[ -z "${BASE_DOMAIN}" ]]; then
  echo "FASCINATE_BASE_DOMAIN is required" >&2
  exit 1
fi

require_root
mkdir -p "$(dirname "${OUTPUT_PATH}")"

{
  echo "{"
  if [[ -n "${ACME_EMAIL}" ]]; then
    printf "  email %s\n" "${ACME_EMAIL}"
  fi
  echo "}"
  echo
  printf "https://%s {\n" "${BASE_DOMAIN}"
  echo "  @private_api path /v1 /v1/*"
  echo "  respond @private_api 404"
  printf "  reverse_proxy %s\n" "${HTTP_ADDR}"
  echo "}"
  echo
  printf "https://*.%s {\n" "${BASE_DOMAIN}"
  echo "  tls internal"
  printf "  reverse_proxy %s\n" "${HTTP_ADDR}"
  echo "}"
} >"${OUTPUT_PATH}"

caddy validate --config "${OUTPUT_PATH}"
