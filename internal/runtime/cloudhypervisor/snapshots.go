package cloudhypervisor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	machineruntime "fascinate/internal/runtime"
	"fascinate/internal/toolauth"
)

func (m *Manager) ListSnapshots(ctx context.Context) ([]machineruntime.Snapshot, error) {
	entries, err := os.ReadDir(m.snapshotDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	snapshots := make([]machineruntime.Snapshot, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		meta, err := m.loadSnapshotMetadata(entry.Name())
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}
		snapshots = append(snapshots, snapshotFromMetadata(meta))
	}

	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].CreatedAt > snapshots[j].CreatedAt
	})
	return snapshots, nil
}

func (m *Manager) GetSnapshot(_ context.Context, name string) (machineruntime.Snapshot, error) {
	meta, err := m.loadSnapshotMetadata(strings.TrimSpace(name))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return machineruntime.Snapshot{}, machineruntime.ErrSnapshotNotFound
		}
		return machineruntime.Snapshot{}, err
	}
	return snapshotFromMetadata(meta), nil
}

func (m *Manager) CreateSnapshot(ctx context.Context, req machineruntime.CreateSnapshotRequest) (machineruntime.Snapshot, error) {
	name := strings.TrimSpace(req.SnapshotName)
	if name == "" {
		return machineruntime.Snapshot{}, fmt.Errorf("snapshot name is required")
	}
	source, err := m.loadMetadata(strings.TrimSpace(req.MachineName))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return machineruntime.Snapshot{}, machineruntime.ErrMachineNotFound
		}
		return machineruntime.Snapshot{}, err
	}

	artifactDir := strings.TrimSpace(req.ArtifactDir)
	if artifactDir == "" {
		artifactDir = m.snapshotDirPath(name)
	}
	meta, err := m.createSnapshotArtifact(ctx, source, name, artifactDir)
	if err != nil {
		return machineruntime.Snapshot{}, err
	}

	return snapshotFromMetadata(meta), nil
}

func (m *Manager) DeleteSnapshot(_ context.Context, name string) error {
	dir := m.snapshotDirPath(strings.TrimSpace(name))
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		return machineruntime.ErrSnapshotNotFound
	} else if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

func (m *Manager) cloneMachineViaSnapshot(ctx context.Context, req machineruntime.CloneMachineRequest) (machineruntime.Machine, error) {
	sourceName := strings.TrimSpace(req.SourceName)
	targetName := strings.TrimSpace(req.TargetName)
	if sourceName == "" || targetName == "" {
		return machineruntime.Machine{}, fmt.Errorf("source and target names are required")
	}

	tempName := fmt.Sprintf(".clone-%s-%d", targetName, time.Now().UnixNano())
	tempDir := m.snapshotDirPath(tempName)
	snapshotMeta, err := m.CreateSnapshot(ctx, machineruntime.CreateSnapshotRequest{
		MachineName:  sourceName,
		SnapshotName: tempName,
		ArtifactDir:  tempDir,
	})
	if err != nil {
		return machineruntime.Machine{}, err
	}
	defer os.RemoveAll(snapshotMeta.ArtifactDir)

	machine, err := m.restoreMachineFromSnapshot(ctx, tempName, machineruntime.CreateMachineRequest{
		Name:         targetName,
		Snapshot:     tempName,
		RootDiskSize: req.RootDiskSize,
	})
	if err != nil {
		return machineruntime.Machine{}, err
	}
	return machine, nil
}

