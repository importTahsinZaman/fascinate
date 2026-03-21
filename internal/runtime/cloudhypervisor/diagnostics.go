package cloudhypervisor

import (
	"context"
	"errors"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	machineruntime "fascinate/internal/runtime"
)

func (m *Manager) InspectMachine(_ context.Context, name string) (machineruntime.MachineDiagnostics, error) {
	meta, err := m.loadMetadata(strings.TrimSpace(name))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return machineruntime.MachineDiagnostics{}, machineruntime.ErrMachineNotFound
		}
		return machineruntime.MachineDiagnostics{}, err
	}

	return machineruntime.MachineDiagnostics{
		Machine:             m.machineFromMetadata(meta),
		RuntimeName:         meta.Name,
		NamespaceName:       meta.NamespaceName,
		BridgeName:          meta.BridgeName,
		TapDevice:           meta.TapDevice,
		MACAddress:          meta.MACAddress,
		DiskPath:            meta.DiskPath,
		SeedPath:            meta.SeedPath,
		LogPath:             meta.LogPath,
		SocketPath:          meta.SocketPath,
		RestoreDir:          meta.RestoreDir,
		HostVethName:        meta.HostVethName,
		NamespaceVethName:   meta.NamespaceVethName,
		HostVethIPv4:        meta.HostVethIPv4,
		NamespaceVethIPv4:   meta.NamespaceVethIPv4,
		VMMProcessID:        meta.ProcessID,
		VMMProcessAlive:     processAlive(meta.ProcessID),
		AppForwardPID:       meta.AppForwardPID,
		AppForwardPort:      meta.AppForwardPort,
		AppForwardAlive:     processAlive(meta.AppForwardPID),
		AppForwardReachable: tcpReachable("127.0.0.1", meta.AppForwardPort),
		SSHForwardPID:       meta.SSHForwardPID,
		SSHForwardPort:      meta.SSHForwardPort,
		SSHForwardAlive:     processAlive(meta.SSHForwardPID),
		SSHForwardReachable: tcpReachable("127.0.0.1", meta.SSHForwardPort),
		LogTail:             tailLines(meta.LogPath, 40),
	}, nil
}

func (m *Manager) InspectSnapshot(_ context.Context, name string) (machineruntime.SnapshotDiagnostics, error) {
	meta, err := m.loadSnapshotMetadata(strings.TrimSpace(name))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return machineruntime.SnapshotDiagnostics{}, machineruntime.ErrSnapshotNotFound
		}
		return machineruntime.SnapshotDiagnostics{}, err
	}

	artifactDir := m.snapshotDirPath(strings.TrimSpace(name))
	return machineruntime.SnapshotDiagnostics{
		Snapshot:          snapshotFromMetadata(meta),
		RuntimeName:       meta.Name,
		ArtifactDir:       artifactDir,
		DiskPath:          meta.DiskPath,
		SeedPath:          meta.SeedPath,
		RestoreDir:        meta.RestoreDir,
		ArtifactDirExists: pathExists(artifactDir),
		DiskExists:        pathExists(meta.DiskPath),
		SeedExists:        pathExists(meta.SeedPath),
		RestoreDirExists:  pathExists(meta.RestoreDir),
	}, nil
}

func pathExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func tcpReachable(host string, port int) bool {
	if strings.TrimSpace(host) == "" || port <= 0 {
		return false
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func tailLines(path string, limit int) []string {
	if strings.TrimSpace(path) == "" || limit <= 0 {
		return nil
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimRight(string(body), "\n"), "\n")
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	var out []string
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}
