package cli

import (
	"flag"
	"fmt"
	"io"
	"strings"
)

type helpDoc struct {
	Topic       string         `json:"topic"`
	Title       string         `json:"title"`
	Summary     string         `json:"summary"`
	Install     string         `json:"install,omitempty"`
	Quickstart  []helpStep     `json:"quickstart,omitempty"`
	Config      *helpConfig    `json:"config,omitempty"`
	Conventions []string       `json:"conventions,omitempty"`
	AgentNotes  []string       `json:"agent_notes,omitempty"`
	Topics      []helpTopicRef `json:"topics,omitempty"`
	Commands    []helpCommand  `json:"commands,omitempty"`
}

type helpStep struct {
	Label   string `json:"label"`
	Command string `json:"command"`
	Note    string `json:"note,omitempty"`
}

type helpConfig struct {
	BaseURLResolution    []string `json:"base_url_resolution,omitempty"`
	TokenResolution      []string `json:"token_resolution,omitempty"`
	ConfigPathResolution []string `json:"config_path_resolution,omitempty"`
}

type helpTopicRef struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
}

type helpCommand struct {
	Group    string   `json:"group"`
	Path     string   `json:"path"`
	Usage    string   `json:"usage"`
	Summary  string   `json:"summary"`
	Examples []string `json:"examples,omitempty"`
	Notes    []string `json:"notes,omitempty"`
}

type helpTopicMeta struct {
	Name       string
	Title      string
	Summary    string
	Notes      []string
	CommandSet map[string]bool
}

func (r Runner) runHelp(args []string) error {
	normalizedArgs, err := reorderKnownFlags(args, map[string]bool{
		"json": true,
	}, nil)
	if err != nil {
		return err
	}
	flags := flag.NewFlagSet("help", flag.ContinueOnError)
	flags.SetOutput(r.Stderr)
	jsonOutput := flags.Bool("json", false, "print structured help as JSON")
	if err := flags.Parse(normalizedArgs); err != nil {
		return err
	}
	if flags.NArg() > 1 {
		return fmt.Errorf("usage: fascinate help [--json] [overview|auth|machine|shell|exec|snapshot|env|diagnostics|agents]")
	}
	topic := "overview"
	if flags.NArg() == 1 {
		topic = normalizeHelpTopic(flags.Arg(0))
	}
	doc, ok := buildHelpDoc(topic)
	if !ok {
		return fmt.Errorf("unknown help topic %q; available topics: overview, auth, machine, shell, exec, snapshot, env, diagnostics, agents", flags.Arg(0))
	}
	if *jsonOutput {
		return writeJSON(r.Stdout, doc)
	}
	return renderHelpHuman(r.Stdout, doc)
}

