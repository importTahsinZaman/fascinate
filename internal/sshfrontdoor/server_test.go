package sshfrontdoor

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"fascinate/internal/config"
	"fascinate/internal/controlplane"
	"fascinate/internal/database"
)

type fakeKeyLookup struct {
	record database.SSHKeyRecord
	err    error
}

func (f *fakeKeyLookup) GetSSHKeyByFingerprint(context.Context, string) (database.SSHKeyRecord, error) {
	if f.err != nil {
		return database.SSHKeyRecord{}, f.err
	}
	return f.record, nil
}

type fakeMachines struct {
	listResult           []controlplane.Machine
	listErr              error
	getResult            controlplane.Machine
	getErr               error
	getOwner             string
	getEnvResult         controlplane.MachineEnv
	getEnvErr            error
	createInput          controlplane.CreateMachineInput
	createResult         controlplane.Machine
	createErr            error
	deleteName           string
	deleteOwner          string
	deleteErr            error
	cloneInput           controlplane.CloneMachineInput
	cloneResult          controlplane.Machine
	cloneErr             error
	listSnapshotsResult  []controlplane.Snapshot
	listSnapshotsErr     error
	createSnapshotInput  controlplane.CreateSnapshotInput
	createSnapshotResult controlplane.Snapshot
	createSnapshotErr    error
	deleteSnapshotName   string
	deleteSnapshotOwner  string
	deleteSnapshotErr    error
	listEnvResult        []controlplane.EnvVar
	listEnvErr           error
	setEnvInput          controlplane.SetEnvVarInput
	setEnvResult         controlplane.EnvVar
	setEnvErr            error
	deleteEnvOwner       string
	deleteEnvKey         string
	deleteEnvErr         error
	syncName             string
	syncOwner            string
	syncErr              error
	tutorialOwner        string
	tutorialErr          error
}

type fakeSignup struct {
	enabled bool
}

func (f *fakeSignup) Enabled() bool {
	return f.enabled
}

func (f *fakeSignup) RequestCode(context.Context, string) error {
	return nil
}

func (f *fakeSignup) VerifyAndRegisterKey(context.Context, string, string, string) (database.User, error) {
	return database.User{}, nil
}

func (f *fakeMachines) ListMachines(context.Context, string) ([]controlplane.Machine, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listResult, nil
}

func (f *fakeMachines) GetMachine(_ context.Context, _ string, ownerEmail string) (controlplane.Machine, error) {
	f.getOwner = ownerEmail
	if f.getErr != nil {
		return controlplane.Machine{}, f.getErr
	}
	return f.getResult, nil
}

func (f *fakeMachines) GetMachineEnv(_ context.Context, _ string, ownerEmail string) (controlplane.MachineEnv, error) {
	f.getOwner = ownerEmail
	if f.getEnvErr != nil {
		return controlplane.MachineEnv{}, f.getEnvErr
	}
	return f.getEnvResult, nil
}

func (f *fakeMachines) CreateMachine(_ context.Context, input controlplane.CreateMachineInput) (controlplane.Machine, error) {
	f.createInput = input
	if f.createErr != nil {
		return controlplane.Machine{}, f.createErr
	}
	return f.createResult, nil
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
	return f.cloneResult, nil
}

func (f *fakeMachines) ListSnapshots(context.Context, string) ([]controlplane.Snapshot, error) {
	if f.listSnapshotsErr != nil {
		return nil, f.listSnapshotsErr
	}
	return f.listSnapshotsResult, nil
}

func (f *fakeMachines) CreateSnapshot(_ context.Context, input controlplane.CreateSnapshotInput) (controlplane.Snapshot, error) {
	f.createSnapshotInput = input
	if f.createSnapshotErr != nil {
		return controlplane.Snapshot{}, f.createSnapshotErr
	}
	return f.createSnapshotResult, nil
}

func (f *fakeMachines) DeleteSnapshot(_ context.Context, name, ownerEmail string) error {
	f.deleteSnapshotName = name
	f.deleteSnapshotOwner = ownerEmail
	return f.deleteSnapshotErr
}

func (f *fakeMachines) ListEnvVars(_ context.Context, _ string) ([]controlplane.EnvVar, error) {
	if f.listEnvErr != nil {
		return nil, f.listEnvErr
	}
	return f.listEnvResult, nil
}

