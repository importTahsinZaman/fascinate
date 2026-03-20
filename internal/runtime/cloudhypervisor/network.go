package cloudhypervisor

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	namespaceBridgeName = "br0"
	namespaceTapName    = "tap0"
	namespaceUplinkName = "uplink0"
)

func (m *Manager) prepareNetworkMetadata(targetName string) (string, string, string, string, string, error) {
	used := map[netip.Addr]struct{}{}
	entries, err := os.ReadDir(m.stateDir)
	if err != nil && !os.IsNotExist(err) {
		return "", "", "", "", "", err
	}

	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == targetName {
			continue
		}
		meta, err := m.loadMetadata(entry.Name())
		if err != nil {
			continue
		}
		addr, err := netip.ParseAddr(strings.TrimSpace(meta.HostVethIPv4))
		if err != nil {
			continue
		}
		used[addr] = struct{}{}
	}

	start := m.namespacePrefix.Addr()
	for i := 0; i < 16384; i++ {
		networkAddr := advanceAddr(start, i*4)
		hostAddr := networkAddr.Next()
		nsAddr := hostAddr.Next()
		if !m.namespacePrefix.Contains(nsAddr) {
			break
		}
		if _, ok := used[hostAddr]; ok {
			continue
		}
		return namespaceName(targetName), hostVethName(targetName), hostAddr.String(), nsAddr.String(), guestMACAddress(), nil
	}

	return "", "", "", "", "", fmt.Errorf("no free namespace uplink addresses remain in %s", m.namespacePrefix.String())
}

func (m *Manager) ensureRootNetwork(ctx context.Context) error {
	uplink, err := m.defaultUplinkInterface(ctx)
	if err != nil {
		return err
	}
	if _, err := m.run(ctx, "sysctl", "-w", "net.ipv4.ip_forward=1"); err != nil {
		return err
	}
	if err := ensureIptablesRule(ctx, m.run, "nat", []string{"POSTROUTING", "-s", m.namespacePrefix.String(), "-o", uplink, "-j", "MASQUERADE"}); err != nil {
		return err
	}
	return nil
}

