package service

import (
	"context"
	"errors"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/config"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/pkg/audit"
	log "github.com/sirupsen/logrus"
)

// Settings -> Instance configuration (sysadmins only) lists every variable of
// the config registry with its effective value, and lets a sysadmin override
// the few a running server reads per use. An override wins over the
// environment; a reset deletes it, returning to the environment or default.
//
// Readers ask this service on every use (PipelinesEnabled, ...). It reads the
// overrides at most once per instanceConfigCacheTTL, so other API replicas
// and the automation worker see a change within that window. When the store
// cannot be read it keeps the values it last read, or the environment before
// the first read.

var (
	ErrInstanceConfigUnknown  = errors.New("unknown configuration variable")
	ErrInstanceConfigReadOnly = errors.New("this variable is set in .env and needs a restart")
)

// InstanceConfigInvalidError is a value the variable does not accept.
type InstanceConfigInvalidError struct{ Message string }

func (e *InstanceConfigInvalidError) Error() string { return e.Message }

const (
	instanceConfigCacheTTL    = 5 * time.Second
	instanceConfigReadTimeout = 2 * time.Second
)

// InstanceConfigStore keeps the overrides (instance_config_overrides).
type InstanceConfigStore interface {
	ListOverrides(ctx context.Context) ([]domain.InstanceConfigOverride, error)
	SetOverride(ctx context.Context, name, value string, actor uuid.UUID) (*domain.InstanceConfigOverride, error)
	DeleteOverride(ctx context.Context, name string) error
}

// InstanceConfigEntry is one variable as the page shows it.
type InstanceConfigEntry struct {
	config.VariableState
	// BaseValue and BaseSource are what a reset returns to: the environment
	// or the default. Set for runtime variables only.
	BaseValue  string
	BaseSource config.Source
	// UpdatedAt is when the override in effect was saved.
	UpdatedAt *time.Time
	// EditableOnRestart marks a variable a sysadmin can change with "Apply and
	// restart"; BaseValue and BaseSource are then what removing the override
	// returns to.
	EditableOnRestart bool
}

type InstanceConfigService struct {
	cfg    *config.Config
	store  InstanceConfigStore
	lookup func(string) (string, bool)
	now    func() time.Time
	logger log.FieldLogger

	// restart is set when restart-only settings can be edited.
	restart *InstanceRestart

	mu        sync.Mutex
	overrides map[string]domain.InstanceConfigOverride
	readAt    time.Time
	read      bool
}

func NewInstanceConfigService(cfg *config.Config, store InstanceConfigStore) *InstanceConfigService {
	return &InstanceConfigService{cfg: cfg, store: store, lookup: os.LookupEnv, now: time.Now, logger: log.StandardLogger()}
}

// PipelinesEnabled is WORKFLOW_PIPELINES_ENABLED in effect.
func (s *InstanceConfigService) PipelinesEnabled() bool {
	return s.boolValue(config.VarWorkflowPipelines, s.cfg.WorkflowPipelines)
}

// RegistrationDisabled is DISABLE_REGISTRATION in effect.
func (s *InstanceConfigService) RegistrationDisabled() bool {
	return s.boolValue(config.VarDisableRegistration, s.cfg.DisableRegistration)
}

// BackupTimeout is BACKUP_TIMEOUT in effect.
func (s *InstanceConfigService) BackupTimeout() time.Duration {
	if raw, ok := s.override(config.VarBackupTimeout); ok {
		if value, err := time.ParseDuration(raw); err == nil && value > 0 {
			return value
		}
		s.logger.WithField("variable", config.VarBackupTimeout).Warn("ignoring an instance configuration override that does not parse")
	}
	return s.cfg.BackupTimeout
}

func (s *InstanceConfigService) boolValue(name string, fallback bool) bool {
	if raw, ok := s.override(name); ok {
		if value, err := strconv.ParseBool(raw); err == nil {
			return value
		}
		s.logger.WithField("variable", name).Warn("ignoring an instance configuration override that does not parse")
	}
	return fallback
}

// override returns the stored value of name, reading the store when the
// cached values are older than the cache window.
func (s *InstanceConfigService) override(name string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.read || s.now().Sub(s.readAt) >= instanceConfigCacheTTL {
		ctx, cancel := context.WithTimeout(context.Background(), instanceConfigReadTimeout)
		rows, err := s.store.ListOverrides(ctx)
		cancel()
		s.read, s.readAt = true, s.now()
		if err != nil {
			s.logger.WithError(err).Warn("instance configuration overrides could not be read; keeping the last values read")
		} else {
			s.overrides = overridesByName(rows)
		}
	}
	row, ok := s.overrides[name]
	return row.Value, ok
}

func overridesByName(rows []domain.InstanceConfigOverride) map[string]domain.InstanceConfigOverride {
	byName := make(map[string]domain.InstanceConfigOverride, len(rows))
	for _, row := range rows {
		byName[row.Name] = row
	}
	return byName
}

// remember replaces the cached overrides with what the store just returned.
func (s *InstanceConfigService) remember(rows map[string]domain.InstanceConfigOverride) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.overrides, s.read, s.readAt = rows, true, s.now()
}

