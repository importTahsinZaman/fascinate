package controlplane

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"fascinate/internal/config"
	"fascinate/internal/database"
	machineruntime "fascinate/internal/runtime"
)

const envVarBuiltinPrefix = "FASCINATE_"

var envVarKeyPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
var envVarReferencePattern = regexp.MustCompile(`\$\{([A-Z][A-Z0-9_]*)\}`)

const (
	envVarMachineName = "FASCINATE_MACHINE_NAME"
	envVarMachineID   = "FASCINATE_MACHINE_ID"
	envVarPublicURL   = "FASCINATE_PUBLIC_URL"
	envVarPrimaryPort = "FASCINATE_PRIMARY_PORT"
	envVarBaseDomain  = "FASCINATE_BASE_DOMAIN"
	envVarHostID      = "FASCINATE_HOST_ID"
	envVarHostRegion  = "FASCINATE_HOST_REGION"
)

// BuiltinEnvVar describes an automatically injected Fascinate VM environment variable.
type BuiltinEnvVar struct {
	Key         string `json:"key"`
	Description string `json:"description"`
}

var builtinEnvVarCatalog = []BuiltinEnvVar{
	{Key: envVarMachineName, Description: "Name of the current VM."},
	{Key: envVarMachineID, Description: "Stable Fascinate ID for the current VM."},
	{Key: envVarPublicURL, Description: "Public HTTPS URL for the current VM, routed to its primary port."},
	{Key: envVarPrimaryPort, Description: "Primary port Fascinate exposes for the current VM."},
	{Key: envVarBaseDomain, Description: "Base domain Fascinate uses to generate machine URLs."},
	{Key: envVarHostID, Description: "ID of the host currently running the VM."},
	{Key: envVarHostRegion, Description: "Region advertised by the host currently running the VM."},
}

