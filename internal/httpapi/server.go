package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"

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
	CreateMachine(context.Context, controlplane.CreateMachineInput) (controlplane.Machine, error)
	DeleteMachine(context.Context, string, string) error
	CloneMachine(context.Context, controlplane.CloneMachineInput) (controlplane.Machine, error)
	ListSnapshots(context.Context, string) ([]controlplane.Snapshot, error)
	CreateSnapshot(context.Context, controlplane.CreateSnapshotInput) (controlplane.Snapshot, error)
	DeleteSnapshot(context.Context, string, string) error
}

func New(cfg config.Config, store *database.Store, runtime runtimeChecker, machines machineManager) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"service":     "fascinate",
			"base_domain": cfg.BaseDomain,
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

			ownerEmail, err := requiredOwnerEmail(strings.TrimSpace(r.URL.Query().Get("owner_email")))
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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
			ownerEmail, err := requiredOwnerEmail(body.OwnerEmail)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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
				ownerEmail, err := requiredOwnerEmail(strings.TrimSpace(r.URL.Query().Get("owner_email")))
				if err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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
				ownerEmail, err := requiredOwnerEmail(strings.TrimSpace(r.URL.Query().Get("owner_email")))
				if err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
					return
				}
				ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
				defer cancel()

				if err := machines.DeleteMachine(ctx, name, ownerEmail); err != nil {
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
			ownerEmail, err := requiredOwnerEmail(body.OwnerEmail)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}

			ctx, cancel := context.WithTimeout(r.Context(), 15*time.Minute)
			defer cancel()

			machine, err := machines.CloneMachine(ctx, controlplane.CloneMachineInput{
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

		http.NotFound(w, r)
	})

	mux.HandleFunc("/v1/snapshots", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			defer cancel()

			ownerEmail, err := requiredOwnerEmail(strings.TrimSpace(r.URL.Query().Get("owner_email")))
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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
			ownerEmail, err := requiredOwnerEmail(body.OwnerEmail)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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

		ownerEmail, err := requiredOwnerEmail(strings.TrimSpace(r.URL.Query().Get("owner_email")))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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

func requiredOwnerEmail(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("owner_email is required")
	}
	return value, nil
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
				writeMachinePage(w, http.StatusNotFound, host, "Unknown machine", "No machine with this name exists.", "")
				return
			}
			writeMachinePage(w, http.StatusBadGateway, host, "Machine unavailable", err.Error(), "")
			return
		}

		target, ok := machineUpstream(machine)
		if !ok {
			writeMachinePage(w, http.StatusOK, host, "No services detected", "This machine is running but nothing is listening yet.", machineShellCommand(machine.Name))
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
			writeMachinePage(w, http.StatusOK, host, "No services detected", "This machine is running but nothing is listening yet.", machineShellCommand(machine.Name))
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

func machineShellCommand(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	return fmt.Sprintf("ssh -tt fascinate.dev shell %s", name)
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

func writeMachinePage(w http.ResponseWriter, status int, host, title, body, command string) {
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
    {{ if .Command }}<pre>{{ .Command }}</pre>{{ end }}
  </main>
</body>
</html>`

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)

	tpl := template.Must(template.New("machine-page").Parse(page))
	_ = tpl.Execute(w, map[string]string{
		"Host":    host,
		"Title":   title,
		"Body":    body,
		"Command": command,
	})
}
