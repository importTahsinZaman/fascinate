package controlplane

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	goruntime "runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"fascinate/internal/database"
	machineruntime "fascinate/internal/runtime"
)

const (
	hostStatusActive    = "ACTIVE"
	hostStatusDraining  = "DRAINING"
	hostStatusUnhealthy = "UNHEALTHY"
)

type hostExecutor interface {
	HealthCheck(context.Context) error
	ListMachines(context.Context) ([]machineruntime.Machine, error)
	GetMachine(context.Context, string) (machineruntime.Machine, error)
	CreateMachine(context.Context, machineruntime.CreateMachineRequest) (machineruntime.Machine, error)
	SyncManagedEnv(context.Context, string, machineruntime.ManagedEnvRequest) error
	StartMachine(context.Context, string) (machineruntime.Machine, error)
	StopMachine(context.Context, string) (machineruntime.Machine, error)
	DeleteMachine(context.Context, string) error
	ForkMachine(context.Context, machineruntime.ForkMachineRequest) (machineruntime.Machine, error)
	ListSnapshots(context.Context) ([]machineruntime.Snapshot, error)
	GetSnapshot(context.Context, string) (machineruntime.Snapshot, error)
	CreateSnapshot(context.Context, machineruntime.CreateSnapshotRequest) (machineruntime.Snapshot, error)
	DeleteSnapshot(context.Context, string) error
	InspectMachine(context.Context, string) (machineruntime.MachineDiagnostics, error)
	InspectSnapshot(context.Context, string) (machineruntime.SnapshotDiagnostics, error)
}

type localHostExecutor struct {
	runtime machineruntime.Manager
	diag    runtimeDiagnosticsProvider
}

func newLocalHostExecutor(runtime machineruntime.Manager) hostExecutor {
	executor := &localHostExecutor{runtime: runtime}
	if provider, ok := runtime.(runtimeDiagnosticsProvider); ok {
		executor.diag = provider
	}
	return executor
}

func (l *localHostExecutor) HealthCheck(ctx context.Context) error {
	return l.runtime.HealthCheck(ctx)
}

func (l *localHostExecutor) ListMachines(ctx context.Context) ([]machineruntime.Machine, error) {
	return l.runtime.ListMachines(ctx)
}

func (l *localHostExecutor) GetMachine(ctx context.Context, name string) (machineruntime.Machine, error) {
	return l.runtime.GetMachine(ctx, name)
}

func (l *localHostExecutor) CreateMachine(ctx context.Context, req machineruntime.CreateMachineRequest) (machineruntime.Machine, error) {
	return l.runtime.CreateMachine(ctx, req)
}

func (l *localHostExecutor) SyncManagedEnv(ctx context.Context, name string, req machineruntime.ManagedEnvRequest) error {
	return l.runtime.SyncManagedEnv(ctx, name, req)
}

func (l *localHostExecutor) StartMachine(ctx context.Context, name string) (machineruntime.Machine, error) {
	return l.runtime.StartMachine(ctx, name)
}

func (l *localHostExecutor) StopMachine(ctx context.Context, name string) (machineruntime.Machine, error) {
	return l.runtime.StopMachine(ctx, name)
}

func (l *localHostExecutor) DeleteMachine(ctx context.Context, name string) error {
	return l.runtime.DeleteMachine(ctx, name)
}

func (l *localHostExecutor) ForkMachine(ctx context.Context, req machineruntime.ForkMachineRequest) (machineruntime.Machine, error) {
	return l.runtime.ForkMachine(ctx, req)
}

func (l *localHostExecutor) ListSnapshots(ctx context.Context) ([]machineruntime.Snapshot, error) {
	return l.runtime.ListSnapshots(ctx)
}

func (l *localHostExecutor) GetSnapshot(ctx context.Context, name string) (machineruntime.Snapshot, error) {
	return l.runtime.GetSnapshot(ctx, name)
}

func (l *localHostExecutor) CreateSnapshot(ctx context.Context, req machineruntime.CreateSnapshotRequest) (machineruntime.Snapshot, error) {
	return l.runtime.CreateSnapshot(ctx, req)
}

