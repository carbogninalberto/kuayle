package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/config"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/require"
)

// memoryConfigStore keeps overrides in memory, as instance_config_overrides
// does, and counts its reads.
type memoryConfigStore struct {
	mu    sync.Mutex
	rows  map[string]domain.InstanceConfigOverride
	reads int
	fail  error
}

func newMemoryConfigStore() *memoryConfigStore {
	return &memoryConfigStore{rows: map[string]domain.InstanceConfigOverride{}}
}

func (s *memoryConfigStore) ListOverrides(context.Context) ([]domain.InstanceConfigOverride, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reads++
	if s.fail != nil {
		return nil, s.fail
	}
	rows := make([]domain.InstanceConfigOverride, 0, len(s.rows))
	for _, row := range s.rows {
		rows = append(rows, row)
	}
	return rows, nil
}

func (s *memoryConfigStore) SetOverride(_ context.Context, name, value string, actor uuid.UUID) (*domain.InstanceConfigOverride, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail != nil {
		return nil, s.fail
	}
	row := domain.InstanceConfigOverride{Name: name, Value: value, UpdatedBy: &actor, UpdatedAt: time.Now()}
	s.rows[name] = row
	return &row, nil
}

func (s *memoryConfigStore) DeleteOverride(_ context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail != nil {
		return s.fail
	}
	delete(s.rows, name)
	return nil
}

type instanceConfigHarness struct {
	svc   *InstanceConfigService
	store *memoryConfigStore
	clock time.Time
}

func newInstanceConfigHarness(cfg *config.Config, env map[string]string) *instanceConfigHarness {
	h := &instanceConfigHarness{store: newMemoryConfigStore(), clock: time.Date(2026, 11, 25, 9, 0, 0, 0, time.UTC)}
	h.svc = NewInstanceConfigService(cfg, h.store)
	h.svc.now = func() time.Time { return h.clock }
	h.svc.lookup = func(name string) (string, bool) {
		value, ok := env[name]
		return value, ok
	}
	return h
}

func entryNamed(t *testing.T, entries []InstanceConfigEntry, name string) InstanceConfigEntry {
	t.Helper()
	for _, entry := range entries {
		if entry.Name == name {
			return entry
		}
	}
	t.Fatalf("%s is not listed", name)
	return InstanceConfigEntry{}
}

// A sysadmin's value wins over the environment at once, and a reset returns
// to the environment; the list says which one is in effect.
func TestInstanceConfigOverrideWinsAndResetReturnsToEnvironment(t *testing.T) {
	cfg := &config.Config{WorkflowPipelines: true, BackupTimeout: 2 * time.Hour}
	h := newInstanceConfigHarness(cfg, map[string]string{config.VarWorkflowPipelines: "true"})
	admin := uuid.New()
	ctx := context.Background()
	require.True(t, h.svc.PipelinesEnabled())

	entry, err := h.svc.Set(ctx, admin, config.VarWorkflowPipelines, "false")
	require.NoError(t, err)
	require.Equal(t, "false", entry.Value)
	require.Equal(t, config.SourceOverride, entry.Source)
	require.Equal(t, "true", entry.BaseValue)
	require.Equal(t, config.SourceEnvironment, entry.BaseSource)
	require.False(t, h.svc.PipelinesEnabled(), "the change applies without a restart")

	_, err = h.svc.Set(ctx, admin, config.VarBackupTimeout, "90m")
	require.NoError(t, err)
	require.Equal(t, 90*time.Minute, h.svc.BackupTimeout())
	_, err = h.svc.Set(ctx, admin, config.VarDisableRegistration, "true")
	require.NoError(t, err)
	require.True(t, h.svc.RegistrationDisabled())

	entries, err := h.svc.List(ctx)
	require.NoError(t, err)
	listed := entryNamed(t, entries, config.VarWorkflowPipelines)
	require.Equal(t, config.SourceOverride, listed.Source)
	require.NotNil(t, listed.UpdatedAt)
	require.Equal(t, config.SourceDefault, entryNamed(t, entries, "JWT_SECRET").Source)

	entry, err = h.svc.Reset(ctx, admin, config.VarWorkflowPipelines)
	require.NoError(t, err)
	require.Equal(t, "true", entry.Value)
	require.Equal(t, config.SourceEnvironment, entry.Source)
	require.Nil(t, entry.UpdatedAt)
	require.True(t, h.svc.PipelinesEnabled())

	entry, err = h.svc.Reset(ctx, admin, config.VarBackupTimeout)
	require.NoError(t, err)
	require.Equal(t, config.SourceDefault, entry.Source)
	require.Equal(t, 2*time.Hour, h.svc.BackupTimeout())
}

