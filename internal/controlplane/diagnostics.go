package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"fascinate/internal/database"
	machineruntime "fascinate/internal/runtime"
	"fascinate/internal/toolauth"
)

type runtimeDiagnosticsProvider interface {
	InspectMachine(context.Context, string) (machineruntime.MachineDiagnostics, error)
	InspectSnapshot(context.Context, string) (machineruntime.SnapshotDiagnostics, error)
}

type Event struct {
	ID        string         `json:"id"`
	Kind      string         `json:"kind"`
	MachineID *string        `json:"machine_id,omitempty"`
	Payload   map[string]any `json:"payload,omitempty"`
	CreatedAt string         `json:"created_at"`
}

type MachineDiagnostics struct {
	Machine          Machine                            `json:"machine"`
	RuntimeName      string                             `json:"runtime_name"`
	SourceSnapshotID *string                            `json:"source_snapshot_id,omitempty"`
	Runtime          *machineruntime.MachineDiagnostics `json:"runtime,omitempty"`
	RecentEvents     []Event                            `json:"recent_events,omitempty"`
}

type SnapshotDiagnostics struct {
	Snapshot     Snapshot                            `json:"snapshot"`
	RuntimeName  string                              `json:"runtime_name"`
	Runtime      *machineruntime.SnapshotDiagnostics `json:"runtime,omitempty"`
	RecentEvents []Event                             `json:"recent_events,omitempty"`
}

type ToolAuthDiagnostics struct {
	OwnerEmail string             `json:"owner_email"`
	Profiles   []toolauth.Profile `json:"profiles"`
	Events     []Event            `json:"events,omitempty"`
}

type BudgetSummary struct {
	CPU           string `json:"cpu"`
	MemoryBytes   int64  `json:"memory_bytes"`
	DiskBytes     int64  `json:"disk_bytes"`
	MachineCount  int    `json:"machine_count"`
	SnapshotCount int    `json:"snapshot_count"`
}

type BudgetDiagnostics struct {
	OwnerEmail           string        `json:"owner_email"`
	Limits               BudgetSummary `json:"limits"`
	Usage                BudgetSummary `json:"usage"`
	Remaining            BudgetSummary `json:"remaining"`
	HostMinFreeDiskBytes int64         `json:"host_min_free_disk_bytes"`
}

func (s *Service) GetHostDiagnostics(ctx context.Context) ([]Host, error) {
	return s.ListHosts(ctx)
}

func (s *Service) GetMachineDiagnostics(ctx context.Context, name, ownerEmail string) (MachineDiagnostics, error) {
	record, err := s.ownedMachineRecord(ctx, name, ownerEmail)
	if err != nil {
		return MachineDiagnostics{}, err
	}

	machine, err := s.machineFromRecordWithRuntime(ctx, record)
	if err != nil {
		return MachineDiagnostics{}, err
	}

	diag := MachineDiagnostics{
		Machine:          machine,
		RuntimeName:      runtimeNameForRecord(record),
		SourceSnapshotID: record.SourceSnapshotID,
	}
	executor, err := s.executorForHostID(machineHostID(record, s.localHostID))
	if err != nil {
		return MachineDiagnostics{}, err
	}
	runtimeDiag, err := executor.InspectMachine(ctx, runtimeNameForRecord(record))
	if err != nil && !errors.Is(err, machineruntime.ErrMachineNotFound) {
		return MachineDiagnostics{}, err
	}
	if err == nil {
		copy := runtimeDiag
		diag.Runtime = &copy
	}

	if events, err := s.store.ListMachineEvents(ctx, record.ID, 25); err == nil {
		diag.RecentEvents = decodeEvents(events)
	} else if !errors.Is(err, database.ErrNotFound) {
		return MachineDiagnostics{}, err
	}

	return diag, nil
}

