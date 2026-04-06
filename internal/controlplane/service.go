package controlplane

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"fascinate/internal/config"
	"fascinate/internal/database"
	machineruntime "fascinate/internal/runtime"
	"fascinate/internal/toolauth"
)

var machineNamePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
var memoryLimitPattern = regexp.MustCompile(`(?i)^([0-9]+(?:\.[0-9]+)?)\s*([kmgt]i?b?|b)?$`)

const (
	machineStateCreating            = "CREATING"
	machineStateRunning             = "RUNNING"
	machineStateStopped             = "STOPPED"
	machineStateFailed              = "FAILED"
	reconcileMachineRecoveryTimeout = 90 * time.Second

	snapshotStateCreating = "CREATING"
	snapshotStateReady    = "READY"
	snapshotStateFailed   = "FAILED"
)

type Service struct {
	cfg         config.Config
	store       *database.Store
	toolAuth    toolAuthManager
	localHostID string
	executors   map[string]hostExecutor
	persistTTL  time.Duration

	userLocksMu sync.Mutex
	userLocks   map[string]*sync.Mutex

	hostLocksMu sync.Mutex
	hostLocks   map[string]*sync.Mutex

	createMu      sync.Mutex
	createCancels map[string]context.CancelFunc
}

type Machine struct {
	ID           string                  `json:"id"`
	Name         string                  `json:"name"`
	OwnerEmail   string                  `json:"owner_email"`
	HostID       string                  `json:"host_id,omitempty"`
	State        string                  `json:"state"`
	PrimaryPort  int                     `json:"primary_port"`
	URL          string                  `json:"url,omitempty"`
	ShowTutorial bool                    `json:"show_tutorial,omitempty"`
	CreatedAt    string                  `json:"created_at"`
	UpdatedAt    string                  `json:"updated_at"`
	Runtime      *machineruntime.Machine `json:"runtime,omitempty"`
}

type CreateMachineInput struct {
	Name         string
	OwnerEmail   string
	SnapshotName string
}

type ForkMachineInput struct {
	SourceName string
	TargetName string
	OwnerEmail string
}

type Snapshot struct {
	ID                string                   `json:"id"`
	Name              string                   `json:"name"`
	OwnerEmail        string                   `json:"owner_email"`
	HostID            string                   `json:"host_id,omitempty"`
	SourceMachineName string                   `json:"source_machine_name,omitempty"`
	State             string                   `json:"state"`
	ArtifactDir       string                   `json:"artifact_dir,omitempty"`
	DiskSizeBytes     int64                    `json:"disk_size_bytes,omitempty"`
	MemorySizeBytes   int64                    `json:"memory_size_bytes,omitempty"`
	RuntimeVersion    string                   `json:"runtime_version,omitempty"`
	FirmwareVersion   string                   `json:"firmware_version,omitempty"`
	CreatedAt         string                   `json:"created_at"`
	UpdatedAt         string                   `json:"updated_at"`
	Runtime           *machineruntime.Snapshot `json:"runtime,omitempty"`
}

type CreateSnapshotInput struct {
	MachineName  string
	SnapshotName string
	OwnerEmail   string
}

type toolAuthManager interface {
	RestoreAll(context.Context, string, string, string) error
	CaptureAll(context.Context, string, string, string) error
	CaptureAllNonDestructive(context.Context, string, string, string) error
	ListProfiles(context.Context, string) ([]toolauth.Profile, error)
}

func New(cfg config.Config, store *database.Store, runtime machineruntime.Manager, extras ...toolAuthManager) *Service {
	var toolAuth toolAuthManager
	if len(extras) > 0 {
		toolAuth = extras[0]
	}
	localHostID := strings.TrimSpace(cfg.HostID)
	if localHostID == "" {
		localHostID = "local-host"
	}
	if strings.TrimSpace(cfg.HostName) == "" {
		cfg.HostName = localHostID
	}
	if strings.TrimSpace(cfg.HostRegion) == "" {
		cfg.HostRegion = "local"
	}
	if strings.TrimSpace(cfg.HostRole) == "" {
		cfg.HostRole = "combined"
	}
	if cfg.HostHeartbeatInterval <= 0 {
		cfg.HostHeartbeatInterval = 30 * time.Second
	}
	if strings.TrimSpace(cfg.DefaultUserMaxCPU) == "" {
		cfg.DefaultUserMaxCPU = "2"
	}
	if strings.TrimSpace(cfg.DefaultUserMaxRAM) == "" {
		cfg.DefaultUserMaxRAM = "8GiB"
	}
	if strings.TrimSpace(cfg.DefaultUserMaxDisk) == "" {
		cfg.DefaultUserMaxDisk = "50GiB"
	}
	if cfg.DefaultUserMaxMachines <= 0 {
		cfg.DefaultUserMaxMachines = cfg.EffectiveDefaultUserMaxMachines()
	}
	if cfg.DefaultUserMaxSnapshots <= 0 {
		cfg.DefaultUserMaxSnapshots = 5
	}
	if strings.TrimSpace(cfg.HostMinFreeDisk) == "" {
		cfg.HostMinFreeDisk = "150GiB"
	}
	if cfg.MaxMachinesPerUser <= 0 {
		cfg.MaxMachinesPerUser = cfg.EffectiveDefaultUserMaxMachines()
	}

	service := &Service{
		cfg:           cfg,
		store:         store,
		toolAuth:      toolAuth,
		localHostID:   localHostID,
		executors:     map[string]hostExecutor{localHostID: newLocalHostExecutor(runtime)},
		persistTTL:    30 * time.Second,
		userLocks:     map[string]*sync.Mutex{},
		hostLocks:     map[string]*sync.Mutex{},
		createCancels: map[string]context.CancelFunc{},
	}

	return service.withLocalHostSeeded()
}

func (s *Service) withLocalHostSeeded() *Service {
	if s == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _ = s.EnsureLocalHost(ctx)
	_ = s.HeartbeatLocalHost(ctx)
	return s
}

func (s *Service) ListMachines(ctx context.Context, ownerEmail string) ([]Machine, error) {
	records, err := s.store.ListMachines(ctx, ownerEmail)
	if err != nil {
		return nil, err
	}

	user, err := s.store.GetUserByEmail(ctx, ownerEmail)
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return nil, err
	}

	liveMachines := map[string]machineruntime.Machine{}
	seenHosts := map[string]struct{}{}
	for _, record := range records {
		hostID := machineHostID(record, s.localHostID)
		if _, ok := seenHosts[hostID]; ok {
			continue
		}
		seenHosts[hostID] = struct{}{}
		executor, err := s.executorForHostID(hostID)
		if err != nil {
			continue
		}
		runtimeMachines, err := executor.ListMachines(ctx)
		if err != nil {
			continue
		}
		for _, machine := range runtimeMachines {
			liveMachines[machine.Name] = machine
		}
	}

	out := make([]Machine, 0, len(records))
	for _, record := range records {
		machine := s.machineFromRecord(ctx, record, liveMachines[runtimeNameForRecord(record)])
		if len(records) == 1 && user.TutorialCompletedAt == nil && strings.EqualFold(machine.State, machineStateRunning) {
			machine.ShowTutorial = true
		}
		out = append(out, machine)
	}

	return out, nil
}

func (s *Service) GetPublicMachine(ctx context.Context, name string) (Machine, error) {
	record, err := s.store.GetMachineByName(ctx, normalizeMachineName(name))
	if err != nil {
		return Machine{}, err
	}

	return s.machineFromRecordWithRuntime(ctx, record)
}