func (l *localHostExecutor) DeleteSnapshot(ctx context.Context, name string) error {
	return l.runtime.DeleteSnapshot(ctx, name)
}

func (l *localHostExecutor) InspectMachine(ctx context.Context, name string) (machineruntime.MachineDiagnostics, error) {
	if l.diag == nil {
		return machineruntime.MachineDiagnostics{}, machineruntime.ErrMachineNotFound
	}
	return l.diag.InspectMachine(ctx, name)
}

func (l *localHostExecutor) InspectSnapshot(ctx context.Context, name string) (machineruntime.SnapshotDiagnostics, error) {
	if l.diag == nil {
		return machineruntime.SnapshotDiagnostics{}, machineruntime.ErrSnapshotNotFound
	}
	return l.diag.InspectSnapshot(ctx, name)
}

type Host struct {
	ID                   string            `json:"id"`
	Name                 string            `json:"name"`
	Region               string            `json:"region"`
	Role                 string            `json:"role"`
	Status               string            `json:"status"`
	Labels               map[string]string `json:"labels,omitempty"`
	Capabilities         []string          `json:"capabilities,omitempty"`
	RuntimeVersion       string            `json:"runtime_version,omitempty"`
	HeartbeatAt          *string           `json:"heartbeat_at,omitempty"`
	HeartbeatFresh       bool              `json:"heartbeat_fresh"`
	PlacementEligible    bool              `json:"placement_eligible"`
	TotalCPU             int               `json:"total_cpu"`
	AllocatedCPU         int               `json:"allocated_cpu"`
	SharedCPUCeiling     string            `json:"shared_cpu_ceiling"`
	NominalActiveCPU     string            `json:"nominal_active_cpu"`
	SharedCPURemaining   string            `json:"shared_cpu_remaining"`
	SharedCPUOvercommit  string            `json:"shared_cpu_overcommit_ratio"`
	TotalMemoryBytes     int64             `json:"total_memory_bytes"`
	AllocatedMemoryBytes int64             `json:"allocated_memory_bytes"`
	TotalDiskBytes       int64             `json:"total_disk_bytes"`
	AllocatedDiskBytes   int64             `json:"allocated_disk_bytes"`
	AvailableDiskBytes   int64             `json:"available_disk_bytes"`
	MachineCount         int               `json:"machine_count"`
	SnapshotCount        int               `json:"snapshot_count"`
	LastError            *string           `json:"last_error,omitempty"`
	CreatedAt            string            `json:"created_at"`
	UpdatedAt            string            `json:"updated_at"`
}

type hostMetrics struct {
	runtimeVersion       string
	totalCPU             int
	allocatedCPU         int
	totalMemoryBytes     int64
	allocatedMemoryBytes int64
	totalDiskBytes       int64
	allocatedDiskBytes   int64
	availableDiskBytes   int64
	machineCount         int
	snapshotCount        int
}

func (s *Service) EnsureLocalHost(ctx context.Context) (Host, error) {
	record, err := s.store.UpsertHost(ctx, database.UpsertHostParams{
		ID:               s.localHostID,
		Name:             s.cfg.HostName,
		Region:           s.cfg.HostRegion,
		Role:             s.cfg.HostRole,
		Status:           hostStatusActive,
		LabelsJSON:       "{}",
		CapabilitiesJSON: `["vm","snapshot","fork","shell","route","combined"]`,
		RuntimeVersion:   strings.TrimSpace(s.cfg.RuntimeBinary),
	})
	if err != nil {
		return Host{}, err
	}
	if err := s.store.AssignHostToMachinesWithoutHost(ctx, s.localHostID); err != nil {
		return Host{}, err
	}
	if err := s.store.AssignHostToSnapshotsWithoutHost(ctx, s.localHostID); err != nil {
		return Host{}, err
	}
	record, err = s.store.GetHostByID(ctx, s.localHostID)
	if err != nil {
		return Host{}, err
	}
	return s.hostFromRecord(record), nil
}

