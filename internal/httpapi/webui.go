package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"fascinate/internal/config"
)

func newWebUIHandler(cfg config.Config) http.Handler {
	distDir := strings.TrimSpace(cfg.WebDistDir)
	indexPath := filepath.Join(distDir, "index.html")
	if distDir == "" {
		return fallbackRootHandler(cfg)
	}
	if _, err := os.Stat(indexPath); err != nil {
		return fallbackRootHandler(cfg)
	}

	fileServer := http.FileServer(http.Dir(distDir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}
		cleanPath := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"))
		if cleanPath == "." {
			http.ServeFile(w, r, indexPath)
			return
		}
		fullPath := filepath.Join(distDir, cleanPath)
		if info, err := os.Stat(fullPath); err == nil && !info.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, indexPath)
	})
}

func fallbackRootHandler(cfg config.Config) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"service":     "fascinate",
			"base_domain": cfg.BaseDomain,
		})
	})
}
