package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"fascinate/internal/controlplane"
)

type MachineManager interface {
	ListMachines(context.Context, string) ([]controlplane.Machine, error)
	ListSnapshots(context.Context, string) ([]controlplane.Snapshot, error)
	ListEnvVars(context.Context, string) ([]controlplane.EnvVar, error)
	CreateMachine(context.Context, controlplane.CreateMachineInput) (controlplane.Machine, error)
	DeleteMachine(context.Context, string, string) error
	ForkMachine(context.Context, controlplane.ForkMachineInput) (controlplane.Machine, error)
	CreateSnapshot(context.Context, controlplane.CreateSnapshotInput) (controlplane.Snapshot, error)
	DeleteSnapshot(context.Context, string, string) error
	SetEnvVar(context.Context, controlplane.SetEnvVarInput) (controlplane.EnvVar, error)
	DeleteEnvVar(context.Context, string, string) error
	CompleteTutorial(context.Context, string) error
}

type mode int

const (
	modeBrowse mode = iota
	modeCreate
	modeFork
	modeDeleteConfirm
	modeSnapshotCreate
	modeSnapshotDeleteConfirm
	modeEnvSet
	modeEnvDeleteConfirm
)

type loadMachinesMsg struct {
	machines  []controlplane.Machine
	snapshots []controlplane.Snapshot
	envVars   []controlplane.EnvVar
	err       error
}

type operationDoneMsg struct {
	info          string
	machine       *controlplane.Machine
	snapshot      *controlplane.Snapshot
	envVar        *controlplane.EnvVar
	deletedEnvKey string
	reload        bool
	err           error
}

type refreshTickMsg struct{}

type browseFocus int

const (
	focusMachines browseFocus = iota
	focusSnapshots
	focusEnvVars
)

type Model struct {
	userEmail string
	machines  MachineManager

	items               []controlplane.Machine
	snapshots           []controlplane.Snapshot
	envVars             []controlplane.EnvVar
	selected            int
	selectedSnapshot    int
	selectedEnvVar      int
	focus               browseFocus
	width               int
	height              int
	mode                mode
	input               textinput.Model
	busy                bool
	status              string
	errMsg              string
	sourceName          string
	pendingName         string
	pendingSnapshotName string
	pendingEnvKey       string
	shellTarget         string
	tutorialTarget      string
	createSourceIndex   int
}

func NewDashboard(userEmail string, machines MachineManager, width, height int) Model {
	input := textinput.New()
	input.CharLimit = 63
	input.Prompt = "> "
	input.PromptStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	input.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
	input.PlaceholderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	input.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Reverse(true)

	return Model{
		userEmail: strings.TrimSpace(userEmail),
		machines:  machines,
		width:     width,
		height:    height,
		input:     input,
		busy:      true,
	}
}

func (m Model) Init() tea.Cmd {
	return m.loadMachinesCmd()
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case refreshTickMsg:
		return m, m.loadMachinesCmd()
	case loadMachinesMsg:
		m.busy = false
		if msg.err != nil {
			m.errMsg = msg.err.Error()
			return m, nil
		}

		m.errMsg = ""
		m.items = msg.machines
		m.snapshots = msg.snapshots
		if len(m.items) == 0 {
			m.selected = 0
			if m.mode == modeFork || m.mode == modeDeleteConfirm {
				m.mode = modeBrowse
			}
		} else {
			if m.selected >= len(m.items) {
				m.selected = len(m.items) - 1
			}
			if m.selected < 0 {
				m.selected = 0
			}
		}
		if len(m.snapshots) == 0 {
			m.selectedSnapshot = 0
			if m.focus == focusSnapshots {
				m.focus = focusMachines
			}
		} else {
			if m.selectedSnapshot >= len(m.snapshots) {
				m.selectedSnapshot = len(m.snapshots) - 1
			}
			if m.selectedSnapshot < 0 {
				m.selectedSnapshot = 0
			}
		}
		m.envVars = msg.envVars
		if len(m.envVars) == 0 {
			m.selectedEnvVar = 0
		} else {
			if m.selectedEnvVar >= len(m.envVars) {
				m.selectedEnvVar = len(m.envVars) - 1
			}
			if m.selectedEnvVar < 0 {
				m.selectedEnvVar = 0
			}
		}
		return m, m.autoRefreshCmd()
	case operationDoneMsg:
		m.busy = false
		if msg.err != nil {
			m.status = ""
			m.errMsg = msg.err.Error()
			return m, nil
		}

		m.errMsg = ""
		m.status = msg.info
		m.mode = modeBrowse
		m.sourceName = ""
		m.pendingName = ""
		m.pendingSnapshotName = ""
		m.pendingEnvKey = ""
		m.input.SetValue("")
		if msg.machine != nil {
			m.upsertMachine(*msg.machine)
		}
		if msg.snapshot != nil {
			m.upsertSnapshot(*msg.snapshot)
		}
		if msg.envVar != nil {
			m.upsertEnvVar(*msg.envVar)
		}
		if msg.deletedEnvKey != "" {
			m.removeEnvVar(msg.deletedEnvKey)
		}
		if msg.reload {
			m.busy = true
			return m, m.loadMachinesCmd()
		}
		return m, m.autoRefreshCmd()
	case tea.KeyMsg:
		switch m.mode {
		case modeCreate, modeFork, modeDeleteConfirm, modeSnapshotCreate, modeSnapshotDeleteConfirm, modeEnvSet, modeEnvDeleteConfirm:
			return m.updateInputMode(msg)
		default:
			return m.updateBrowseMode(msg)
		}
	}

	return m, nil
}

