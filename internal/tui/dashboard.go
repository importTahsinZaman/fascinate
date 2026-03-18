package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"fascinate/internal/controlplane"
)

type MachineManager interface {
	ListMachines(context.Context, string) ([]controlplane.Machine, error)
	GetMachine(context.Context, string) (controlplane.Machine, error)
	CreateMachine(context.Context, controlplane.CreateMachineInput) (controlplane.Machine, error)
	DeleteMachine(context.Context, string) error
	CloneMachine(context.Context, controlplane.CloneMachineInput) (controlplane.Machine, error)
}

type mode int

const (
	modeBrowse mode = iota
	modeDetail
	modeCreate
	modeClone
	modeDeleteConfirm
)

type loadMachinesMsg struct {
	machines []controlplane.Machine
	err      error
}

type operationDoneMsg struct {
	info string
	err  error
}

type Model struct {
	userEmail string
	machines  MachineManager

	items       []controlplane.Machine
	selected    int
	width       int
	height      int
	mode        mode
	input       textinput.Model
	busy        bool
	status      string
	errMsg      string
	sourceName  string
	pendingName string
	shellTarget string
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
			if m.mode == modeDetail || m.mode == modeClone || m.mode == modeDeleteConfirm {
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
		return m, nil
	case operationDoneMsg:
		m.busy = false
		if msg.err != nil {
			m.errMsg = msg.err.Error()
			return m, nil
		}

		m.errMsg = ""
		m.status = msg.info
		m.mode = modeBrowse
		m.sourceName = ""
		m.pendingName = ""
		m.input.SetValue("")
		m.busy = true
		return m, m.loadMachinesCmd()
	case tea.KeyMsg:
		switch m.mode {
		case modeCreate, modeClone, modeDeleteConfirm:
			return m.updateInputMode(msg)
		case modeDetail:
			return m.updateDetailMode(msg)
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
	case modeDetail:
		sections = append(sections, m.renderDetail())
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
		sections = append(sections, m.renderList())
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
		if _, ok := m.selectedMachine(); ok {
			m.mode = modeDetail
		}
		return m, nil
	case "s":
		selected, ok := m.selectedMachine()
		if !ok {
			return m, nil
		}
		m.shellTarget = selected.Name
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
		if !ok {
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

func (m Model) updateDetailMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "s":
		selected, ok := m.selectedMachine()
		if !ok {
			return m, nil
		}
		m.shellTarget = selected.Name
		return m, tea.Quit
	case "esc", "enter":
		m.mode = modeBrowse
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
			m.busy = true
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

func (m Model) renderList() string {
	width := m.contentWidth()

	if len(m.items) == 0 {
		empty := lipgloss.NewStyle().
			Foreground(lipgloss.Color("8")).
			Render("No machines yet.\n\nPress n to create your first machine.")
		return m.renderPanel(width, "Machines", empty)
	}

	var out strings.Builder
	cardWidth := m.cardWidth(width)
	for i, machine := range m.items {
		if i > 0 {
			out.WriteString("\n")
		}
		out.WriteString(m.renderMachineCard(machine, i == m.selected, cardWidth))
	}

	return m.renderPanel(width, "Machines", out.String())
}

func (m Model) renderDetail() string {
	width := m.contentWidth()

	selected, ok := m.selectedMachine()
	if !ok {
		return m.renderPanel(width, "Machine Detail", "No machine selected.\n\nesc to go back")
	}

	var out strings.Builder
	out.WriteString(m.renderKeyValue("name", selected.Name))
	out.WriteString("\n")
	out.WriteString(m.renderKeyValue("owner", selected.OwnerEmail))
	out.WriteString("\n")
	out.WriteString(m.renderKeyValue("state", selected.State))
	out.WriteString("\n")
	out.WriteString(m.renderKeyValue("url", selected.URL))
	out.WriteString("\n")
	out.WriteString(m.renderKeyValue("primary port", fmt.Sprintf("%d", selected.PrimaryPort)))
	if selected.Runtime != nil {
		out.WriteString("\n")
		out.WriteString(m.renderKeyValue("runtime type", selected.Runtime.Type))
		if len(selected.Runtime.IPv4) > 0 {
			out.WriteString("\n")
			out.WriteString(m.renderKeyValue("ipv4", strings.Join(selected.Runtime.IPv4, ", ")))
		}
	}
	out.WriteString("\n\n")
	out.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("s shell | enter back | esc back"))
	return m.renderPanel(width, "Machine Detail", out.String())
}

func (m Model) WantsShell() bool {
	return strings.TrimSpace(m.shellTarget) != ""
}

func (m Model) ShellTarget() string {
	return strings.TrimSpace(m.shellTarget)
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
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()

		machine, err := m.machines.CreateMachine(ctx, controlplane.CreateMachineInput{
			Name:       name,
			OwnerEmail: m.userEmail,
		})
		if err != nil {
			return operationDoneMsg{err: err}
		}
		return operationDoneMsg{info: fmt.Sprintf("created %s", machine.Name)}
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
		return operationDoneMsg{info: fmt.Sprintf("cloned %s to %s", sourceName, machine.Name)}
	}
}

func (m Model) deleteMachineCmd(name string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()

		if err := m.machines.DeleteMachine(ctx, name); err != nil {
			return operationDoneMsg{err: err}
		}
		return operationDoneMsg{info: fmt.Sprintf("deleted %s", name)}
	}
}

func (m Model) contentWidth() int {
	if m.width <= 0 {
		return 92
	}
	return maxInt(40, m.width-2)
}

func (m Model) panelInnerWidth(totalWidth int) int {
	return maxInt(20, totalWidth-4)
}

func (m Model) cardWidth(totalWidth int) int {
	return maxInt(16, m.panelInnerWidth(totalWidth)-4)
}

func (m Model) renderHeader(width int) string {
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12")).Render("fascinate")
	modeBadge := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("0")).
		Background(lipgloss.Color("12")).
		Padding(0, 1).
		Render(strings.ToUpper(m.modeLabel()))

	meta := fmt.Sprintf("%d machine%s", len(m.items), plural(len(m.items)))
	if m.busy {
		meta += " | syncing"
	}

	var out strings.Builder
	out.WriteString(m.padLine(title, modeBadge, m.panelInnerWidth(width)))
	out.WriteString("\n")
	out.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("7")).Render(m.truncate("signed in as "+m.userEmail, m.panelInnerWidth(width))))
	out.WriteString("\n")
	out.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(m.truncate(meta, m.panelInnerWidth(width))))

	return m.renderPanel(width, "Control Plane", out.String())
}

