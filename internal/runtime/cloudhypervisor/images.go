package cloudhypervisor

import (
	"context"
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	machineruntime "fascinate/internal/runtime"
)

const (
	imageArtifactFileName = "fascinate-base.raw"
	imageManifestFileName = "manifest.json"
)

type ImageManifest struct {
	Format         string            `json:"format"`
	Version        string            `json:"version"`
	State          string            `json:"state,omitempty"`
	BuiltAt        string            `json:"built_at,omitempty"`
	ValidatedAt    string            `json:"validated_at,omitempty"`
	PromotedAt     string            `json:"promoted_at,omitempty"`
	GuestUser      string            `json:"guest_user,omitempty"`
	SourceImageURL string            `json:"source_image_url,omitempty"`
	BuildInputs    imageBuildInputs  `json:"build_inputs,omitempty"`
	Tools          map[string]string `json:"tools,omitempty"`
	ImagePath      string            `json:"image_path,omitempty"`
	ManifestPath   string            `json:"manifest_path,omitempty"`
}

type ImageBuildResult struct {
	Version      string `json:"version"`
	ArtifactDir  string `json:"artifact_dir"`
	ImagePath    string `json:"image_path"`
	ManifestPath string `json:"manifest_path"`
}

type ImageStatus struct {
	Current    *ImageManifest  `json:"current,omitempty"`
	Previous   *ImageManifest  `json:"previous,omitempty"`
	Candidates []ImageManifest `json:"candidates,omitempty"`
	Releases   []ImageManifest `json:"releases,omitempty"`
}

type imageLayout struct {
	storeDir      string
	cacheDir      string
	candidatesDir string
	releasesDir   string
	currentLink   string
	previousLink  string
}