func (s *Service) GetMachine(ctx context.Context, name, ownerEmail string) (Machine, error) {
	record, err := s.ownedMachineRecord(ctx, name, ownerEmail)
	if err != nil {
		return Machine{}, err
	}

	return s.machineFromRecordWithRuntime(ctx, record)
}

func (s *Service) machineFromRecordWithRuntime(ctx context.Context, record database.MachineRecord) (Machine, error) {
	executor, err := s.executorForHostID(machineHostID(record, s.localHostID))
	if err != nil {
		return Machine{}, err
	}
	liveMachine, err := executor.GetMachine(ctx, runtimeNameForRecord(record))
	if err != nil {
		if errors.Is(err, machineruntime.ErrMachineNotFound) {
			if machineStateAllowsMissingRuntime(record.State) {
				return s.machineFromRecord(ctx, record, machineruntime.Machine{}), nil
			}
			_ = s.store.UpdateMachineState(ctx, record.ID, "missing")
			record.State = "missing"
			return s.machineFromRecord(ctx, record, machineruntime.Machine{}), nil
		}
		return Machine{}, err
	}

	return s.machineFromRecord(ctx, record, liveMachine), nil
}

func (s *Service) CreateMachine(ctx context.Context, input CreateMachineInput) (Machine, error) {
	name, err := validateMachineName(input.Name)
	if err != nil {
		return Machine{}, err
	}
	ownerEmail := normalizeEmail(input.OwnerEmail)
	if ownerEmail == "" {
		return Machine{}, fmt.Errorf("owner email is required")
	}
	unlock := s.lockUserMutations(ownerEmail)
	defer unlock()

	user, err := s.ensureUser(ctx, ownerEmail)
	if err != nil {
		return Machine{}, err
	}
	existingRecords, err := s.store.ListMachines(ctx, ownerEmail)
	if err != nil {
		return Machine{}, err
	}
	spec, err := s.defaultMachineSpec()
	if err != nil {
		return Machine{}, err
	}
	if err := s.validateMachineSizeLimit(spec.CPU, formatByteSize(spec.MemoryBytes), formatByteSize(spec.DiskBytes)); err != nil {
		return Machine{}, err
	}

	var sourceSnapshotID *string
	var hostID string
	if snapshotName := strings.TrimSpace(input.SnapshotName); snapshotName != "" {
		snapshotRecord, err := s.store.GetSnapshotByName(ctx, user.ID, snapshotName)
		if err != nil {
			return Machine{}, err
		}
		if !strings.EqualFold(snapshotRecord.State, snapshotStateReady) {
			return Machine{}, fmt.Errorf("snapshot %q is %s", snapshotName, strings.ToLower(strings.TrimSpace(snapshotRecord.State)))
		}
		spec, err = s.snapshotSpecForRecord(snapshotRecord)
		if err != nil {
			return Machine{}, err
		}
		if err := s.validateMachineSizeLimit(spec.CPU, formatByteSize(spec.MemoryBytes), formatByteSize(spec.DiskBytes)); err != nil {
			return Machine{}, err
		}
		hostID = snapshotHostID(snapshotRecord)
		if hostID == "" {
			hostID = s.localHostID
		}
		if _, err := s.executorForHostID(hostID); err != nil {
			return Machine{}, fmt.Errorf("snapshot host unavailable: %w", err)
		}
		sourceSnapshotID = &snapshotRecord.ID
	} else {
		host, err := s.getPlacementHost(ctx, spec.CPU, formatByteSize(spec.MemoryBytes), formatByteSize(spec.DiskBytes))
		if err != nil {
			return Machine{}, err
		}
		hostID = host.ID
		s.syncToolAuthFromOwnerRunningMachines(ctx, user.ID, name)
	}

	persistCtx, cancel := s.newPersistContext()
	defer cancel()

	record, err := func() (database.MachineRecord, error) {
		unlockHost := s.lockHostMutations(hostID)
		defer unlockHost()

		hostRecord, err := s.hostByID(persistCtx, hostID)
		if err != nil {
			return database.MachineRecord{}, err
		}
		if err := s.ensureUserCanCreateMachine(persistCtx, user, spec); err != nil {
			s.recordEventBestEffort(&user.ID, nil, "machine.create.rejected", map[string]any{
				"machine_name":    name,
				"source_snapshot": strings.TrimSpace(input.SnapshotName),
				"cpu":             spec.CPU,
				"memory_bytes":    spec.MemoryBytes,
				"disk_bytes":      spec.DiskBytes,
				"error":           err.Error(),
			})
			return database.MachineRecord{}, err
		}
		if err := s.ensureHostCanFitMachine(persistCtx, hostRecord, spec); err != nil {
			s.recordEventBestEffort(&user.ID, nil, "machine.create.rejected", map[string]any{
				"machine_name":    name,
				"source_snapshot": strings.TrimSpace(input.SnapshotName),
				"host_id":         hostRecord.ID,
				"cpu":             spec.CPU,
				"memory_bytes":    spec.MemoryBytes,
				"disk_bytes":      spec.DiskBytes,
				"error":           err.Error(),
			})
			return database.MachineRecord{}, err
		}

		return s.store.CreateMachine(persistCtx, database.CreateMachineParams{
			ID:               uuid.NewString(),
			Name:             name,
			OwnerUserID:      user.ID,
			HostID:           &hostID,
			RuntimeName:      name,
			SourceSnapshotID: sourceSnapshotID,
			State:            machineStateCreating,
			CPU:              spec.CPU,
			MemoryBytes:      spec.MemoryBytes,
			DiskBytes:        spec.DiskBytes,
			PrimaryPort:      s.cfg.DefaultPrimaryPort,
		})
	}()
	if err != nil {
		return Machine{}, err
	}
	s.recordEventBestEffort(&user.ID, &record.ID, "machine.create.queued", map[string]any{
		"machine_name":        record.Name,
		"runtime_name":        runtimeNameForRecord(record),
		"source_snapshot_id":  sourceSnapshotIDValue(record.SourceSnapshotID),
		"source_snapshot_set": record.SourceSnapshotID != nil,
		"stage":               "queued",
	})

	if len(existingRecords) > 0 {
		s.markUserTutorialCompletedBestEffort(user.ID)
	}

	s.queueMachineCreate(record)

	return s.machineFromRecord(ctx, record, machineruntime.Machine{}), nil
}

func (s *Service) DeleteMachine(ctx context.Context, name, ownerEmail string) error {
	ownerEmail = normalizeEmail(ownerEmail)
	if ownerEmail == "" {
		return fmt.Errorf("owner email is required")
	}
	unlock := s.lockUserMutations(ownerEmail)
	defer unlock()

	record, err := s.ownedMachineRecord(ctx, name, ownerEmail)
	if err != nil {
		return err
	}
	if strings.EqualFold(record.State, machineStateCreating) {
		return fmt.Errorf("machine %q is still creating", record.Name)
	}
	s.recordEventBestEffort(&record.OwnerUserID, &record.ID, "machine.delete.started", map[string]any{
		"machine_name": record.Name,
		"runtime_name": runtimeNameForRecord(record),
	})

	deleteCtx, cancel := s.newPersistContext()
	defer cancel()

	runtimeName := runtimeNameForRecord(record)
	if cancelCreate, ok := s.takeCreateCancel(runtimeName); ok {
		cancelCreate()
	}

	s.captureToolAuthBestEffort(deleteCtx, record)

	executor, err := s.executorForHostID(machineHostID(record, s.localHostID))
	if err != nil {
		return err
	}
	if err := executor.DeleteMachine(deleteCtx, runtimeName); err != nil && !errors.Is(err, machineruntime.ErrMachineNotFound) {
		s.recordEventBestEffort(&record.OwnerUserID, &record.ID, "machine.delete.failed", map[string]any{
			"machine_name": record.Name,
			"runtime_name": runtimeName,
			"error":        err.Error(),
		})
		return err
	}

	if err := s.store.MarkMachineDeleted(deleteCtx, record.ID); err != nil {
		return err
	}
	s.recordEventBestEffort(&record.OwnerUserID, &record.ID, "machine.delete.succeeded", map[string]any{
		"machine_name": record.Name,
		"runtime_name": runtimeName,
	})
	return nil
}

