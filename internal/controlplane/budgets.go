package controlplane

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"fascinate/internal/database"
	machineruntime "fascinate/internal/runtime"
)

type resourceSpec struct {
	CPU         string
	MemoryBytes int64
	DiskBytes   int64
}

type userBudget struct {
	MaxCPU           float64
	MaxCPUText       string
	MaxMemoryBytes   int64
	MaxDiskBytes     int64
	MaxMachineCount  int
	MaxSnapshotCount int
}

type userUsage struct {
	CPU                       float64
	MemoryBytes               int64
	DiskBytes                 int64
	RetainedMachineDiskBytes  int64
	RetainedSnapshotDiskBytes int64
	MachineCount              int
	SnapshotCount             int
	CreatingMachineCount      int
	StartingMachineCount      int
	RunningMachineCount       int
	StoppingMachineCount      int
	StoppedMachineCount       int
	DeletingMachineCount      int
}

type hostUsage struct {
	CPU                       float64
	MemoryBytes               int64
	DiskBytes                 int64
	RetainedMachineDiskBytes  int64
	RetainedSnapshotDiskBytes int64
	MachineCount              int
	SnapshotCount             int
	CreatingMachineCount      int
	StartingMachineCount      int
	RunningMachineCount       int
	StoppingMachineCount      int
	StoppedMachineCount       int
	DeletingMachineCount      int
	AvailableDiskBytes        int64
}

type budgetDelta struct {
	CPU           float64
	CPUText       string
	MemoryBytes   int64
	DiskBytes     int64
	MachineCount  int
	SnapshotCount int
}

func (s *Service) lockHostMutations(hostID string) func() {
	key := strings.TrimSpace(hostID)
	if key == "" {
		key = s.localHostID
	}

	s.hostLocksMu.Lock()
	lock, ok := s.hostLocks[key]
	if !ok {
		lock = &sync.Mutex{}
		s.hostLocks[key] = lock
	}
	s.hostLocksMu.Unlock()

	lock.Lock()
	return lock.Unlock
}

func (s *Service) defaultMachineSpec() (resourceSpec, error) {
	memoryBytes, err := parseByteSize(s.cfg.DefaultMachineRAM)
	if err != nil {
		return resourceSpec{}, fmt.Errorf("invalid default machine memory %q: %w", s.cfg.DefaultMachineRAM, err)
	}
	diskBytes, err := parseByteSize(s.cfg.DefaultMachineDisk)
	if err != nil {
		return resourceSpec{}, fmt.Errorf("invalid default machine disk %q: %w", s.cfg.DefaultMachineDisk, err)
	}
	return resourceSpec{
		CPU:         strings.TrimSpace(s.cfg.DefaultMachineCPU),
		MemoryBytes: memoryBytes,
		DiskBytes:   diskBytes,
	}, nil
}

func (s *Service) defaultUserBudgetDefaults() (database.UserBudgetDefaults, error) {
	maxMemoryBytes, err := parseByteSize(s.cfg.DefaultUserMaxRAM)
	if err != nil {
		return database.UserBudgetDefaults{}, fmt.Errorf("invalid default user max memory %q: %w", s.cfg.DefaultUserMaxRAM, err)
	}
	maxDiskBytes, err := parseByteSize(s.cfg.DefaultUserMaxDisk)
	if err != nil {
		return database.UserBudgetDefaults{}, fmt.Errorf("invalid default user max disk %q: %w", s.cfg.DefaultUserMaxDisk, err)
	}
	return database.UserBudgetDefaults{
		MaxCPU:           strings.TrimSpace(s.cfg.DefaultUserMaxCPU),
		MaxMemoryBytes:   maxMemoryBytes,
		MaxDiskBytes:     maxDiskBytes,
		MaxMachineCount:  s.cfg.EffectiveDefaultUserMaxMachines(),
		MaxSnapshotCount: s.cfg.DefaultUserMaxSnapshots,
	}, nil
}

func (s *Service) machineSpecForRecord(record database.MachineRecord) (resourceSpec, error) {
	defaults, err := s.defaultMachineSpec()
	if err != nil {
		return resourceSpec{}, err
	}
	if strings.TrimSpace(record.CPU) != "" {
		defaults.CPU = strings.TrimSpace(record.CPU)
	}
	if record.MemoryBytes > 0 {
		defaults.MemoryBytes = record.MemoryBytes
	}
	if record.DiskBytes > 0 {
		defaults.DiskBytes = record.DiskBytes
	}
	return defaults, nil
}

