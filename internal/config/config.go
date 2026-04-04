package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr                string
	DataDir                 string
	DBPath                  string
	WebDistDir              string
	BaseDomain              string
	AdminEmails             []string
	HostID                  string
	HostName                string
	HostRegion              string
	HostRole                string
	HostHeartbeatInterval   time.Duration
	RuntimeBinary           string
	RuntimeStateDir         string
	RuntimeSnapshotDir      string
	VMBridgeName            string
	VMBridgeCIDR            string
	VMGuestCIDR             string
	VMNamespaceCIDR         string
	VMFirmwarePath          string
	QemuImgBinary           string
	CloudLocalDSBinary      string
	SSHClientBinary         string
	GuestSSHKeyPath         string
	GuestSSHUser            string
	DefaultImage            string
	DefaultMachineCPU       string
	DefaultMachineRAM       string
	DefaultMachineDisk      string
	DefaultUserMaxCPU       string
	DefaultUserMaxRAM       string
	DefaultUserMaxDisk      string
	DefaultUserMaxMachines  int
	DefaultUserMaxSnapshots int
	MaxMachinesPerUser      int
	MaxMachineCPU           string
	MaxMachineRAM           string
	MaxMachineDisk          string
	HostMinFreeDisk         string
	DefaultPrimaryPort      int
	ToolAuthDir             string
	ToolAuthKeyPath         string
	ToolAuthSyncInterval    time.Duration
	ResendAPIKey            string
	ResendBaseURL           string
	EmailFrom               string
	SignupCodeTTL           time.Duration
	WebSessionTTL           time.Duration
	WebSessionCookieName    string
	TerminalSessionTTL      time.Duration
}