func (s *Service) ForkMachine(ctx context.Context, input ForkMachineInput) (Machine, error) {
	sourceName, err := validateMachineName(input.SourceName)
	if err != nil {
		return Machine{}, err
	}

	targetName, err := validateMachineName(input.TargetName)
	if err != nil {
		return Machine{}, err
	}
	if sourceName == targetName {
		return Machine{}, fmt.Errorf("source and target machine names must differ")
	}

	ownerEmail := normalizeEmail(input.OwnerEmail)
	if ownerEmail == "" {
		return Machine{}, fmt.Errorf("owner email is required")
	}
	unlock := s.lockUserMutations(ownerEmail)
	defer unlock()

	sourceRecord, err := s.store.GetMachineByName(ctx, sourceName)
	if err != nil {
		return Machine{}, err
	}
	if normalizeEmail(sourceRecord.OwnerEmail) != ownerEmail {
		return Machine{}, database.ErrNotFound
	}
	if err := ensureMachineRunningForAction(sourceRecord, "forked"); err != nil {
		return Machine{}, err
	}
	sourceHostID := machineHostID(sourceRecord, s.localHostID)
	executor, err := s.executorForHostID(sourceHostID)
	if err != nil {
		return Machine{}, err
	}
	liveSource, err := executor.GetMachine(ctx, runtimeNameForRecord(sourceRecord))
	if err != nil {
		return Machine{}, err
	}
	spec, err := s.machineSpecFromRuntime(sourceRecord, liveSource)
	if err != nil {
		return Machine{}, err
	}
	if err := s.validateMachineSizeLimit(spec.CPU, formatByteSize(spec.MemoryBytes), formatByteSize(spec.DiskBytes)); err != nil {
		return Machine{}, err
	}

	user, err := s.ensureUser(ctx, ownerEmail)
	if err != nil {
		return Machine{}, err
	}
	persistCtx, cancel := s.newPersistContext()
	defer cancel()

	targetID := uuid.NewString()
	record, err := func() (database.MachineRecord, error) {
		unlockHost := s.lockHostMutations(sourceHostID)
		defer unlockHost()

		hostRecord, err := s.hostByID(persistCtx, sourceHostID)
		if err != nil {
			return database.MachineRecord{}, err
		}
		if err := s.ensureUserCanCreateMachine(persistCtx, user, spec); err != nil {
			s.recordEventBestEffort(&user.ID, &sourceRecord.ID, "machine.fork.rejected", map[string]any{
				"source_name":  sourceName,
				"target_name":  targetName,
				"source_id":    sourceRecord.ID,
				"cpu":          spec.CPU,
				"memory_bytes": spec.MemoryBytes,
				"disk_bytes":   spec.DiskBytes,
				"error":        err.Error(),
			})
			return database.MachineRecord{}, err
		}
		if err := s.ensureHostCanFitMachine(persistCtx, hostRecord, spec); err != nil {
			s.recordEventBestEffort(&user.ID, &sourceRecord.ID, "machine.fork.rejected", map[string]any{
				"source_name":  sourceName,
				"target_name":  targetName,
				"source_id":    sourceRecord.ID,
				"host_id":      hostRecord.ID,
				"cpu":          spec.CPU,
				"memory_bytes": spec.MemoryBytes,
				"disk_bytes":   spec.DiskBytes,
				"error":        err.Error(),
			})
			return database.MachineRecord{}, err
		}

		return s.store.CreateMachine(persistCtx, database.CreateMachineParams{
			ID:          targetID,
			Name:        targetName,
			OwnerUserID: user.ID,
			HostID:      &sourceHostID,
			RuntimeName: targetName,
			State:       machineStateCreating,
			CPU:         spec.CPU,
			MemoryBytes: spec.MemoryBytes,
			DiskBytes:   spec.DiskBytes,
			PrimaryPort: sourceRecord.PrimaryPort,
		})
	}()
	if err != nil {
		return Machine{}, err
	}
	s.recordEventBestEffort(&user.ID, &sourceRecord.ID, "machine.fork.started", map[string]any{
		"stage":          "runtime_fork",
		"source_name":    sourceName,
		"source_id":      sourceRecord.ID,
		"target_name":    targetName,
		"target_id":      record.ID,
		"source_runtime": runtimeNameForRecord(sourceRecord),
	})

	liveMachine, err := executor.ForkMachine(ctx, machineruntime.ForkMachineRequest{
		MachineID:    targetID,
		SourceName:   runtimeNameForRecord(sourceRecord),
		TargetName:   targetName,
		RootDiskSize: formatByteSize(spec.DiskBytes),
	})
	if err != nil {
		s.finishForkFailure(record, sourceRecord, "runtime_fork", err)
		return Machine{}, err
	}
	record.RuntimeName = liveMachine.Name
	record.State = liveMachine.State
	finalizeCtx, finalizeCancel := s.newPersistContext()
	defer finalizeCancel()
	if err := s.store.UpdateMachineState(finalizeCtx, record.ID, liveMachine.State); err != nil {
		s.finishForkFailure(record, sourceRecord, "state_persist", err)
		return Machine{}, err
	}

	s.markUserTutorialCompletedBestEffort(user.ID)
	if err := s.syncManagedEnv(ctx, record); err != nil {
		s.finishForkFailure(record, sourceRecord, "env_sync", err)
		return Machine{}, err
	}
	s.recordEventBestEffort(&user.ID, &record.ID, "machine.fork.succeeded", map[string]any{
		"stage":        "complete",
		"source_name":  sourceName,
		"target_name":  targetName,
		"target_id":    record.ID,
		"runtime_name": liveMachine.Name,
	})

	return s.machineFromRecord(ctx, record, liveMachine), nil
}

func (s *Service) CompleteTutorial(ctx context.Context, ownerEmail string) error {
	user, err := s.store.GetUserByEmail(ctx, ownerEmail)
	if err != nil {
		return err
	}

	return s.store.MarkUserTutorialCompleted(ctx, user.ID)
}

func (s *Service) SyncToolAuth(ctx context.Context, name, ownerEmail string) error {
	record, err := s.ownedMachineRecord(ctx, name, ownerEmail)
	if err != nil {
		return err
	}
	return s.syncToolAuthForRecord(ctx, record, true)
}

func (s *Service) ListSnapshots(ctx context.Context, ownerEmail string) ([]Snapshot, error) {
	ownerEmail = normalizeEmail(ownerEmail)
	if ownerEmail == "" {
		return nil, fmt.Errorf("owner email is required")
	}

	records, err := s.store.ListSnapshots(ctx, ownerEmail)
	if err != nil {
		return nil, err
	}

	runtimeSnapshots := map[string]machineruntime.Snapshot{}
	seenHosts := map[string]struct{}{}
	for _, record := range records {
		hostID := snapshotHostID(record)
		if hostID == "" {
			hostID = s.localHostID
		}
		if _, ok := seenHosts[hostID]; ok {
			continue
		}
		seenHosts[hostID] = struct{}{}
		executor, err := s.executorForHostID(hostID)
		if err != nil {
			continue
		}
		live, err := executor.ListSnapshots(ctx)
		if err != nil {
			continue
		}
		for _, snapshot := range live {
			runtimeSnapshots[strings.TrimSpace(snapshot.Name)] = snapshot
		}
	}

	out := make([]Snapshot, 0, len(records))
	for _, record := range records {
		out = append(out, s.snapshotFromRecord(ctx, record, runtimeSnapshots[strings.TrimSpace(record.RuntimeName)]))
	}
	return out, nil
}

