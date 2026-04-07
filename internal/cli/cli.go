package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/coder/websocket"
)

type Runner struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

func Run(ctx context.Context, args []string) error {
	runner := Runner{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
	return runner.Run(ctx, args)
}

func (r Runner) Run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return r.runHelp(nil)
	}
	switch args[0] {
	case "-h", "--help":
		return r.runHelp(args[1:])
	case "diagnostics":
		return r.runDiagnostics(ctx, args[1:])
	case "env":
		return r.runEnv(ctx, args[1:])
	case "exec":
		return r.runExec(ctx, args[1:])
	case "login":
		return r.runLogin(ctx, args[1:])
	case "logout":
		return r.runLogout(ctx, args[1:])
	case "machine":
		return r.runMachine(ctx, args[1:])
	case "shell":
		return r.runShell(ctx, args[1:])
	case "snapshot":
		return r.runSnapshot(ctx, args[1:])
	case "whoami":
		return r.runWhoAmI(ctx, args[1:])
	case "help":
		return r.runHelp(args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func (r Runner) runLogin(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("login", flag.ContinueOnError)
	flags.SetOutput(r.Stderr)
	baseURL := flags.String("base-url", "", "Fascinate API base URL")
	email := flags.String("email", "", "email address for login")
	code := flags.String("code", "", "verification code from email")
	tokenName := flags.String("token-name", "fascinate-cli", "name for the stored API token")
	noStore := flags.Bool("no-store", false, "do not persist the resulting API token")
	if err := flags.Parse(args); err != nil {
		return err
	}

	stored, path, err := LoadConfig()
	if err != nil {
		return err
	}
	client := Client{
		BaseURL: firstNonEmpty(*baseURL, ResolveBaseURL(stored)),
	}

	selectedEmail := strings.TrimSpace(*email)
	if selectedEmail == "" {
		selectedEmail = strings.TrimSpace(stored.Email)
	}
	if selectedEmail == "" {
		if !isInteractive(r.Stdin, r.Stdout) {
			return fmt.Errorf("email is required when stdin/stdout are not interactive")
		}
		value, err := r.prompt("Email: ")
		if err != nil {
			return err
		}
		selectedEmail = value
	}
	if err := client.RequestLoginCode(ctx, selectedEmail); err != nil {
		return err
	}

	selectedCode := strings.TrimSpace(*code)
	if selectedCode == "" {
		if !isInteractive(r.Stdin, r.Stdout) {
			return fmt.Errorf("verification code is required when stdin/stdout are not interactive")
		}
		value, err := r.prompt("Verification code: ")
		if err != nil {
			return err
		}
		selectedCode = value
	}
	verify, err := client.VerifyLoginCode(ctx, selectedEmail, selectedCode, *tokenName)
	if err != nil {
		return err
	}

	if !*noStore {
		if err := SaveConfig(path, Config{
			BaseURL: client.BaseURL,
			Token:   verify.Token,
			Email:   selectedEmail,
		}); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintf(r.Stdout, "Logged in as %s\n", verify.User.Email); err != nil {
		return err
	}
	if *noStore {
		_, err = fmt.Fprintln(r.Stdout, "API token was issued but not stored.")
		return err
	}
	_, err = fmt.Fprintf(r.Stdout, "Saved CLI credentials to %s\n", path)
	return err
}

func (r Runner) runLogout(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("logout", flag.ContinueOnError)
	flags.SetOutput(r.Stderr)
	baseURL := flags.String("base-url", "", "Fascinate API base URL")
	if err := flags.Parse(args); err != nil {
		return err
	}

	stored, path, err := LoadConfig()
	if err != nil {
		return err
	}
	client := Client{
		BaseURL: firstNonEmpty(*baseURL, ResolveBaseURL(stored)),
		Token:   ResolveToken(stored),
	}
	if strings.TrimSpace(client.Token) != "" {
		_ = client.Logout(ctx)
	}
	stored.Token = ""
	if err := SaveConfig(path, stored); err != nil {
		return err
	}
	_, err = fmt.Fprintln(r.Stdout, "Logged out.")
	return err
}

func (r Runner) runWhoAmI(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("whoami", flag.ContinueOnError)
	flags.SetOutput(r.Stderr)
	baseURL := flags.String("base-url", "", "Fascinate API base URL")
	if err := flags.Parse(args); err != nil {
		return err
	}

	stored, _, err := LoadConfig()
	if err != nil {
		return err
	}
	client := Client{
		BaseURL: firstNonEmpty(*baseURL, ResolveBaseURL(stored)),
		Token:   ResolveToken(stored),
	}
	if strings.TrimSpace(client.Token) == "" {
		return fmt.Errorf("no API token configured; run `fascinate login` first or set FASCINATE_TOKEN")
	}
	user, token, err := client.Session(ctx)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(r.Stdout, "%s (%s, expires %s)\n", user.Email, token.Name, token.ExpiresAt)
	return err
}

func (r Runner) runShell(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: fascinate shell <list|create|attach|delete|send|lines>")
	}
	switch args[0] {
	case "list":
		return r.runShellList(ctx, args[1:])
	case "create":
		return r.runShellCreate(ctx, args[1:])
	case "attach":
		return r.runShellAttach(ctx, args[1:])
	case "delete":
		return r.runShellDelete(ctx, args[1:])
	case "send":
		return r.runShellSend(ctx, args[1:])
	case "lines":
		return r.runShellLines(ctx, args[1:])
	default:
		return fmt.Errorf("unknown shell command %q", args[0])
	}
}

func (r Runner) runShellList(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("shell list", flag.ContinueOnError)
	flags.SetOutput(r.Stderr)
	baseURL := flags.String("base-url", "", "Fascinate API base URL")
	jsonOutput := flags.Bool("json", false, "print JSON to stdout")
	if err := flags.Parse(args); err != nil {
		return err
	}
	client, err := r.authedClient(*baseURL)
	if err != nil {
		return err
	}
	shells, err := client.ListShells(ctx)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeJSON(r.Stdout, map[string]any{"shells": shells})
	}
	for _, shell := range shells {
		if _, err := fmt.Fprintf(r.Stdout, "%s\t%s\t%s\t%s\n", shell.ID, shell.MachineName, shell.State, shell.Name); err != nil {
			return err
		}
	}
	return nil
}

func (r Runner) runShellCreate(ctx context.Context, args []string) error {
	normalizedArgs, err := reorderKnownFlags(args, map[string]bool{
		"json": true,
	}, map[string]bool{
		"base-url": true,
		"name":     true,
	})
	if err != nil {
		return err
	}
	flags := flag.NewFlagSet("shell create", flag.ContinueOnError)
	flags.SetOutput(r.Stderr)
	baseURL := flags.String("base-url", "", "Fascinate API base URL")
	name := flags.String("name", "", "shell name")
	jsonOutput := flags.Bool("json", false, "print JSON to stdout")
	if err := flags.Parse(normalizedArgs); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: fascinate shell create [--name <name>] <machine>")
	}
	client, err := r.authedClient(*baseURL)
	if err != nil {
		return err
	}
	shell, err := client.CreateShell(ctx, flags.Arg(0), *name)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeJSON(r.Stdout, shell)
	}
	_, err = fmt.Fprintf(r.Stdout, "Created shell %s on %s\n", shell.ID, shell.MachineName)
	return err
}

