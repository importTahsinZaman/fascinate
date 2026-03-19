package cloudhypervisor

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/crypto/ssh"

	"fascinate/internal/config"
	machineruntime "fascinate/internal/runtime"
)

const (
	metadataFileName = "machine.json"
	diskFileName     = "disk.qcow2"
	seedFileName     = "seed.img"
	logFileName      = "cloud-hypervisor.log"
	socketFileName   = "cloud-hypervisor.sock"
)

type metadata struct {
	Name         string `json:"name"`
	CPU          string `json:"cpu"`
	Memory       string `json:"memory"`
	Disk         string `json:"disk"`
	PrimaryPort  int    `json:"primary_port"`
	IPv4         string `json:"ipv4"`
	GuestUser    string `json:"guest_user"`
	TapDevice    string `json:"tap_device"`
	MACAddress   string `json:"mac_address"`
	DiskPath     string `json:"disk_path"`
	SeedPath     string `json:"seed_path"`
	LogPath      string `json:"log_path"`
	SocketPath   string `json:"socket_path"`
	ProcessID    int    `json:"process_id"`
	CreatedAtUTC string `json:"created_at_utc"`
}

type Manager struct {
	binary            string
	qemuImgBinary     string
	cloudLocalDS      string
	stateDir          string
	bridgeName        string
	bridgePrefix      netip.Prefix
	guestPrefix       netip.Prefix
	firmwarePath      string
	defaultGuestUser  string
	guestSSHKeyPath   string
	guestSSHPublicKey string
	waitForGuest      func(context.Context, string) error
	now               func() time.Time
}

func New(cfg config.Config) (*Manager, error) {
	if err := os.MkdirAll(cfg.RuntimeStateDir, 0o755); err != nil {
		return nil, err
	}

	publicKey, err := loadOrCreateGuestSSHKey(cfg.GuestSSHKeyPath)
	if err != nil {
		return nil, err
	}

	bridgePrefix, err := netip.ParsePrefix(strings.TrimSpace(cfg.VMBridgeCIDR))
	if err != nil {
		return nil, fmt.Errorf("parse VM bridge CIDR: %w", err)
	}
	guestPrefix, err := netip.ParsePrefix(strings.TrimSpace(cfg.VMGuestCIDR))
	if err != nil {
		return nil, fmt.Errorf("parse VM guest CIDR: %w", err)
	}

	manager := &Manager{
		binary:            strings.TrimSpace(cfg.RuntimeBinary),
		qemuImgBinary:     strings.TrimSpace(cfg.QemuImgBinary),
		cloudLocalDS:      strings.TrimSpace(cfg.CloudLocalDSBinary),
		stateDir:          strings.TrimSpace(cfg.RuntimeStateDir),
		bridgeName:        strings.TrimSpace(cfg.VMBridgeName),
		bridgePrefix:      bridgePrefix,
		guestPrefix:       guestPrefix,
		firmwarePath:      strings.TrimSpace(cfg.VMFirmwarePath),
		defaultGuestUser:  strings.TrimSpace(cfg.GuestSSHUser),
		guestSSHKeyPath:   strings.TrimSpace(cfg.GuestSSHKeyPath),
		guestSSHPublicKey: publicKey,
		now:               time.Now,
	}
	manager.waitForGuest = manager.waitForGuestSSH

	if manager.binary == "" {
		manager.binary = "cloud-hypervisor"
	}
	if manager.qemuImgBinary == "" {
		manager.qemuImgBinary = "qemu-img"
	}
	if manager.cloudLocalDS == "" {
		manager.cloudLocalDS = "cloud-localds"
	}
	if manager.defaultGuestUser == "" {
		manager.defaultGuestUser = "ubuntu"
	}

	return manager, nil
}

func (m *Manager) HealthCheck(ctx context.Context) error {
	_, err := m.run(ctx, m.binary, "--version")
	return err
}

func (m *Manager) ListMachines(ctx context.Context) ([]machineruntime.Machine, error) {
	entries, err := os.ReadDir(m.stateDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	machines := make([]machineruntime.Machine, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		meta, err := m.loadMetadata(entry.Name())
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}

		machines = append(machines, m.machineFromMetadata(meta))
	}

	sort.Slice(machines, func(i, j int) bool {
		return machines[i].Name < machines[j].Name
	})

	return machines, nil
}

