package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"fascinate/internal/browserterm"
	"fascinate/internal/controlplane"
	"fascinate/internal/database"
)

type Client struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
}

type User struct {
	ID      string `json:"id"`
	Email   string `json:"email"`
	IsAdmin bool   `json:"is_admin"`
}

type TokenInfo struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	ExpiresAt  string `json:"expires_at"`
	CreatedAt  string `json:"created_at"`
	LastUsedAt string `json:"last_used_at"`
}

type VerifyResponse struct {
	User      User   `json:"user"`
	Token     string `json:"token"`
	TokenID   string `json:"token_id"`
	TokenName string `json:"token_name"`
	ExpiresAt string `json:"expires_at"`
}

type Shell struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	UserEmail      string  `json:"user_email,omitempty"`
	MachineName    string  `json:"machine_name"`
	HostID         string  `json:"host_id,omitempty"`
	State          string  `json:"state"`
	CWD            string  `json:"cwd,omitempty"`
	LastAttachedAt *string `json:"last_attached_at,omitempty"`
	LastError      string  `json:"last_error,omitempty"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
}

type AttachmentInfo struct {
	ID          string `json:"id"`
	HostID      string `json:"host_id"`
	MachineName string `json:"machine_name"`
	AttachURL   string `json:"attach_url"`
	ExpiresAt   string `json:"expires_at"`
}

type EnvVar struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

type ExecLaunch struct {
	Exec      browserterm.Exec `json:"exec"`
	StreamURL string           `json:"stream_url"`
	CancelURL string           `json:"cancel_url"`
}

func (c Client) RequestLoginCode(ctx context.Context, email string) error {
	return c.request(ctx, http.MethodPost, "/v1/cli/auth/request-code", map[string]string{
		"email": strings.TrimSpace(email),
	}, nil)
}

func (c Client) VerifyLoginCode(ctx context.Context, email, code, tokenName string) (VerifyResponse, error) {
	var out VerifyResponse
	err := c.request(ctx, http.MethodPost, "/v1/cli/auth/verify", map[string]string{
		"email":      strings.TrimSpace(email),
		"code":       strings.TrimSpace(code),
		"token_name": strings.TrimSpace(tokenName),
	}, &out)
	return out, err
}

func (c Client) Session(ctx context.Context) (User, TokenInfo, error) {
	var body struct {
		User  User      `json:"user"`
		Token TokenInfo `json:"token"`
	}
	err := c.request(ctx, http.MethodGet, "/v1/cli/auth/session", nil, &body)
	return body.User, body.Token, err
}

func (c Client) Logout(ctx context.Context) error {
	return c.request(ctx, http.MethodPost, "/v1/cli/auth/logout", nil, nil)
}

func (c Client) ListShells(ctx context.Context) ([]Shell, error) {
	var body struct {
		Shells []Shell `json:"shells"`
	}
	err := c.request(ctx, http.MethodGet, "/v1/shells", nil, &body)
	return body.Shells, err
}

func (c Client) GetShell(ctx context.Context, shellID string) (Shell, error) {
	var out Shell
	err := c.request(ctx, http.MethodGet, "/v1/shells/"+url.PathEscape(strings.TrimSpace(shellID)), nil, &out)
	return out, err
}

func (c Client) CreateShell(ctx context.Context, machineName, name string) (Shell, error) {
	var out Shell
	err := c.request(ctx, http.MethodPost, "/v1/shells", map[string]string{
		"machine_name": strings.TrimSpace(machineName),
		"name":         strings.TrimSpace(name),
	}, &out)
	return out, err
}

func (c Client) DeleteShell(ctx context.Context, shellID string) error {
	return c.request(ctx, http.MethodDelete, "/v1/shells/"+url.PathEscape(strings.TrimSpace(shellID)), nil, nil)
}

func (c Client) CreateShellAttachment(ctx context.Context, shellID string, cols, rows int) (AttachmentInfo, error) {
	var out AttachmentInfo
	err := c.request(ctx, http.MethodPost, "/v1/shells/"+url.PathEscape(strings.TrimSpace(shellID))+"/attach", map[string]int{
		"cols": cols,
		"rows": rows,
	}, &out)
	return out, err
}

func (c Client) SendShellInput(ctx context.Context, shellID, input string) error {
	return c.request(ctx, http.MethodPost, "/v1/shells/"+url.PathEscape(strings.TrimSpace(shellID))+"/input", map[string]string{
		"input": input,
	}, nil)
}

func (c Client) ReadShellLines(ctx context.Context, shellID string, limit int) ([]string, error) {
	path := "/v1/shells/" + url.PathEscape(strings.TrimSpace(shellID)) + "/lines"
	if limit > 0 {
		path += fmt.Sprintf("?limit=%d", limit)
	}
	var body struct {
		Lines []string `json:"lines"`
	}
	err := c.request(ctx, http.MethodGet, path, nil, &body)
	return body.Lines, err
}

func (c Client) ListMachines(ctx context.Context) ([]controlplane.Machine, error) {
	var body struct {
		Machines []controlplane.Machine `json:"machines"`
	}
	err := c.request(ctx, http.MethodGet, "/v1/machines", nil, &body)
	return body.Machines, err
}

func (c Client) GetMachine(ctx context.Context, machineName string) (controlplane.Machine, error) {
	var out controlplane.Machine
	err := c.request(ctx, http.MethodGet, "/v1/machines/"+url.PathEscape(strings.TrimSpace(machineName)), nil, &out)
	return out, err
}

func (c Client) CreateMachine(ctx context.Context, name, snapshotName string) (controlplane.Machine, error) {
	var out controlplane.Machine
	err := c.request(ctx, http.MethodPost, "/v1/machines", map[string]string{
		"name":          strings.TrimSpace(name),
		"snapshot_name": strings.TrimSpace(snapshotName),
	}, &out)
	return out, err
}

func (c Client) DeleteMachine(ctx context.Context, machineName string) error {
	return c.request(ctx, http.MethodDelete, "/v1/machines/"+url.PathEscape(strings.TrimSpace(machineName)), nil, nil)
}

func (c Client) ForkMachine(ctx context.Context, sourceName, targetName string) (controlplane.Machine, error) {
	var out controlplane.Machine
	err := c.request(ctx, http.MethodPost, "/v1/machines/"+url.PathEscape(strings.TrimSpace(sourceName))+"/fork", map[string]string{
		"target_name": strings.TrimSpace(targetName),
	}, &out)
	return out, err
}

func (c Client) GetMachineEnv(ctx context.Context, machineName string) (controlplane.MachineEnv, error) {
	var out controlplane.MachineEnv
	err := c.request(ctx, http.MethodGet, "/v1/machines/"+url.PathEscape(strings.TrimSpace(machineName))+"/env", nil, &out)
	return out, err
}

func (c Client) ListSnapshots(ctx context.Context) ([]controlplane.Snapshot, error) {
	var body struct {
		Snapshots []controlplane.Snapshot `json:"snapshots"`
	}
	err := c.request(ctx, http.MethodGet, "/v1/snapshots", nil, &body)
	return body.Snapshots, err
}

func (c Client) CreateSnapshot(ctx context.Context, machineName, snapshotName string) (controlplane.Snapshot, error) {
	var out controlplane.Snapshot
	err := c.request(ctx, http.MethodPost, "/v1/snapshots", map[string]string{
		"machine_name":  strings.TrimSpace(machineName),
		"snapshot_name": strings.TrimSpace(snapshotName),
	}, &out)
	return out, err
}

func (c Client) DeleteSnapshot(ctx context.Context, snapshotName string) error {
	return c.request(ctx, http.MethodDelete, "/v1/snapshots/"+url.PathEscape(strings.TrimSpace(snapshotName)), nil, nil)
}

func (c Client) RestoreSnapshot(ctx context.Context, snapshotName, machineName string) (controlplane.Machine, error) {
	return c.CreateMachine(ctx, machineName, snapshotName)
}

func (c Client) ListEnvVars(ctx context.Context) ([]EnvVar, []controlplane.BuiltinEnvVar, error) {
	var body struct {
		EnvVars        []EnvVar                     `json:"env_vars"`
		BuiltinEnvVars []controlplane.BuiltinEnvVar `json:"builtin_env_vars"`
	}
	err := c.request(ctx, http.MethodGet, "/v1/env-vars", nil, &body)
	return body.EnvVars, body.BuiltinEnvVars, err
}

func (c Client) SetEnvVar(ctx context.Context, key, value string) (EnvVar, error) {
	var out EnvVar
	err := c.request(ctx, http.MethodPut, "/v1/env-vars", map[string]string{
		"key":   strings.TrimSpace(key),
		"value": value,
	}, &out)
	return out, err
}

func (c Client) DeleteEnvVar(ctx context.Context, key string) error {
	return c.request(ctx, http.MethodDelete, "/v1/env-vars/"+url.PathEscape(strings.TrimSpace(key)), nil, nil)
}

func (c Client) DiagnosticsEvents(ctx context.Context, limit int) ([]controlplane.Event, database.EventStreamDiagnostics, error) {
	path := "/v1/diagnostics/events"
	if limit > 0 {
		path += fmt.Sprintf("?limit=%d", limit)
	}
	var body struct {
		Events []controlplane.Event            `json:"events"`
		Stream database.EventStreamDiagnostics `json:"stream"`
	}
	err := c.request(ctx, http.MethodGet, path, nil, &body)
	return body.Events, body.Stream, err
}

func (c Client) DiagnosticsHosts(ctx context.Context) ([]controlplane.Host, error) {
	var body struct {
		Hosts []controlplane.Host `json:"hosts"`
	}
	err := c.request(ctx, http.MethodGet, "/v1/diagnostics/hosts", nil, &body)
	return body.Hosts, err
}

func (c Client) DiagnosticsBudgets(ctx context.Context) (controlplane.BudgetDiagnostics, error) {
	var out controlplane.BudgetDiagnostics
	err := c.request(ctx, http.MethodGet, "/v1/diagnostics/budgets", nil, &out)
	return out, err
}

func (c Client) DiagnosticsToolAuth(ctx context.Context) (controlplane.ToolAuthDiagnostics, error) {
	var out controlplane.ToolAuthDiagnostics
	err := c.request(ctx, http.MethodGet, "/v1/diagnostics/tool-auth", nil, &out)
	return out, err
}

func (c Client) DiagnosticsMachine(ctx context.Context, machineName string) (controlplane.MachineDiagnostics, error) {
	var out controlplane.MachineDiagnostics
	err := c.request(ctx, http.MethodGet, "/v1/diagnostics/machines/"+url.PathEscape(strings.TrimSpace(machineName)), nil, &out)
	return out, err
}

func (c Client) DiagnosticsSnapshot(ctx context.Context, snapshotName string) (controlplane.SnapshotDiagnostics, error) {
	var out controlplane.SnapshotDiagnostics
	err := c.request(ctx, http.MethodGet, "/v1/diagnostics/snapshots/"+url.PathEscape(strings.TrimSpace(snapshotName)), nil, &out)
	return out, err
}

func (c Client) DiagnosticsShells(ctx context.Context) (browserterm.Diagnostics, error) {
	var out browserterm.Diagnostics
	err := c.request(ctx, http.MethodGet, "/v1/diagnostics/terminal-sessions", nil, &out)
	return out, err
}

func (c Client) DiagnosticsExecs(ctx context.Context, limit int) (browserterm.ExecDiagnostics, error) {
	path := "/v1/diagnostics/execs"
	if limit > 0 {
		path += fmt.Sprintf("?limit=%d", limit)
	}
	var out browserterm.ExecDiagnostics
	err := c.request(ctx, http.MethodGet, path, nil, &out)
	return out, err
}

func (c Client) CreateExec(ctx context.Context, machineName, commandText, cwd string, timeout time.Duration) (ExecLaunch, error) {
	var out ExecLaunch
	timeoutSeconds := 0
	if timeout > 0 {
		timeoutSeconds = int(timeout / time.Second)
		if timeoutSeconds <= 0 {
			timeoutSeconds = 1
		}
	}
	err := c.request(ctx, http.MethodPost, "/v1/execs", map[string]any{
		"machine_name":    strings.TrimSpace(machineName),
		"command_text":    strings.TrimSpace(commandText),
		"cwd":             strings.TrimSpace(cwd),
		"timeout_seconds": timeoutSeconds,
	}, &out)
	return out, err
}

func (c Client) GetExec(ctx context.Context, execID string) (browserterm.Exec, error) {
	var out browserterm.Exec
	err := c.request(ctx, http.MethodGet, "/v1/execs/"+url.PathEscape(strings.TrimSpace(execID)), nil, &out)
	return out, err
}

func (c Client) ListExecs(ctx context.Context, limit int) ([]browserterm.Exec, error) {
	path := "/v1/execs"
	if limit > 0 {
		path += fmt.Sprintf("?limit=%d", limit)
	}
	var body struct {
		Execs []browserterm.Exec `json:"execs"`
	}
	err := c.request(ctx, http.MethodGet, path, nil, &body)
	return body.Execs, err
}

func (c Client) CancelExec(ctx context.Context, execID string) error {
	return c.request(ctx, http.MethodPost, "/v1/execs/"+url.PathEscape(strings.TrimSpace(execID))+"/cancel", nil, nil)
}

func (c Client) StreamExec(ctx context.Context, execID string, handler func(browserterm.ExecStreamEvent) error) error {
	req, err := c.newRequest(ctx, http.MethodGet, "/v1/execs/"+url.PathEscape(strings.TrimSpace(execID))+"/stream", nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClientForStream().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var problem struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&problem)
		if strings.TrimSpace(problem.Error) != "" {
			return fmt.Errorf("%s", problem.Error)
		}
		return fmt.Errorf("%s", resp.Status)
	}

	reader := bufio.NewReader(resp.Body)
	var eventType string
	var dataLines []string
	emit := func() error {
		if len(dataLines) == 0 {
			return nil
		}
		var event browserterm.ExecStreamEvent
		if err := json.Unmarshal([]byte(strings.Join(dataLines, "\n")), &event); err != nil {
			return err
		}
		if strings.TrimSpace(event.Type) == "" {
			event.Type = eventType
		}
		dataLines = nil
		eventType = ""
		if handler == nil {
			return nil
		}
		return handler(event)
	}

	for {
		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		line = strings.TrimRight(line, "\r\n")
		switch {
		case strings.HasPrefix(line, "event:"):
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		case line == "":
			if emitErr := emit(); emitErr != nil {
				return emitErr
			}
		}
		if errors.Is(err, io.EOF) {
			if len(dataLines) > 0 {
				return emit()
			}
			return nil
		}
	}
}

func (c Client) request(ctx context.Context, method, path string, input any, output any) error {
	req, err := c.newRequest(ctx, method, path, input)
	if err != nil {
		return err
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var problem struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&problem)
		if strings.TrimSpace(problem.Error) != "" {
			return fmt.Errorf("%s", problem.Error)
		}
		return fmt.Errorf("%s", resp.Status)
	}
	if output == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(output)
}

func (c Client) newRequest(ctx context.Context, method, path string, input any) (*http.Request, error) {
	baseURL := normalizeBaseURL(c.BaseURL)
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	var body io.Reader
	if input != nil {
		payload, err := json.Marshal(input)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(c.Token) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(c.Token))
	}
	return req, nil
}

func (c Client) httpClient() *http.Client {
	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return httpClient
}

func (c Client) httpClientForStream() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{}
}
