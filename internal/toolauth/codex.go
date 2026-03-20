package toolauth

import (
	"path/filepath"
	"strings"
)

const (
	ToolIDCodex                 = "codex"
	AuthMethodCodexChatGPT      = "codex-chatgpt"
	CodexSessionStateSpecVersion = 1
)

type CodexChatGPTAdapter struct{}

func (CodexChatGPTAdapter) ToolID() string {
	return ToolIDCodex
}

func (CodexChatGPTAdapter) AuthMethodID() string {
	return AuthMethodCodexChatGPT
}

func (CodexChatGPTAdapter) StorageMode() StorageMode {
	return StorageModeSessionState
}

func (CodexChatGPTAdapter) SessionStateSpec(guestUser string) SessionStateSpec {
	guestUser = strings.TrimSpace(guestUser)
	if guestUser == "" {
		guestUser = "ubuntu"
	}

	return SessionStateSpec{
		Version: CodexSessionStateSpecVersion,
		Roots: []SessionStateRoot{
			{
				Path:             filepath.Join("/home", guestUser, ".codex"),
				Kind:             SessionStateRootKindDirectory,
				Owner:            guestUser,
				Group:            guestUser,
				DirectoryMode:    0o700,
				ExcludeBaseNames: []string{"AGENTS.md"},
			},
		},
	}
}