func (m *Manager) GetMachine(_ context.Context, name string) (machineruntime.Machine, error) {
	meta, err := m.loadMetadata(strings.TrimSpace(name))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return machineruntime.Machine{}, machineruntime.ErrMachineNotFound
		}
		return machineruntime.Machine{}, err
	}

	return m.machineFromMetadata(meta), nil
}

func (m *Manager) CreateMachine(ctx context.Context, req machineruntime.CreateMachineRequest) (machineruntime.Machine, error) {
	name := strings.TrimSpace(req.Name)
	baseImage := strings.TrimSpace(req.Image)
	if name == "" || baseImage == "" {
		return machineruntime.Machine{}, fmt.Errorf("machine name and image are required")
	}

	machineDir := m.machineDir(name)
	if _, err := os.Stat(machineDir); err == nil {
		return machineruntime.Machine{}, fmt.Errorf("machine %q already exists", name)
	} else if !errors.Is(err, os.ErrNotExist) {
		return machineruntime.Machine{}, err
	}
	if err := os.MkdirAll(machineDir, 0o755); err != nil {
		return machineruntime.Machine{}, err
	}

	meta, err := m.prepareMetadata(name, req)
	if err != nil {
		_ = os.RemoveAll(machineDir)
		return machineruntime.Machine{}, err
	}

	if err := m.createOverlayDisk(ctx, baseImage, meta.DiskPath, meta.Disk); err != nil {
		_ = os.RemoveAll(machineDir)
		return machineruntime.Machine{}, err
	}
	if err := m.writeSeedImage(ctx, meta); err != nil {
		_ = os.RemoveAll(machineDir)
		return machineruntime.Machine{}, err
	}
	if err := m.createTapDevice(ctx, meta.TapDevice); err != nil {
		_ = os.RemoveAll(machineDir)
		return machineruntime.Machine{}, err
	}
	if err := m.startVM(ctx, meta); err != nil {
		_ = m.cleanupMachine(context.Background(), meta)
		return machineruntime.Machine{}, err
	}
	if err := m.storeMetadata(meta); err != nil {
		_ = m.cleanupMachine(context.Background(), meta)
		return machineruntime.Machine{}, err
	}
	if err := m.waitForGuest(ctx, meta.IPv4); err != nil {
		_ = m.cleanupMachine(context.Background(), meta)
		return machineruntime.Machine{}, err
	}

	return m.machineFromMetadata(meta), nil
}

func (m *Manager) DeleteMachine(ctx context.Context, name string) error {
	meta, err := m.loadMetadata(strings.TrimSpace(name))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return machineruntime.ErrMachineNotFound
		}
		return err
	}

	return m.cleanupMachine(ctx, meta)
}

func (m *Manager) CloneMachine(ctx context.Context, req machineruntime.CloneMachineRequest) (machineruntime.Machine, error) {
	sourceName := strings.TrimSpace(req.SourceName)
	targetName := strings.TrimSpace(req.TargetName)
	if sourceName == "" || targetName == "" {
		return machineruntime.Machine{}, fmt.Errorf("source and target names are required")
	}

	source, err := m.loadMetadata(sourceName)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return machineruntime.Machine{}, machineruntime.ErrMachineNotFound
		}
		return machineruntime.Machine{}, err
	}

	targetDir := m.machineDir(targetName)
	if _, err := os.Stat(targetDir); err == nil {
		return machineruntime.Machine{}, fmt.Errorf("machine %q already exists", targetName)
	} else if !errors.Is(err, os.ErrNotExist) {
		return machineruntime.Machine{}, err
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return machineruntime.Machine{}, err
	}

	target := source
	target.Name = targetName
	target.ProcessID = 0
	target.TapDevice = tapDeviceName(targetName)
	target.DiskPath = filepath.Join(targetDir, diskFileName)
	target.SeedPath = filepath.Join(targetDir, seedFileName)
	target.LogPath = filepath.Join(targetDir, logFileName)
	target.SocketPath = filepath.Join(targetDir, socketFileName)
	target.CreatedAtUTC = m.now().UTC().Format(time.RFC3339)
	target.IPv4, err = m.allocateIPv4(targetName)
	if err != nil {
		_ = os.RemoveAll(targetDir)
		return machineruntime.Machine{}, err
	}
	target.MACAddress = macFromIPv4(target.IPv4)
	if strings.TrimSpace(req.RootDiskSize) != "" {
		target.Disk = strings.TrimSpace(req.RootDiskSize)
	}

	if err := m.copyDisk(ctx, source.DiskPath, target.DiskPath); err != nil {
		_ = os.RemoveAll(targetDir)
		return machineruntime.Machine{}, err
	}
	if err := m.resizeDisk(ctx, target.DiskPath, target.Disk); err != nil {
		_ = os.RemoveAll(targetDir)
		return machineruntime.Machine{}, err
	}
	if err := m.writeSeedImage(ctx, target); err != nil {
		_ = os.RemoveAll(targetDir)
		return machineruntime.Machine{}, err
	}
	if err := m.createTapDevice(ctx, target.TapDevice); err != nil {
		_ = os.RemoveAll(targetDir)
		return machineruntime.Machine{}, err
	}
	if err := m.startVM(ctx, target); err != nil {
		_ = m.cleanupMachine(context.Background(), target)
		return machineruntime.Machine{}, err
	}
	if err := m.storeMetadata(target); err != nil {
		_ = m.cleanupMachine(context.Background(), target)
		return machineruntime.Machine{}, err
	}
	if err := m.waitForGuest(ctx, target.IPv4); err != nil {
		_ = m.cleanupMachine(context.Background(), target)
		return machineruntime.Machine{}, err
	}

	return m.machineFromMetadata(target), nil
}

