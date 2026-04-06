package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"html/template"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"

	"fascinate/internal/browserterm"
	"fascinate/internal/config"
	"fascinate/internal/controlplane"
	"fascinate/internal/database"
	machineruntime "fascinate/internal/runtime"
)

type runtimeChecker interface {
	HealthCheck(context.Context) error
	ListMachines(context.Context) ([]machineruntime.Machine, error)
}

type machineManager interface {
	ListMachines(context.Context, string) ([]controlplane.Machine, error)
	GetMachine(context.Context, string, string) (controlplane.Machine, error)
	GetPublicMachine(context.Context, string) (controlplane.Machine, error)
	GetMachineEnv(context.Context, string, string) (controlplane.MachineEnv, error)
	CreateMachine(context.Context, controlplane.CreateMachineInput) (controlplane.Machine, error)
	StartMachine(context.Context, string, string) (controlplane.Machine, error)
	StopMachine(context.Context, string, string) (controlplane.Machine, error)
	DeleteMachine(context.Context, string, string) error
	ForkMachine(context.Context, controlplane.ForkMachineInput) (controlplane.Machine, error)
	ListSnapshots(context.Context, string) ([]controlplane.Snapshot, error)
	CreateSnapshot(context.Context, controlplane.CreateSnapshotInput) (controlplane.Snapshot, error)
	DeleteSnapshot(context.Context, string, string) error
	ListEnvVars(context.Context, string) ([]controlplane.EnvVar, error)
	SetEnvVar(context.Context, controlplane.SetEnvVarInput) (controlplane.EnvVar, error)
	DeleteEnvVar(context.Context, string, string) error
}

type diagnosticsManager interface {
	ListHosts(context.Context) ([]controlplane.Host, error)
	GetBudgetDiagnostics(context.Context, string) (controlplane.BudgetDiagnostics, error)
	GetMachineDiagnostics(context.Context, string, string) (controlplane.MachineDiagnostics, error)
	GetSnapshotDiagnostics(context.Context, string, string) (controlplane.SnapshotDiagnostics, error)
	GetToolAuthDiagnostics(context.Context, string) (controlplane.ToolAuthDiagnostics, error)
	ListOwnerEvents(context.Context, string, int) ([]controlplane.Event, error)
}

type terminalManager interface {
	CreateSession(context.Context, string, string, int, int) (browserterm.SessionInit, error)
	ReattachSession(context.Context, string, string, int, int) (browserterm.SessionInit, error)
	CloseSession(context.Context, string, string) error
	CloseMachineSessions(context.Context, string, string) error
	GetGitStatus(context.Context, string, string, string) (browserterm.GitRepoStatus, error)
	GetGitDiffBatch(context.Context, string, string, browserterm.GitDiffBatchRequest) (browserterm.GitDiffBatchResponse, error)
	StreamSession(http.ResponseWriter, *http.Request, string) error
	Diagnostics() browserterm.Diagnostics
}

type readinessChecker interface {
	ReadinessStatus() (ready bool, status string)
}