func (s *Service) HeartbeatLocalHost(ctx context.Context) error {
	metrics, healthy, lastErr := s.collectLocalHostMetrics(ctx)
	if err := s.store.UpdateHostHeartbeat(ctx, database.UpdateHostHeartbeatParams{
		ID:                   s.localHostID,
		RuntimeVersion:       metrics.runtimeVersion,
		Healthy:              healthy,
		TotalCPU:             metrics.totalCPU,
		AllocatedCPU:         metrics.allocatedCPU,
		TotalMemoryBytes:     metrics.totalMemoryBytes,
		AllocatedMemoryBytes: metrics.allocatedMemoryBytes,
		TotalDiskBytes:       metrics.totalDiskBytes,
		AllocatedDiskBytes:   metrics.allocatedDiskBytes,
		AvailableDiskBytes:   metrics.availableDiskBytes,
		MachineCount:         metrics.machineCount,
		SnapshotCount:        metrics.snapshotCount,
		LastError:            lastErr,
	}); err != nil {
		return err
	}
	if !healthy && lastErr != nil {
		return fmt.Errorf("local host unhealthy: %s", strings.TrimSpace(*lastErr))
	}
	return nil
}

func (s *Service) ListHosts(ctx context.Context) ([]Host, error) {
	records, err := s.store.ListHosts(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Host, 0, len(records))
	for _, record := range records {
		out = append(out, s.hostFromRecord(record))
	}
	return out, nil
}

func (s *Service) collectLocalHostMetrics(ctx context.Context) (hostMetrics, bool, *string) {
	metrics := hostMetrics{
		runtimeVersion: strings.TrimSpace(s.cfg.RuntimeBinary),
		totalCPU:       goruntime.NumCPU(),
	}

	if version := s.detectLocalRuntimeVersion(ctx); version != "" {
		metrics.runtimeVersion = version
	}

	if totalMemory, err := totalSystemMemoryBytes(); err == nil {
		metrics.totalMemoryBytes = totalMemory
	}
	if totalDisk, availableDisk, err := diskStats(s.cfg.DataDir); err == nil {
		metrics.totalDiskBytes = totalDisk
		metrics.availableDiskBytes = availableDisk
	}

	executor, err := s.executorForHostID(s.localHostID)
	if err != nil {
		message := err.Error()
		return metrics, false, &message
	}
	if err := executor.HealthCheck(ctx); err != nil {
		message := err.Error()
		return metrics, false, &message
	}
	if err := s.refreshLocalMachineDiskUsage(ctx); err != nil {
		message := err.Error()
		return metrics, false, &message
	}

	usage, err := s.calculateHostUsage(ctx, s.localHostID)
	if err != nil {
		message := err.Error()
		return metrics, false, &message
	}
	metrics.allocatedCPU = roundActiveCPU(usage.CPU)
	metrics.allocatedMemoryBytes = usage.MemoryBytes
	metrics.allocatedDiskBytes = usage.DiskBytes
	metrics.machineCount = usage.MachineCount
	metrics.snapshotCount = usage.SnapshotCount

	return metrics, true, nil
}

