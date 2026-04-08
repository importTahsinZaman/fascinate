#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
ARTIFACT_ROOT="$(cd -- "${SCRIPT_DIR}/../.." && pwd)"

BUILD_SCRIPT="${ARTIFACT_ROOT}/ops/cloudhypervisor/build-base-image.sh"
VALIDATE_SCRIPT="${ARTIFACT_ROOT}/ops/cloudhypervisor/validate-base-image.sh"
PROMOTE_SCRIPT="${ARTIFACT_ROOT}/ops/cloudhypervisor/promote-base-image.sh"
ROLLBACK_SCRIPT="${ARTIFACT_ROOT}/ops/cloudhypervisor/rollback-base-image.sh"
STATUS_SCRIPT="${ARTIFACT_ROOT}/ops/cloudhypervisor/base-image-status.sh"
SMOKE_VERSION="${FASCINATE_IMAGE_SMOKE_VERSION:-smoke-$(date -u +%Y%m%d-%H%M%S)}"
KEEP_PROMOTED="${FASCINATE_IMAGE_SMOKE_KEEP_PROMOTED:-0}"
ORIGINAL_CURRENT=""
PROMOTED=0

cleanup() {
  if [[ "${PROMOTED}" != "1" ]]; then
    return
  fi
  if [[ "${KEEP_PROMOTED}" == "1" ]]; then
    return
  fi
  if [[ -z "${ORIGINAL_CURRENT}" || "${ORIGINAL_CURRENT}" == "<none>" || "${ORIGINAL_CURRENT}" == "${SMOKE_VERSION}" ]]; then
    return
  fi

  echo "rolling back image smoke promotion to ${ORIGINAL_CURRENT}"
  "${ROLLBACK_SCRIPT}" --version "${ORIGINAL_CURRENT}" >/dev/null
}

trap cleanup EXIT

if ! command -v awk >/dev/null 2>&1; then
  echo "missing required command: awk" >&2
  exit 1
fi

if ! command -v date >/dev/null 2>&1; then
  echo "missing required command: date" >&2
  exit 1
fi

echo "capturing current image status"
ORIGINAL_CURRENT="$("${STATUS_SCRIPT}" | awk -F '\t' '$1 == "current" { print $2 }')"

echo "building image candidate ${SMOKE_VERSION}"
"${BUILD_SCRIPT}" --version "${SMOKE_VERSION}"

echo "validating image candidate ${SMOKE_VERSION}"
"${VALIDATE_SCRIPT}" --version "${SMOKE_VERSION}"

echo "promoting image candidate ${SMOKE_VERSION}"
"${PROMOTE_SCRIPT}" --version "${SMOKE_VERSION}"
PROMOTED=1

echo "verifying promoted image status"
CURRENT_AFTER_PROMOTE="$("${STATUS_SCRIPT}" | awk -F '\t' '$1 == "current" { print $2 }')"
if [[ "${CURRENT_AFTER_PROMOTE}" != "${SMOKE_VERSION}" ]]; then
  echo "expected current image ${SMOKE_VERSION}, got ${CURRENT_AFTER_PROMOTE:-<empty>}" >&2
  exit 1
fi

"${STATUS_SCRIPT}"
echo "image smoke passed"