func (s *Service) GetSnapshotDiagnostics(ctx context.Context, name, ownerEmail string) (SnapshotDiagnostics, error) {
	ownerEmail = normalizeEmail(ownerEmail)
	if ownerEmail == "" {
		return SnapshotDiagnostics{}, errors.New("owner email is required")
	}
	user, err := s.store.GetUserByEmail(ctx, ownerEmail)
	if err != nil {
		return SnapshotDiagnostics{}, err
	}
	record, err := s.store.GetSnapshotByName(ctx, user.ID, strings.TrimSpace(name))
	if err != nil {
		return SnapshotDiagnostics{}, err
	}

	diag := SnapshotDiagnostics{
		Snapshot:    s.snapshotFromRecord(ctx, record, machineruntime.Snapshot{}),
		RuntimeName: strings.TrimSpace(record.RuntimeName),
	}
	executor, err := s.executorForHostID(snapshotHostID(record))
	if err != nil {
		return SnapshotDiagnostics{}, err
	}
	runtimeDiag, err := executor.InspectSnapshot(ctx, record.RuntimeName)
	if err != nil && !errors.Is(err, machineruntime.ErrSnapshotNotFound) {
		return SnapshotDiagnostics{}, err
	}
	if err == nil {
		copy := runtimeDiag
		diag.Runtime = &copy
	}

	if events, err := s.store.ListActorEvents(ctx, user.ID, 100); err == nil {
		var filtered []database.EventRecord
		for _, event := range events {
			if eventMatchesSnapshot(event, record) {
				filtered = append(filtered, event)
			}
		}
		diag.RecentEvents = decodeEvents(filtered)
	} else if !errors.Is(err, database.ErrNotFound) {
		return SnapshotDiagnostics{}, err
	}

	return diag, nil
}

func (s *Service) GetToolAuthDiagnostics(ctx context.Context, ownerEmail string) (ToolAuthDiagnostics, error) {
	ownerEmail = normalizeEmail(ownerEmail)
	if ownerEmail == "" {
		return ToolAuthDiagnostics{}, errors.New("owner email is required")
	}
	user, err := s.store.GetUserByEmail(ctx, ownerEmail)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return ToolAuthDiagnostics{OwnerEmail: ownerEmail}, nil
		}
		return ToolAuthDiagnostics{}, err
	}

	var profiles []toolauth.Profile
	if s.toolAuth != nil {
		profiles, err = s.toolAuth.ListProfiles(ctx, user.ID)
		if err != nil {
			return ToolAuthDiagnostics{}, err
		}
	}

	diag := ToolAuthDiagnostics{
		OwnerEmail: ownerEmail,
		Profiles:   profiles,
	}
	if events, err := s.store.ListActorEvents(ctx, user.ID, 100); err == nil {
		var filtered []database.EventRecord
		for _, event := range events {
			if strings.HasPrefix(strings.TrimSpace(event.Kind), "toolauth.") {
				filtered = append(filtered, event)
			}
		}
		diag.Events = decodeEvents(filtered)
	} else if !errors.Is(err, database.ErrNotFound) {
		return ToolAuthDiagnostics{}, err
	}

	return diag, nil
}