func (s *Service) machineSpecFromRuntime(record database.MachineRecord, live machineruntime.Machine) (resourceSpec, error) {
	spec, err := s.machineSpecForRecord(record)
	if err != nil {
		return resourceSpec{}, err
	}
	if strings.TrimSpace(live.CPU) != "" {
		spec.CPU = strings.TrimSpace(live.CPU)
	}
	if strings.TrimSpace(live.Memory) != "" {
		memoryBytes, err := parseByteSize(live.Memory)
		if err != nil {
			return resourceSpec{}, fmt.Errorf("invalid runtime memory %q: %w", live.Memory, err)
		}
		spec.MemoryBytes = memoryBytes
	}
	if strings.TrimSpace(live.Disk) != "" {
		diskBytes, err := parseByteSize(live.Disk)
		if err != nil {
			return resourceSpec{}, fmt.Errorf("invalid runtime disk %q: %w", live.Disk, err)
		}
		spec.DiskBytes = diskBytes
	}
	return spec, nil
}

func (s *Service) snapshotSpecForRecord(record database.SnapshotRecord) (resourceSpec, error) {
	defaults, err := s.defaultMachineSpec()
	if err != nil {
		return resourceSpec{}, err
	}
	if strings.TrimSpace(record.CPU) != "" {
		defaults.CPU = strings.TrimSpace(record.CPU)
	}
	if record.MemoryBytes > 0 {
		defaults.MemoryBytes = record.MemoryBytes
	}
	if record.DiskBytes > 0 {
		defaults.DiskBytes = record.DiskBytes
	}
	return defaults, nil
}

func (s *Service) userBudgetForUser(user database.User) (userBudget, error) {
	maxCPUText := strings.TrimSpace(user.MaxCPU)
	if maxCPUText == "" {
		maxCPUText = strings.TrimSpace(s.cfg.DefaultUserMaxCPU)
	}
	maxCPU, err := parseCPUCount(maxCPUText)
	if err != nil {
		return userBudget{}, fmt.Errorf("invalid user cpu budget %q: %w", maxCPUText, err)
	}

	maxMemoryBytes := user.MaxMemoryBytes
	if maxMemoryBytes <= 0 {
		maxMemoryBytes, err = parseByteSize(s.cfg.DefaultUserMaxRAM)
		if err != nil {
			return userBudget{}, fmt.Errorf("invalid default user max memory %q: %w", s.cfg.DefaultUserMaxRAM, err)
		}
	}

	maxDiskBytes := user.MaxDiskBytes
	if maxDiskBytes <= 0 {
		maxDiskBytes, err = parseByteSize(s.cfg.DefaultUserMaxDisk)
		if err != nil {
			return userBudget{}, fmt.Errorf("invalid default user max disk %q: %w", s.cfg.DefaultUserMaxDisk, err)
		}
	}

	maxMachineCount := user.MaxMachineCount
	if maxMachineCount <= 0 {
		maxMachineCount = s.cfg.EffectiveDefaultUserMaxMachines()
	}

	maxSnapshotCount := user.MaxSnapshotCount
	if maxSnapshotCount <= 0 {
		maxSnapshotCount = s.cfg.DefaultUserMaxSnapshots
	}

	return userBudget{
		MaxCPU:           maxCPU,
		MaxCPUText:       maxCPUText,
		MaxMemoryBytes:   maxMemoryBytes,
		MaxDiskBytes:     maxDiskBytes,
		MaxMachineCount:  maxMachineCount,
		MaxSnapshotCount: maxSnapshotCount,
	}, nil
}

