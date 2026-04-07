package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"fascinate/internal/browserterm"
)

type ExitError struct {
	Code    int
	Message string
}

func (e ExitError) Error() string {
	message := strings.TrimSpace(e.Message)
	if message != "" {
		return message
	}
	if e.Code > 0 {
		return fmt.Sprintf("command exited with status %d", e.Code)
	}
	return "command failed"
}

func (e ExitError) ExitCode() int {
	if e.Code > 0 {
		return e.Code
	}
	return 1
}

func (r Runner) runMachine(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: fascinate machine <list|get|create|fork|delete|env>")
	}
	switch args[0] {
	case "list":
		return r.runMachineList(ctx, args[1:])
	case "get":
		return r.runMachineGet(ctx, args[1:])
	case "create":
		return r.runMachineCreate(ctx, args[1:])
	case "fork":
		return r.runMachineFork(ctx, args[1:])
	case "delete":
		return r.runMachineDelete(ctx, args[1:])
	case "env":
		return r.runMachineEnv(ctx, args[1:])
	default:
		return fmt.Errorf("unknown machine command %q", args[0])
	}
}

func (r Runner) runSnapshot(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: fascinate snapshot <list|create|restore|delete>")
	}
	switch args[0] {
	case "list":
		return r.runSnapshotList(ctx, args[1:])
	case "create":
		return r.runSnapshotCreate(ctx, args[1:])
	case "restore":
		return r.runSnapshotRestore(ctx, args[1:])
	case "delete":
		return r.runSnapshotDelete(ctx, args[1:])
	default:
		return fmt.Errorf("unknown snapshot command %q", args[0])
	}
}

func (r Runner) runEnv(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: fascinate env <list|set|unset>")
	}
	switch args[0] {
	case "list":
		return r.runEnvList(ctx, args[1:])
	case "set":
		return r.runEnvSet(ctx, args[1:])
	case "unset":
		return r.runEnvUnset(ctx, args[1:])
	default:
		return fmt.Errorf("unknown env command %q", args[0])
	}
}

func (r Runner) runDiagnostics(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: fascinate diagnostics <events|hosts|budgets|tool-auth|machine|snapshot|shells|execs>")
	}
	switch args[0] {
	case "events":
		return r.runDiagnosticsEvents(ctx, args[1:])
	case "hosts":
		return r.runDiagnosticsHosts(ctx, args[1:])
	case "budgets":
		return r.runDiagnosticsBudgets(ctx, args[1:])
	case "tool-auth":
		return r.runDiagnosticsToolAuth(ctx, args[1:])
	case "machine":
		return r.runDiagnosticsMachine(ctx, args[1:])
	case "snapshot":
		return r.runDiagnosticsSnapshot(ctx, args[1:])
	case "shells":
		return r.runDiagnosticsShells(ctx, args[1:])
	case "execs":
		return r.runDiagnosticsExecs(ctx, args[1:])
	default:
		return fmt.Errorf("unknown diagnostics command %q", args[0])
	}
}

