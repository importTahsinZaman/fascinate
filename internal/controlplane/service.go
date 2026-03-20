package controlplane

import (
	"context"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"fascinate/internal/config"
	"fascinate/internal/database"
	machineruntime "fascinate/internal/runtime"
)

var machineNamePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
var memoryLimitPattern = regexp.MustCompile(`(?i)^([0-9]+(?:\.[0-9]+)?)\s*([kmgt]i?b?|b)?$`)

const (
	machineStateCreating = "CREATING"
	machineStateRunning  = "RUNNING"
	machineStateFailed   = "FAILED"
)

type Service struct {
	cfg      config.Config
	store    *database.Store
	runtime  machineruntime.Manager
	toolAuth toolAuthManager
	mu       sync.Mutex

	createCancels map[string]context.CancelFunc
}

type Machine struct {
	ID           string                  `json:"id"`
	Name         string                  `json:"name"`
	OwnerEmail   string                  `json:"owner_email"`
	State        string                  `json:"state"`
	PrimaryPort  int                     `json:"primary_port"`
	URL          string                  `json:"url,omitempty"`
	ShowTutorial bool                    `json:"show_tutorial,omitempty"`
	CreatedAt    string                  `json:"created_at"`
	UpdatedAt    string                  `json:"updated_at"`
	Runtime      *machineruntime.Machine `json:"runtime,omitempty"`
}

type CreateMachineInput struct {
	Name       string
	OwnerEmail string
}

type CloneMachineInput struct {
	SourceName string
	TargetName string
	OwnerEmail string
}

type toolAuthManager interface {
	RestoreAll(context.Context, string, string, string) error
	CaptureAll(context.Context, string, string, string) error
}