func (s *Service) calculateUserUsage(ctx context.Context, userID string) (userUsage, error) {
	var usage userUsage

	machines, err := s.store.ListMachines(ctx, "")
	if err != nil {
		return usage, err
	}
	for _, record := range machines {
		if strings.TrimSpace(record.OwnerUserID) != strings.TrimSpace(userID) {
			continue
		}
		spec, err := s.machineSpecForRecord(record)
		if err != nil {
			return usage, err
		}
		if machineCountsTowardQuota(record.State) {
			usage.MachineCount++
			accumulateMachinePowerState(&usage.CreatingMachineCount, &usage.StartingMachineCount, &usage.RunningMachineCount, &usage.StoppingMachineCount, &usage.StoppedMachineCount, &usage.DeletingMachineCount, record.State)
		}
		if machineConsumesCompute(record.State) {
			cpu, err := parseCPUCount(spec.CPU)
			if err != nil {
				return usage, err
			}
			usage.CPU += cpu
			usage.MemoryBytes += spec.MemoryBytes
		}
		if machineConsumesDisk(record.State) {
			retainedBytes := retainedMachineDiskBytes(record)
			usage.RetainedMachineDiskBytes += retainedBytes
			usage.DiskBytes += retainedBytes
		}
	}

	snapshots, err := s.store.ListSnapshots(ctx, "")
	if err != nil {
		return usage, err
	}
	for _, record := range snapshots {
		if strings.TrimSpace(record.OwnerUserID) != strings.TrimSpace(userID) {
			continue
		}
		if snapshotCountsTowardQuota(record.State) {
			usage.SnapshotCount++
		}
		if snapshotConsumesDisk(record.State) {
			retainedBytes := snapshotReservedDiskBytes(record)
			usage.RetainedSnapshotDiskBytes += retainedBytes
			usage.DiskBytes += retainedBytes
		}
	}

	return usage, nil
}

func (s *Service) calculateHostUsage(ctx context.Context, hostID string) (hostUsage, error) {
	hostID = strings.TrimSpace(hostID)
	if hostID == "" {
		hostID = s.localHostID
	}

	var usage hostUsage

	machines, err := s.store.ListMachines(ctx, "")
	if err != nil {
		return usage, err
	}
	for _, record := range machines {
		if machineHostID(record, s.localHostID) != hostID {
			continue
		}
		spec, err := s.machineSpecForRecord(record)
		if err != nil {
			return usage, err
		}
		if machineCountsTowardQuota(record.State) {
			usage.MachineCount++
			accumulateMachinePowerState(&usage.CreatingMachineCount, &usage.StartingMachineCount, &usage.RunningMachineCount, &usage.StoppingMachineCount, &usage.StoppedMachineCount, &usage.DeletingMachineCount, record.State)
		}
		if machineConsumesCompute(record.State) {
			cpu, err := parseCPUCount(spec.CPU)
			if err != nil {
				return usage, err
			}
			usage.CPU += cpu
			usage.MemoryBytes += spec.MemoryBytes
		}
		if machineConsumesDisk(record.State) {
			retainedBytes := retainedMachineDiskBytes(record)
			usage.RetainedMachineDiskBytes += retainedBytes
			usage.DiskBytes += retainedBytes
		}
	}

	snapshots, err := s.store.ListSnapshots(ctx, "")
	if err != nil {
		return usage, err
	}
	for _, record := range snapshots {
		if snapshotHostID(record) != hostID {
			continue
		}
		if snapshotCountsTowardQuota(record.State) {
			usage.SnapshotCount++
		}
		if snapshotConsumesDisk(record.State) {
			retainedBytes := snapshotReservedDiskBytes(record)
			usage.RetainedSnapshotDiskBytes += retainedBytes
			usage.DiskBytes += retainedBytes
		}
	}

	availableDiskBytes, err := s.hostAvailableDiskBytes(hostID)
	if err == nil {
		usage.AvailableDiskBytes = availableDiskBytes
	}

	return usage, nil
}

func (s *Service) hostAvailableDiskBytes(hostID string) (int64, error) {
	if strings.TrimSpace(hostID) == strings.TrimSpace(s.localHostID) {
		_, available, err := diskStats(s.cfg.DataDir)
		if err == nil {
			return available, nil
		}
	}

	record, err := s.store.GetHostByID(context.Background(), hostID)
	if err != nil {
		return 0, err
	}
	return record.AvailableDiskBytes, nil
}

func (s *Service) hostMinFreeDiskBytes() (int64, error) {
	value := strings.TrimSpace(s.cfg.HostMinFreeDisk)
	if value == "" {
		return 0, nil
	}
	switch strings.ToLower(value) {
	case "0", "0b":
		return 0, nil
	}
	return parseByteSize(value)
}

func (s *Service) ensureUserCanCreateMachine(ctx context.Context, user database.User, spec resourceSpec, retainedDiskBytes int64) error {
	delta, err := computeBudgetDelta(spec, retainedDiskBytes)
	if err != nil {
		return err
	}
	delta.MachineCount = 1
	return s.ensureUserCanFitDelta(ctx, user, delta)
}

