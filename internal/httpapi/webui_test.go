package httpapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"fascinate/internal/config"
)

func TestWebUIHandlerServesPublishedInstallerAndCLIAssets(t *testing.T) {
	publicDir := t.TempDir()
	distDir := t.TempDir()

	writeTestFile(t, filepath.Join(publicDir, "install.sh"), "#!/usr/bin/env bash\necho install\n")
	writeTestFile(t, filepath.Join(publicDir, "cli", "index.json"), `{"latestVersion":"1.2.3"}`+"\n")
	writeTestFile(t, filepath.Join(distDir, "index.html"), "<html>app</html>\n")

	handler := newWebUIHandler(config.Config{
		WebDistDir:      distDir,
		PublicAssetsDir: publicDir,
	})

	installReq := httptest.NewRequest(http.MethodGet, "/install.sh", nil)
	installRec := httptest.NewRecorder()
	handler.ServeHTTP(installRec, installReq)
	if installRec.Code != http.StatusOK {
		t.Fatalf("expected installer status 200, got %d", installRec.Code)
	}
	if got := installRec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("expected installer cache-control no-store, got %q", got)
	}
	if body := installRec.Body.String(); body != "#!/usr/bin/env bash\necho install\n" {
		t.Fatalf("unexpected installer body %q", body)
	}

	indexReq := httptest.NewRequest(http.MethodGet, "/cli/index.json", nil)
	indexRec := httptest.NewRecorder()
	handler.ServeHTTP(indexRec, indexReq)
	if indexRec.Code != http.StatusOK {
		t.Fatalf("expected cli index status 200, got %d", indexRec.Code)
	}
	if got := indexRec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("expected index cache-control no-store, got %q", got)
	}
	if body := indexRec.Body.String(); body != "{\"latestVersion\":\"1.2.3\"}\n" {
		t.Fatalf("unexpected index body %q", body)
	}

	appReq := httptest.NewRequest(http.MethodGet, "/app", nil)
	appRec := httptest.NewRecorder()
	handler.ServeHTTP(appRec, appReq)
	if appRec.Code != http.StatusOK {
		t.Fatalf("expected app status 200, got %d", appRec.Code)
	}
	if body := appRec.Body.String(); body != "<html>app</html>\n" {
		t.Fatalf("unexpected app body %q", body)
	}
}

func TestWebUIHandlerReturnsNotFoundForMissingPublishedAsset(t *testing.T) {
	handler := newWebUIHandler(config.Config{
		PublicAssetsDir: t.TempDir(),
	})

	req := httptest.NewRequest(http.MethodGet, "/install.sh", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", rec.Code)
	}
}

func writeTestFile(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer file.Close()
	if _, err := io.WriteString(file, body); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