func (m *Manager) restoreMachineFromSnapshot(ctx context.Context, snapshotName string, req machineruntime.CreateMachineRequest) (machineruntime.Machine, error) {
	snapshotName = strings.TrimSpace(snapshotName)
	name := strings.TrimSpace(req.Name)
	if snapshotName == "" || name == "" {
		return machineruntime.Machine{}, fmt.Errorf("snapshot and target machine name are required")
	}

	snapshotMeta, err := m.loadSnapshotMetadata(snapshotName)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return machineruntime.Machine{}, machineruntime.ErrSnapshotNotFound
		}
		return machineruntime.Machine{}, err
	}

	targetDir := m.machineDir(name)
	if _, err := os.Stat(targetDir); err == nil {
		return machineruntime.Machine{}, fmt.Errorf("machine %q already exists", name)
	} else if !errors.Is(err, os.ErrNotExist) {
		return machineruntime.Machine{}, err
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return machineruntime.Machine{}, err
	}

	m.networkMu.Lock()
	networkLocked := true
	defer func() {
		if networkLocked {
			m.networkMu.Unlock()
		}
	}()

	createReq := machineruntime.CreateMachineRequest{
		Name:         name,
		CPU:          snapshotMeta.CPU,
		Memory:       snapshotMeta.Memory,
		RootDiskSize: snapshotMeta.Disk,
		PrimaryPort:  snapshotMeta.PrimaryPort,
	}
	if strings.TrimSpace(req.RootDiskSize) != "" {
		createReq.RootDiskSize = strings.TrimSpace(req.RootDiskSize)
	}
	if req.PrimaryPort > 0 {
		createReq.PrimaryPort = req.PrimaryPort
	}

	targetMeta, err := m.prepareMetadata(name, createReq)
	if err != nil {
		_ = os.RemoveAll(targetDir)
		return machineruntime.Machine{}, err
	}
	targetMeta.CPU = snapshotMeta.CPU
	targetMeta.Memory = snapshotMeta.Memory
	targetMeta.Disk = createReq.RootDiskSize
	targetMeta.PrimaryPort = createReq.PrimaryPort
	targetMeta.MACAddress = snapshotMeta.MACAddress
	if err := m.storeMetadata(targetMeta); err != nil {
		_ = os.RemoveAll(targetDir)
		return machineruntime.Machine{}, err
	}

	if err := m.copyDisk(ctx, snapshotMeta.DiskPath, targetMeta.DiskPath); err != nil {
		_ = os.RemoveAll(targetDir)
		return machineruntime.Machine{}, err
	}
	if err := m.resizeDisk(ctx, targetMeta.DiskPath, targetMeta.Disk); err != nil {
		_ = os.RemoveAll(targetDir)
		return machineruntime.Machine{}, err
	}
	if err := m.writeSeedImage(ctx, targetMeta); err != nil {
		_ = os.RemoveAll(targetDir)
		return machineruntime.Machine{}, err
	}
	if err := copyDir(snapshotMeta.RestoreDir, targetMeta.RestoreDir); err != nil {
		_ = os.RemoveAll(targetDir)
		return machineruntime.Machine{}, err
	}
	if err := rewriteSnapshotConfig(filepath.Join(targetMeta.RestoreDir, "config.json"), map[string]string{
		snapshotMeta.DiskPath: targetMeta.DiskPath,
		snapshotMeta.SeedPath: targetMeta.SeedPath,
	}); err != nil {
		_ = os.RemoveAll(targetDir)
		return machineruntime.Machine{}, err
	}
	if err := m.createNamespaceNetwork(ctx, targetMeta); err != nil {
		_ = os.RemoveAll(targetDir)
		return machineruntime.Machine{}, err
	}
	if err := m.restoreVM(ctx, &targetMeta); err != nil {
		_ = m.cleanupMachine(context.Background(), targetMeta)
		return machineruntime.Machine{}, err
	}
	m.networkMu.Unlock()
	networkLocked = false
	if err := m.startAppForwarder(ctx, &targetMeta); err != nil {
		_ = m.cleanupMachine(context.Background(), targetMeta)
		return machineruntime.Machine{}, err
	}
	if err := m.startSSHForwarder(ctx, &targetMeta); err != nil {
		_ = m.cleanupMachine(context.Background(), targetMeta)
		return machineruntime.Machine{}, err
	}
	if err := m.waitForGuest(ctx, targetMeta); err != nil {
		_ = m.cleanupMachine(context.Background(), targetMeta)
		return machineruntime.Machine{}, err
	}
	if err := m.refreshMachineIdentity(ctx, targetMeta); err != nil {
		_ = m.cleanupMachine(context.Background(), targetMeta)
		return machineruntime.Machine{}, err
	}

	return m.machineFromMetadata(targetMeta), nil
}