func (f *fakeMachines) SetEnvVar(_ context.Context, input controlplane.SetEnvVarInput) (controlplane.EnvVar, error) {
	f.setEnvInput = input
	if f.setEnvErr != nil {
		return controlplane.EnvVar{}, f.setEnvErr
	}
	return f.setEnvResult, nil
}

func (f *fakeMachines) DeleteEnvVar(_ context.Context, ownerEmail, key string) error {
	f.deleteEnvOwner = ownerEmail
	f.deleteEnvKey = key
	return f.deleteEnvErr
}

func (f *fakeMachines) SyncToolAuth(_ context.Context, name, ownerEmail string) error {
	f.syncName = name
	f.syncOwner = ownerEmail
	return f.syncErr
}

func (f *fakeMachines) CompleteTutorial(_ context.Context, ownerEmail string) error {
	f.tutorialOwner = ownerEmail
	return f.tutorialErr
}

type stubChannel struct {
	bytes.Buffer
	requests []string
}

func (c *stubChannel) Read(_ []byte) (int, error) {
	return 0, io.EOF
}

func (c *stubChannel) Close() error {
	return nil
}

func (c *stubChannel) CloseWrite() error {
	return nil
}

func (c *stubChannel) SendRequest(name string, _ bool, _ []byte) (bool, error) {
	c.requests = append(c.requests, name)
	return true, nil
}

func (c *stubChannel) Stderr() io.ReadWriter {
	return c
}

func TestPublicKeyCallbackAcceptsKnownKey(t *testing.T) {
	t.Parallel()

	publicKey, _, fingerprint := generateAuthorizedKey(t)
	server := newTestServer(t, &fakeKeyLookup{
		record: database.SSHKeyRecord{
			UserEmail:   "dev@example.com",
			Name:        "laptop",
			Fingerprint: fingerprint,
		},
	}, &fakeMachines{})

	perms, err := server.config.PublicKeyCallback(nil, publicKey)
	if err != nil {
		t.Fatal(err)
	}
	if perms.Extensions["user_email"] != "dev@example.com" {
		t.Fatalf("unexpected permissions: %+v", perms.Extensions)
	}
}

func TestPublicKeyCallbackRejectsUnknownKey(t *testing.T) {
	t.Parallel()

	publicKey, _, _ := generateAuthorizedKey(t)
	server := newTestServer(t, &fakeKeyLookup{err: database.ErrNotFound}, &fakeMachines{})

	if _, err := server.config.PublicKeyCallback(nil, publicKey); err == nil {
		t.Fatalf("expected authorization error")
	}
}

func TestPublicKeyCallbackAllowsUnknownKeyWhenSignupEnabled(t *testing.T) {
	t.Parallel()

	publicKey, _, fingerprint := generateAuthorizedKey(t)
	server := newTestServer(t, &fakeKeyLookup{err: database.ErrNotFound}, &fakeMachines{}, &fakeSignup{enabled: true})

	perms, err := server.config.PublicKeyCallback(nil, publicKey)
	if err != nil {
		t.Fatal(err)
	}
	if perms.Extensions["signup_required"] != "true" {
		t.Fatalf("expected signup-required permissions, got %+v", perms.Extensions)
	}
	if perms.Extensions["fingerprint"] != fingerprint {
		t.Fatalf("unexpected fingerprint: %+v", perms.Extensions)
	}
}

func TestRunCommandMachines(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, &fakeKeyLookup{}, &fakeMachines{
		listResult: []controlplane.Machine{{Name: "habits", State: "RUNNING", URL: "https://habits.fascinate.dev"}},
	})

	channel := &stubChannel{}
	status := server.runCommand(channel, nil, sessionAuth{userEmail: "dev@example.com"}, "machines", sessionPTY{size: windowSize{width: 80, height: 24}, term: "xterm-256color"})
	if status != 0 {
		t.Fatalf("expected zero status, got %d", status)
	}
	if got := channel.String(); got == "" || !bytes.Contains([]byte(got), []byte("habits")) {
		t.Fatalf("unexpected channel output: %q", got)
	}
}

func TestRunCommandUnknownCommand(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, &fakeKeyLookup{}, &fakeMachines{})

	channel := &stubChannel{}
	status := server.runCommand(channel, nil, sessionAuth{userEmail: "dev@example.com"}, "wat", sessionPTY{size: windowSize{width: 80, height: 24}, term: "xterm-256color"})
	if status != 127 {
		t.Fatalf("expected 127, got %d", status)
	}
	if got := channel.String(); !bytes.Contains([]byte(got), []byte("unknown command")) {
		t.Fatalf("unexpected channel output: %q", got)
	}
}