func (r Runner) runExec(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("exec", flag.ContinueOnError)
	flags.SetOutput(r.Stderr)
	baseURL := flags.String("base-url", "", "Fascinate API base URL")
	cwd := flags.String("cwd", "", "working directory inside the machine")
	timeoutText := flags.String("timeout", "", "maximum runtime (for example 30s or 120)")
	jsonOutput := flags.Bool("json", false, "print the final structured result as JSON")
	jsonLines := flags.Bool("jsonl", false, "stream execution events as JSON lines")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *jsonOutput && *jsonLines {
		return fmt.Errorf("--json and --jsonl cannot be combined")
	}
	if flags.NArg() < 2 {
		return fmt.Errorf("usage: fascinate exec [--cwd <path>] [--timeout <duration>] [--json|--jsonl] <machine> -- <command...>")
	}
	timeout, err := parseTimeout(strings.TrimSpace(*timeoutText))
	if err != nil {
		return err
	}

	client, err := r.authedClient(*baseURL)
	if err != nil {
		return err
	}
	machineName := flags.Arg(0)
	commandText := strings.TrimSpace(strings.Join(flags.Args()[1:], " "))
	if commandText == "" {
		return fmt.Errorf("command is required")
	}
	createCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	launch, err := client.CreateExec(createCtx, machineName, commandText, *cwd, timeout)
	if err != nil {
		return err
	}

	switch {
	case *jsonOutput:
		result, err := r.waitForExecResult(ctx, client, launch.Exec.ID)
		if err != nil {
			return err
		}
		if err := writeJSON(r.Stdout, result); err != nil {
			return err
		}
		return execResultError(result)
	case *jsonLines:
		result, err := r.streamExecJSONL(ctx, client, launch.Exec.ID)
		if err != nil {
			return err
		}
		return execResultError(result)
	default:
		result, err := r.streamExecHuman(ctx, client, launch.Exec.ID)
		if err != nil {
			return err
		}
		return execResultError(result)
	}
}

func (r Runner) runMachineList(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("machine list", flag.ContinueOnError)
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
	machines, err := client.ListMachines(ctx)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeJSON(r.Stdout, machines)
	}
	for _, machine := range machines {
		if _, err := fmt.Fprintf(r.Stdout, "%s\t%s\t%s\t%s\n", machine.Name, machine.State, machine.HostID, machine.URL); err != nil {
			return err
		}
	}
	return nil
}

func (r Runner) runMachineGet(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("machine get", flag.ContinueOnError)
	flags.SetOutput(r.Stderr)
	baseURL := flags.String("base-url", "", "Fascinate API base URL")
	jsonOutput := flags.Bool("json", false, "print JSON to stdout")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: fascinate machine get <name>")
	}
	client, err := r.authedClient(*baseURL)
	if err != nil {
		return err
	}
	machine, err := client.GetMachine(ctx, flags.Arg(0))
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeJSON(r.Stdout, machine)
	}
	_, err = fmt.Fprintf(r.Stdout, "%s\t%s\t%s\t%s\n", machine.Name, machine.State, machine.HostID, machine.URL)
	return err
}

func (r Runner) runMachineCreate(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("machine create", flag.ContinueOnError)
	flags.SetOutput(r.Stderr)
	baseURL := flags.String("base-url", "", "Fascinate API base URL")
	snapshot := flags.String("snapshot", "", "restore from a snapshot")
	jsonOutput := flags.Bool("json", false, "print JSON to stdout")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: fascinate machine create [--snapshot <snapshot>] <name>")
	}
	client, err := r.authedClient(*baseURL)
	if err != nil {
		return err
	}
	machine, err := client.CreateMachine(ctx, flags.Arg(0), *snapshot)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeJSON(r.Stdout, machine)
	}
	_, err = fmt.Fprintf(r.Stdout, "Queued machine %s (%s)\n", machine.Name, machine.State)
	return err
}

func (r Runner) runMachineFork(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("machine fork", flag.ContinueOnError)
	flags.SetOutput(r.Stderr)
	baseURL := flags.String("base-url", "", "Fascinate API base URL")
	jsonOutput := flags.Bool("json", false, "print JSON to stdout")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 2 {
		return fmt.Errorf("usage: fascinate machine fork <source> <target>")
	}
	client, err := r.authedClient(*baseURL)
	if err != nil {
		return err
	}
	machine, err := client.ForkMachine(ctx, flags.Arg(0), flags.Arg(1))
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeJSON(r.Stdout, machine)
	}
	_, err = fmt.Fprintf(r.Stdout, "Queued fork %s from %s (%s)\n", machine.Name, flags.Arg(0), machine.State)
	return err
}