func (r Runner) runShellDelete(ctx context.Context, args []string) error {
	normalizedArgs, err := reorderKnownFlags(args, map[string]bool{
		"yes": true,
	}, map[string]bool{
		"base-url": true,
	})
	if err != nil {
		return err
	}
	flags := flag.NewFlagSet("shell delete", flag.ContinueOnError)
	flags.SetOutput(r.Stderr)
	baseURL := flags.String("base-url", "", "Fascinate API base URL")
	yes := flags.Bool("yes", false, "skip the confirmation prompt")
	if err := flags.Parse(normalizedArgs); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: fascinate shell delete [--yes] <shell-id>")
	}
	if err := r.confirmAction(*yes, fmt.Sprintf("Delete shell %s", flags.Arg(0))); err != nil {
		return err
	}
	client, err := r.authedClient(*baseURL)
	if err != nil {
		return err
	}
	if err := client.DeleteShell(ctx, flags.Arg(0)); err != nil {
		return err
	}
	_, err = fmt.Fprintf(r.Stdout, "Deleted shell %s\n", flags.Arg(0))
	return err
}

func (r Runner) runShellAttach(ctx context.Context, args []string) error {
	normalizedArgs, err := reorderKnownFlags(args, nil, map[string]bool{
		"base-url": true,
		"cols":     true,
		"rows":     true,
	})
	if err != nil {
		return err
	}
	flags := flag.NewFlagSet("shell attach", flag.ContinueOnError)
	flags.SetOutput(r.Stderr)
	baseURL := flags.String("base-url", "", "Fascinate API base URL")
	cols := flags.Int("cols", 0, "terminal columns")
	rows := flags.Int("rows", 0, "terminal rows")
	if err := flags.Parse(normalizedArgs); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: fascinate shell attach [--cols N --rows N] <shell-id>")
	}
	client, err := r.authedClient(*baseURL)
	if err != nil {
		return err
	}
	if *cols <= 0 || *rows <= 0 {
		detectedCols, detectedRows := detectTerminalSize(r.Stdin)
		if *cols <= 0 {
			*cols = detectedCols
		}
		if *rows <= 0 {
			*rows = detectedRows
		}
	}
	attach, err := client.CreateShellAttachment(ctx, flags.Arg(0), *cols, *rows)
	if err != nil {
		return err
	}
	return r.streamShellAttachment(ctx, client.BaseURL, attach.AttachURL, *cols, *rows)
}

