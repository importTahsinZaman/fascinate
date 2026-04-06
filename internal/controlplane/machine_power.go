package controlplane

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"fascinate/internal/database"
	machineruntime "fascinate/internal/runtime"
)

func (s *Service) StartMachine(ctx context.Context, name, ownerEmail string) (Machine, error) {
	ownerEmail = normalizeEmail(ownerEmail)
	if ownerEmail == "" {
		return Machine{}, fmt.Errorf("owner email is required")
	}
	unlock := s.lockUserMutations(ownerEmail)
	defer unlock()

	record, err := s.ownedMachineRecord(ctx, name, ownerEmail)
	if err != nil {
		return Machine{}, err
	}
	if err := ensureMachineStoppedForStart(record); err != nil {
		return Machine{}, err
	}

	spec, err := s.machineSpecForRecord(record)
	if err != nil {
		return Machine{}, err
	}
	user, err := s.ensureUser(ctx, ownerEmail)
	if err != nil {
		return Machine{}, err
	}

	hostID := machineHostID(record, s.localHostID)
	persistCtx, cancel := s.newPersistContext()
	defer cancel()

	unlockHost := s.lockHostMutations(hostID)
	hostRecord, err := s.hostByID(persistCtx, hostID)
	if err != nil {
		unlockHost()
		return Machine{}, err
	}
	if err := s.ensureUserCanStartMachine(persistCtx, user, spec); err != nil {
		unlockHost()
		s.recordEventBestEffort(&user.ID, &record.ID, "machine.start.rejected", map[string]any{
			"machine_name": record.Name,
			"cpu":          spec.CPU,
			"memory_bytes": spec.MemoryBytes,
			"error":        err.Error(),
		})
		return Machine{}, err
	}
	if err := s.ensureHostCanStartMachine(persistCtx, hostRecord, spec); err != nil {
		unlockHost()
		s.recordEventBestEffort(&user.ID, &record.ID, "machine.start.rejected", map[string]any{
			"machine_name": record.Name,
			"host_id":      hostRecord.ID,
			"cpu":          spec.CPU,
			"memory_bytes": spec.MemoryBytes,
			"error":        err.Error(),
		})
		return Machine{}, err
	}
	if err := s.store.UpdateMachineState(persistCtx, record.ID, machineStateStarting); err != nil {
		unlockHost()
		return Machine{}, err
	}
	unlockHost()
	record.State = machineStateStarting
	s.recordEventBestEffort(&user.ID, &record.ID, "machine.start.started", map[string]any{
		"machine_name": record.Name,
		"runtime_name": runtimeNameForRecord(record),
		"stage":        "runtime_start",
	})

	executor, err := s.executorForHostID(hostID)
	if err != nil {
		_ = s.store.UpdateMachineState(context.Background(), record.ID, machineStateStopped)
		return Machine{}, err
	}
	liveMachine, err := executor.StartMachine(ctx, runtimeNameForRecord(record))
	if err != nil {
		s.finishStartFailure(record, "runtime_start", err)
		return Machine{}, err
	}
	if err := s.syncManagedEnv(ctx, record); err != nil {
		stopCtx, stopCancel := s.newPersistContext()
		_, _ = executor.StopMachine(stopCtx, runtimeNameForRecord(record))
		stopCancel()
		s.finishStartFailure(record, "managed_env_sync", err)
		return Machine{}, err
	}
	if err := s.finishStartSuccess(record, liveMachine); err != nil {
		s.finishStartFailure(record, "persist_running_state", err)
		return Machine{}, err
	}

	refreshed, err := s.store.GetMachineByName(ctx, record.Name)
	if err != nil {
		return Machine{}, err
	}
	return s.machineFromRecord(ctx, refreshed, liveMachine), nil
}

func (s *Service) StopMachine(ctx context.Context, name, ownerEmail string) (Machine, error) {
	ownerEmail = normalizeEmail(ownerEmail)
	if ownerEmail == "" {
		return Machine{}, fmt.Errorf("owner email is required")
	}
	unlock := s.lockUserMutations(ownerEmail)
	defer unlock()

	record, err := s.ownedMachineRecord(ctx, name, ownerEmail)
	if err != nil {
		return Machine{}, err
	}
	if err := ensureMachineRunningForStop(record); err != nil {
		return Machine{}, err
	}

	persistCtx, cancel := s.newPersistContext()
	defer cancel()
	if err := s.store.UpdateMachineState(persistCtx, record.ID, machineStateStopping); err != nil {
		return Machine{}, err
	}
	record.State = machineStateStopping
	s.recordEventBestEffort(&record.OwnerUserID, &record.ID, "machine.stop.started", map[string]any{
		"machine_name": record.Name,
		"runtime_name": runtimeNameForRecord(record),
		"stage":        "runtime_stop",
	})

	executor, err := s.executorForHostID(machineHostID(record, s.localHostID))
	if err != nil {
		_ = s.store.UpdateMachineState(context.Background(), record.ID, machineStateRunning)
		return Machine{}, err
	}
	liveMachine, err := executor.StopMachine(ctx, runtimeNameForRecord(record))
	if err != nil {
		s.finishStopFailure(record, "runtime_stop", err)
		return Machine{}, err
	}
	if err := s.finishStopSuccess(record, liveMachine); err != nil {
		s.finishStopFailure(record, "persist_stopped_state", err)
		return Machine{}, err
	}

	refreshed, err := s.store.GetMachineByName(ctx, record.Name)
	if err != nil {
		return Machine{}, err
	}
	return s.machineFromRecord(ctx, refreshed, liveMachine), nil
}

