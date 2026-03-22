package cloudhypervisor

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/crypto/ssh"

	"fascinate/internal/config"
	machineruntime "fascinate/internal/runtime"
	"fascinate/internal/toolauth"
)

const (
	metadataFileName = "machine.json"
	snapshotFileName = "snapshot.json"
	diskFileName     = "disk.qcow2"
	seedFileName     = "seed.img"
	logFileName      = "cloud-hypervisor.log"
	socketFileName   = "cloud-hypervisor.sock"
	restoreDirName   = "restore"
	portFileName     = "app-forward.port"
)

type metadata struct {
	MachineID         string `json:"machine_id"`
	Name              string `json:"name"`
	CPU               string `json:"cpu"`
	Memory            string `json:"memory"`
	Disk              string `json:"disk"`
	PrimaryPort       int    `json:"primary_port"`
	IPv4              string `json:"ipv4"`
	GuestGatewayIPv4  string `json:"guest_gateway_ipv4"`
	GuestUser         string `json:"guest_user"`
	TapDevice         string `json:"tap_device"`
	MACAddress        string `json:"mac_address"`
	NamespaceName     string `json:"namespace_name"`
	BridgeName        string `json:"bridge_name"`
	HostVethName      string `json:"host_veth_name"`
	NamespaceVethName string `json:"namespace_veth_name"`
	HostVethIPv4      string `json:"host_veth_ipv4"`
	NamespaceVethIPv4 string `json:"namespace_veth_ipv4"`
	DiskPath          string `json:"disk_path"`
	SeedPath          string `json:"seed_path"`
	LogPath           string `json:"log_path"`
	SocketPath        string `json:"socket_path"`
	RestoreDir        string `json:"restore_dir"`
	AppForwardPort    int    `json:"app_forward_port"`
	AppForwardPID     int    `json:"app_forward_pid"`
	SSHForwardPort    int    `json:"ssh_forward_port"`
	SSHForwardPID     int    `json:"ssh_forward_pid"`
	ProcessID         int    `json:"process_id"`
	CreatedAtUTC      string `json:"created_at_utc"`
}

type snapshotMetadata struct {
	Name              string `json:"name"`
	SourceMachineName string `json:"source_machine_name"`
	State             string `json:"state"`
	CPU               string `json:"cpu"`
	Memory            string `json:"memory"`
	Disk              string `json:"disk"`
	PrimaryPort       int    `json:"primary_port"`
	IPv4              string `json:"ipv4"`
	GuestGatewayIPv4  string `json:"guest_gateway_ipv4"`
	GuestUser         string `json:"guest_user"`
	TapDevice         string `json:"tap_device"`
	MACAddress        string `json:"mac_address"`
	DiskPath          string `json:"disk_path"`
	SeedPath          string `json:"seed_path"`
	RestoreDir        string `json:"restore_dir"`
	DiskSizeBytes     int64  `json:"disk_size_bytes"`
	MemorySizeBytes   int64  `json:"memory_size_bytes"`
	RuntimeVersion    string `json:"runtime_version"`
	FirmwareVersion   string `json:"firmware_version"`
	CreatedAtUTC      string `json:"created_at_utc"`
}

type Manager struct {
	binary            string
	qemuImgBinary     string
	cloudLocalDS      string
	stateDir          string
	snapshotDir       string
	baseDomain        string
	hostID            string
	hostRegion        string
	bridgePrefix      netip.Prefix
	guestPrefix       netip.Prefix
	namespacePrefix   netip.Prefix
	firmwarePath      string
	defaultGuestUser  string
	guestSSHKeyPath   string
	guestSSHPublicKey string
	sshClientBinary   string
	selfBinary        string
	waitForGuest      func(context.Context, metadata) error
	now               func() time.Time
	listHostAddrs     func() ([]netip.Addr, error)
	networkMu         sync.Mutex
}