func (r Runner) runMachineDelete(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("machine delete", flag.ContinueOnError)
	flags.SetOutput(r.Stderr)
	baseURL := flags.String("base-url", "", "Fascinate API base URL")
	yes := flags.Bool("yes", false, "skip the confirmation prompt")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: fascinate machine delete [--yes] <name>")
	}
	if err := r.confirmAction(*yes, fmt.Sprintf("Delete machine %s", flags.Arg(0))); err != nil {
		return err
	}
	client, err := r.authedClient(*baseURL)
	if err != nil {
		return err
	}
	if err := client.DeleteMachine(ctx, flags.Arg(0)); err != nil {
		return err
	}
	_, err = fmt.Fprintf(r.Stdout, "Deleted machine %s\n", flags.Arg(0))
	return err
}

func (r Runner) runMachineEnv(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("machine env", flag.ContinueOnError)
	flags.SetOutput(r.Stderr)
	baseURL := flags.String("base-url", "", "Fascinate API base URL")
	jsonOutput := flags.Bool("json", false, "print JSON to stdout")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: fascinate machine env <name>")
	}
	client, err := r.authedClient(*baseURL)
	if err != nil {
		return err
	}
	env, err := client.GetMachineEnv(ctx, flags.Arg(0))
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeJSON(r.Stdout, env)
	}
	for _, entry := range env.Entries {
		if _, err := fmt.Fprintf(r.Stdout, "%s=%s\n", entry.Key, entry.Value); err != nil {
			return err
		}
	}
	return nil
}

func (r Runner) runSnapshotList(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("snapshot list", flag.ContinueOnError)
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
	snapshots, err := client.ListSnapshots(ctx)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeJSON(r.Stdout, snapshots)
	}
	for _, snapshot := range snapshots {
		if _, err := fmt.Fprintf(r.Stdout, "%s\t%s\t%s\n", snapshot.Name, snapshot.State, snapshot.SourceMachineName); err != nil {
			return err
		}
	}
	return nil
}

func (r Runner) runSnapshotCreate(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("snapshot create", flag.ContinueOnError)
	flags.SetOutput(r.Stderr)
	baseURL := flags.String("base-url", "", "Fascinate API base URL")
	jsonOutput := flags.Bool("json", false, "print JSON to stdout")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 2 {
		return fmt.Errorf("usage: fascinate snapshot create <machine> <snapshot>")
	}
	client, err := r.authedClient(*baseURL)
	if err != nil {
		return err
	}
	snapshot, err := client.CreateSnapshot(ctx, flags.Arg(0), flags.Arg(1))
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeJSON(r.Stdout, snapshot)
	}
	_, err = fmt.Fprintf(r.Stdout, "Queued snapshot %s from %s (%s)\n", snapshot.Name, flags.Arg(0), snapshot.State)
	return err
}

func (r Runner) runSnapshotRestore(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("snapshot restore", flag.ContinueOnError)
	flags.SetOutput(r.Stderr)
	baseURL := flags.String("base-url", "", "Fascinate API base URL")
	jsonOutput := flags.Bool("json", false, "print JSON to stdout")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 2 {
		return fmt.Errorf("usage: fascinate snapshot restore <snapshot> <machine>")
	}
	client, err := r.authedClient(*baseURL)
	if err != nil {
		return err
	}
	machine, err := client.RestoreSnapshot(ctx, flags.Arg(0), flags.Arg(1))
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeJSON(r.Stdout, machine)
	}
	_, err = fmt.Fprintf(r.Stdout, "Queued machine %s from snapshot %s (%s)\n", machine.Name, flags.Arg(0), machine.State)
	return err
}

func (r Runner) runSnapshotDelete(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("snapshot delete", flag.ContinueOnError)
	flags.SetOutput(r.Stderr)
	baseURL := flags.String("base-url", "", "Fascinate API base URL")
	yes := flags.Bool("yes", false, "skip the confirmation prompt")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: fascinate snapshot delete [--yes] <name>")
	}
	if err := r.confirmAction(*yes, fmt.Sprintf("Delete snapshot %s", flags.Arg(0))); err != nil {
		return err
	}
	client, err := r.authedClient(*baseURL)
	if err != nil {
		return err
	}
	if err := client.DeleteSnapshot(ctx, flags.Arg(0)); err != nil {
		return err
	}
	_, err = fmt.Fprintf(r.Stdout, "Deleted snapshot %s\n", flags.Arg(0))
	return err
}

