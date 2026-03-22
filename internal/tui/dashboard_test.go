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
	listResult          []controlplane.Machine
	snapshotResult      []controlplane.Snapshot
	envVarResult        []controlplane.EnvVar
	listErr             error
	createInput         controlplane.CreateMachineInput
	createErr           error
	cloneInput          controlplane.CloneMachineInput
	cloneErr            error
	deleteName          string
	deleteOwner         string
	deleteErr           error
	createSnapshotInput controlplane.CreateSnapshotInput
	createSnapshotErr   error
	deleteSnapshotName  string
	deleteSnapshotOwner string
	deleteSnapshotErr   error
	setEnvInput         controlplane.SetEnvVarInput
	setEnvErr           error
	deleteEnvKey        string
	deleteEnvOwner      string
	deleteEnvErr        error
	tutorialOwner       string
	tutorialErr         error
}

func (f *fakeMachines) ListMachines(context.Context, string) ([]controlplane.Machine, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listResult, nil
}

func (f *fakeMachines) ListSnapshots(context.Context, string) ([]controlplane.Snapshot, error) {
	return f.snapshotResult, nil
}

func (f *fakeMachines) ListEnvVars(context.Context, string) ([]controlplane.EnvVar, error) {
	return f.envVarResult, nil
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

func (f *fakeMachines) CreateSnapshot(_ context.Context, input controlplane.CreateSnapshotInput) (controlplane.Snapshot, error) {
	f.createSnapshotInput = input
	if f.createSnapshotErr != nil {
		return controlplane.Snapshot{}, f.createSnapshotErr
	}
	return controlplane.Snapshot{
		Name:              input.SnapshotName,
		SourceMachineName: input.MachineName,
		State:             "CREATING",
	}, nil
}

func (f *fakeMachines) DeleteSnapshot(_ context.Context, name, ownerEmail string) error {
	f.deleteSnapshotName = name
	f.deleteSnapshotOwner = ownerEmail
	return f.deleteSnapshotErr
}

func (f *fakeMachines) SetEnvVar(_ context.Context, input controlplane.SetEnvVarInput) (controlplane.EnvVar, error) {
	f.setEnvInput = input
	if f.setEnvErr != nil {
		return controlplane.EnvVar{}, f.setEnvErr
	}
	return controlplane.EnvVar{Key: input.Key, RawValue: input.Value, UpdatedAt: "2026-03-22T00:00:00Z"}, nil
}

func (f *fakeMachines) DeleteEnvVar(_ context.Context, ownerEmail, key string) error {
	f.deleteEnvOwner = ownerEmail
	f.deleteEnvKey = key
	return f.deleteEnvErr
}

func (f *fakeMachines) CompleteTutorial(_ context.Context, ownerEmail string) error {
	f.tutorialOwner = ownerEmail
	return f.tutorialErr
}

func TestModelLoadsMachinesOnInit(t *testing.T) {
	t.Parallel()

	manager := &fakeMachines{
		listResult:   []controlplane.Machine{{Name: "habits", State: "RUNNING"}},
		envVarResult: []controlplane.EnvVar{{Key: "APP_LABEL", RawValue: "demo"}},
	}
	model := NewDashboard("dev@example.com", manager, 80, 24)

	msg := model.Init()()
	updated, _ := model.Update(msg)
	got := updated.(Model)
	if len(got.items) != 1 || got.items[0].Name != "habits" {
		t.Fatalf("unexpected items: %+v", got.items)
	}
	if len(got.envVars) != 1 || got.envVars[0].Key != "APP_LABEL" {
		t.Fatalf("unexpected env vars: %+v", got.envVars)
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

func TestModelCreateMachineFromSelectedSnapshot(t *testing.T) {
	t.Parallel()

	manager := &fakeMachines{
		snapshotResult: []controlplane.Snapshot{{Name: "baseline", State: "READY", SourceMachineName: "tic-tac-toe"}},
	}
	model := NewDashboard("dev@example.com", manager, 80, 24)

	updated, _ := model.Update(loadMachinesMsg{snapshots: manager.snapshotResult})
	withSnapshots := updated.(Model)
	updated, _ = withSnapshots.Update(tea.KeyMsg{Type: tea.KeyTab})
	focusedSnapshots := updated.(Model)
	updated, _ = focusedSnapshots.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	createMode := updated.(Model)
	createMode.input.SetValue("habits")

	updated, cmd := createMode.Update(tea.KeyMsg{Type: tea.KeyEnter})
	resultMsg := cmd()
	updated, _ = updated.(Model).Update(resultMsg)
	got := updated.(Model)
	if len(got.items) != 1 || got.items[0].Name != "habits" {
		t.Fatalf("expected created machine card, got %+v", got.items)
	}
	if manager.createInput.SnapshotName != "baseline" {
		t.Fatalf("expected snapshot-backed create, got %+v", manager.createInput)
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

func TestModelAddEnvVarFlow(t *testing.T) {
	t.Parallel()

	manager := &fakeMachines{}
	model := NewDashboard("dev@example.com", manager, 80, 24)

	updated, _ := model.Update(loadMachinesMsg{})
	browse := updated.(Model)
	updated, _ = browse.Update(tea.KeyMsg{Type: tea.KeyTab})
	updated, _ = updated.(Model).Update(tea.KeyMsg{Type: tea.KeyTab})
	envFocus := updated.(Model)
	updated, _ = envFocus.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	envMode := updated.(Model)
	envMode.input.SetValue("FRONTEND_URL=${FASCINATE_PUBLIC_URL}")

	updated, cmd := envMode.Update(tea.KeyMsg{Type: tea.KeyEnter})
	resultMsg := cmd()
	updated, _ = updated.(Model).Update(resultMsg)
	got := updated.(Model)

	if manager.setEnvInput.OwnerEmail != "dev@example.com" || manager.setEnvInput.Key != "FRONTEND_URL" || manager.setEnvInput.Value != "${FASCINATE_PUBLIC_URL}" {
		t.Fatalf("unexpected env input: %+v", manager.setEnvInput)
	}
	if len(got.envVars) != 1 || got.envVars[0].Key != "FRONTEND_URL" {
		t.Fatalf("expected env var in model, got %+v", got.envVars)
	}
}

func TestModelEditEnvVarPrefillsAssignment(t *testing.T) {
	t.Parallel()

	manager := &fakeMachines{
		envVarResult: []controlplane.EnvVar{{Key: "APP_LABEL", RawValue: "old-value"}},
	}
	model := NewDashboard("dev@example.com", manager, 80, 24)

	updated, _ := model.Update(loadMachinesMsg{envVars: manager.envVarResult})
	browse := updated.(Model)
	updated, _ = browse.Update(tea.KeyMsg{Type: tea.KeyTab})
	updated, _ = updated.(Model).Update(tea.KeyMsg{Type: tea.KeyTab})
	envFocus := updated.(Model)
	updated, _ = envFocus.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	got := updated.(Model)

	if got.mode != modeEnvSet {
		t.Fatalf("expected env set mode, got %v", got.mode)
	}
	if got.input.Value() != "APP_LABEL=old-value" {
		t.Fatalf("unexpected input value: %q", got.input.Value())
	}
}

func TestModelDeleteEnvVarFlow(t *testing.T) {
	t.Parallel()

	manager := &fakeMachines{
		envVarResult: []controlplane.EnvVar{{Key: "APP_LABEL", RawValue: "old-value"}},
	}
	model := NewDashboard("dev@example.com", manager, 80, 24)

	updated, _ := model.Update(loadMachinesMsg{envVars: manager.envVarResult})
	browse := updated.(Model)
	updated, _ = browse.Update(tea.KeyMsg{Type: tea.KeyTab})
	updated, _ = updated.(Model).Update(tea.KeyMsg{Type: tea.KeyTab})
	envFocus := updated.(Model)
	updated, _ = envFocus.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	deleteMode := updated.(Model)
	deleteMode.input.SetValue("APP_LABEL")

	updated, cmd := deleteMode.Update(tea.KeyMsg{Type: tea.KeyEnter})
	resultMsg := cmd()
	updated, _ = updated.(Model).Update(resultMsg)
	got := updated.(Model)

	if manager.deleteEnvOwner != "dev@example.com" || manager.deleteEnvKey != "APP_LABEL" {
		t.Fatalf("unexpected env delete call: owner=%q key=%q", manager.deleteEnvOwner, manager.deleteEnvKey)
	}
	if len(got.envVars) != 0 {
		t.Fatalf("expected env vars to be empty, got %+v", got.envVars)
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

func TestModelCreateSnapshotFlow(t *testing.T) {
	t.Parallel()

	manager := &fakeMachines{
		listResult: []controlplane.Machine{{Name: "habits", State: "RUNNING"}},
	}
	model := NewDashboard("dev@example.com", manager, 80, 24)

	updated, _ := model.Update(loadMachinesMsg{machines: manager.listResult})
	withItems := updated.(Model)
	updated, _ = withItems.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	snapshotMode := updated.(Model)
	snapshotMode.input.SetValue("habits-snap")

	updated, cmd := snapshotMode.Update(tea.KeyMsg{Type: tea.KeyEnter})
	resultMsg := cmd()
	updated, _ = updated.(Model).Update(resultMsg)
	got := updated.(Model)
	if len(got.snapshots) != 1 || got.snapshots[0].Name != "habits-snap" {
		t.Fatalf("expected snapshot card, got %+v", got.snapshots)
	}
	if manager.createSnapshotInput.MachineName != "habits" || manager.createSnapshotInput.SnapshotName != "habits-snap" {
		t.Fatalf("unexpected snapshot input: %+v", manager.createSnapshotInput)
	}
}

func TestModelDeleteSnapshotFromSnapshotFocus(t *testing.T) {
	t.Parallel()

	manager := &fakeMachines{
		snapshotResult: []controlplane.Snapshot{{Name: "baseline", State: "READY", SourceMachineName: "habits"}},
	}
	model := NewDashboard("dev@example.com", manager, 80, 24)

	updated, _ := model.Update(loadMachinesMsg{snapshots: manager.snapshotResult})
	withSnapshots := updated.(Model)
	updated, _ = withSnapshots.Update(tea.KeyMsg{Type: tea.KeyTab})
	focused := updated.(Model)
	updated, _ = focused.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	deleteMode := updated.(Model)
	deleteMode.input.SetValue("baseline")

	updated, cmd := deleteMode.Update(tea.KeyMsg{Type: tea.KeyEnter})
	resultMsg := cmd()
	updated, _ = updated.(Model).Update(resultMsg)
	if manager.deleteSnapshotName != "baseline" || manager.deleteSnapshotOwner != "dev@example.com" {
		t.Fatalf("unexpected snapshot delete call: %q %q", manager.deleteSnapshotName, manager.deleteSnapshotOwner)
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
		snapshotResult: []controlplane.Snapshot{
			{
				Name:              "tic-live",
				State:             "READY",
				SourceMachineName: "tic-tac-toe",
				DiskSizeBytes:     5 * 1024 * 1024 * 1024,
				MemorySizeBytes:   512 * 1024 * 1024,
				CreatedAt:         "2026-03-20T01:00:00Z",
			},
		},
	}
	model := NewDashboard("dev@example.com", manager, 100, 30)

	updated, _ := model.Update(loadMachinesMsg{machines: manager.listResult, snapshots: manager.snapshotResult})
	view := updated.(Model).View()

	if !containsAll(view, "Fascinate", "tic-tac-toe", "Port 3000:", "https://tic-tac-toe.fascinate.dev", "IPv4", "notes", "SNAPSHOTS", "tic-live", "(enter) shell", "(t) tutorial", "(q) quit") {
		t.Fatalf("unexpected browse view: %q", view)
	}
	if !strings.Contains(view, "ENV VARS") {
		t.Fatalf("expected env vars panel in browse view: %q", view)
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