func (m *Manager) prepareMetadata(name string, req machineruntime.CreateMachineRequest) (metadata, error) {
	ipv4, err := m.allocateIPv4(name)
	if err != nil {
		return metadata{}, err
	}

	machineDir := m.machineDir(name)
	disk := strings.TrimSpace(req.RootDiskSize)
	if disk == "" {
		disk = "20GiB"
	}

	return metadata{
		Name:         name,
		CPU:          strings.TrimSpace(req.CPU),
		Memory:       strings.TrimSpace(req.Memory),
		Disk:         disk,
		PrimaryPort:  req.PrimaryPort,
		IPv4:         ipv4,
		GuestUser:    m.defaultGuestUser,
		TapDevice:    tapDeviceName(name),
		MACAddress:   macFromIPv4(ipv4),
		DiskPath:     filepath.Join(machineDir, diskFileName),
		SeedPath:     filepath.Join(machineDir, seedFileName),
		LogPath:      filepath.Join(machineDir, logFileName),
		SocketPath:   filepath.Join(machineDir, socketFileName),
		CreatedAtUTC: m.now().UTC().Format(time.RFC3339),
	}, nil
}

func (m *Manager) machineDir(name string) string {
	return filepath.Join(m.stateDir, strings.TrimSpace(name))
}

func (m *Manager) machineFromMetadata(meta metadata) machineruntime.Machine {
	state := "STOPPED"
	if meta.ProcessID > 0 && processAlive(meta.ProcessID) {
		state = "RUNNING"
	}

	return machineruntime.Machine{
		Name:      meta.Name,
		Type:      "vm",
		State:     state,
		CPU:       meta.CPU,
		Memory:    meta.Memory,
		Disk:      meta.Disk,
		IPv4:      []string{meta.IPv4},
		GuestUser: meta.GuestUser,
	}
}

func (m *Manager) loadMetadata(name string) (metadata, error) {
	var meta metadata
	body, err := os.ReadFile(filepath.Join(m.machineDir(name), metadataFileName))
	if err != nil {
		return metadata{}, err
	}
	if err := json.Unmarshal(body, &meta); err != nil {
		return metadata{}, err
	}
	return meta, nil
}

func (m *Manager) storeMetadata(meta metadata) error {
	body, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(m.machineDir(meta.Name), metadataFileName), body, 0o600)
}

func (m *Manager) allocateIPv4(targetName string) (string, error) {
	used := map[netip.Addr]struct{}{}
	entries, err := os.ReadDir(m.stateDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if entry.Name() == targetName {
			continue
		}
		meta, err := m.loadMetadata(entry.Name())
		if err != nil {
			continue
		}
		addr, err := netip.ParseAddr(meta.IPv4)
		if err != nil {
			continue
		}
		used[addr] = struct{}{}
	}

	start := m.guestPrefix.Addr()
	for i := 10; i < 255; i++ {
		addr := advanceAddr(start, i)
		if !m.guestPrefix.Contains(addr) {
			break
		}
		if _, ok := used[addr]; ok {
			continue
		}
		return addr.String(), nil
	}

	return "", fmt.Errorf("no free guest IP addresses remain in %s", m.guestPrefix.String())
}

