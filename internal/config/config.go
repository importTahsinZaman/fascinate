package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr           string
	SSHAddr            string
	DataDir            string
	DBPath             string
	BaseDomain         string
	AdminEmails        []string
	IncusBinary        string
	IncusStoragePool   string
	DefaultImage       string
	DefaultMachineCPU  string
	DefaultMachineRAM  string
	DefaultMachineDisk string
	MaxMachinesPerUser int
	MaxMachineCPU      string
	MaxMachineRAM      string
	MaxMachineDisk     string
	DefaultPrimaryPort int
	SSHHostKeyPath     string
	ResendAPIKey       string
	ResendBaseURL      string
	EmailFrom          string
	SignupCodeTTL      time.Duration
}

func Load() Config {
	dataDir := getenv("FASCINATE_DATA_DIR", "./data")
	dbPath := getenv("FASCINATE_DB_PATH", "")
	if dbPath == "" {
		dbPath = filepath.Join(dataDir, "fascinate.db")
	}

	return Config{
		HTTPAddr:           getenv("FASCINATE_HTTP_ADDR", "127.0.0.1:8080"),
		SSHAddr:            getenv("FASCINATE_SSH_ADDR", "127.0.0.1:2222"),
		DataDir:            dataDir,
		DBPath:             dbPath,
		BaseDomain:         getenv("FASCINATE_BASE_DOMAIN", ""),
		AdminEmails:        splitCSV(getenv("FASCINATE_ADMIN_EMAILS", "")),
		IncusBinary:        getenv("FASCINATE_INCUS_BINARY", "incus"),
		IncusStoragePool:   getenv("FASCINATE_INCUS_STORAGE_POOL", "machines"),
		DefaultImage:       getenv("FASCINATE_DEFAULT_IMAGE", "images:ubuntu/24.04"),
		DefaultMachineCPU:  getenv("FASCINATE_DEFAULT_MACHINE_CPU", "1"),
		DefaultMachineRAM:  getenv("FASCINATE_DEFAULT_MACHINE_RAM", "2GiB"),
		DefaultMachineDisk: getenv("FASCINATE_DEFAULT_MACHINE_DISK", "20GiB"),
		MaxMachinesPerUser: getenvInt("FASCINATE_MAX_MACHINES_PER_USER", 3),
		MaxMachineCPU:      getenv("FASCINATE_MAX_MACHINE_CPU", "2"),
		MaxMachineRAM:      getenv("FASCINATE_MAX_MACHINE_RAM", "4GiB"),
		MaxMachineDisk:     getenv("FASCINATE_MAX_MACHINE_DISK", "20GiB"),
		DefaultPrimaryPort: getenvInt("FASCINATE_DEFAULT_PRIMARY_PORT", 3000),
		SSHHostKeyPath:     getenv("FASCINATE_SSH_HOST_KEY_PATH", filepath.Join(dataDir, "ssh_host_ed25519_key")),
		ResendAPIKey:       getenv("FASCINATE_RESEND_API_KEY", ""),
		ResendBaseURL:      getenv("FASCINATE_RESEND_BASE_URL", "https://api.resend.com"),
		EmailFrom:          getenv("FASCINATE_EMAIL_FROM", ""),
		SignupCodeTTL:      getenvDuration("FASCINATE_SIGNUP_CODE_TTL", 15*time.Minute),
	}
}

func getenv(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	return value
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}

	return out
}

func getenvInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}

	return parsed
}

func getenvDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}

	return parsed
}
