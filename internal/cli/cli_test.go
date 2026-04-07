package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoginRequiresEmailWhenNotInteractive(t *testing.T) {
	t.Setenv(envCLIConfigPath, t.TempDir()+"/config.json")

	runner := Runner{
		Stdin:  bytes.NewBuffer(nil),
		Stdout: &bytes.Buffer{},
		Stderr: &bytes.Buffer{},
	}
	err := runner.Run(context.Background(), []string{"login"})
	if err == nil || !strings.Contains(err.Error(), "email is required") {
		t.Fatalf("expected email error, got %v", err)
	}
}

func TestWhoAmIRequiresToken(t *testing.T) {
	t.Setenv(envCLIConfigPath, t.TempDir()+"/config.json")

	runner := Runner{
		Stdin:  bytes.NewBuffer(nil),
		Stdout: &bytes.Buffer{},
		Stderr: &bytes.Buffer{},
	}
	err := runner.Run(context.Background(), []string{"whoami"})
	if err == nil || !strings.Contains(err.Error(), "no API token configured") {
		t.Fatalf("expected missing token error, got %v", err)
	}
}

func TestLogoutWarnsWhenRemoteRevocationFails(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	t.Setenv(envCLIConfigPath, configPath)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/cli/auth/logout" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "gateway unavailable"})
	}))
	defer server.Close()

	if err := SaveConfig(configPath, Config{
		BaseURL: server.URL,
		Token:   "cli-token",
		Email:   "dev@example.com",
	}); err != nil {
		t.Fatal(err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	runner := Runner{
		Stdin:  bytes.NewBuffer(nil),
		Stdout: stdout,
		Stderr: stderr,
	}
	if err := runner.Run(context.Background(), []string{"logout"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "Logged out.") {
		t.Fatalf("unexpected stdout %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "remote token revocation failed") {
		t.Fatalf("expected warning on stderr, got %q", stderr.String())
	}
	cfg, _, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Token != "" {
		t.Fatalf("expected token to be cleared locally, got %+v", cfg)
	}
}

func TestHelpIncludesQuickstartAndTopicHints(t *testing.T) {
	stdout := &bytes.Buffer{}
	runner := Runner{
		Stdin:  bytes.NewBuffer(nil),
		Stdout: stdout,
		Stderr: &bytes.Buffer{},
	}
	if err := runner.Run(context.Background(), []string{"help"}); err != nil {
		t.Fatal(err)
	}
	body := stdout.String()
	for _, want := range []string{
		"Fascinate CLI",
		"Quick start",
		"fascinate login --email you@example.com",
		"fascinate help --json [topic]",
		"https://fascinate.dev by default",
		"Agent notes",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected help output to contain %q, got %q", want, body)
		}
	}
}

func TestHelpJSONSupportsTopicSelection(t *testing.T) {
	stdout := &bytes.Buffer{}
	runner := Runner{
		Stdin:  bytes.NewBuffer(nil),
		Stdout: stdout,
		Stderr: &bytes.Buffer{},
	}
	if err := runner.Run(context.Background(), []string{"help", "agents", "--json"}); err != nil {
		t.Fatal(err)
	}
	body := stdout.String()
	for _, want := range []string{
		`"topic": "agents"`,
		`"title": "Fascinate CLI For Agents"`,
		`"path": "exec"`,
		`"path": "shell create"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected help json to contain %q, got %q", want, body)
		}
	}
}

func TestDashHelpAlias(t *testing.T) {
	stdout := &bytes.Buffer{}
	runner := Runner{
		Stdin:  bytes.NewBuffer(nil),
		Stdout: stdout,
		Stderr: &bytes.Buffer{},
	}
	if err := runner.Run(context.Background(), []string{"--help"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "Fascinate CLI") {
		t.Fatalf("unexpected stdout %q", stdout.String())
	}
}

func TestShellListJSON(t *testing.T) {
	t.Setenv(envCLIConfigPath, t.TempDir()+"/config.json")
	t.Setenv(envBaseURL, "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/shells" || r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("unexpected request %s %s auth=%q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"shells": []map[string]any{
				{"id": "shell-1", "name": "primary", "machine_name": "m-1", "state": "READY"},
			},
		})
	}))
	defer server.Close()

	t.Setenv(envBaseURL, server.URL)
	t.Setenv(envToken, "test-token")
	stdout := &bytes.Buffer{}
	runner := Runner{
		Stdin:  bytes.NewBuffer(nil),
		Stdout: stdout,
		Stderr: &bytes.Buffer{},
	}
	if err := runner.Run(context.Background(), []string{"shell", "list", "--json"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"shells": [`) {
		t.Fatalf("expected wrapped shells output, got %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), `"id": "shell-1"`) {
		t.Fatalf("unexpected stdout %q", stdout.String())
	}
}

func TestShellLinesAcceptsTrailingJSONFlag(t *testing.T) {
	t.Setenv(envCLIConfigPath, t.TempDir()+"/config.json")
	t.Setenv(envToken, "test-token")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/shells/shell-1/lines" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"lines": []string{"hello", "world"},
		})
	}))
	defer server.Close()

	t.Setenv(envBaseURL, server.URL)
	stdout := &bytes.Buffer{}
	runner := Runner{
		Stdin:  bytes.NewBuffer(nil),
		Stdout: stdout,
		Stderr: &bytes.Buffer{},
	}
	if err := runner.Run(context.Background(), []string{"shell", "lines", "shell-1", "--json"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"lines": [`) || !strings.Contains(stdout.String(), `"hello"`) {
		t.Fatalf("unexpected stdout %q", stdout.String())
	}
}

func TestShellSendAppendsNewline(t *testing.T) {
	t.Setenv(envCLIConfigPath, t.TempDir()+"/config.json")
	t.Setenv(envToken, "test-token")

	var captured struct {
		Input string `json:"input"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/shells/shell-1/input" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	t.Setenv(envBaseURL, server.URL)
	stdout := &bytes.Buffer{}
	runner := Runner{
		Stdin:  bytes.NewBuffer(nil),
		Stdout: stdout,
		Stderr: &bytes.Buffer{},
	}
	if err := runner.Run(context.Background(), []string{"shell", "send", "shell-1", "pwd"}); err != nil {
		t.Fatal(err)
	}
	if captured.Input != "pwd\n" {
		t.Fatalf("expected newline-appended input, got %q", captured.Input)
	}
	if !strings.Contains(stdout.String(), "Sent input to shell shell-1") {
		t.Fatalf("unexpected stdout %q", stdout.String())
	}
}

func TestMachineListJSON(t *testing.T) {
	t.Setenv(envCLIConfigPath, t.TempDir()+"/config.json")
	t.Setenv(envToken, "test-token")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/machines" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"machines": []map[string]any{
				{"id": "machine-1", "name": "m-1", "state": "RUNNING"},
			},
		})
	}))
	defer server.Close()

	t.Setenv(envBaseURL, server.URL)
	stdout := &bytes.Buffer{}
	runner := Runner{
		Stdin:  bytes.NewBuffer(nil),
		Stdout: stdout,
		Stderr: &bytes.Buffer{},
	}
	if err := runner.Run(context.Background(), []string{"machine", "list", "--json"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"machines": [`) {
		t.Fatalf("expected wrapped machine output, got %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), `"name": "m-1"`) {
		t.Fatalf("unexpected stdout %q", stdout.String())
	}
}

func TestMachineGetAcceptsTrailingJSONFlag(t *testing.T) {
	t.Setenv(envCLIConfigPath, t.TempDir()+"/config.json")
	t.Setenv(envToken, "test-token")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/machines/hello" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":    "machine-hello",
			"name":  "hello",
			"state": "RUNNING",
		})
	}))
	defer server.Close()

	t.Setenv(envBaseURL, server.URL)
	stdout := &bytes.Buffer{}
	runner := Runner{
		Stdin:  bytes.NewBuffer(nil),
		Stdout: stdout,
		Stderr: &bytes.Buffer{},
	}
	if err := runner.Run(context.Background(), []string{"machine", "get", "hello", "--json"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"name": "hello"`) {
		t.Fatalf("unexpected stdout %q", stdout.String())
	}
}

func TestExecJSONReturnsExitCodeAndStructuredResult(t *testing.T) {
	t.Setenv(envCLIConfigPath, t.TempDir()+"/config.json")
	t.Setenv(envToken, "test-token")

	getCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/execs":
			if r.Method != http.MethodPost {
				t.Fatalf("unexpected method %s", r.Method)
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if got := body["command_text"]; got != "false" {
				t.Fatalf("expected command_text false, got %#v", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"exec": map[string]any{
					"id":           "exec-1",
					"machine_name": "m-1",
					"state":        "RUNNING",
				},
				"stream_url": "/v1/execs/exec-1/stream",
				"cancel_url": "/v1/execs/exec-1/cancel",
			})
		case "/v1/execs/exec-1":
			getCount++
			state := "RUNNING"
			exitCode := any(nil)
			if getCount >= 2 {
				state = "FAILED"
				exitCode = 7
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":           "exec-1",
				"machine_name": "m-1",
				"state":        state,
				"exit_code":    exitCode,
				"stdout_text":  "hello\n",
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	t.Setenv(envBaseURL, server.URL)
	stdout := &bytes.Buffer{}
	runner := Runner{
		Stdin:  bytes.NewBuffer(nil),
		Stdout: stdout,
		Stderr: &bytes.Buffer{},
	}
	err := runner.Run(context.Background(), []string{"exec", "--json", "m-1", "--", "false"})
	var exitErr ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 7 {
		t.Fatalf("expected exit error 7, got %v", err)
	}
	if !strings.Contains(stdout.String(), `"state": "FAILED"`) {
		t.Fatalf("expected structured exec result, got %q", stdout.String())
	}
}

func TestDiagnosticsMachineAcceptsTrailingJSONFlag(t *testing.T) {
	t.Setenv(envCLIConfigPath, t.TempDir()+"/config.json")
	t.Setenv(envToken, "test-token")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/diagnostics/machines/m-1" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"machine": map[string]any{
				"id":    "machine-1",
				"name":  "m-1",
				"state": "RUNNING",
			},
			"recent_events": []map[string]any{},
		})
	}))
	defer server.Close()

	t.Setenv(envBaseURL, server.URL)
	stdout := &bytes.Buffer{}
	runner := Runner{
		Stdin:  bytes.NewBuffer(nil),
		Stdout: stdout,
		Stderr: &bytes.Buffer{},
	}
	if err := runner.Run(context.Background(), []string{"diagnostics", "machine", "m-1", "--json"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"name": "m-1"`) {
		t.Fatalf("unexpected stdout %q", stdout.String())
	}
}

func TestSnapshotRestoreAcceptsTrailingJSONFlag(t *testing.T) {
	t.Setenv(envCLIConfigPath, t.TempDir()+"/config.json")
	t.Setenv(envToken, "test-token")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/machines" || r.Method != http.MethodPost {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["name"] != "restored" || body["snapshot_name"] != "snap-1" {
			t.Fatalf("unexpected body %#v", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":    "machine-1",
			"name":  "restored",
			"state": "CREATING",
		})
	}))
	defer server.Close()

	t.Setenv(envBaseURL, server.URL)
	stdout := &bytes.Buffer{}
	runner := Runner{
		Stdin:  bytes.NewBuffer(nil),
		Stdout: stdout,
		Stderr: &bytes.Buffer{},
	}
	if err := runner.Run(context.Background(), []string{"snapshot", "restore", "snap-1", "restored", "--json"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"name": "restored"`) {
		t.Fatalf("unexpected stdout %q", stdout.String())
	}
}

func TestResolveWebSocketURL(t *testing.T) {
	got, err := resolveWebSocketURL("https://fascinate.dev", "/v1/terminal/sessions/shell-1/stream?token=abc")
	if err != nil {
		t.Fatal(err)
	}
	if got != "wss://fascinate.dev/v1/terminal/sessions/shell-1/stream?token=abc" {
		t.Fatalf("unexpected websocket URL %q", got)
	}
}
