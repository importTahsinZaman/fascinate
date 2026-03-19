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
	CreateMachine(context.Context, controlplane.CreateMachineInput) (controlplane.Machine, error)
	DeleteMachine(context.Context, string, string) error
	CloneMachine(context.Context, controlplane.CloneMachineInput) (controlplane.Machine, error)
	CompleteTutorial(context.Context, string) error
}

type mode int

const (
	modeBrowse mode = iota
	modeCreate
	modeClone
	modeDeleteConfirm
)

type loadMachinesMsg struct {
	machines []controlplane.Machine
	err      error
}

type operationDoneMsg struct {
	info    string
	machine *controlplane.Machine
	reload  bool
	err     error
}

type refreshTickMsg struct{}

type Model struct {
	userEmail string
	machines  MachineManager

	items          []controlplane.Machine
	selected       int
	width          int
	height         int
	mode           mode
	input          textinput.Model
	busy           bool
	status         string
	errMsg         string
	sourceName     string
	pendingName    string
	shellTarget    string
	tutorialTarget string
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
		if len(m.items) == 0 {
			m.selected = 0
			if m.mode == modeClone || m.mode == modeDeleteConfirm {
				m.mode = modeBrowse
			}
			return m, nil
		}
		if m.selected >= len(m.items) {
			m.selected = len(m.items) - 1
		}
		if m.selected < 0 {
			m.selected = 0
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
		m.input.SetValue("")
		if msg.machine != nil {
			m.upsertMachine(*msg.machine)
		}
		if msg.reload {
			m.busy = true
			return m, m.loadMachinesCmd()
		}
		return m, m.autoRefreshCmd()
	case tea.KeyMsg:
		switch m.mode {
		case modeCreate, modeClone, modeDeleteConfirm:
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
		sections = append(sections, m.renderInputPanel(
			"Create Machine",
			"Provision a fresh Ubuntu development box with Claude Code ready to go.",
			"name",
			m.input.View(),
			"enter create | esc cancel",
		))
	case modeClone:
		sections = append(sections, m.renderInputPanel(
			"Clone Machine",
			fmt.Sprintf("Create a copy of %s with a new public URL and shell target.", m.sourceName),
			"new name",
			m.input.View(),
			"enter clone | esc cancel",
		))
	case modeDeleteConfirm:
		sections = append(sections, m.renderInputPanel(
			"Delete Machine",
			fmt.Sprintf("Type %s exactly to confirm deletion.", m.pendingName),
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
	case "up", "k":
		if m.selected > 0 {
			m.selected--
		}
		return m, nil
	case "down", "j":
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
		m.input.Focus()
		return m, nil
	case "c":
		selected, ok := m.selectedMachine()
		if !ok || !machineAllowsClone(selected) {
			return m, nil
		}
		m.mode = modeClone
		m.sourceName = selected.Name
		m.input.Placeholder = selected.Name + "-v2"
		m.input.SetValue("")
		m.input.Focus()
		return m, nil
	case "d":
		selected, ok := m.selectedMachine()
		if !ok {
			return m, nil
		}
		m.mode = modeDeleteConfirm
		m.pendingName = selected.Name
		m.input.Placeholder = selected.Name
		m.input.SetValue("")
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
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
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
			return m, m.createMachineCmd(value)
		case modeClone:
			if value == "" {
				m.errMsg = "clone target name is required"
				return m, nil
			}
			m.busy = true
			m.input.Blur()
			return m, m.cloneMachineCmd(m.sourceName, value)
		case modeDeleteConfirm:
			if value != m.pendingName {
				m.errMsg = "confirmation did not match machine name"
				return m, nil
			}
			m.busy = true
			m.input.Blur()
			return m, m.deleteMachineCmd(m.pendingName)
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

func (m Model) loadMachinesCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		machines, err := m.machines.ListMachines(ctx, m.userEmail)
		return loadMachinesMsg{machines: machines, err: err}
	}
}

func (m Model) createMachineCmd(name string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		machine, err := m.machines.CreateMachine(ctx, controlplane.CreateMachineInput{
			Name:       name,
			OwnerEmail: m.userEmail,
		})
		if err != nil {
			return operationDoneMsg{err: err}
		}
		return operationDoneMsg{
			info:    fmt.Sprintf("creating %s", machine.Name),
			machine: &machine,
		}
	}
}

func (m Model) cloneMachineCmd(sourceName, targetName string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()

		machine, err := m.machines.CloneMachine(ctx, controlplane.CloneMachineInput{
			SourceName: sourceName,
			TargetName: targetName,
			OwnerEmail: m.userEmail,
		})
		if err != nil {
			return operationDoneMsg{err: err}
		}
		return operationDoneMsg{
			info:    fmt.Sprintf("cloned %s to %s", sourceName, machine.Name),
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
	if len(m.items) == 0 {
		empty := lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")).
			Render("No machines yet.\n\nPress n to create your first machine.")
		return m.renderPanel(width, "Machines", empty, false)
	}

	var cards []string
	for i, machine := range m.items {
		cards = append(cards, m.renderMachineCard(machine, i == m.selected, width))
	}
	return strings.Join(cards, "\n\n")
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
		if machineAllowsClone(machine) {
			actions = append(actions, "(c) clone")
		}
		actions = append(actions, "(d) delete")
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

func (m Model) renderFooter(width int) string {
	help := "(n) new  •  (r) refresh  •  (q) quit"
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
	case modeClone:
		return "clone"
	case modeDeleteConfirm:
		return "delete"
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

func (m Model) hasPendingProvisioning() bool {
	for _, machine := range m.items {
		if strings.EqualFold(machine.State, "CREATING") {
			return true
		}
	}
	return false
}

func machineAllowsShell(machine controlplane.Machine) bool {
	return strings.EqualFold(machine.State, "RUNNING")
}

func machineAllowsClone(machine controlplane.Machine) bool {
	return strings.EqualFold(machine.State, "RUNNING")
}

func machineAllowsTutorial(machine controlplane.Machine) bool {
	return machine.ShowTutorial && machineAllowsShell(machine)
}