func buildHelpDoc(topic string) (helpDoc, bool) {
	topic = normalizeHelpTopic(topic)
	commands := helpCommands()
	topicRefs := helpTopics()
	switch topic {
	case "", "overview":
		return helpDoc{
			Topic:   "overview",
			Title:   "Fascinate CLI",
			Summary: "Machines, shells, snapshots, env vars, diagnostics, and exec runs all talk to the same Fascinate control plane as the web UI.",
			Install: "curl -fsSL https://fascinate.dev/install.sh | bash",
			Quickstart: []helpStep{
				{
					Label:   "Authenticate",
					Command: "fascinate login --email you@example.com",
					Note:    "Requests an email code, verifies it, and stores a bearer token for later commands.",
				},
				{
					Label:   "Check session",
					Command: "fascinate whoami",
					Note:    "Shows the active CLI identity and token expiry.",
				},
				{
					Label:   "Create a machine",
					Command: "fascinate machine create my-machine",
					Note:    "Queues a new VM on the configured Fascinate host.",
				},
				{
					Label:   "Start a durable shell",
					Command: "fascinate shell create my-machine",
					Note:    "Creates a shared shell resource that the web UI can also see and attach to.",
				},
				{
					Label:   "Run a one-shot command",
					Command: "fascinate exec --json my-machine -- pwd",
					Note:    "Best default for agents because it returns structured stdout, stderr, state, and exit code.",
				},
				{
					Label:   "Inspect platform state",
					Command: "fascinate diagnostics events --json",
					Note:    "Streams recent lifecycle events from the same backend the web UI consumes.",
				},
			},
			Config: &helpConfig{
				BaseURLResolution: []string{
					"--base-url on the command",
					envBaseURL,
					"saved CLI config base_url",
					"http://127.0.0.1:8080 by default",
				},
				TokenResolution: []string{
					envToken,
					"saved CLI config token",
				},
				ConfigPathResolution: []string{
					envCLIConfigPath,
					envCLIConfigDir + "/config.json",
					"XDG config home at fascinate/config.json",
					"the user config dir at fascinate/config.json",
				},
			},
			Conventions: []string{
				"Use --json whenever a command offers it if another tool needs to parse stdout.",
				"Use --jsonl with exec when an agent wants ordered streaming events instead of a final block.",
				"Use -- before the remote command for fascinate exec.",
				"Non-interactive destructive commands require --yes instead of prompting.",
				"Shells are durable backend resources, so shell create, send, attach, and delete stay in sync with the web UI.",
			},
			AgentNotes: []string{
				"Prefer fascinate exec --json for one-shot tasks and validations.",
				"Use shell create plus shell send and shell lines when you need a long-lived shared session.",
				"Use fascinate help --json to discover commands and usage programmatically.",
			},
			Topics:   topicRefs,
			Commands: commands,
		}, true
	default:
		meta, ok := helpTopicByName(topic)
		if !ok {
			return helpDoc{}, false
		}
		filtered := make([]helpCommand, 0, len(commands))
		for _, command := range commands {
			if meta.CommandSet[command.Path] {
				filtered = append(filtered, command)
			}
		}
		doc := helpDoc{
			Topic:      meta.Name,
			Title:      meta.Title,
			Summary:    meta.Summary,
			Commands:   filtered,
			AgentNotes: meta.Notes,
		}
		if meta.Name == "auth" {
			doc.Config = &helpConfig{
				BaseURLResolution: []string{
					"--base-url on the command",
					envBaseURL,
					"saved CLI config base_url",
					"http://127.0.0.1:8080 by default",
				},
				TokenResolution: []string{
					envToken,
					"saved CLI config token",
				},
				ConfigPathResolution: []string{
					envCLIConfigPath,
					envCLIConfigDir + "/config.json",
					"XDG config home at fascinate/config.json",
					"the user config dir at fascinate/config.json",
				},
			}
		}
		return doc, true
	}
}

func normalizeHelpTopic(topic string) string {
	topic = strings.ToLower(strings.TrimSpace(topic))
	switch topic {
	case "", "overview":
		return "overview"
	case "auth", "login", "logout", "whoami":
		return "auth"
	case "machine", "machines":
		return "machine"
	case "shell", "shells":
		return "shell"
	case "exec", "execution":
		return "exec"
	case "snapshot", "snapshots":
		return "snapshot"
	case "env", "env-var", "env-vars":
		return "env"
	case "diagnostics", "diag":
		return "diagnostics"
	case "agent", "agents", "automation":
		return "agents"
	default:
		return topic
	}
}