// Only runtime variables are accepted, each by type and range; nothing is
// stored for a refused value.
func TestInstanceConfigRefusesUnknownReadOnlyAndInvalidValues(t *testing.T) {
	h := newInstanceConfigHarness(&config.Config{BackupTimeout: 2 * time.Hour}, nil)
	admin := uuid.New()
	ctx := context.Background()

	_, err := h.svc.Set(ctx, admin, "NOT_A_VARIABLE", "true")
	require.ErrorIs(t, err, ErrInstanceConfigUnknown)
	_, err = h.svc.Reset(ctx, admin, "NOT_A_VARIABLE")
	require.ErrorIs(t, err, ErrInstanceConfigUnknown)
	for _, name := range []string{"JWT_SECRET", "DATABASE_URL", "FRONTEND_URL", "ISSUE_DELETE_GRACE_PERIOD", "PORT"} {
		_, err = h.svc.Set(ctx, admin, name, "x")
		require.ErrorIs(t, err, ErrInstanceConfigReadOnly, name)
		_, err = h.svc.Reset(ctx, admin, name)
		require.ErrorIs(t, err, ErrInstanceConfigReadOnly, name)
	}
	for name, value := range map[string]string{
		config.VarWorkflowPipelines:   "maybe",
		config.VarDisableRegistration: "",
		config.VarBackupTimeout:       "1s",
	} {
		_, err = h.svc.Set(ctx, admin, name, value)
		var invalid *InstanceConfigInvalidError
		require.ErrorAs(t, err, &invalid, name)
		require.NotEmpty(t, invalid.Message)
	}
	_, err = h.svc.Set(ctx, admin, config.VarBackupTimeout, "1000h")
	var invalid *InstanceConfigInvalidError
	require.ErrorAs(t, err, &invalid)
	require.Empty(t, h.store.rows)
}

// Readers go through a short cache: one store read per window, and a change
// made by another replica is seen once the window passes.
func TestInstanceConfigReadsTheStoreOncePerWindow(t *testing.T) {
	h := newInstanceConfigHarness(&config.Config{WorkflowPipelines: true}, nil)
	for range 50 {
		require.True(t, h.svc.PipelinesEnabled())
	}
	require.Equal(t, 1, h.store.reads)

	h.store.rows[config.VarWorkflowPipelines] = domain.InstanceConfigOverride{Name: config.VarWorkflowPipelines, Value: "false"}
	require.True(t, h.svc.PipelinesEnabled(), "another replica's change waits for the window")
	h.clock = h.clock.Add(instanceConfigCacheTTL)
	require.False(t, h.svc.PipelinesEnabled())
	require.Equal(t, 2, h.store.reads)
}

// When the store cannot be read, readers keep the last values read, or the
// environment before the first read, and try again after the window.
func TestInstanceConfigFallsBackWhenTheStoreFails(t *testing.T) {
	h := newInstanceConfigHarness(&config.Config{WorkflowPipelines: true}, nil)
	h.store.fail = errors.New("database is down")
	require.True(t, h.svc.PipelinesEnabled(), "the environment before the first read")
	require.True(t, h.svc.PipelinesEnabled())
	require.Equal(t, 1, h.store.reads, "a failed read is not retried within the window")

	h.store.fail = nil
	h.store.rows[config.VarWorkflowPipelines] = domain.InstanceConfigOverride{Name: config.VarWorkflowPipelines, Value: "false"}
	h.clock = h.clock.Add(instanceConfigCacheTTL)
	require.False(t, h.svc.PipelinesEnabled())

	h.store.fail = errors.New("database is down again")
	h.clock = h.clock.Add(instanceConfigCacheTTL)
	require.False(t, h.svc.PipelinesEnabled(), "the last values read")

	_, err := h.svc.List(context.Background())
	require.Error(t, err, "the page reports a store failure")

	// A stored value that does not parse falls back to the environment.
	h.store.fail = nil
	h.store.rows[config.VarWorkflowPipelines] = domain.InstanceConfigOverride{Name: config.VarWorkflowPipelines, Value: "garbage"}
	h.clock = h.clock.Add(instanceConfigCacheTTL)
	require.True(t, h.svc.PipelinesEnabled())
}

