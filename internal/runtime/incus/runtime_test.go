package incus

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateMachineSetsRootDiskSize(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "incus.log")
	binary := writeFakeIncusBinary(t, logPath, `[{"name":"vmtest","status":"Running","type":"container","config":{"limits.cpu":"1","limits.memory":"2GiB"},"expanded_devices":{"root":{"size":"20GiB"}},"state":{"network":{}}}]`)
	runtime := NewCLI(binary)

	machine, err := runtime.CreateMachine(context.Background(), CreateMachineRequest{
		Name:         "vmtest",
		Image:        "fascinate-base",
		StoragePool:  "machines",
		CPU:          "1",
		Memory:       "2GiB",
		RootDiskSize: "20GiB",
		PrimaryPort:  3000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if machine.Disk != "20GiB" {
		t.Fatalf("expected disk size 20GiB, got %q", machine.Disk)
	}

	logOutput := readLog(t, logPath)
	if !strings.Contains(logOutput, "config device set vmtest root size 20GiB") {
		t.Fatalf("expected root disk size command, got:\n%s", logOutput)
	}
}

func TestCloneMachineSetsRootDiskSize(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "incus.log")
	binary := writeFakeIncusBinary(t, logPath, `[{"name":"vmtest-clone","status":"Running","type":"container","config":{"limits.cpu":"1","limits.memory":"2GiB"},"expanded_devices":{"root":{"size":"20GiB"}},"state":{"network":{}}}]`)
	runtime := NewCLI(binary)

	machine, err := runtime.CloneMachine(context.Background(), CloneMachineRequest{
		SourceName:   "vmtest",
		TargetName:   "vmtest-clone",
		RootDiskSize: "20GiB",
	})
	if err != nil {
		t.Fatal(err)
	}
	if machine.Disk != "20GiB" {
		t.Fatalf("expected disk size 20GiB, got %q", machine.Disk)
	}

	logOutput := readLog(t, logPath)
	if !strings.Contains(logOutput, "config device set vmtest-clone root size 20GiB") {
		t.Fatalf("expected clone root disk size command, got:\n%s", logOutput)
	}
}

func writeFakeIncusBinary(t *testing.T, logPath, listJSON string) string {
	t.Helper()

	scriptPath := filepath.Join(t.TempDir(), "incus")
	script := `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$INCUS_TEST_LOG"
case "${1:-}" in
  list)
    printf '%s\n' "$INCUS_TEST_LIST_JSON"
    ;;
esac
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("INCUS_TEST_LOG", logPath)
	t.Setenv("INCUS_TEST_LIST_JSON", listJSON)

	return scriptPath
}

func readLog(t *testing.T, logPath string) string {
	t.Helper()

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}

	return string(data)
}
