package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	HTTPAddr           string
	DataDir            string
	DBPath             string
	BaseDomain         string
	AdminEmails        []string
	IncusBinary        string
	IncusStoragePool   string
	DefaultImage       string
	DefaultMachineCPU  string
	DefaultMachineRAM  string
	DefaultPrimaryPort int
}

func Load() Config {
	dataDir := getenv("FASCINATE_DATA_DIR", "./data")
	dbPath := getenv("FASCINATE_DB_PATH", "")
	if dbPath == "" {
		dbPath = filepath.Join(dataDir, "fascinate.db")
	}

	return Config{
		HTTPAddr:           getenv("FASCINATE_HTTP_ADDR", "127.0.0.1:8080"),
		DataDir:            dataDir,
		DBPath:             dbPath,
		BaseDomain:         getenv("FASCINATE_BASE_DOMAIN", ""),
		AdminEmails:        splitCSV(getenv("FASCINATE_ADMIN_EMAILS", "")),
		IncusBinary:        getenv("FASCINATE_INCUS_BINARY", "incus"),
		IncusStoragePool:   getenv("FASCINATE_INCUS_STORAGE_POOL", "machines"),
		DefaultImage:       getenv("FASCINATE_DEFAULT_IMAGE", "images:ubuntu/24.04"),
		DefaultMachineCPU:  getenv("FASCINATE_DEFAULT_MACHINE_CPU", "1"),
		DefaultMachineRAM:  getenv("FASCINATE_DEFAULT_MACHINE_RAM", "2GiB"),
		DefaultPrimaryPort: getenvInt("FASCINATE_DEFAULT_PRIMARY_PORT", 3000),
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
