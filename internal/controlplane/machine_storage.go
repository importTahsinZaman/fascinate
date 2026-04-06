package controlplane

import (
	"context"
	"errors"
	"os"
	"strings"
	"syscall"

	"fascinate/internal/database"
	machineruntime "fascinate/internal/runtime"
)

const freshMachineRetainedDiskEstimateBytes = 1 << 30

func retainedMachineDiskBytes(record database.MachineRecord) int64 {
	if record.DiskUsageBytes > 0 {
		return record.DiskUsageBytes
	}
	return 0
}

func estimatedFreshMachineRetainedDiskBytes(spec resourceSpec) int64 {
	switch {
	case spec.DiskBytes <= 0:
		return freshMachineRetainedDiskEstimateBytes
	case spec.DiskBytes < freshMachineRetainedDiskEstimateBytes:
		return spec.DiskBytes
	default:
		return freshMachineRetainedDiskEstimateBytes
	}
}

func estimatedSnapshotRestoreDiskBytes(record database.SnapshotRecord, spec resourceSpec) int64 {
	if record.DiskSizeBytes > 0 {
		return record.DiskSizeBytes
	}
	if spec.DiskBytes > 0 {
		return spec.DiskBytes
	}
	return estimatedFreshMachineRetainedDiskBytes(spec)
}

func estimatedForkDiskBytes(record database.MachineRecord, spec resourceSpec) int64 {
	if retainedBytes := retainedMachineDiskBytes(record); retainedBytes > 0 {
		return retainedBytes
	}
	if spec.DiskBytes > 0 {
		return spec.DiskBytes
	}
	return estimatedFreshMachineRetainedDiskBytes(spec)
}

func estimatedSnapshotArtifactBytes(record database.MachineRecord, spec resourceSpec) int64 {
	memoryBytes := spec.MemoryBytes
	if memoryBytes < 0 {
		memoryBytes = 0
	}
	if retainedBytes := retainedMachineDiskBytes(record); retainedBytes > 0 {
		return retainedBytes + memoryBytes
	}
	return spec.DiskBytes + memoryBytes
}

func (s *Service) refreshMachineDiskUsage(ctx context.Context, record database.MachineRecord) (database.MachineRecord, error) {
	usageBytes, err := s.machineDiskUsageBytes(ctx, record)
	if err != nil {
		return record, err
	}
	if usageBytes == record.DiskUsageBytes {
		return record, nil
	}
	if err := s.store.UpdateMachineDiskUsage(ctx, record.ID, usageBytes); err != nil {
		return record, err
	}
	record.DiskUsageBytes = usageBytes
	return record, nil
}

func (s *Service) refreshLocalMachineDiskUsage(ctx context.Context) error {
	records, err := s.store.ListMachines(ctx, "")
	if err != nil {
		return err
	}

	var syncErrs []error
	for _, record := range records {
		if machineHostID(record, s.localHostID) != s.localHostID {
			continue
		}
		if strings.EqualFold(record.State, machineStateFailed) || strings.EqualFold(record.State, machineStateDeleting) {
			continue
		}
		if _, err := s.refreshMachineDiskUsage(ctx, record); err != nil &&
			!errors.Is(err, machineruntime.ErrMachineNotFound) &&
			!errors.Is(err, database.ErrNotFound) {
			syncErrs = append(syncErrs, err)
		}
	}

	return errors.Join(syncErrs...)
}

func (s *Service) machineDiskUsageBytes(ctx context.Context, record database.MachineRecord) (int64, error) {
	executor, err := s.executorForHostID(machineHostID(record, s.localHostID))
	if err != nil {
		return 0, err
	}
	diag, err := executor.InspectMachine(ctx, runtimeNameForRecord(record))
	if err != nil {
		return 0, err
	}
	return allocatedFileSize(diag.DiskPath)
}

func allocatedFileSize(path string) (int64, error) {
	info, err := os.Stat(strings.TrimSpace(path))
	if err != nil {
		return 0, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if ok && stat.Blocks > 0 {
		return int64(stat.Blocks) * 512, nil
	}
	return info.Size(), nil
}