func (m Model) View() string {
	width := m.contentWidth()

	sections := []string{
		m.renderHeader(width),
	}

	if banner := m.renderBanner(width); banner != "" {
		sections = append(sections, banner)
	}

	switch m.mode {
	case modeCreate:
		footer := "enter create | esc cancel"
		if len(m.createSources()) > 1 {
			footer = "←/→ source | enter create | esc cancel"
		}
		sections = append(sections, m.renderInputPanel(
			"Create Machine",
			"Provision a fresh Ubuntu development box or restore a saved snapshot.",
			"name",
			m.input.View()+"\n\n"+m.renderCreateSourceLine(),
			footer,
		))
	case modeFork:
		sections = append(sections, m.renderInputPanel(
			"Fork Machine",
			fmt.Sprintf("Create a copy of %s with a new public URL and shell target.", m.sourceName),
			"new name",
			m.input.View(),
			"enter fork | esc cancel",
		))
	case modeDeleteConfirm:
		sections = append(sections, m.renderInputPanel(
			"Delete Machine",
			fmt.Sprintf("Type %s exactly to confirm deletion.", m.pendingName),
			"confirm",
			m.input.View(),
			"enter delete | esc cancel",
		))
	case modeSnapshotCreate:
		sections = append(sections, m.renderInputPanel(
			"Save Snapshot",
			fmt.Sprintf("Capture disk, memory, and device state for %s.", m.sourceName),
			"snapshot name",
			m.input.View(),
			"enter save | esc cancel",
		))
	case modeSnapshotDeleteConfirm:
		sections = append(sections, m.renderInputPanel(
			"Delete Snapshot",
			fmt.Sprintf("Type %s exactly to delete this saved snapshot.", m.pendingSnapshotName),
			"confirm",
			m.input.View(),
			"enter delete | esc cancel",
		))
	case modeEnvSet:
		sections = append(sections, m.renderInputPanel(
			"Set Env Var",
			"Create or update a user env var. Built-in FASCINATE_* vars are managed by Fascinate and cannot be overridden here.",
			"KEY=value",
			m.input.View(),
			"enter save | esc cancel",
		))
	case modeEnvDeleteConfirm:
		sections = append(sections, m.renderInputPanel(
			"Delete Env Var",
			fmt.Sprintf("Type %s exactly to remove this user env var from all future shells and process starts.", m.pendingEnvKey),
			"confirm",
			m.input.View(),
			"enter delete | esc cancel",
		))
	default:
		sections = append(sections, m.renderBrowse(width))
	}

	sections = append(sections, m.renderFooter(width))
	return strings.Join(sections, "\n\n")
}

