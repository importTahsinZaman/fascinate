package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"fascinate/internal/controlplane"
	machineruntime "fascinate/internal/runtime"
)

type fakeMachines struct {
	listResult    []controlplane.Machine
	listErr       error
	createInput   controlplane.CreateMachineInput
	createErr     error
	cloneInput    controlplane.CloneMachineInput
	cloneErr      error
	deleteName    string
	deleteOwner   string
	deleteErr     error
	tutorialOwner string
	tutorialErr   error
}

func (f *fakeMachines) ListMachines(context.Context, string) ([]controlplane.Machine, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listResult, nil
}

func (f *fakeMachines) CreateMachine(_ context.Context, input controlplane.CreateMachineInput) (controlplane.Machine, error) {
	f.createInput = input
	if f.createErr != nil {
		return controlplane.Machine{}, f.createErr
	}
	return controlplane.Machine{
		Name:        input.Name,
		State:       "CREATING",
		PrimaryPort: 3000,
		URL:         "https://" + input.Name + ".fascinate.dev",
	}, nil
}

func (f *fakeMachines) DeleteMachine(_ context.Context, name, ownerEmail string) error {
	f.deleteName = name
	f.deleteOwner = ownerEmail
	return f.deleteErr
}

func (f *fakeMachines) CloneMachine(_ context.Context, input controlplane.CloneMachineInput) (controlplane.Machine, error) {
	f.cloneInput = input
	if f.cloneErr != nil {
		return controlplane.Machine{}, f.cloneErr
	}
	return controlplane.Machine{Name: input.TargetName}, nil
}

func (f *fakeMachines) CompleteTutorial(_ context.Context, ownerEmail string) error {
	f.tutorialOwner = ownerEmail
	return f.tutorialErr
}

func TestModelLoadsMachinesOnInit(t *testing.T) {
	t.Parallel()

	manager := &fakeMachines{
		listResult: []controlplane.Machine{{Name: "habits", State: "RUNNING"}},
	}
	model := NewDashboard("dev@example.com", manager, 80, 24)

	msg := model.Init()()
	updated, _ := model.Update(msg)
	got := updated.(Model)
	if len(got.items) != 1 || got.items[0].Name != "habits" {
		t.Fatalf("unexpected items: %+v", got.items)
	}
}

func TestModelCreateMachineFlow(t *testing.T) {
	t.Parallel()

	manager := &fakeMachines{}
	model := NewDashboard("dev@example.com", manager, 80, 24)

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	createdModel := updated.(Model)
	createdModel.input.SetValue("habits")

	updated, cmd := createdModel.Update(tea.KeyMsg{Type: tea.KeyEnter})
	inFlight := updated.(Model)
	if inFlight.mode != modeBrowse {
		t.Fatalf("expected browse mode after create submit, got %v", inFlight.mode)
	}

	resultMsg := cmd()
	updated, cmd = inFlight.Update(resultMsg)
	afterResult := updated.(Model)
	if afterResult.mode != modeBrowse {
		t.Fatalf("expected browse mode after create result, got %v", afterResult.mode)
	}
	if len(afterResult.items) != 1 || afterResult.items[0].State != "CREATING" {
		t.Fatalf("expected creating machine card, got %+v", afterResult.items)
	}
	if cmd == nil {
		t.Fatalf("expected auto-refresh command for creating machine")
	}

	if manager.createInput.Name != "habits" || manager.createInput.OwnerEmail != "dev@example.com" {
		t.Fatalf("unexpected create input: %+v", manager.createInput)
	}
}

func TestModelDeleteConfirmationMismatch(t *testing.T) {
	t.Parallel()

	manager := &fakeMachines{listResult: []controlplane.Machine{{Name: "habits", State: "RUNNING"}}}
	model := NewDashboard("dev@example.com", manager, 80, 24)

	updated, _ := model.Update(loadMachinesMsg{machines: manager.listResult})
	withItems := updated.(Model)
	updated, _ = withItems.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	deleteMode := updated.(Model)
	deleteMode.input.SetValue("wrong")

	updated, _ = deleteMode.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(Model)
	if got.errMsg == "" {
		t.Fatalf("expected confirmation error")
	}
	if manager.deleteName != "" {
		t.Fatalf("expected no delete call, got %q", manager.deleteName)
	}
}

func TestModelCloneMachineError(t *testing.T) {
	t.Parallel()

	manager := &fakeMachines{
		listResult: []controlplane.Machine{{Name: "habits", State: "RUNNING"}},
		cloneErr:   errors.New("boom"),
	}
	model := NewDashboard("dev@example.com", manager, 80, 24)

	updated, _ := model.Update(loadMachinesMsg{machines: manager.listResult})
	withItems := updated.(Model)
	updated, _ = withItems.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	cloneMode := updated.(Model)
	cloneMode.input.SetValue("habits-v2")

	updated, cmd := cloneMode.Update(tea.KeyMsg{Type: tea.KeyEnter})
	resultMsg := cmd()
	updated, _ = updated.(Model).Update(resultMsg)
	got := updated.(Model)
	if got.errMsg == "" {
		t.Fatalf("expected clone error")
	}
	if manager.cloneInput.TargetName != "habits-v2" {
		t.Fatalf("unexpected clone input: %+v", manager.cloneInput)
	}
}

