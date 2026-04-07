package browserterm

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	pathpkg "path"
	"strings"

	"fascinate/internal/controlplane"
)

type FileTransfer struct {
	MachineName      string `json:"machine_name"`
	Path             string `json:"path"`
	Direction        string `json:"direction"`
	BytesTransferred int64  `json:"bytes_transferred"`
}

type countingReader struct {
	reader io.Reader
	count  int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.count += int64(n)
	return n, err
}

type countingWriter struct {
	writer io.Writer
	count  int64
}

func (w *countingWriter) Write(p []byte) (int, error) {
	n, err := w.writer.Write(p)
	w.count += int64(n)
	return n, err
}

func (m *Manager) UploadArchive(ctx context.Context, userEmail, machineName, destinationPath string, archive io.Reader) (FileTransfer, error) {
	if archive == nil {
		return FileTransfer{}, fmt.Errorf("transfer archive is required")
	}
	machine, guestUser, err := m.transferMachine(ctx, userEmail, machineName)
	if err != nil {
		return FileTransfer{}, err
	}
	extractDir, err := uploadExtractDir(destinationPath, guestUser)
	if err != nil {
		return FileTransfer{}, err
	}

	counter := &countingReader{reader: archive}
	command := fmt.Sprintf("mkdir -p %s && tar -xpf - -C %s", shellLiteral(extractDir), shellLiteral(extractDir))
	if err := m.streamGuestCommand(ctx, machine, guestUser, command, counter, io.Discard); err != nil {
		return FileTransfer{}, err
	}
	return FileTransfer{
		MachineName:      machine.Name,
		Path:             strings.TrimSpace(destinationPath),
		Direction:        "upload",
		BytesTransferred: counter.count,
	}, nil
}

func (m *Manager) DownloadArchive(ctx context.Context, userEmail, machineName, sourcePath string, output io.Writer) (FileTransfer, error) {
	if output == nil {
		return FileTransfer{}, fmt.Errorf("download output is required")
	}
	machine, guestUser, err := m.transferMachine(ctx, userEmail, machineName)
	if err != nil {
		return FileTransfer{}, err
	}
	parentDir, baseName, normalizedPath, err := downloadSourceParts(sourcePath, guestUser)
	if err != nil {
		return FileTransfer{}, err
	}

	counter := &countingWriter{writer: output}
	command := fmt.Sprintf("cd %s && tar -cpf - -- %s", shellLiteral(parentDir), shellLiteral(baseName))
	if err := m.streamGuestCommand(ctx, machine, guestUser, command, nil, counter); err != nil {
		return FileTransfer{}, err
	}
	return FileTransfer{
		MachineName:      machine.Name,
		Path:             normalizedPath,
		Direction:        "download",
		BytesTransferred: counter.count,
	}, nil
}

func (m *Manager) transferMachine(ctx context.Context, userEmail, machineName string) (controlplane.Machine, string, error) {
	machine, err := m.machines.GetMachine(ctx, machineName, userEmail)
	if err != nil {
		return controlplane.Machine{}, "", err
	}
	if !strings.EqualFold(machine.State, "RUNNING") {
		return controlplane.Machine{}, "", fmt.Errorf("machine %q must be running before file transfer is available", machineName)
	}
	hostID := strings.TrimSpace(machine.HostID)
	if hostID == "" {
		hostID = m.localHostID
	}
	if hostID != m.localHostID {
		return controlplane.Machine{}, "", fmt.Errorf("file transfer gateway for host %q is not available", hostID)
	}
	if machine.Runtime == nil {
		return controlplane.Machine{}, "", fmt.Errorf("machine %q is not available", machineName)
	}
	return machine, guestUserForMachine(machine), nil
}

func (m *Manager) streamGuestCommand(ctx context.Context, machine controlplane.Machine, guestUser, shellCommand string, stdin io.Reader, stdout io.Writer) error {
	if machine.Runtime == nil {
		return fmt.Errorf("machine %q is not available", machine.Name)
	}
	targetHost := strings.TrimSpace(machine.Runtime.SSHHost)
	targetPort := machine.Runtime.SSHPort
	if targetHost == "" || targetPort <= 0 {
		return fmt.Errorf("machine %q does not have a reachable guest shell endpoint", machine.Name)
	}
	if err := m.waitForGuestAccess(ctx, machine, targetHost, targetPort, guestUser); err != nil {
		return err
	}

	args := append(m.guestSSHArgs(guestUser, targetHost, targetPort), guestSSHRemoteCommand("xterm-256color", shellCommand))
	cmd := exec.CommandContext(ctx, m.sshClientBinary, args...)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		text := strings.TrimSpace(stderr.String())
		if text == "" {
			return err
		}
		return fmt.Errorf("%w: %s", err, text)
	}
	return nil
}

func uploadExtractDir(destinationPath, guestUser string) (string, error) {
	normalized := normalizeTransferPath(destinationPath, guestUser)
	if normalized == "" {
		return "", fmt.Errorf("destination path is required")
	}
	if strings.HasSuffix(normalized, "/") {
		trimmed := strings.TrimRight(normalized, "/")
		if trimmed == "" {
			return "/", nil
		}
		return trimmed, nil
	}
	dir := pathpkg.Dir(normalized)
	if dir == "" {
		return ".", nil
	}
	return dir, nil
}

func downloadSourceParts(sourcePath, guestUser string) (parentDir string, baseName string, normalized string, err error) {
	normalized = strings.TrimRight(normalizeTransferPath(sourcePath, guestUser), "/")
	if normalized == "" || normalized == "/" || normalized == "." {
		return "", "", "", fmt.Errorf("source path is required")
	}
	parentDir = pathpkg.Dir(normalized)
	if parentDir == "" {
		parentDir = "."
	}
	baseName = pathpkg.Base(normalized)
	if baseName == "" || baseName == "." || baseName == "/" {
		return "", "", "", fmt.Errorf("source path is required")
	}
	return parentDir, baseName, normalized, nil
}

func normalizeTransferPath(value, guestUser string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	keepTrailing := strings.HasSuffix(value, "/")
	trimmed := strings.TrimRight(value, "/")
	if trimmed == "" {
		trimmed = "/"
	}
	normalized := normalizeGuestCwd(trimmed, guestUser)
	if normalized == "" {
		normalized = trimmed
	}
	if keepTrailing && normalized != "/" {
		normalized += "/"
	}
	return normalized
}