func (m *Manager) BuildImage(ctx context.Context, version string) (ImageBuildResult, error) {
	layout := m.imageLayout()
	if err := os.MkdirAll(layout.cacheDir, 0o755); err != nil {
		return ImageBuildResult{}, err
	}
	if err := os.MkdirAll(layout.candidatesDir, 0o755); err != nil {
		return ImageBuildResult{}, err
	}
	if err := os.MkdirAll(layout.releasesDir, 0o755); err != nil {
		return ImageBuildResult{}, err
	}

	version = strings.TrimSpace(version)
	if version == "" {
		version = m.now().UTC().Format("20060102-150405")
	}
	candidateDir := filepath.Join(layout.candidatesDir, version)
	releaseDir := filepath.Join(layout.releasesDir, version)
	if pathExists(candidateDir) || pathExists(releaseDir) {
		return ImageBuildResult{}, fmt.Errorf("image version %q already exists", version)
	}

	tmpDir := candidateDir + ".tmp"
	_ = os.RemoveAll(tmpDir)
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return ImageBuildResult{}, err
	}
	defer os.RemoveAll(tmpDir)

	sourceImagePath, err := m.ensureSourceImage(ctx, layout)
	if err != nil {
		return ImageBuildResult{}, err
	}

	buildName := tempImageMachineName("imgbld", version, m.now())
	req := machineruntime.CreateMachineRequest{
		MachineID:    buildName,
		Name:         buildName,
		Image:        sourceImagePath,
		CPU:          "1",
		Memory:       "2GiB",
		RootDiskSize: "20GiB",
		PrimaryPort:  3000,
	}

	_, meta, err := m.createMachineFromBaseImage(ctx, req, func(meta metadata) string {
		return imageCloudInitUserData(meta.Name, meta.GuestUser, m.guestSSHPublicKey, imageProvisioningScript(meta.GuestUser, m.imageBuildInputs, version))
	}, machineReadinessCommand(), false)
	if err != nil {
		return ImageBuildResult{}, err
	}
	defer m.cleanupMachine(context.Background(), meta)

	manifestBody, err := m.runGuestCommandOutput(ctx, meta, "cat "+shellQuote(guestImageManifestPath), nil)
	if err != nil {
		return ImageBuildResult{}, err
	}
	if err := m.runGuestCommand(ctx, meta, imageSealScript(meta.GuestUser)); err != nil {
		return ImageBuildResult{}, err
	}
	if err := m.stopMachineRuntime(ctx, &meta); err != nil {
		return ImageBuildResult{}, err
	}

	imagePath := filepath.Join(tmpDir, imageArtifactFileName)
	if _, err := m.run(ctx, m.qemuImgBinary, "convert", "-O", "raw", meta.DiskPath, imagePath); err != nil {
		return ImageBuildResult{}, err
	}
	if _, err := os.Stat(imagePath); err != nil {
		return ImageBuildResult{}, fmt.Errorf("candidate image missing at %q: %w", imagePath, err)
	}

	manifest := ImageManifest{
		Format:         "fascinate-image-manifest/v1",
		Version:        version,
		State:          "candidate",
		SourceImageURL: m.sourceImageURL,
		ImagePath:      imagePath,
		ManifestPath:   filepath.Join(tmpDir, imageManifestFileName),
	}
	if err := json.Unmarshal(manifestBody, &manifest); err != nil {
		return ImageBuildResult{}, fmt.Errorf("parse guest image manifest: %w", err)
	}
	manifest.State = "candidate"
	manifest.SourceImageURL = m.sourceImageURL
	manifest.ImagePath = imagePath
	manifest.ManifestPath = filepath.Join(tmpDir, imageManifestFileName)
	if manifest.BuildInputs.SourceImageURL == "" {
		manifest.BuildInputs.SourceImageURL = m.sourceImageURL
	}
	if err := writeImageManifest(manifest.ManifestPath, manifest); err != nil {
		return ImageBuildResult{}, err
	}
	if err := os.Rename(tmpDir, candidateDir); err != nil {
		return ImageBuildResult{}, err
	}

	result := ImageBuildResult{
		Version:      version,
		ArtifactDir:  candidateDir,
		ImagePath:    filepath.Join(candidateDir, imageArtifactFileName),
		ManifestPath: filepath.Join(candidateDir, imageManifestFileName),
	}
	manifest.ImagePath = result.ImagePath
	manifest.ManifestPath = result.ManifestPath
	if err := writeImageManifest(result.ManifestPath, manifest); err != nil {
		return ImageBuildResult{}, err
	}
	return result, nil
}

func (m *Manager) ValidateImage(ctx context.Context, version string) (ImageBuildResult, error) {
	dir, manifest, err := m.findImageVersion(version)
	if err != nil {
		return ImageBuildResult{}, err
	}

	validateName := tempImageMachineName("imgval", manifest.Version, m.now())
	req := machineruntime.CreateMachineRequest{
		MachineID:    validateName,
		Name:         validateName,
		Image:        manifest.ImagePath,
		CPU:          "1",
		Memory:       "2GiB",
		RootDiskSize: "20GiB",
		PrimaryPort:  3000,
	}

	_, meta, err := m.createMachineFromBaseImage(ctx, req, func(meta metadata) string {
		return machineBootUserData(meta, m.baseDomain, m.guestSSHPublicKey, m.hostID, m.hostRegion)
	}, machineReadinessCommand(), false)
	if err != nil {
		return ImageBuildResult{}, err
	}
	defer m.cleanupMachine(context.Background(), meta)

	if err := m.runGuestCommand(ctx, meta, imageValidationCommand(meta, m.baseDomain)); err != nil {
		return ImageBuildResult{}, err
	}

	manifest.ValidatedAt = m.now().UTC().Format(time.RFC3339)
	manifest.ManifestPath = filepath.Join(dir, imageManifestFileName)
	manifest.ImagePath = filepath.Join(dir, imageArtifactFileName)
	if err := writeImageManifest(manifest.ManifestPath, manifest); err != nil {
		return ImageBuildResult{}, err
	}

	return ImageBuildResult{
		Version:      manifest.Version,
		ArtifactDir:  dir,
		ImagePath:    manifest.ImagePath,
		ManifestPath: manifest.ManifestPath,
	}, nil
}

