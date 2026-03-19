package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr           string
	SSHAddr            string
	DataDir            string
	DBPath             string
	BaseDomain         string
	AdminEmails        []string
	RuntimeBinary      string
	RuntimeStateDir    string
	VMBridgeName       string
	VMBridgeCIDR       string
	VMGuestCIDR        string
	VMFirmwarePath     string
	QemuImgBinary      string
	CloudLocalDSBinary string
	SSHClientBinary    string
	GuestSSHKeyPath    string
	GuestSSHUser       string
	DefaultImage       string
	DefaultMachineCPU  string
	DefaultMachineRAM  string
	DefaultMachineDisk string
	MaxMachinesPerUser int
	MaxMachineCPU      string
	MaxMachineRAM      string
	MaxMachineDisk     string
	DefaultPrimaryPort int
	SSHHostKeyPath     string
	ResendAPIKey       string
	ResendBaseURL      string
	EmailFrom          string
	SignupCodeTTL      time.Duration
}

func Load() Config {
	dataDir := getenv("FASCINATE_DATA_DIR", "./data")
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
		runtimeBinary = getenv("FASCINATE_INCUS_BINARY", "cloud-hypervisor")
	}
	runtimeStateDir := getenv("FASCINATE_RUNTIME_STATE_DIR", "")
	if runtimeStateDir == "" {
		runtimeStateDir = filepath.Join(dataDir, "machines")
	}
	guestSSHKeyPath := getenv("FASCINATE_GUEST_SSH_KEY_PATH", "")
	if guestSSHKeyPath == "" {
		guestSSHKeyPath = filepath.Join(dataDir, "guest_ssh_ed25519")
	}

	return Config{
		HTTPAddr:           getenv("FASCINATE_HTTP_ADDR", "127.0.0.1:8080"),
		SSHAddr:            getenv("FASCINATE_SSH_ADDR", "127.0.0.1:2222"),
		DataDir:            dataDir,
		DBPath:             dbPath,
		BaseDomain:         getenv("FASCINATE_BASE_DOMAIN", ""),
		AdminEmails:        splitCSV(getenv("FASCINATE_ADMIN_EMAILS", "")),
		RuntimeBinary:      runtimeBinary,
		RuntimeStateDir:    runtimeStateDir,
		VMBridgeName:       getenv("FASCINATE_VM_BRIDGE_NAME", "fascbr0"),
		VMBridgeCIDR:       getenv("FASCINATE_VM_BRIDGE_CIDR", "10.42.0.1/24"),
		VMGuestCIDR:        getenv("FASCINATE_VM_GUEST_CIDR", "10.42.0.0/24"),
		VMFirmwarePath:     getenv("FASCINATE_VM_FIRMWARE_PATH", "/usr/local/bin/hypervisor-fw"),
		QemuImgBinary:      getenv("FASCINATE_QEMU_IMG_BINARY", "qemu-img"),
		CloudLocalDSBinary: getenv("FASCINATE_CLOUD_LOCALDS_BINARY", "cloud-localds"),
		SSHClientBinary:    getenv("FASCINATE_SSH_CLIENT_BINARY", "ssh"),
		GuestSSHKeyPath:    guestSSHKeyPath,
		GuestSSHUser:       getenv("FASCINATE_GUEST_SSH_USER", "ubuntu"),
		DefaultImage:       defaultImage,
		DefaultMachineCPU:  getenv("FASCINATE_DEFAULT_MACHINE_CPU", "1"),
		DefaultMachineRAM:  getenv("FASCINATE_DEFAULT_MACHINE_RAM", "2GiB"),
		DefaultMachineDisk: getenv("FASCINATE_DEFAULT_MACHINE_DISK", "20GiB"),
		MaxMachinesPerUser: getenvInt("FASCINATE_MAX_MACHINES_PER_USER", 3),
		MaxMachineCPU:      getenv("FASCINATE_MAX_MACHINE_CPU", "2"),
		MaxMachineRAM:      getenv("FASCINATE_MAX_MACHINE_RAM", "4GiB"),
		MaxMachineDisk:     getenv("FASCINATE_MAX_MACHINE_DISK", "20GiB"),
		DefaultPrimaryPort: getenvInt("FASCINATE_DEFAULT_PRIMARY_PORT", 3000),
		SSHHostKeyPath:     getenv("FASCINATE_SSH_HOST_KEY_PATH", filepath.Join(dataDir, "ssh_host_ed25519_key")),
		ResendAPIKey:       getenv("FASCINATE_RESEND_API_KEY", ""),
		ResendBaseURL:      getenv("FASCINATE_RESEND_BASE_URL", "https://api.resend.com"),
		EmailFrom:          getenv("FASCINATE_EMAIL_FROM", ""),
		SignupCodeTTL:      getenvDuration("FASCINATE_SIGNUP_CODE_TTL", 15*time.Minute),
	}
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