func New(cfg config.Config) (*Manager, error) {
	if err := os.MkdirAll(cfg.RuntimeStateDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cfg.RuntimeSnapshotDir, 0o755); err != nil {
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
	namespacePrefix, err := netip.ParsePrefix(strings.TrimSpace(cfg.VMNamespaceCIDR))
	if err != nil {
		return nil, fmt.Errorf("parse VM namespace CIDR: %w", err)
	}
	selfBinary, err := os.Executable()
	if err != nil {
		return nil, err
	}

	manager := &Manager{
		binary:            strings.TrimSpace(cfg.RuntimeBinary),
		qemuImgBinary:     strings.TrimSpace(cfg.QemuImgBinary),
		cloudLocalDS:      strings.TrimSpace(cfg.CloudLocalDSBinary),
		stateDir:          strings.TrimSpace(cfg.RuntimeStateDir),
		snapshotDir:       strings.TrimSpace(cfg.RuntimeSnapshotDir),
		baseDomain:        strings.TrimSpace(cfg.BaseDomain),
		hostID:            strings.TrimSpace(cfg.HostID),
		hostRegion:        strings.TrimSpace(cfg.HostRegion),
		bridgePrefix:      bridgePrefix,
		guestPrefix:       guestPrefix,
		namespacePrefix:   namespacePrefix,
		firmwarePath:      strings.TrimSpace(cfg.VMFirmwarePath),
		defaultGuestUser:  strings.TrimSpace(cfg.GuestSSHUser),
		guestSSHKeyPath:   strings.TrimSpace(cfg.GuestSSHKeyPath),
		guestSSHPublicKey: publicKey,
		sshClientBinary:   strings.TrimSpace(cfg.SSHClientBinary),
		selfBinary:        selfBinary,
		now:               time.Now,
		listHostAddrs:     listHostInterfaceAddrs,
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
	if manager.sshClientBinary == "" {
		manager.sshClientBinary = "ssh"
	}
	if manager.defaultGuestUser == "" {
		manager.defaultGuestUser = "ubuntu"
	}
	if manager.hostID == "" {
		manager.hostID = "local-host"
	}
	if manager.hostRegion == "" {
		manager.hostRegion = "local"
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
		if meta.ProcessID > 0 && processAlive(meta.ProcessID) && ((meta.AppForwardPID <= 0 || !processAlive(meta.AppForwardPID)) || (meta.SSHForwardPID <= 0 || !processAlive(meta.SSHForwardPID))) {
			if err := m.startAppForwarder(ctx, &meta); err != nil {
				return nil, err
			}
			if err := m.startSSHForwarder(ctx, &meta); err != nil {
				return nil, err
			}
		}

		machines = append(machines, m.machineFromMetadata(meta))
	}

	sort.Slice(machines, func(i, j int) bool {
		return machines[i].Name < machines[j].Name
	})

	return machines, nil
}

func (m *Manager) GetMachine(ctx context.Context, name string) (machineruntime.Machine, error) {
	meta, err := m.loadMetadata(strings.TrimSpace(name))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return machineruntime.Machine{}, machineruntime.ErrMachineNotFound
		}
		return machineruntime.Machine{}, err
	}
	if meta.ProcessID > 0 && processAlive(meta.ProcessID) && ((meta.AppForwardPID <= 0 || !processAlive(meta.AppForwardPID)) || (meta.SSHForwardPID <= 0 || !processAlive(meta.SSHForwardPID))) {
		if err := m.startAppForwarder(ctx, &meta); err != nil {
			return machineruntime.Machine{}, err
		}
		if err := m.startSSHForwarder(ctx, &meta); err != nil {
			return machineruntime.Machine{}, err
		}
	}

	return m.machineFromMetadata(meta), nil
}

func (m *Manager) CreateMachine(ctx context.Context, req machineruntime.CreateMachineRequest) (machineruntime.Machine, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return machineruntime.Machine{}, fmt.Errorf("machine name is required")
	}
	if snapshotName := strings.TrimSpace(req.Snapshot); snapshotName != "" {
		return m.restoreMachineFromSnapshot(ctx, snapshotName, req)
	}
	baseImage := strings.TrimSpace(req.Image)
	if baseImage == "" {
		return machineruntime.Machine{}, fmt.Errorf("machine image is required")
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

	m.networkMu.Lock()
	networkLocked := true
	defer func() {
		if networkLocked {
			m.networkMu.Unlock()
		}
	}()

	meta, err := m.prepareMetadata(name, req)
	if err != nil {
		_ = os.RemoveAll(machineDir)
		return machineruntime.Machine{}, err
	}
	if err := m.storeMetadata(meta); err != nil {
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
	if err := m.createNamespaceNetwork(ctx, meta); err != nil {
		_ = os.RemoveAll(machineDir)
		return machineruntime.Machine{}, err
	}
	if err := m.startVM(ctx, &meta); err != nil {
		_ = m.cleanupMachine(context.Background(), meta)
		return machineruntime.Machine{}, err
	}
	m.networkMu.Unlock()
	networkLocked = false
	if err := m.startAppForwarder(ctx, &meta); err != nil {
		_ = m.cleanupMachine(context.Background(), meta)
		return machineruntime.Machine{}, err
	}
	if err := m.startSSHForwarder(ctx, &meta); err != nil {
		_ = m.cleanupMachine(context.Background(), meta)
		return machineruntime.Machine{}, err
	}
	if err := m.waitForGuest(ctx, meta); err != nil {
		_ = m.cleanupMachine(context.Background(), meta)
		return machineruntime.Machine{}, err
	}

	return m.machineFromMetadata(meta), nil
}

func (m *Manager) StartMachine(ctx context.Context, name string) (machineruntime.Machine, error) {
	meta, err := m.loadMetadata(strings.TrimSpace(name))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return machineruntime.Machine{}, machineruntime.ErrMachineNotFound
		}
		return machineruntime.Machine{}, err
	}

	if meta.ProcessID > 0 && processAlive(meta.ProcessID) {
		if err := m.startAppForwarder(ctx, &meta); err != nil {
			return machineruntime.Machine{}, err
		}
		if err := m.startSSHForwarder(ctx, &meta); err != nil {
			return machineruntime.Machine{}, err
		}
		return m.machineFromMetadata(meta), nil
	}

	m.networkMu.Lock()
	networkLocked := true
	defer func() {
		if networkLocked {
			m.networkMu.Unlock()
		}
	}()

	if err := m.createNamespaceNetwork(ctx, meta); err != nil {
		return machineruntime.Machine{}, err
	}
	if err := m.startVM(ctx, &meta); err != nil {
		m.stopMachineRuntime(context.Background(), meta)
		return machineruntime.Machine{}, err
	}
	m.networkMu.Unlock()
	networkLocked = false
	if err := m.startAppForwarder(ctx, &meta); err != nil {
		m.stopMachineRuntime(context.Background(), meta)
		return machineruntime.Machine{}, err
	}
	if err := m.startSSHForwarder(ctx, &meta); err != nil {
		m.stopMachineRuntime(context.Background(), meta)
		return machineruntime.Machine{}, err
	}
	if err := m.waitForGuest(ctx, meta); err != nil {
		m.stopMachineRuntime(context.Background(), meta)
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
	return m.cloneMachineViaSnapshot(ctx, req)
}