func (m Model) updateBrowseMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "tab":
		switch m.focus {
		case focusMachines:
			m.focus = focusSnapshots
		case focusSnapshots:
			m.focus = focusEnvVars
		default:
			m.focus = focusMachines
		}
		return m, nil
	case "up", "k":
		if m.focus == focusSnapshots {
			if m.selectedSnapshot > 0 {
				m.selectedSnapshot--
			}
			return m, nil
		}
		if m.focus == focusEnvVars {
			if m.selectedEnvVar > 0 {
				m.selectedEnvVar--
			}
			return m, nil
		}
		if m.selected > 0 {
			m.selected--
		}
		return m, nil
	case "down", "j":
		if m.focus == focusSnapshots {
			if m.selectedSnapshot < len(m.snapshots)-1 {
				m.selectedSnapshot++
			}
			return m, nil
		}
		if m.focus == focusEnvVars {
			if m.selectedEnvVar < len(m.envVars)-1 {
				m.selectedEnvVar++
			}
			return m, nil
		}
		if m.selected < len(m.items)-1 {
			m.selected++
		}
		return m, nil
	case "r":
		m.busy = true
		m.status = ""
		m.errMsg = ""
		return m, m.loadMachinesCmd()
	case "enter":
		if m.focus == focusSnapshots || m.focus == focusEnvVars {
			return m, nil
		}
		selected, ok := m.selectedMachine()
		if !ok || !machineAllowsShell(selected) {
			return m, nil
		}
		m.shellTarget = selected.Name
		return m, tea.Quit
	case "t":
		selected, ok := m.selectedMachine()
		if !ok || !machineAllowsTutorial(selected) {
			return m, nil
		}
		m.tutorialTarget = selected.Name
		return m, tea.Quit
	case "n":
		m.mode = modeCreate
		m.status = ""
		m.errMsg = ""
		m.input.Placeholder = "machine-name"
		m.input.SetValue("")
		m.createSourceIndex = m.defaultCreateSourceIndex()
		m.input.Focus()
		return m, nil
	case "c":
		if m.focus == focusSnapshots {
			return m, nil
		}
		selected, ok := m.selectedMachine()
		if !ok || !machineAllowsFork(selected) {
			return m, nil
		}
		m.mode = modeFork
		m.sourceName = selected.Name
		m.input.Placeholder = selected.Name + "-v2"
		m.input.SetValue("")
		m.input.Focus()
		return m, nil
	case "d":
		if m.focus == focusSnapshots {
			snapshot, ok := m.selectedSnapshotItem()
			if !ok || !snapshotAllowsDelete(snapshot) {
				return m, nil
			}
			m.mode = modeSnapshotDeleteConfirm
			m.pendingSnapshotName = snapshot.Name
			m.input.Placeholder = snapshot.Name
			m.input.SetValue("")
			m.input.Focus()
			return m, nil
		}
		if m.focus == focusEnvVars {
			envVar, ok := m.selectedEnvVarItem()
			if !ok {
				return m, nil
			}
			m.mode = modeEnvDeleteConfirm
			m.pendingEnvKey = envVar.Key
			m.input.Placeholder = envVar.Key
			m.input.SetValue("")
			m.input.Focus()
			return m, nil
		}
		selected, ok := m.selectedMachine()
		if !ok || !machineAllowsDelete(selected) {
			return m, nil
		}
		m.mode = modeDeleteConfirm
		m.pendingName = selected.Name
		m.input.Placeholder = selected.Name
		m.input.SetValue("")
		m.input.Focus()
		return m, nil
	case "p":
		if m.focus == focusSnapshots || m.focus == focusEnvVars {
			return m, nil
		}
		selected, ok := m.selectedMachine()
		if !ok || !machineAllowsSnapshot(selected) {
			return m, nil
		}
		m.mode = modeSnapshotCreate
		m.sourceName = selected.Name
		m.input.Placeholder = selected.Name + "-snapshot"
		m.input.SetValue("")
		m.input.Focus()
		return m, nil
	case "a":
		if m.focus != focusEnvVars {
			return m, nil
		}
		m.mode = modeEnvSet
		m.status = ""
		m.errMsg = ""
		m.pendingEnvKey = ""
		m.input.Placeholder = "KEY=value"
		m.input.SetValue("")
		m.input.Focus()
		return m, nil
	case "e":
		if m.focus != focusEnvVars {
			return m, nil
		}
		envVar, ok := m.selectedEnvVarItem()
		if !ok {
			return m, nil
		}
		m.mode = modeEnvSet
		m.status = ""
		m.errMsg = ""
		m.pendingEnvKey = envVar.Key
		m.input.Placeholder = "KEY=value"
		m.input.SetValue(envVar.Key + "=" + envVar.RawValue)
		m.input.Focus()
		return m, nil
	default:
		return m, nil
	}
}

func (m Model) updateInputMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeBrowse
		m.input.Blur()
		m.input.SetValue("")
		m.sourceName = ""
		m.pendingName = ""
		m.pendingSnapshotName = ""
		m.pendingEnvKey = ""
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	case "left", "h":
		if m.mode == modeCreate && len(m.createSources()) > 1 && m.createSourceIndex > 0 {
			m.createSourceIndex--
		}
		return m, nil
	case "right", "l":
		if m.mode == modeCreate && len(m.createSources()) > 1 && m.createSourceIndex < len(m.createSources())-1 {
			m.createSourceIndex++
		}
		return m, nil
	case "enter":
		value := strings.TrimSpace(m.input.Value())
		switch m.mode {
		case modeCreate:
			if value == "" {
				m.errMsg = "machine name is required"
				return m, nil
			}
			m.mode = modeBrowse
			m.status = fmt.Sprintf("creating %s", value)
			m.errMsg = ""
			m.input.Blur()
			return m, m.createMachineCmd(value, m.selectedCreateSnapshot())
		case modeFork:
			if value == "" {
				m.errMsg = "fork target name is required"
				return m, nil
			}
			m.busy = true
			m.input.Blur()
			return m, m.forkMachineCmd(m.sourceName, value)
		case modeDeleteConfirm:
			if value != m.pendingName {
				m.errMsg = "confirmation did not match machine name"
				return m, nil
			}
			m.busy = true
			m.input.Blur()
			return m, m.deleteMachineCmd(m.pendingName)
		case modeSnapshotCreate:
			if value == "" {
				m.errMsg = "snapshot name is required"
				return m, nil
			}
			m.mode = modeBrowse
			m.status = fmt.Sprintf("snapshotting %s", m.sourceName)
			m.errMsg = ""
			m.input.Blur()
			return m, m.createSnapshotCmd(m.sourceName, value)
		case modeSnapshotDeleteConfirm:
			if value != m.pendingSnapshotName {
				m.errMsg = "confirmation did not match snapshot name"
				return m, nil
			}
			m.busy = true
			m.input.Blur()
			return m, m.deleteSnapshotCmd(m.pendingSnapshotName)
		case modeEnvSet:
			key, rawValue, err := parseEnvAssignment(m.input.Value())
			if err != nil {
				m.errMsg = err.Error()
				return m, nil
			}
			m.busy = true
			m.input.Blur()
			return m, m.setEnvVarCmd(key, rawValue)
		case modeEnvDeleteConfirm:
			if value != m.pendingEnvKey {
				m.errMsg = "confirmation did not match env var key"
				return m, nil
			}
			m.busy = true
			m.input.Blur()
			return m, m.deleteEnvVarCmd(m.pendingEnvKey)
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) WantsShell() bool {
	return strings.TrimSpace(m.shellTarget) != ""
}

