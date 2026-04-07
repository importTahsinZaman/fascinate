package cli

import (
	"archive/tar"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"
)

type transferReadCounter struct {
	reader io.Reader
	count  int64
}

func (r *transferReadCounter) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.count += int64(n)
	return n, err
}

func (r Runner) runUpload(ctx context.Context, args []string) error {
	normalizedArgs, err := reorderKnownFlags(args, map[string]bool{
		"json": true,
	}, map[string]bool{
		"base-url": true,
	})
	if err != nil {
		return err
	}
	flags := flagSet("upload", r.Stderr)
	baseURL := flags.String("base-url", "", "Fascinate API base URL")
	jsonOutput := flags.Bool("json", false, "print JSON to stdout")
	if err := flags.Parse(normalizedArgs); err != nil {
		return err
	}
	if flags.NArg() != 2 {
		return fmt.Errorf("usage: fascinate upload [--base-url <url>] [--json] <local-path> <machine>:<remote-path>")
	}

	localPath := flags.Arg(0)
	machineName, remotePath, err := parseRemoteMachinePath(flags.Arg(1))
	if err != nil {
		return err
	}
	archiveRoot, err := uploadArchiveRoot(localPath, remotePath)
	if err != nil {
		return err
	}

	client, err := r.authedClient(*baseURL)
	if err != nil {
		return err
	}

	reader, writer := io.Pipe()
	go func() {
		writer.CloseWithError(writeTransferArchive(writer, localPath, archiveRoot))
	}()

	result, err := client.UploadArchive(ctx, machineName, remotePath, reader)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeJSON(r.Stdout, result)
	}
	_, err = fmt.Fprintf(r.Stdout, "Uploaded %s to %s:%s (%d bytes)\n", localPath, machineName, remotePath, result.BytesTransferred)
	return err
}

func (r Runner) runDownload(ctx context.Context, args []string) error {
	normalizedArgs, err := reorderKnownFlags(args, map[string]bool{
		"json": true,
	}, map[string]bool{
		"base-url": true,
	})
	if err != nil {
		return err
	}
	flags := flagSet("download", r.Stderr)
	baseURL := flags.String("base-url", "", "Fascinate API base URL")
	jsonOutput := flags.Bool("json", false, "print JSON to stdout")
	if err := flags.Parse(normalizedArgs); err != nil {
		return err
	}
	if flags.NArg() != 2 {
		return fmt.Errorf("usage: fascinate download [--base-url <url>] [--json] <machine>:<remote-path> <local-path>")
	}

	machineName, remotePath, err := parseRemoteMachinePath(flags.Arg(0))
	if err != nil {
		return err
	}
	localPath := flags.Arg(1)

	client, err := r.authedClient(*baseURL)
	if err != nil {
		return err
	}
	body, err := client.DownloadArchive(ctx, machineName, remotePath)
	if err != nil {
		return err
	}
	defer body.Close()

	counter := &transferReadCounter{reader: body}
	if err := extractTransferArchive(counter, localPath); err != nil {
		return err
	}
	result := map[string]any{
		"machine_name":      machineName,
		"path":              remotePath,
		"direction":         "download",
		"destination":       localPath,
		"bytes_transferred": counter.count,
	}
	if *jsonOutput {
		return writeJSON(r.Stdout, result)
	}
	_, err = fmt.Fprintf(r.Stdout, "Downloaded %s:%s to %s\n", machineName, remotePath, localPath)
	return err
}

func parseRemoteMachinePath(value string) (string, string, error) {
	trimmed := strings.TrimSpace(value)
	separator := strings.IndexByte(trimmed, ':')
	if separator <= 0 || separator >= len(trimmed)-1 {
		return "", "", fmt.Errorf("remote path must use <machine>:<path>")
	}
	machineName := strings.TrimSpace(trimmed[:separator])
	remotePath := strings.TrimSpace(trimmed[separator+1:])
	if machineName == "" || remotePath == "" {
		return "", "", fmt.Errorf("remote path must use <machine>:<path>")
	}
	return machineName, remotePath, nil
}

func uploadArchiveRoot(localPath, remotePath string) (string, error) {
	info, err := os.Stat(localPath)
	if err != nil {
		return "", err
	}
	if strings.HasSuffix(strings.TrimSpace(remotePath), "/") {
		abs, err := filepath.Abs(localPath)
		if err != nil {
			return "", err
		}
		return filepath.Base(abs), nil
	}
	root := pathpkg.Base(strings.TrimSpace(remotePath))
	if root == "" || root == "." || root == "/" {
		return "", fmt.Errorf("remote path must name a file or directory")
	}
	if info.IsDir() && root == "." {
		return "", fmt.Errorf("remote path must name a directory")
	}
	return root, nil
}