func (m *Manager) prepareMetadata(name string, req machineruntime.CreateMachineRequest) (metadata, error) {
	namespaceName, hostVethName, hostVethIPv4, namespaceVethIPv4, macAddress, err := m.prepareNetworkMetadata(name)
	if err != nil {
		return metadata{}, err
	}

	machineDir := m.machineDir(name)
	disk := strings.TrimSpace(req.RootDiskSize)
	if disk == "" {
		disk = "20GiB"
	}
	guestGateway := m.bridgePrefix.Addr().String()
	guestIPv4 := advanceAddr(m.bridgePrefix.Addr(), 1).String()

	return metadata{
		MachineID:         strings.TrimSpace(req.MachineID),
		Name:              name,
		CPU:               strings.TrimSpace(req.CPU),
		Memory:            strings.TrimSpace(req.Memory),
		Disk:              disk,
		PrimaryPort:       req.PrimaryPort,
		IPv4:              guestIPv4,
		GuestGatewayIPv4:  guestGateway,
		GuestUser:         m.defaultGuestUser,
		TapDevice:         namespaceTapName,
		MACAddress:        macAddress,
		NamespaceName:     namespaceName,
		BridgeName:        namespaceBridgeName,
		HostVethName:      hostVethName,
		NamespaceVethName: namespacePeerVethName(name),
		HostVethIPv4:      hostVethIPv4,
		NamespaceVethIPv4: namespaceVethIPv4,
		DiskPath:          filepath.Join(machineDir, diskFileName),
		SeedPath:          filepath.Join(machineDir, seedFileName),
		LogPath:           filepath.Join(machineDir, logFileName),
		SocketPath:        filepath.Join(machineDir, socketFileName),
		RestoreDir:        filepath.Join(machineDir, restoreDirName),
		CreatedAtUTC:      m.now().UTC().Format(time.RFC3339),
	}, nil
}

func (m *Manager) machineDir(name string) string {
	return filepath.Join(m.stateDir, strings.TrimSpace(name))
}

func (m *Manager) snapshotDirPath(name string) string {
	return filepath.Join(m.snapshotDir, strings.TrimSpace(name))
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
		AppHost:   "127.0.0.1",
		AppPort:   meta.AppForwardPort,
		SSHHost:   "127.0.0.1",
		SSHPort:   meta.SSHForwardPort,
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

func (m *Manager) loadSnapshotMetadata(name string) (snapshotMetadata, error) {
	var meta snapshotMetadata
	body, err := os.ReadFile(filepath.Join(m.snapshotDirPath(name), snapshotFileName))
	if err != nil {
		return snapshotMetadata{}, err
	}
	if err := json.Unmarshal(body, &meta); err != nil {
		return snapshotMetadata{}, err
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

func (m *Manager) storeSnapshotMetadata(meta snapshotMetadata) error {
	return storeSnapshotMetadataAt(m.snapshotDirPath(meta.Name), meta)
}

func storeSnapshotMetadataAt(dir string, meta snapshotMetadata) error {
	body, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, snapshotFileName), body, 0o600)
}

func (m *Manager) metadataByIPv4(ipv4 string) (metadata, error) {
	entries, err := os.ReadDir(m.stateDir)
	if err != nil {
		return metadata{}, err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		meta, err := m.loadMetadata(entry.Name())
		if err != nil {
			continue
		}
		if strings.TrimSpace(meta.IPv4) == strings.TrimSpace(ipv4) {
			return meta, nil
		}
	}
	return metadata{}, machineruntime.ErrMachineNotFound
}

func (m *Manager) createOverlayDisk(ctx context.Context, baseImage, diskPath, size string) error {
	if _, err := os.Stat(baseImage); err != nil {
		return fmt.Errorf("base image %q is not available: %w", baseImage, err)
	}
	baseImageFormat, err := m.imageFormat(ctx, baseImage)
	if err != nil {
		return err
	}
	sizeArg, err := qemuImgSizeArg(size)
	if err != nil {
		return err
	}
	args := []string{
		"create",
		"-f", "qcow2",
		"-F", baseImageFormat,
		"-b", baseImage,
		diskPath,
		sizeArg,
	}
	_, err = m.run(ctx, m.qemuImgBinary, args...)
	return err
}

func (m *Manager) imageFormat(ctx context.Context, imagePath string) (string, error) {
	output, err := m.run(ctx, m.qemuImgBinary, "info", "--output=json", imagePath)
	if err != nil {
		return "", err
	}

	var info struct {
		Format string `json:"format"`
	}
	if err := json.Unmarshal([]byte(output), &info); err != nil {
		return "", fmt.Errorf("parse qemu-img info for %s: %w", imagePath, err)
	}
	if strings.TrimSpace(info.Format) == "" {
		return "", fmt.Errorf("qemu-img info for %s did not report a format", imagePath)
	}

	return strings.TrimSpace(info.Format), nil
}

func (m *Manager) copyDisk(ctx context.Context, sourcePath, targetPath string) error {
	// Snapshot copies read from disks that are still held open by a paused VMM.
	// A direct filesystem copy avoids qcow2 lock negotiation entirely; on Linux
	// we prefer a sparse/reflink-aware cp and fall back to a plain file copy if
	// those flags are unavailable.
	if _, err := m.run(ctx, "cp", "--reflink=auto", "--sparse=always", sourcePath, targetPath); err == nil {
		return nil
	}
	return copyFile(sourcePath, targetPath)
}

func (m *Manager) materializeSnapshotDisk(ctx context.Context, sourcePath, targetPath string) error {
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}
	_, err := m.run(ctx, m.qemuImgBinary, "convert", "-O", "qcow2", sourcePath, targetPath)
	if err != nil {
		return err
	}
	if _, err := os.Stat(targetPath); err != nil {
		return fmt.Errorf("materialized snapshot disk missing at %q: %w", targetPath, err)
	}
	return nil
}

