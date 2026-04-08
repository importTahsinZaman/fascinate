package cloudhypervisor

import (
	"fmt"
	"path/filepath"
	"strings"

	"fascinate/internal/toolauth"
)

const guestImageManifestPath = "/etc/fascinate/image-manifest.json"

type imageBuildInputs struct {
	NodeVersion      string `json:"node_version,omitempty"`
	GoVersion        string `json:"go_version,omitempty"`
	CodexVersion     string `json:"codex_version,omitempty"`
	ClaudeInstallURL string `json:"claude_install_url,omitempty"`
	SourceImageURL   string `json:"source_image_url,omitempty"`
}

func machineReadinessCommand() string {
	return strings.Join([]string{
		"test -f /var/lib/cloud/instance/boot-finished",
		"test -f " + shellQuote(guestImageManifestPath),
		"claude --version >/dev/null 2>&1",
		"codex --version >/dev/null 2>&1",
		"gh --version >/dev/null 2>&1",
		"node --version >/dev/null 2>&1",
		"go version >/dev/null 2>&1",
		"docker --version >/dev/null 2>&1",
		"systemctl is-active --quiet docker",
	}, " && ")
}

func imageCloudInitUserData(hostname, guestUser, publicKey, script string) string {
	script = strings.TrimSpace(script)
	if script == "" {
		script = "set -euo pipefail\n"
	}
	return fmt.Sprintf(`#cloud-config
preserve_hostname: false
hostname: %s
users:
  - default
  - name: %s
    shell: /bin/bash
    sudo: ALL=(ALL) NOPASSWD:ALL
    groups: [adm, sudo]
    ssh_authorized_keys:
      - %s
ssh_pwauth: false
disable_root: true
runcmd:
  - [bash, /usr/local/sbin/fascinate-firstboot.sh]
write_files:
  - path: /usr/local/sbin/fascinate-firstboot.sh
    permissions: "0755"
    owner: root:root
    content: |
%s
`, strings.TrimSpace(hostname), strings.TrimSpace(guestUser), strings.TrimSpace(publicKey), indentBlock(script, "      "))
}

func machineBootUserData(meta metadata, baseDomain, publicKey, hostID, hostRegion string) string {
	return imageCloudInitUserData(meta.Name, meta.GuestUser, publicKey, machineFinalizationScript(meta, baseDomain, hostID, hostRegion))
}

func machineFinalizationScript(meta metadata, baseDomain, hostID, hostRegion string) string {
	envFile, envShell, envJSON, profileScript := bootstrapManagedEnvFiles(meta, baseDomain, hostID, hostRegion)
	agentInstructions := toolauth.ClaudeMachineInstructions(meta.Name, baseDomain, meta.PrimaryPort)
	return fmt.Sprintf(`set -euo pipefail

sudo hostnamectl set-hostname %s || true
sudo mkdir -p /etc/fascinate /etc/claude-code /etc/profile.d
sudo mkdir -p /root/.claude /root/.codex
sudo mkdir -p /home/%s/.claude /home/%s/.codex /home/%s/.config/gh /home/%s/.local/bin
sudo mkdir -p /etc/skel/.claude /etc/skel/.codex

sudo tee /etc/fascinate/env >/dev/null <<'EOF_ENV'
%sEOF_ENV
sudo tee /etc/fascinate/env.sh >/dev/null <<'EOF_ENV_SH'
%sEOF_ENV_SH
sudo tee /etc/fascinate/env.json >/dev/null <<'EOF_ENV_JSON'
%sEOF_ENV_JSON
sudo tee /etc/profile.d/fascinate-env.sh >/dev/null <<'EOF_PROFILE'
%sEOF_PROFILE
sudo chmod 0644 /etc/fascinate/env /etc/fascinate/env.sh /etc/fascinate/env.json /etc/profile.d/fascinate-env.sh

sudo tee /etc/profile.d/fascinate-paths.sh >/dev/null <<'EOF_PATHS'
case ":$PATH:" in
  *":$HOME/.local/bin:"*) ;;
  *) export PATH="$HOME/.local/bin:$PATH" ;;
esac
EOF_PATHS
sudo chmod 0644 /etc/profile.d/fascinate-paths.sh

sudo tee /etc/fascinate/AGENTS.md >/dev/null <<'EOF_AGENTS'
%s
EOF_AGENTS
sudo chmod 0644 /etc/fascinate/AGENTS.md

sudo ln -sfn /etc/fascinate/AGENTS.md /etc/claude-code/CLAUDE.md
sudo ln -sfn /etc/fascinate/AGENTS.md /root/AGENTS.md
sudo ln -sfn /etc/fascinate/AGENTS.md /root/.claude/CLAUDE.md
sudo ln -sfn /etc/fascinate/AGENTS.md /root/.codex/AGENTS.md
sudo ln -sfn /etc/fascinate/AGENTS.md /home/%s/AGENTS.md
sudo ln -sfn /etc/fascinate/AGENTS.md /home/%s/.claude/CLAUDE.md
sudo ln -sfn /etc/fascinate/AGENTS.md /home/%s/.codex/AGENTS.md
sudo ln -sfn /etc/fascinate/AGENTS.md /etc/skel/AGENTS.md
sudo ln -sfn /etc/fascinate/AGENTS.md /etc/skel/.claude/CLAUDE.md
sudo ln -sfn /etc/fascinate/AGENTS.md /etc/skel/.codex/AGENTS.md

sudo chown -R %s:%s /home/%s/.claude /home/%s/.codex /home/%s/.config /home/%s/.local
sudo chown -h %s:%s /home/%s/AGENTS.md /home/%s/.claude/CLAUDE.md /home/%s/.codex/AGENTS.md || true
`, shellQuote(meta.Name), meta.GuestUser, meta.GuestUser, meta.GuestUser, meta.GuestUser, envFile, envShell, envJSON, profileScript, agentInstructions, meta.GuestUser, meta.GuestUser, meta.GuestUser, meta.GuestUser, meta.GuestUser, meta.GuestUser, meta.GuestUser, meta.GuestUser, meta.GuestUser, meta.GuestUser, meta.GuestUser, meta.GuestUser, meta.GuestUser, meta.GuestUser)
}