func TestRunCommandCreateMachine(t *testing.T) {
	t.Parallel()

	machines := &fakeMachines{
		createResult: controlplane.Machine{Name: "habits", State: "CREATING", URL: "https://habits.fascinate.dev"},
	}
	server := newTestServer(t, &fakeKeyLookup{}, machines)

	channel := &stubChannel{}
	status := server.runCommand(channel, nil, sessionAuth{userEmail: "dev@example.com"}, "create habits", sessionPTY{size: windowSize{width: 80, height: 24}, term: "xterm-256color"})
	if status != 0 {
		t.Fatalf("expected zero status, got %d", status)
	}
	if machines.createInput.OwnerEmail != "dev@example.com" || machines.createInput.Name != "habits" {
		t.Fatalf("unexpected create input: %+v", machines.createInput)
	}
	if got := channel.String(); !bytes.Contains([]byte(got), []byte("creating habits")) {
		t.Fatalf("unexpected channel output: %q", got)
	}
}

func TestRunCommandCloneMachine(t *testing.T) {
	t.Parallel()

	machines := &fakeMachines{
		cloneResult: controlplane.Machine{Name: "habits-v2", URL: "https://habits-v2.fascinate.dev"},
	}
	server := newTestServer(t, &fakeKeyLookup{}, machines)

	channel := &stubChannel{}
	status := server.runCommand(channel, nil, sessionAuth{userEmail: "dev@example.com"}, "clone habits habits-v2", sessionPTY{size: windowSize{width: 80, height: 24}, term: "xterm-256color"})
	if status != 0 {
		t.Fatalf("expected zero status, got %d", status)
	}
	if machines.cloneInput.SourceName != "habits" || machines.cloneInput.TargetName != "habits-v2" {
		t.Fatalf("unexpected clone input: %+v", machines.cloneInput)
	}
}

func TestRunCommandListEnvVars(t *testing.T) {
	t.Parallel()

	machines := &fakeMachines{
		listEnvResult: []controlplane.EnvVar{{Key: "FRONTEND_URL", RawValue: "${FASCINATE_PUBLIC_URL}"}},
	}
	server := newTestServer(t, &fakeKeyLookup{}, machines)

	channel := &stubChannel{}
	status := server.runCommand(channel, nil, sessionAuth{userEmail: "dev@example.com"}, "env", sessionPTY{size: windowSize{width: 80, height: 24}, term: "xterm-256color"})
	if status != 0 {
		t.Fatalf("expected zero status, got %d", status)
	}
	if got := channel.String(); !bytes.Contains([]byte(got), []byte("FRONTEND_URL")) {
		t.Fatalf("unexpected channel output: %q", got)
	}
}

func TestRunCommandSetEnvVar(t *testing.T) {
	t.Parallel()

	machines := &fakeMachines{
		setEnvResult: controlplane.EnvVar{Key: "FRONTEND_URL", RawValue: "${FASCINATE_PUBLIC_URL}"},
	}
	server := newTestServer(t, &fakeKeyLookup{}, machines)

	channel := &stubChannel{}
	status := server.runCommand(channel, nil, sessionAuth{userEmail: "dev@example.com"}, "env set FRONTEND_URL ${FASCINATE_PUBLIC_URL}", sessionPTY{size: windowSize{width: 80, height: 24}, term: "xterm-256color"})
	if status != 0 {
		t.Fatalf("expected zero status, got %d", status)
	}
	if machines.setEnvInput.Key != "FRONTEND_URL" || machines.setEnvInput.OwnerEmail != "dev@example.com" {
		t.Fatalf("unexpected env input: %+v", machines.setEnvInput)
	}
}