func (m *Manager) createSnapshotArtifact(ctx context.Context, source metadata, snapshotName, artifactDir string) (snapshotMetadata, error) {
	if _, err := os.Stat(artifactDir); err == nil {
		return snapshotMetadata{}, fmt.Errorf("snapshot %q already exists", snapshotName)
	} else if !errors.Is(err, os.ErrNotExist) {
		return snapshotMetadata{}, err
	}

	tmpDir := artifactDir + ".tmp"
	_ = os.RemoveAll(tmpDir)
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return snapshotMetadata{}, err
	}
	restoreDir := filepath.Join(tmpDir, restoreDirName)
	if err := os.MkdirAll(restoreDir, 0o755); err != nil {
		_ = os.RemoveAll(tmpDir)
		return snapshotMetadata{}, err
	}

	if err := m.pauseVM(ctx, source); err != nil {
		_ = os.RemoveAll(tmpDir)
		return snapshotMetadata{}, err
	}
	defer m.resumeVM(context.Background(), source)

	if err := m.snapshotVM(ctx, source, restoreDir); err != nil {
		_ = os.RemoveAll(tmpDir)
		return snapshotMetadata{}, err
	}
	diskPath := filepath.Join(tmpDir, diskFileName)
	if err := m.copyDisk(ctx, source.DiskPath, diskPath); err != nil {
		_ = os.RemoveAll(tmpDir)
		return snapshotMetadata{}, err
	}
	seedPath := filepath.Join(tmpDir, seedFileName)
	if err := copyFile(source.SeedPath, seedPath); err != nil {
		_ = os.RemoveAll(tmpDir)
		return snapshotMetadata{}, err
	}
	if err := rewriteSnapshotConfig(filepath.Join(restoreDir, "config.json"), map[string]string{
		source.DiskPath: diskPath,
		source.SeedPath: seedPath,
	}); err != nil {
		_ = os.RemoveAll(tmpDir)
		return snapshotMetadata{}, err
	}

	runtimeVersion, err := m.hypervisorVersion(ctx)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return snapshotMetadata{}, err
	}
	diskSize, err := fileSize(diskPath)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return snapshotMetadata{}, err
	}
	memorySize, err := dirSize(restoreDir)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return snapshotMetadata{}, err
	}

	meta := snapshotMetadata{
		Name:              snapshotName,
		SourceMachineName: source.Name,
		State:             "READY",
		CPU:               source.CPU,
		Memory:            source.Memory,
		Disk:              source.Disk,
		PrimaryPort:       source.PrimaryPort,
		IPv4:              source.IPv4,
		GuestGatewayIPv4:  source.GuestGatewayIPv4,
		GuestUser:         source.GuestUser,
		TapDevice:         source.TapDevice,
		MACAddress:        source.MACAddress,
		DiskPath:          diskPath,
		SeedPath:          seedPath,
		RestoreDir:        restoreDir,
		DiskSizeBytes:     diskSize,
		MemorySizeBytes:   memorySize,
		RuntimeVersion:    runtimeVersion,
		FirmwareVersion:   filepath.Base(m.firmwarePath),
		CreatedAtUTC:      m.now().UTC().Format(time.RFC3339),
	}
	if err := storeSnapshotMetadataAt(tmpDir, meta); err != nil {
		_ = os.RemoveAll(tmpDir)
		return snapshotMetadata{}, err
	}
	if err := os.Rename(tmpDir, artifactDir); err != nil {
		_ = os.RemoveAll(tmpDir)
		return snapshotMetadata{}, err
	}
	finalDiskPath := filepath.Join(artifactDir, diskFileName)
	finalSeedPath := filepath.Join(artifactDir, seedFileName)
	if err := rewriteSnapshotConfig(filepath.Join(artifactDir, restoreDirName, "config.json"), map[string]string{
		diskPath: finalDiskPath,
		seedPath: finalSeedPath,
	}); err != nil {
		_ = os.RemoveAll(artifactDir)
		return snapshotMetadata{}, err
	}
	meta.DiskPath = finalDiskPath
	meta.SeedPath = finalSeedPath
	meta.RestoreDir = filepath.Join(artifactDir, restoreDirName)
	if err := m.storeSnapshotMetadata(meta); err != nil {
		_ = os.RemoveAll(artifactDir)
		return snapshotMetadata{}, err
	}
	return meta, nil
}

func (m *Manager) pauseVM(ctx context.Context, meta metadata) error {
	return m.vmmRequest(ctx, meta.SocketPath, http.MethodPut, "/vm.pause", nil)
}

func (m *Manager) resumeVM(ctx context.Context, meta metadata) error {
	return m.vmmRequest(ctx, meta.SocketPath, http.MethodPut, "/vm.resume", nil)
}

