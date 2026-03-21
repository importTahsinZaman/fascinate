package toolauth

import (
	"path/filepath"
	"strings"
)

const (
	ToolIDGitHub                = "github"
	AuthMethodGitHubCLI         = "github-cli"
	GitHubCLISessionSpecVersion = 1
)

type GitHubCLIAdapter struct{}

func (GitHubCLIAdapter) ToolID() string {
	return ToolIDGitHub
}

func (GitHubCLIAdapter) AuthMethodID() string {
	return AuthMethodGitHubCLI
}

func (GitHubCLIAdapter) StorageMode() StorageMode {
	return StorageModeSessionState
}

func (GitHubCLIAdapter) SessionStateSpec(guestUser string) SessionStateSpec {
	guestUser = strings.TrimSpace(guestUser)
	if guestUser == "" {
		guestUser = "ubuntu"
	}

	guestHome := filepath.Join("/home", guestUser)
	return SessionStateSpec{
		Version: GitHubCLISessionSpecVersion,
		Roots: []SessionStateRoot{
			{
				Path:          filepath.Join(guestHome, ".gitconfig"),
				Kind:          SessionStateRootKindFile,
				Owner:         guestUser,
				Group:         guestUser,
				DirectoryMode: 0o700,
			},
			{
				Path:          filepath.Join(guestHome, ".git-credentials"),
				Kind:          SessionStateRootKindFile,
				Owner:         guestUser,
				Group:         guestUser,
				DirectoryMode: 0o700,
			},
			{
				Path:          filepath.Join(guestHome, ".config", "gh"),
				Kind:          SessionStateRootKindDirectory,
				Owner:         guestUser,
				Group:         guestUser,
				DirectoryMode: 0o700,
			},
		},
	}
}