func TestRunCommandMachineEnv(t *testing.T) {
	t.Parallel()

	machines := &fakeMachines{
		getEnvResult: controlplane.MachineEnv{
			MachineName: "m-1",
			Entries: []controlplane.EffectiveEnvVar{
				{Key: "FASCINATE_PUBLIC_URL", Value: "https://m-1.fascinate.dev"},
			},
		},
	}
	server := newTestServer(t, &fakeKeyLookup{}, machines)

	channel := &stubChannel{}
	status := server.runCommand(channel, nil, sessionAuth{userEmail: "dev@example.com"}, "env machine m-1", sessionPTY{size: windowSize{width: 80, height: 24}, term: "xterm-256color"})
	if status != 0 {
		t.Fatalf("expected zero status, got %d", status)
	}
	if got := channel.String(); !bytes.Contains([]byte(got), []byte("FASCINATE_PUBLIC_URL=https://m-1.fascinate.dev")) {
		t.Fatalf("unexpected channel output: %q", got)
	}
}

func TestRunCommandDeleteMachineRequiresTypedConfirmation(t *testing.T) {
	t.Parallel()

	machines := &fakeMachines{}
	server := newTestServer(t, &fakeKeyLookup{}, machines)

	channel := &stubChannel{}
	status := server.runCommand(channel, nil, sessionAuth{userEmail: "dev@example.com"}, "delete habits", sessionPTY{size: windowSize{width: 80, height: 24}, term: "xterm-256color"})
	if status != 2 {
		t.Fatalf("expected usage status, got %d", status)
	}
	if machines.deleteName != "" {
		t.Fatalf("expected no delete call, got %q", machines.deleteName)
	}
}

func TestRunCommandDeleteMachine(t *testing.T) {
	t.Parallel()

	machines := &fakeMachines{}
	server := newTestServer(t, &fakeKeyLookup{}, machines)

	channel := &stubChannel{}
	status := server.runCommand(channel, nil, sessionAuth{userEmail: "dev@example.com"}, "delete habits --confirm habits", sessionPTY{size: windowSize{width: 80, height: 24}, term: "xterm-256color"})
	if status != 0 {
		t.Fatalf("expected zero status, got %d", status)
	}
	if machines.deleteName != "habits" {
		t.Fatalf("expected delete of habits, got %q", machines.deleteName)
	}
	if machines.deleteOwner != "dev@example.com" {
		t.Fatalf("expected delete owner dev@example.com, got %q", machines.deleteOwner)
	}
}

func TestRenderMachinesReturnsError(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, &fakeKeyLookup{}, &fakeMachines{listErr: errors.New("boom")})

	channel := &stubChannel{}
	if err := server.renderMachines(channel, "dev@example.com"); err == nil {
		t.Fatalf("expected renderMachines to fail")
	}
}

func TestRunCommandRequiresSignupForUnknownKey(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, &fakeKeyLookup{}, &fakeMachines{})

	channel := &stubChannel{}
	status := server.runCommand(channel, nil, sessionAuth{signupRequired: true}, "machines", sessionPTY{size: windowSize{width: 80, height: 24}, term: "xterm-256color"})
	if status != 1 {
		t.Fatalf("expected status 1, got %d", status)
	}
	if got := channel.String(); !bytes.Contains([]byte(got), []byte("complete signup")) {
		t.Fatalf("unexpected output: %q", got)
	}
}

func TestRunCommandShellMachine(t *testing.T) {
	t.Parallel()

	machines := &fakeMachines{
		getResult: controlplane.Machine{
			Name:       "habits",
			OwnerEmail: "dev@example.com",
			State:      "RUNNING",
		},
	}
	server := newTestServer(t, &fakeKeyLookup{}, machines)

	var launched string
	server.shellRunner = func(_ ssh.Channel, _ <-chan *ssh.Request, _ windowSize, machineName string) error {
		launched = machineName
		return nil
	}

	channel := &stubChannel{}
	status := server.runCommand(channel, nil, sessionAuth{userEmail: "dev@example.com"}, "shell habits", sessionPTY{size: windowSize{width: 120, height: 40}, term: "xterm-256color"})
	if status != 0 {
		t.Fatalf("expected zero status, got %d", status)
	}
	if launched != "habits" {
		t.Fatalf("unexpected shell target: %q", launched)
	}
	if machines.getOwner != "dev@example.com" {
		t.Fatalf("expected owner lookup for dev@example.com, got %q", machines.getOwner)
	}
	if machines.syncName != "habits" || machines.syncOwner != "dev@example.com" {
		t.Fatalf("expected auth sync for habits/dev@example.com, got name=%q owner=%q", machines.syncName, machines.syncOwner)
	}
}