func TestModelShellActionFromBrowseMode(t *testing.T) {
	t.Parallel()

	manager := &fakeMachines{
		listResult: []controlplane.Machine{{Name: "habits", State: "RUNNING"}},
	}
	model := NewDashboard("dev@example.com", manager, 80, 24)

	updated, _ := model.Update(loadMachinesMsg{machines: manager.listResult})
	withItems := updated.(Model)
	updated, cmd := withItems.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(Model)
	if cmd == nil {
		t.Fatalf("expected quit command")
	}
	if !got.WantsShell() {
		t.Fatalf("expected shell action")
	}
	if got.ShellTarget() != "habits" {
		t.Fatalf("unexpected shell target: %q", got.ShellTarget())
	}
}

func TestModelShellActionIgnoredWhileMachineCreating(t *testing.T) {
	t.Parallel()

	manager := &fakeMachines{
		listResult: []controlplane.Machine{{Name: "habits", State: "CREATING"}},
	}
	model := NewDashboard("dev@example.com", manager, 80, 24)

	updated, _ := model.Update(loadMachinesMsg{machines: manager.listResult})
	withItems := updated.(Model)
	updated, cmd := withItems.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(Model)
	if cmd != nil {
		t.Fatalf("expected no shell command while creating")
	}
	if got.WantsShell() {
		t.Fatalf("expected no shell action while creating")
	}
}

func TestModelDeleteActionIgnoredWhileMachineCreating(t *testing.T) {
	t.Parallel()

	manager := &fakeMachines{
		listResult: []controlplane.Machine{{Name: "habits", State: "CREATING"}},
	}
	model := NewDashboard("dev@example.com", manager, 80, 24)

	updated, _ := model.Update(loadMachinesMsg{machines: manager.listResult})
	withItems := updated.(Model)
	updated, _ = withItems.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	got := updated.(Model)
	if got.mode != modeBrowse {
		t.Fatalf("expected to stay in browse mode, got %v", got.mode)
	}
	if got.pendingName != "" {
		t.Fatalf("expected no pending delete target, got %q", got.pendingName)
	}
}

func TestViewOmitsDeleteActionWhileMachineCreating(t *testing.T) {
	t.Parallel()

	manager := &fakeMachines{
		listResult: []controlplane.Machine{{Name: "habits", State: "CREATING"}},
	}
	model := NewDashboard("dev@example.com", manager, 80, 24)

	updated, _ := model.Update(loadMachinesMsg{machines: manager.listResult})
	got := updated.(Model).View()
	if strings.Contains(got, "(d) delete") {
		t.Fatalf("expected creating machine view to omit delete action, got %q", got)
	}
}

func TestModelTutorialActionFromBrowseMode(t *testing.T) {
	t.Parallel()

	manager := &fakeMachines{
		listResult: []controlplane.Machine{{Name: "habits", State: "RUNNING", ShowTutorial: true}},
	}
	model := NewDashboard("dev@example.com", manager, 80, 24)

	updated, _ := model.Update(loadMachinesMsg{machines: manager.listResult})
	withItems := updated.(Model)
	updated, cmd := withItems.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	got := updated.(Model)
	if cmd == nil {
		t.Fatalf("expected quit command")
	}
	if !got.WantsTutorial() {
		t.Fatalf("expected tutorial action")
	}
	if got.TutorialTarget() != "habits" {
		t.Fatalf("unexpected tutorial target: %q", got.TutorialTarget())
	}
}

func TestEnterDoesNotSwitchModes(t *testing.T) {
	t.Parallel()

	manager := &fakeMachines{
		listResult: []controlplane.Machine{{Name: "habits", State: "RUNNING"}},
	}
	model := NewDashboard("dev@example.com", manager, 80, 24)

	updated, _ := model.Update(loadMachinesMsg{machines: manager.listResult})
	withItems := updated.(Model)
	updated, _ = withItems.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(Model)
	if got.mode != modeBrowse {
		t.Fatalf("expected browse mode, got %v", got.mode)
	}
}

func TestViewRendersMachineCardsWithSelectedState(t *testing.T) {
	t.Parallel()

	manager := &fakeMachines{
		listResult: []controlplane.Machine{
			{
				Name:         "tic-tac-toe",
				State:        "RUNNING",
				URL:          "https://tic-tac-toe.fascinate.dev",
				PrimaryPort:  3000,
				ShowTutorial: true,
				Runtime: &machineruntime.Machine{
					IPv4: []string{"10.223.166.84"},
				},
			},
			{
				Name:        "notes",
				State:       "STOPPED",
				URL:         "https://notes.fascinate.dev",
				PrimaryPort: 3000,
			},
		},
	}
	model := NewDashboard("dev@example.com", manager, 100, 30)

	updated, _ := model.Update(loadMachinesMsg{machines: manager.listResult})
	view := updated.(Model).View()

	if !containsAll(view, "Fascinate", "tic-tac-toe", "Port 3000:", "https://tic-tac-toe.fascinate.dev", "IPv4", "notes", "(enter) shell", "(t) tutorial", "(q) quit") {
		t.Fatalf("unexpected browse view: %q", view)
	}
	if strings.Contains(view, "Selected machine") || strings.Contains(view, "Your machines") || strings.Contains(view, "SELECTED") || strings.Contains(view, "enter detail") {
		t.Fatalf("unexpected legacy browse layout: %q", view)
	}
}

func containsAll(value string, fragments ...string) bool {
	for _, fragment := range fragments {
		if !strings.Contains(value, fragment) {
			return false
		}
	}
	return true
}