func New(cfg config.Config, store *database.Store, runtime runtimeChecker, machines machineManager, auth browserAuthService, terminals terminalManager, readiness readinessChecker) http.Handler {
	mux := http.NewServeMux()
	diagnostics, _ := machines.(diagnosticsManager)

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

		if readiness != nil {
			ready, status := readiness.ReadinessStatus()
			if !ready {
				if strings.TrimSpace(status) == "" {
					status = "startup recovery in progress"
				}
				writeJSON(w, http.StatusServiceUnavailable, map[string]string{
					"status": status,
				})
				return
			}
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

	mux.HandleFunc("/v1/auth/request-code", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
		if auth == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "browser auth is not configured"})
			return
		}
		var body struct {
			Email string `json:"email"`
		}
		if err := decodeJSON(r, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		if err := auth.RequestCode(ctx, body.Email); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "verification code sent"})
	})

	mux.HandleFunc("/v1/auth/verify", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
		if auth == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "browser auth is not configured"})
			return
		}
		var body struct {
			Email string `json:"email"`
			Code  string `json:"code"`
		}
		if err := decodeJSON(r, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		session, err := auth.VerifyCode(ctx, body.Email, body.Code, r.UserAgent(), requestIPAddress(r))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		setSessionCookie(w, cfg, session.RawToken, session.ExpiresAt)
		writeJSON(w, http.StatusOK, map[string]any{
			"user":       session.User,
			"expires_at": session.ExpiresAt.Format(time.RFC3339),
		})
	})

	mux.HandleFunc("/v1/auth/session", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		session, err := requireBrowserSession(ctx, r, cfg, auth)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"user": session.User,
		})
	})

	mux.HandleFunc("/v1/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
		if auth != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			defer cancel()
			cookie, err := r.Cookie(cfg.WebSessionCookieName)
			if err == nil {
				_ = auth.Logout(ctx, cookie.Value)
			}
		}
		clearSessionCookie(w, cfg)
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("/v1/workspaces/default", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		session, err := requireBrowserSession(ctx, r, cfg, auth)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
			return
		}

		switch r.Method {
		case http.MethodGet:
			record, err := store.GetWorkspaceLayout(ctx, session.User.ID, "default")
			if err != nil {
				if !errors.Is(err, database.ErrNotFound) {
					writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
					return
				}
				writeJSON(w, http.StatusOK, map[string]any{
					"name":   "default",
					"layout": json.RawMessage(`{"version":1,"windows":[]}`),
				})
				return
			}
			writeWorkspaceLayout(w, record)
		case http.MethodPut:
			var body struct {
				Layout json.RawMessage `json:"layout"`
			}
			if err := decodeJSON(r, &body); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			layoutBody := strings.TrimSpace(string(body.Layout))
			if layoutBody == "" {
				layoutBody = `{"version":1,"windows":[]}`
			}
			if !json.Valid([]byte(layoutBody)) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "layout must be valid JSON"})
				return
			}
			record, err := store.UpsertWorkspaceLayout(ctx, database.UpsertWorkspaceLayoutParams{
				UserID:     session.User.ID,
				Name:       "default",
				LayoutJSON: layoutBody,
			})
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			writeWorkspaceLayout(w, record)
		default:
			writeMethodNotAllowed(w, http.MethodGet, http.MethodPut)
		}
	})

	mux.HandleFunc("/v1/terminal/sessions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
		if terminals == nil {
			http.NotFound(w, r)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		session, err := requireBrowserSession(ctx, r, cfg, auth)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
			return
		}
		var body struct {
			MachineName string `json:"machine_name"`
			Cols        int    `json:"cols"`
			Rows        int    `json:"rows"`
		}
		if err := decodeJSON(r, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		init, err := terminals.CreateSession(ctx, session.User.Email, body.MachineName, body.Cols, body.Rows)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, init)
	})

	mux.HandleFunc("/v1/terminal/sessions/", func(w http.ResponseWriter, r *http.Request) {
		if terminals == nil {
			http.NotFound(w, r)
			return
		}
		path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/terminal/sessions/"), "/")
		if path == "" {
			http.NotFound(w, r)
			return
		}
		parts := strings.Split(path, "/")
		if len(parts) == 2 && parts[1] == "stream" {
			if err := terminals.StreamSession(w, r, parts[0]); err != nil {
				writeServiceError(w, err)
			}
			return
		}
		if len(parts) == 3 && parts[1] == "git" && parts[2] == "status" {
			if r.Method != http.MethodPost {
				writeMethodNotAllowed(w, http.MethodPost)
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			defer cancel()
			session, err := requireBrowserSession(ctx, r, cfg, auth)
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
				return
			}
			var body struct {
				Cwd string `json:"cwd"`
			}
			if err := decodeJSON(r, &body); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			status, err := terminals.GetGitStatus(ctx, session.User.Email, parts[0], body.Cwd)
			if err != nil {
				writeServiceError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, status)
			return
		}
		if len(parts) == 3 && parts[1] == "git" && parts[2] == "diffs" {
			if r.Method != http.MethodPost {
				writeMethodNotAllowed(w, http.MethodPost)
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
			defer cancel()
			session, err := requireBrowserSession(ctx, r, cfg, auth)
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
				return
			}
			var body browserterm.GitDiffBatchRequest
			if err := decodeJSON(r, &body); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			diff, err := terminals.GetGitDiffBatch(ctx, session.User.Email, parts[0], body)
			if err != nil {
				writeServiceError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, diff)
			return
		}
		if len(parts) == 2 && parts[1] == "attach" {
			if r.Method != http.MethodPost {
				writeMethodNotAllowed(w, http.MethodPost)
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
			defer cancel()
			session, err := requireBrowserSession(ctx, r, cfg, auth)
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
				return
			}
			var body struct {
				Cols int `json:"cols"`
				Rows int `json:"rows"`
			}
			if err := decodeJSON(r, &body); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			init, err := terminals.ReattachSession(ctx, session.User.Email, parts[0], body.Cols, body.Rows)
			if err != nil {
				writeServiceError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, init)
			return
		}
		if len(parts) == 1 && r.Method == http.MethodDelete {
			ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
			defer cancel()
			session, err := requireBrowserSession(ctx, r, cfg, auth)
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
				return
			}
			if err := terminals.CloseSession(ctx, session.User.Email, parts[0]); err != nil {
				writeServiceError(w, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.NotFound(w, r)
	})

	mux.HandleFunc("/v1/machines", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			defer cancel()

			ownerEmail, err := ownerEmailForRequest(ctx, r, cfg, auth, strings.TrimSpace(r.URL.Query().Get("owner_email")))
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
				return
			}
			machineList, err := machines.ListMachines(ctx, ownerEmail)
			if err != nil {
				writeServiceError(w, err)
				return
			}

			writeJSON(w, http.StatusOK, map[string]any{"machines": machineList})
		case http.MethodPost:
			var body struct {
				Name         string `json:"name"`
				OwnerEmail   string `json:"owner_email"`
				SnapshotName string `json:"snapshot_name"`
			}
			if err := decodeJSON(r, &body); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			ownerEmail, err := ownerEmailForRequest(r.Context(), r, cfg, auth, body.OwnerEmail)
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
				return
			}

			ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
			defer cancel()

			machine, err := machines.CreateMachine(ctx, controlplane.CreateMachineInput{
				Name:         body.Name,
				OwnerEmail:   ownerEmail,
				SnapshotName: body.SnapshotName,
			})
			if err != nil {
				writeServiceError(w, err)
				return
			}

			writeJSON(w, http.StatusAccepted, machine)
		default:
			writeMethodNotAllowed(w, http.MethodGet, http.MethodPost)
		}
	})

	mux.HandleFunc("/v1/env-vars", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			defer cancel()

			ownerEmail, err := ownerEmailForRequest(ctx, r, cfg, auth, strings.TrimSpace(r.URL.Query().Get("owner_email")))
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
				return
			}
			envVars, err := machines.ListEnvVars(ctx, ownerEmail)
			if err != nil {
				writeServiceError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"env_vars": envVars})
		case http.MethodPut:
			var body struct {
				OwnerEmail string `json:"owner_email"`
				Key        string `json:"key"`
				Value      string `json:"value"`
			}
			if err := decodeJSON(r, &body); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			ownerEmail, err := ownerEmailForRequest(r.Context(), r, cfg, auth, body.OwnerEmail)
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
			defer cancel()

			envVar, err := machines.SetEnvVar(ctx, controlplane.SetEnvVarInput{
				OwnerEmail: ownerEmail,
				Key:        body.Key,
				Value:      body.Value,
			})
			if err != nil {
				writeServiceError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, envVar)
		default:
			writeMethodNotAllowed(w, http.MethodGet, http.MethodPut)
		}
	})

	mux.HandleFunc("/v1/env-vars/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			writeMethodNotAllowed(w, http.MethodDelete)
			return
		}
		key := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/env-vars/"), "/")
		if key == "" {
			http.NotFound(w, r)
			return
		}
		ownerEmail, err := ownerEmailForRequest(r.Context(), r, cfg, auth, strings.TrimSpace(r.URL.Query().Get("owner_email")))
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()

		if err := machines.DeleteEnvVar(ctx, ownerEmail, key); err != nil {
			writeServiceError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
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
				ownerEmail, err := ownerEmailForRequest(r.Context(), r, cfg, auth, strings.TrimSpace(r.URL.Query().Get("owner_email")))
				if err != nil {
					writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
					return
				}
				ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
				defer cancel()

				machine, err := machines.GetMachine(ctx, name, ownerEmail)
				if err != nil {
					writeServiceError(w, err)
					return
				}

				writeJSON(w, http.StatusOK, machine)
			case http.MethodDelete:
				ownerEmail, err := ownerEmailForRequest(r.Context(), r, cfg, auth, strings.TrimSpace(r.URL.Query().Get("owner_email")))
				if err != nil {
					writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
					return
				}
				ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
				defer cancel()

				if err := machines.DeleteMachine(ctx, name, ownerEmail); err != nil {
					writeServiceError(w, err)
					return
				}
				if terminals != nil {
					_ = terminals.CloseMachineSessions(ctx, ownerEmail, name)
				}

				w.WriteHeader(http.StatusNoContent)
			default:
				writeMethodNotAllowed(w, http.MethodGet, http.MethodDelete)
			}
			return
		}

		if len(parts) == 2 && parts[1] == "start" {
			if r.Method != http.MethodPost {
				writeMethodNotAllowed(w, http.MethodPost)
				return
			}
			ownerEmail, err := ownerEmailForRequest(r.Context(), r, cfg, auth, strings.TrimSpace(r.URL.Query().Get("owner_email")))
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 15*time.Minute)
			defer cancel()

			machine, err := machines.StartMachine(ctx, name, ownerEmail)
			if err != nil {
				writeServiceError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, machine)
			return
		}

		if len(parts) == 2 && parts[1] == "stop" {
			if r.Method != http.MethodPost {
				writeMethodNotAllowed(w, http.MethodPost)
				return
			}
			ownerEmail, err := ownerEmailForRequest(r.Context(), r, cfg, auth, strings.TrimSpace(r.URL.Query().Get("owner_email")))
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
			defer cancel()

			machine, err := machines.StopMachine(ctx, name, ownerEmail)
			if err != nil {
				writeServiceError(w, err)
				return
			}
			if terminals != nil {
				_ = terminals.CloseMachineSessions(ctx, ownerEmail, name)
			}
			writeJSON(w, http.StatusOK, machine)
			return
		}

		if len(parts) == 2 && parts[1] == "fork" {
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
			ownerEmail, err := ownerEmailForRequest(r.Context(), r, cfg, auth, body.OwnerEmail)
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
				return
			}

			ctx, cancel := context.WithTimeout(r.Context(), 15*time.Minute)
			defer cancel()

			machine, err := machines.ForkMachine(ctx, controlplane.ForkMachineInput{
				SourceName: name,
				TargetName: body.TargetName,
				OwnerEmail: ownerEmail,
			})
			if err != nil {
				writeServiceError(w, err)
				return
			}

			writeJSON(w, http.StatusCreated, machine)
			return
		}

		if len(parts) == 2 && parts[1] == "env" {
			if r.Method != http.MethodGet {
				writeMethodNotAllowed(w, http.MethodGet)
				return
			}
			ownerEmail, err := ownerEmailForRequest(r.Context(), r, cfg, auth, strings.TrimSpace(r.URL.Query().Get("owner_email")))
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			defer cancel()

			env, err := machines.GetMachineEnv(ctx, name, ownerEmail)
			if err != nil {
				writeServiceError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, env)
			return
		}

		http.NotFound(w, r)
	})

	mux.HandleFunc("/v1/snapshots", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			defer cancel()

			ownerEmail, err := ownerEmailForRequest(r.Context(), r, cfg, auth, strings.TrimSpace(r.URL.Query().Get("owner_email")))
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
				return
			}
			snapshotList, err := machines.ListSnapshots(ctx, ownerEmail)
			if err != nil {
				writeServiceError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"snapshots": snapshotList})
		case http.MethodPost:
			var body struct {
				MachineName  string `json:"machine_name"`
				SnapshotName string `json:"snapshot_name"`
				OwnerEmail   string `json:"owner_email"`
			}
			if err := decodeJSON(r, &body); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			ownerEmail, err := ownerEmailForRequest(r.Context(), r, cfg, auth, body.OwnerEmail)
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
			defer cancel()
			snapshot, err := machines.CreateSnapshot(ctx, controlplane.CreateSnapshotInput{
				MachineName:  body.MachineName,
				SnapshotName: body.SnapshotName,
				OwnerEmail:   ownerEmail,
			})
			if err != nil {
				writeServiceError(w, err)
				return
			}
			writeJSON(w, http.StatusAccepted, snapshot)
		default:
			writeMethodNotAllowed(w, http.MethodGet, http.MethodPost)
		}
	})

	mux.HandleFunc("/v1/snapshots/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/snapshots/"), "/")
		if name == "" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodDelete {
			writeMethodNotAllowed(w, http.MethodDelete)
			return
		}

		ownerEmail, err := ownerEmailForRequest(r.Context(), r, cfg, auth, strings.TrimSpace(r.URL.Query().Get("owner_email")))
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		if err := machines.DeleteSnapshot(ctx, name, ownerEmail); err != nil {
			writeServiceError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("/v1/diagnostics/events", func(w http.ResponseWriter, r *http.Request) {
		if diagnostics == nil {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}

		ownerEmail, err := ownerEmailForRequest(r.Context(), r, cfg, auth, strings.TrimSpace(r.URL.Query().Get("owner_email")))
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
			return
		}
		limit := 50
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			value, err := strconv.Atoi(raw)
			if err != nil || value <= 0 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "limit must be a positive integer"})
				return
			}
			limit = value
		}

		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		events, err := diagnostics.ListOwnerEvents(ctx, ownerEmail, limit)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"events": events})
	})

	mux.HandleFunc("/v1/diagnostics/hosts", func(w http.ResponseWriter, r *http.Request) {
		if diagnostics == nil {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		hosts, err := diagnostics.ListHosts(ctx)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"hosts": hosts})
	})

	mux.HandleFunc("/v1/diagnostics/budgets", func(w http.ResponseWriter, r *http.Request) {
		if diagnostics == nil {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}

		ownerEmail, err := ownerEmailForRequest(r.Context(), r, cfg, auth, strings.TrimSpace(r.URL.Query().Get("owner_email")))
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		diag, err := diagnostics.GetBudgetDiagnostics(ctx, ownerEmail)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, diag)
	})

	mux.HandleFunc("/v1/diagnostics/tool-auth", func(w http.ResponseWriter, r *http.Request) {
		if diagnostics == nil {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}

		ownerEmail, err := ownerEmailForRequest(r.Context(), r, cfg, auth, strings.TrimSpace(r.URL.Query().Get("owner_email")))
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		diag, err := diagnostics.GetToolAuthDiagnostics(ctx, ownerEmail)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, diag)
	})

	mux.HandleFunc("/v1/diagnostics/machines/", func(w http.ResponseWriter, r *http.Request) {
		if diagnostics == nil {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		name := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/diagnostics/machines/"), "/")
		if name == "" {
			http.NotFound(w, r)
			return
		}

		ownerEmail, err := ownerEmailForRequest(r.Context(), r, cfg, auth, strings.TrimSpace(r.URL.Query().Get("owner_email")))
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		diag, err := diagnostics.GetMachineDiagnostics(ctx, name, ownerEmail)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, diag)
	})

	mux.HandleFunc("/v1/diagnostics/snapshots/", func(w http.ResponseWriter, r *http.Request) {
		if diagnostics == nil {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		name := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/diagnostics/snapshots/"), "/")
		if name == "" {
			http.NotFound(w, r)
			return
		}

		ownerEmail, err := ownerEmailForRequest(r.Context(), r, cfg, auth, strings.TrimSpace(r.URL.Query().Get("owner_email")))
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		diag, err := diagnostics.GetSnapshotDiagnostics(ctx, name, ownerEmail)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, diag)
	})

	mux.HandleFunc("/v1/diagnostics/terminal-sessions", func(w http.ResponseWriter, r *http.Request) {
		if terminals == nil {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		writeJSON(w, http.StatusOK, terminals.Diagnostics())
	})

	mux.Handle("/", newWebUIHandler(cfg))

	return withMachineProxy(cfg, machines, mux)
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
	case errors.Is(err, database.ErrNotFound), errors.Is(err, machineruntime.ErrMachineNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	case errors.Is(err, database.ErrConflict):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
}

func writeWorkspaceLayout(w http.ResponseWriter, record database.WorkspaceLayoutRecord) {
	layout := json.RawMessage(record.LayoutJSON)
	if !json.Valid(layout) {
		layout = json.RawMessage(`{"version":1,"windows":[]}`)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":         record.ID,
		"name":       record.Name,
		"layout":     layout,
		"created_at": record.CreatedAt,
		"updated_at": record.UpdatedAt,
	})
}