func renderHelpHuman(w io.Writer, doc helpDoc) error {
	var builder strings.Builder
	builder.WriteString(doc.Title)
	builder.WriteString("\n\n")
	builder.WriteString(doc.Summary)
	builder.WriteString("\n")

	if strings.TrimSpace(doc.Install) != "" {
		builder.WriteString("\nInstall\n")
		builder.WriteString("  ")
		builder.WriteString(doc.Install)
		builder.WriteString("\n")
	}

	if len(doc.Quickstart) > 0 {
		builder.WriteString("\nQuick start\n")
		for index, step := range doc.Quickstart {
			builder.WriteString(fmt.Sprintf("  %d. %s\n", index+1, step.Label))
			builder.WriteString("     ")
			builder.WriteString(step.Command)
			builder.WriteString("\n")
			if strings.TrimSpace(step.Note) != "" {
				builder.WriteString("     ")
				builder.WriteString(step.Note)
				builder.WriteString("\n")
			}
		}
	}

	if doc.Config != nil {
		builder.WriteString("\nConfig and auth resolution\n")
		writeHelpList(&builder, "Base URL:", doc.Config.BaseURLResolution)
		writeHelpList(&builder, "Token:", doc.Config.TokenResolution)
		writeHelpList(&builder, "Config path:", doc.Config.ConfigPathResolution)
	}

	if len(doc.Conventions) > 0 {
		builder.WriteString("\nOutput and automation\n")
		writeHelpList(&builder, "", doc.Conventions)
	}

	if len(doc.Topics) > 0 {
		builder.WriteString("\nTopics\n")
		for _, topic := range doc.Topics {
			builder.WriteString("  ")
			builder.WriteString(topic.Name)
			builder.WriteString("  ")
			builder.WriteString(topic.Summary)
			builder.WriteString("\n")
		}
	}

	if len(doc.Commands) > 0 {
		builder.WriteString("\nCommands\n")
		lastGroup := ""
		for _, command := range doc.Commands {
			if command.Group != lastGroup {
				builder.WriteString("  ")
				builder.WriteString(titleCase(command.Group))
				builder.WriteString("\n")
				lastGroup = command.Group
			}
			builder.WriteString("    ")
			builder.WriteString(command.Usage)
			builder.WriteString("\n")
			builder.WriteString("      ")
			builder.WriteString(command.Summary)
			builder.WriteString("\n")
		}
	}

	if len(doc.AgentNotes) > 0 {
		builder.WriteString("\nAgent notes\n")
		writeHelpList(&builder, "", doc.AgentNotes)
	}

	builder.WriteString("\nMore help\n")
	builder.WriteString("  fascinate help <topic>\n")
	builder.WriteString("  fascinate help --json [topic]\n")

	_, err := io.WriteString(w, builder.String())
	return err
}

func writeHelpList(builder *strings.Builder, label string, items []string) {
	if len(items) == 0 {
		return
	}
	if strings.TrimSpace(label) != "" {
		builder.WriteString("  ")
		builder.WriteString(label)
		builder.WriteString("\n")
	}
	for _, item := range items {
		if strings.TrimSpace(item) == "" {
			continue
		}
		builder.WriteString("    - ")
		builder.WriteString(item)
		builder.WriteString("\n")
	}
}

func titleCase(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return strings.ToUpper(value[:1]) + value[1:]
}

func helpTopics() []helpTopicRef {
	return []helpTopicRef{
		{Name: "auth", Summary: "login, logout, whoami, and config precedence"},
		{Name: "machine", Summary: "create, inspect, fork, delete, and read machine env"},
		{Name: "shell", Summary: "shared shell lifecycle, attach, send, lines, and delete"},
		{Name: "exec", Summary: "non-interactive command execution for humans and agents"},
		{Name: "snapshot", Summary: "save, restore, list, and delete VM snapshots"},
		{Name: "env", Summary: "list, set, and unset user env vars"},
		{Name: "diagnostics", Summary: "inspect events, hosts, budgets, machines, snapshots, shells, and execs"},
		{Name: "agents", Summary: "recommended automation patterns and parsing contract"},
	}
}