func (s *Service) detectLocalRuntimeVersion(ctx context.Context) string {
	binary := strings.TrimSpace(s.cfg.RuntimeBinary)
	if binary == "" {
		return ""
	}
	output, err := exec.CommandContext(ctx, binary, "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.SplitN(string(output), "\n", 2)[0])
}

func (s *Service) hostFromRecord(record database.HostRecord) Host {
	sharedCPUCeiling, err := s.hostSharedCPUCeiling(record.TotalCPU)
	if err != nil {
		sharedCPUCeiling = 0
	}
	sharedCPURemaining := maxFloat64(sharedCPUCeiling - float64(record.AllocatedCPU))
	return Host{
		ID:                   record.ID,
		Name:                 record.Name,
		Region:               record.Region,
		Role:                 record.Role,
		Status:               record.Status,
		Labels:               parseLabels(record.LabelsJSON),
		Capabilities:         parseCapabilities(record.CapabilitiesJSON),
		RuntimeVersion:       record.RuntimeVersion,
		HeartbeatAt:          record.HeartbeatAt,
		HeartbeatFresh:       s.hostHeartbeatFresh(record),
		PlacementEligible:    s.hostPlacementEligible(record, s.cfg.DefaultMachineCPU, s.cfg.DefaultMachineRAM, s.cfg.DefaultMachineDisk),
		TotalCPU:             record.TotalCPU,
		AllocatedCPU:         record.AllocatedCPU,
		SharedCPUCeiling:     formatCPUCount(sharedCPUCeiling),
		NominalActiveCPU:     formatCPUCount(float64(record.AllocatedCPU)),
		SharedCPURemaining:   formatCPUCount(sharedCPURemaining),
		SharedCPUOvercommit:  strings.TrimSpace(s.cfg.HostSharedCPURatio),
		TotalMemoryBytes:     record.TotalMemoryBytes,
		AllocatedMemoryBytes: record.AllocatedMemoryBytes,
		TotalDiskBytes:       record.TotalDiskBytes,
		AllocatedDiskBytes:   record.AllocatedDiskBytes,
		AvailableDiskBytes:   record.AvailableDiskBytes,
		MachineCount:         record.MachineCount,
		SnapshotCount:        record.SnapshotCount,
		LastError:            record.LastError,
		CreatedAt:            record.CreatedAt,
		UpdatedAt:            record.UpdatedAt,
	}
}

func (s *Service) hostHeartbeatFresh(record database.HostRecord) bool {
	if record.HeartbeatAt == nil || strings.TrimSpace(*record.HeartbeatAt) == "" {
		return false
	}
	heartbeatAt, err := time.Parse(time.RFC3339, strings.TrimSpace(*record.HeartbeatAt))
	if err != nil {
		heartbeatAt, err = time.Parse("2006-01-02 15:04:05", strings.TrimSpace(*record.HeartbeatAt))
		if err != nil {
			return false
		}
	}
	grace := s.cfg.HostHeartbeatInterval * 3
	if grace <= 0 {
		grace = 90 * time.Second
	}
	return time.Since(heartbeatAt) <= grace
}

func (s *Service) hostPlacementEligible(record database.HostRecord, cpu, memory, disk string) bool {
	return s.hostPlacementErr(record, cpu, memory, disk) == nil
}

func (s *Service) hostHasCapacity(record database.HostRecord, cpu, memory, disk string) bool {
	return s.hostCapacityErr(record, cpu, memory, disk) == nil
}

func (s *Service) hostPlacementErr(record database.HostRecord, cpu, memory, disk string) error {
	if !strings.EqualFold(strings.TrimSpace(record.Status), hostStatusActive) {
		return fmt.Errorf("host %s is not active", record.ID)
	}
	if !s.hostHeartbeatFresh(record) {
		return fmt.Errorf("host %s heartbeat is stale", record.ID)
	}
	return s.hostCapacityErr(record, cpu, memory, disk)
}

func (s *Service) hostCapacityErr(record database.HostRecord, cpu, memory, disk string) error {
	requestedCPU, err := parseCPUCount(cpu)
	if err != nil || requestedCPU <= 0 {
		return fmt.Errorf("invalid requested cpu %q", cpu)
	}
	sharedCPUCeiling, err := s.hostSharedCPUCeiling(record.TotalCPU)
	if err != nil {
		return err
	}
	if sharedCPUCeiling > 0 && float64(record.AllocatedCPU)+requestedCPU > sharedCPUCeiling {
		return fmt.Errorf("shared host cpu capacity exhausted on %s: active %s + requested %s > ceiling %s", record.ID, formatCPUCount(float64(record.AllocatedCPU)), formatCPUCount(requestedCPU), formatCPUCount(sharedCPUCeiling))
	}

	requestedMemoryBytes, err := parseByteSize(memory)
	if err != nil || requestedMemoryBytes <= 0 {
		return fmt.Errorf("invalid requested memory %q", memory)
	}
	if record.TotalMemoryBytes > 0 && record.AllocatedMemoryBytes+requestedMemoryBytes > record.TotalMemoryBytes {
		return fmt.Errorf("host %s lacks memory capacity", record.ID)
	}

	requestedDiskBytes, err := parseByteSize(disk)
	if err != nil || requestedDiskBytes <= 0 {
		return fmt.Errorf("invalid requested disk %q", disk)
	}
	if record.TotalDiskBytes > 0 && record.AllocatedDiskBytes+requestedDiskBytes > record.TotalDiskBytes {
		return fmt.Errorf("host %s lacks disk capacity", record.ID)
	}
	minFreeDiskBytes, err := s.hostMinFreeDiskBytes()
	if err != nil {
		return err
	}
	if record.AvailableDiskBytes > 0 && record.AvailableDiskBytes-requestedDiskBytes < minFreeDiskBytes {
		return fmt.Errorf("host %s lacks free disk headroom", record.ID)
	}

	return nil
}

func parseLabels(value string) map[string]string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parsed := map[string]string{}
	if err := json.Unmarshal([]byte(value), &parsed); err != nil {
		return nil
	}
	return parsed
}