func (m *Manager) resizeDisk(ctx context.Context, diskPath, size string) error {
	if strings.TrimSpace(size) == "" {
		return nil
	}
	if _, err := os.Stat(diskPath); err != nil {
		return fmt.Errorf("disk %q is not available for resize: %w", diskPath, err)
	}
	sizeArg, err := qemuImgSizeArg(size)
	if err != nil {
		return err
	}
	_, err = m.run(ctx, m.qemuImgBinary, "resize", diskPath, sizeArg)
	return err
}

func (m *Manager) writeSeedImage(ctx context.Context, meta metadata) error {
	userData := cloudInitUserData(meta, m.baseDomain, m.guestSSHPublicKey, m.hostID, m.hostRegion)
	metaData := fmt.Sprintf("instance-id: fascinate-%s\nlocal-hostname: %s\n", meta.Name, meta.Name)
	networkConfig := cloudInitNetworkConfig(meta.IPv4, meta.MACAddress, m.guestPrefix, m.bridgePrefix.Addr())

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
	m.deleteTapDevice(ctx, tapName)
	if _, err := m.run(ctx, "ip", "tuntap", "add", "dev", tapName, "mode", "tap"); err != nil {
		return err
	}
	if _, err := m.run(ctx, "ip", "link", "set", tapName, "up"); err != nil {
		return err
	}
	return nil
}

func (m *Manager) startVM(ctx context.Context, meta *metadata) error {
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
		"--console", "off",
		"--disk", "path=" + meta.DiskPath + ",image_type=qcow2,backing_files=on", "path=" + meta.SeedPath + ",readonly=on,image_type=raw",
		"--net", "tap=" + meta.TapDevice + ",mac=" + meta.MACAddress,
	}

	fullArgs := append([]string{"netns", "exec", meta.NamespaceName, m.binary}, args...)
	cmd := exec.CommandContext(ctx, "ip", fullArgs...)
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

	return m.storeMetadata(*meta)
}

func (m *Manager) stopMachineRuntime(ctx context.Context, meta metadata) {
	_ = m.stopAppForwarder(ctx, &meta)
	_ = m.stopSSHForwarder(ctx, &meta)
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

	m.deleteNamespaceNetwork(ctx, meta)
}

func (m *Manager) cleanupMachine(ctx context.Context, meta metadata) error {
	m.stopMachineRuntime(ctx, meta)
	if err := os.RemoveAll(m.machineDir(meta.Name)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	return nil
}

func (m *Manager) waitForGuestSSH(ctx context.Context, meta metadata) error {
	deadline := time.Now().Add(15 * time.Minute)
	for {
		err := m.runGuestCommand(ctx, meta, "test -f /var/lib/cloud/instance/boot-finished && command -v claude >/dev/null 2>&1 && command -v codex >/dev/null 2>&1 && command -v gh >/dev/null 2>&1 && command -v node >/dev/null 2>&1 && command -v go >/dev/null 2>&1 && command -v docker >/dev/null 2>&1 && systemctl is-active --quiet docker")
		if err == nil {
			return nil
		}

		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for guest SSH on %s", net.JoinHostPort(strings.TrimSpace(meta.IPv4), "22"))
		}
		time.Sleep(2 * time.Second)
	}
}

func (m *Manager) runGuestCommand(ctx context.Context, meta metadata, command string) error {
	_, err := m.runGuestCommandOutput(ctx, meta, command, nil)
	return err
}

func (m *Manager) CaptureSessionState(ctx context.Context, runtimeName string, spec toolauth.SessionStateSpec) ([]byte, error) {
	meta, err := m.loadMetadata(strings.TrimSpace(runtimeName))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, machineruntime.ErrMachineNotFound
		}
		return nil, err
	}

	output, err := m.runGuestCommandOutput(ctx, meta, captureSessionStateCommand(spec), nil)
	if err != nil {
		return nil, err
	}
	if len(output) == 0 {
		return toolauth.EmptySessionStateArchive()
	}

	return output, nil
}