func (s *Service) CreateSnapshot(ctx context.Context, input CreateSnapshotInput) (Snapshot, error) {
	ownerEmail := normalizeEmail(input.OwnerEmail)
	if ownerEmail == "" {
		return Snapshot{}, fmt.Errorf("owner email is required")
	}
	snapshotName := strings.TrimSpace(input.SnapshotName)
	if snapshotName == "" {
		return Snapshot{}, fmt.Errorf("snapshot name is required")
	}
	unlock := s.lockUserMutations(ownerEmail)
	defer unlock()

	user, err := s.ensureUser(ctx, ownerEmail)
	if err != nil {
		return Snapshot{}, err
	}
	machineRecord, err := s.ownedMachineRecord(ctx, input.MachineName, ownerEmail)
	if err != nil {
		return Snapshot{}, err
	}
	if err := ensureMachineRunningForAction(machineRecord, "snapshotted"); err != nil {
		return Snapshot{}, err
	}
	sourceSpec, err := s.machineSpecForRecord(machineRecord)
	if err != nil {
		return Snapshot{}, err
	}
	reservedBytes := sourceSpec.DiskBytes + sourceSpec.MemoryBytes

	persistCtx, cancel := s.newPersistContext()
	defer cancel()

	runtimeName := uuid.NewString()
	hostID := machineHostID(machineRecord, s.localHostID)
	record, err := func() (database.SnapshotRecord, error) {
		unlockHost := s.lockHostMutations(hostID)
		defer unlockHost()

		hostRecord, err := s.hostByID(persistCtx, hostID)
		if err != nil {
			return database.SnapshotRecord{}, err
		}
		if err := s.ensureUserCanCreateSnapshot(persistCtx, user, reservedBytes); err != nil {
			s.recordEventBestEffort(&user.ID, &machineRecord.ID, "snapshot.create.rejected", map[string]any{
				"snapshot_name":  snapshotName,
				"source_machine": machineRecord.Name,
				"reserved_bytes": reservedBytes,
				"error":          err.Error(),
			})
			return database.SnapshotRecord{}, err
		}
		if err := s.ensureHostCanFitSnapshot(persistCtx, hostRecord, reservedBytes); err != nil {
			s.recordEventBestEffort(&user.ID, &machineRecord.ID, "snapshot.create.rejected", map[string]any{
				"snapshot_name":  snapshotName,
				"source_machine": machineRecord.Name,
				"host_id":        hostRecord.ID,
				"reserved_bytes": reservedBytes,
				"error":          err.Error(),
			})
			return database.SnapshotRecord{}, err
		}

		return s.store.CreateSnapshot(persistCtx, database.CreateSnapshotParams{
			ID:              uuid.NewString(),
			Name:            snapshotName,
			OwnerUserID:     user.ID,
			HostID:          &hostID,
			SourceMachineID: &machineRecord.ID,
			RuntimeName:     runtimeName,
			State:           snapshotStateCreating,
			CPU:             sourceSpec.CPU,
			MemoryBytes:     sourceSpec.MemoryBytes,
			DiskBytes:       sourceSpec.DiskBytes,
			ArtifactDir:     filepath.Join(s.cfg.RuntimeSnapshotDir, runtimeName),
			DiskSizeBytes:   sourceSpec.DiskBytes,
			MemorySizeBytes: sourceSpec.MemoryBytes,
		})
	}()
	if err != nil {
		return Snapshot{}, err
	}
	s.recordEventBestEffort(&user.ID, &machineRecord.ID, "snapshot.create.queued", map[string]any{
		"snapshot_id":    record.ID,
		"snapshot_name":  record.Name,
		"runtime_name":   record.RuntimeName,
		"source_machine": machineRecord.Name,
		"stage":          "queued",
	})

	go s.runSnapshotCreate(record)
	return s.snapshotFromRecord(ctx, record, machineruntime.Snapshot{}), nil
}

func (s *Service) DeleteSnapshot(ctx context.Context, name, ownerEmail string) error {
	ownerEmail = normalizeEmail(ownerEmail)
	if ownerEmail == "" {
		return fmt.Errorf("owner email is required")
	}
	unlock := s.lockUserMutations(ownerEmail)
	defer unlock()

	user, err := s.store.GetUserByEmail(ctx, ownerEmail)
	if err != nil {
		return err
	}
	record, err := s.store.GetSnapshotByName(ctx, user.ID, strings.TrimSpace(name))
	if err != nil {
		return err
	}
	s.recordEventBestEffort(&user.ID, record.SourceMachineID, "snapshot.delete.started", map[string]any{
		"snapshot_id":   record.ID,
		"snapshot_name": record.Name,
		"runtime_name":  record.RuntimeName,
	})

	deleteCtx, cancel := s.newPersistContext()
	defer cancel()
	executor, err := s.executorForHostID(snapshotHostID(record))
	if err != nil {
		return err
	}
	if err := executor.DeleteSnapshot(deleteCtx, record.RuntimeName); err != nil && !errors.Is(err, machineruntime.ErrSnapshotNotFound) {
		s.recordEventBestEffort(&user.ID, record.SourceMachineID, "snapshot.delete.failed", map[string]any{
			"snapshot_id":   record.ID,
			"snapshot_name": record.Name,
			"runtime_name":  record.RuntimeName,
			"error":         err.Error(),
		})
		return err
	}
	if err := s.store.MarkSnapshotDeleted(deleteCtx, record.ID); err != nil {
		return err
	}
	s.recordEventBestEffort(&user.ID, record.SourceMachineID, "snapshot.delete.succeeded", map[string]any{
		"snapshot_id":   record.ID,
		"snapshot_name": record.Name,
		"runtime_name":  record.RuntimeName,
	})
	return nil
}

func (s *Service) ownedMachineRecord(ctx context.Context, name, ownerEmail string) (database.MachineRecord, error) {
	ownerEmail = normalizeEmail(ownerEmail)
	if ownerEmail == "" {
		return database.MachineRecord{}, fmt.Errorf("owner email is required")
	}

	record, err := s.store.GetMachineByName(ctx, normalizeMachineName(name))
	if err != nil {
		return database.MachineRecord{}, err
	}
	if normalizeEmail(record.OwnerEmail) != ownerEmail {
		return database.MachineRecord{}, database.ErrNotFound
	}

	return record, nil
}

func (s *Service) enforceMachineCreatePolicy(ctx context.Context, ownerEmail, cpu, memory, disk string) error {
	if err := s.enforceMachineCountLimit(ctx, ownerEmail); err != nil {
		return err
	}
	return s.validateMachineSizeLimit(cpu, memory, disk)
}

func (s *Service) enforceMachineCountLimit(ctx context.Context, ownerEmail string) error {
	if s.cfg.MaxMachinesPerUser <= 0 {
		return nil
	}

	records, err := s.store.ListMachines(ctx, ownerEmail)
	if err != nil {
		return err
	}
	if len(records) >= s.cfg.MaxMachinesPerUser {
		return fmt.Errorf("machine quota exceeded: maximum %d machines per user", s.cfg.MaxMachinesPerUser)
	}

	return nil
}

