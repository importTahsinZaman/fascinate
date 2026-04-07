package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAndSaveConfig(t *testing.T) {
	t.Setenv(envCLIConfigPath, filepath.Join(t.TempDir(), "config.json"))

	path, err := configPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveConfig(path, Config{
		BaseURL: "https://fascinate.dev",
		Token:   "abc",
		Email:   "dev@example.com",
	}); err != nil {
		t.Fatal(err)
	}

	cfg, loadedPath, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if loadedPath != path {
		t.Fatalf("unexpected config path %q", loadedPath)
	}
	if cfg.Token != "abc" || cfg.Email != "dev@example.com" {
		t.Fatalf("unexpected config %+v", cfg)
	}
}

func TestResolveTokenHonorsEnvironmentOverride(t *testing.T) {
	t.Setenv(envToken, "override")

	token := ResolveToken(Config{Token: "stored"})
	if token != "override" {
		t.Fatalf("expected env override, got %q", token)
	}
}

func TestResolveBaseURLDefaultsToPublicOrigin(t *testing.T) {
	t.Setenv(envBaseURL, "")
	if got := ResolveBaseURL(Config{}); got != defaultBaseURL {
		t.Fatalf("unexpected base URL %q", got)
	}
}

func TestConfigPathUsesXDGConfigHome(t *testing.T) {
	home := t.TempDir()
	xdgConfigHome := filepath.Join(home, ".config")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", xdgConfigHome)
	t.Setenv(envCLIConfigPath, "")
	t.Setenv(envCLIConfigDir, "")

	path, err := configPath()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != filepath.Join(xdgConfigHome, "fascinate") {
		t.Fatalf("unexpected config path %q", path)
	}
}

func TestSaveConfigUsesPrivatePermissions(t *testing.T) {
	t.Setenv(envCLIConfigPath, filepath.Join(t.TempDir(), "config.json"))
	path, err := configPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveConfig(path, Config{Token: "secret"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected 0600 permissions, got %#o", info.Mode().Perm())
	}
}