func (m *Manager) snapshotVM(ctx context.Context, meta metadata, destinationDir string) error {
	body := map[string]string{"destination_url": "file://" + destinationDir}
	return m.vmmRequest(ctx, meta.SocketPath, http.MethodPut, "/vm.snapshot", body)
}

func (m *Manager) restoreVM(ctx context.Context, meta *metadata) error {
	logFile, err := os.OpenFile(meta.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer logFile.Close()

	args := []string{
		"netns", "exec", meta.NamespaceName,
		m.binary,
		"--api-socket", meta.SocketPath,
		"--restore", restoreArg(meta.RestoreDir),
	}
	cmd := exec.CommandContext(ctx, "ip", args...)
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
	if err := m.storeMetadata(*meta); err != nil {
		return err
	}
	if err := m.waitForVMMReady(ctx, meta.SocketPath); err != nil {
		return err
	}
	return m.resumeVM(ctx, *meta)
}

func restoreArg(restoreDir string) string {
	return "source_url=file://" + restoreDir
}

func (m *Manager) waitForVMMReady(ctx context.Context, socketPath string) error {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if err := m.vmmRequest(ctx, socketPath, http.MethodGet, "/vmm.ping", nil); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return fmt.Errorf("cloud-hypervisor API did not become ready at %s", socketPath)
}

func (m *Manager) vmmRequest(ctx context.Context, socketPath, method, apiPath string, body any) error {
	var payload io.Reader
	if body != nil {
		buf := new(bytes.Buffer)
		if err := json.NewEncoder(buf).Encode(body); err != nil {
			return err
		}
		payload = buf
	}

	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socketPath)
		},
	}
	defer transport.CloseIdleConnections()

	req, err := http.NewRequestWithContext(ctx, method, "http://unix"+normalizeAPIPath(apiPath), payload)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := (&http.Client{Transport: transport, Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s %s: %s", method, apiPath, strings.TrimSpace(string(data)))
	}
	return nil
}

func normalizeAPIPath(apiPath string) string {
	apiPath = strings.TrimSpace(apiPath)
	if apiPath == "" {
		return "/api/v1"
	}
	if strings.HasPrefix(apiPath, "/api/v1/") || apiPath == "/api/v1" {
		return apiPath
	}
	if !strings.HasPrefix(apiPath, "/") {
		apiPath = "/" + apiPath
	}
	return "/api/v1" + apiPath
}

func (m *Manager) hypervisorVersion(ctx context.Context) (string, error) {
	output, err := m.run(ctx, m.binary, "--version")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func snapshotFromMetadata(meta snapshotMetadata) machineruntime.Snapshot {
	return machineruntime.Snapshot{
		Name:              meta.Name,
		SourceMachineName: meta.SourceMachineName,
		State:             meta.State,
		ArtifactDir:       filepath.Dir(meta.DiskPath),
		DiskSizeBytes:     meta.DiskSizeBytes,
		MemorySizeBytes:   meta.MemorySizeBytes,
		RuntimeVersion:    meta.RuntimeVersion,
		FirmwareVersion:   meta.FirmwareVersion,
		CreatedAt:         meta.CreatedAtUTC,
	}
}

func rewriteSnapshotConfig(configPath string, replacements map[string]string) error {
	body, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		return err
	}
	rewritten := rewriteStrings(value, replacements)
	body, err = json.MarshalIndent(rewritten, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, body, 0o600)
}

func rewriteStrings(value any, replacements map[string]string) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, nested := range typed {
			out[key] = rewriteStrings(nested, replacements)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, nested := range typed {
			out[i] = rewriteStrings(nested, replacements)
		}
		return out
	case string:
		if replacement, ok := replacements[typed]; ok {
			return replacement
		}
		return typed
	default:
		return value
	}
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

func dirSize(root string) (int64, error) {
	var total int64
	err := filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total, err
}

func fileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

func (m *Manager) refreshMachineIdentity(ctx context.Context, meta metadata) error {
	command := strings.Join([]string{
		"sudo hostnamectl set-hostname " + shellQuote(meta.Name) + " || true",
		"sudo mkdir -p /etc/fascinate",
		"sudo tee /etc/fascinate/AGENTS.md >/dev/null <<'EOF'\n" + toolauth.ClaudeMachineInstructions(meta.Name, m.baseDomain, meta.PrimaryPort) + "\nEOF",
	}, "\n")
	return m.runGuestCommand(ctx, meta, command)
}
