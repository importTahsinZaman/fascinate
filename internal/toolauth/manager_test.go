package toolauth

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fascinate/internal/config"
)

type fakeGuestTransport struct {
	captureRuntime string
	captureSpec    SessionStateSpec
	captureArchive []byte
	captureErr     error
	captureFunc    func(string, SessionStateSpec) ([]byte, error)

	restoreRuntime string
	restoreSpec    SessionStateSpec
	restoreArchive []byte
	restoreErr     error
	restoreFunc    func(string, SessionStateSpec, []byte) error
}

func (f *fakeGuestTransport) CaptureSessionState(_ context.Context, runtimeName string, spec SessionStateSpec) ([]byte, error) {
	f.captureRuntime = runtimeName
	f.captureSpec = spec
	if f.captureFunc != nil {
		return f.captureFunc(runtimeName, spec)
	}
	if f.captureErr != nil {
		return nil, f.captureErr
	}
	return f.captureArchive, nil
}

func (f *fakeGuestTransport) RestoreSessionState(_ context.Context, runtimeName string, spec SessionStateSpec, archive []byte) error {
	f.restoreRuntime = runtimeName
	f.restoreSpec = spec
	f.restoreArchive = append([]byte(nil), archive...)
	if f.restoreFunc != nil {
		return f.restoreFunc(runtimeName, spec, archive)
	}
	return f.restoreErr
}

type fakeSessionAdapter struct {
	toolID       string
	authMethodID string
	spec         SessionStateSpec
}

func (f fakeSessionAdapter) ToolID() string {
	return f.toolID
}

func (f fakeSessionAdapter) AuthMethodID() string {
	return f.authMethodID
}

func (f fakeSessionAdapter) StorageMode() StorageMode {
	return StorageModeSessionState
}

func (f fakeSessionAdapter) SessionStateSpec(string) SessionStateSpec {
	return f.spec
}

type fakeUnsupportedAdapter struct{}

func (fakeUnsupportedAdapter) ToolID() string {
	return "unsupported"
}

func (fakeUnsupportedAdapter) AuthMethodID() string {
	return "api-key"
}

func (fakeUnsupportedAdapter) StorageMode() StorageMode {
	return StorageModeSecretMaterial
}

func TestStoreSaveLoadSessionStateCreatesRollbackBackup(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := context.Background()
	key := ProfileKey{UserID: "user-1", ToolID: "claude", AuthMethodID: "claude-subscription"}

	firstArchive, err := EmptySessionStateArchive()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSessionState(ctx, Profile{
		Key:         key,
		StorageMode: StorageModeSessionState,
		Version:     1,
		Empty:       true,
	}, firstArchive); err != nil {
		t.Fatal(err)
	}

	secondArchive := archiveWithFile(t, "/home/ubuntu/.claude/session.json", `{"ok":true}`)
	if err := store.SaveSessionState(ctx, Profile{
		Key:         key,
		StorageMode: StorageModeSessionState,
		Version:     1,
		Empty:       false,
	}, secondArchive); err != nil {
		t.Fatal(err)
	}

	profile, loaded, err := store.LoadSessionState(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if profile.Empty {
		t.Fatalf("expected non-empty profile after second save")
	}
	if !bytes.Equal(loaded, secondArchive) {
		t.Fatalf("expected latest archive to load")
	}

	profilePath := filepath.Join(store.profileDir(key), "profile.prev.json")
	statePath := filepath.Join(store.profileDir(key), "state.prev.enc")
	if _, err := os.Stat(profilePath); err != nil {
		t.Fatalf("expected rollback profile backup, got %v", err)
	}
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("expected rollback state backup, got %v", err)
	}
}