func (r Runner) runEnvList(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("env list", flag.ContinueOnError)
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
	envVars, builtinEnvVars, err := client.ListEnvVars(ctx)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeJSON(r.Stdout, map[string]any{
			"env_vars":         envVars,
			"builtin_env_vars": builtinEnvVars,
		})
	}
	for _, envVar := range envVars {
		if _, err := fmt.Fprintf(r.Stdout, "%s=%s\n", envVar.Key, envVar.Value); err != nil {
			return err
		}
	}
	return nil
}

func (r Runner) runEnvSet(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("env set", flag.ContinueOnError)
	flags.SetOutput(r.Stderr)
	baseURL := flags.String("base-url", "", "Fascinate API base URL")
	jsonOutput := flags.Bool("json", false, "print JSON to stdout")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() < 2 {
		return fmt.Errorf("usage: fascinate env set <KEY> <VALUE>")
	}
	client, err := r.authedClient(*baseURL)
	if err != nil {
		return err
	}
	envVar, err := client.SetEnvVar(ctx, flags.Arg(0), strings.Join(flags.Args()[1:], " "))
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeJSON(r.Stdout, envVar)
	}
	_, err = fmt.Fprintf(r.Stdout, "Set %s\n", envVar.Key)
	return err
}

func (r Runner) runEnvUnset(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("env unset", flag.ContinueOnError)
	flags.SetOutput(r.Stderr)
	baseURL := flags.String("base-url", "", "Fascinate API base URL")
	yes := flags.Bool("yes", false, "skip the confirmation prompt")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: fascinate env unset [--yes] <KEY>")
	}
	if err := r.confirmAction(*yes, fmt.Sprintf("Unset env var %s", flags.Arg(0))); err != nil {
		return err
	}
	client, err := r.authedClient(*baseURL)
	if err != nil {
		return err
	}
	if err := client.DeleteEnvVar(ctx, flags.Arg(0)); err != nil {
		return err
	}
	_, err = fmt.Fprintf(r.Stdout, "Unset %s\n", flags.Arg(0))
	return err
}

func (r Runner) runDiagnosticsEvents(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("diagnostics events", flag.ContinueOnError)
	flags.SetOutput(r.Stderr)
	baseURL := flags.String("base-url", "", "Fascinate API base URL")
	limit := flags.Int("limit", 25, "maximum number of events to return")
	jsonOutput := flags.Bool("json", false, "print JSON to stdout")
	if err := flags.Parse(args); err != nil {
		return err
	}
	client, err := r.authedClient(*baseURL)
	if err != nil {
		return err
	}
	events, stream, err := client.DiagnosticsEvents(ctx, *limit)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeJSON(r.Stdout, map[string]any{"events": events, "stream": stream})
	}
	if _, err := fmt.Fprintf(r.Stdout, "subscribers=%d latest=%s %s\n", stream.ActiveSubscribers, stream.LatestEventID, stream.LatestEventKind); err != nil {
		return err
	}
	for _, event := range events {
		if _, err := fmt.Fprintf(r.Stdout, "%s\t%s\n", event.CreatedAt, event.Kind); err != nil {
			return err
		}
	}
	return nil
}

func (r Runner) runDiagnosticsHosts(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("diagnostics hosts", flag.ContinueOnError)
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
	hosts, err := client.DiagnosticsHosts(ctx)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeJSON(r.Stdout, hosts)
	}
	for _, host := range hosts {
		if _, err := fmt.Fprintf(r.Stdout, "%s\t%s\t%s\t%d machines\n", host.ID, host.Status, host.Region, host.MachineCount); err != nil {
			return err
		}
	}
	return nil
}