func helpTopicByName(name string) (helpTopicMeta, bool) {
	topics := map[string]helpTopicMeta{
		"auth": {
			Name:    "auth",
			Title:   "Fascinate CLI Auth",
			Summary: "Authenticate the CLI with the same email-code identity flow used by the web app.",
			Notes: []string{
				"Set FASCINATE_TOKEN for non-interactive agents that should not read or write local config.",
				"Use fascinate whoami to verify which account and token are active before mutating resources.",
			},
			CommandSet: commandSet("login", "logout", "whoami"),
		},
		"machine": {
			Name:    "machine",
			Title:   "Fascinate CLI Machines",
			Summary: "Create, inspect, fork, delete, and inspect env for Fascinate VMs.",
			Notes: []string{
				"Machine names are the stable handles used by shell and exec commands.",
				"Machine lifecycle stays synchronized with the web UI through the same backend resources.",
			},
			CommandSet: commandSet("machine list", "machine get", "machine create", "machine fork", "machine delete", "machine env"),
		},
		"shell": {
			Name:    "shell",
			Title:   "Fascinate CLI Shells",
			Summary: "Create and operate durable shared shells inside a VM.",
			Notes: []string{
				"Use shell create when you want a long-lived interactive session visible in both CLI and web.",
				"Use shell lines for non-interactive inspection without attaching a TTY.",
			},
			CommandSet: commandSet("shell list", "shell create", "shell attach", "shell send", "shell lines", "shell delete"),
		},
		"exec": {
			Name:    "exec",
			Title:   "Fascinate CLI Exec",
			Summary: "Run a one-shot command in a machine and get structured results.",
			Notes: []string{
				"Prefer exec --json for agent workflows that need stable stdout, stderr, exit code, and timeout fields.",
				"Prefer exec --jsonl when a consumer needs ordered streaming events before completion.",
			},
			CommandSet: commandSet("exec"),
		},
		"snapshot": {
			Name:    "snapshot",
			Title:   "Fascinate CLI Snapshots",
			Summary: "Save, restore, list, and delete full-VM snapshots.",
			Notes: []string{
				"Restoring a snapshot creates a new machine rather than mutating the original machine in place.",
				"Forking a machine is separate from snapshots and is covered under fascinate machine fork.",
			},
			CommandSet: commandSet("snapshot list", "snapshot create", "snapshot restore", "snapshot delete"),
		},
		"env": {
			Name:    "env",
			Title:   "Fascinate CLI Env Vars",
			Summary: "Manage user-defined env vars that Fascinate injects into created, restored, and forked machines.",
			Notes: []string{
				"User-defined keys cannot override the reserved FASCINATE_ prefix.",
				"Use machine env to inspect the resolved env inside a specific machine.",
			},
			CommandSet: commandSet("env list", "env set", "env unset", "machine env"),
		},
		"diagnostics": {
			Name:    "diagnostics",
			Title:   "Fascinate CLI Diagnostics",
			Summary: "Inspect events, hosts, budgets, per-machine state, per-snapshot state, shell inventory, and exec history.",
			Notes: []string{
				"Diagnostics commands are read-only and are the fastest way to inspect current backend state from automation.",
				"diagnostics events is the broadest starting point when you need to understand lifecycle sequencing.",
			},
			CommandSet: commandSet("diagnostics events", "diagnostics hosts", "diagnostics budgets", "diagnostics tool-auth", "diagnostics machine", "diagnostics snapshot", "diagnostics shells", "diagnostics execs"),
		},
		"agents": {
			Name:    "agents",
			Title:   "Fascinate CLI For Agents",
			Summary: "Use structured output first and treat shells as durable shared state only when you need an interactive session.",
			Notes: []string{
				"Start with fascinate help --json to inspect supported commands without scraping human text.",
				"Prefer exec --json or --jsonl over shell attach when a task can run non-interactively.",
				"Use shell create, shell send, and shell lines only for stateful or multi-step interactive flows.",
				"Pass --base-url explicitly when a workflow may target multiple Fascinate environments.",
			},
			CommandSet: commandSet("help", "exec", "machine list", "machine get", "shell list", "shell create", "shell send", "shell lines", "diagnostics events", "diagnostics shells", "diagnostics execs"),
		},
	}
	meta, ok := topics[name]
	return meta, ok
}

