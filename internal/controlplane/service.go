package controlplane

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"fascinate/internal/config"
	"fascinate/internal/database"
	"fascinate/internal/runtime/incus"
)

var machineNamePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

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
	name, err := validateMachineName(input.Name)
	if err != nil {
		return Machine{}, err
	}

	user, err := s.ensureUser(ctx, input.OwnerEmail)
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

	sourceRecord, err := s.store.GetMachineByName(ctx, sourceName)
	if err != nil {
		return Machine{}, err
	}
	if normalizeEmail(sourceRecord.OwnerEmail) != ownerEmail {
		return Machine{}, database.ErrNotFound
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

func machineURL(name, baseDomain string) string {
	baseDomain = strings.TrimSpace(baseDomain)
	if baseDomain == "" {
		return ""
	}

	return fmt.Sprintf("https://%s.%s", name, baseDomain)
}