func (m *Manager) createNamespaceNetwork(ctx context.Context, meta metadata) error {
	if err := m.ensureRootNetwork(ctx); err != nil {
		return err
	}
	m.deleteNamespaceNetwork(context.Background(), meta)

	if _, err := m.run(ctx, "ip", "netns", "add", meta.NamespaceName); err != nil {
		return err
	}
	if _, err := m.run(ctx, "ip", "link", "add", meta.HostVethName, "type", "veth", "peer", "name", meta.NamespaceVethName); err != nil {
		m.deleteNamespaceNetwork(context.Background(), meta)
		return err
	}
	if _, err := m.run(ctx, "ip", "link", "set", meta.NamespaceVethName, "netns", meta.NamespaceName); err != nil {
		m.deleteNamespaceNetwork(context.Background(), meta)
		return err
	}
	if _, err := m.run(ctx, "ip", "addr", "add", meta.HostVethIPv4+"/30", "dev", meta.HostVethName); err != nil {
		m.deleteNamespaceNetwork(context.Background(), meta)
		return err
	}
	if _, err := m.run(ctx, "ip", "link", "set", meta.HostVethName, "up"); err != nil {
		m.deleteNamespaceNetwork(context.Background(), meta)
		return err
	}
	if err := ensureIptablesRule(ctx, m.run, "filter", []string{"FORWARD", "-i", meta.HostVethName, "-j", "ACCEPT"}); err != nil {
		m.deleteNamespaceNetwork(context.Background(), meta)
		return err
	}
	if err := ensureIptablesRule(ctx, m.run, "filter", []string{"FORWARD", "-o", meta.HostVethName, "-m", "state", "--state", "RELATED,ESTABLISHED", "-j", "ACCEPT"}); err != nil {
		m.deleteNamespaceNetwork(context.Background(), meta)
		return err
	}

	if _, err := m.runInNamespace(ctx, meta.NamespaceName, "ip", "link", "set", "lo", "up"); err != nil {
		m.deleteNamespaceNetwork(context.Background(), meta)
		return err
	}
	if _, err := m.runInNamespace(ctx, meta.NamespaceName, "ip", "link", "set", meta.NamespaceVethName, "name", namespaceUplinkName); err != nil {
		m.deleteNamespaceNetwork(context.Background(), meta)
		return err
	}
	if _, err := m.runInNamespace(ctx, meta.NamespaceName, "ip", "addr", "add", meta.NamespaceVethIPv4+"/30", "dev", namespaceUplinkName); err != nil {
		m.deleteNamespaceNetwork(context.Background(), meta)
		return err
	}
	if _, err := m.runInNamespace(ctx, meta.NamespaceName, "ip", "link", "set", namespaceUplinkName, "up"); err != nil {
		m.deleteNamespaceNetwork(context.Background(), meta)
		return err
	}
	if _, err := m.runInNamespace(ctx, meta.NamespaceName, "ip", "route", "add", "default", "via", meta.HostVethIPv4, "dev", namespaceUplinkName); err != nil {
		m.deleteNamespaceNetwork(context.Background(), meta)
		return err
	}
	if _, err := m.runInNamespace(ctx, meta.NamespaceName, "ip", "link", "add", meta.BridgeName, "type", "bridge"); err != nil {
		m.deleteNamespaceNetwork(context.Background(), meta)
		return err
	}
	if _, err := m.runInNamespace(ctx, meta.NamespaceName, "ip", "addr", "add", meta.GuestGatewayIPv4+"/"+strconv.Itoa(m.guestPrefix.Bits()), "dev", meta.BridgeName); err != nil {
		m.deleteNamespaceNetwork(context.Background(), meta)
		return err
	}
	if _, err := m.runInNamespace(ctx, meta.NamespaceName, "ip", "link", "set", meta.BridgeName, "up"); err != nil {
		m.deleteNamespaceNetwork(context.Background(), meta)
		return err
	}
	if _, err := m.runInNamespace(ctx, meta.NamespaceName, "ip", "tuntap", "add", "dev", meta.TapDevice, "mode", "tap"); err != nil {
		m.deleteNamespaceNetwork(context.Background(), meta)
		return err
	}
	if _, err := m.runInNamespace(ctx, meta.NamespaceName, "ip", "link", "set", meta.TapDevice, "master", meta.BridgeName); err != nil {
		m.deleteNamespaceNetwork(context.Background(), meta)
		return err
	}
	if _, err := m.runInNamespace(ctx, meta.NamespaceName, "ip", "link", "set", meta.TapDevice, "up"); err != nil {
		m.deleteNamespaceNetwork(context.Background(), meta)
		return err
	}
	if _, err := m.runInNamespace(ctx, meta.NamespaceName, "sysctl", "-w", "net.ipv4.ip_forward=1"); err != nil {
		m.deleteNamespaceNetwork(context.Background(), meta)
		return err
	}
	if err := ensureNamespaceIptablesRule(ctx, m, meta.NamespaceName, "nat", []string{"POSTROUTING", "-s", m.guestPrefix.String(), "-o", namespaceUplinkName, "-j", "MASQUERADE"}); err != nil {
		m.deleteNamespaceNetwork(context.Background(), meta)
		return err
	}
	if err := ensureNamespaceIptablesRule(ctx, m, meta.NamespaceName, "filter", []string{"FORWARD", "-i", meta.BridgeName, "-o", namespaceUplinkName, "-j", "ACCEPT"}); err != nil {
		m.deleteNamespaceNetwork(context.Background(), meta)
		return err
	}
	if err := ensureNamespaceIptablesRule(ctx, m, meta.NamespaceName, "filter", []string{"FORWARD", "-o", meta.BridgeName, "-i", namespaceUplinkName, "-m", "state", "--state", "RELATED,ESTABLISHED", "-j", "ACCEPT"}); err != nil {
		m.deleteNamespaceNetwork(context.Background(), meta)
		return err
	}

	return nil
}

func (m *Manager) deleteNamespaceNetwork(ctx context.Context, meta metadata) {
	_ = m.stopAppForwarder(ctx, &meta)
	_ = m.stopSSHForwarder(ctx, &meta)
	if strings.TrimSpace(meta.HostVethName) != "" {
		_, _ = m.run(ctx, "ip", "link", "del", meta.HostVethName)
	}
	if strings.TrimSpace(meta.NamespaceName) != "" {
		_, _ = m.run(ctx, "ip", "netns", "del", meta.NamespaceName)
	}
}

func (m *Manager) startAppForwarder(ctx context.Context, meta *metadata) error {
	return m.startForwarder(ctx, meta, meta.PrimaryPort, filepath.Join(m.machineDir(meta.Name), portFileName), &meta.AppForwardPID, &meta.AppForwardPort)
}