func TestManagerCaptureAndRestoreClaudeSessionState(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	guest := &fakeGuestTransport{
		captureArchive: archiveWithFile(t, "/home/ubuntu/.claude/auth.json", `{"token":"abc"}`),
	}
	manager, err := NewManager(store, guest, ClaudeSubscriptionAdapter{})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := manager.CaptureAll(ctx, "user-1", "space-shooter", "ubuntu"); err != nil {
		t.Fatal(err)
	}
	if guest.captureRuntime != "space-shooter" {
		t.Fatalf("unexpected capture runtime: %q", guest.captureRuntime)
	}
	if len(guest.captureSpec.Roots) != 2 {
		t.Fatalf("unexpected capture roots: %+v", guest.captureSpec.Roots)
	}
	if guest.captureSpec.Roots[0].Path != "/home/ubuntu/.claude.json" || guest.captureSpec.Roots[0].Kind != SessionStateRootKindFile {
		t.Fatalf("unexpected file root: %+v", guest.captureSpec.Roots[0])
	}
	if guest.captureSpec.Roots[1].Path != "/home/ubuntu/.claude" || guest.captureSpec.Roots[1].Kind != SessionStateRootKindDirectory {
		t.Fatalf("unexpected capture spec: %+v", guest.captureSpec)
	}

	if err := manager.RestoreAll(ctx, "user-1", "second-machine", "ubuntu"); err != nil {
		t.Fatal(err)
	}
	if guest.restoreRuntime != "second-machine" {
		t.Fatalf("unexpected restore runtime: %q", guest.restoreRuntime)
	}
	if !bytes.Equal(guest.restoreArchive, guest.captureArchive) {
		t.Fatalf("expected stored archive to be restored")
	}
}

func TestManagerCaptureAndRestoreCodexSessionState(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	guest := &fakeGuestTransport{
		captureArchive: archiveWithFile(t, "/home/ubuntu/.codex/auth.json", `{"access_token":"abc"}`),
	}
	manager, err := NewManager(store, guest, CodexChatGPTAdapter{})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := manager.CaptureAll(ctx, "user-1", "tic-tac-toe", "ubuntu"); err != nil {
		t.Fatal(err)
	}
	if guest.captureRuntime != "tic-tac-toe" {
		t.Fatalf("unexpected capture runtime: %q", guest.captureRuntime)
	}
	if len(guest.captureSpec.Roots) != 1 {
		t.Fatalf("unexpected capture roots: %+v", guest.captureSpec.Roots)
	}
	if guest.captureSpec.Roots[0].Path != "/home/ubuntu/.codex" || guest.captureSpec.Roots[0].Kind != SessionStateRootKindDirectory {
		t.Fatalf("unexpected capture spec: %+v", guest.captureSpec.Roots)
	}
	if len(guest.captureSpec.Roots[0].ExcludeBaseNames) != 1 || guest.captureSpec.Roots[0].ExcludeBaseNames[0] != "AGENTS.md" {
		t.Fatalf("unexpected codex exclude list: %+v", guest.captureSpec.Roots[0].ExcludeBaseNames)
	}

	if err := manager.RestoreAll(ctx, "user-1", "space-shooter", "ubuntu"); err != nil {
		t.Fatal(err)
	}
	if guest.restoreRuntime != "space-shooter" {
		t.Fatalf("unexpected restore runtime: %q", guest.restoreRuntime)
	}
	if !bytes.Equal(guest.restoreArchive, guest.captureArchive) {
		t.Fatalf("expected stored archive to be restored")
	}
}

func TestClaudeSubscriptionAdapterIncludesTopLevelConfigFile(t *testing.T) {
	t.Parallel()

	spec := (ClaudeSubscriptionAdapter{}).SessionStateSpec("ubuntu")
	if len(spec.Roots) != 2 {
		t.Fatalf("expected 2 roots, got %+v", spec.Roots)
	}
	if spec.Roots[0].Path != "/home/ubuntu/.claude.json" || spec.Roots[0].Kind != SessionStateRootKindFile {
		t.Fatalf("unexpected top-level Claude config root: %+v", spec.Roots[0])
	}
	if spec.Roots[1].Path != "/home/ubuntu/.claude" || spec.Roots[1].Kind != SessionStateRootKindDirectory {
		t.Fatalf("unexpected Claude directory root: %+v", spec.Roots[1])
	}
}