func imageProvisioningScript(guestUser string, inputs imageBuildInputs, imageVersion string) string {
	nodeVersion := strings.TrimSpace(inputs.NodeVersion)
	if nodeVersion == "" {
		nodeVersion = "latest-lts"
	}
	goVersion := strings.TrimSpace(inputs.GoVersion)
	if goVersion == "" {
		goVersion = "latest"
	}
	codexVersion := strings.TrimSpace(inputs.CodexVersion)
	if codexVersion == "" {
		codexVersion = "latest"
	}
	claudeInstallURL := strings.TrimSpace(inputs.ClaudeInstallURL)
	if claudeInstallURL == "" {
		claudeInstallURL = "https://claude.ai/install.sh"
	}
	sourceImageURL := strings.TrimSpace(inputs.SourceImageURL)

	return fmt.Sprintf(`set -euo pipefail

export DEBIAN_FRONTEND=noninteractive

resolve_node_version() {
  local requested=%s
  case "${requested}" in
    latest-lts)
      curl -fsSL https://nodejs.org/dist/index.json | python3 -c 'import json, sys; releases = json.load(sys.stdin); print(next(release["version"] for release in releases if release.get("lts")))' ;;
    latest|"")
      curl -fsSL https://nodejs.org/dist/index.json | python3 -c 'import json, sys; releases = json.load(sys.stdin); print(releases[0]["version"])' ;;
    v*)
      printf "%%s\n" "${requested}" ;;
    *)
      printf "v%%s\n" "${requested}" ;;
  esac
}

resolve_go_version() {
  local requested=%s
  case "${requested}" in
    latest|"")
      curl -fsSL https://go.dev/dl/?mode=json | python3 -c 'import json, sys; releases = json.load(sys.stdin); print(releases[0]["version"].removeprefix("go"))' ;;
    go*)
      printf "%%s\n" "${requested#go}" ;;
    *)
      printf "%%s\n" "${requested}" ;;
  esac
}

resolve_codex_version() {
  local requested=%s
  case "${requested}" in
    latest|"")
      npm view @openai/codex version | tr -d '\n' ;;
    *)
      printf "%%s\n" "${requested}" ;;
  esac
}

node_arch() {
  case "$(dpkg --print-architecture)" in
    amd64) printf "%%s\n" "x64" ;;
    arm64) printf "%%s\n" "arm64" ;;
    *)
      printf "unsupported node architecture: %%s\n" "$(dpkg --print-architecture)" >&2
      exit 1 ;;
  esac
}

go_arch() {
  case "$(dpkg --print-architecture)" in
    amd64) printf "%%s\n" "amd64" ;;
    arm64) printf "%%s\n" "arm64" ;;
    *)
      printf "unsupported go architecture: %%s\n" "$(dpkg --print-architecture)" >&2
      exit 1 ;;
  esac
}

install_node() {
  local version="$1"
  local arch="$2"
  local file="node-${version}-linux-${arch}.tar.xz"
  local base_url="https://nodejs.org/dist/${version}"

  curl -fsSLO "${base_url}/${file}"
  curl -fsSL "${base_url}/SHASUMS256.txt" -o SHASUMS256.txt
  grep " ${file}$" SHASUMS256.txt | sha256sum -c -

  rm -rf /usr/local/lib/nodejs
  mkdir -p /usr/local/lib/nodejs
  tar -xJf "${file}" -C /usr/local/lib/nodejs

  ln -sf "/usr/local/lib/nodejs/node-${version}-linux-${arch}/bin/node" /usr/local/bin/node
  ln -sf "/usr/local/lib/nodejs/node-${version}-linux-${arch}/bin/npm" /usr/local/bin/npm
  ln -sf "/usr/local/lib/nodejs/node-${version}-linux-${arch}/bin/npx" /usr/local/bin/npx
  ln -sf "/usr/local/lib/nodejs/node-${version}-linux-${arch}/bin/corepack" /usr/local/bin/corepack
  corepack enable >/dev/null 2>&1 || true

  rm -f "${file}" SHASUMS256.txt
}

install_go() {
  local version="$1"
  local arch="$2"
  local file="go${version}.linux-${arch}.tar.gz"
  local checksum=""

  curl -fsSLo "${file}" "https://dl.google.com/go/${file}"
  checksum="$(curl -fsSL "https://go.dev/dl/?mode=json&include=all" | python3 -c 'import json, sys; target = sys.argv[1]; releases = json.load(sys.stdin); print(next(entry["sha256"] for release in releases for entry in release.get("files", []) if entry.get("filename") == target))' "${file}")"
  printf "%%s  %%s\n" "${checksum}" "${file}" | sha256sum -c -

  rm -rf /usr/local/go
  tar -C /usr/local -xzf "${file}"
  ln -sf /usr/local/go/bin/go /usr/local/bin/go
  ln -sf /usr/local/go/bin/gofmt /usr/local/bin/gofmt

  rm -f "${file}"
}

install_claude() {
  local guest_user=%s
  local install_url=%s
  su - "${guest_user}" -lc 'mkdir -p "$HOME/.local/bin"'
  su - "${guest_user}" -lc 'export PATH="$HOME/.local/bin:$PATH"; curl -fsSL '"${install_url}"' | bash'
  local guest_claude_path=""
  guest_claude_path="$(su - "${guest_user}" -lc 'export PATH="$HOME/.local/bin:$PATH"; command -v claude' | tr -d '\n')"
  if [[ -z "${guest_claude_path}" ]]; then
    echo "claude install did not produce a claude binary" >&2
    exit 1
  fi
  ln -sfn "${guest_claude_path}" /usr/local/bin/claude
}

apt-get update
apt-get upgrade -y
apt-get install -y build-essential ca-certificates curl docker.io file fzf gh git gnupg jq lsb-release make openssh-client procps python-is-python3 python3 python3-pip python3-venv ripgrep rsync sqlite3 tmux unzip wget xz-utils zip

NODE_RESOLVED_VERSION="$(resolve_node_version)"
GO_RESOLVED_VERSION="$(resolve_go_version)"
install_node "${NODE_RESOLVED_VERSION}" "$(node_arch)"
install_go "${GO_RESOLVED_VERSION}" "$(go_arch)"
CODEX_RESOLVED_VERSION="$(resolve_codex_version)"
npm install -g --force "@openai/codex@${CODEX_RESOLVED_VERSION}"
install_claude

mkdir -p /etc/systemd/system/docker.service.d /etc/fascinate /etc/claude-code /etc/profile.d
cat >/etc/systemd/system/docker.service.d/10-fascinate.conf <<'EOF_DOCKER'
[Service]
Environment=DOCKER_RAMDISK=true
EOF_DOCKER

cat >/etc/profile.d/fascinate-paths.sh <<'EOF_PATHS'
case ":$PATH:" in
  *":$HOME/.local/bin:"*) ;;
  *) export PATH="$HOME/.local/bin:$PATH" ;;
esac
EOF_PATHS
chmod 0644 /etc/profile.d/fascinate-paths.sh

systemctl daemon-reload
systemctl enable --now docker
usermod -aG docker %s || true

mkdir -p /root/.claude /root/.codex /root/.config/gh
mkdir -p /home/%s/.claude /home/%s/.codex /home/%s/.config/gh /home/%s/.local/bin
mkdir -p /etc/skel/.claude /etc/skel/.codex
chown -R %s:%s /home/%s/.claude /home/%s/.codex /home/%s/.config /home/%s/.local || true

python3 - <<'EOF_MANIFEST'
import json
import pathlib
import subprocess

def output(command):
    return subprocess.check_output(command, shell=True, text=True).strip()

manifest = {
    "format": "fascinate-image-manifest/v1",
    "version": %q,
    "built_at": output("date -u +%%Y-%%m-%%dT%%H:%%M:%%SZ"),
    "guest_user": %q,
    "build_inputs": {
        "node_version": %q,
        "go_version": %q,
        "codex_version": %q,
        "claude_install_url": %q,
        "source_image_url": %q,
    },
    "tools": {
        "python": output("python3 --version"),
        "node": output("node --version"),
        "npm": output("npm --version"),
        "go": output("go version"),
        "docker": output("docker --version"),
        "gh": output("gh --version | head -n1"),
        "claude": output("su - %s -lc 'export PATH=\"$HOME/.local/bin:$PATH\"; claude --version'"),
        "codex": output("codex --version"),
    },
}
path = pathlib.Path(%q)
path.parent.mkdir(parents=True, exist_ok=True)
path.write_text(json.dumps(manifest, indent=2) + "\n")
EOF_MANIFEST

apt-get clean
rm -rf /var/lib/apt/lists/*
`, shellQuote(nodeVersion), shellQuote(goVersion), shellQuote(codexVersion), shellQuote(guestUser), shellQuote(claudeInstallURL), guestUser, guestUser, guestUser, guestUser, guestUser, guestUser, guestUser, guestUser, guestUser, guestUser, guestUser, imageVersion, guestUser, nodeVersion, goVersion, codexVersion, claudeInstallURL, sourceImageURL, guestUser, guestImageManifestPath)
}