func (s *Service) validateMachineSizeLimit(cpu, memory, disk string) error {
	maxCPU := strings.TrimSpace(s.cfg.MaxMachineCPU)
	if maxCPU != "" {
		requestedCPU, err := parseCPUCount(cpu)
		if err != nil {
			return fmt.Errorf("invalid machine CPU limit %q: %w", cpu, err)
		}
		allowedCPU, err := parseCPUCount(maxCPU)
		if err != nil {
			return fmt.Errorf("invalid configured max machine CPU %q: %w", maxCPU, err)
		}
		if requestedCPU > allowedCPU {
			return fmt.Errorf("machine size exceeds limit: cpu %s > %s", strings.TrimSpace(cpu), maxCPU)
		}
	}

	maxMemory := strings.TrimSpace(s.cfg.MaxMachineRAM)
	if maxMemory != "" {
		requestedMemory, err := parseByteSize(memory)
		if err != nil {
			return fmt.Errorf("invalid machine memory limit %q: %w", memory, err)
		}
		allowedMemory, err := parseByteSize(maxMemory)
		if err != nil {
			return fmt.Errorf("invalid configured max machine memory %q: %w", maxMemory, err)
		}
		if requestedMemory > allowedMemory {
			return fmt.Errorf("machine size exceeds limit: memory %s > %s", strings.TrimSpace(memory), maxMemory)
		}
	}

	maxDisk := strings.TrimSpace(s.cfg.MaxMachineDisk)
	if maxDisk != "" {
		requestedDisk, err := parseByteSize(disk)
		if err != nil {
			return fmt.Errorf("invalid machine disk limit %q: %w", disk, err)
		}
		allowedDisk, err := parseByteSize(maxDisk)
		if err != nil {
			return fmt.Errorf("invalid configured max machine disk %q: %w", maxDisk, err)
		}
		if requestedDisk > allowedDisk {
			return fmt.Errorf("machine size exceeds limit: disk %s > %s", strings.TrimSpace(disk), maxDisk)
		}
	}

	return nil
}

func (s *Service) machineFromRecord(ctx context.Context, record database.MachineRecord, live machineruntime.Machine) Machine {
	if live.Name != "" && live.State != "" && live.State != record.State && shouldAdoptRuntimeState(record.State, live.State) {
		_ = s.store.UpdateMachineState(ctx, record.ID, live.State)
		record.State = live.State
	}

	var runtimeMachine *machineruntime.Machine
	if live.Name != "" {
		copy := live
		runtimeMachine = &copy
	}

	return Machine{
		ID:          record.ID,
		Name:        record.Name,
		OwnerEmail:  record.OwnerEmail,
		HostID:      machineHostID(record, s.localHostID),
		State:       record.State,
		PrimaryPort: record.PrimaryPort,
		URL:         machineURL(record.Name, s.cfg.BaseDomain),
		CreatedAt:   record.CreatedAt,
		UpdatedAt:   record.UpdatedAt,
		Runtime:     runtimeMachine,
	}
}

func (s *Service) snapshotFromRecord(ctx context.Context, record database.SnapshotRecord, live machineruntime.Snapshot) Snapshot {
	if live.Name != "" && live.State != "" && !strings.EqualFold(record.State, live.State) {
		_ = s.store.UpdateSnapshotState(ctx, record.ID, live.State)
		record.State = live.State
	}

	var runtimeSnapshot *machineruntime.Snapshot
	if live.Name != "" {
		copy := live
		runtimeSnapshot = &copy
	}

	sourceMachineName := ""
	if record.SourceMachineName != nil {
		sourceMachineName = strings.TrimSpace(*record.SourceMachineName)
	}

	return Snapshot{
		ID:                record.ID,
		Name:              record.Name,
		OwnerEmail:        record.OwnerEmail,
		HostID:            coalesceHostID(snapshotHostID(record), s.localHostID),
		SourceMachineName: sourceMachineName,
		State:             record.State,
		ArtifactDir:       record.ArtifactDir,
		DiskSizeBytes:     record.DiskSizeBytes,
		MemorySizeBytes:   record.MemorySizeBytes,
		RuntimeVersion:    record.RuntimeVersion,
		FirmwareVersion:   record.FirmwareVersion,
		CreatedAt:         record.CreatedAt,
		UpdatedAt:         record.UpdatedAt,
		Runtime:           runtimeSnapshot,
	}
}

func shouldAdoptRuntimeState(recordState, runtimeState string) bool {
	recordState = strings.ToUpper(strings.TrimSpace(recordState))
	runtimeState = strings.ToUpper(strings.TrimSpace(runtimeState))

	if recordState == "" || runtimeState == "" {
		return false
	}
	if recordState == runtimeState {
		return false
	}
	if recordState == machineStateCreating {
		return false
	}

	return true
}

func ensureMachineRunningForAction(record database.MachineRecord, action string) error {
	if strings.EqualFold(record.State, machineStateRunning) {
		return nil
	}

	if strings.EqualFold(record.State, machineStateCreating) {
		return fmt.Errorf("machine %q is still creating", record.Name)
	}

	state := strings.ToLower(strings.TrimSpace(record.State))
	if state == "" {
		state = "unavailable"
	}
	return fmt.Errorf("machine %q is %s and cannot be %s", record.Name, state, action)
}

func (s *Service) ensureUser(ctx context.Context, email string) (database.User, error) {
	email = normalizeEmail(email)
	if email == "" {
		return database.User{}, fmt.Errorf("owner email is required")
	}

	user, err := s.store.UpsertUser(ctx, email, s.isAdminEmail(email))
	if err != nil {
		return database.User{}, err
	}
	defaults, err := s.defaultUserBudgetDefaults()
	if err != nil {
		return database.User{}, err
	}
	if err := s.store.ApplyUserBudgetDefaults(ctx, user.ID, defaults); err != nil {
		return database.User{}, err
	}
	return s.store.GetUserByEmail(ctx, email)
}