func (s *Service) GetBudgetDiagnostics(ctx context.Context, ownerEmail string) (BudgetDiagnostics, error) {
	ownerEmail = normalizeEmail(ownerEmail)
	if ownerEmail == "" {
		return BudgetDiagnostics{}, errors.New("owner email is required")
	}

	user, userErr := s.store.GetUserByEmail(ctx, ownerEmail)
	if userErr != nil && !errors.Is(userErr, database.ErrNotFound) {
		return BudgetDiagnostics{}, userErr
	}

	budgetUser := user
	if errors.Is(userErr, database.ErrNotFound) {
		budgetUser.Email = ownerEmail
	}
	budget, err := s.userBudgetForUser(budgetUser)
	if err != nil {
		return BudgetDiagnostics{}, err
	}

	var usage userUsage
	if !errors.Is(userErr, database.ErrNotFound) {
		usage, err = s.calculateUserUsage(ctx, user.ID)
		if err != nil {
			return BudgetDiagnostics{}, err
		}
	}

	minFreeDiskBytes, err := s.hostMinFreeDiskBytes()
	if err != nil {
		return BudgetDiagnostics{}, err
	}

	return BudgetDiagnostics{
		OwnerEmail: ownerEmail,
		Limits: BudgetSummary{
			CPU:           budget.MaxCPUText,
			MemoryBytes:   budget.MaxMemoryBytes,
			DiskBytes:     budget.MaxDiskBytes,
			MachineCount:  budget.MaxMachineCount,
			SnapshotCount: budget.MaxSnapshotCount,
		},
		Usage: BudgetSummary{
			CPU:           formatCPUCount(usage.CPU),
			MemoryBytes:   usage.MemoryBytes,
			DiskBytes:     usage.DiskBytes,
			MachineCount:  usage.MachineCount,
			SnapshotCount: usage.SnapshotCount,
		},
		Remaining: BudgetSummary{
			CPU:           formatCPUCount(maxFloat64(budget.MaxCPU - usage.CPU)),
			MemoryBytes:   maxInt64(budget.MaxMemoryBytes - usage.MemoryBytes),
			DiskBytes:     maxInt64(budget.MaxDiskBytes - usage.DiskBytes),
			MachineCount:  maxInt(budget.MaxMachineCount - usage.MachineCount),
			SnapshotCount: maxInt(budget.MaxSnapshotCount - usage.SnapshotCount),
		},
		HostMinFreeDiskBytes: minFreeDiskBytes,
	}, nil
}

func (s *Service) ListOwnerEvents(ctx context.Context, ownerEmail string, limit int) ([]Event, error) {
	ownerEmail = normalizeEmail(ownerEmail)
	if ownerEmail == "" {
		return nil, errors.New("owner email is required")
	}
	user, err := s.store.GetUserByEmail(ctx, ownerEmail)
	if err != nil {
		return nil, err
	}
	records, err := s.store.ListActorEvents(ctx, user.ID, limit)
	if err != nil {
		return nil, err
	}
	return decodeEvents(records), nil
}

func (s *Service) recordEventBestEffort(actorUserID, machineID *string, kind string, payload map[string]any) {
	if s == nil || s.store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_, _ = s.store.CreateEvent(ctx, database.CreateEventParams{
		ID:          uuid.NewString(),
		ActorUserID: actorUserID,
		MachineID:   machineID,
		Kind:        kind,
		PayloadJSON: string(body),
	})
}

func decodeEvents(records []database.EventRecord) []Event {
	out := make([]Event, 0, len(records))
	for _, record := range records {
		payload := map[string]any{}
		if strings.TrimSpace(record.PayloadJSON) != "" {
			_ = json.Unmarshal([]byte(record.PayloadJSON), &payload)
		}
		out = append(out, Event{
			ID:        record.ID,
			Kind:      record.Kind,
			MachineID: record.MachineID,
			Payload:   payload,
			CreatedAt: record.CreatedAt,
		})
	}
	return out
}

func eventMatchesSnapshot(record database.EventRecord, snapshot database.SnapshotRecord) bool {
	if strings.HasPrefix(strings.TrimSpace(record.Kind), "snapshot.") {
		var payload map[string]any
		if err := json.Unmarshal([]byte(record.PayloadJSON), &payload); err != nil {
			return false
		}
		for _, key := range []string{"snapshot_id", "snapshot_name", "runtime_name"} {
			value, _ := payload[key].(string)
			switch key {
			case "snapshot_id":
				if strings.TrimSpace(value) == strings.TrimSpace(snapshot.ID) {
					return true
				}
			case "snapshot_name":
				if strings.TrimSpace(value) == strings.TrimSpace(snapshot.Name) {
					return true
				}
			case "runtime_name":
				if strings.TrimSpace(value) == strings.TrimSpace(snapshot.RuntimeName) {
					return true
				}
			}
		}
	}
	return false
}

func maxInt(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

func maxInt64(value int64) int64 {
	if value < 0 {
		return 0
	}
	return value
}

func maxFloat64(value float64) float64 {
	if value < 0 {
		return 0
	}
	return value
}
