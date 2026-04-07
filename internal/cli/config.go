package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	envCLIConfigPath = "FASCINATE_CLI_CONFIG"
	envCLIConfigDir  = "FASCINATE_CLI_CONFIG_DIR"
	envBaseURL       = "FASCINATE_BASE_URL"
	envToken         = "FASCINATE_TOKEN"
	defaultBaseURL   = "https://fascinate.dev"
)

type Config struct {
	BaseURL string `json:"base_url"`
	Token   string `json:"token,omitempty"`
	Email   string `json:"email,omitempty"`
}

func configPath() (string, error) {
	if value := strings.TrimSpace(os.Getenv(envCLIConfigPath)); value != "" {
		return value, nil
	}
	if dir := strings.TrimSpace(os.Getenv(envCLIConfigDir)); dir != "" {
		return filepath.Join(dir, "config.json"), nil
	}
	if dir := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); dir != "" {
		return filepath.Join(dir, "fascinate", "config.json"), nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		home, homeErr := os.UserHomeDir()
		if homeErr != nil {
			return "", fmt.Errorf("resolve CLI config path: %w", err)
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "fascinate", "config.json"), nil
}

func LoadConfig() (Config, string, error) {
	path, err := configPath()
	if err != nil {
		return Config{}, "", err
	}
	body, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, path, nil
		}
		return Config{}, "", err
	}
	var cfg Config
	if err := json.Unmarshal(body, &cfg); err != nil {
		return Config{}, "", err
	}
	return cfg, path, nil
}

func SaveConfig(path string, cfg Config) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("config path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	body, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	return os.WriteFile(path, body, 0o600)
}

func ResolveBaseURL(stored Config) string {
	if value := strings.TrimSpace(os.Getenv(envBaseURL)); value != "" {
		return normalizeBaseURL(value)
	}
	if value := strings.TrimSpace(stored.BaseURL); value != "" {
		return normalizeBaseURL(value)
	}
	return defaultBaseURL
}

func ResolveToken(stored Config) string {
	if value := strings.TrimSpace(os.Getenv(envToken)); value != "" {
		return value
	}
	return strings.TrimSpace(stored.Token)
}

func normalizeBaseURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return defaultBaseURL
	}
	if !strings.Contains(value, "://") {
		if isLocalHostReference(value) {
			value = "http://" + value
		} else {
			value = "https://" + value
		}
	}
	return strings.TrimRight(value, "/")
}

func isLocalHostReference(value string) bool {
	host := strings.ToLower(strings.TrimSpace(value))
	return strings.HasPrefix(host, "localhost") ||
		strings.HasPrefix(host, "127.0.0.1") ||
		strings.HasPrefix(host, "[::1]") ||
		strings.HasPrefix(host, "0.0.0.0")
}
