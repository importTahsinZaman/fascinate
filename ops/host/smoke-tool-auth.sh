#!/usr/bin/env bash
set -euo pipefail

ENV_FILE="${FASCINATE_ENV_FILE:-/etc/fascinate/fascinate.env}"
SMOKE_EMAIL="${FASCINATE_SMOKE_EMAIL:-smoke@example.com}"
SMOKE_FIRST_NAME="${FASCINATE_TOOL_AUTH_FIRST_NAME:-tool-auth-1-$(date +%s)}"
SMOKE_SECOND_NAME="${FASCINATE_TOOL_AUTH_SECOND_NAME:-tool-auth-2-$(date +%s)}"
SMOKE_THIRD_NAME="${FASCINATE_TOOL_AUTH_THIRD_NAME:-tool-auth-3-$(date +%s)}"

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

api_url() {
  printf 'http://%s%s' "${FASCINATE_HTTP_ADDR}" "$1"
}

delete_machine() {
  local name="$1"
  curl -fsS -X DELETE "$(api_url "/v1/machines/${name}?owner_email=${SMOKE_EMAIL}")" >/dev/null 2>&1 || true
}

cleanup() {
  delete_machine "${SMOKE_FIRST_NAME}"
  delete_machine "${SMOKE_SECOND_NAME}"
  delete_machine "${SMOKE_THIRD_NAME}"
}

wait_for_machine_state() {
  local name="$1"
  local want_state="$2"
  local attempts=180

  while (( attempts > 0 )); do
    local state
    state="$(curl -fsS "$(api_url "/v1/machines/${name}?owner_email=${SMOKE_EMAIL}")" 2>/dev/null | jq -r '.state // empty' || true)"
    if [[ "${state}" == "${want_state}" ]]; then
      return 0
    fi
    attempts=$((attempts - 1))
    sleep 2
  done

  echo "machine ${name} never reached ${want_state}" >&2
  exit 1
}

wait_for_machine_deleted() {
  local name="$1"
  local attempts=120

  while (( attempts > 0 )); do
    if ! curl -fsS "$(api_url "/v1/machines/${name}?owner_email=${SMOKE_EMAIL}")" >/dev/null 2>&1; then
      return 0
    fi
    attempts=$((attempts - 1))
    sleep 2
  done

  echo "machine ${name} was not deleted" >&2
  exit 1
}

guest_ssh_host() {
  local name="$1"
  curl -fsS "$(api_url "/v1/machines/${name}?owner_email=${SMOKE_EMAIL}")" | jq -r '.runtime.ssh_host // empty'
}

guest_ssh_port() {
  local name="$1"
  curl -fsS "$(api_url "/v1/machines/${name}?owner_email=${SMOKE_EMAIL}")" | jq -r '.runtime.ssh_port // 0'
}

run_guest_command() {
  local name="$1"
  shift

  local host port
  host="$(guest_ssh_host "${name}")"
  port="$(guest_ssh_port "${name}")"
  if [[ -z "${host}" || "${port}" == "0" ]]; then
    echo "missing guest SSH target for ${name}" >&2
    exit 1
  fi

  "${FASCINATE_SSH_CLIENT_BINARY}" \
    -i "${FASCINATE_GUEST_SSH_KEY_PATH}" \
    -o BatchMode=yes \
    -o StrictHostKeyChecking=no \
    -o UserKnownHostsFile=/dev/null \
    -p "${port}" \
    "${FASCINATE_GUEST_SSH_USER}@${host}" \
    "$@"
}

create_machine() {
  local name="$1"

  echo "creating ${name}"
  curl -fsS \
    -X POST \
    -H 'Content-Type: application/json' \
    -d "{\"name\":\"${name}\",\"owner_email\":\"${SMOKE_EMAIL}\"}" \
    "$(api_url '/v1/machines')" >/dev/null

  wait_for_machine_state "${name}" "RUNNING"
}