func (m Model) ShellTarget() string {
	return strings.TrimSpace(m.shellTarget)
}

func (m Model) WantsTutorial() bool {
	return strings.TrimSpace(m.tutorialTarget) != ""
}

func (m Model) TutorialTarget() string {
	return strings.TrimSpace(m.tutorialTarget)
}

func (m Model) selectedMachine() (controlplane.Machine, bool) {
	if len(m.items) == 0 || m.selected < 0 || m.selected >= len(m.items) {
		return controlplane.Machine{}, false
	}
	return m.items[m.selected], true
}

func (m Model) selectedSnapshotItem() (controlplane.Snapshot, bool) {
	if len(m.snapshots) == 0 || m.selectedSnapshot < 0 || m.selectedSnapshot >= len(m.snapshots) {
		return controlplane.Snapshot{}, false
	}
	return m.snapshots[m.selectedSnapshot], true
}

func (m Model) selectedEnvVarItem() (controlplane.EnvVar, bool) {
	if len(m.envVars) == 0 || m.selectedEnvVar < 0 || m.selectedEnvVar >= len(m.envVars) {
		return controlplane.EnvVar{}, false
	}
	return m.envVars[m.selectedEnvVar], true
}

func (m Model) loadMachinesCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		machines, err := m.machines.ListMachines(ctx, m.userEmail)
		if err != nil {
			return loadMachinesMsg{err: err}
		}
		snapshots, err := m.machines.ListSnapshots(ctx, m.userEmail)
		if err != nil {
			return loadMachinesMsg{err: err}
		}
		envVars, err := m.machines.ListEnvVars(ctx, m.userEmail)
		return loadMachinesMsg{machines: machines, snapshots: snapshots, envVars: envVars, err: err}
	}
}

func (m Model) createMachineCmd(name, snapshotName string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		machine, err := m.machines.CreateMachine(ctx, controlplane.CreateMachineInput{
			Name:         name,
			OwnerEmail:   m.userEmail,
			SnapshotName: snapshotName,
		})
		if err != nil {
			return operationDoneMsg{err: err}
		}
		info := fmt.Sprintf("creating %s", machine.Name)
		if snapshotName != "" {
			info = fmt.Sprintf("restoring %s from %s", machine.Name, snapshotName)
		}
		return operationDoneMsg{
			info:    info,
			machine: &machine,
		}
	}
}

func (m Model) forkMachineCmd(sourceName, targetName string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()

		machine, err := m.machines.ForkMachine(ctx, controlplane.ForkMachineInput{
			SourceName: sourceName,
			TargetName: targetName,
			OwnerEmail: m.userEmail,
		})
		if err != nil {
			return operationDoneMsg{err: err}
		}
		return operationDoneMsg{
			info:    fmt.Sprintf("forked %s to %s", sourceName, machine.Name),
			machine: &machine,
			reload:  true,
		}
	}
}

func (m Model) deleteMachineCmd(name string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()

		if err := m.machines.DeleteMachine(ctx, name, m.userEmail); err != nil {
			return operationDoneMsg{err: err}
		}
		return operationDoneMsg{info: fmt.Sprintf("deleted %s", name), reload: true}
	}
}

func (m Model) createSnapshotCmd(machineName, snapshotName string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		snapshot, err := m.machines.CreateSnapshot(ctx, controlplane.CreateSnapshotInput{
			MachineName:  machineName,
			SnapshotName: snapshotName,
			OwnerEmail:   m.userEmail,
		})
		if err != nil {
			return operationDoneMsg{err: err}
		}
		return operationDoneMsg{
			info:     fmt.Sprintf("snapshotting %s to %s", machineName, snapshot.Name),
			snapshot: &snapshot,
		}
	}
}

func (m Model) deleteSnapshotCmd(name string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := m.machines.DeleteSnapshot(ctx, name, m.userEmail); err != nil {
			return operationDoneMsg{err: err}
		}
		return operationDoneMsg{info: fmt.Sprintf("deleted snapshot %s", name), reload: true}
	}
}