func imageSealScript(guestUser string) string {
	return fmt.Sprintf(`set -euo pipefail

sudo rm -f /etc/fascinate/env /etc/fascinate/env.sh /etc/fascinate/env.json /etc/profile.d/fascinate-env.sh /etc/fascinate/AGENTS.md
sudo rm -f /etc/claude-code/CLAUDE.md /root/AGENTS.md /root/.claude/CLAUDE.md /root/.codex/AGENTS.md
sudo rm -f /home/%s/AGENTS.md /home/%s/.claude/CLAUDE.md /home/%s/.codex/AGENTS.md
sudo rm -f /etc/skel/AGENTS.md /etc/skel/.claude/CLAUDE.md /etc/skel/.codex/AGENTS.md
sudo rm -f /root/.claude.json /home/%s/.claude.json
sudo rm -f /root/.claude/session.json /home/%s/.claude/session.json
sudo rm -f /root/.codex/auth.json /home/%s/.codex/auth.json
sudo rm -f /root/.config/gh/hosts.yml /home/%s/.config/gh/hosts.yml
sudo rm -f /root/.git-credentials /home/%s/.git-credentials
sudo rm -f /etc/ssh/ssh_host_*
sudo cloud-init clean --logs || true
sudo rm -rf /var/lib/cloud/instances/* /var/lib/cloud/instance /var/lib/cloud/seed/nocloud*
sudo truncate -s 0 /etc/machine-id
sudo rm -f /var/lib/dbus/machine-id /var/lib/systemd/random-seed
sudo rm -f /root/.bash_history /home/%s/.bash_history
sudo find /tmp -mindepth 1 -maxdepth 1 -exec rm -rf {} +
sudo find /var/tmp -mindepth 1 -maxdepth 1 -exec rm -rf {} +
sudo sync
`, guestUser, guestUser, guestUser, guestUser, guestUser, guestUser, guestUser, guestUser, guestUser)
}