func New(cfg config.Config, store *database.Store, runtime machineruntime.Manager, extras ...toolAuthManager) *Service {
	var toolAuth toolAuthManager
	if len(extras) > 0 {
		toolAuth = extras[0]
	}

	return &Service{
		cfg:           cfg,
		store:         store,
		runtime:       runtime,
		toolAuth:      toolAuth,
		createCancels: map[string]context.CancelFunc{},
	}
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
	if runtimeMachines, err := s.runtime.ListMachines(ctx); err == nil {
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
	liveMachine, err := s.runtime.GetMachine(ctx, runtimeNameForRecord(record))
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
	s.mu.Lock()
	defer s.mu.Unlock()

	name, err := validateMachineName(input.Name)
	if err != nil {
		return Machine{}, err
	}
	ownerEmail := normalizeEmail(input.OwnerEmail)
	if ownerEmail == "" {
		return Machine{}, fmt.Errorf("owner email is required")
	}
	existingRecords, err := s.store.ListMachines(ctx, ownerEmail)
	if err != nil {
		return Machine{}, err
	}
	if err := s.enforceMachineCreatePolicy(ctx, ownerEmail, s.cfg.DefaultMachineCPU, s.cfg.DefaultMachineRAM, s.cfg.DefaultMachineDisk); err != nil {
		return Machine{}, err
	}

	user, err := s.ensureUser(ctx, ownerEmail)
	if err != nil {
		return Machine{}, err
	}
	s.syncToolAuthFromOwnerRunningMachines(ctx, user.ID, name)

	persistCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	record, err := s.store.CreateMachine(persistCtx, database.CreateMachineParams{
		ID:          uuid.NewString(),
		Name:        name,
		OwnerUserID: user.ID,
		RuntimeName: name,
		State:       machineStateCreating,
		PrimaryPort: s.cfg.DefaultPrimaryPort,
	})
	if err != nil {
		return Machine{}, err
	}

	if len(existingRecords) > 0 {
		if err := s.store.MarkUserTutorialCompleted(persistCtx, user.ID); err != nil {
			return Machine{}, err
		}
	}

	s.queueMachineCreateLocked(record)

	return s.machineFromRecord(ctx, record, machineruntime.Machine{}), nil
}

func (s *Service) DeleteMachine(ctx context.Context, name, ownerEmail string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	record, err := s.ownedMachineRecord(ctx, name, ownerEmail)
	if err != nil {
		return err
	}

	deleteCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	runtimeName := runtimeNameForRecord(record)
	if cancelCreate, ok := s.createCancels[runtimeName]; ok {
		cancelCreate()
		delete(s.createCancels, runtimeName)
	}

	s.captureToolAuthBestEffort(deleteCtx, record)

	if err := s.runtime.DeleteMachine(deleteCtx, runtimeName); err != nil && !errors.Is(err, machineruntime.ErrMachineNotFound) {
		return err
	}

	return s.store.MarkMachineDeleted(deleteCtx, record.ID)
}

func (s *Service) CloneMachine(ctx context.Context, input CloneMachineInput) (Machine, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

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
	if err := s.enforceMachineCountLimit(ctx, ownerEmail); err != nil {
		return Machine{}, err
	}

	sourceRecord, err := s.store.GetMachineByName(ctx, sourceName)
	if err != nil {
		return Machine{}, err
	}
	if normalizeEmail(sourceRecord.OwnerEmail) != ownerEmail {
		return Machine{}, database.ErrNotFound
	}
	liveSource, err := s.runtime.GetMachine(ctx, runtimeNameForRecord(sourceRecord))
	if err != nil {
		return Machine{}, err
	}
	rootDiskSize := strings.TrimSpace(liveSource.Disk)
	if rootDiskSize == "" {
		rootDiskSize = s.cfg.DefaultMachineDisk
	}
	if err := s.validateMachineSizeLimit(liveSource.CPU, liveSource.Memory, rootDiskSize); err != nil {
		return Machine{}, err
	}

	user, err := s.ensureUser(ctx, ownerEmail)
	if err != nil {
		return Machine{}, err
	}

	liveMachine, err := s.runtime.CloneMachine(ctx, machineruntime.CloneMachineRequest{
		SourceName:   runtimeNameForRecord(sourceRecord),
		TargetName:   targetName,
		RootDiskSize: rootDiskSize,
	})
	if err != nil {
		return Machine{}, err
	}
	if s.toolAuth != nil {
		if err := s.toolAuth.RestoreAll(ctx, user.ID, liveMachine.Name, liveMachine.GuestUser); err != nil {
			s.cleanupRuntimeMachine(targetName)
			return Machine{}, err
		}
	}

	persistCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	record, err := s.store.CreateMachine(persistCtx, database.CreateMachineParams{
		ID:          uuid.NewString(),
		Name:        targetName,
		OwnerUserID: user.ID,
		RuntimeName: liveMachine.Name,
		State:       liveMachine.State,
		PrimaryPort: sourceRecord.PrimaryPort,
	})
	if err != nil {
		s.cleanupRuntimeMachine(targetName)
		return Machine{}, err
	}

	if err := s.store.MarkUserTutorialCompleted(persistCtx, user.ID); err != nil {
		return Machine{}, err
	}

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
	return s.syncToolAuthForRecord(ctx, record)
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
		State:       record.State,
		PrimaryPort: record.PrimaryPort,
		URL:         machineURL(record.Name, s.cfg.BaseDomain),
		CreatedAt:   record.CreatedAt,
		UpdatedAt:   record.UpdatedAt,
		Runtime:     runtimeMachine,
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

func (s *Service) ensureUser(ctx context.Context, email string) (database.User, error) {
	email = normalizeEmail(email)
	if email == "" {
		return database.User{}, fmt.Errorf("owner email is required")
	}

	return s.store.UpsertUser(ctx, email, s.isAdminEmail(email))
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

	runtimeMachines, err := s.runtime.ListMachines(ctx)
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
		if err := s.runtime.DeleteMachine(ctx, name); err != nil && !errors.Is(err, machineruntime.ErrMachineNotFound) {
			return err
		}
	}

	for runtimeName, record := range recordByRuntime {
		liveMachine, ok := liveMachines[runtimeName]
		if !ok {
			if strings.EqualFold(record.State, machineStateCreating) {
				s.mu.Lock()
				s.queueMachineCreateLocked(record)
				s.mu.Unlock()
			}
			continue
		}

		if !strings.EqualFold(record.State, liveMachine.State) {
			if err := s.store.UpdateMachineState(ctx, record.ID, liveMachine.State); err != nil && !errors.Is(err, database.ErrNotFound) {
				return err
			}
		}
	}

	return nil
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
		if err := s.syncToolAuthForRecord(ctx, record); err != nil && !errors.Is(err, database.ErrNotFound) && !errors.Is(err, machineruntime.ErrMachineNotFound) {
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

func (s *Service) cleanupRuntimeMachine(name string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = s.runtime.DeleteMachine(ctx, name)
}

func (s *Service) queueMachineCreateLocked(record database.MachineRecord) {
	runtimeName := strings.TrimSpace(record.RuntimeName)
	if runtimeName == "" {
		runtimeName = strings.TrimSpace(record.Name)
	}
	if runtimeName == "" {
		return
	}
	if _, ok := s.createCancels[runtimeName]; ok {
		return
	}

	createCtx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	s.createCancels[runtimeName] = cancel

	go s.runMachineCreate(createCtx, record, machineruntime.CreateMachineRequest{
		Name:         runtimeName,
		Image:        s.cfg.DefaultImage,
		CPU:          s.cfg.DefaultMachineCPU,
		Memory:       s.cfg.DefaultMachineRAM,
		RootDiskSize: s.cfg.DefaultMachineDisk,
		PrimaryPort:  record.PrimaryPort,
	})
}

func (s *Service) runMachineCreate(ctx context.Context, record database.MachineRecord, req machineruntime.CreateMachineRequest) {
	defer func() {
		s.mu.Lock()
		if cancel, ok := s.createCancels[req.Name]; ok {
			cancel()
			delete(s.createCancels, req.Name)
		}
		s.mu.Unlock()
	}()

	liveMachine, err := s.runtime.CreateMachine(ctx, req)
	if err != nil {
		s.finishCreateFailure(record, req.Name, err)
		return
	}

	if err := s.restoreToolAuth(ctx, record, liveMachine); err != nil {
		s.finishCreateFailure(record, req.Name, err)
		return
	}

	s.finishCreateSuccess(record, liveMachine)
}

func (s *Service) finishCreateSuccess(record database.MachineRecord, liveMachine machineruntime.Machine) {
	persistCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	currentRecord, err := s.store.GetMachineByName(persistCtx, record.Name)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			s.cleanupRuntimeMachine(runtimeNameForRecord(record))
			return
		}
		log.Printf("fascinate: finalize machine %s: %v", record.Name, err)
		s.cleanupRuntimeMachine(runtimeNameForRecord(record))
		return
	}

	if err := s.store.UpdateMachineState(persistCtx, currentRecord.ID, liveMachine.State); err != nil {
		if errors.Is(err, database.ErrNotFound) {
			s.cleanupRuntimeMachine(runtimeNameForRecord(record))
			return
		}
		log.Printf("fascinate: update machine %s state: %v", record.Name, err)
	}
}

func (s *Service) finishCreateFailure(record database.MachineRecord, runtimeName string, createErr error) {
	log.Printf("fascinate: machine create failed for %s: %v", record.Name, createErr)
	s.cleanupRuntimeMachine(runtimeName)

	persistCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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

	return s.toolAuth.RestoreAll(ctx, record.OwnerUserID, runtimeName, guestUser)
}

func (s *Service) syncToolAuthForRecord(ctx context.Context, record database.MachineRecord) error {
	if s.toolAuth == nil {
		return nil
	}

	runtimeName := runtimeNameForRecord(record)
	if runtimeName == "" {
		return nil
	}

	liveMachine, err := s.runtime.GetMachine(ctx, runtimeName)
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

	return s.toolAuth.CaptureAll(ctx, record.OwnerUserID, runtimeName, guestUser)
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

		if err := s.syncToolAuthForRecord(ctx, candidate); err != nil &&
			!errors.Is(err, database.ErrNotFound) &&
			!errors.Is(err, machineruntime.ErrMachineNotFound) {
			log.Printf("fascinate: pre-create tool auth sync from %s: %v", candidate.Name, err)
		}
	}
}

func (s *Service) captureToolAuthBestEffort(ctx context.Context, record database.MachineRecord) {
	if err := s.syncToolAuthForRecord(ctx, record); err != nil && !errors.Is(err, database.ErrNotFound) && !errors.Is(err, machineruntime.ErrMachineNotFound) {
		log.Printf("fascinate: capture tool auth for %s: %v", record.Name, err)
	}
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