func parseCapabilities(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	var parsed []string
	if err := json.Unmarshal([]byte(value), &parsed); err != nil {
		return nil
	}
	return parsed
}

func totalSystemMemoryBytes() (int64, error) {
	body, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			break
		}
		value, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return 0, err
		}
		return value * 1024, nil
	}
	return 0, fmt.Errorf("MemTotal not found in /proc/meminfo")
}

func diskStats(path string) (int64, int64, error) {
	var fsStat syscall.Statfs_t
	if err := syscall.Statfs(path, &fsStat); err != nil {
		return 0, 0, err
	}
	total := int64(fsStat.Blocks) * int64(fsStat.Bsize)
	available := int64(fsStat.Bavail) * int64(fsStat.Bsize)
	return total, available, nil
}

func machineHostID(record database.MachineRecord, fallback string) string {
	if record.HostID != nil && strings.TrimSpace(*record.HostID) != "" {
		return strings.TrimSpace(*record.HostID)
	}
	return strings.TrimSpace(fallback)
}

func snapshotHostID(record database.SnapshotRecord) string {
	if record.HostID != nil && strings.TrimSpace(*record.HostID) != "" {
		return strings.TrimSpace(*record.HostID)
	}
	return ""
}

func coalesceHostID(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value != "" {
		return value
	}
	return strings.TrimSpace(fallback)
}

func (s *Service) executorForHostID(hostID string) (hostExecutor, error) {
	hostID = strings.TrimSpace(hostID)
	if hostID == "" {
		hostID = s.localHostID
	}
	executor, ok := s.executors[hostID]
	if !ok {
		return nil, fmt.Errorf("host %s is not available", hostID)
	}
	return executor, nil
}

func (s *Service) getPlacementHost(ctx context.Context, cpu, memory, disk string) (database.HostRecord, error) {
	hosts, err := s.store.ListHosts(ctx)
	if err != nil {
		return database.HostRecord{}, err
	}
	var placementErr error
	for _, host := range hosts {
		if err := s.hostPlacementErr(host, cpu, memory, disk); err == nil {
			return host, nil
		} else if placementErr == nil {
			placementErr = err
		}
	}
	if placementErr != nil {
		return database.HostRecord{}, placementErr
	}
	return database.HostRecord{}, fmt.Errorf("no eligible hosts available for placement")
}

func (s *Service) hostByID(ctx context.Context, id string) (database.HostRecord, error) {
	if strings.TrimSpace(id) == "" {
		return database.HostRecord{}, fmt.Errorf("host id is required")
	}
	return s.store.GetHostByID(ctx, id)
}

func (s *Service) parseHostOrZero(value string) int {
	out, _ := strconv.Atoi(strings.TrimSpace(value))
	return out
}
