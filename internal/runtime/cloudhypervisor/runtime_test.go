package cloudhypervisor

import (
	"context"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fascinate/internal/config"
)

func TestPrepareNetworkMetadataSkipsUsedNamespaceAddresses(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	manager := &Manager{
		stateDir:        stateDir,
		guestPrefix:     mustPrefix(t, "10.42.0.0/24"),
		bridgePrefix:    mustPrefix(t, "10.42.0.1/24"),
		namespacePrefix: mustPrefix(t, "100.96.0.0/16"),
	}

	if err := os.MkdirAll(filepath.Join(stateDir, "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := manager.storeMetadata(metadata{
		Name:              "alpha",
		IPv4:              "10.42.0.2",
		GuestUser:         "ubuntu",
		TapDevice:         "tap0",
		HostVethIPv4:      "100.96.0.1",
		NamespaceName:     "fscnsalpha",
		BridgeName:        "br0",
		HostVethName:      "fsvalpha",
		NamespaceVethName: "uplink0",
		DiskPath:          "/tmp/alpha.qcow2",
		SeedPath:          "/tmp/alpha.seed",
		LogPath:           "/tmp/alpha.log",
		SocketPath:        "/tmp/alpha.sock",
	}); err != nil {
		t.Fatal(err)
	}

	_, _, hostIPv4, namespaceIPv4, mac, err := manager.prepareNetworkMetadata("beta")
	if err != nil {
		t.Fatal(err)
	}
	if hostIPv4 != "100.96.0.5" {
		t.Fatalf("expected next free host veth IP to be 100.96.0.5, got %q", hostIPv4)
	}
	if namespaceIPv4 != "100.96.0.6" {
		t.Fatalf("expected next free namespace uplink IP to be 100.96.0.6, got %q", namespaceIPv4)
	}
	if mac != "02:fc:0a:2a:00:02" {
		t.Fatalf("unexpected fixed guest MAC %q", mac)
	}
}

func TestLoadOrCreateGuestSSHKeyWritesPublicKey(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "guest_ssh_ed25519")
	publicKey, err := loadOrCreateGuestSSHKey(path)
	if err != nil {
		t.Fatal(err)
	}
	if publicKey == "" {
		t.Fatalf("expected public key to be generated")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected private key file, got %v", err)
	}
	if _, err := os.Stat(path + ".pub"); err != nil {
		t.Fatalf("expected public key file, got %v", err)
	}
}

func TestMachineFromMetadataUsesRunningState(t *testing.T) {
	t.Parallel()

	manager := &Manager{}
	running := manager.machineFromMetadata(metadata{
		Name:      "alpha",
		CPU:       "1",
		Memory:    "2GiB",
		Disk:      "20GiB",
		IPv4:      "10.42.0.10",
		GuestUser: "ubuntu",
		ProcessID: os.Getpid(),
	})
	if running.State != "RUNNING" {
		t.Fatalf("expected running state, got %q", running.State)
	}

	stopped := manager.machineFromMetadata(metadata{
		Name:      "beta",
		CPU:       "1",
		Memory:    "2GiB",
		Disk:      "20GiB",
		IPv4:      "10.42.0.11",
		GuestUser: "ubuntu",
		ProcessID: 999999,
	})
	if stopped.State != "STOPPED" {
		t.Fatalf("expected stopped state, got %q", stopped.State)
	}
}

func TestNewUsesVMDefaults(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	cfg := config.Config{
		DataDir:            dataDir,
		RuntimeStateDir:    filepath.Join(dataDir, "machines"),
		RuntimeSnapshotDir: filepath.Join(dataDir, "snapshots"),
		VMBridgeCIDR:       "10.42.0.1/24",
		VMGuestCIDR:        "10.42.0.0/24",
		VMNamespaceCIDR:    "100.96.0.0/16",
		GuestSSHKeyPath:    filepath.Join(dataDir, "guest_ssh_ed25519"),
		GuestSSHUser:       "ubuntu",
		RuntimeBinary:      "cloud-hypervisor",
		QemuImgBinary:      "qemu-img",
		CloudLocalDSBinary: "cloud-localds",
		VMFirmwarePath:     "/usr/local/share/cloud-hypervisor/CLOUDHV.fd",
	}

	manager, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if manager.now == nil || manager.waitForGuest == nil {
		t.Fatalf("expected runtime helpers to be configured")
	}
	if manager.defaultGuestUser != "ubuntu" {
		t.Fatalf("unexpected guest user %q", manager.defaultGuestUser)
	}
}

func TestTapDeviceNameIsStableAndHashed(t *testing.T) {
	t.Parallel()

	first := tapDeviceName("tic-tac-toe")
	second := tapDeviceName("tic_tac_toe")
	other := tapDeviceName("tic-tac-toes")

	if first != second {
		t.Fatalf("expected equivalent names to hash the same, got %q vs %q", first, second)
	}
	if first == other {
		t.Fatalf("expected different names to avoid collisions, got %q", first)
	}
	if len(first) > 15 {
		t.Fatalf("tap device name too long: %q", first)
	}
}

func TestHostVethNameIsStableAndAvoidsPrefixCollisions(t *testing.T) {
	t.Parallel()

	first := hostVethName("tic-tac-toe")
	second := hostVethName("tic_tac_toe")
	other := hostVethName("tic-tac-toe-v2")

	if first != second {
		t.Fatalf("expected equivalent names to hash the same, got %q vs %q", first, second)
	}
	if first == other {
		t.Fatalf("expected different names to avoid collisions, got %q", first)
	}
	if len(first) > 15 {
		t.Fatalf("host veth name too long: %q", first)
	}
}

