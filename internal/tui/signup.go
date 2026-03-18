package tui

import (
	"context"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"fascinate/internal/database"
)

type SignupManager interface {
	RequestCode(context.Context, string) error
	VerifyAndRegisterKey(context.Context, string, string, string) (database.User, error)
}

type signupStage int

const (
	signupStageEmail signupStage = iota
	signupStageCode
)

type signupCodeRequestedMsg struct {
	err error
}

type signupVerifiedMsg struct {
	err error
}

type SignupModel struct {
	signup    SignupManager
	publicKey string
	stage     signupStage
	input     textinput.Model
	email     string
	status    string
	errMsg    string
	busy      bool
	verified  bool
	width     int
	height    int
}

func NewSignup(signup SignupManager, publicKey string) SignupModel {
	input := textinput.New()
	input.Placeholder = ""
	input.Focus()

	return SignupModel{
		signup:    signup,
		publicKey: strings.TrimSpace(publicKey),
		stage:     signupStageEmail,
		input:     input,
	}
}

func (m SignupModel) Init() tea.Cmd {
	return nil
}

func (m SignupModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case signupCodeRequestedMsg:
		m.busy = false
		if msg.err != nil {
			m.errMsg = msg.err.Error()
			return m, nil
		}

		m.errMsg = ""
		m.stage = signupStageCode
		m.status = "verification code sent"
		m.input.SetValue("")
		m.input.Placeholder = ""
		m.input.Focus()
		return m, nil
	case signupVerifiedMsg:
		m.busy = false
		if msg.err != nil {
			m.errMsg = msg.err.Error()
			return m, nil
		}

		m.errMsg = ""
		m.status = "verification complete"
		m.verified = true
		return m, tea.Quit
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "esc":
			if m.stage == signupStageCode {
				m.stage = signupStageEmail
				m.errMsg = ""
				m.status = ""
				m.input.SetValue(m.email)
				m.input.Placeholder = ""
				m.input.Focus()
			}
			return m, nil
		case "enter":
			return m.submit()
		}

		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m SignupModel) View() string {
	var out strings.Builder

	out.WriteString(lipgloss.NewStyle().Bold(true).Render("Fascinate"))
	out.WriteString("\n\n")

	out.WriteString(lipgloss.NewStyle().Faint(true).Render("Persistent ai agent dev machines; share apps with a hosted *.fascinate.dev link"))
	out.WriteString("\n\n")

	if m.status != "" {
		out.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render(m.status))
		out.WriteString("\n\n")
	}
	if m.errMsg != "" {
		out.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render("error: " + m.errMsg))
		out.WriteString("\n\n")
	}
	if m.busy {
		out.WriteString(lipgloss.NewStyle().Faint(true).Render("working..."))
		out.WriteString("\n\n")
	}

	switch m.stage {
	case signupStageEmail:
		out.WriteString("Sign Up:\n\n")
		out.WriteString("Email: ")
		out.WriteString(m.input.View())
		out.WriteString("\n\nenter to send code • q to quit")
	case signupStageCode:
		out.WriteString("Verify Code:\n\n")
		out.WriteString("Code: ")
		out.WriteString(m.input.View())
		out.WriteString("\n\nenter to verify • esc to go back • q to quit")
	}

	return out.String()
}

func (m SignupModel) submit() (tea.Model, tea.Cmd) {
	value := strings.TrimSpace(m.input.Value())
	switch m.stage {
	case signupStageEmail:
		if value == "" {
			m.errMsg = "email is required"
			return m, nil
		}

		m.busy = true
		m.email = strings.ToLower(value)
		return m, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			return signupCodeRequestedMsg{err: m.signup.RequestCode(ctx, m.email)}
		}
	case signupStageCode:
		if value == "" {
			m.errMsg = "verification code is required"
			return m, nil
		}

		m.busy = true
		return m, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_, err := m.signup.VerifyAndRegisterKey(ctx, m.email, value, m.publicKey)
			return signupVerifiedMsg{err: err}
		}
	default:
		return m, nil
	}
}

func (m SignupModel) VerifiedEmail() string {
	return m.email
}

func (m SignupModel) Verified() bool {
	return m.verified
}