func TestRunCommandTutorialMachine(t *testing.T) {
	t.Parallel()

	machines := &fakeMachines{
		getResult: controlplane.Machine{
			Name:         "habits",
			OwnerEmail:   "dev@example.com",
			State:        "RUNNING",
			ShowTutorial: true,
		},
	}
	server := newTestServer(t, &fakeKeyLookup{}, machines)

	var launched string
	server.tutorialRunner = func(_ ssh.Channel, _ <-chan *ssh.Request, _ windowSize, machineName string) error {
		launched = machineName
		return nil
	}

	channel := &stubChannel{}
	status := server.runCommand(channel, nil, sessionAuth{userEmail: "dev@example.com"}, "tutorial habits", sessionPTY{size: windowSize{width: 120, height: 40}, term: "xterm-256color"})
	if status != 0 {
		t.Fatalf("expected zero status, got %d", status)
	}
	if launched != "habits" {
		t.Fatalf("unexpected tutorial target: %q", launched)
	}
	if machines.tutorialOwner != "dev@example.com" {
		t.Fatalf("expected tutorial completion for dev@example.com, got %q", machines.tutorialOwner)
	}
}

func TestTutorialShellCommandUsesParentWorkspace(t *testing.T) {
	t.Parallel()

	command := tutorialShellCommand()
	if !bytes.Contains([]byte(command), []byte("cd "+tutorialWorkspace)) {
		t.Fatalf("expected tutorial command to cd into %s, got %q", tutorialWorkspace, command)
	}
	if bytes.Contains([]byte(command), []byte("cd "+tutorialWorkspace+"/flappy-bird")) {
		t.Fatalf("expected tutorial command to avoid the final app directory, got %q", command)
	}
	if !bytes.Contains([]byte(command), []byte("flappy-bird-app")) {
		t.Fatalf("expected tutorial prompt to mention the new app folder, got %q", command)
	}
	if !bytes.Contains([]byte(command), []byte("non-interactive")) {
		t.Fatalf("expected tutorial prompt to require non-interactive scaffolding, got %q", command)
	}
}

func TestGuestShellCommandIncludesGitHubAuthHint(t *testing.T) {
	t.Parallel()

	command := guestShellCommand()
	if !bytes.Contains([]byte(command), []byte("gh auth login && gh auth setup-git")) {
		t.Fatalf("expected guest shell command to mention GitHub auth setup, got %q", command)
	}
	if !bytes.Contains([]byte(command), []byte("gh auth status --hostname github.com")) {
		t.Fatalf("expected guest shell command to probe gh auth state, got %q", command)
	}
}

func TestGuestShellCommandIsShellValid(t *testing.T) {
	t.Parallel()

	cmd := exec.Command("bash", "-n", "-c", guestShellCommand())
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("guest shell command should parse as shell, got %v: %s", err, strings.TrimSpace(string(output)))
	}
}

func TestGuestSSHRemoteCommandQuotesShellCommand(t *testing.T) {
	t.Parallel()

	command := guestSSHRemoteCommand("xterm-256color", guestShellCommand())
	if !bytes.Contains([]byte(command), []byte("sh -lc 'if command -v gh >/dev/null 2>&1 && ! gh auth status --hostname github.com >/dev/null 2>&1; then")) {
		t.Fatalf("unexpected guest ssh command: %q", command)
	}
	if !bytes.Contains([]byte(command), []byte("gh auth login && gh auth setup-git")) {
		t.Fatalf("expected quoted shell command to keep GitHub auth hint, got %q", command)
	}
}

func TestGuestSSHArgsSplitHostAndPort(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, &fakeKeyLookup{}, &fakeMachines{})
	server.guestSSHKeyPath = "/tmp/fascinate-key"

	args := server.guestSSHArgs("ubuntu", "127.0.0.1", 35841)
	got := strings.Join(args, " ")
	if !bytes.Contains([]byte(got), []byte("-p 35841")) {
		t.Fatalf("expected -p flag in ssh args, got %q", got)
	}
	if !bytes.Contains([]byte(got), []byte("ubuntu@127.0.0.1")) {
		t.Fatalf("expected host without embedded port in ssh args, got %q", got)
	}
	if bytes.Contains([]byte(got), []byte("ubuntu@127.0.0.1:35841")) {
		t.Fatalf("expected host and port to be split, got %q", got)
	}
}