func (m *Manager) RestoreSessionState(ctx context.Context, runtimeName string, spec toolauth.SessionStateSpec, archive []byte) error {
	meta, err := m.loadMetadata(strings.TrimSpace(runtimeName))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return machineruntime.ErrMachineNotFound
		}
		return err
	}

	_, err = m.runGuestCommandOutput(ctx, meta, restoreSessionStateCommand(spec), bytes.NewReader(archive))
	return err
}

func (m *Manager) SyncManagedEnv(ctx context.Context, runtimeName string, req machineruntime.ManagedEnvRequest) error {
	meta, err := m.loadMetadata(strings.TrimSpace(runtimeName))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return machineruntime.ErrMachineNotFound
		}
		return err
	}
	return m.syncManagedEnvFiles(ctx, meta, req.Entries)
}

func (m *Manager) runGuestCommandOutput(ctx context.Context, meta metadata, command string, stdin io.Reader) ([]byte, error) {
	args := []string{
		"netns", "exec", meta.NamespaceName,
		m.sshClientBinary,
		"-i", m.guestSSHKeyPath,
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "LogLevel=ERROR",
		"-o", "ConnectTimeout=5",
		fmt.Sprintf("%s@%s", meta.GuestUser, meta.IPv4),
		"bash -lc " + shellQuote(command),
	}

	cmd := exec.CommandContext(ctx, "ip", args...)
	if stdin != nil {
		cmd.Stdin = stdin
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = strings.TrimSpace(stdout.String())
		}
		if message == "" {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %s", err, message)
	}
	return stdout.Bytes(), nil
}

func (m *Manager) run(ctx context.Context, binary string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, binary, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w: %s", binary, strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func (m *Manager) syncManagedEnvFiles(ctx context.Context, meta metadata, entries map[string]string) error {
	envFile, envShell, envJSON, profileScript, err := renderManagedEnvFiles(entries)
	if err != nil {
		return err
	}

	command := strings.Join([]string{
		"sudo mkdir -p /etc/fascinate /etc/profile.d",
		"sudo tee /etc/fascinate/env >/dev/null <<'EOF_FASCINATE_ENV'\n" + envFile + "EOF_FASCINATE_ENV",
		"sudo tee /etc/fascinate/env.sh >/dev/null <<'EOF_FASCINATE_ENV_SH'\n" + envShell + "EOF_FASCINATE_ENV_SH",
		"sudo tee /etc/fascinate/env.json >/dev/null <<'EOF_FASCINATE_ENV_JSON'\n" + envJSON + "EOF_FASCINATE_ENV_JSON",
		"sudo tee /etc/profile.d/fascinate-env.sh >/dev/null <<'EOF_FASCINATE_PROFILE'\n" + profileScript + "EOF_FASCINATE_PROFILE",
		"sudo chmod 0644 /etc/fascinate/env /etc/fascinate/env.sh /etc/fascinate/env.json /etc/profile.d/fascinate-env.sh",
	}, "\n")
	return m.runGuestCommand(ctx, meta, command)
}

func managedEnvEntries(meta metadata, baseDomain, hostID, hostRegion string) map[string]string {
	return map[string]string{
		"FASCINATE_BASE_DOMAIN":  strings.TrimSpace(baseDomain),
		"FASCINATE_HOST_ID":      strings.TrimSpace(hostID),
		"FASCINATE_HOST_REGION":  strings.TrimSpace(hostRegion),
		"FASCINATE_MACHINE_ID":   strings.TrimSpace(meta.MachineID),
		"FASCINATE_MACHINE_NAME": strings.TrimSpace(meta.Name),
		"FASCINATE_PRIMARY_PORT": strconv.Itoa(meta.PrimaryPort),
		"FASCINATE_PUBLIC_URL":   "https://" + machinePublicHost(meta.Name, baseDomain),
	}
}

func bootstrapManagedEnvFiles(meta metadata, baseDomain, hostID, hostRegion string) (string, string, string, string) {
	envFile, envShell, envJSON, profileScript, err := renderManagedEnvFiles(managedEnvEntries(meta, baseDomain, hostID, hostRegion))
	if err != nil {
		return "", "", "{}\n", managedEnvProfileScript()
	}
	return envFile, envShell, envJSON, profileScript
}

func renderManagedEnvFiles(entries map[string]string) (string, string, string, string, error) {
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var envBuilder strings.Builder
	var shellBuilder strings.Builder
	for _, key := range keys {
		value := entries[key]
		if strings.Contains(value, "\n") || strings.Contains(value, "\r") {
			return "", "", "", "", fmt.Errorf("managed env value for %q must be single-line", key)
		}
		envBuilder.WriteString(key)
		envBuilder.WriteString("=")
		envBuilder.WriteString(value)
		envBuilder.WriteString("\n")

		shellBuilder.WriteString("export ")
		shellBuilder.WriteString(key)
		shellBuilder.WriteString("=")
		shellBuilder.WriteString(shellQuote(value))
		shellBuilder.WriteString("\n")
	}

	jsonBody, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return "", "", "", "", err
	}

	return envBuilder.String(), shellBuilder.String(), string(jsonBody) + "\n", managedEnvProfileScript(), nil
}

