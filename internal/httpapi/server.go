package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"fascinate/internal/config"
	"fascinate/internal/controlplane"
	"fascinate/internal/database"
	"fascinate/internal/runtime/incus"
)

type runtimeChecker interface {
	HealthCheck(context.Context) error
	ListMachines(context.Context) ([]incus.Machine, error)
}

type machineManager interface {
	ListMachines(context.Context, string) ([]controlplane.Machine, error)
	GetMachine(context.Context, string) (controlplane.Machine, error)
	CreateMachine(context.Context, controlplane.CreateMachineInput) (controlplane.Machine, error)
	DeleteMachine(context.Context, string) error
	CloneMachine(context.Context, controlplane.CloneMachineInput) (controlplane.Machine, error)
}

func New(cfg config.Config, store *database.Store, runtime runtimeChecker, machines machineManager) http.Handler {
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
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}

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

	mux.HandleFunc("/v1/machines", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			defer cancel()

			ownerEmail := strings.TrimSpace(r.URL.Query().Get("owner_email"))
			machineList, err := machines.ListMachines(ctx, ownerEmail)
			if err != nil {
				writeServiceError(w, err)
				return
			}

			writeJSON(w, http.StatusOK, map[string]any{"machines": machineList})
		case http.MethodPost:
			var body struct {
				Name       string `json:"name"`
				OwnerEmail string `json:"owner_email"`
			}
			if err := decodeJSON(r, &body); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}

			ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
			defer cancel()

			machine, err := machines.CreateMachine(ctx, controlplane.CreateMachineInput{
				Name:       body.Name,
				OwnerEmail: body.OwnerEmail,
			})
			if err != nil {
				writeServiceError(w, err)
				return
			}

			writeJSON(w, http.StatusCreated, machine)
		default:
			writeMethodNotAllowed(w, http.MethodGet, http.MethodPost)
		}
	})

	mux.HandleFunc("/v1/machines/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/v1/machines/")
		path = strings.Trim(path, "/")
		if path == "" {
			http.NotFound(w, r)
			return
		}

		parts := strings.Split(path, "/")
		name := parts[0]
		if len(parts) == 1 {
			switch r.Method {
			case http.MethodGet:
				ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
				defer cancel()

				machine, err := machines.GetMachine(ctx, name)
				if err != nil {
					writeServiceError(w, err)
					return
				}

				writeJSON(w, http.StatusOK, machine)
			case http.MethodDelete:
				ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
				defer cancel()

				if err := machines.DeleteMachine(ctx, name); err != nil {
					writeServiceError(w, err)
					return
				}

				w.WriteHeader(http.StatusNoContent)
			default:
				writeMethodNotAllowed(w, http.MethodGet, http.MethodDelete)
			}
			return
		}

		if len(parts) == 2 && parts[1] == "clone" {
			if r.Method != http.MethodPost {
				writeMethodNotAllowed(w, http.MethodPost)
				return
			}

			var body struct {
				TargetName string `json:"target_name"`
				OwnerEmail string `json:"owner_email"`
			}
			if err := decodeJSON(r, &body); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}

			ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
			defer cancel()

			machine, err := machines.CloneMachine(ctx, controlplane.CloneMachineInput{
				SourceName: name,
				TargetName: body.TargetName,
				OwnerEmail: body.OwnerEmail,
			})
			if err != nil {
				writeServiceError(w, err)
				return
			}

			writeJSON(w, http.StatusCreated, machine)
			return
		}

		http.NotFound(w, r)
	})

	return mux
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func decodeJSON(r *http.Request, dest any) error {
	defer r.Body.Close()

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(dest); err != nil {
		return err
	}

	return nil
}

func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, database.ErrNotFound), errors.Is(err, incus.ErrMachineNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	case errors.Is(err, database.ErrConflict):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
}

func writeMethodNotAllowed(w http.ResponseWriter, methods ...string) {
	w.Header().Set("Allow", strings.Join(methods, ", "))
	writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
}