func TestCloudInitUserDataInstallsCanonicalAgentDocs(t *testing.T) {
	t.Parallel()

	userData := cloudInitUserData(metadata{
		Name:        "tic-tac-toe",
		PrimaryPort: 3000,
		GuestUser:   "ubuntu",
	}, "fascinate.dev", "ssh-ed25519 AAAATEST fascinate")

	for _, snippet := range []string{
		"/etc/fascinate/AGENTS.md",
		"/etc/claude-code/CLAUDE.md",
		"/root/.claude/CLAUDE.md",
		"/root/.codex/AGENTS.md",
		"/home/ubuntu/.claude/CLAUDE.md",
		"/home/ubuntu/.codex/AGENTS.md",
		"chown ubuntu:ubuntu /home/ubuntu/.claude /home/ubuntu/.codex || true",
		"@openai/codex@latest",
		"apt-get install -y build-essential ca-certificates curl docker.io file fzf gh git",
		"https://tic-tac-toe.fascinate.dev",
		"add this hostname to allowedDevOrigins",
		"gh auth login",
		"gh auth setup-git",
	} {
		if !strings.Contains(userData, snippet) {
			t.Fatalf("expected cloud-init user-data to contain %q", snippet)
		}
	}
}

func TestNormalizeAPIPathAddsCloudHypervisorPrefix(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"":                 "/api/v1",
		"vm.pause":         "/api/v1/vm.pause",
		"/vm.pause":        "/api/v1/vm.pause",
		"/api/v1/vm.pause": "/api/v1/vm.pause",
	}

	for input, want := range cases {
		if got := normalizeAPIPath(input); got != want {
			t.Fatalf("normalizeAPIPath(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestCopyDiskPrefersSparseFilesystemCopy(t *testing.T) {
	tempDir := t.TempDir()
	argsPath := filepath.Join(tempDir, "args.txt")
	sourcePath := filepath.Join(tempDir, "source.qcow2")
	targetPath := filepath.Join(tempDir, "target.qcow2")
	scriptPath := filepath.Join(tempDir, "cp")

	if err := os.WriteFile(sourcePath, []byte("disk-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$@\" > \"" + argsPath + "\"\n" +
		"/bin/cp \"$3\" \"$4\"\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tempDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	manager := &Manager{}
	if err := manager.copyDisk(context.Background(), sourcePath, targetPath); err != nil {
		t.Fatal(err)
	}

	targetBytes, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(targetBytes) != "disk-bytes" {
		t.Fatalf("expected copied disk bytes, got %q", string(targetBytes))
	}

	argsBytes, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Fields(string(argsBytes))
	want := []string{"--reflink=auto", "--sparse=always", sourcePath, targetPath}
	if len(got) != len(want) {
		t.Fatalf("unexpected cp args %q", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("cp arg %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestStoreSnapshotMetadataAtWritesSnapshotFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	meta := snapshotMetadata{
		Name:           "snapshot-one",
		State:          "READY",
		RuntimeVersion: "cloud-hypervisor 46.0",
	}

	if err := storeSnapshotMetadataAt(dir, meta); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(filepath.Join(dir, snapshotFileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"name": "snapshot-one"`) {
		t.Fatalf("unexpected snapshot metadata body: %s", string(body))
	}
}

func TestRestoreArgOmitsUnsupportedResumeFlag(t *testing.T) {
	t.Parallel()

	arg := restoreArg("/var/lib/fascinate/machines/example/restore")
	if !strings.Contains(arg, "source_url=file:///var/lib/fascinate/machines/example/restore") {
		t.Fatalf("unexpected restore arg %q", arg)
	}
	if strings.Contains(arg, "resume=") {
		t.Fatalf("restore arg should not include resume flag: %q", arg)
	}
}

func TestRewriteSnapshotConfigUpdatesTempArtifactPaths(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	body := `{"disks":[{"path":"/tmp/snapshot-artifact.tmp/disk.qcow2"},{"path":"/tmp/snapshot-artifact.tmp/seed.img"}]}`
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := rewriteSnapshotConfig(configPath, map[string]string{
		"/tmp/snapshot-artifact.tmp/disk.qcow2": "/var/lib/fascinate/snapshots/snapshot-one/disk.qcow2",
		"/tmp/snapshot-artifact.tmp/seed.img":   "/var/lib/fascinate/snapshots/snapshot-one/seed.img",
	}); err != nil {
		t.Fatal(err)
	}

	rewritten, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(rewritten)
	if !strings.Contains(text, "/var/lib/fascinate/snapshots/snapshot-one/disk.qcow2") {
		t.Fatalf("expected rewritten disk path in %s", text)
	}
	if !strings.Contains(text, "/var/lib/fascinate/snapshots/snapshot-one/seed.img") {
		t.Fatalf("expected rewritten seed path in %s", text)
	}
	if strings.Contains(text, "/tmp/snapshot-artifact.tmp/") {
		t.Fatalf("expected temp artifact paths to be removed from %s", text)
	}
}

func mustPrefix(t *testing.T, value string) netip.Prefix {
	t.Helper()
	prefix, err := netip.ParsePrefix(value)
	if err != nil {
		t.Fatal(err)
	}
	return prefix
}