func (m Model) setEnvVarCmd(key, value string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		envVar, err := m.machines.SetEnvVar(ctx, controlplane.SetEnvVarInput{
			OwnerEmail: m.userEmail,
			Key:        key,
			Value:      value,
		})
		if err != nil {
			return operationDoneMsg{err: err}
		}
		return operationDoneMsg{
			info:   fmt.Sprintf("set %s", envVar.Key),
			envVar: &envVar,
		}
	}
}

func (m Model) deleteEnvVarCmd(key string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		if err := m.machines.DeleteEnvVar(ctx, m.userEmail, key); err != nil {
			return operationDoneMsg{err: err}
		}
		return operationDoneMsg{
			info:          fmt.Sprintf("deleted %s", key),
			deletedEnvKey: key,
		}
	}
}

func (m Model) contentWidth() int {
	if m.width <= 0 {
		return 96
	}
	return maxInt(48, m.width-2)
}

func (m Model) renderHeader(width int) string {
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("81")).
		Render("Fascinate")
	modeBadge := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("0")).
		Background(lipgloss.Color("81")).
		Padding(0, 1).
		Render(strings.ToUpper(m.modeLabel()))

	meta := fmt.Sprintf("%d machine%s", len(m.items), plural(len(m.items)))
	if m.busy {
		meta += " | syncing"
	} else if m.hasPendingProvisioning() {
		meta += " | provisioning"
	}

	var out strings.Builder
	out.WriteString(m.padLine(title, modeBadge, width))
	out.WriteString("\n")
	out.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Render(m.truncate("signed in as "+m.userEmail, width)))
	out.WriteString("\n")
	out.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Render(m.truncate(meta, width)))

	return out.String()
}

func (m Model) renderBanner(width int) string {
	var messages []string
	if m.status != "" {
		messages = append(messages, m.renderPill("OK", lipgloss.Color("22"), lipgloss.Color("120"))+" "+lipgloss.NewStyle().Foreground(lipgloss.Color("120")).Render(m.status))
	}
	if m.errMsg != "" {
		messages = append(messages, m.renderPill("ERR", lipgloss.Color("52"), lipgloss.Color("204"))+" "+lipgloss.NewStyle().Foreground(lipgloss.Color("210")).Render(m.errMsg))
	}
	if m.busy {
		messages = append(messages, m.renderPill("SYNC", lipgloss.Color("17"), lipgloss.Color("117"))+" "+lipgloss.NewStyle().Foreground(lipgloss.Color("117")).Render("refreshing machine state"))
	}
	if len(messages) == 0 {
		return ""
	}
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("252")).
		Render(m.truncate(strings.Join(messages, "   "), width))
}

func (m Model) renderInputPanel(title, description, label, value, footer string) string {
	width := m.contentWidth()

	var out strings.Builder
	out.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Render(m.truncate(description, m.panelInnerWidth(width))))
	out.WriteString("\n\n")
	out.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81")).Render(strings.ToUpper(label)))
	out.WriteString("\n")
	out.WriteString(value)
	out.WriteString("\n\n")
	out.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Render(footer))

	return m.renderPanel(width, title, out.String(), false)
}

func (m Model) renderBrowse(width int) string {
	var sections []string
	if len(m.items) == 0 {
		sections = append(sections, m.renderPanel(width, "Machines", lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")).
			Render("No machines yet.\n\nPress n to create your first machine."), m.focus == focusMachines))
	} else {
		var cards []string
		for i, machine := range m.items {
			cards = append(cards, m.renderMachineCard(machine, i == m.selected, width))
		}
		sections = append(sections, strings.Join(cards, "\n\n"))
	}

	snapshotContent := ""
	if len(m.snapshots) == 0 {
		snapshotContent = lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")).
			Render("No saved snapshots yet.\n\nPress p on a running machine to save one.")
	} else {
		var rows []string
		for i, snapshot := range m.snapshots {
			rows = append(rows, m.renderSnapshotRow(snapshot, i == m.selectedSnapshot, width))
		}
		if snapshot, ok := m.selectedSnapshotItem(); ok {
			rows = append(rows, "")
			detail := []string{
				m.renderKeyValue("Source", snapshot.SourceMachineName),
			}
			if snapshot.DiskSizeBytes > 0 || snapshot.MemorySizeBytes > 0 {
				detail = append(detail, m.renderKeyValue("Size", fmt.Sprintf("disk %s  •  memory %s", byteSummary(snapshot.DiskSizeBytes), byteSummary(snapshot.MemorySizeBytes))))
			}
			if snapshotAllowsDelete(snapshot) {
				detail = append(detail, "")
				detail = append(detail, lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Render("(d) delete snapshot"))
			}
			if strings.EqualFold(snapshot.State, "CREATING") {
				detail = append(detail, lipgloss.NewStyle().
					Foreground(lipgloss.Color("117")).
					Render("Recording snapshot state and storing artifacts."))
			}
			rows = append(rows, detail...)
		}
		snapshotContent = strings.Join(rows, "\n")
	}

	sections = append(sections, m.renderPanel(width, "Snapshots", snapshotContent, m.focus == focusSnapshots))
	envContent := ""
	if len(m.envVars) == 0 {
		envContent = lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")).
			Render("No user env vars yet.\n\nPress a to add one. Built-in FASCINATE_* vars are still injected into every machine.")
	} else {
		var rows []string
		for i, envVar := range m.envVars {
			rows = append(rows, m.renderEnvVarRow(envVar, i == m.selectedEnvVar, width))
		}
		if envVar, ok := m.selectedEnvVarItem(); ok {
			rows = append(rows, "")
			detail := []string{
				m.renderKeyValue("Value", envVar.RawValue),
				m.renderKeyValue("Updated", envVar.UpdatedAt),
				"",
				lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Render("(a) add  •  (e) edit  •  (d) delete"),
			}
			rows = append(rows, detail...)
		}
		envContent = strings.Join(rows, "\n")
	}
	sections = append(sections, m.renderPanel(width, "Env Vars", envContent, m.focus == focusEnvVars))
	return strings.Join(sections, "\n\n")
}