func managedEnvProfileScript() string {
	return "if [ -f /etc/fascinate/env.sh ]; then\n  . /etc/fascinate/env.sh\nfi\n"
}

func cloudInitUserData(meta metadata, baseDomain, publicKey, hostID, hostRegion string) string {
	envFile, envShell, envJSON, profileScript := bootstrapManagedEnvFiles(meta, baseDomain, hostID, hostRegion)
	bootstrapScript := fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive

resolve_node_version() {
  local requested="${FASCINATE_NODE_VERSION:-latest}"

  case "${requested}" in
    ""|latest)
      curl -fsSL https://nodejs.org/dist/index.json | python3 -c 'import json, sys; releases = json.load(sys.stdin); print(releases[0]["version"])'
      ;;
    latest-lts)
      curl -fsSL https://nodejs.org/dist/index.json | python3 -c 'import json, sys; releases = json.load(sys.stdin); print(next(release["version"] for release in releases if release.get("lts")))'
      ;;
    v*)
      printf "%%s\n" "${requested}"
      ;;
    *)
      printf "v%%s\n" "${requested}"
      ;;
  esac
}

resolve_go_version() {
  local requested="${FASCINATE_GO_VERSION:-latest}"

  case "${requested}" in
    ""|latest)
      curl -fsSL https://go.dev/dl/?mode=json | python3 -c 'import json, sys; releases = json.load(sys.stdin); print(releases[0]["version"].removeprefix("go"))'
      ;;
    go*)
      printf "%%s\n" "${requested#go}"
      ;;
    *)
      printf "%%s\n" "${requested}"
      ;;
  esac
}

node_arch() {
  case "$(dpkg --print-architecture)" in
    amd64) printf "%%s\n" "x64" ;;
    arm64) printf "%%s\n" "arm64" ;;
    *)
      printf "unsupported node architecture: %%s\n" "$(dpkg --print-architecture)" >&2
      exit 1
      ;;
  esac
}

go_arch() {
  case "$(dpkg --print-architecture)" in
    amd64) printf "%%s\n" "amd64" ;;
    arm64) printf "%%s\n" "arm64" ;;
    *)
      printf "unsupported go architecture: %%s\n" "$(dpkg --print-architecture)" >&2
      exit 1
      ;;
  esac
}

install_node() {
  local version="$1"
  local arch="$2"
  local file="node-${version}-linux-${arch}.tar.xz"
  local base_url="https://nodejs.org/dist/${version}"

  curl -fsSLO "${base_url}/${file}"
  curl -fsSL "${base_url}/SHASUMS256.txt" -o SHASUMS256.txt
  grep " ${file}$" SHASUMS256.txt | sha256sum -c -

  rm -rf /usr/local/lib/nodejs
  mkdir -p /usr/local/lib/nodejs
  tar -xJf "${file}" -C /usr/local/lib/nodejs

  ln -sf "/usr/local/lib/nodejs/node-${version}-linux-${arch}/bin/node" /usr/local/bin/node
  ln -sf "/usr/local/lib/nodejs/node-${version}-linux-${arch}/bin/npm" /usr/local/bin/npm
  ln -sf "/usr/local/lib/nodejs/node-${version}-linux-${arch}/bin/npx" /usr/local/bin/npx
  ln -sf "/usr/local/lib/nodejs/node-${version}-linux-${arch}/bin/corepack" /usr/local/bin/corepack
  npm config set prefix /usr/local >/dev/null 2>&1 || true
  corepack enable >/dev/null 2>&1 || true

  rm -f "${file}" SHASUMS256.txt
}

install_go() {
  local version="$1"
  local arch="$2"
  local file="go${version}.linux-${arch}.tar.gz"
  local checksum=""

  curl -fsSLo "${file}" "https://dl.google.com/go/${file}"
  checksum="$(curl -fsSL "https://go.dev/dl/?mode=json&include=all" | python3 -c 'import json, sys; target = sys.argv[1]; releases = json.load(sys.stdin); print(next(entry["sha256"] for release in releases for entry in release.get("files", []) if entry.get("filename") == target))' "${file}")"
  printf "%%s  %%s\n" "${checksum}" "${file}" | sha256sum -c -

  rm -rf /usr/local/go
  tar -C /usr/local -xzf "${file}"
  ln -sf /usr/local/go/bin/go /usr/local/bin/go
  ln -sf /usr/local/go/bin/gofmt /usr/local/bin/gofmt

  rm -f "${file}"
}

apt-get update
apt-get upgrade -y
apt-get install -y build-essential ca-certificates curl docker.io file fzf gh git gnupg jq lsb-release make openssh-client procps python-is-python3 python3 python3-pip python3-venv ripgrep rsync sqlite3 tmux unzip wget xz-utils zip

NODE_RESOLVED_VERSION="$(resolve_node_version)"
GO_RESOLVED_VERSION="$(resolve_go_version)"
install_node "${NODE_RESOLVED_VERSION}" "$(node_arch)"
install_go "${GO_RESOLVED_VERSION}" "$(go_arch)"
npm install -g --force npm@latest @anthropic-ai/claude-code @openai/codex@latest

mkdir -p /etc/systemd/system/docker.service.d
cat >/etc/systemd/system/docker.service.d/10-fascinate.conf <<'EOF_DOCKER'
[Service]
Environment=DOCKER_RAMDISK=true
EOF_DOCKER