func Load() Config {
	dataDir := getenv("FASCINATE_DATA_DIR", "./data")
	hostID := getenv("FASCINATE_HOST_ID", "")
	if hostID == "" {
		hostID = "local-host"
	}
	hostName := getenv("FASCINATE_HOST_NAME", "")
	if hostName == "" {
		if systemHost, err := os.Hostname(); err == nil && strings.TrimSpace(systemHost) != "" {
			hostName = strings.TrimSpace(systemHost)
		} else {
			hostName = hostID
		}
	}
	dbPath := getenv("FASCINATE_DB_PATH", "")
	if dbPath == "" {
		dbPath = filepath.Join(dataDir, "fascinate.db")
	}
	defaultImage := getenv("FASCINATE_DEFAULT_IMAGE", "")
	if defaultImage == "" {
		defaultImage = filepath.Join(dataDir, "images", "fascinate-base.raw")
	}
	runtimeBinary := getenv("FASCINATE_RUNTIME_BINARY", "")
	if runtimeBinary == "" {
		runtimeBinary = "cloud-hypervisor"
	}
	runtimeStateDir := getenv("FASCINATE_RUNTIME_STATE_DIR", "")
	if runtimeStateDir == "" {
		runtimeStateDir = filepath.Join(dataDir, "machines")
	}
	runtimeSnapshotDir := getenv("FASCINATE_RUNTIME_SNAPSHOT_DIR", "")
	if runtimeSnapshotDir == "" {
		runtimeSnapshotDir = filepath.Join(dataDir, "snapshots")
	}
	guestSSHKeyPath := getenv("FASCINATE_GUEST_SSH_KEY_PATH", "")
	if guestSSHKeyPath == "" {
		guestSSHKeyPath = filepath.Join(dataDir, "guest_ssh_ed25519")
	}
	toolAuthDir := getenv("FASCINATE_TOOL_AUTH_DIR", "")
	if toolAuthDir == "" {
		toolAuthDir = filepath.Join(dataDir, "tool-auth")
	}
	toolAuthKeyPath := getenv("FASCINATE_TOOL_AUTH_KEY_PATH", "")
	if toolAuthKeyPath == "" {
		toolAuthKeyPath = filepath.Join(dataDir, "tool_auth.key")
	}

	return Config{
		HTTPAddr:                getenv("FASCINATE_HTTP_ADDR", "127.0.0.1:8080"),
		DataDir:                 dataDir,
		DBPath:                  dbPath,
		WebDistDir:              getenv("FASCINATE_WEB_DIST_DIR", "./web/dist"),
		BaseDomain:              getenv("FASCINATE_BASE_DOMAIN", ""),
		AdminEmails:             splitCSV(getenv("FASCINATE_ADMIN_EMAILS", "")),
		HostID:                  hostID,
		HostName:                hostName,
		HostRegion:              getenv("FASCINATE_HOST_REGION", "local"),
		HostRole:                getenv("FASCINATE_HOST_ROLE", "combined"),
		HostHeartbeatInterval:   getenvDuration("FASCINATE_HOST_HEARTBEAT_INTERVAL", 30*time.Second),
		RuntimeBinary:           runtimeBinary,
		RuntimeStateDir:         runtimeStateDir,
		RuntimeSnapshotDir:      runtimeSnapshotDir,
		VMBridgeName:            getenv("FASCINATE_VM_BRIDGE_NAME", "fascbr0"),
		VMBridgeCIDR:            getenv("FASCINATE_VM_BRIDGE_CIDR", "10.42.0.1/24"),
		VMGuestCIDR:             getenv("FASCINATE_VM_GUEST_CIDR", "10.42.0.0/24"),
		VMNamespaceCIDR:         getenv("FASCINATE_VM_NAMESPACE_CIDR", "100.96.0.0/16"),
		VMFirmwarePath:          getenv("FASCINATE_VM_FIRMWARE_PATH", "/usr/local/share/cloud-hypervisor/CLOUDHV.fd"),
		QemuImgBinary:           getenv("FASCINATE_QEMU_IMG_BINARY", "qemu-img"),
		CloudLocalDSBinary:      getenv("FASCINATE_CLOUD_LOCALDS_BINARY", "cloud-localds"),
		SSHClientBinary:         getenv("FASCINATE_SSH_CLIENT_BINARY", "ssh"),
		GuestSSHKeyPath:         guestSSHKeyPath,
		GuestSSHUser:            getenv("FASCINATE_GUEST_SSH_USER", "ubuntu"),
		DefaultImage:            defaultImage,
		DefaultMachineCPU:       getenv("FASCINATE_DEFAULT_MACHINE_CPU", "1"),
		DefaultMachineRAM:       getenv("FASCINATE_DEFAULT_MACHINE_RAM", "2GiB"),
		DefaultMachineDisk:      getenv("FASCINATE_DEFAULT_MACHINE_DISK", "20GiB"),
		DefaultUserMaxCPU:       getenv("FASCINATE_DEFAULT_USER_MAX_CPU", "2"),
		DefaultUserMaxRAM:       getenv("FASCINATE_DEFAULT_USER_MAX_RAM", "8GiB"),
		DefaultUserMaxDisk:      getenv("FASCINATE_DEFAULT_USER_MAX_DISK", "50GiB"),
		DefaultUserMaxMachines:  getenvInt("FASCINATE_DEFAULT_USER_MAX_MACHINES", getenvInt("FASCINATE_MAX_MACHINES_PER_USER", 25)),
		DefaultUserMaxSnapshots: getenvInt("FASCINATE_DEFAULT_USER_MAX_SNAPSHOTS", 5),
		MaxMachinesPerUser:      getenvInt("FASCINATE_MAX_MACHINES_PER_USER", 25),
		MaxMachineCPU:           getenv("FASCINATE_MAX_MACHINE_CPU", "2"),
		MaxMachineRAM:           getenv("FASCINATE_MAX_MACHINE_RAM", "4GiB"),
		MaxMachineDisk:          getenv("FASCINATE_MAX_MACHINE_DISK", "20GiB"),
		HostMinFreeDisk:         getenv("FASCINATE_HOST_MIN_FREE_DISK", "150GiB"),
		DefaultPrimaryPort:      getenvInt("FASCINATE_DEFAULT_PRIMARY_PORT", 3000),
		ToolAuthDir:             toolAuthDir,
		ToolAuthKeyPath:         toolAuthKeyPath,
		ToolAuthSyncInterval:    getenvDuration("FASCINATE_TOOL_AUTH_SYNC_INTERVAL", 2*time.Minute),
		ResendAPIKey:            getenv("FASCINATE_RESEND_API_KEY", ""),
		ResendBaseURL:           getenv("FASCINATE_RESEND_BASE_URL", "https://api.resend.com"),
		EmailFrom:               getenv("FASCINATE_EMAIL_FROM", ""),
		SignupCodeTTL:           getenvDuration("FASCINATE_SIGNUP_CODE_TTL", 15*time.Minute),
		WebSessionTTL:           getenvDuration("FASCINATE_WEB_SESSION_TTL", 30*24*time.Hour),
		WebSessionCookieName:    getenv("FASCINATE_WEB_SESSION_COOKIE_NAME", "fascinate_session"),
		TerminalSessionTTL:      getenvDuration("FASCINATE_TERMINAL_SESSION_TTL", 5*time.Minute),
	}
}

func (c Config) EffectiveDefaultUserMaxMachines() int {
	if c.DefaultUserMaxMachines > 0 {
		return c.DefaultUserMaxMachines
	}
	if c.MaxMachinesPerUser > 0 {
		return c.MaxMachinesPerUser
	}
	return 25
}

func getenv(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	return value
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}

	return out
}

func getenvInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}

	return parsed
}

func getenvDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}

	return parsed
}
