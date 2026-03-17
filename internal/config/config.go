package config

import (
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	HTTPAddr    string
	DataDir     string
	DBPath      string
	BaseDomain  string
	AdminEmails []string
	IncusBinary string
}

func Load() Config {
	dataDir := getenv("FASCINATE_DATA_DIR", "./data")
	dbPath := getenv("FASCINATE_DB_PATH", "")
	if dbPath == "" {
		dbPath = filepath.Join(dataDir, "fascinate.db")
	}

	return Config{
		HTTPAddr:    getenv("FASCINATE_HTTP_ADDR", "127.0.0.1:8080"),
		DataDir:     dataDir,
		DBPath:      dbPath,
		BaseDomain:  getenv("FASCINATE_BASE_DOMAIN", ""),
		AdminEmails: splitCSV(getenv("FASCINATE_ADMIN_EMAILS", "")),
		IncusBinary: getenv("FASCINATE_INCUS_BINARY", "incus"),
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