func imageValidationCommand(meta metadata, baseDomain string) string {
	commands := []string{
		machineReadinessCommand(),
		"test -f " + shellQuote("/etc/fascinate/AGENTS.md"),
		"test -L " + shellQuote(filepath.Join("/home", meta.GuestUser, "AGENTS.md")),
		"grep -q '^FASCINATE_MACHINE_NAME=" + escapeForSingleQuotedGrep(meta.Name) + "$' /etc/fascinate/env",
		"grep -q '^FASCINATE_MACHINE_ID=" + escapeForSingleQuotedGrep(meta.MachineID) + "$' /etc/fascinate/env",
		"test ! -f " + shellQuote(filepath.Join("/home", meta.GuestUser, ".claude.json")),
		"test ! -f " + shellQuote(filepath.Join("/home", meta.GuestUser, ".claude", "session.json")),
		"test ! -f " + shellQuote(filepath.Join("/home", meta.GuestUser, ".codex", "auth.json")),
		"test ! -f " + shellQuote(filepath.Join("/home", meta.GuestUser, ".config", "gh", "hosts.yml")),
	}
	if strings.TrimSpace(baseDomain) != "" {
		publicURL := "https://" + machinePublicHost(meta.Name, baseDomain)
		commands = append(commands, "grep -q '^FASCINATE_PUBLIC_URL="+escapeForSingleQuotedGrep(publicURL)+"$' /etc/fascinate/env")
	}
	return strings.Join(commands, " && ")
}

func escapeForSingleQuotedGrep(value string) string {
	return strings.ReplaceAll(strings.TrimSpace(value), `'`, `'\''`)
}