func (m Model) renderMachineCard(machine controlplane.Machine, selected bool, totalWidth int) string {
	innerWidth := m.panelInnerWidth(totalWidth)

	name := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("255")).
		Render(m.truncate(machine.Name, maxInt(18, innerWidth-18)))

	header := m.padLine(name, m.statusBadge(machine.State), innerWidth)

	var lines []string
	lines = append(lines, header)

	if strings.TrimSpace(machine.URL) != "" {
		lines = append(lines, m.renderKeyValue(
			fmt.Sprintf("Port %d:", machine.PrimaryPort),
			machine.URL,
		))
	} else {
		lines = append(lines, m.renderKeyValue("Port", fmt.Sprintf("%d", machine.PrimaryPort)))
	}
	if machine.Runtime != nil && len(machine.Runtime.IPv4) > 0 {
		lines = append(lines, m.renderKeyValue("IPv4", strings.Join(machine.Runtime.IPv4, ", ")))
	}
	if machine.Runtime != nil && strings.TrimSpace(machine.Runtime.Type) != "" {
		lines = append(lines, m.renderKeyValue("Runtime", machine.Runtime.Type))
	}

	if selected {
		lines = append(lines, "")
		actions := make([]string, 0, 4)
		if machineAllowsShell(machine) {
			actions = append(actions, "(enter) shell")
		}
		if machineAllowsTutorial(machine) {
			actions = append(actions, "(t) tutorial")
		}
		if machineAllowsFork(machine) {
			actions = append(actions, "(c) fork")
		}
		if machineAllowsSnapshot(machine) {
			actions = append(actions, "(p) snapshot")
		}
		if machineAllowsDelete(machine) {
			actions = append(actions, "(d) delete")
		}
		if strings.EqualFold(machine.State, "CREATING") {
			lines = append(lines, lipgloss.NewStyle().
				Foreground(lipgloss.Color("117")).
				Render("Provisioning VM and waiting for the guest to become ready."))
		}
		lines = append(lines, lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")).
			Render(strings.Join(actions, "  •  ")))
	}

	style := lipgloss.NewStyle().
		Padding(0, 1).
		Width(innerWidth).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240"))
	if selected {
		style = style.
			BorderStyle(lipgloss.DoubleBorder()).
			BorderForeground(lipgloss.Color("81")).
			Background(lipgloss.Color("235"))
	}

	return style.Render(strings.Join(lines, "\n"))
}

func (m Model) renderSnapshotRow(snapshot controlplane.Snapshot, selected bool, totalWidth int) string {
	innerWidth := m.panelInnerWidth(totalWidth)
	name := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("255")).
		Render(m.truncate(snapshot.Name, maxInt(18, innerWidth-18)))
	header := m.padLine(name, m.statusBadge(snapshot.State), innerWidth)

	lines := []string{header}
	if strings.TrimSpace(snapshot.SourceMachineName) != "" {
		lines = append(lines, m.renderKeyValue("From", snapshot.SourceMachineName))
	}
	if snapshot.CreatedAt != "" {
		lines = append(lines, m.renderKeyValue("Created", snapshot.CreatedAt))
	}

	style := lipgloss.NewStyle().
		Padding(0, 1).
		Width(innerWidth).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240"))
	if selected {
		style = style.
			BorderStyle(lipgloss.DoubleBorder()).
			BorderForeground(lipgloss.Color("81")).
			Background(lipgloss.Color("235"))
	}
	return style.Render(strings.Join(lines, "\n"))
}