func ensureMachineStoppedForStart(record database.MachineRecord) error {
	switch strings.ToUpper(strings.TrimSpace(record.State)) {
	case machineStateStopped:
		return nil
	case machineStateStarting:
		return fmt.Errorf("machine %q is already starting", record.Name)
	case machineStateCreating:
		return fmt.Errorf("machine %q is still creating", record.Name)
	case machineStateStopping:
		return fmt.Errorf("machine %q is still stopping", record.Name)
	case machineStateDeleting:
		return fmt.Errorf("machine %q is being deleted", record.Name)
	case machineStateRunning:
		return fmt.Errorf("machine %q is already running", record.Name)
	default:
		state := strings.ToLower(strings.TrimSpace(record.State))
		if state == "" {
			state = "unavailable"
		}
		return fmt.Errorf("machine %q is %s and cannot be started", record.Name, state)
	}
}

func ensureMachineRunningForStop(record database.MachineRecord) error {
	switch strings.ToUpper(strings.TrimSpace(record.State)) {
	case machineStateRunning:
		return nil
	case machineStateStopping:
		return fmt.Errorf("machine %q is already stopping", record.Name)
	case machineStateCreating:
		return fmt.Errorf("machine %q is still creating", record.Name)
	case machineStateStarting:
		return fmt.Errorf("machine %q is still starting", record.Name)
	case machineStateStopped:
		return fmt.Errorf("machine %q is already stopped", record.Name)
	case machineStateDeleting:
		return fmt.Errorf("machine %q is being deleted", record.Name)
	default:
		state := strings.ToLower(strings.TrimSpace(record.State))
		if state == "" {
			state = "unavailable"
		}
		return fmt.Errorf("machine %q is %s and cannot be stopped", record.Name, state)
	}
}

func (s *Service) finishStartSuccess(record database.MachineRecord, liveMachine machineruntime.Machine) error {
	persistCtx, cancel := s.newPersistContext()
	defer cancel()

	currentRecord := record
	if refreshed, refreshErr := s.refreshMachineDiskUsage(persistCtx, currentRecord); refreshErr == nil {
		currentRecord = refreshed
	}
	if err := s.store.UpdateMachineStateAndDiskUsage(persistCtx, currentRecord.ID, machineStateRunning, retainedMachineDiskBytes(currentRecord)); err != nil && !errors.Is(err, database.ErrNotFound) {
		return err
	}
	s.recordEventBestEffort(&record.OwnerUserID, &record.ID, "machine.start.succeeded", map[string]any{
		"machine_name": record.Name,
		"runtime_name": liveMachine.Name,
		"stage":        "complete",
		"state":        machineStateRunning,
	})
	return nil
}

func (s *Service) finishStartFailure(record database.MachineRecord, stage string, startErr error) {
	persistCtx, cancel := s.newPersistContext()
	defer cancel()
	if err := s.store.UpdateMachineState(persistCtx, record.ID, machineStateStopped); err != nil && !errors.Is(err, database.ErrNotFound) {
		return
	}
	s.recordEventBestEffort(&record.OwnerUserID, &record.ID, "machine.start.failed", map[string]any{
		"machine_name": record.Name,
		"runtime_name": runtimeNameForRecord(record),
		"stage":        strings.TrimSpace(stage),
		"error":        startErr.Error(),
	})
}

func (s *Service) finishStopSuccess(record database.MachineRecord, liveMachine machineruntime.Machine) error {
	persistCtx, cancel := s.newPersistContext()
	defer cancel()

	currentRecord := record
	if refreshed, refreshErr := s.refreshMachineDiskUsage(persistCtx, currentRecord); refreshErr == nil {
		currentRecord = refreshed
	}
	if err := s.store.UpdateMachineStateAndDiskUsage(persistCtx, currentRecord.ID, machineStateStopped, retainedMachineDiskBytes(currentRecord)); err != nil && !errors.Is(err, database.ErrNotFound) {
		return err
	}
	s.recordEventBestEffort(&record.OwnerUserID, &record.ID, "machine.stop.succeeded", map[string]any{
		"machine_name": record.Name,
		"runtime_name": liveMachine.Name,
		"stage":        "complete",
		"state":        machineStateStopped,
	})
	return nil
}

func (s *Service) finishStopFailure(record database.MachineRecord, stage string, stopErr error) {
	persistCtx, cancel := s.newPersistContext()
	defer cancel()
	if err := s.store.UpdateMachineState(persistCtx, record.ID, machineStateRunning); err != nil && !errors.Is(err, database.ErrNotFound) {
		return
	}
	s.recordEventBestEffort(&record.OwnerUserID, &record.ID, "machine.stop.failed", map[string]any{
		"machine_name": record.Name,
		"runtime_name": runtimeNameForRecord(record),
		"stage":        strings.TrimSpace(stage),
		"error":        stopErr.Error(),
	})
}
