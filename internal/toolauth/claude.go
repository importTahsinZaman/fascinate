package toolauth

import (
	"fmt"
	"path/filepath"
	"strings"
)

const (
	ToolIDClaude                      = "claude"
	AuthMethodClaudeSubscription      = "claude-subscription"
	ClaudeSessionStateSpecVersion int = 1
)

type ClaudeSubscriptionAdapter struct{}

func (ClaudeSubscriptionAdapter) ToolID() string {
	return ToolIDClaude
}

func (ClaudeSubscriptionAdapter) AuthMethodID() string {
	return AuthMethodClaudeSubscription
}

func (ClaudeSubscriptionAdapter) StorageMode() StorageMode {
	return StorageModeSessionState
}

func (ClaudeSubscriptionAdapter) SessionStateSpec(guestUser string) SessionStateSpec {
	guestUser = strings.TrimSpace(guestUser)
	if guestUser == "" {
		guestUser = "ubuntu"
	}

	guestHome := filepath.Join("/home", guestUser)
	return SessionStateSpec{
		Version: ClaudeSessionStateSpecVersion,
		Roots: []SessionStateRoot{
			{
				Path:  filepath.Join(guestHome, ".claude.json"),
				Kind:  SessionStateRootKindFile,
				Owner: guestUser,
				Group: guestUser,
			},
			{
				Path:             filepath.Join(guestHome, ".claude"),
				Kind:             SessionStateRootKindDirectory,
				Owner:            guestUser,
				Group:            guestUser,
				DirectoryMode:    0o700,
				ExcludeBaseNames: []string{"CLAUDE.md"},
			},
		},
	}
}

func ClaudeMachineInstructions(machineName, baseDomain string, primaryPort int) string {
	publicHost := strings.TrimSpace(baseDomain)
	if machineName != "" && publicHost != "" {
		publicHost = fmt.Sprintf("%s.%s", strings.TrimSpace(machineName), publicHost)
	}
	if machineName != "" && publicHost == "" {
		publicHost = strings.TrimSpace(machineName)
	}
	allowedHostPattern := publicHost
	if trimmedBaseDomain := strings.TrimSpace(baseDomain); trimmedBaseDomain != "" {
		allowedHostPattern = "*." + trimmedBaseDomain
	}

	return fmt.Sprintf(`You are running inside a Fascinate VM.

Fascinate handles public HTTPS for this machine at https://%s.

Rules:
- Bind application servers to 0.0.0.0.
- Port %d is exposed at https://%s.
- Do not configure TLS certificates inside this machine for public app traffic.
- Verify that apps are actually usable from the Fascinate URL, not just localhost.
- If a framework restricts allowed hostnames or development origins, include %s.
- For Next.js development, add %s to allowedDevOrigins.
- Fascinate injects machine env vars at /etc/fascinate/env, /etc/fascinate/env.sh, /etc/fascinate/env.json, and /etc/profile.d/fascinate-env.sh.
- Prefer Fascinate env vars like FASCINATE_PUBLIC_URL, FASCINATE_MACHINE_NAME, and FASCINATE_PRIMARY_PORT over hardcoded machine hostnames in app config.
- For fork-safe config, prefer values like FRONTEND_URL=${FASCINATE_PUBLIC_URL} instead of literal https://%s URLs.
- Docker is available.
- Data on disk persists across restarts.
- Claude Code is preinstalled as 'claude'.
- Codex CLI is preinstalled as 'codex'.
- GitHub CLI is preinstalled as 'gh'. For private GitHub repositories, run 'gh auth login' and then 'gh auth setup-git'; Fascinate will persist that login to future VMs.`, publicHost, primaryPort, publicHost, allowedHostPattern, allowedHostPattern, publicHost)
}