func (m Model) renderEnvVarRow(envVar controlplane.EnvVar, selected bool, totalWidth int) string {
	innerWidth := m.panelInnerWidth(totalWidth)
	name := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("255")).
		Render(m.truncate(envVar.Key, maxInt(18, innerWidth-4)))
	lines := []string{name, lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Render(m.truncate(envVar.RawValue, innerWidth))}

	style := lipgloss.NewStyle().
		Padding(0, 1).
		Width(innerWidth).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240"))
	if selected {
		style = style.
			BorderStyle(lipgloss.DoubleBorder()).
			BorderForeground(lipgloss.Color("81")).
			Background(lipgloss.Color("235"))
	}
	return style.Render(strings.Join(lines, "\n"))
}

func (m Model) renderFooter(width int) string {
	help := "(n) new  •  (r) refresh  •  (q) quit"
	switch m.focus {
	case focusMachines:
		help = "(n) new  •  (p) snapshot  •  (tab) snapshots  •  (r) refresh  •  (q) quit"
	case focusSnapshots:
		help = "(n) new  •  (d) delete snapshot  •  (tab) env vars  •  (r) refresh  •  (q) quit"
	case focusEnvVars:
		help = "(a) add env  •  (e) edit env  •  (d) delete env  •  (tab) machines  •  (r) refresh  •  (q) quit"
	}
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	if width <= 0 {
		return style.Render(help)
	}
	return style.Width(width).Align(lipgloss.Center).Render(m.truncate(help, width))
}

func (m Model) renderPanel(width int, title, content string, accent bool) string {
	innerWidth := m.panelInnerWidth(width)
	titleBar := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("81")).
		Render(strings.ToUpper(title))

	style := lipgloss.NewStyle().
		Padding(0, 1).
		Width(innerWidth).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240"))
	if accent {
		style = style.BorderForeground(lipgloss.Color("81"))
	}

	return style.Render(titleBar + "\n\n" + content)
}

func (m Model) renderKeyValue(label, value string) string {
	labelText := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81")).Render(label)
	return labelText + "  " + lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Render(value)
}

func (m Model) statusBadge(state string) string {
	value := strings.ToUpper(strings.TrimSpace(state))
	if value == "" {
		value = "UNKNOWN"
	}

	style := lipgloss.NewStyle().Bold(true).Padding(0, 1)
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "running":
		style = style.Foreground(lipgloss.Color("22")).Background(lipgloss.Color("120"))
	case "starting", "creating":
		style = style.Foreground(lipgloss.Color("58")).Background(lipgloss.Color("221"))
	case "failed":
		style = style.Foreground(lipgloss.Color("52")).Background(lipgloss.Color("204"))
	case "stopped":
		style = style.Foreground(lipgloss.Color("251")).Background(lipgloss.Color("238"))
	case "deleted", "missing":
		style = style.Foreground(lipgloss.Color("52")).Background(lipgloss.Color("204"))
	default:
		style = style.Foreground(lipgloss.Color("17")).Background(lipgloss.Color("81"))
	}
	return style.Render(value)
}

func (m Model) renderPill(label string, fg, bg lipgloss.Color) string {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(fg).
		Background(bg).
		Padding(0, 1).
		Render(label)
}

func (m Model) truncate(value string, width int) string {
	if width <= 0 || lipgloss.Width(value) <= width {
		return value
	}

	runes := []rune(value)
	if width <= 3 {
		return string(runes[:width])
	}
	return string(runes[:width-3]) + "..."
}

func (m Model) padLine(left, right string, width int) string {
	leftWidth := lipgloss.Width(left)
	rightWidth := lipgloss.Width(right)
	if width <= 0 {
		return left + " " + right
	}
	if leftWidth+rightWidth+1 >= width {
		return m.truncate(left, maxInt(1, width-rightWidth-1)) + " " + right
	}
	return left + strings.Repeat(" ", width-leftWidth-rightWidth) + right
}

func (m Model) modeLabel() string {
	switch m.mode {
	case modeCreate:
		return "create"
	case modeFork:
		return "fork"
	case modeDeleteConfirm:
		return "delete"
	case modeEnvSet, modeEnvDeleteConfirm:
		return "env"
	default:
		return "browse"
	}
}