func (s *Service) ReconcileRuntimeState(ctx context.Context) error {
	records, err := s.store.ListMachines(ctx, "")
	if err != nil {
		return err
	}

	known := make(map[string]struct{}, len(records))
	recordByRuntime := make(map[string]database.MachineRecord, len(records))
	for _, record := range records {
		runtimeName := strings.TrimSpace(record.RuntimeName)
		if runtimeName == "" {
			runtimeName = strings.TrimSpace(record.Name)
		}
		if runtimeName == "" {
			continue
		}
		known[runtimeName] = struct{}{}
		recordByRuntime[runtimeName] = record
	}

	executor, err := s.executorForHostID(s.localHostID)
	if err != nil {
		return err
	}
	runtimeMachines, err := executor.ListMachines(ctx)
	if err != nil {
		return err
	}

	liveMachines := make(map[string]machineruntime.Machine, len(runtimeMachines))
	for _, machine := range runtimeMachines {
		name := strings.TrimSpace(machine.Name)
		if name == "" {
			continue
		}
		liveMachines[name] = machine
	}

	for _, machine := range runtimeMachines {
		name := strings.TrimSpace(machine.Name)
		if name == "" {
			continue
		}
		if _, ok := known[name]; ok {
			continue
		}
		if err := executor.DeleteMachine(ctx, name); err != nil && !errors.Is(err, machineruntime.ErrMachineNotFound) {
			return err
		}
	}

	var reconcileErrs []error
	for runtimeName, record := range recordByRuntime {
		liveMachine, ok := liveMachines[runtimeName]
		if !ok {
			if strings.EqualFold(record.State, machineStateCreating) && machineHostID(record, s.localHostID) == s.localHostID {
				s.queueMachineCreate(record)
			}
			continue
		}

		if strings.EqualFold(liveMachine.State, machineStateStopped) && !strings.EqualFold(record.State, machineStateCreating) {
			log.Printf("fascinate: recovering machine %s", runtimeName)
			s.recordEventBestEffort(&record.OwnerUserID, &record.ID, "machine.recover.started", map[string]any{
				"machine_name": record.Name,
				"runtime_name": runtimeName,
			})
			recoverCtx, cancel := context.WithTimeout(ctx, reconcileMachineRecoveryTimeout)
			recovered, err := executor.StartMachine(recoverCtx, runtimeName)
			cancel()
			if err != nil {
				log.Printf("fascinate: recover machine %s failed: %v", runtimeName, err)
				s.recordEventBestEffort(&record.OwnerUserID, &record.ID, "machine.recover.failed", map[string]any{
					"machine_name": record.Name,
					"runtime_name": runtimeName,
					"error":        err.Error(),
				})
				reconcileErrs = append(reconcileErrs, fmt.Errorf("%s: %w", runtimeName, err))
			} else {
				log.Printf("fascinate: recovered machine %s", runtimeName)
				liveMachine = recovered
				s.recordEventBestEffort(&record.OwnerUserID, &record.ID, "machine.recover.succeeded", map[string]any{
					"machine_name": record.Name,
					"runtime_name": runtimeName,
					"state":        recovered.State,
				})
			}
		}

		if !strings.EqualFold(record.State, liveMachine.State) {
			if err := s.store.UpdateMachineState(ctx, record.ID, liveMachine.State); err != nil && !errors.Is(err, database.ErrNotFound) {
				return err
			}
		}
	}

	return errors.Join(reconcileErrs...)
}

func (s *Service) SyncRunningToolAuth(ctx context.Context) error {
	if s.toolAuth == nil {
		return nil
	}

	records, err := s.store.ListMachines(ctx, "")
	if err != nil {
		return err
	}

	var syncErrs []error
	for _, record := range records {
		if !strings.EqualFold(record.State, machineStateRunning) {
			continue
		}
		if err := s.syncToolAuthForRecord(ctx, record, false); err != nil && !errors.Is(err, database.ErrNotFound) && !errors.Is(err, machineruntime.ErrMachineNotFound) {
			s.recordEventBestEffort(&record.OwnerUserID, &record.ID, "toolauth.capture.failed", map[string]any{
				"machine_name": record.Name,
				"runtime_name": runtimeNameForRecord(record),
				"checkpoint":   "background_sync",
				"error":        err.Error(),
			})
			syncErrs = append(syncErrs, fmt.Errorf("%s: %w", record.Name, err))
		}
	}

	return errors.Join(syncErrs...)
}

func (s *Service) isAdminEmail(email string) bool {
	for _, candidate := range s.cfg.AdminEmails {
		if normalizeEmail(candidate) == normalizeEmail(email) {
			return true
		}
	}

	return false
}

func (s *Service) cleanupRuntimeMachine(name, hostID string) {
	ctx, cancel := s.newPersistContext()
	defer cancel()
	executor, err := s.executorForHostID(hostID)
	if err != nil {
		return
	}
	_ = executor.DeleteMachine(ctx, name)
}

func (s *Service) queueMachineCreate(record database.MachineRecord) {
	runtimeName := strings.TrimSpace(record.RuntimeName)
	if runtimeName == "" {
		runtimeName = strings.TrimSpace(record.Name)
	}
	if runtimeName == "" {
		return
	}
	createCtx, ok := s.registerCreateCancel(runtimeName)
	if !ok {
		return
	}

	spec, err := s.machineSpecForRecord(record)
	if err != nil {
		s.finishCreateFailure(record, runtimeName, "resource_lookup", err)
		s.clearCreateCancel(runtimeName)
		return
	}

	req := machineruntime.CreateMachineRequest{
		MachineID:    record.ID,
		Name:         runtimeName,
		Image:        s.cfg.DefaultImage,
		CPU:          spec.CPU,
		Memory:       formatByteSize(spec.MemoryBytes),
		RootDiskSize: formatByteSize(spec.DiskBytes),
		PrimaryPort:  record.PrimaryPort,
	}
	if record.SourceSnapshotID != nil && strings.TrimSpace(*record.SourceSnapshotID) != "" {
		snapshotRecord, err := s.store.GetSnapshotByID(context.Background(), strings.TrimSpace(*record.SourceSnapshotID))
		if err != nil {
			s.finishCreateFailure(record, runtimeName, "snapshot_lookup", err)
			s.clearCreateCancel(runtimeName)
			return
		}
		req.Snapshot = snapshotRecord.RuntimeName
		req.Image = ""
	}

	go s.runMachineCreate(createCtx, record, req)
}

func (s *Service) runMachineCreate(ctx context.Context, record database.MachineRecord, req machineruntime.CreateMachineRequest) {
	defer func() {
		s.clearCreateCancel(req.Name)
	}()
	s.recordEventBestEffort(&record.OwnerUserID, &record.ID, "machine.create.started", map[string]any{
		"machine_name": record.Name,
		"runtime_name": req.Name,
		"stage":        "runtime_create",
	})

	executor, err := s.executorForHostID(machineHostID(record, s.localHostID))
	if err != nil {
		s.finishCreateFailure(record, req.Name, "host_lookup", err)
		return
	}
	liveMachine, err := executor.CreateMachine(ctx, req)
	if err != nil {
		s.finishCreateFailure(record, req.Name, "runtime_create", err)
		return
	}
	s.recordEventBestEffort(&record.OwnerUserID, &record.ID, "machine.create.runtime_ready", map[string]any{
		"machine_name": record.Name,
		"runtime_name": req.Name,
		"stage":        "runtime_ready",
	})

	if record.SourceSnapshotID == nil {
		if err := s.restoreToolAuth(ctx, record, liveMachine); err != nil {
			s.finishCreateFailure(record, req.Name, "tool_auth_restore", err)
			return
		}
	}
	if err := s.syncManagedEnv(ctx, record); err != nil {
		s.finishCreateFailure(record, req.Name, "managed_env_sync", err)
		return
	}

	s.finishCreateSuccess(record, liveMachine)
}

func (s *Service) runSnapshotCreate(record database.SnapshotRecord) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	s.recordEventBestEffort(&record.OwnerUserID, record.SourceMachineID, "snapshot.create.started", map[string]any{
		"snapshot_id":   record.ID,
		"snapshot_name": record.Name,
		"runtime_name":  record.RuntimeName,
		"stage":         "runtime_snapshot",
	})

	executor, err := s.executorForHostID(snapshotHostID(record))
	if err != nil {
		s.finishSnapshotFailure(record, "host_lookup", err)
		return
	}
	runtimeSnapshot, err := executor.CreateSnapshot(ctx, machineruntime.CreateSnapshotRequest{
		MachineName:  sourceMachineNameForSnapshot(record),
		SnapshotName: record.RuntimeName,
		ArtifactDir:  record.ArtifactDir,
	})
	if err != nil {
		s.finishSnapshotFailure(record, "runtime_snapshot", err)
		return
	}
	s.finishSnapshotSuccess(record, runtimeSnapshot)
}

func (s *Service) lockUserMutations(ownerEmail string) func() {
	key := normalizeEmail(ownerEmail)
	s.userLocksMu.Lock()
	lock, ok := s.userLocks[key]
	if !ok {
		lock = &sync.Mutex{}
		s.userLocks[key] = lock
	}
	s.userLocksMu.Unlock()

	lock.Lock()
	return lock.Unlock
}