type EnvVar struct {
	Key       string `json:"key"`
	RawValue  string `json:"raw_value"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type SetEnvVarInput struct {
	OwnerEmail string
	Key        string
	Value      string
}

type EffectiveEnvVar struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type MachineEnv struct {
	MachineName string            `json:"machine_name"`
	Entries     []EffectiveEnvVar `json:"entries"`
}

// BuiltinEnvVars returns the built-in Fascinate VM environment variables in UI display order.
func BuiltinEnvVars() []BuiltinEnvVar {
	out := make([]BuiltinEnvVar, len(builtinEnvVarCatalog))
	copy(out, builtinEnvVarCatalog)
	return out
}

func (s *Service) ListEnvVars(ctx context.Context, ownerEmail string) ([]EnvVar, error) {
	ownerEmail = normalizeEmail(ownerEmail)
	if ownerEmail == "" {
		return nil, fmt.Errorf("owner email is required")
	}

	user, err := s.store.GetUserByEmail(ctx, ownerEmail)
	if err != nil {
		if err == database.ErrNotFound {
			return []EnvVar{}, nil
		}
		return nil, err
	}

	records, err := s.store.ListUserEnvVars(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	return envVarsFromRecords(records), nil
}

func (s *Service) SetEnvVar(ctx context.Context, input SetEnvVarInput) (EnvVar, error) {
	ownerEmail := normalizeEmail(input.OwnerEmail)
	if ownerEmail == "" {
		return EnvVar{}, fmt.Errorf("owner email is required")
	}

	unlock := s.lockUserMutations(ownerEmail)
	defer unlock()

	user, err := s.ensureUser(ctx, ownerEmail)
	if err != nil {
		return EnvVar{}, err
	}

	key, err := validateEnvVarKey(input.Key)
	if err != nil {
		return EnvVar{}, err
	}
	value, err := validateEnvVarValue(input.Value)
	if err != nil {
		return EnvVar{}, err
	}

	records, err := s.store.ListUserEnvVars(ctx, user.ID)
	if err != nil {
		return EnvVar{}, err
	}
	rawValues := rawEnvVarMap(records)
	rawValues[key] = value

	if _, err := renderEffectiveEnv(validationBuiltins(s.cfg), rawValues); err != nil {
		return EnvVar{}, err
	}

	record, err := s.store.UpsertUserEnvVar(ctx, database.UpsertEnvVarParams{
		UserID:   user.ID,
		Key:      key,
		RawValue: value,
	})
	if err != nil {
		return EnvVar{}, err
	}

	s.syncRunningMachineEnvBestEffort(ownerEmail, user.ID)
	return envVarFromRecord(record), nil
}

func (s *Service) DeleteEnvVar(ctx context.Context, ownerEmail, key string) error {
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
	key, err = validateEnvVarKey(key)
	if err != nil {
		return err
	}
	if err := s.store.DeleteUserEnvVar(ctx, user.ID, key); err != nil {
		return err
	}

	s.syncRunningMachineEnvBestEffort(ownerEmail, user.ID)
	return nil
}

func (s *Service) GetMachineEnv(ctx context.Context, name, ownerEmail string) (MachineEnv, error) {
	record, err := s.ownedMachineRecord(ctx, name, ownerEmail)
	if err != nil {
		return MachineEnv{}, err
	}

	entries, err := s.renderMachineEnvEntries(ctx, record)
	if err != nil {
		return MachineEnv{}, err
	}
	return MachineEnv{
		MachineName: record.Name,
		Entries:     entries,
	}, nil
}

func (s *Service) syncManagedEnv(ctx context.Context, record database.MachineRecord) error {
	executor, err := s.executorForHostID(machineHostID(record, s.localHostID))
	if err != nil {
		return err
	}
	entries, err := s.renderMachineEnvMap(ctx, record)
	if err != nil {
		return err
	}
	return executor.SyncManagedEnv(ctx, runtimeNameForRecord(record), machineruntime.ManagedEnvRequest{
		Entries: entries,
	})
}

func (s *Service) syncRunningMachineEnvBestEffort(ownerEmail, ownerUserID string) {
	if s == nil || s.store == nil {
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		records, err := s.store.ListMachines(ctx, ownerEmail)
		if err != nil {
			return
		}

		for _, record := range records {
			if !strings.EqualFold(record.State, machineStateRunning) {
				continue
			}
			if err := s.syncManagedEnv(ctx, record); err != nil {
				s.recordEventBestEffort(&ownerUserID, &record.ID, "env.sync.failed", map[string]any{
					"machine_name": record.Name,
					"runtime_name": runtimeNameForRecord(record),
					"error":        err.Error(),
				})
			} else {
				s.recordEventBestEffort(&ownerUserID, &record.ID, "env.sync.succeeded", map[string]any{
					"machine_name": record.Name,
					"runtime_name": runtimeNameForRecord(record),
				})
			}
		}
	}()
}

func (s *Service) renderMachineEnvMap(ctx context.Context, record database.MachineRecord) (map[string]string, error) {
	userVars, err := s.userEnvVarMap(ctx, record.OwnerUserID)
	if err != nil {
		return nil, err
	}
	return renderEffectiveEnv(s.machineBuiltins(ctx, record), userVars)
}

func (s *Service) renderMachineEnvEntries(ctx context.Context, record database.MachineRecord) ([]EffectiveEnvVar, error) {
	values, err := s.renderMachineEnvMap(ctx, record)
	if err != nil {
		return nil, err
	}
	keys := sortedEnvKeys(values)
	entries := make([]EffectiveEnvVar, 0, len(keys))
	for _, key := range keys {
		entries = append(entries, EffectiveEnvVar{
			Key:   key,
			Value: values[key],
		})
	}
	return entries, nil
}

func (s *Service) userEnvVarMap(ctx context.Context, userID string) (map[string]string, error) {
	records, err := s.store.ListUserEnvVars(ctx, userID)
	if err != nil {
		return nil, err
	}
	return rawEnvVarMap(records), nil
}

func (s *Service) machineBuiltins(ctx context.Context, record database.MachineRecord) map[string]string {
	hostID := machineHostID(record, s.localHostID)
	hostRegion := strings.TrimSpace(s.cfg.HostRegion)
	if hostID != "" {
		if host, err := s.store.GetHostByID(ctx, hostID); err == nil {
			hostRegion = strings.TrimSpace(host.Region)
		}
	}

	return machineBuiltins(record.ID, record.Name, s.cfg.BaseDomain, record.PrimaryPort, hostID, hostRegion)
}

func validationBuiltins(cfg config.Config) map[string]string {
	return map[string]string{
		envVarMachineName: "",
		envVarMachineID:   "",
		envVarPublicURL:   "",
		envVarPrimaryPort: "",
		envVarBaseDomain:  strings.TrimSpace(cfg.BaseDomain),
		envVarHostID:      strings.TrimSpace(cfg.HostID),
		envVarHostRegion:  strings.TrimSpace(cfg.HostRegion),
	}
}

func machineBuiltins(machineID, machineName, baseDomain string, primaryPort int, hostID, hostRegion string) map[string]string {
	return map[string]string{
		envVarMachineName: strings.TrimSpace(machineName),
		envVarMachineID:   strings.TrimSpace(machineID),
		envVarPublicURL:   machineURL(machineName, baseDomain),
		envVarPrimaryPort: strconv.Itoa(primaryPort),
		envVarBaseDomain:  strings.TrimSpace(baseDomain),
		envVarHostID:      strings.TrimSpace(hostID),
		envVarHostRegion:  strings.TrimSpace(hostRegion),
	}
}

func renderEffectiveEnv(builtins map[string]string, raw map[string]string) (map[string]string, error) {
	values := make(map[string]string, len(builtins)+len(raw))
	for key, value := range builtins {
		values[key] = value
	}

	resolved := make(map[string]string, len(raw))
	resolving := make(map[string]bool, len(raw))
	var resolve func(string) (string, error)
	resolve = func(key string) (string, error) {
		if value, ok := values[key]; ok {
			return value, nil
		}
		if value, ok := resolved[key]; ok {
			return value, nil
		}
		rawValue, ok := raw[key]
		if !ok {
			return "", fmt.Errorf("env var %q references undefined key %q", key, key)
		}
		if resolving[key] {
			return "", fmt.Errorf("env var %q participates in a reference cycle", key)
		}

		resolving[key] = true
		defer delete(resolving, key)

		matches := envVarReferencePattern.FindAllStringSubmatch(rawValue, -1)
		rendered := rawValue
		for _, match := range matches {
			reference := strings.TrimSpace(match[1])
			referencedValue, ok := values[reference]
			if !ok {
				if _, rawOk := raw[reference]; !rawOk {
					return "", fmt.Errorf("env var %q references undefined key %q", key, reference)
				}
				var err error
				referencedValue, err = resolve(reference)
				if err != nil {
					return "", err
				}
			}
			rendered = strings.ReplaceAll(rendered, match[0], referencedValue)
		}

		resolved[key] = rendered
		values[key] = rendered
		return rendered, nil
	}

	keys := sortedEnvKeys(raw)
	for _, key := range keys {
		if _, err := resolve(key); err != nil {
			return nil, err
		}
	}

	final := make(map[string]string, len(values))
	for key, value := range values {
		if strings.HasPrefix(key, "_FASCINATE_") {
			continue
		}
		final[key] = value
	}
	return final, nil
}

func validateEnvVarKey(value string) (string, error) {
	key := strings.ToUpper(strings.TrimSpace(value))
	if key == "" {
		return "", fmt.Errorf("env var key is required")
	}
	if strings.HasPrefix(key, envVarBuiltinPrefix) {
		return "", fmt.Errorf("env var key %q is reserved", key)
	}
	if !envVarKeyPattern.MatchString(key) {
		return "", fmt.Errorf("env var key %q must match %s", key, envVarKeyPattern.String())
	}
	return key, nil
}

func validateEnvVarValue(value string) (string, error) {
	if strings.Contains(value, "\n") || strings.Contains(value, "\r") {
		return "", fmt.Errorf("env var values must be single-line")
	}
	return value, nil
}

func envVarsFromRecords(records []database.EnvVarRecord) []EnvVar {
	out := make([]EnvVar, 0, len(records))
	for _, record := range records {
		out = append(out, envVarFromRecord(record))
	}
	return out
}

func envVarFromRecord(record database.EnvVarRecord) EnvVar {
	return EnvVar{
		Key:       record.Key,
		RawValue:  record.RawValue,
		CreatedAt: record.CreatedAt,
		UpdatedAt: record.UpdatedAt,
	}
}

func rawEnvVarMap(records []database.EnvVarRecord) map[string]string {
	out := make(map[string]string, len(records))
	for _, record := range records {
		out[strings.TrimSpace(record.Key)] = record.RawValue
	}
	return out
}

func sortedEnvKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