// Every change and reset is written to the audit log with who, what and the
// values before and after.
func TestInstanceConfigChangesAreAudited(t *testing.T) {
	hook := logtest.NewGlobal()
	defer logrus.StandardLogger().ReplaceHooks(make(logrus.LevelHooks))
	h := newInstanceConfigHarness(&config.Config{WorkflowPipelines: true}, nil)
	admin := uuid.New()
	ctx := context.Background()

	_, err := h.svc.Set(ctx, admin, config.VarWorkflowPipelines, "false")
	require.NoError(t, err)
	entry := hook.LastEntry()
	require.NotNil(t, entry)
	require.Equal(t, true, entry.Data["audit"])
	require.Equal(t, "system.config_updated", entry.Data["action"])
	require.Equal(t, admin.String(), entry.Data["actor_id"])
	require.Equal(t, config.VarWorkflowPipelines, entry.Data["variable"])
	require.Equal(t, "true", entry.Data["from"])
	require.Equal(t, "false", entry.Data["to"])

	_, err = h.svc.Reset(ctx, admin, config.VarWorkflowPipelines)
	require.NoError(t, err)
	entry = hook.LastEntry()
	require.Equal(t, "system.config_reset", entry.Data["action"])
	require.Equal(t, "false", entry.Data["from"])
	require.Equal(t, "true", entry.Data["to"])

	hook.Reset()
	_, err = h.svc.Set(ctx, admin, config.VarWorkflowPipelines, "nope")
	require.Error(t, err)
	_, err = h.svc.Set(ctx, admin, "JWT_SECRET", "x")
	require.Error(t, err)
	for _, logged := range hook.AllEntries() {
		require.NotEqual(t, "system.config_updated", logged.Data["action"], "a refused change is not recorded as made")
	}
}

// The list never carries a secret, whatever the environment holds.
func TestInstanceConfigListNeverCarriesSecrets(t *testing.T) {
	cfg := &config.Config{JWTSecret: "jwt-secret-value-that-must-not-leak", DatabaseURL: "postgres://kuayle:dbpass-must-not-leak@db/kuayle"}
	cfg.SMTP.Password = "smtp-password-must-not-leak"
	h := newInstanceConfigHarness(cfg, map[string]string{"JWT_SECRET": cfg.JWTSecret, "KUAYLE_BACKUP_PASSPHRASE": "passphrase-must-not-leak"})
	entries, err := h.svc.List(context.Background())
	require.NoError(t, err)
	raw, err := json.Marshal(entries)
	require.NoError(t, err)
	for _, leaked := range []string{"jwt-secret-value", "dbpass", "smtp-password", "passphrase-must"} {
		require.NotContains(t, string(raw), leaked)
	}
	jwt := entryNamed(t, entries, "JWT_SECRET")
	require.True(t, jwt.Set)
	require.Equal(t, config.SourceEnvironment, jwt.Source)
	require.True(t, entryNamed(t, entries, "KUAYLE_BACKUP_PASSPHRASE").Set)
}

// The pipeline switch of the task service follows the instance setting, so
// saving and starting pipelines stop and resume without a restart.
func TestPipelineSwitchFollowsTheInstanceSetting(t *testing.T) {
	h := newInstanceConfigHarness(&config.Config{WorkflowPipelines: true}, nil)
	tasks := &AgentTaskService{}
	tasks.SetPipelineSwitch(h.svc.PipelinesEnabled)
	pipeline := []byte(`{"kind":"pipeline"}`)
	require.NoError(t, tasks.pipelineRefusal(pipeline))

	admin := uuid.New()
	_, err := h.svc.Set(context.Background(), admin, config.VarWorkflowPipelines, "false")
	require.NoError(t, err)
	require.False(t, tasks.PipelinesEnabled())
	require.ErrorIs(t, tasks.pipelineRefusal(pipeline), ErrPipelinesDisabled)

	_, err = h.svc.Reset(context.Background(), admin, config.VarWorkflowPipelines)
	require.NoError(t, err)
	require.True(t, tasks.PipelinesEnabled())
	require.NoError(t, tasks.pipelineRefusal(pipeline))
}
