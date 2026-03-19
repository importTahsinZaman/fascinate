package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEnvFileSetsValues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fascinate.env")
	body := "" +
		"# comment\n" +
		"FASCINATE_HTTP_ADDR=0.0.0.0:8080\n" +
		"export FASCINATE_EMAIL_FROM=\"Fascinate <nate@fascinate.dev>\"\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := LoadEnvFile(path); err != nil {
		t.Fatalf("LoadEnvFile returned error: %v", err)
	}

	if got := os.Getenv("FASCINATE_HTTP_ADDR"); got != "0.0.0.0:8080" {
		t.Fatalf("HTTP addr = %q, want %q", got, "0.0.0.0:8080")
	}
	if got := os.Getenv("FASCINATE_EMAIL_FROM"); got != "Fascinate <nate@fascinate.dev>" {
		t.Fatalf("email from = %q", got)
	}
}

func TestLoadEnvFileRespectsExistingEnv(t *testing.T) {
	t.Setenv("FASCINATE_HTTP_ADDR", "127.0.0.1:9999")
	dir := t.TempDir()
	path := filepath.Join(dir, "fascinate.env")
	if err := os.WriteFile(path, []byte("FASCINATE_HTTP_ADDR=0.0.0.0:8080\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := LoadEnvFile(path); err != nil {
		t.Fatalf("LoadEnvFile returned error: %v", err)
	}

	if got := os.Getenv("FASCINATE_HTTP_ADDR"); got != "127.0.0.1:9999" {
		t.Fatalf("existing env was overwritten: %q", got)
	}
}

func TestLoadEnvFileMissingIsIgnored(t *testing.T) {
	if err := LoadEnvFile(filepath.Join(t.TempDir(), "missing.env")); err != nil {
		t.Fatalf("LoadEnvFile returned error for missing file: %v", err)
	}
}