func (m Model) panelInnerWidth(totalWidth int) int {
	return maxInt(20, totalWidth-4)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func plural(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

func (m Model) autoRefreshCmd() tea.Cmd {
	if !m.hasPendingProvisioning() {
		return nil
	}

	return tea.Tick(2*time.Second, func(time.Time) tea.Msg {
		return refreshTickMsg{}
	})
}

func (m *Model) upsertMachine(machine controlplane.Machine) {
	for idx, item := range m.items {
		if item.Name != machine.Name {
			continue
		}
		m.items[idx] = machine
		m.selected = idx
		return
	}

	m.items = append(m.items, machine)
	sort.Slice(m.items, func(i, j int) bool {
		return m.items[i].Name < m.items[j].Name
	})
	for idx, item := range m.items {
		if item.Name == machine.Name {
			m.selected = idx
			return
		}
	}
}

func (m *Model) upsertSnapshot(snapshot controlplane.Snapshot) {
	for idx, item := range m.snapshots {
		if item.Name != snapshot.Name {
			continue
		}
		m.snapshots[idx] = snapshot
		m.selectedSnapshot = idx
		return
	}

	m.snapshots = append(m.snapshots, snapshot)
	sort.Slice(m.snapshots, func(i, j int) bool {
		if m.snapshots[i].CreatedAt == m.snapshots[j].CreatedAt {
			return m.snapshots[i].Name < m.snapshots[j].Name
		}
		return m.snapshots[i].CreatedAt > m.snapshots[j].CreatedAt
	})
	for idx, item := range m.snapshots {
		if item.Name == snapshot.Name {
			m.selectedSnapshot = idx
			return
		}
	}
}

func (m *Model) upsertEnvVar(envVar controlplane.EnvVar) {
	for idx, item := range m.envVars {
		if item.Key != envVar.Key {
			continue
		}
		m.envVars[idx] = envVar
		m.selectedEnvVar = idx
		return
	}

	m.envVars = append(m.envVars, envVar)
	sort.Slice(m.envVars, func(i, j int) bool {
		return m.envVars[i].Key < m.envVars[j].Key
	})
	for idx, item := range m.envVars {
		if item.Key == envVar.Key {
			m.selectedEnvVar = idx
			return
		}
	}
}

func (m *Model) removeEnvVar(key string) {
	for idx, item := range m.envVars {
		if item.Key != key {
			continue
		}
		m.envVars = append(m.envVars[:idx], m.envVars[idx+1:]...)
		if len(m.envVars) == 0 {
			m.selectedEnvVar = 0
			if m.focus == focusEnvVars {
				m.focus = focusMachines
			}
			return
		}
		if m.selectedEnvVar >= len(m.envVars) {
			m.selectedEnvVar = len(m.envVars) - 1
		}
		if m.selectedEnvVar < 0 {
			m.selectedEnvVar = 0
		}
		return
	}
}

func (m Model) hasPendingProvisioning() bool {
	for _, machine := range m.items {
		if strings.EqualFold(machine.State, "CREATING") {
			return true
		}
	}
	for _, snapshot := range m.snapshots {
		if strings.EqualFold(snapshot.State, "CREATING") {
			return true
		}
	}
	return false
}

func machineAllowsShell(machine controlplane.Machine) bool {
	return strings.EqualFold(machine.State, "RUNNING")
}

func machineAllowsFork(machine controlplane.Machine) bool {
	return strings.EqualFold(machine.State, "RUNNING")
}

func machineAllowsTutorial(machine controlplane.Machine) bool {
	return machine.ShowTutorial && machineAllowsShell(machine)
}

func machineAllowsDelete(machine controlplane.Machine) bool {
	return !strings.EqualFold(machine.State, "CREATING")
}

func machineAllowsSnapshot(machine controlplane.Machine) bool {
	return strings.EqualFold(machine.State, "RUNNING")
}

func snapshotAllowsDelete(snapshot controlplane.Snapshot) bool {
	return !strings.EqualFold(snapshot.State, "CREATING")
}

func (m Model) createSources() []string {
	sources := []string{""}
	for _, snapshot := range m.snapshots {
		if strings.EqualFold(snapshot.State, "READY") {
			sources = append(sources, snapshot.Name)
		}
	}
	return sources
}

func (m Model) defaultCreateSourceIndex() int {
	if m.focus != focusSnapshots {
		return 0
	}
	selected, ok := m.selectedSnapshotItem()
	if !ok || !strings.EqualFold(selected.State, "READY") {
		return 0
	}
	for idx, source := range m.createSources() {
		if source == selected.Name {
			return idx
		}
	}
	return 0
}

func (m Model) selectedCreateSnapshot() string {
	sources := m.createSources()
	if m.createSourceIndex < 0 || m.createSourceIndex >= len(sources) {
		return ""
	}
	return sources[m.createSourceIndex]
}

func (m Model) renderCreateSourceLine() string {
	source := m.selectedCreateSnapshot()
	if source == "" {
		return m.renderKeyValue("Source", "base image")
	}
	return m.renderKeyValue("Source", "snapshot "+source)
}

func byteSummary(size int64) string {
	switch {
	case size <= 0:
		return "0 B"
	case size >= 1024*1024*1024:
		return fmt.Sprintf("%.1f GiB", float64(size)/(1024*1024*1024))
	case size >= 1024*1024:
		return fmt.Sprintf("%.1f MiB", float64(size)/(1024*1024))
	case size >= 1024:
		return fmt.Sprintf("%.1f KiB", float64(size)/1024)
	default:
		return fmt.Sprintf("%d B", size)
	}
}

func parseEnvAssignment(value string) (string, string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", "", fmt.Errorf("env assignment is required")
	}
	index := strings.Index(value, "=")
	if index <= 0 {
		return "", "", fmt.Errorf("env assignment must look like KEY=value")
	}
	key := strings.TrimSpace(value[:index])
	if key == "" {
		return "", "", fmt.Errorf("env var key is required")
	}
	return key, value[index+1:], nil
}