func (m *Manager) PromoteImage(_ context.Context, version string) (ImageBuildResult, error) {
	layout := m.imageLayout()
	version = strings.TrimSpace(version)
	if version == "" {
		return ImageBuildResult{}, fmt.Errorf("image version is required")
	}

	candidateDir := filepath.Join(layout.candidatesDir, version)
	releaseDir := filepath.Join(layout.releasesDir, version)
	if !pathExists(candidateDir) {
		if pathExists(releaseDir) {
			return ImageBuildResult{}, fmt.Errorf("image %q is already promoted", version)
		}
		return ImageBuildResult{}, fmt.Errorf("candidate image %q not found", version)
	}

	manifest, err := readImageManifest(filepath.Join(candidateDir, imageManifestFileName))
	if err != nil {
		return ImageBuildResult{}, err
	}
	if strings.TrimSpace(manifest.ValidatedAt) == "" {
		return ImageBuildResult{}, fmt.Errorf("candidate image %q has not been validated", version)
	}

	previousTarget := symlinkTarget(layout.currentLink)
	if err := os.Rename(candidateDir, releaseDir); err != nil {
		return ImageBuildResult{}, err
	}

	manifest.State = "released"
	manifest.PromotedAt = m.now().UTC().Format(time.RFC3339)
	manifest.ImagePath = filepath.Join(releaseDir, imageArtifactFileName)
	manifest.ManifestPath = filepath.Join(releaseDir, imageManifestFileName)
	if err := writeImageManifest(manifest.ManifestPath, manifest); err != nil {
		return ImageBuildResult{}, err
	}

	if previousTarget != "" && previousTarget != releaseDir {
		if err := rewriteSymlink(layout.previousLink, previousTarget); err != nil {
			return ImageBuildResult{}, err
		}
	}
	if err := rewriteSymlink(layout.currentLink, releaseDir); err != nil {
		return ImageBuildResult{}, err
	}

	return ImageBuildResult{
		Version:      manifest.Version,
		ArtifactDir:  releaseDir,
		ImagePath:    manifest.ImagePath,
		ManifestPath: manifest.ManifestPath,
	}, nil
}

func tempImageMachineName(prefix, version string, now time.Time) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(version)))
	return fmt.Sprintf(".%s-%x-%x", strings.TrimSpace(prefix), sum[:4], now.UTC().Unix())
}

func (m *Manager) RollbackImage(_ context.Context, version string) (ImageBuildResult, error) {
	layout := m.imageLayout()
	targetDir := ""
	version = strings.TrimSpace(version)
	if version != "" {
		targetDir = filepath.Join(layout.releasesDir, version)
		if !pathExists(targetDir) {
			return ImageBuildResult{}, fmt.Errorf("release image %q not found", version)
		}
	} else {
		targetDir = symlinkTarget(layout.previousLink)
		if targetDir == "" {
			return ImageBuildResult{}, fmt.Errorf("no previous promoted image available for rollback")
		}
	}

	currentTarget := symlinkTarget(layout.currentLink)
	if currentTarget == "" {
		return ImageBuildResult{}, fmt.Errorf("no current promoted image is configured")
	}
	if currentTarget == targetDir {
		return ImageBuildResult{}, fmt.Errorf("image %q is already current", filepath.Base(targetDir))
	}

	if err := rewriteSymlink(layout.previousLink, currentTarget); err != nil {
		return ImageBuildResult{}, err
	}
	if err := rewriteSymlink(layout.currentLink, targetDir); err != nil {
		return ImageBuildResult{}, err
	}

	manifestPath := filepath.Join(targetDir, imageManifestFileName)
	manifest, err := readImageManifest(manifestPath)
	if err != nil {
		return ImageBuildResult{}, err
	}
	manifest.ImagePath = filepath.Join(targetDir, imageArtifactFileName)
	manifest.ManifestPath = manifestPath

	return ImageBuildResult{
		Version:      manifest.Version,
		ArtifactDir:  targetDir,
		ImagePath:    manifest.ImagePath,
		ManifestPath: manifest.ManifestPath,
	}, nil
}