func writeTransferArchive(w io.Writer, sourcePath, rootName string) error {
	sourceInfo, err := os.Lstat(sourcePath)
	if err != nil {
		return err
	}
	rootName = strings.Trim(strings.ReplaceAll(rootName, "\\", "/"), "/")
	if rootName == "" {
		return fmt.Errorf("archive root name is required")
	}
	tw := tar.NewWriter(w)
	defer tw.Close()

	if !sourceInfo.IsDir() {
		return writeTransferArchiveEntry(tw, sourcePath, rootName, sourceInfo)
	}

	return filepath.Walk(sourcePath, func(current string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		name := rootName
		if current != sourcePath {
			rel, err := filepath.Rel(sourcePath, current)
			if err != nil {
				return err
			}
			name = pathpkg.Join(rootName, filepath.ToSlash(rel))
		}
		return writeTransferArchiveEntry(tw, current, name, info)
	})
}

func writeTransferArchiveEntry(tw *tar.Writer, localPath, archiveName string, info os.FileInfo) error {
	linkTarget := ""
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(localPath)
		if err != nil {
			return err
		}
		linkTarget = target
	}
	header, err := tar.FileInfoHeader(info, linkTarget)
	if err != nil {
		return err
	}
	header.Name = strings.TrimPrefix(strings.ReplaceAll(archiveName, "\\", "/"), "/")
	if info.IsDir() && !strings.HasSuffix(header.Name, "/") {
		header.Name += "/"
	}
	if err := tw.WriteHeader(header); err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	file, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(tw, file)
	return err
}

func extractTransferArchive(reader io.Reader, destination string) error {
	destination = strings.TrimSpace(destination)
	if destination == "" {
		return fmt.Errorf("local destination is required")
	}
	tempDir, err := os.MkdirTemp("", "fascinate-download-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)

	tr := tar.NewReader(reader)
	roots := map[string]struct{}{}
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name, err := sanitizeArchivePath(header.Name)
		if err != nil {
			return err
		}
		if name == "" {
			continue
		}
		parts := strings.Split(name, "/")
		roots[parts[0]] = struct{}{}
		target := filepath.Join(tempDir, filepath.FromSlash(name))
		if err := writeArchiveEntry(target, header, tr); err != nil {
			return err
		}
	}
	if len(roots) != 1 {
		return fmt.Errorf("downloaded archive must contain exactly one root entry")
	}
	var rootName string
	for rootName = range roots {
	}
	rootPath := filepath.Join(tempDir, filepath.FromSlash(rootName))
	return installDownloadedRoot(rootPath, destination)
}

func sanitizeArchivePath(name string) (string, error) {
	name = strings.TrimSpace(strings.ReplaceAll(name, "\\", "/"))
	name = strings.TrimPrefix(name, "./")
	if name == "" {
		return "", nil
	}
	clean := pathpkg.Clean(name)
	if clean == "." {
		return "", nil
	}
	if strings.HasPrefix(clean, "../") || clean == ".." || strings.HasPrefix(clean, "/") {
		return "", fmt.Errorf("downloaded archive contains an unsafe path: %s", name)
	}
	return clean, nil
}

func writeArchiveEntry(target string, header *tar.Header, reader io.Reader) error {
	switch header.Typeflag {
	case tar.TypeDir:
		return os.MkdirAll(target, header.FileInfo().Mode().Perm())
	case tar.TypeReg, tar.TypeRegA:
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, header.FileInfo().Mode().Perm())
		if err != nil {
			return err
		}
		defer file.Close()
		_, err = io.Copy(file, reader)
		return err
	case tar.TypeSymlink:
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.Symlink(header.Linkname, target)
	case tar.TypeXGlobalHeader, tar.TypeXHeader:
		return nil
	default:
		return fmt.Errorf("unsupported archive entry type %d for %s", header.Typeflag, header.Name)
	}
}

func installDownloadedRoot(rootPath, destination string) error {
	originalDestination := destination
	destination = filepath.Clean(destination)
	targetPath := destination
	if strings.HasSuffix(originalDestination, string(os.PathSeparator)) {
		if err := os.MkdirAll(destination, 0o755); err != nil {
			return err
		}
		targetPath = filepath.Join(destination, filepath.Base(rootPath))
	} else if info, err := os.Stat(destination); err == nil && info.IsDir() {
		targetPath = filepath.Join(destination, filepath.Base(rootPath))
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	if _, err := os.Stat(targetPath); err == nil {
		return fmt.Errorf("destination already exists: %s", targetPath)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}
	return os.Rename(rootPath, targetPath)
}

func flagSet(name string, stderr io.Writer) *flag.FlagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(stderr)
	return flags
}