func (r Runner) runShellSend(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("shell send", flag.ContinueOnError)
	flags.SetOutput(r.Stderr)
	baseURL := flags.String("base-url", "", "Fascinate API base URL")
	raw := flags.Bool("raw", false, "send input exactly as provided")
	readFromStdin := flags.Bool("stdin", false, "read input from stdin")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() < 1 {
		return fmt.Errorf("usage: fascinate shell send [--raw] [--stdin] <shell-id> [text]")
	}
	shellID := flags.Arg(0)
	input := strings.Join(flags.Args()[1:], " ")
	if *readFromStdin {
		payload, err := io.ReadAll(r.Stdin)
		if err != nil {
			return err
		}
		input = string(payload)
	}
	if strings.TrimSpace(input) == "" && !*raw {
		return fmt.Errorf("shell input is required")
	}
	if !*raw && !strings.HasSuffix(input, "\n") {
		input += "\n"
	}
	client, err := r.authedClient(*baseURL)
	if err != nil {
		return err
	}
	if err := client.SendShellInput(ctx, shellID, input); err != nil {
		return err
	}
	_, err = fmt.Fprintf(r.Stdout, "Sent input to shell %s\n", shellID)
	return err
}

func (r Runner) runShellLines(ctx context.Context, args []string) error {
	normalizedArgs, err := reorderKnownFlags(args, map[string]bool{
		"json": true,
	}, map[string]bool{
		"base-url": true,
		"limit":    true,
	})
	if err != nil {
		return err
	}
	flags := flag.NewFlagSet("shell lines", flag.ContinueOnError)
	flags.SetOutput(r.Stderr)
	baseURL := flags.String("base-url", "", "Fascinate API base URL")
	limit := flags.Int("limit", 100, "maximum lines to return")
	jsonOutput := flags.Bool("json", false, "print JSON to stdout")
	if err := flags.Parse(normalizedArgs); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: fascinate shell lines [--limit N] <shell-id>")
	}
	client, err := r.authedClient(*baseURL)
	if err != nil {
		return err
	}
	lines, err := client.ReadShellLines(ctx, flags.Arg(0), *limit)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeJSON(r.Stdout, map[string]any{"lines": lines})
	}
	for _, line := range lines {
		if _, err := fmt.Fprintln(r.Stdout, line); err != nil {
			return err
		}
	}
	return nil
}

func (r Runner) printHelp() error {
	return r.runHelp(nil)
}

func (r Runner) prompt(label string) (string, error) {
	if _, err := fmt.Fprint(r.Stdout, label); err != nil {
		return "", err
	}
	reader := bufio.NewReader(r.Stdin)
	value, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimSpace(value), nil
}