func (m *Manager) ImageStatus(_ context.Context) (ImageStatus, error) {
	layout := m.imageLayout()
	status := ImageStatus{}
	if currentTarget := symlinkTarget(layout.currentLink); currentTarget != "" {
		manifest, err := readImageManifest(filepath.Join(currentTarget, imageManifestFileName))
		if err != nil {
			return ImageStatus{}, err
		}
		status.Current = &manifest
	}
	if previousTarget := symlinkTarget(layout.previousLink); previousTarget != "" {
		manifest, err := readImageManifest(filepath.Join(previousTarget, imageManifestFileName))
		if err != nil {
			return ImageStatus{}, err
		}
		status.Previous = &manifest
	}
	candidates, err := listImageManifests(layout.candidatesDir)
	if err != nil {
		return ImageStatus{}, err
	}
	releases, err := listImageManifests(layout.releasesDir)
	if err != nil {
		return ImageStatus{}, err
	}
	status.Candidates = candidates
	status.Releases = releases
	return status, nil
}

func (m *Manager) ensureSourceImage(ctx context.Context, layout imageLayout) (string, error) {
	url := strings.TrimSpace(m.sourceImageURL)
	if url == "" {
		return "", fmt.Errorf("base source image URL is required")
	}
	name := filepath.Base(strings.Split(url, "?")[0])
	if name == "" || name == "." || name == "/" {
		name = "ubuntu-cloudimg.qcow2"
	}
	path := filepath.Join(layout.cacheDir, name)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if _, err := m.run(ctx, "curl", "-fsSL", url, "-o", path); err != nil {
		return "", err
	}
	return path, nil
}

func (m *Manager) createMachineFromBaseImage(ctx context.Context, req machineruntime.CreateMachineRequest, userData func(metadata) string, readinessCommand string, startForwarders bool) (machineruntime.Machine, metadata, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return machineruntime.Machine{}, metadata{}, fmt.Errorf("machine name is required")
	}
	baseImage := strings.TrimSpace(req.Image)
	if baseImage == "" {
		return machineruntime.Machine{}, metadata{}, fmt.Errorf("machine image is required")
	}

	machineDir := m.machineDir(name)
	if _, err := os.Stat(machineDir); err == nil {
		return machineruntime.Machine{}, metadata{}, fmt.Errorf("machine %q already exists", name)
	} else if !errors.Is(err, os.ErrNotExist) {
		return machineruntime.Machine{}, metadata{}, err
	}
	if err := os.MkdirAll(machineDir, 0o755); err != nil {
		return machineruntime.Machine{}, metadata{}, err
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
		return machineruntime.Machine{}, metadata{}, err
	}
	if err := m.storeMetadata(meta); err != nil {
		_ = os.RemoveAll(machineDir)
		return machineruntime.Machine{}, metadata{}, err
	}
	if err := m.createOverlayDisk(ctx, baseImage, meta.DiskPath, meta.Disk); err != nil {
		_ = os.RemoveAll(machineDir)
		return machineruntime.Machine{}, metadata{}, err
	}
	if err := m.writeSeedImageWithUserData(ctx, meta, userData(meta)); err != nil {
		_ = os.RemoveAll(machineDir)
		return machineruntime.Machine{}, metadata{}, err
	}
	if err := m.createNamespaceNetwork(ctx, meta); err != nil {
		_ = os.RemoveAll(machineDir)
		return machineruntime.Machine{}, metadata{}, err
	}
	if err := m.startVM(ctx, &meta); err != nil {
		_ = m.cleanupMachine(context.Background(), meta)
		return machineruntime.Machine{}, metadata{}, err
	}
	m.networkMu.Unlock()
	networkLocked = false
	if startForwarders {
		if err := m.startAppForwarder(ctx, &meta); err != nil {
			_ = m.cleanupMachine(context.Background(), meta)
			return machineruntime.Machine{}, metadata{}, err
		}
		if err := m.startSSHForwarder(ctx, &meta); err != nil {
			_ = m.cleanupMachine(context.Background(), meta)
			return machineruntime.Machine{}, metadata{}, err
		}
	}
	if err := m.waitForGuestCommand(ctx, meta, readinessCommand); err != nil {
		_ = m.cleanupMachine(context.Background(), meta)
		return machineruntime.Machine{}, metadata{}, err
	}
	return m.machineFromMetadata(meta), meta, nil
}

