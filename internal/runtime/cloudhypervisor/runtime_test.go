package cloudhypervisor

import (
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fascinate/internal/config"
)

func TestAllocateIPv4SkipsUsedAddresses(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	manager := &Manager{
		stateDir:     stateDir,
		guestPrefix:  mustPrefix(t, "10.42.0.0/24"),
		bridgePrefix: mustPrefix(t, "10.42.0.1/24"),
	}

	if err := os.MkdirAll(filepath.Join(stateDir, "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := manager.storeMetadata(metadata{
		Name:       "alpha",
		IPv4:       "10.42.0.10",
		GuestUser:  "ubuntu",
		TapDevice:  "tapalpha",
		DiskPath:   "/tmp/alpha.qcow2",
		SeedPath:   "/tmp/alpha.seed",
		LogPath:    "/tmp/alpha.log",
		SocketPath: "/tmp/alpha.sock",
	}); err != nil {
		t.Fatal(err)
	}

	got, err := manager.allocateIPv4("beta")
	if err != nil {
		t.Fatal(err)
	}
	if got != "10.42.0.11" {
		t.Fatalf("expected next free guest IP to be 10.42.0.11, got %q", got)
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

	cfg := config.Config{
		DataDir:            t.TempDir(),
		RuntimeStateDir:    filepath.Join(t.TempDir(), "machines"),
		VMBridgeCIDR:       "10.42.0.1/24",
		VMGuestCIDR:        "10.42.0.0/24",
		GuestSSHKeyPath:    filepath.Join(t.TempDir(), "guest_ssh_ed25519"),
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
		"https://tic-tac-toe.fascinate.dev",
		"add this hostname to allowedDevOrigins",
	} {
		if !strings.Contains(userData, snippet) {
			t.Fatalf("expected cloud-init user-data to contain %q", snippet)
		}
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