func TestCodexChatGPTAdapterIncludesCodexDirectory(t *testing.T) {
	t.Parallel()

	spec := (CodexChatGPTAdapter{}).SessionStateSpec("ubuntu")
	if len(spec.Roots) != 1 {
		t.Fatalf("expected 1 root, got %+v", spec.Roots)
	}
	if spec.Roots[0].Path != "/home/ubuntu/.codex" || spec.Roots[0].Kind != SessionStateRootKindDirectory {
		t.Fatalf("unexpected codex root: %+v", spec.Roots[0])
	}
	if len(spec.Roots[0].ExcludeBaseNames) != 1 || spec.Roots[0].ExcludeBaseNames[0] != "AGENTS.md" {
		t.Fatalf("unexpected codex exclude list: %+v", spec.Roots[0].ExcludeBaseNames)
	}
}

func TestManagerCapturePersistsExactEmptyState(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	emptyArchive, err := EmptySessionStateArchive()
	if err != nil {
		t.Fatal(err)
	}
	guest := &fakeGuestTransport{captureArchive: emptyArchive}
	manager, err := NewManager(store, guest, ClaudeSubscriptionAdapter{})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := manager.CaptureAll(ctx, "user-1", "space-shooter", "ubuntu"); err != nil {
		t.Fatal(err)
	}

	profile, loaded, err := store.LoadSessionState(ctx, ProfileKey{
		UserID:       "user-1",
		ToolID:       ToolIDClaude,
		AuthMethodID: AuthMethodClaudeSubscription,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !profile.Empty {
		t.Fatalf("expected empty profile")
	}
	if !bytes.Equal(loaded, emptyArchive) {
		t.Fatalf("expected empty archive to be stored exactly")
	}
}

func TestManagerCaptureAllNonDestructivePreservesExistingNonEmptyState(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	guest := &fakeGuestTransport{
		captureArchive: archiveWithFile(t, "/home/ubuntu/.claude/auth.json", `{"token":"abc"}`),
	}
	manager, err := NewManager(store, guest, ClaudeSubscriptionAdapter{})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := manager.CaptureAll(ctx, "user-1", "tic-tac-toe", "ubuntu"); err != nil {
		t.Fatal(err)
	}

	emptyArchive, err := EmptySessionStateArchive()
	if err != nil {
		t.Fatal(err)
	}
	guest.captureArchive = emptyArchive
	if err := manager.CaptureAllNonDestructive(ctx, "user-1", "space-shooter", "ubuntu"); err != nil {
		t.Fatal(err)
	}

	profile, loaded, err := store.LoadSessionState(ctx, ProfileKey{
		UserID:       "user-1",
		ToolID:       ToolIDClaude,
		AuthMethodID: AuthMethodClaudeSubscription,
	})
	if err != nil {
		t.Fatal(err)
	}
	if profile.Empty {
		t.Fatalf("expected stored non-empty profile to be preserved")
	}
	if bytes.Equal(loaded, emptyArchive) {
		t.Fatalf("expected existing non-empty bundle to survive non-destructive empty capture")
	}
}

func TestManagerCaptureAllContinuesPastAdapterErrors(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	guest := &fakeGuestTransport{
		captureFunc: func(_ string, spec SessionStateSpec) ([]byte, error) {
			if len(spec.Roots) > 0 && spec.Roots[0].Path == "/home/ubuntu/.broken" {
				return nil, errors.New("broken capture")
			}
			return archiveWithFile(t, "/home/ubuntu/.codex/auth.json", `{"access_token":"abc"}`), nil
		},
	}
	manager, err := NewManager(
		store,
		guest,
		fakeSessionAdapter{
			toolID:       "broken",
			authMethodID: "broken-session",
			spec: SessionStateSpec{
				Version: 1,
				Roots: []SessionStateRoot{{
					Path:          "/home/ubuntu/.broken",
					Kind:          SessionStateRootKindDirectory,
					DirectoryMode: 0o700,
				}},
			},
		},
		CodexChatGPTAdapter{},
	)
	if err != nil {
		t.Fatal(err)
	}

	err = manager.CaptureAll(context.Background(), "user-1", "space-shooter", "ubuntu")
	if err == nil || !strings.Contains(err.Error(), "broken capture") {
		t.Fatalf("expected joined capture error, got %v", err)
	}

	profile, _, err := store.LoadSessionState(context.Background(), ProfileKey{
		UserID:       "user-1",
		ToolID:       ToolIDCodex,
		AuthMethodID: AuthMethodCodexChatGPT,
	})
	if err != nil {
		t.Fatal(err)
	}
	if profile.Empty {
		t.Fatalf("expected codex capture to succeed despite another adapter failing")
	}
}

func TestManagerRestoreAllContinuesPastAdapterErrors(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := context.Background()
	if err := store.SaveSessionState(ctx, Profile{
		Key: ProfileKey{
			UserID:       "user-1",
			ToolID:       "broken",
			AuthMethodID: "broken-session",
		},
		StorageMode: StorageModeSessionState,
		Version:     1,
		Empty:       false,
	}, archiveWithFile(t, "/home/ubuntu/.broken/session.json", `{"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSessionState(ctx, Profile{
		Key: ProfileKey{
			UserID:       "user-1",
			ToolID:       ToolIDCodex,
			AuthMethodID: AuthMethodCodexChatGPT,
		},
		StorageMode: StorageModeSessionState,
		Version:     1,
		Empty:       false,
	}, archiveWithFile(t, "/home/ubuntu/.codex/auth.json", `{"access_token":"abc"}`)); err != nil {
		t.Fatal(err)
	}

	var restoredRoots []string
	guest := &fakeGuestTransport{
		restoreFunc: func(_ string, spec SessionStateSpec, _ []byte) error {
			if len(spec.Roots) > 0 {
				restoredRoots = append(restoredRoots, spec.Roots[0].Path)
			}
			if len(spec.Roots) > 0 && spec.Roots[0].Path == "/home/ubuntu/.broken" {
				return errors.New("broken restore")
			}
			return nil
		},
	}
	manager, err := NewManager(
		store,
		guest,
		fakeSessionAdapter{
			toolID:       "broken",
			authMethodID: "broken-session",
			spec: SessionStateSpec{
				Version: 1,
				Roots: []SessionStateRoot{{
					Path:          "/home/ubuntu/.broken",
					Kind:          SessionStateRootKindDirectory,
					DirectoryMode: 0o700,
				}},
			},
		},
		CodexChatGPTAdapter{},
	)
	if err != nil {
		t.Fatal(err)
	}

	err = manager.RestoreAll(ctx, "user-1", "space-shooter", "ubuntu")
	if err == nil || !strings.Contains(err.Error(), "broken restore") {
		t.Fatalf("expected joined restore error, got %v", err)
	}
	if len(restoredRoots) != 2 {
		t.Fatalf("expected both restore attempts, got %+v", restoredRoots)
	}
}

func TestNewManagerRejectsUnsupportedAdapter(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	guest := &fakeGuestTransport{}
	if _, err := NewManager(store, guest, fakeUnsupportedAdapter{}); err == nil || !strings.Contains(err.Error(), "unsupported tool auth adapter") {
		t.Fatalf("expected unsupported adapter error, got %v", err)
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()

	root := t.TempDir()
	cfg := config.Config{
		ToolAuthDir:     filepath.Join(root, "tool-auth"),
		ToolAuthKeyPath: filepath.Join(root, "tool-auth.key"),
	}
	store, err := NewStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func archiveWithFile(t *testing.T, path, body string) []byte {
	t.Helper()

	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)

	payload := []byte(body)
	if err := tarWriter.WriteHeader(&tar.Header{
		Name: path,
		Mode: 0o600,
		Size: int64(len(payload)),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}

	return buffer.Bytes()
}