func TestWaitForGuestAccessRetriesTransientFailures(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, &fakeKeyLookup{}, &fakeMachines{})
	server.guestReadyWait = 50 * time.Millisecond
	server.guestReadyPoll = time.Millisecond

	attempts := 0
	server.guestReadyProbe = func(context.Context, string, string) error {
		attempts++
		if attempts < 3 {
			return errors.New("ssh: connect to host 10.42.0.12 port 22: Connection refused")
		}
		return nil
	}

	err := server.waitForGuestAccess(context.Background(), controlplane.Machine{Name: "space-shooter"}, "10.42.0.12", 22, "ubuntu")
	if err != nil {
		t.Fatalf("expected guest access probe to recover, got %v", err)
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
}

func TestWaitForGuestAccessReturnsFriendlyBootingMessage(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, &fakeKeyLookup{}, &fakeMachines{})
	server.guestReadyWait = 5 * time.Millisecond
	server.guestReadyPoll = time.Millisecond
	server.guestReadyProbe = func(context.Context, string, string) error {
		return errors.New("ssh: connect to host 10.42.0.12 port 22: Connection refused")
	}

	err := server.waitForGuestAccess(context.Background(), controlplane.Machine{Name: "space-shooter"}, "10.42.0.12", 22, "ubuntu")
	if err == nil {
		t.Fatalf("expected readiness error")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("still booting")) {
		t.Fatalf("expected friendly booting message, got %v", err)
	}
}

func TestWaitForGuestAccessReturnsNonRetryableErrorImmediately(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, &fakeKeyLookup{}, &fakeMachines{})
	server.guestReadyWait = time.Second
	server.guestReadyPoll = time.Millisecond
	server.guestReadyProbe = func(context.Context, string, string) error {
		return errors.New("Permission denied (publickey)")
	}

	err := server.waitForGuestAccess(context.Background(), controlplane.Machine{Name: "space-shooter"}, "10.42.0.12", 22, "ubuntu")
	if err == nil {
		t.Fatalf("expected error")
	}
	if err.Error() != "Permission denied (publickey)" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunCommandShellMachineRejectsWrongOwner(t *testing.T) {
	t.Parallel()

	machines := &fakeMachines{
		getErr: database.ErrNotFound,
	}
	server := newTestServer(t, &fakeKeyLookup{}, machines)

	channel := &stubChannel{}
	status := server.runCommand(channel, nil, sessionAuth{userEmail: "dev@example.com"}, "shell habits", sessionPTY{size: windowSize{width: 120, height: 40}, term: "xterm-256color"})
	if status != 1 {
		t.Fatalf("expected status 1, got %d", status)
	}
	if got := channel.String(); !bytes.Contains([]byte(got), []byte("not found")) {
		t.Fatalf("unexpected output: %q", got)
	}
}

func TestParsePTYRequestUpdatesTermAndSize(t *testing.T) {
	t.Parallel()

	payload := ssh.Marshal(struct {
		Term   string
		Width  uint32
		Height uint32
		PxW    uint32
		PxH    uint32
		Modes  string
	}{
		Term:   "screen-256color",
		Width:  132,
		Height: 43,
	})

	got := parsePTYRequest(payload, sessionPTY{
		size: windowSize{width: 80, height: 24},
		term: "xterm-256color",
	})

	if got.term != "screen-256color" {
		t.Fatalf("unexpected term: %q", got.term)
	}
	if got.size.width != 132 || got.size.height != 43 {
		t.Fatalf("unexpected size: %+v", got.size)
	}
}

func newTestServer(t *testing.T, keys keyLookup, machines machineManager, signup ...signupManager) *Server {
	t.Helper()

	var signupService signupManager
	if len(signup) > 0 {
		signupService = signup[0]
	}

	server, err := New(config.Config{
		SSHAddr:        "127.0.0.1:0",
		SSHHostKeyPath: filepath.Join(t.TempDir(), "hostkey"),
	}, keys, machines, signupService)
	if err != nil {
		t.Fatal(err)
	}

	return server
}

func generateAuthorizedKey(t *testing.T) (ssh.PublicKey, string, string) {
	t.Helper()

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	publicKey, err := ssh.NewPublicKey(privateKey.Public())
	if err != nil {
		t.Fatal(err)
	}

	return publicKey, string(ssh.MarshalAuthorizedKey(publicKey)), ssh.FingerprintSHA256(publicKey)
}