func (r Runner) runDiagnosticsBudgets(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("diagnostics budgets", flag.ContinueOnError)
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
	diag, err := client.DiagnosticsBudgets(ctx)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeJSON(r.Stdout, diag)
	}
	_, err = fmt.Fprintf(r.Stdout, "machines=%d/%d snapshots=%d/%d cpu=%s/%s\n", diag.Usage.MachineCount, diag.Limits.MachineCount, diag.Usage.SnapshotCount, diag.Limits.SnapshotCount, diag.Usage.CPU, diag.Limits.CPU)
	return err
}

func (r Runner) runDiagnosticsToolAuth(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("diagnostics tool-auth", flag.ContinueOnError)
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
	diag, err := client.DiagnosticsToolAuth(ctx)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeJSON(r.Stdout, diag)
	}
	_, err = fmt.Fprintf(r.Stdout, "profiles=%d events=%d\n", len(diag.Profiles), len(diag.Events))
	return err
}

func (r Runner) runDiagnosticsMachine(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("diagnostics machine", flag.ContinueOnError)
	flags.SetOutput(r.Stderr)
	baseURL := flags.String("base-url", "", "Fascinate API base URL")
	jsonOutput := flags.Bool("json", false, "print JSON to stdout")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: fascinate diagnostics machine <name>")
	}
	client, err := r.authedClient(*baseURL)
	if err != nil {
		return err
	}
	diag, err := client.DiagnosticsMachine(ctx, flags.Arg(0))
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeJSON(r.Stdout, diag)
	}
	_, err = fmt.Fprintf(r.Stdout, "%s\t%s\t%s events=%d\n", diag.Machine.Name, diag.Machine.State, diag.Machine.HostID, len(diag.RecentEvents))
	return err
}

func (r Runner) runDiagnosticsSnapshot(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("diagnostics snapshot", flag.ContinueOnError)
	flags.SetOutput(r.Stderr)
	baseURL := flags.String("base-url", "", "Fascinate API base URL")
	jsonOutput := flags.Bool("json", false, "print JSON to stdout")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: fascinate diagnostics snapshot <name>")
	}
	client, err := r.authedClient(*baseURL)
	if err != nil {
		return err
	}
	diag, err := client.DiagnosticsSnapshot(ctx, flags.Arg(0))
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeJSON(r.Stdout, diag)
	}
	_, err = fmt.Fprintf(r.Stdout, "%s\t%s\t%s events=%d\n", diag.Snapshot.Name, diag.Snapshot.State, diag.Snapshot.HostID, len(diag.RecentEvents))
	return err
}

func (r Runner) runDiagnosticsShells(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("diagnostics shells", flag.ContinueOnError)
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
	diag, err := client.DiagnosticsShells(ctx)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeJSON(r.Stdout, diag)
	}
	_, err = fmt.Fprintf(r.Stdout, "active=%d created=%d attach_failures=%d disconnects=%d\n", diag.ActiveSessions, diag.TotalCreated, diag.TotalAttachFailures, diag.TotalDisconnects)
	return err
}

func (r Runner) runDiagnosticsExecs(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("diagnostics execs", flag.ContinueOnError)
	flags.SetOutput(r.Stderr)
	baseURL := flags.String("base-url", "", "Fascinate API base URL")
	limit := flags.Int("limit", 25, "maximum number of execs to return")
	jsonOutput := flags.Bool("json", false, "print JSON to stdout")
	if err := flags.Parse(args); err != nil {
		return err
	}
	client, err := r.authedClient(*baseURL)
	if err != nil {
		return err
	}
	diag, err := client.DiagnosticsExecs(ctx, *limit)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeJSON(r.Stdout, diag)
	}
	_, err = fmt.Fprintf(r.Stdout, "active=%d recent=%d\n", diag.Active, len(diag.Execs))
	return err
}

