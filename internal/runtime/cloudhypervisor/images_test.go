package cloudhypervisor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPromoteImageUpdatesCurrentAndPreviousLinks(t *testing.T) {
	t.Parallel()

	manager := newTestImageManager(t)
	writeTestImageCandidate(t, manager, "20260407-01", true)

	first, err := manager.PromoteImage(context.Background(), "20260407-01")
	if err != nil {
		t.Fatal(err)
	}
	if first.ArtifactDir != filepath.Join(manager.imageLayout().releasesDir, "20260407-01") {
		t.Fatalf("unexpected first promoted artifact dir %q", first.ArtifactDir)
	}
	if got := symlinkTarget(manager.imageLayout().currentLink); got != first.ArtifactDir {
		t.Fatalf("expected current symlink %q, got %q", first.ArtifactDir, got)
	}
	if got := symlinkTarget(manager.imageLayout().previousLink); got != "" {
		t.Fatalf("expected previous symlink to be empty after first promote, got %q", got)
	}

	writeTestImageCandidate(t, manager, "20260407-02", true)
	second, err := manager.PromoteImage(context.Background(), "20260407-02")
	if err != nil {
		t.Fatal(err)
	}
	if got := symlinkTarget(manager.imageLayout().currentLink); got != second.ArtifactDir {
		t.Fatalf("expected current symlink %q, got %q", second.ArtifactDir, got)
	}
	if got := symlinkTarget(manager.imageLayout().previousLink); got != first.ArtifactDir {
		t.Fatalf("expected previous symlink %q, got %q", first.ArtifactDir, got)
	}

	status, err := manager.ImageStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.Current == nil || status.Current.Version != "20260407-02" {
		t.Fatalf("expected current version 20260407-02, got %#v", status.Current)
	}
	if status.Previous == nil || status.Previous.Version != "20260407-01" {
		t.Fatalf("expected previous version 20260407-01, got %#v", status.Previous)
	}
}

func TestRollbackImageRepointsCurrentToPreviousRelease(t *testing.T) {
	t.Parallel()

	manager := newTestImageManager(t)
	writeTestImageCandidate(t, manager, "20260407-01", true)
	writeTestImageCandidate(t, manager, "20260407-02", true)
	if _, err := manager.PromoteImage(context.Background(), "20260407-01"); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.PromoteImage(context.Background(), "20260407-02"); err != nil {
		t.Fatal(err)
	}

	result, err := manager.RollbackImage(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Version != "20260407-01" {
		t.Fatalf("expected rollback version 20260407-01, got %q", result.Version)
	}
	if got := symlinkTarget(manager.imageLayout().currentLink); got != filepath.Join(manager.imageLayout().releasesDir, "20260407-01") {
		t.Fatalf("expected current symlink to point at rolled-back image, got %q", got)
	}
	if got := symlinkTarget(manager.imageLayout().previousLink); got != filepath.Join(manager.imageLayout().releasesDir, "20260407-02") {
		t.Fatalf("expected previous symlink to point at former current image, got %q", got)
	}
}

func TestPromoteImageRequiresValidatedCandidate(t *testing.T) {
	t.Parallel()

	manager := newTestImageManager(t)
	writeTestImageCandidate(t, manager, "20260407-01", false)

	if _, err := manager.PromoteImage(context.Background(), "20260407-01"); err == nil {
		t.Fatalf("expected unvalidated candidate promotion to fail")
	}
}

func newTestImageManager(t *testing.T) *Manager {
	t.Helper()

	manager := &Manager{
		imageStoreDir: t.TempDir(),
		now: func() time.Time {
			return time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)
		},
	}
	layout := manager.imageLayout()
	for _, dir := range []string{layout.candidatesDir, layout.releasesDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return manager
}

func writeTestImageCandidate(t *testing.T, manager *Manager, version string, validated bool) {
	t.Helper()

	dir := filepath.Join(manager.imageLayout().candidatesDir, version)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	imagePath := filepath.Join(dir, imageArtifactFileName)
	if err := os.WriteFile(imagePath, []byte("test-image"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := ImageManifest{
		Format:       "fascinate-image-manifest/v1",
		Version:      version,
		State:        "candidate",
		BuiltAt:      "2026-04-07T12:00:00Z",
		ImagePath:    imagePath,
		ManifestPath: filepath.Join(dir, imageManifestFileName),
	}
	if validated {
		manifest.ValidatedAt = "2026-04-07T12:05:00Z"
	}
	if err := writeImageManifest(manifest.ManifestPath, manifest); err != nil {
		t.Fatal(err)
	}
}