func (s *Service) ensureUserCanStartMachine(ctx context.Context, user database.User, spec resourceSpec) error {
	delta, err := computeBudgetDelta(spec, 0)
	if err != nil {
		return err
	}
	return s.ensureUserCanFitDelta(ctx, user, delta)
}

func (s *Service) ensureUserCanFitDelta(ctx context.Context, user database.User, delta budgetDelta) error {
	budget, err := s.userBudgetForUser(user)
	if err != nil {
		return err
	}
	usage, err := s.calculateUserUsage(ctx, user.ID)
	if err != nil {
		return err
	}
	if budget.MaxMachineCount > 0 && usage.MachineCount+delta.MachineCount > budget.MaxMachineCount {
		return fmt.Errorf("machine quota exceeded: maximum %d machines per user", budget.MaxMachineCount)
	}
	if budget.MaxCPU > 0 && usage.CPU+delta.CPU > budget.MaxCPU {
		return fmt.Errorf("shared cpu budget exceeded: active %s + requested %s > limit %s", formatCPUCount(usage.CPU), delta.CPUText, budget.MaxCPUText)
	}
	if budget.MaxMemoryBytes > 0 && usage.MemoryBytes+delta.MemoryBytes > budget.MaxMemoryBytes {
		return fmt.Errorf("shared memory budget exceeded: active %s + requested %s > limit %s", formatByteSize(usage.MemoryBytes), formatByteSize(delta.MemoryBytes), formatByteSize(budget.MaxMemoryBytes))
	}
	if budget.MaxDiskBytes > 0 && usage.DiskBytes+delta.DiskBytes > budget.MaxDiskBytes {
		return fmt.Errorf("retained storage budget exceeded: retained %s + requested %s > limit %s", formatByteSize(usage.DiskBytes), formatByteSize(delta.DiskBytes), formatByteSize(budget.MaxDiskBytes))
	}
	return nil
}

func (s *Service) ensureUserCanCreateSnapshot(ctx context.Context, user database.User, reservedBytes int64) error {
	return s.ensureUserCanFitSnapshotDelta(ctx, user, budgetDelta{
		DiskBytes:     reservedBytes,
		SnapshotCount: 1,
	})
}

func (s *Service) ensureUserCanFitSnapshotDelta(ctx context.Context, user database.User, delta budgetDelta) error {
	budget, err := s.userBudgetForUser(user)
	if err != nil {
		return err
	}
	usage, err := s.calculateUserUsage(ctx, user.ID)
	if err != nil {
		return err
	}
	if budget.MaxSnapshotCount > 0 && usage.SnapshotCount+delta.SnapshotCount > budget.MaxSnapshotCount {
		return fmt.Errorf("snapshot quota exceeded: maximum %d retained snapshots per user", budget.MaxSnapshotCount)
	}
	if budget.MaxDiskBytes > 0 && usage.DiskBytes+delta.DiskBytes > budget.MaxDiskBytes {
		return fmt.Errorf("retained storage budget exceeded: retained %s + requested %s > limit %s", formatByteSize(usage.DiskBytes), formatByteSize(delta.DiskBytes), formatByteSize(budget.MaxDiskBytes))
	}
	return nil
}

func (s *Service) ensureHostCanFitMachine(ctx context.Context, host database.HostRecord, spec resourceSpec, retainedDiskBytes int64) error {
	delta, err := computeBudgetDelta(spec, retainedDiskBytes)
	if err != nil {
		return err
	}
	return s.ensureHostCanFitDelta(ctx, host, delta)
}

func (s *Service) ensureHostCanStartMachine(ctx context.Context, host database.HostRecord, spec resourceSpec) error {
	delta, err := computeBudgetDelta(spec, 0)
	if err != nil {
		return err
	}
	return s.ensureHostCanFitDelta(ctx, host, delta)
}

