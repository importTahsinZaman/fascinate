#!/usr/bin/env bash

release_lib_repo_root() {
  local source_dir
  source_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
  (cd -- "${source_dir}/../.." && pwd)
}

require_command() {
  local name="$1"
  if ! command -v "${name}" >/dev/null 2>&1; then
    echo "missing required command: ${name}" >&2
    exit 1
  fi
}

require_root() {
  if [[ "${EUID}" -ne 0 ]]; then
    echo "run as root" >&2
    exit 1
  fi
}

resolve_pnpm() {
  if command -v pnpm >/dev/null 2>&1; then
    printf 'pnpm'
    return
  fi
  if command -v corepack >/dev/null 2>&1; then
    printf 'corepack pnpm'
    return
  fi
  echo "pnpm or corepack is required" >&2
  exit 1
}

utc_now() {
  date -u +"%Y-%m-%dT%H:%M:%SZ"
}

sha256_file() {
  local path="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "${path}" | awk '{print $1}'
    return
  fi
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "${path}" | awk '{print $1}'
    return
  fi
  echo "missing required command: sha256sum or shasum" >&2
  exit 1
}

file_size_bytes() {
  local path="$1"
  if stat -c %s "${path}" >/dev/null 2>&1; then
    stat -c %s "${path}"
    return
  fi
  if stat -f %z "${path}" >/dev/null 2>&1; then
    stat -f %z "${path}"
    return
  fi
  echo "unable to determine file size for ${path}" >&2
  exit 1
}

git_source_revision() {
  local repo_root="$1"
  if git -C "${repo_root}" rev-parse HEAD >/dev/null 2>&1; then
    git -C "${repo_root}" rev-parse HEAD
    return
  fi
  printf ''
}

git_dirty_flag() {
  local repo_root="$1"
  if ! git -C "${repo_root}" rev-parse HEAD >/dev/null 2>&1; then
    printf 'false'
    return
  fi
  if [[ -n "$(git -C "${repo_root}" status --porcelain --untracked-files=normal 2>/dev/null)" ]]; then
    printf 'true'
    return
  fi
  printf 'false'
}

build_release_id() {
  local artifact_type="$1"
  local target_os="$2"
  local target_arch="$3"
  local source_revision="$4"
  local dirty="$5"
  local revision_short="nogit"
  local dirty_suffix=""

  if [[ -n "${source_revision}" ]]; then
    revision_short="${source_revision:0:12}"
  fi
  if [[ "${dirty}" == "true" ]]; then
    dirty_suffix="-dirty"
  fi

  printf '%s-%s-%s%s-%s-%s' \
    "${artifact_type}" \
    "$(date -u +"%Y%m%dT%H%M%SZ")" \
    "${revision_short}" \
    "${dirty_suffix}" \
    "${target_os}" \
    "${target_arch}"
}

manifest_payload_entries_json() {
  local artifact_root="$1"

  (
    cd "${artifact_root}"
    find . -type f ! -name manifest.json | sed 's#^\./##' | LC_ALL=C sort
  ) | while IFS= read -r relative_path; do
    jq -cn \
      --arg path "${relative_path}" \
      --arg sha256 "$(sha256_file "${artifact_root}/${relative_path}")" \
      --argjson size "$(file_size_bytes "${artifact_root}/${relative_path}")" \
      '{path: $path, sha256: $sha256, size: $size}'
  done | jq -s '.'
}

write_manifest() {
  local artifact_root="$1"
  local artifact_type="$2"
  local release_id="$3"
  local built_at="$4"
  local target_os="$5"
  local target_arch="$6"
  local source_revision="$7"
  local source_dirty="$8"
  local payload_json

  payload_json="$(manifest_payload_entries_json "${artifact_root}")"

  jq -n \
    --arg artifactType "${artifact_type}" \
    --arg releaseID "${release_id}" \
    --arg builtAt "${built_at}" \
    --arg targetOS "${target_os}" \
    --arg targetArch "${target_arch}" \
    --arg sourceRevision "${source_revision}" \
    --argjson sourceDirty "${source_dirty}" \
    --argjson payload "${payload_json}" \
    '{
      schemaVersion: 1,
      artifactType: $artifactType,
      releaseID: $releaseID,
      builtAt: $builtAt,
      sourceRevision: (if $sourceRevision == "" then null else $sourceRevision end),
      sourceDirty: $sourceDirty,
      targetOS: $targetOS,
      targetArch: $targetArch,
      payload: $payload
    }' >"${artifact_root}/manifest.json"
}

copy_tree_contents() {
  local source_dir="$1"
  local target_dir="$2"

  if [[ ! -d "${source_dir}" ]]; then
    return
  fi

  mkdir -p "${target_dir}"
  cp -R "${source_dir}/." "${target_dir}/"
}

write_release_identity() {
  local manifest_path="$1"
  local release_dir="$2"
  local installed_at="$3"
  local output_path="$4"

  jq \
    --arg manifestPath "${manifest_path}" \
    --arg releaseDir "${release_dir}" \
    --arg installedAt "${installed_at}" \
    '{
      artifactType,
      releaseID,
      builtAt,
      sourceRevision,
      sourceDirty,
      targetOS,
      targetArch,
      manifestPath: $manifestPath,
      releaseDir: $releaseDir,
      installedAt: $installedAt
    }' "${manifest_path}" >"${output_path}"
}

update_installed_release_state() {
  local state_path="$1"
  local component="$2"
  local manifest_path="$3"
  local release_dir="$4"
  local installed_at="$5"
  local state_input
  local identity_file
  local tmp_output

  state_input="$(mktemp)"
  identity_file="$(mktemp)"
  tmp_output="$(mktemp)"

  if [[ -f "${state_path}" ]]; then
    cp "${state_path}" "${state_input}"
  else
    printf '{}\n' >"${state_input}"
  fi

  write_release_identity "${manifest_path}" "${release_dir}" "${installed_at}" "${identity_file}"

  jq \
    --slurpfile identity "${identity_file}" \
    --arg component "${component}" \
    --arg updatedAt "${installed_at}" \
    '
      .schemaVersion = 1 |
      .updatedAt = $updatedAt |
      if $component == "full" then
        .binary = $identity[0] |
        .web = $identity[0]
      elif $component == "web" then
        .web = $identity[0]
      else
        error("unsupported release state component")
      end
    ' "${state_input}" >"${tmp_output}"

  install -m 0644 "${tmp_output}" "${state_path}"

  rm -f "${state_input}" "${identity_file}" "${tmp_output}"
}