systemctl daemon-reload
systemctl enable --now docker
usermod -aG docker %s || true

mkdir -p /etc/fascinate /etc/claude-code
mkdir -p /root/.claude /root/.codex
mkdir -p /home/%s/.claude /home/%s/.codex
mkdir -p /etc/skel/.claude /etc/skel/.codex
chown %s:%s /home/%s/.claude /home/%s/.codex || true

cat >/etc/fascinate/env <<'EOF_ENV'
%sEOF_ENV

cat >/etc/fascinate/env.sh <<'EOF_ENV_SH'
%sEOF_ENV_SH

cat >/etc/fascinate/env.json <<'EOF_ENV_JSON'
%sEOF_ENV_JSON

cat >/etc/profile.d/fascinate-env.sh <<'EOF_PROFILE'
%sEOF_PROFILE

chmod 0644 /etc/fascinate/env /etc/fascinate/env.sh /etc/fascinate/env.json /etc/profile.d/fascinate-env.sh

cat >/etc/fascinate/AGENTS.md <<'EOF_AGENTS'
%s
EOF_AGENTS

chmod 0644 /etc/fascinate/AGENTS.md

ln -sfn /etc/fascinate/AGENTS.md /etc/claude-code/CLAUDE.md
ln -sfn /etc/fascinate/AGENTS.md /root/AGENTS.md
ln -sfn /etc/fascinate/AGENTS.md /root/.claude/CLAUDE.md
ln -sfn /etc/fascinate/AGENTS.md /root/.codex/AGENTS.md
ln -sfn /etc/fascinate/AGENTS.md /home/%s/AGENTS.md
ln -sfn /etc/fascinate/AGENTS.md /home/%s/.claude/CLAUDE.md
ln -sfn /etc/fascinate/AGENTS.md /home/%s/.codex/AGENTS.md
ln -sfn /etc/fascinate/AGENTS.md /etc/skel/AGENTS.md
ln -sfn /etc/fascinate/AGENTS.md /etc/skel/.claude/CLAUDE.md
ln -sfn /etc/fascinate/AGENTS.md /etc/skel/.codex/AGENTS.md