func (s *Service) newPersistContext() (context.Context, context.CancelFunc) {
	timeout := s.persistTTL
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return context.WithTimeout(context.Background(), timeout)
}

func (s *Service) registerCreateCancel(runtimeName string) (context.Context, bool) {
	runtimeName = strings.TrimSpace(runtimeName)
	if runtimeName == "" {
		return nil, false
	}

	s.createMu.Lock()
	defer s.createMu.Unlock()
	if _, ok := s.createCancels[runtimeName]; ok {
		return nil, false
	}

	createCtx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	s.createCancels[runtimeName] = cancel
	return createCtx, true
}

func (s *Service) takeCreateCancel(runtimeName string) (context.CancelFunc, bool) {
	runtimeName = strings.TrimSpace(runtimeName)
	if runtimeName == "" {
		return nil, false
	}

	s.createMu.Lock()
	defer s.createMu.Unlock()
	cancel, ok := s.createCancels[runtimeName]
	if ok {
		delete(s.createCancels, runtimeName)
	}
	return cancel, ok
}

func (s *Service) clearCreateCancel(runtimeName string) {
	cancel, ok := s.takeCreateCancel(runtimeName)
	if ok {
		cancel()
	}
}

func (s *Service) finishCreateSuccess(record database.MachineRecord, liveMachine machineruntime.Machine) {
	persistCtx, cancel := s.newPersistContext()
	defer cancel()

	currentRecord, err := s.store.GetMachineByName(persistCtx, record.Name)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			s.cleanupRuntimeMachine(runtimeNameForRecord(record), machineHostID(record, s.localHostID))
			return
		}
		log.Printf("fascinate: finalize machine %s: %v", record.Name, err)
		s.cleanupRuntimeMachine(runtimeNameForRecord(record), machineHostID(record, s.localHostID))
		return
	}

	if err := s.store.UpdateMachineState(persistCtx, currentRecord.ID, liveMachine.State); err != nil {
		if errors.Is(err, database.ErrNotFound) {
			s.cleanupRuntimeMachine(runtimeNameForRecord(record), machineHostID(record, s.localHostID))
			return
		}
		log.Printf("fascinate: update machine %s state: %v", record.Name, err)
	}
	s.recordEventBestEffort(&record.OwnerUserID, &record.ID, "machine.create.succeeded", map[string]any{
		"machine_name": record.Name,
		"runtime_name": liveMachine.Name,
		"stage":        "complete",
		"state":        liveMachine.State,
	})
}

func (s *Service) finishCreateFailure(record database.MachineRecord, runtimeName, stage string, createErr error) {
	log.Printf("fascinate: machine create failed for %s: %v", record.Name, createErr)
	s.cleanupRuntimeMachine(runtimeName, machineHostID(record, s.localHostID))

	persistCtx, cancel := s.newPersistContext()
	defer cancel()

	currentRecord, err := s.store.GetMachineByName(persistCtx, record.Name)
	if err != nil {
		if !errors.Is(err, database.ErrNotFound) {
			log.Printf("fascinate: load failed machine %s: %v", record.Name, err)
		}
		return
	}

	if err := s.store.UpdateMachineState(persistCtx, currentRecord.ID, machineStateFailed); err != nil && !errors.Is(err, database.ErrNotFound) {
		log.Printf("fascinate: mark machine %s failed: %v", record.Name, err)
	}
	s.recordEventBestEffort(&record.OwnerUserID, &record.ID, "machine.create.failed", map[string]any{
		"machine_name": record.Name,
		"runtime_name": runtimeName,
		"stage":        strings.TrimSpace(stage),
		"error":        createErr.Error(),
	})
}

func (s *Service) finishForkFailure(record database.MachineRecord, sourceRecord database.MachineRecord, stage string, forkErr error) {
	log.Printf("fascinate: machine fork failed for %s -> %s: %v", sourceRecord.Name, record.Name, forkErr)
	s.cleanupRuntimeMachine(runtimeNameForRecord(record), machineHostID(record, s.localHostID))

	persistCtx, cancel := s.newPersistContext()
	defer cancel()

	currentRecord, err := s.store.GetMachineByName(persistCtx, record.Name)
	if err != nil {
		if !errors.Is(err, database.ErrNotFound) {
			log.Printf("fascinate: load failed fork target %s: %v", record.Name, err)
		}
	} else if err := s.store.UpdateMachineState(persistCtx, currentRecord.ID, machineStateFailed); err != nil && !errors.Is(err, database.ErrNotFound) {
		log.Printf("fascinate: mark fork target %s failed: %v", record.Name, err)
	}

	s.recordEventBestEffort(&record.OwnerUserID, &record.ID, "machine.fork.failed", map[string]any{
		"source_name":  sourceRecord.Name,
		"source_id":    sourceRecord.ID,
		"target_name":  record.Name,
		"target_id":    record.ID,
		"runtime_name": runtimeNameForRecord(record),
		"stage":        strings.TrimSpace(stage),
		"error":        forkErr.Error(),
	})
}

func (s *Service) finishSnapshotSuccess(record database.SnapshotRecord, runtimeSnapshot machineruntime.Snapshot) {
	persistCtx, cancel := s.newPersistContext()
	defer cancel()

	currentRecord, err := s.store.GetSnapshotByID(persistCtx, record.ID)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			executor, execErr := s.executorForHostID(snapshotHostID(record))
			if execErr == nil {
				_ = executor.DeleteSnapshot(persistCtx, record.RuntimeName)
			}
			return
		}
		log.Printf("fascinate: finalize snapshot %s: %v", record.Name, err)
		return
	}

	if err := s.store.UpdateSnapshotArtifacts(persistCtx, currentRecord.ID, runtimeSnapshot.DiskSizeBytes, runtimeSnapshot.MemorySizeBytes, runtimeSnapshot.RuntimeVersion, runtimeSnapshot.FirmwareVersion); err != nil && !errors.Is(err, database.ErrNotFound) {
		log.Printf("fascinate: update snapshot %s artifacts: %v", record.Name, err)
	}
	if err := s.store.UpdateSnapshotState(persistCtx, currentRecord.ID, snapshotStateReady); err != nil && !errors.Is(err, database.ErrNotFound) {
		log.Printf("fascinate: update snapshot %s state: %v", record.Name, err)
	}
	s.recordEventBestEffort(&record.OwnerUserID, record.SourceMachineID, "snapshot.create.succeeded", map[string]any{
		"snapshot_id":   record.ID,
		"snapshot_name": record.Name,
		"runtime_name":  record.RuntimeName,
		"stage":         "complete",
	})
}

func (s *Service) finishSnapshotFailure(record database.SnapshotRecord, stage string, snapshotErr error) {
	log.Printf("fascinate: snapshot create failed for %s: %v", record.Name, snapshotErr)

	persistCtx, cancel := s.newPersistContext()
	defer cancel()

	currentRecord, err := s.store.GetSnapshotByID(persistCtx, record.ID)
	if err != nil {
		if !errors.Is(err, database.ErrNotFound) {
			log.Printf("fascinate: load failed snapshot %s: %v", record.Name, err)
		}
		return
	}
	if err := s.store.UpdateSnapshotState(persistCtx, currentRecord.ID, snapshotStateFailed); err != nil && !errors.Is(err, database.ErrNotFound) {
		log.Printf("fascinate: mark snapshot %s failed: %v", record.Name, err)
	}
	s.recordEventBestEffort(&record.OwnerUserID, record.SourceMachineID, "snapshot.create.failed", map[string]any{
		"snapshot_id":   record.ID,
		"snapshot_name": record.Name,
		"runtime_name":  record.RuntimeName,
		"stage":         strings.TrimSpace(stage),
		"error":         snapshotErr.Error(),
	})
}