seed_tool_auth_state() {
  local name="$1"
  run_guest_command "${name}" "bash -lc '
    set -euo pipefail
    mkdir -p /home/ubuntu/.claude /home/ubuntu/.codex /home/ubuntu/.config/gh
    cat > /home/ubuntu/.claude.json <<\"EOF\"
{\"session\":\"claude-seeded\"}
EOF
    cat > /home/ubuntu/.claude/session.json <<\"EOF\"
{\"access_token\":\"claude-token\"}
EOF
    cat > /home/ubuntu/.codex/auth.json <<\"EOF\"
{\"access_token\":\"codex-token\"}
EOF
    cat > /home/ubuntu/.gitconfig <<\"EOF\"
[credential]
	helper = !/usr/bin/gh auth git-credential
EOF
    cat > /home/ubuntu/.git-credentials <<\"EOF\"
https://oauth:gho_example@github.com
EOF
    cat > /home/ubuntu/.config/gh/hosts.yml <<\"EOF\"
github.com:
    user: octocat
    oauth_token: gho_example
    git_protocol: https
EOF
    chown -R ubuntu:ubuntu /home/ubuntu/.claude /home/ubuntu/.codex /home/ubuntu/.config /home/ubuntu/.claude.json /home/ubuntu/.gitconfig /home/ubuntu/.git-credentials
    chmod 600 /home/ubuntu/.claude.json /home/ubuntu/.gitconfig /home/ubuntu/.git-credentials
  '"
}

verify_tool_auth_state_present() {
  local name="$1"
  run_guest_command "${name}" "bash -lc '
    set -euo pipefail
    test -f /home/ubuntu/.claude.json
    grep -q claude-seeded /home/ubuntu/.claude.json
    test -f /home/ubuntu/.claude/session.json
    grep -q codex-token /home/ubuntu/.codex/auth.json
    test -f /home/ubuntu/.config/gh/hosts.yml
    grep -q octocat /home/ubuntu/.config/gh/hosts.yml
    test -f /home/ubuntu/.gitconfig
    grep -q gh\\ auth\\ git-credential /home/ubuntu/.gitconfig
    test -f /home/ubuntu/.git-credentials
  '"
}

clear_tool_auth_state() {
  local name="$1"
  run_guest_command "${name}" "bash -lc '
    set -euo pipefail
    rm -f /home/ubuntu/.claude.json
    rm -rf /home/ubuntu/.claude
    mkdir -p /home/ubuntu/.claude
    rm -rf /home/ubuntu/.codex
    mkdir -p /home/ubuntu/.codex
    rm -f /home/ubuntu/.gitconfig /home/ubuntu/.git-credentials
    rm -rf /home/ubuntu/.config/gh
    mkdir -p /home/ubuntu/.config/gh
    chown -R ubuntu:ubuntu /home/ubuntu/.claude /home/ubuntu/.codex /home/ubuntu/.config
  '"
}

verify_tool_auth_state_absent() {
  local name="$1"
  run_guest_command "${name}" "bash -lc '
    set -euo pipefail
    test ! -f /home/ubuntu/.claude.json
    test ! -f /home/ubuntu/.claude/session.json
    test ! -f /home/ubuntu/.codex/auth.json
    test ! -f /home/ubuntu/.config/gh/hosts.yml
    test ! -f /home/ubuntu/.git-credentials
  '"
}

verify_tool_auth_profiles() {
  local expected_empty="$1"
  local output
  output="$(curl -fsS "$(api_url "/v1/diagnostics/tool-auth?owner_email=${SMOKE_EMAIL}")")"

  if [[ "${expected_empty}" == "false" ]]; then
    jq -e '.profiles | length >= 3' >/dev/null <<<"${output}"
    jq -e '.profiles[] | select(.key.tool_id == "claude" and .key.auth_method_id == "claude-subscription" and .empty == false)' >/dev/null <<<"${output}"
    jq -e '.profiles[] | select(.key.tool_id == "codex" and .key.auth_method_id == "codex-chatgpt" and .empty == false)' >/dev/null <<<"${output}"
    jq -e '.profiles[] | select(.key.tool_id == "github" and .key.auth_method_id == "github-cli" and .empty == false)' >/dev/null <<<"${output}"
  else
    jq -e '.profiles[] | select(.key.tool_id == "claude" and .empty == true)' >/dev/null <<<"${output}"
    jq -e '.profiles[] | select(.key.tool_id == "codex" and .empty == true)' >/dev/null <<<"${output}"
    jq -e '.profiles[] | select(.key.tool_id == "github" and .empty == true)' >/dev/null <<<"${output}"
  fi
}

main() {
  require_root

  for command_name in curl jq; do
    require_command "${command_name}"
  done

  if [[ ! -f "${ENV_FILE}" ]]; then
    echo "missing env file: ${ENV_FILE}" >&2
    exit 1
  fi

  set -a
  # shellcheck disable=SC1090
  source "${ENV_FILE}"
  set +a

  require_command "${FASCINATE_SSH_CLIENT_BINARY}"

  trap cleanup EXIT
  cleanup

  create_machine "${SMOKE_FIRST_NAME}"
  echo "seeding representative Claude, Codex, and GitHub auth state"
  seed_tool_auth_state "${SMOKE_FIRST_NAME}"

  echo "capturing seeded auth state via machine delete"
  delete_machine "${SMOKE_FIRST_NAME}"
  wait_for_machine_deleted "${SMOKE_FIRST_NAME}"
  verify_tool_auth_profiles false

  create_machine "${SMOKE_SECOND_NAME}"
  echo "verifying auth state restored into second machine"
  verify_tool_auth_state_present "${SMOKE_SECOND_NAME}"

  echo "clearing auth state inside second machine"
  clear_tool_auth_state "${SMOKE_SECOND_NAME}"
  delete_machine "${SMOKE_SECOND_NAME}"
  wait_for_machine_deleted "${SMOKE_SECOND_NAME}"
  verify_tool_auth_profiles true

  create_machine "${SMOKE_THIRD_NAME}"
  echo "verifying logged-out state restores into third machine"
  verify_tool_auth_state_absent "${SMOKE_THIRD_NAME}"

  echo "tool-auth smoke passed"
  trap - EXIT
  cleanup
}

main "$@"