func (m Model) renderBanner(width int) string {
	var messages []string
	if m.status != "" {
		messages = append(messages, lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render("OK  "+m.status))
	}
	if m.errMsg != "" {
		messages = append(messages, lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render("ERR "+m.errMsg))
	}
	if m.busy {
		messages = append(messages, lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render("... working"))
	}
	if len(messages) == 0 {
		return ""
	}
	return m.renderPanel(width, "Status", strings.Join(messages, "\n"))
}

func (m Model) renderInputPanel(title, description, label, value, footer string) string {
	width := m.contentWidth()

	var out strings.Builder
	out.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("7")).Render(m.truncate(description, m.panelInnerWidth(width))))
	out.WriteString("\n\n")
	out.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12")).Render(label + ":"))
	out.WriteString("\n")
	out.WriteString(value)
	out.WriteString("\n\n")
	out.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(footer))

	return m.renderPanel(width, title, out.String())
}

func (m Model) renderMachineCard(machine controlplane.Machine, selected bool, width int) string {
	name := lipgloss.NewStyle().Bold(true).Render(m.truncate(machine.Name, width))
	state := m.statusBadge(machine.State)

	url := lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Render(m.truncate(machine.URL, width))
	actions := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("enter details | s shell | c clone | d delete")

	var out strings.Builder
	if selected {
		out.WriteString(lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("0")).
			Background(lipgloss.Color("10")).
			Padding(0, 1).
			Render("SELECTED"))
		out.WriteString("\n")
	}
	out.WriteString(name)
	out.WriteString("\n")
	out.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("state: "))
	out.WriteString(state)
	out.WriteString("\n")
	out.WriteString(url)
	out.WriteString("\n")
	out.WriteString(actions)

	style := lipgloss.NewStyle().
		Padding(0, 1).
		BorderStyle(asciiBorder()).
		Width(width)
	if selected {
		style = style.
			BorderForeground(lipgloss.Color("12")).
			Background(lipgloss.Color("235"))
	} else {
		style = style.BorderForeground(lipgloss.Color("8"))
	}

	return style.Render(out.String())
}

func (m Model) renderFooter(width int) string {
	help := "j/k move | enter detail | s shell | n new | c clone | d delete | r refresh | q quit"
	return m.renderPanel(width, "Keys", lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(m.truncate(help, m.panelInnerWidth(width))))
}

func (m Model) renderPanel(width int, title, content string) string {
	innerWidth := m.panelInnerWidth(width)
	titleBar := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("12")).
		Render(strings.ToUpper(title))

	style := lipgloss.NewStyle().
		Padding(0, 1).
		Width(innerWidth).
		BorderStyle(asciiBorder()).
		BorderForeground(lipgloss.Color("8"))

	return style.Render(titleBar + "\n\n" + content)
}

func (m Model) renderKeyValue(label, value string) string {
	labelText := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12")).Render(label + ":")
	return labelText + " " + value
}

func (m Model) statusBadge(state string) string {
	value := strings.ToUpper(strings.TrimSpace(state))
	if value == "" {
		value = "UNKNOWN"
	}

	style := lipgloss.NewStyle().Bold(true).Padding(0, 1)
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "running":
		style = style.Foreground(lipgloss.Color("0")).Background(lipgloss.Color("10"))
	case "starting":
		style = style.Foreground(lipgloss.Color("0")).Background(lipgloss.Color("11"))
	case "stopped":
		style = style.Foreground(lipgloss.Color("15")).Background(lipgloss.Color("8"))
	case "deleted", "missing":
		style = style.Foreground(lipgloss.Color("15")).Background(lipgloss.Color("9"))
	default:
		style = style.Foreground(lipgloss.Color("15")).Background(lipgloss.Color("8"))
	}
	return style.Render(value)
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
	case modeDetail:
		return "detail"
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

func asciiBorder() lipgloss.Border {
	return lipgloss.Border{
		Top:          "-",
		Bottom:       "-",
		Left:         "|",
		Right:        "|",
		TopLeft:      "+",
		TopRight:     "+",
		BottomLeft:   "+",
		BottomRight:  "+",
		MiddleLeft:   "|",
		MiddleRight:  "|",
		Middle:       "-",
		MiddleTop:    "+",
		MiddleBottom: "+",
	}
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