func writeMethodNotAllowed(w http.ResponseWriter, methods ...string) {
	w.Header().Set("Allow", strings.Join(methods, ", "))
	writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
}

func withMachineProxy(cfg config.Config, machines machineManager, next http.Handler) http.Handler {
	baseDomain := normalizeHost(cfg.BaseDomain)
	if baseDomain == "" {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := normalizeHost(r.Host)
		if host == "" || host == baseDomain || host == "www."+baseDomain {
			next.ServeHTTP(w, r)
			return
		}
		if !strings.HasSuffix(host, "."+baseDomain) {
			next.ServeHTTP(w, r)
			return
		}

		machineName := strings.TrimSuffix(host, "."+baseDomain)
		if machineName == "" || strings.Contains(machineName, ".") {
			next.ServeHTTP(w, r)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		machine, err := machines.GetPublicMachine(ctx, machineName)
		if err != nil {
			if errors.Is(err, database.ErrNotFound) || errors.Is(err, machineruntime.ErrMachineNotFound) {
				writeMachinePage(w, http.StatusNotFound, host, "Unknown machine", "No machine with this name exists.")
				return
			}
			writeMachinePage(w, http.StatusBadGateway, host, "Machine unavailable", err.Error())
			return
		}
		switch strings.ToUpper(strings.TrimSpace(machine.State)) {
		case "STOPPED":
			writeMachinePage(w, http.StatusServiceUnavailable, host, "Machine is stopped", "Start this machine in Fascinate before opening its public app URL.")
			return
		case "CREATING", "STARTING", "STOPPING", "DELETING":
			writeMachinePage(w, http.StatusServiceUnavailable, host, "Machine is busy", "This machine is transitioning and is not route-ready yet.")
			return
		}

		target, ok := machineUpstream(machine)
		if !ok {
			writeMachinePage(w, http.StatusOK, host, "No services detected", "This machine is running but nothing is listening yet. Open Fascinate to inspect the machine and start a browser shell.")
			return
		}

		proxy := httputil.NewSingleHostReverseProxy(target)
		baseDirector := proxy.Director
		proxy.Director = func(req *http.Request) {
			baseDirector(req)
			req.Host = r.Host
			if req.Header.Get("X-Forwarded-Host") == "" {
				req.Header.Set("X-Forwarded-Host", r.Host)
			}
		}
		proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
			writeMachinePage(w, http.StatusOK, host, "No services detected", "This machine is running but nothing is listening yet. Open Fascinate to inspect the machine and start a browser shell.")
		}
		proxy.ServeHTTP(w, r)
	})
}