func (r Runner) confirmAction(force bool, message string) error {
	if force {
		return nil
	}
	if !isInteractive(r.Stdin, r.Stdout) {
		return fmt.Errorf("%s requires confirmation; rerun with --yes", strings.TrimSpace(message))
	}
	answer, err := r.prompt(strings.TrimSpace(message) + "? [y/N]: ")
	if err != nil {
		return err
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer != "y" && answer != "yes" {
		return fmt.Errorf("confirmation declined")
	}
	return nil
}

func isInteractive(stdin io.Reader, stdout io.Writer) bool {
	inFile, inOK := stdin.(*os.File)
	outFile, outOK := stdout.(*os.File)
	if !inOK || !outOK {
		return false
	}
	inInfo, err := inFile.Stat()
	if err != nil {
		return false
	}
	outInfo, err := outFile.Stat()
	if err != nil {
		return false
	}
	return (inInfo.Mode()&os.ModeCharDevice) != 0 && (outInfo.Mode()&os.ModeCharDevice) != 0
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (r Runner) authedClient(baseURL string) (Client, error) {
	stored, _, err := LoadConfig()
	if err != nil {
		return Client{}, err
	}
	client := Client{
		BaseURL: firstNonEmpty(baseURL, ResolveBaseURL(stored)),
		Token:   ResolveToken(stored),
	}
	if strings.TrimSpace(client.Token) == "" {
		return Client{}, fmt.Errorf("no API token configured; run `fascinate login` first or set FASCINATE_TOKEN")
	}
	return client, nil
}

func writeJSON(w io.Writer, value any) error {
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	_, err = w.Write(body)
	return err
}

func (r Runner) streamShellAttachment(ctx context.Context, baseURL, attachURL string, cols, rows int) error {
	wsURL, err := resolveWebSocketURL(baseURL, attachURL)
	if err != nil {
		return err
	}
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		return err
	}
	defer conn.CloseNow()

	if cols > 0 && rows > 0 {
		_ = writeShellControl(conn, ctx, map[string]any{
			"type": "resize",
			"cols": cols,
			"rows": rows,
		})
	}

	restore := func() {}
	if isInteractive(r.Stdin, r.Stdout) {
		if file, ok := r.Stdin.(*os.File); ok {
			if cleanup, err := enterRawMode(file); err == nil {
				restore = cleanup
			}
		}
	}
	defer restore()

	errCh := make(chan error, 2)

	go func() {
		buffer := make([]byte, 16*1024)
		for {
			n, readErr := r.Stdin.Read(buffer)
			if n > 0 {
				writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				err := conn.Write(writeCtx, websocket.MessageBinary, append([]byte(nil), buffer[:n]...))
				cancel()
				if err != nil {
					errCh <- err
					return
				}
			}
			if readErr != nil {
				if readErr == io.EOF {
					_ = conn.Close(websocket.StatusNormalClosure, "")
					errCh <- nil
					return
				}
				errCh <- readErr
				return
			}
		}
	}()

	go func() {
		for {
			msgType, payload, err := conn.Read(ctx)
			if err != nil {
				if status := websocket.CloseStatus(err); status == websocket.StatusNormalClosure || status == websocket.StatusGoingAway || status == websocket.StatusNoStatusRcvd {
					errCh <- nil
					return
				}
				errCh <- err
				return
			}
			switch msgType {
			case websocket.MessageBinary:
				if _, err := r.Stdout.Write(payload); err != nil {
					errCh <- err
					return
				}
			case websocket.MessageText:
				var message struct {
					Type  string `json:"type"`
					Error string `json:"error"`
				}
				if err := json.Unmarshal(payload, &message); err != nil {
					continue
				}
				if message.Type == "error" && strings.TrimSpace(message.Error) != "" {
					errCh <- fmt.Errorf("%s", strings.TrimSpace(message.Error))
					return
				}
			}
		}
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

func resolveWebSocketURL(baseURL, attachURL string) (string, error) {
	baseURL = normalizeBaseURL(baseURL)
	if baseURL == "" {
		baseURL = "http://127.0.0.1:8080"
	}
	if strings.HasPrefix(attachURL, "ws://") || strings.HasPrefix(attachURL, "wss://") {
		return attachURL, nil
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	switch base.Scheme {
	case "https":
		base.Scheme = "wss"
	default:
		base.Scheme = "ws"
	}
	relative, err := url.Parse(attachURL)
	if err != nil {
		return "", err
	}
	return base.ResolveReference(relative).String(), nil
}

func writeShellControl(conn *websocket.Conn, ctx context.Context, value any) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return conn.Write(writeCtx, websocket.MessageText, body)
}

func detectTerminalSize(stdin io.Reader) (int, int) {
	defaultCols, defaultRows := 120, 40
	file, ok := stdin.(*os.File)
	if !ok {
		return defaultCols, defaultRows
	}
	cmd := exec.Command("stty", "size")
	cmd.Stdin = file
	output, err := cmd.Output()
	if err != nil {
		return defaultCols, defaultRows
	}
	var rows, cols int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(output)), "%d %d", &rows, &cols); err != nil || cols <= 0 || rows <= 0 {
		return defaultCols, defaultRows
	}
	return cols, rows
}

func enterRawMode(file *os.File) (func(), error) {
	stateCmd := exec.Command("stty", "-g")
	stateCmd.Stdin = file
	output, err := stateCmd.Output()
	if err != nil {
		return func() {}, err
	}
	state := strings.TrimSpace(string(output))
	rawCmd := exec.Command("stty", "raw", "-echo")
	rawCmd.Stdin = file
	if err := rawCmd.Run(); err != nil {
		return func() {}, err
	}
	return func() {
		restoreCmd := exec.Command("stty", state)
		restoreCmd.Stdin = file
		_ = restoreCmd.Run()
	}, nil
}
