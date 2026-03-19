#!/usr/bin/env bash
set -euo pipefail

OUTPUT_IMAGE="${FASCINATE_DEFAULT_IMAGE:-/var/lib/fascinate/images/fascinate-base.qcow2}"
CACHE_DIR="${FASCINATE_IMAGE_CACHE_DIR:-$(dirname -- "${OUTPUT_IMAGE}")/cache}"
SOURCE_IMAGE_URL="${FASCINATE_BASE_SOURCE_IMAGE_URL:-https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img}"

require_command() {
  local name="$1"
  if ! command -v "${name}" >/dev/null 2>&1; then
    echo "missing required command: ${name}" >&2
    exit 1
  fi
}

main() {
  require_command curl
  require_command cp

  mkdir -p "$(dirname -- "${OUTPUT_IMAGE}")" "${CACHE_DIR}"

  local source_image="${CACHE_DIR}/$(basename -- "${SOURCE_IMAGE_URL}")"
  local temp_image="${OUTPUT_IMAGE}.tmp"
  trap "rm -f '${temp_image}'" EXIT

  curl -fsSL "${SOURCE_IMAGE_URL}" -o "${source_image}"
  cp "${source_image}" "${temp_image}"
  mv -f "${temp_image}" "${OUTPUT_IMAGE}"

  echo "built base image ${OUTPUT_IMAGE}"
  qemu-img info "${OUTPUT_IMAGE}" | sed -n '1,12p'
}

main "$@"