chown -h %s:%s /home/%s/AGENTS.md /home/%s/.claude/CLAUDE.md /home/%s/.codex/AGENTS.md || true
apt-get clean
rm -rf /var/lib/apt/lists/*
`, meta.GuestUser, meta.GuestUser, meta.GuestUser, meta.GuestUser, meta.GuestUser, meta.GuestUser, meta.GuestUser, envFile, envShell, envJSON, profileScript, toolauth.ClaudeMachineInstructions(meta.Name, baseDomain, meta.PrimaryPort), meta.GuestUser, meta.GuestUser, meta.GuestUser, meta.GuestUser, meta.GuestUser, meta.GuestUser, meta.GuestUser, meta.GuestUser)

	return fmt.Sprintf(`#cloud-config
preserve_hostname: false
hostname: %s
users:
  - default
  - name: %s
    shell: /bin/bash
    sudo: ALL=(ALL) NOPASSWD:ALL
    groups: [adm, sudo]
    ssh_authorized_keys:
      - %s
ssh_pwauth: false
disable_root: true
runcmd:
  - [bash, /usr/local/sbin/fascinate-firstboot.sh]
write_files:
  - path: /usr/local/sbin/fascinate-firstboot.sh
    permissions: "0755"
    owner: root:root
    content: |
%s
`, meta.Name, meta.GuestUser, strings.TrimSpace(publicKey), indentBlock(bootstrapScript, "      "))
}

func cloudInitNetworkConfig(ipv4, macAddress string, guestPrefix netip.Prefix, gateway netip.Addr) string {
	return fmt.Sprintf(`version: 2
ethernets:
  ens4:
    match:
      macaddress: "%s"
    dhcp4: false
    addresses: [%s/%d]
    gateway4: %s
    nameservers:
      addresses: [1.1.1.1, 1.0.0.1]
`, strings.ToLower(strings.TrimSpace(macAddress)), ipv4, guestPrefix.Bits(), gateway.String())
}

func captureSessionStateCommand(spec toolauth.SessionStateSpec) string {
	var script strings.Builder
	script.WriteString("set -euo pipefail\n")
	script.WriteString("declare -a paths=()\n")
	for _, root := range spec.Roots {
		if strings.TrimSpace(root.Path) == "" {
			continue
		}
		script.WriteString("if [ -e ")
		script.WriteString(shellQuote(root.Path))
		script.WriteString(" ]; then paths+=(")
		script.WriteString(shellQuote(root.Path))
		script.WriteString("); fi\n")
	}
	script.WriteString("if [ \"${#paths[@]}\" -eq 0 ]; then exit 0; fi\n")
	script.WriteString("tar -czf - -P --ignore-failed-read")
	for _, root := range spec.Roots {
		for _, baseName := range root.ExcludeBaseNames {
			if strings.TrimSpace(root.Path) == "" || strings.TrimSpace(baseName) == "" {
				continue
			}
			script.WriteString(" --exclude=")
			script.WriteString(shellQuote(filepath.Join(root.Path, baseName)))
		}
	}
	script.WriteString(" \"${paths[@]}\"\n")
	return "bash -lc " + shellQuote(script.String())
}

func restoreSessionStateCommand(spec toolauth.SessionStateSpec) string {
	var script strings.Builder
	script.WriteString("set -euo pipefail\n")
	script.WriteString("archive=\"$(mktemp)\"\n")
	script.WriteString("trap 'rm -f \"$archive\"' EXIT\n")
	script.WriteString("cat >\"$archive\"\n")
	for _, root := range spec.Roots {
		path := strings.TrimSpace(root.Path)
		if path == "" {
			continue
		}
		if root.Kind == toolauth.SessionStateRootKindFile {
			script.WriteString("mkdir -p ")
			script.WriteString(shellQuote(filepath.Dir(path)))
			script.WriteString("\n")
			script.WriteString("rm -f ")
			script.WriteString(shellQuote(path))
			script.WriteString("\n")
			continue
		}

		script.WriteString("mkdir -p ")
		script.WriteString(shellQuote(path))
		script.WriteString("\n")
		if root.DirectoryMode > 0 {
			script.WriteString("chmod ")
			script.WriteString(fmt.Sprintf("%04o ", root.DirectoryMode))
			script.WriteString(shellQuote(path))
			script.WriteString("\n")
		}
		if strings.TrimSpace(root.Owner) != "" || strings.TrimSpace(root.Group) != "" {
			script.WriteString("chown ")
			script.WriteString(shellQuote(strings.TrimSpace(root.Owner) + ":" + strings.TrimSpace(root.Group)))
			script.WriteString(" ")
			script.WriteString(shellQuote(path))
			script.WriteString(" || true\n")
		}
		script.WriteString("find ")
		script.WriteString(shellQuote(path))
		script.WriteString(" -mindepth 1")
		for _, baseName := range root.ExcludeBaseNames {
			if strings.TrimSpace(baseName) == "" {
				continue
			}
			script.WriteString(" ! -name ")
			script.WriteString(shellQuote(baseName))
		}
		script.WriteString(" -exec rm -rf {} +\n")
	}
	script.WriteString("if [ -s \"$archive\" ]; then tar -xzf \"$archive\" -P; fi\n")
	for _, root := range spec.Roots {
		path := strings.TrimSpace(root.Path)
		if path == "" {
			continue
		}
		if strings.TrimSpace(root.Owner) != "" || strings.TrimSpace(root.Group) != "" {
			script.WriteString("if [ -e ")
			script.WriteString(shellQuote(path))
			script.WriteString(" ]; then chown ")
			script.WriteString(shellQuote(strings.TrimSpace(root.Owner) + ":" + strings.TrimSpace(root.Group)))
			script.WriteString(" ")
			script.WriteString(shellQuote(path))
			script.WriteString("; fi\n")
		}
		if root.Kind == toolauth.SessionStateRootKindDirectory && root.DirectoryMode > 0 {
			script.WriteString("if [ -d ")
			script.WriteString(shellQuote(path))
			script.WriteString(" ]; then chmod ")
			script.WriteString(fmt.Sprintf("%04o ", root.DirectoryMode))
			script.WriteString(shellQuote(path))
			script.WriteString("; fi\n")
		}
	}
	return "bash -lc " + shellQuote(script.String())
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func machinePublicHost(name, baseDomain string) string {
	name = strings.TrimSpace(name)
	baseDomain = strings.TrimSpace(baseDomain)
	if name == "" {
		return baseDomain
	}
	if baseDomain == "" {
		return name
	}
	return name + "." + baseDomain
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

func indentBlock(value, prefix string) string {
	lines := strings.Split(strings.TrimRight(value, "\n"), "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
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

func qemuImgSizeArg(value string) (string, error) {
	bytes, err := parseByteSize(value)
	if err != nil {
		return "", err
	}
	if bytes <= 0 {
		return "", fmt.Errorf("disk size must be positive")
	}
	return strconv.FormatInt(bytes, 10), nil
}

func tapDeviceName(name string) string {
	base := compactTapName(name)
	if len(base) > 4 {
		base = base[:4]
	}
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(compactTapName(name)))
	return fmt.Sprintf("fsc%s%06x", base, hasher.Sum32()&0xffffff)
}

func compactTapName(name string) string {
	var builder strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
		}
	}
	if builder.Len() == 0 {
		return "vm"
	}
	return builder.String()
}

func (m *Manager) deleteTapDevice(ctx context.Context, tapName string) {
	tapName = strings.TrimSpace(tapName)
	if tapName == "" {
		return
	}
	_, _ = m.run(ctx, "ip", "link", "set", tapName, "down")
	_, _ = m.run(ctx, "ip", "tuntap", "del", "dev", tapName, "mode", "tap")
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
		if err := os.Chmod(path, 0o600); err != nil {
			return "", err
		}
		signer, err := ssh.ParsePrivateKey(body)
		if err != nil {
			return "", err
		}
		publicKey := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))
		if err := os.WriteFile(path+".pub", []byte(publicKey+"\n"), 0o644); err != nil {
			return "", err
		}
		return publicKey, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", err
	}

	privateKeyPEMBlock, err := ssh.MarshalPrivateKey(privateKey, "")
	if err != nil {
		return "", err
	}

	privateKeyPEM := pem.EncodeToMemory(privateKeyPEMBlock)
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
