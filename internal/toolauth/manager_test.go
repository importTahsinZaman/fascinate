package toolauth

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"testing"

	"fascinate/internal/config"
)

type fakeGuestTransport struct {
	captureRuntime string
	captureSpec    SessionStateSpec
	captureArchive []byte
	captureErr     error

	restoreRuntime string
	restoreSpec    SessionStateSpec
	restoreArchive []byte
	restoreErr     error
}

func (f *fakeGuestTransport) CaptureSessionState(_ context.Context, runtimeName string, spec SessionStateSpec) ([]byte, error) {
	f.captureRuntime = runtimeName
	f.captureSpec = spec
	if f.captureErr != nil {
		return nil, f.captureErr
	}
	return f.captureArchive, nil
}

func (f *fakeGuestTransport) RestoreSessionState(_ context.Context, runtimeName string, spec SessionStateSpec, archive []byte) error {
	f.restoreRuntime = runtimeName
	f.restoreSpec = spec
	f.restoreArchive = append([]byte(nil), archive...)
	return f.restoreErr
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