func (r Runner) streamExecHuman(ctx context.Context, client Client, execID string) (browserterm.Exec, error) {
	var result browserterm.Exec
	streamCtx, streamCancel := context.WithCancel(context.Background())
	defer streamCancel()
	r.watchExecInterrupt(ctx, client, execID)
	err := client.StreamExec(streamCtx, execID, func(event browserterm.ExecStreamEvent) error {
		switch event.Type {
		case "stdout":
			_, err := io.WriteString(r.Stdout, event.Data)
			return err
		case "stderr":
			_, err := io.WriteString(r.Stderr, event.Data)
			return err
		case "result":
			if event.Exec != nil {
				result = *event.Exec
			}
		}
		return nil
	})
	if err != nil {
		return result, err
	}
	if strings.TrimSpace(result.ID) == "" {
		return r.waitForExecResult(context.Background(), client, execID)
	}
	return result, nil
}

func (r Runner) streamExecJSONL(ctx context.Context, client Client, execID string) (browserterm.Exec, error) {
	var result browserterm.Exec
	streamCtx, streamCancel := context.WithCancel(context.Background())
	defer streamCancel()
	r.watchExecInterrupt(ctx, client, execID)
	err := client.StreamExec(streamCtx, execID, func(event browserterm.ExecStreamEvent) error {
		if err := writeJSONLine(r.Stdout, event); err != nil {
			return err
		}
		if event.Type == "result" && event.Exec != nil {
			result = *event.Exec
		}
		return nil
	})
	if err != nil {
		return result, err
	}
	if strings.TrimSpace(result.ID) == "" {
		return r.waitForExecResult(context.Background(), client, execID)
	}
	return result, nil
}

func (r Runner) waitForExecResult(ctx context.Context, client Client, execID string) (browserterm.Exec, error) {
	r.watchExecInterrupt(ctx, client, execID)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	cancelDeadline := time.Time{}
	for {
		pollCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		execResult, err := client.GetExec(pollCtx, execID)
		cancel()
		if err != nil {
			return browserterm.Exec{}, err
		}
		if execTerminal(execResult.State) {
			return execResult, nil
		}
		if ctx.Err() != nil && cancelDeadline.IsZero() {
			cancelDeadline = time.Now().Add(15 * time.Second)
		}
		if !cancelDeadline.IsZero() && time.Now().After(cancelDeadline) {
			return browserterm.Exec{}, ExitError{Code: 130, Message: "command cancellation timed out"}
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
		}
	}
}

func (r Runner) watchExecInterrupt(ctx context.Context, client Client, execID string) {
	if ctx == nil {
		return
	}
	go func() {
		<-ctx.Done()
		cancelCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = client.CancelExec(cancelCtx, execID)
	}()
}

func execTerminal(state string) bool {
	switch strings.ToUpper(strings.TrimSpace(state)) {
	case "SUCCEEDED", "FAILED", "TIMED_OUT", "CANCELLED", "ERROR":
		return true
	default:
		return false
	}
}

func execResultError(result browserterm.Exec) error {
	switch strings.ToUpper(strings.TrimSpace(result.State)) {
	case "SUCCEEDED":
		return nil
	case "FAILED":
		if result.ExitCode != nil {
			return ExitError{Code: *result.ExitCode, Message: fmt.Sprintf("command exited with status %d", *result.ExitCode)}
		}
		return ExitError{Code: 1, Message: "command exited non-zero"}
	case "TIMED_OUT":
		return ExitError{Code: 124, Message: "command timed out"}
	case "CANCELLED":
		return ExitError{Code: 130, Message: "command cancelled"}
	default:
		return ExitError{Code: 1, Message: firstNonEmpty(strings.TrimSpace(result.StderrText), "command failed")}
	}
}

func parseTimeout(value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	if strings.IndexFunc(value, func(r rune) bool { return r < '0' || r > '9' }) == -1 {
		value += "s"
	}
	return time.ParseDuration(value)
}

func writeJSONLine(w io.Writer, value any) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	body = append(body, '\n')
	_, err = w.Write(body)
	return err
}