func (m *Manager) createOverlayDisk(ctx context.Context, baseImage, diskPath, size string) error {
	if _, err := os.Stat(baseImage); err != nil {
		return fmt.Errorf("base image %q is not available: %w", baseImage, err)
	}
	args := []string{
		"create",
		"-f", "qcow2",
		"-F", "qcow2",
		"-b", baseImage,
		diskPath,
		strings.TrimSpace(size),
	}
	_, err := m.run(ctx, m.qemuImgBinary, args...)
	return err
}

func (m *Manager) copyDisk(ctx context.Context, sourcePath, targetPath string) error {
	_, err := m.run(ctx, m.qemuImgBinary, "convert", "-O", "qcow2", sourcePath, targetPath)
	return err
}

func (m *Manager) resizeDisk(ctx context.Context, diskPath, size string) error {
	if strings.TrimSpace(size) == "" {
		return nil
	}
	_, err := m.run(ctx, m.qemuImgBinary, "resize", diskPath, strings.TrimSpace(size))
	return err
}

func (m *Manager) writeSeedImage(ctx context.Context, meta metadata) error {
	userData := cloudInitUserData(meta, m.guestSSHPublicKey)
	metaData := fmt.Sprintf("instance-id: fascinate-%s\nlocal-hostname: %s\n", meta.Name, meta.Name)
	networkConfig := cloudInitNetworkConfig(meta.IPv4, m.guestPrefix, m.bridgePrefix.Addr())

	dir := m.machineDir(meta.Name)
	userDataPath := filepath.Join(dir, "user-data")
	metaDataPath := filepath.Join(dir, "meta-data")
	networkPath := filepath.Join(dir, "network-config")
	if err := os.WriteFile(userDataPath, []byte(userData), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(metaDataPath, []byte(metaData), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(networkPath, []byte(networkConfig), 0o600); err != nil {
		return err
	}

	_, err := m.run(ctx, m.cloudLocalDS, "--network-config", networkPath, meta.SeedPath, userDataPath, metaDataPath)
	return err
}

func (m *Manager) createTapDevice(ctx context.Context, tapName string) error {
	if _, err := m.run(ctx, "ip", "tuntap", "add", "dev", tapName, "mode", "tap"); err != nil {
		return err
	}
	if _, err := m.run(ctx, "ip", "link", "set", tapName, "master", m.bridgeName); err != nil {
		return err
	}
	if _, err := m.run(ctx, "ip", "link", "set", tapName, "up"); err != nil {
		return err
	}
	return nil
}

func (m *Manager) startVM(ctx context.Context, meta metadata) error {
	logFile, err := os.OpenFile(meta.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer logFile.Close()

	memoryArg, err := cloudHypervisorMemoryArg(meta.Memory)
	if err != nil {
		return err
	}
	cpuArg := "boot=" + strings.TrimSpace(meta.CPU)
	args := []string{
		"--api-socket", meta.SocketPath,
		"--cpus", cpuArg,
		"--memory", memoryArg,
		"--firmware", m.firmwarePath,
		"--serial", "tty",
		"--disk", "path=" + meta.DiskPath,
		"--disk", "path=" + meta.SeedPath + ",readonly=on",
		"--net", "tap=" + meta.TapDevice + ",mac=" + meta.MACAddress,
	}

	cmd := exec.CommandContext(ctx, m.binary, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return err
	}

	meta.ProcessID = cmd.Process.Pid
	if err := cmd.Process.Release(); err != nil {
		return err
	}

	return m.storeMetadata(meta)
}

func (m *Manager) cleanupMachine(ctx context.Context, meta metadata) error {
	if meta.ProcessID > 0 {
		if pgid, err := syscall.Getpgid(meta.ProcessID); err == nil {
			_ = syscall.Kill(-pgid, syscall.SIGTERM)
			time.Sleep(2 * time.Second)
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
		} else {
			_ = syscall.Kill(meta.ProcessID, syscall.SIGTERM)
			time.Sleep(2 * time.Second)
			_ = syscall.Kill(meta.ProcessID, syscall.SIGKILL)
		}
	}

	_, _ = m.run(ctx, "ip", "link", "set", meta.TapDevice, "down")
	_, _ = m.run(ctx, "ip", "tuntap", "del", "dev", meta.TapDevice, "mode", "tap")

	if err := os.RemoveAll(m.machineDir(meta.Name)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	return nil
}

func (m *Manager) waitForGuestSSH(ctx context.Context, ipv4 string) error {
	address := net.JoinHostPort(strings.TrimSpace(ipv4), "22")
	deadline := time.Now().Add(2 * time.Minute)
	for {
		dialer := net.Dialer{Timeout: 3 * time.Second}
		conn, err := dialer.DialContext(ctx, "tcp", address)
		if err == nil {
			_ = conn.Close()
			return nil
		}

		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for guest SSH on %s", address)
		}
		time.Sleep(2 * time.Second)
	}
}

func (m *Manager) run(ctx context.Context, binary string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, binary, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w: %s", binary, strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func cloudInitUserData(meta metadata, publicKey string) string {
	return fmt.Sprintf(`#cloud-config
preserve_hostname: false
hostname: %s
users:
  - default
  - name: %s
    shell: /bin/bash
    sudo: ALL=(ALL) NOPASSWD:ALL
    groups: [adm, sudo, docker]
    ssh_authorized_keys:
      - %s
ssh_pwauth: false
disable_root: true
`, meta.Name, meta.GuestUser, strings.TrimSpace(publicKey))
}

func cloudInitNetworkConfig(ipv4 string, guestPrefix netip.Prefix, gateway netip.Addr) string {
	return fmt.Sprintf(`version: 2
ethernets:
  eth0:
    dhcp4: false
    addresses:
      - %s/%d
    routes:
      - to: default
        via: %s
    nameservers:
      addresses: [1.1.1.1, 1.0.0.1]
`, ipv4, guestPrefix.Bits(), gateway.String())
}

func cloudHypervisorMemoryArg(value string) (string, error) {
	bytes, err := parseByteSize(value)
	if err != nil {
		return "", err
	}
	mebibytes := bytes / (1024 * 1024)
	if mebibytes <= 0 {
		return "", fmt.Errorf("memory must be positive")
	}
	return fmt.Sprintf("size=%dM", mebibytes), nil
}

func parseByteSize(value string) (int64, error) {
	value = strings.TrimSpace(strings.ToUpper(value))
	if value == "" {
		return 0, fmt.Errorf("size value is required")
	}

	units := []struct {
		suffix string
		scale  int64
	}{
		{"TIB", 1024 * 1024 * 1024 * 1024},
		{"TB", 1000 * 1000 * 1000 * 1000},
		{"GIB", 1024 * 1024 * 1024},
		{"GB", 1000 * 1000 * 1000},
		{"MIB", 1024 * 1024},
		{"MB", 1000 * 1000},
		{"KIB", 1024},
		{"KB", 1000},
		{"B", 1},
	}
	for _, unit := range units {
		if !strings.HasSuffix(value, unit.suffix) {
			continue
		}
		number := strings.TrimSpace(strings.TrimSuffix(value, unit.suffix))
		parsed, err := strconv.ParseFloat(number, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid size %q", value)
		}
		return int64(parsed * float64(unit.scale)), nil
	}

	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q", value)
	}
	return int64(parsed), nil
}

func tapDeviceName(name string) string {
	base := strings.NewReplacer("-", "", "_", "").Replace(strings.TrimSpace(name))
	if len(base) > 8 {
		base = base[:8]
	}
	return fmt.Sprintf("fsc%s", base)
}

func macFromIPv4(ipv4 string) string {
	ip := net.ParseIP(strings.TrimSpace(ipv4)).To4()
	if ip == nil {
		return "02:fc:00:00:00:01"
	}
	return fmt.Sprintf("02:fc:%02x:%02x:%02x:%02x", ip[0], ip[1], ip[2], ip[3])
}

func advanceAddr(addr netip.Addr, steps int) netip.Addr {
	current := addr
	for i := 0; i < steps; i++ {
		current = current.Next()
	}
	return current
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func loadOrCreateGuestSSHKey(path string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}

	if body, err := os.ReadFile(path); err == nil {
		signer, err := ssh.ParsePrivateKey(body)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey()))), nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", err
	}

	privateKeyBytes, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return "", err
	}

	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: privateKeyBytes,
	})
	if err := os.WriteFile(path, privateKeyPEM, 0o600); err != nil {
		return "", err
	}

	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		return "", err
	}
	publicKey := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))
	if err := os.WriteFile(path+".pub", []byte(publicKey+"\n"), 0o644); err != nil {
		return "", err
	}

	return publicKey, nil
}