func machineUpstream(machine controlplane.Machine) (*url.URL, bool) {
	if machine.Runtime == nil {
		return nil, false
	}
	targetHost := strings.TrimSpace(machine.Runtime.AppHost)
	targetPort := machine.Runtime.AppPort
	if targetHost == "" || targetPort <= 0 {
		return nil, false
	}

	return &url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(targetHost, strconv.Itoa(targetPort)),
	}, true
}

func normalizeHost(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	host, _, err := net.SplitHostPort(value)
	if err == nil && host != "" {
		value = host
	}

	return strings.ToLower(strings.TrimSuffix(value, "."))
}

func writeMachinePage(w http.ResponseWriter, status int, host, title, body string) {
	const page = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{ .Host }}</title>
  <style>
    :root { color-scheme: light; }
    body { margin: 0; font-family: ui-sans-serif, system-ui, sans-serif; background: #f7f7f5; color: #161616; }
    main { max-width: 720px; margin: 12vh auto; padding: 0 24px; }
    h1 { margin-bottom: 12px; font-size: 40px; }
    p { font-size: 18px; line-height: 1.5; color: #4f4f4f; }
    pre { margin-top: 28px; padding: 18px 20px; border-radius: 14px; background: #111111; color: #f6f6f6; overflow-x: auto; }
  </style>
</head>
<body>
  <main>
    <h1>{{ .Title }}</h1>
    <p>{{ .Body }}</p>
  </main>
</body>
</html>`

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)

	tpl := template.Must(template.New("machine-page").Parse(page))
	_ = tpl.Execute(w, map[string]string{
		"Host":  host,
		"Title": title,
		"Body":  body,
	})
}