func commandSet(paths ...string) map[string]bool {
	set := make(map[string]bool, len(paths))
	for _, path := range paths {
		set[path] = true
	}
	return set
}

func helpCommands() []helpCommand {
	return []helpCommand{
		{
			Group:   "auth",
			Path:    "help",
			Usage:   "fascinate help [--json] [topic]",
			Summary: "Show onboarding help for humans or structured help for agents.",
			Examples: []string{
				"fascinate help",
				"fascinate help --json agents",
			},
		},
		{
			Group:   "auth",
			Path:    "login",
			Usage:   "fascinate login [--base-url <url>] --email <email> [--code <code>] [--token-name <name>] [--no-store]",
			Summary: "Request an email code, verify it, and optionally store a bearer token locally.",
		},
		{
			Group:   "auth",
			Path:    "logout",
			Usage:   "fascinate logout [--base-url <url>]",
			Summary: "Revoke the current CLI token on the server and clear the stored local token.",
		},
		{
			Group:   "auth",
			Path:    "whoami",
			Usage:   "fascinate whoami [--base-url <url>]",
			Summary: "Show the active user and token details for the current CLI context.",
		},
		{
			Group:   "machine",
			Path:    "machine list",
			Usage:   "fascinate machine list [--base-url <url>] [--json]",
			Summary: "List all machines visible to the current user.",
		},
		{
			Group:   "machine",
			Path:    "machine get",
			Usage:   "fascinate machine get [--base-url <url>] [--json] <name>",
			Summary: "Fetch one machine by name, including runtime routing details when available.",
			Examples: []string{
				"fascinate machine get my-machine --json",
			},
		},
		{
			Group:   "machine",
			Path:    "machine create",
			Usage:   "fascinate machine create [--base-url <url>] [--snapshot <snapshot>] [--json] <name>",
			Summary: "Create a new machine from the default image or a named snapshot.",
		},
		{
			Group:   "machine",
			Path:    "machine fork",
			Usage:   "fascinate machine fork [--base-url <url>] [--json] <source> <target>",
			Summary: "Create a new machine by forking an existing source machine.",
		},
		{
			Group:   "machine",
			Path:    "machine delete",
			Usage:   "fascinate machine delete [--base-url <url>] [--yes] <name>",
			Summary: "Delete a machine and release its active compute resources.",
		},
		{
			Group:   "machine",
			Path:    "machine env",
			Usage:   "fascinate machine env [--base-url <url>] [--json] <name>",
			Summary: "Show the resolved env vars for a specific machine.",
		},
		{
			Group:   "shell",
			Path:    "shell list",
			Usage:   "fascinate shell list [--base-url <url>] [--json]",
			Summary: "List durable shell resources for the current user.",
		},
		{
			Group:   "shell",
			Path:    "shell create",
			Usage:   "fascinate shell create [--base-url <url>] [--name <name>] [--json] <machine>",
			Summary: "Create a durable shared shell inside a machine.",
		},
		{
			Group:   "shell",
			Path:    "shell attach",
			Usage:   "fascinate shell attach [--base-url <url>] [--cols N --rows N] <shell-id>",
			Summary: "Attach the local terminal to an existing shell via WebSocket.",
		},
		{
			Group:   "shell",
			Path:    "shell send",
			Usage:   "fascinate shell send [--base-url <url>] [--raw] [--stdin] <shell-id> [text]",
			Summary: "Send input into a shell without attaching an interactive terminal.",
		},
		{
			Group:   "shell",
			Path:    "shell lines",
			Usage:   "fascinate shell lines [--base-url <url>] [--limit N] [--json] <shell-id>",
			Summary: "Read recent shell output lines for inspection or automation.",
		},
		{
			Group:   "shell",
			Path:    "shell delete",
			Usage:   "fascinate shell delete [--base-url <url>] [--yes] <shell-id>",
			Summary: "Delete a durable shell resource.",
		},
		{
			Group:   "exec",
			Path:    "exec",
			Usage:   "fascinate exec [--base-url <url>] [--cwd <path>] [--timeout <duration>] [--json|--jsonl] <machine> -- <command...>",
			Summary: "Run a one-shot command in a machine and return structured results.",
			Examples: []string{
				"fascinate exec --json my-machine -- pwd",
				"fascinate exec --jsonl my-machine -- sh -lc 'echo hello; echo err >&2'",
			},
		},
		{
			Group:   "snapshot",
			Path:    "snapshot list",
			Usage:   "fascinate snapshot list [--base-url <url>] [--json]",
			Summary: "List retained VM snapshots for the current user.",
		},
		{
			Group:   "snapshot",
			Path:    "snapshot create",
			Usage:   "fascinate snapshot create [--base-url <url>] [--json] <machine> <snapshot>",
			Summary: "Save a new snapshot from a running machine.",
		},
		{
			Group:   "snapshot",
			Path:    "snapshot restore",
			Usage:   "fascinate snapshot restore [--base-url <url>] [--json] <snapshot> <machine>",
			Summary: "Create a new machine from a snapshot.",
		},
		{
			Group:   "snapshot",
			Path:    "snapshot delete",
			Usage:   "fascinate snapshot delete [--base-url <url>] [--yes] <name>",
			Summary: "Delete a retained snapshot and free its stored artifact space.",
		},
		{
			Group:   "env",
			Path:    "env list",
			Usage:   "fascinate env list [--base-url <url>] [--json]",
			Summary: "List user-defined Fascinate env vars.",
		},
		{
			Group:   "env",
			Path:    "env set",
			Usage:   "fascinate env set [--base-url <url>] [--json] <KEY> <VALUE>",
			Summary: "Create or replace a user-defined env var.",
		},
		{
			Group:   "env",
			Path:    "env unset",
			Usage:   "fascinate env unset [--base-url <url>] [--yes] <KEY>",
			Summary: "Delete a user-defined env var.",
		},
		{
			Group:   "diagnostics",
			Path:    "diagnostics events",
			Usage:   "fascinate diagnostics events [--base-url <url>] [--json] [--limit N]",
			Summary: "Read recent owner-scoped lifecycle events.",
		},
		{
			Group:   "diagnostics",
			Path:    "diagnostics hosts",
			Usage:   "fascinate diagnostics hosts [--base-url <url>] [--json]",
			Summary: "Inspect registered hosts and current placement eligibility.",
		},
		{
			Group:   "diagnostics",
			Path:    "diagnostics budgets",
			Usage:   "fascinate diagnostics budgets [--base-url <url>] [--json]",
			Summary: "Inspect user budget consumption and remaining headroom.",
		},
		{
			Group:   "diagnostics",
			Path:    "diagnostics tool-auth",
			Usage:   "fascinate diagnostics tool-auth [--base-url <url>] [--json]",
			Summary: "Inspect persisted tool-auth state for the current user.",
		},
		{
			Group:   "diagnostics",
			Path:    "diagnostics machine",
			Usage:   "fascinate diagnostics machine [--base-url <url>] [--json] <name>",
			Summary: "Inspect runtime handles and recent lifecycle events for one machine.",
		},
		{
			Group:   "diagnostics",
			Path:    "diagnostics snapshot",
			Usage:   "fascinate diagnostics snapshot [--base-url <url>] [--json] <name>",
			Summary: "Inspect artifact state and recent lifecycle events for one snapshot.",
		},
		{
			Group:   "diagnostics",
			Path:    "diagnostics shells",
			Usage:   "fascinate diagnostics shells [--base-url <url>] [--json]",
			Summary: "Inspect shell inventory and attachment status.",
		},
		{
			Group:   "diagnostics",
			Path:    "diagnostics execs",
			Usage:   "fascinate diagnostics execs [--base-url <url>] [--json]",
			Summary: "Inspect recent exec runs and their final states.",
		},
	}
}