func (m *Manager) startSSHForwarder(ctx context.Context, meta *metadata) error {
	return m.startForwarder(ctx, meta, 22, filepath.Join(m.machineDir(meta.Name), "ssh-forward.port"), &meta.SSHForwardPID, &meta.SSHForwardPort)
}

func (m *Manager) startForwarder(ctx context.Context, meta *metadata, targetPort int, portFile string, pidTarget *int, portTarget *int) error {
	if meta == nil {
		return fmt.Errorf("metadata is required")
	}
	if *pidTarget > 0 && processAlive(*pidTarget) && *portTarget > 0 {
		return nil
	}

	_ = os.Remove(portFile)

	logFile, err := os.OpenFile(meta.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer logFile.Close()

	cmd := exec.CommandContext(ctx, m.selfBinary,
		"netns-forward",
		"--namespace", meta.NamespaceName,
		"--listen", "127.0.0.1:0",
		"--target", net.JoinHostPort(meta.IPv4, strconv.Itoa(targetPort)),
		"--port-file", portFile,
	)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	*pidTarget = cmd.Process.Pid
	if err := cmd.Process.Release(); err != nil {
		return err
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		body, err := os.ReadFile(portFile)
		if err == nil {
			port, err := strconv.Atoi(strings.TrimSpace(string(body)))
			if err != nil || port <= 0 {
				return fmt.Errorf("invalid forwarder port in %s", portFile)
			}
			*portTarget = port
			return m.storeMetadata(*meta)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for app forwarder port file")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (m *Manager) stopAppForwarder(_ context.Context, meta *metadata) error {
	if meta == nil {
		return nil
	}
	if err := m.stopForwarderProcess(meta.AppForwardPID); err != nil {
		return err
	}
	meta.AppForwardPID = 0
	meta.AppForwardPort = 0
	return nil
}

func (m *Manager) stopSSHForwarder(_ context.Context, meta *metadata) error {
	if meta == nil {
		return nil
	}
	if err := m.stopForwarderProcess(meta.SSHForwardPID); err != nil {
		return err
	}
	meta.SSHForwardPID = 0
	meta.SSHForwardPort = 0
	return nil
}

func (m *Manager) stopForwarderProcess(pid int) error {
	if pid <= 0 {
		return nil
	}
	if pgid, err := syscall.Getpgid(pid); err == nil {
		_ = syscall.Kill(-pgid, syscall.SIGTERM)
	} else {
		_ = syscall.Kill(pid, syscall.SIGTERM)
	}
	time.Sleep(500 * time.Millisecond)
	if pgid, err := syscall.Getpgid(pid); err == nil {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
	} else {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
	return nil
}

func (m *Manager) runInNamespace(ctx context.Context, namespace string, binary string, args ...string) ([]byte, error) {
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}
	fullArgs := append([]string{"netns", "exec", namespace, binary}, args...)
	return m.run(ctx, "ip", fullArgs...)
}

func ensureIptablesRule(ctx context.Context, runner func(context.Context, string, ...string) ([]byte, error), table string, rule []string) error {
	check := append([]string{"-t", table, "-C"}, rule...)
	if _, err := runner(ctx, "iptables", check...); err == nil {
		return nil
	}
	add := append([]string{"-t", table, "-A"}, rule...)
	_, err := runner(ctx, "iptables", add...)
	return err
}

func ensureNamespaceIptablesRule(ctx context.Context, m *Manager, namespace, table string, rule []string) error {
	check := append([]string{"-t", table, "-C"}, rule...)
	if _, err := m.runInNamespace(ctx, namespace, "iptables", check...); err == nil {
		return nil
	}
	add := append([]string{"-t", table, "-A"}, rule...)
	_, err := m.runInNamespace(ctx, namespace, "iptables", add...)
	return err
}

func (m *Manager) defaultUplinkInterface(ctx context.Context) (string, error) {
	output, err := m.run(ctx, "sh", "-lc", `ip route get 1.1.1.1 2>/dev/null | awk '{for (i = 1; i <= NF; i++) if ($i == "dev") {print $(i + 1); exit}}'`)
	if err != nil {
		return "", err
	}
	uplink := strings.TrimSpace(string(output))
	if uplink == "" {
		return "", fmt.Errorf("could not determine uplink interface")
	}
	return uplink, nil
}

func namespaceName(name string) string {
	return "fscns" + compactTapName(name)
}

func hostVethName(name string) string {
	base := compactTapName(name)
	if len(base) > 6 {
		base = base[:6]
	}
	return "fsv" + base
}

func guestMACAddress() string {
	return "02:fc:0a:2a:00:02"
}