func (m *Manager) waitForGuestCommand(ctx context.Context, meta metadata, command string) error {
	command = strings.TrimSpace(command)
	if command == "" {
		command = machineReadinessCommand()
	}
	deadline := time.Now().Add(15 * time.Minute)
	for {
		err := m.runGuestCommand(ctx, meta, command)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for guest readiness on %s", net.JoinHostPort(strings.TrimSpace(meta.IPv4), "22"))
		}
		time.Sleep(2 * time.Second)
	}
}

func (m *Manager) writeSeedImageWithUserData(ctx context.Context, meta metadata, userData string) error {
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

func (m *Manager) imageLayout() imageLayout {
	storeDir := strings.TrimSpace(m.imageStoreDir)
	if storeDir == "" {
		storeDir = filepath.Join(filepath.Dir(m.stateDir), "images")
	}
	return imageLayout{
		storeDir:      storeDir,
		cacheDir:      filepath.Join(storeDir, "cache"),
		candidatesDir: filepath.Join(storeDir, "candidates"),
		releasesDir:   filepath.Join(storeDir, "releases"),
		currentLink:   filepath.Join(storeDir, "current"),
		previousLink:  filepath.Join(storeDir, "previous"),
	}
}

func (m *Manager) findImageVersion(version string) (string, ImageManifest, error) {
	version = strings.TrimSpace(version)
	if version == "" {
		return "", ImageManifest{}, fmt.Errorf("image version is required")
	}
	layout := m.imageLayout()
	for _, root := range []string{layout.candidatesDir, layout.releasesDir} {
		dir := filepath.Join(root, version)
		if !pathExists(dir) {
			continue
		}
		manifest, err := readImageManifest(filepath.Join(dir, imageManifestFileName))
		if err != nil {
			return "", ImageManifest{}, err
		}
		manifest.ImagePath = filepath.Join(dir, imageArtifactFileName)
		manifest.ManifestPath = filepath.Join(dir, imageManifestFileName)
		return dir, manifest, nil
	}
	return "", ImageManifest{}, fmt.Errorf("image version %q not found", version)
}

func listImageManifests(root string) ([]ImageManifest, error) {
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]ImageManifest, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		manifestPath := filepath.Join(root, entry.Name(), imageManifestFileName)
		manifest, err := readImageManifest(manifestPath)
		if err != nil {
			return nil, err
		}
		manifest.ImagePath = filepath.Join(root, entry.Name(), imageArtifactFileName)
		manifest.ManifestPath = manifestPath
		out = append(out, manifest)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Version > out[j].Version
	})
	return out, nil
}

func readImageManifest(path string) (ImageManifest, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return ImageManifest{}, err
	}
	var manifest ImageManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return ImageManifest{}, err
	}
	return manifest, nil
}

func writeImageManifest(path string, manifest ImageManifest) error {
	body, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(body, '\n'), 0o600)
}

func rewriteSymlink(path, target string) error {
	target = strings.TrimSpace(target)
	if target == "" {
		return fmt.Errorf("symlink target is required")
	}
	_ = os.Remove(path)
	return os.Symlink(target, path)
}

func symlinkTarget(path string) string {
	target, err := os.Readlink(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(target)
}

func sanitizeImageVersion(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '-' || r == '_':
			builder.WriteRune(r)
		}
	}
	if builder.Len() == 0 {
		return "image"
	}
	return builder.String()
}