func (s *Service) markUserTutorialCompletedBestEffort(userID string) {
	persistCtx, cancel := s.newPersistContext()
	defer cancel()
	if err := s.store.MarkUserTutorialCompleted(persistCtx, userID); err != nil && !errors.Is(err, database.ErrNotFound) {
		log.Printf("fascinate: mark tutorial completed for %s: %v", userID, err)
	}
}

func (s *Service) restoreToolAuth(ctx context.Context, record database.MachineRecord, liveMachine machineruntime.Machine) error {
	if s.toolAuth == nil {
		return nil
	}

	runtimeName := strings.TrimSpace(liveMachine.Name)
	if runtimeName == "" {
		runtimeName = runtimeNameForRecord(record)
	}
	guestUser := strings.TrimSpace(liveMachine.GuestUser)
	if guestUser == "" {
		guestUser = strings.TrimSpace(s.cfg.GuestSSHUser)
	}
	if runtimeName == "" || guestUser == "" {
		return nil
	}
	err := s.toolAuth.RestoreAll(ctx, record.OwnerUserID, runtimeName, guestUser)
	if err != nil {
		s.recordEventBestEffort(&record.OwnerUserID, &record.ID, "toolauth.restore.failed", map[string]any{
			"machine_name": record.Name,
			"runtime_name": runtimeName,
			"checkpoint":   "machine_create",
			"error":        err.Error(),
		})
	}
	return err
}

func (s *Service) syncToolAuthForRecord(ctx context.Context, record database.MachineRecord, exact bool) error {
	if s.toolAuth == nil {
		return nil
	}

	runtimeName := runtimeNameForRecord(record)
	if runtimeName == "" {
		return nil
	}

	executor, err := s.executorForHostID(machineHostID(record, s.localHostID))
	if err != nil {
		return err
	}
	liveMachine, err := executor.GetMachine(ctx, runtimeName)
	if err != nil {
		return err
	}
	if !strings.EqualFold(liveMachine.State, machineStateRunning) {
		return nil
	}

	guestUser := strings.TrimSpace(liveMachine.GuestUser)
	if guestUser == "" {
		guestUser = strings.TrimSpace(s.cfg.GuestSSHUser)
	}
	if guestUser == "" {
		return nil
	}

	if exact {
		return s.toolAuth.CaptureAll(ctx, record.OwnerUserID, runtimeName, guestUser)
	}
	return s.toolAuth.CaptureAllNonDestructive(ctx, record.OwnerUserID, runtimeName, guestUser)
}

func (s *Service) syncToolAuthFromOwnerRunningMachines(ctx context.Context, ownerUserID, excludeRuntimeName string) {
	if s.toolAuth == nil {
		return
	}

	records, err := s.store.ListMachines(ctx, "")
	if err != nil {
		log.Printf("fascinate: list machines for tool auth sync: %v", err)
		return
	}

	excludeRuntimeName = strings.TrimSpace(excludeRuntimeName)
	for _, candidate := range records {
		if candidate.OwnerUserID != ownerUserID {
			continue
		}
		if !strings.EqualFold(candidate.State, machineStateRunning) {
			continue
		}

		runtimeName := runtimeNameForRecord(candidate)
		if runtimeName == "" || runtimeName == excludeRuntimeName {
			continue
		}

		if err := s.syncToolAuthForRecord(ctx, candidate, false); err != nil &&
			!errors.Is(err, database.ErrNotFound) &&
			!errors.Is(err, machineruntime.ErrMachineNotFound) {
			s.recordEventBestEffort(&candidate.OwnerUserID, &candidate.ID, "toolauth.capture.failed", map[string]any{
				"machine_name": candidate.Name,
				"runtime_name": runtimeNameForRecord(candidate),
				"checkpoint":   "pre_create_sync",
				"error":        err.Error(),
			})
			log.Printf("fascinate: pre-create tool auth sync from %s: %v", candidate.Name, err)
		}
	}
}

func (s *Service) captureToolAuthBestEffort(ctx context.Context, record database.MachineRecord) {
	if err := s.syncToolAuthForRecord(ctx, record, true); err != nil && !errors.Is(err, database.ErrNotFound) && !errors.Is(err, machineruntime.ErrMachineNotFound) {
		log.Printf("fascinate: capture tool auth for %s: %v", record.Name, err)
		s.recordEventBestEffort(&record.OwnerUserID, &record.ID, "toolauth.capture.failed", map[string]any{
			"machine_name": record.Name,
			"runtime_name": runtimeNameForRecord(record),
			"checkpoint":   "machine_delete",
			"error":        err.Error(),
		})
	}
}

func sourceSnapshotIDValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func machineStateAllowsMissingRuntime(state string) bool {
	switch strings.ToUpper(strings.TrimSpace(state)) {
	case machineStateCreating, machineStateFailed:
		return true
	default:
		return false
	}
}

func runtimeNameForRecord(record database.MachineRecord) string {
	runtimeName := strings.TrimSpace(record.RuntimeName)
	if runtimeName != "" {
		return runtimeName
	}
	return strings.TrimSpace(record.Name)
}

func sourceMachineNameForSnapshot(record database.SnapshotRecord) string {
	if record.SourceMachineName != nil && strings.TrimSpace(*record.SourceMachineName) != "" {
		return strings.TrimSpace(*record.SourceMachineName)
	}
	return ""
}

func validateMachineName(value string) (string, error) {
	name := normalizeMachineName(value)
	if !machineNamePattern.MatchString(name) {
		return "", fmt.Errorf("machine name must match %s", machineNamePattern.String())
	}

	return name, nil
}

func normalizeMachineName(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeEmail(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func parseCPUCount(value string) (float64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("cpu value is required")
	}

	count, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("cpu value must be numeric")
	}
	if count <= 0 {
		return 0, fmt.Errorf("cpu value must be positive")
	}

	return count, nil
}

func parseByteSize(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("size value is required")
	}

	matches := memoryLimitPattern.FindStringSubmatch(value)
	if matches == nil {
		return 0, fmt.Errorf("unsupported size value")
	}

	number, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return 0, fmt.Errorf("size value must be numeric")
	}
	if number <= 0 {
		return 0, fmt.Errorf("size value must be positive")
	}

	unit := strings.ToLower(matches[2])
	switch unit {
	case "", "b":
		return int64(number), nil
	case "k", "kb":
		return int64(number * 1000), nil
	case "ki", "kib":
		return int64(number * 1024), nil
	case "m", "mb":
		return int64(number * 1000 * 1000), nil
	case "mi", "mib":
		return int64(number * 1024 * 1024), nil
	case "g", "gb":
		return int64(number * 1000 * 1000 * 1000), nil
	case "gi", "gib":
		return int64(number * 1024 * 1024 * 1024), nil
	case "t", "tb":
		return int64(number * 1000 * 1000 * 1000 * 1000), nil
	case "ti", "tib":
		return int64(number * 1024 * 1024 * 1024 * 1024), nil
	default:
		return 0, fmt.Errorf("unsupported memory unit")
	}
}

func machineURL(name, baseDomain string) string {
	baseDomain = strings.TrimSpace(baseDomain)
	if baseDomain == "" {
		return ""
	}

	return fmt.Sprintf("https://%s.%s", name, baseDomain)
}
