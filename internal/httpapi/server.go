package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"fascinate/internal/config"
	"fascinate/internal/database"
	"fascinate/internal/runtime/incus"
)

type runtimeChecker interface {
	HealthCheck(context.Context) error
	ListMachines(context.Context) ([]incus.Machine, error)
}

func New(cfg config.Config, store *database.Store, runtime runtimeChecker) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"service":      "fascinate",
			"base_domain":  cfg.BaseDomain,
			"admin_emails": cfg.AdminEmails,
		})
	})

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()

		if err := store.Ping(ctx); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status": "database unavailable",
			})
			return
		}

		if err := runtime.HealthCheck(ctx); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status": "runtime unavailable",
			})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})

	mux.HandleFunc("/v1/runtime/machines", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		machines, err := runtime.ListMachines(ctx)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"machines": machines,
		})
	})

	return mux
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
