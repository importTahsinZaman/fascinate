package sshfrontdoor

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"path/filepath"
	"testing"

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
	listResult    []controlplane.Machine
	listErr       error
	getResult     controlplane.Machine
	getErr        error
	getOwner      string
	createInput   controlplane.CreateMachineInput
	createResult  controlplane.Machine
	createErr     error
	deleteName    string
	deleteOwner   string
	deleteErr     error
	cloneInput    controlplane.CloneMachineInput
	cloneResult   controlplane.Machine
	cloneErr      error
	tutorialOwner string
	tutorialErr   error
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
		createResult: controlplane.Machine{Name: "habits", URL: "https://habits.fascinate.dev"},
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
	if got := channel.String(); !bytes.Contains([]byte(got), []byte("created habits")) {
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
}

func TestRunCommandTutorialMachine(t *testing.T) {
	t.Parallel()

	machines := &fakeMachines{
		getResult: controlplane.Machine{
			Name:         "habits",
			OwnerEmail:   "dev@example.com",
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