func (s *Service) ensureHostCanFitDelta(ctx context.Context, host database.HostRecord, delta budgetDelta) error {
	if !strings.EqualFold(strings.TrimSpace(host.Status), hostStatusActive) {
		return fmt.Errorf("host %s is not active", host.ID)
	}
	if !s.hostHeartbeatFresh(host) {
		return fmt.Errorf("host %s heartbeat is stale", host.ID)
	}

	usage, err := s.calculateHostUsage(ctx, host.ID)
	if err != nil {
		return err
	}
	if host.TotalCPU > 0 && usage.CPU+delta.CPU > float64(host.TotalCPU) {
		return fmt.Errorf("host %s lacks cpu capacity", host.ID)
	}
	if host.TotalMemoryBytes > 0 && usage.MemoryBytes+delta.MemoryBytes > host.TotalMemoryBytes {
		return fmt.Errorf("host %s lacks memory capacity", host.ID)
	}
	if host.TotalDiskBytes > 0 && usage.DiskBytes+delta.DiskBytes > host.TotalDiskBytes {
		return fmt.Errorf("host %s lacks disk capacity", host.ID)
	}

	minFreeDiskBytes, err := s.hostMinFreeDiskBytes()
	if err != nil {
		return fmt.Errorf("invalid host minimum free disk %q: %w", s.cfg.HostMinFreeDisk, err)
	}
	if usage.AvailableDiskBytes > 0 && usage.AvailableDiskBytes-delta.DiskBytes < minFreeDiskBytes {
		return fmt.Errorf("host %s lacks free disk headroom", host.ID)
	}
	return nil
}

func (s *Service) ensureHostCanFitSnapshot(ctx context.Context, host database.HostRecord, reservedBytes int64) error {
	return s.ensureHostCanFitDelta(ctx, host, budgetDelta{DiskBytes: reservedBytes})
}

func machineCountsTowardQuota(state string) bool {
	return !strings.EqualFold(strings.TrimSpace(state), machineStateFailed)
}

func machineConsumesCompute(state string) bool {
	switch strings.ToUpper(strings.TrimSpace(state)) {
	case machineStateCreating, machineStateStarting, machineStateRunning, machineStateStopping:
		return true
	default:
		return false
	}
}

func machineConsumesDisk(state string) bool {
	return !strings.EqualFold(strings.TrimSpace(state), machineStateFailed)
}

func snapshotCountsTowardQuota(state string) bool {
	return !strings.EqualFold(strings.TrimSpace(state), snapshotStateFailed)
}

func snapshotConsumesDisk(state string) bool {
	return !strings.EqualFold(strings.TrimSpace(state), snapshotStateFailed)
}

func snapshotReservedDiskBytes(record database.SnapshotRecord) int64 {
	if record.DiskSizeBytes > 0 || record.MemorySizeBytes > 0 {
		return record.DiskSizeBytes + record.MemorySizeBytes
	}
	return record.DiskBytes + record.MemoryBytes
}

func computeBudgetDelta(spec resourceSpec, retainedDiskBytes int64) (budgetDelta, error) {
	requestedCPU, err := parseCPUCount(spec.CPU)
	if err != nil {
		return budgetDelta{}, err
	}
	if retainedDiskBytes < 0 {
		retainedDiskBytes = 0
	}
	return budgetDelta{
		CPU:         requestedCPU,
		CPUText:     strings.TrimSpace(spec.CPU),
		MemoryBytes: spec.MemoryBytes,
		DiskBytes:   retainedDiskBytes,
	}, nil
}

func accumulateMachinePowerState(creating, starting, running, stopping, stopped, deleting *int, state string) {
	switch strings.ToUpper(strings.TrimSpace(state)) {
	case machineStateCreating:
		*creating = *creating + 1
	case machineStateStarting:
		*starting = *starting + 1
	case machineStateRunning:
		*running = *running + 1
	case machineStateStopping:
		*stopping = *stopping + 1
	case machineStateStopped:
		*stopped = *stopped + 1
	case machineStateDeleting:
		*deleting = *deleting + 1
	}
}

func formatByteSize(value int64) string {
	switch {
	case value <= 0:
		return "0B"
	case value%(1<<40) == 0:
		return fmt.Sprintf("%dTiB", value/(1<<40))
	case value%(1<<30) == 0:
		return fmt.Sprintf("%dGiB", value/(1<<30))
	case value%(1<<20) == 0:
		return fmt.Sprintf("%dMiB", value/(1<<20))
	case value%(1<<10) == 0:
		return fmt.Sprintf("%dKiB", value/(1<<10))
	default:
		return fmt.Sprintf("%dB", value)
	}
}

func formatCPUCount(value float64) string {
	if value == float64(int64(value)) {
		return fmt.Sprintf("%d", int64(value))
	}
	return fmt.Sprintf("%.2f", value)
}
