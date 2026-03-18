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
	var out strings.Builder

	title := lipgloss.NewStyle().Bold(true).Render("fascinate")
	subtitle := lipgloss.NewStyle().Faint(true).Render(fmt.Sprintf("signed in as %s", m.userEmail))
	out.WriteString(title)
	out.WriteString("\n")
	out.WriteString(subtitle)
	out.WriteString("\n\n")

	if m.status != "" {
		out.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render(m.status))
		out.WriteString("\n")
	}
	if m.errMsg != "" {
		out.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render("error: " + m.errMsg))
		out.WriteString("\n")
	}
	if m.busy {
		out.WriteString(lipgloss.NewStyle().Faint(true).Render("working..."))
		out.WriteString("\n")
	}

	switch m.mode {
	case modeDetail:
		out.WriteString(m.renderDetail())
	case modeCreate:
		out.WriteString("Create machine\n")
		out.WriteString("name: ")
		out.WriteString(m.input.View())
		out.WriteString("\n\nenter to create, esc to cancel")
	case modeClone:
		out.WriteString(fmt.Sprintf("Clone machine %s\n", m.sourceName))
		out.WriteString("new name: ")
		out.WriteString(m.input.View())
		out.WriteString("\n\nenter to clone, esc to cancel")
	case modeDeleteConfirm:
		out.WriteString(fmt.Sprintf("Delete machine %s\n", m.pendingName))
		out.WriteString("type the machine name to confirm: ")
		out.WriteString(m.input.View())
		out.WriteString("\n\nenter to delete, esc to cancel")
	default:
		out.WriteString(m.renderList())
	}

	out.WriteString("\n\n")
	out.WriteString(lipgloss.NewStyle().Faint(true).Render("j/k or arrows move • enter details • s shell • n create • c clone • d delete • r refresh • q quit"))

	return out.String()
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
	if len(m.items) == 0 {
		return "No machines yet.\n\nPress n to create your first one."
	}

	var out strings.Builder
	out.WriteString("Your machines\n\n")
	for i, machine := range m.items {
		cursor := "  "
		if i == m.selected {
			cursor = "> "
		}
		out.WriteString(fmt.Sprintf("%s%-18s %-10s %s\n", cursor, machine.Name, machine.State, machine.URL))
	}

	return out.String()
}

func (m Model) renderDetail() string {
	selected, ok := m.selectedMachine()
	if !ok {
		return "No machine selected.\n\nesc to go back"
	}

	var out strings.Builder
	out.WriteString("Machine detail\n\n")
	out.WriteString(fmt.Sprintf("name: %s\n", selected.Name))
	out.WriteString(fmt.Sprintf("owner: %s\n", selected.OwnerEmail))
	out.WriteString(fmt.Sprintf("state: %s\n", selected.State))
	out.WriteString(fmt.Sprintf("url: %s\n", selected.URL))
	out.WriteString(fmt.Sprintf("primary port: %d\n", selected.PrimaryPort))
	if selected.Runtime != nil {
		out.WriteString(fmt.Sprintf("runtime type: %s\n", selected.Runtime.Type))
		if len(selected.Runtime.IPv4) > 0 {
			out.WriteString(fmt.Sprintf("ipv4: %s\n", strings.Join(selected.Runtime.IPv4, ", ")))
		}
	}
	out.WriteString("\ns to open shell • enter or esc to go back")
	return out.String()
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
