package tui

import (
	"context"
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"fascinate/internal/controlplane"
)

type fakeMachines struct {
	listResult  []controlplane.Machine
	listErr     error
	createInput controlplane.CreateMachineInput
	createErr   error
	cloneInput  controlplane.CloneMachineInput
	cloneErr    error
	deleteName  string
	deleteErr   error
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
	return controlplane.Machine{Name: input.Name}, nil
}

func (f *fakeMachines) DeleteMachine(_ context.Context, name string) error {
	f.deleteName = name
	return f.deleteErr
}

func (f *fakeMachines) CloneMachine(_ context.Context, input controlplane.CloneMachineInput) (controlplane.Machine, error) {
	f.cloneInput = input
	if f.cloneErr != nil {
		return controlplane.Machine{}, f.cloneErr
	}
	return controlplane.Machine{Name: input.TargetName}, nil
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
	if !inFlight.busy {
		t.Fatalf("expected busy state after submit")
	}

	resultMsg := cmd()
	updated, cmd = inFlight.Update(resultMsg)
	afterResult := updated.(Model)
	if !afterResult.busy {
		t.Fatalf("expected refresh after operation")
	}
	_ = cmd()

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