// List returns every registered variable with its effective value, reading
// the overrides from the store.
func (s *InstanceConfigService) List(ctx context.Context) ([]InstanceConfigEntry, error) {
	rows, err := s.store.ListOverrides(ctx)
	if err != nil {
		return nil, err
	}
	overrides := overridesByName(rows)
	s.remember(overrides)
	states := config.Describe(s.cfg, s.lookup)
	var base map[string]config.VariableState
	if s.restart != nil {
		base = map[string]config.VariableState{}
		for _, state := range config.Describe(s.restart.startup.Base, s.lookup) {
			base[state.Name] = state
		}
	}
	entries := make([]InstanceConfigEntry, 0, len(states))
	for _, state := range states {
		entry := entryWith(state, overrides)
		if s.restart != nil && state.RestartEditable() {
			s.layerRestart(&entry, base[state.Name], overrides)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// layerRestart marks a restart-only variable editable and says where its
// effective value comes from: the stored override when this process started
// with it, the environment or default otherwise.
func (s *InstanceConfigService) layerRestart(entry *InstanceConfigEntry, base config.VariableState, overrides map[string]domain.InstanceConfigOverride) {
	entry.EditableOnRestart = true
	entry.BaseValue, entry.BaseSource = base.Value, base.Source
	if _, applied := s.restart.startup.Applied[entry.Name]; applied {
		entry.Source = config.SourceOverride
		if row, ok := overrides[entry.Name]; ok {
			updated := row.UpdatedAt
			entry.UpdatedAt = &updated
		}
	}
}

// SetRestart enables editing of the restart-only settings.
func (s *InstanceConfigService) SetRestart(restart *InstanceRestart) { s.restart = restart }

// RestartStatus reports whether "Apply and restart" is possible and the
// latest run.
func (s *InstanceConfigService) RestartStatus(ctx context.Context) (RestartStatus, error) {
	if s.restart == nil {
		return RestartStatus{}, nil
	}
	return s.restart.Status(ctx)
}

// PlanRestart is the dry run of a set of restart-only values.
func (s *InstanceConfigService) PlanRestart(ctx context.Context, proposed map[string]*string) (*RestartPlan, error) {
	if s.restart == nil {
		return nil, ErrInstanceRestartUnsupported
	}
	return s.restart.Plan(ctx, proposed)
}

// ApplyRestart stores restart-only values and restarts the API.
func (s *InstanceConfigService) ApplyRestart(ctx context.Context, actor uuid.UUID, proposed map[string]*string) (*domain.InstanceRestartRun, error) {
	if s.restart == nil {
		return nil, ErrInstanceRestartUnsupported
	}
	return s.restart.Apply(ctx, actor, proposed)
}

// entryWith lays a runtime variable's override over its environment value.
func entryWith(state config.VariableState, overrides map[string]domain.InstanceConfigOverride) InstanceConfigEntry {
	entry := InstanceConfigEntry{VariableState: state}
	if state.Runtime == nil {
		return entry
	}
	entry.BaseValue, entry.BaseSource = state.Value, state.Source
	if row, ok := overrides[state.Name]; ok {
		updated := row.UpdatedAt
		entry.Value, entry.Source, entry.Set, entry.UpdatedAt = row.Value, config.SourceOverride, row.Value != "", &updated
	}
	return entry
}

// Set stores a sysadmin's value for a runtime variable.
func (s *InstanceConfigService) Set(ctx context.Context, actor uuid.UUID, name, raw string) (*InstanceConfigEntry, error) {
	variable, err := runtimeVariable(name)
	if err != nil {
		return nil, err
	}
	value, err := variable.Normalize(raw)
	if err != nil {
		return nil, &InstanceConfigInvalidError{Message: err.Error()}
	}
	before, err := s.entry(ctx, name)
	if err != nil {
		return nil, err
	}
	if _, err := s.store.SetOverride(ctx, name, value, actor); err != nil {
		return nil, err
	}
	after, err := s.entry(ctx, name)
	if err != nil {
		return nil, err
	}
	audit.Log("system.config_updated", actor, map[string]interface{}{"variable": name, "from": before.Value, "to": after.Value})
	return after, nil
}

// Reset deletes the override of a runtime variable, returning it to the
// environment or default.
func (s *InstanceConfigService) Reset(ctx context.Context, actor uuid.UUID, name string) (*InstanceConfigEntry, error) {
	if _, err := runtimeVariable(name); err != nil {
		return nil, err
	}
	before, err := s.entry(ctx, name)
	if err != nil {
		return nil, err
	}
	if err := s.store.DeleteOverride(ctx, name); err != nil {
		return nil, err
	}
	after, err := s.entry(ctx, name)
	if err != nil {
		return nil, err
	}
	audit.Log("system.config_reset", actor, map[string]interface{}{"variable": name, "from": before.Value, "to": after.Value})
	return after, nil
}

func runtimeVariable(name string) (config.Variable, error) {
	variable, ok := config.Lookup(name)
	if !ok {
		return config.Variable{}, ErrInstanceConfigUnknown
	}
	if variable.Runtime == nil {
		return config.Variable{}, ErrInstanceConfigReadOnly
	}
	return variable, nil
}

func (s *InstanceConfigService) entry(ctx context.Context, name string) (*InstanceConfigEntry, error) {
	entries, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.Name == name {
			return &entry, nil
		}
	}
	return nil, ErrInstanceConfigUnknown
}
