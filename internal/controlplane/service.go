package controlplane

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"fascinate/internal/config"
	"fascinate/internal/database"
	"fascinate/internal/runtime/incus"
)

var machineNamePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
var memoryLimitPattern = regexp.MustCompile(`(?i)^([0-9]+(?:\.[0-9]+)?)\s*([kmgt]i?b?|b)?$`)

type Runtime interface {
	HealthCheck(context.Context) error
	ListMachines(context.Context) ([]incus.Machine, error)
	GetMachine(context.Context, string) (incus.Machine, error)
	CreateMachine(context.Context, incus.CreateMachineRequest) (incus.Machine, error)
	DeleteMachine(context.Context, string) error
	CloneMachine(context.Context, incus.CloneMachineRequest) (incus.Machine, error)
}

type Service struct {
	cfg     config.Config
	store   *database.Store
	runtime Runtime
	mu      sync.Mutex
}

type Machine struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	OwnerEmail  string         `json:"owner_email"`
	State       string         `json:"state"`
	PrimaryPort int            `json:"primary_port"`
	URL         string         `json:"url,omitempty"`
	CreatedAt   string         `json:"created_at"`
	UpdatedAt   string         `json:"updated_at"`
	Runtime     *incus.Machine `json:"runtime,omitempty"`
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

func New(cfg config.Config, store *database.Store, runtime Runtime) *Service {
	return &Service{
		cfg:     cfg,
		store:   store,
		runtime: runtime,
	}
}

func (s *Service) ListMachines(ctx context.Context, ownerEmail string) ([]Machine, error) {
	records, err := s.store.ListMachines(ctx, ownerEmail)
	if err != nil {
		return nil, err
	}

	liveMachines := map[string]incus.Machine{}
	if runtimeMachines, err := s.runtime.ListMachines(ctx); err == nil {
		for _, machine := range runtimeMachines {
			liveMachines[machine.Name] = machine
		}
	}

	out := make([]Machine, 0, len(records))
	for _, record := range records {
		out = append(out, s.machineFromRecord(ctx, record, liveMachines[record.IncusName]))
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
	liveMachine, err := s.runtime.GetMachine(ctx, record.IncusName)
	if err != nil {
		if errors.Is(err, incus.ErrMachineNotFound) {
			_ = s.store.UpdateMachineState(ctx, record.ID, "missing")
			record.State = "missing"
			return s.machineFromRecord(ctx, record, incus.Machine{}), nil
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
	if err := s.enforceMachineCreatePolicy(ctx, ownerEmail, s.cfg.DefaultMachineCPU, s.cfg.DefaultMachineRAM); err != nil {
		return Machine{}, err
	}

	user, err := s.ensureUser(ctx, ownerEmail)
	if err != nil {
		return Machine{}, err
	}

	liveMachine, err := s.runtime.CreateMachine(ctx, incus.CreateMachineRequest{
		Name:        name,
		Image:       s.cfg.DefaultImage,
		StoragePool: s.cfg.IncusStoragePool,
		CPU:         s.cfg.DefaultMachineCPU,
		Memory:      s.cfg.DefaultMachineRAM,
		PrimaryPort: s.cfg.DefaultPrimaryPort,
	})
	if err != nil {
		return Machine{}, err
	}

	record, err := s.store.CreateMachine(ctx, database.CreateMachineParams{
		ID:          uuid.NewString(),
		Name:        name,
		OwnerUserID: user.ID,
		IncusName:   liveMachine.Name,
		State:       liveMachine.State,
		PrimaryPort: s.cfg.DefaultPrimaryPort,
	})
	if err != nil {
		s.cleanupRuntimeMachine(name)
		return Machine{}, err
	}

	return s.machineFromRecord(ctx, record, liveMachine), nil
}

func (s *Service) DeleteMachine(ctx context.Context, name, ownerEmail string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	record, err := s.ownedMachineRecord(ctx, name, ownerEmail)
	if err != nil {
		return err
	}

	if err := s.runtime.DeleteMachine(ctx, record.IncusName); err != nil && !errors.Is(err, incus.ErrMachineNotFound) {
		return err
	}

	return s.store.MarkMachineDeleted(ctx, record.ID)
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
	liveSource, err := s.runtime.GetMachine(ctx, sourceRecord.IncusName)
	if err != nil {
		return Machine{}, err
	}
	if err := s.validateMachineSizeLimit(liveSource.CPU, liveSource.Memory); err != nil {
		return Machine{}, err
	}

	user, err := s.ensureUser(ctx, ownerEmail)
	if err != nil {
		return Machine{}, err
	}

	liveMachine, err := s.runtime.CloneMachine(ctx, incus.CloneMachineRequest{
		SourceName: sourceRecord.IncusName,
		TargetName: targetName,
	})
	if err != nil {
		return Machine{}, err
	}

	record, err := s.store.CreateMachine(ctx, database.CreateMachineParams{
		ID:          uuid.NewString(),
		Name:        targetName,
		OwnerUserID: user.ID,
		IncusName:   liveMachine.Name,
		State:       liveMachine.State,
		PrimaryPort: sourceRecord.PrimaryPort,
	})
	if err != nil {
		s.cleanupRuntimeMachine(targetName)
		return Machine{}, err
	}

	return s.machineFromRecord(ctx, record, liveMachine), nil
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

func (s *Service) enforceMachineCreatePolicy(ctx context.Context, ownerEmail, cpu, memory string) error {
	if err := s.enforceMachineCountLimit(ctx, ownerEmail); err != nil {
		return err
	}
	return s.validateMachineSizeLimit(cpu, memory)
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

func (s *Service) validateMachineSizeLimit(cpu, memory string) error {
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
		requestedMemory, err := parseMemoryBytes(memory)
		if err != nil {
			return fmt.Errorf("invalid machine memory limit %q: %w", memory, err)
		}
		allowedMemory, err := parseMemoryBytes(maxMemory)
		if err != nil {
			return fmt.Errorf("invalid configured max machine memory %q: %w", maxMemory, err)
		}
		if requestedMemory > allowedMemory {
			return fmt.Errorf("machine size exceeds limit: memory %s > %s", strings.TrimSpace(memory), maxMemory)
		}
	}

	return nil
}

func (s *Service) machineFromRecord(ctx context.Context, record database.MachineRecord, live incus.Machine) Machine {
	if live.Name != "" && live.State != "" && live.State != record.State {
		_ = s.store.UpdateMachineState(ctx, record.ID, live.State)
		record.State = live.State
	}

	var runtimeMachine *incus.Machine
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

func (s *Service) ensureUser(ctx context.Context, email string) (database.User, error) {
	email = normalizeEmail(email)
	if email == "" {
		return database.User{}, fmt.Errorf("owner email is required")
	}

	return s.store.UpsertUser(ctx, email, s.isAdminEmail(email))
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

func parseMemoryBytes(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("memory value is required")
	}

	matches := memoryLimitPattern.FindStringSubmatch(value)
	if matches == nil {
		return 0, fmt.Errorf("unsupported memory value")
	}

	number, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return 0, fmt.Errorf("memory value must be numeric")
	}
	if number <= 0 {
		return 0, fmt.Errorf("memory value must be positive")
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
