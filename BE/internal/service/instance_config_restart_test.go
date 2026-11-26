package service

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/config"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/instanceconfigtest"
	"github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const restartSecret = "SENTINELJWTSECRETSENTINELJWTSECRET00"

func restartBaseConfig() *config.Config {
	return &config.Config{
		JWTSecret: restartSecret, IssueDeleteGracePeriod: 720 * time.Hour, PasskeysEnabled: true,
		PasskeyRPDisplayName: "Kuayle", SSOInstanceDefaultRole: "member", RestartSupervised: true,
	}
}

func newRestartForTest(t *testing.T, store *instanceconfigtest.RestartStore, supervised bool) (*InstanceRestart, *atomic.Int32) {
	t.Helper()
	base := restartBaseConfig()
	startup := ResolveStartup(context.Background(), store, base, true, time.Now, logrus.New())
	var exits atomic.Int32
	restart := NewInstanceRestart(store, startup, supervised, func() { exits.Add(1) })
	restart.exitDelay = 0
	return restart, &exits
}

func proposal(pairs ...string) map[string]*string {
	out := map[string]*string{}
	for i := 0; i < len(pairs); i += 2 {
		if pairs[i+1] == "<revert>" {
			out[pairs[i]] = nil
			continue
		}
		value := pairs[i+1]
		out[pairs[i]] = &value
	}
	return out
}

func issuesByName(plan *RestartPlan) map[string]string {
	out := map[string]string{}
	for _, issue := range plan.Issues {
		out[issue.Name] = issue.Message
	}
	return out
}

func TestPlanValidatesWithoutStoringAnything(t *testing.T) {
	store := instanceconfigtest.NewRestartStore()
	restart, exits := newRestartForTest(t, store, true)

	plan, err := restart.Plan(context.Background(), proposal(
		"SMTP_PORT", "70000", // range
		"SSO_OIDC_DEFAULT_ROLE", "owner", // enum
		"S3_CDN_BASE_URL", "ftp://cdn.example.com", // URL format
		"ISSUE_DELETE_GRACE_PERIOD", "soon", // type
		"JWT_SECRET", "x", // secret
		"DATABASE_URL", "postgres://localhost/kuayle", // connection string
		"OIDC_ALLOW_PRIVATE_NETWORK", "true", // SSRF guard
		"WORKFLOW_PIPELINES_ENABLED", "false", // takes effect without a restart
		"NOT_A_SETTING", "1",
	))
	require.NoError(t, err)
	assert.False(t, plan.Valid())
	issues := issuesByName(plan)
	assert.Len(t, issues, 9)
	assert.Contains(t, issues["SMTP_PORT"], "between 1 and 65535")
	assert.Contains(t, issues["SSO_OIDC_DEFAULT_ROLE"], "member, guest")
	assert.Contains(t, issues["S3_CDN_BASE_URL"], "http")
	assert.Contains(t, issues["ISSUE_DELETE_GRACE_PERIOD"], "duration")
	for _, forbidden := range []string{"JWT_SECRET", "DATABASE_URL", "OIDC_ALLOW_PRIVATE_NETWORK"} {
		assert.Contains(t, issues[forbidden], "cannot be changed from Settings", forbidden)
	}
	assert.Contains(t, issues["WORKFLOW_PIPELINES_ENABLED"], "without a restart")
	assert.Contains(t, issues["NOT_A_SETTING"], "not a setting")
	assert.Empty(t, plan.Changes)
	assert.Empty(t, store.Overrides, "a dry run stores nothing")
	assert.Empty(t, store.Runs)
	assert.Zero(t, exits.Load())
}

func TestPlanChecksTheWholeConfigurationAndReportsChanges(t *testing.T) {
	store := instanceconfigtest.NewRestartStore()
	restart, _ := newRestartForTest(t, store, true)

	plan, err := restart.Plan(context.Background(), proposal("FORGE_WEBHOOK_BASE_URL", "https://example.com/a/../b"))
	require.NoError(t, err)
	require.Len(t, plan.Issues, 1)
	assert.Empty(t, plan.Issues[0].Name, "a problem of the configuration as a whole names no variable")
	assert.Contains(t, plan.Issues[0].Message, "FORGE_WEBHOOK_BASE_URL")

	plan, err = restart.Plan(context.Background(), proposal(
		"PASSKEYS_ENABLED", "false", "SMTP_PORT", "2525", "ISSUE_DELETE_GRACE_PERIOD", "90m",
		"PASSKEY_RP_DISPLAY_NAME", "Kuayle", // already in effect
		"SMTP_FROM", "<revert>", // nothing to revert
	))
	require.NoError(t, err)
	require.True(t, plan.Valid(), "%v", plan.Issues)
	assert.ElementsMatch(t, []domain.InstanceRestartChange{
		{Name: "PASSKEYS_ENABLED", From: "true", To: "false"},
		{Name: "SMTP_PORT", From: "0", To: "2525"},
		{Name: "ISSUE_DELETE_GRACE_PERIOD", From: "720h", To: "1h30m"},
	}, plan.Changes)
	assert.Empty(t, store.Overrides)
}

func TestApplyStoresOverridesRecordsTheRunAndRestarts(t *testing.T) {
	hook := logtest.NewGlobal()
	defer hook.Reset()
	store := instanceconfigtest.NewRestartStore()
	restart, exits := newRestartForTest(t, store, true)
	admin := uuid.New()

	run, err := restart.Apply(context.Background(), admin, proposal("PASSKEYS_ENABLED", "false", "SMTP_HOST", "Mail.Example.com"))
	require.NoError(t, err)
	assert.Equal(t, domain.RestartRunRestarting, run.Status)
	require.NotNil(t, run.RequestedBy)
	assert.Equal(t, admin, *run.RequestedBy)
	assert.Equal(t, map[string]string{"PASSKEYS_ENABLED": "false", "SMTP_HOST": "mail.example.com"}, store.Overrides)
	assert.ElementsMatch(t, []domain.InstanceRestartChange{
		{Name: "PASSKEYS_ENABLED", From: "true", To: "false"},
		{Name: "SMTP_HOST", From: "", To: "mail.example.com"},
	}, run.Changes)
	assert.Nil(t, run.Previous["PASSKEYS_ENABLED"], "no override before")
	require.Eventually(t, func() bool { return exits.Load() == 1 }, time.Second, time.Millisecond, "the API exits to be restarted")

	// Audited: who, when, which variables, old -> new; never a secret.
	var found bool
	for _, entry := range hook.AllEntries() {
		assert.NotContains(t, entry.Message, restartSecret)
		for _, value := range entry.Data {
			assert.NotContains(t, fmt.Sprint(value), restartSecret)
		}
		if entry.Data["action"] != "system.config_restart_requested" {
			continue
		}
		found = true
		assert.Equal(t, true, entry.Data["audit"])
		assert.Equal(t, admin.String(), entry.Data["actor_id"])
		assert.Equal(t, run.ID, entry.Data["run_id"])
		assert.NotNil(t, entry.Data["requested_at"])
		changes, ok := entry.Data["changes"].([]map[string]string)
		require.True(t, ok)
		assert.Contains(t, changes, map[string]string{"variable": "PASSKEYS_ENABLED", "from": "true", "to": "false"})
	}
	assert.True(t, found, "an audit event is logged")

	_, err = restart.Apply(context.Background(), admin, proposal("SMTP_PORT", "2525"))
	assert.ErrorIs(t, err, ErrInstanceRestartActive, "one run at a time")
	assert.Len(t, store.Runs, 1)
}

func TestApplyRefusesWhatCannotBeAppliedAndStoresNothing(t *testing.T) {
	store := instanceconfigtest.NewRestartStore()
	restart, exits := newRestartForTest(t, store, false)
	_, err := restart.Apply(context.Background(), uuid.New(), proposal("PASSKEYS_ENABLED", "false"))
	assert.ErrorIs(t, err, ErrInstanceRestartUnsupported, "nothing would start the API again")

	restart, exits = newRestartForTest(t, store, true)
	_, err = restart.Apply(context.Background(), uuid.New(), proposal("SMTP_PORT", "0", "JWT_SECRET", "x"))
	var invalid *RestartInvalidError
	require.ErrorAs(t, err, &invalid)
	assert.Len(t, invalid.Issues, 2)

	_, err = restart.Apply(context.Background(), uuid.New(), proposal("PASSKEYS_ENABLED", "true"))
	assert.ErrorIs(t, err, ErrInstanceRestartNoChanges)

	assert.Empty(t, store.Overrides)
	assert.Empty(t, store.Runs)
	time.Sleep(20 * time.Millisecond)
	assert.Zero(t, exits.Load(), "the API is not stopped for a refused request")
}

func TestApplyCanRevertAnOverrideToTheEnvironment(t *testing.T) {
	store := instanceconfigtest.NewRestartStore()
	store.Overrides["SMTP_PORT"] = "2525"
	restart, _ := newRestartForTest(t, store, true)
	run, err := restart.Apply(context.Background(), uuid.New(), proposal("SMTP_PORT", "<revert>"))
	require.NoError(t, err)
	assert.NotContains(t, store.Overrides, "SMTP_PORT")
	require.NotNil(t, run.Previous["SMTP_PORT"])
	assert.Equal(t, "2525", *run.Previous["SMTP_PORT"])
	assert.Equal(t, []domain.InstanceRestartChange{{Name: "SMTP_PORT", From: "2525", To: "0"}}, run.Changes)
}

func resolve(t *testing.T, store *instanceconfigtest.RestartStore, confirm bool, now time.Time) *StartupResult {
	t.Helper()
	return ResolveStartup(context.Background(), store, restartBaseConfig(), confirm, func() time.Time { return now }, logrus.New())
}

func TestResolveStartupMergesTheStoredOverridesOverTheEnvironment(t *testing.T) {
	store := instanceconfigtest.NewRestartStore()
	store.Overrides = map[string]string{
		"PASSKEYS_ENABLED":           "false",
		"SMTP_PORT":                  "2525",
		"BACKUP_TIMEOUT":             "3h",     // a runtime override: not this feature's business
		"JWT_SECRET":                 "hacked", // never honoured, even if a row exists
		"OIDC_ALLOW_PRIVATE_NETWORK": "true",
	}
	got := resolve(t, store, true, time.Now())
	assert.False(t, got.Config.PasskeysEnabled)
	assert.Equal(t, 2525, got.Config.SMTP.Port)
	assert.Equal(t, restartSecret, got.Config.JWTSecret)
	assert.False(t, got.Config.OIDCAllowPrivateNetwork)
	assert.Equal(t, map[string]string{"PASSKEYS_ENABLED": "false", "SMTP_PORT": "2525"}, got.Applied)
	assert.Nil(t, got.Run)
	assert.True(t, got.Base.PasskeysEnabled, "the environment alone stays available")
}

func TestResolveStartupTracksTheStartsOfAnApply(t *testing.T) {
	store := instanceconfigtest.NewRestartStore()
	restart, _ := newRestartForTest(t, store, true)
	_, err := restart.Apply(context.Background(), uuid.New(), proposal("SMTP_PORT", "2525"))
	require.NoError(t, err)

	first := resolve(t, store, true, time.Now())
	require.NotNil(t, first.Run)
	assert.Equal(t, domain.RestartRunApplying, first.Run.Status)
	assert.Equal(t, 1, first.Run.BootAttempts)
	assert.Equal(t, 2525, first.Config.SMTP.Port)

	// A crash before the health report: the next start counts again.
	second := resolve(t, store, true, time.Now())
	require.NotNil(t, second.Run)
	assert.Equal(t, 2, second.Run.BootAttempts)
	assert.Equal(t, 2525, second.Config.SMTP.Port)
}

func TestResolveStartupRollsBackAnApplyThatKeepsCrashing(t *testing.T) {
	store := instanceconfigtest.NewRestartStore()
	store.Overrides["SMTP_PORT"] = "1025"
	restart, _ := newRestartForTest(t, store, true)
	_, err := restart.Apply(context.Background(), uuid.New(), proposal("SMTP_PORT", "2525", "PASSKEYS_ENABLED", "false"))
	require.NoError(t, err)

	for i := 1; i <= restartMaxBoots; i++ {
		got := resolve(t, store, true, time.Now())
		require.NotNil(t, got.Run, "start %d still tries the new values", i)
		assert.Equal(t, 2525, got.Config.SMTP.Port)
	}
	got := resolve(t, store, true, time.Now())
	assert.Nil(t, got.Run)
	assert.Equal(t, domain.RestartRunRolledBack, store.Status())
	assert.Contains(t, got.Notice, "previous settings were restored")
	assert.Equal(t, 1025, got.Config.SMTP.Port, "the previous override is back")
	assert.True(t, got.Config.PasskeysEnabled, "a variable without a previous override returns to the environment")
	assert.Equal(t, map[string]string{"SMTP_PORT": "1025"}, store.Overrides)
}

func TestResolveStartupRollsBackAnApplyThatIsTooLate(t *testing.T) {
	store := instanceconfigtest.NewRestartStore()
	restart, _ := newRestartForTest(t, store, true)
	_, err := restart.Apply(context.Background(), uuid.New(), proposal("SMTP_PORT", "2525"))
	require.NoError(t, err)
	start := time.Now()
	require.NotNil(t, resolve(t, store, true, start).Run)

	got := resolve(t, store, true, start.Add(restartBootWindow+time.Minute))
	assert.Nil(t, got.Run)
	assert.Zero(t, got.Config.SMTP.Port)
	assert.Equal(t, domain.RestartRunRolledBack, store.Status())
	assert.Empty(t, store.Overrides)
}

func TestResolveStartupRollsBackNewValuesThatMakeAnInvalidConfiguration(t *testing.T) {
	store := instanceconfigtest.NewRestartStore()
	restart, _ := newRestartForTest(t, store, true)
	_, err := restart.Apply(context.Background(), uuid.New(), proposal("SMTP_PORT", "2525"))
	require.NoError(t, err)
	// The environment changed under the stored value: the merge is now invalid.
	base := restartBaseConfig()
	base.JWTSecret = "short"

	got := ResolveStartup(context.Background(), store, base, true, time.Now, logrus.New())
	assert.Nil(t, got.Run)
	assert.Equal(t, domain.RestartRunRolledBack, store.Status())
	assert.Contains(t, got.Notice, "JWT_SECRET")
	assert.Same(t, base, got.Config)
	assert.Empty(t, store.Overrides)
}

func TestResolveStartupFallsBackToTheEnvironmentWhenStoredValuesAreInvalid(t *testing.T) {
	store := instanceconfigtest.NewRestartStore()
	store.Overrides["SSO_OIDC_DEFAULT_ROLE"] = "owner" // edited by hand in the database
	got := resolve(t, store, true, time.Now())
	assert.Equal(t, "member", got.Config.SSOInstanceDefaultRole)
	assert.Empty(t, got.Applied)
	assert.Contains(t, got.Notice, "not used")

	store.FailList = errors.New("database is down")
	got = resolve(t, store, true, time.Now())
	assert.Same(t, got.Base, got.Config, "the server still starts")
}

func TestResolveStartupOfAWorkerNeverChangesARun(t *testing.T) {
	store := instanceconfigtest.NewRestartStore()
	restart, _ := newRestartForTest(t, store, true)
	_, err := restart.Apply(context.Background(), uuid.New(), proposal("SMTP_PORT", "2525"))
	require.NoError(t, err)

	got := resolve(t, store, false, time.Now())
	assert.Equal(t, 2525, got.Config.SMTP.Port)
	assert.Nil(t, got.Run)
	assert.Equal(t, domain.RestartRunRestarting, store.Status())
	assert.Zero(t, store.Runs[0].BootAttempts)
}

func applyingRun(t *testing.T, store *instanceconfigtest.RestartStore) *domain.InstanceRestartRun {
	t.Helper()
	restart, _ := newRestartForTest(t, store, true)
	_, err := restart.Apply(context.Background(), uuid.New(), proposal("SMTP_PORT", "2525"))
	require.NoError(t, err)
	return resolve(t, store, true, time.Now()).Run
}

func TestWatchRestartHealthConfirmsAHealthyStart(t *testing.T) {
	store := instanceconfigtest.NewRestartStore()
	run := applyingRun(t, store)
	var calls int
	probe := func(context.Context) error {
		calls++
		if calls < 3 {
			return errors.New("starting")
		}
		return nil
	}
	var exits atomic.Int32
	WatchRestartHealth(context.Background(), store, run, probe, time.Second, time.Millisecond, func() { exits.Add(1) }, logrus.New())
	assert.Equal(t, domain.RestartRunConfirmed, store.Status())
	assert.Equal(t, map[string]string{"SMTP_PORT": "2525"}, store.Overrides)
	assert.Zero(t, exits.Load())
}

func TestWatchRestartHealthRollsBackAndRestartsWhenTheAPIStaysUnhealthy(t *testing.T) {
	hook := logtest.NewGlobal()
	defer hook.Reset()
	store := instanceconfigtest.NewRestartStore()
	store.Overrides["SMTP_PORT"] = "1025"
	restart, _ := newRestartForTest(t, store, true)
	_, err := restart.Apply(context.Background(), uuid.New(), proposal("SMTP_PORT", "2525"))
	require.NoError(t, err)
	run := resolve(t, store, true, time.Now()).Run

	var exits atomic.Int32
	WatchRestartHealth(context.Background(), store, run, func(context.Context) error { return errors.New("down") },
		30*time.Millisecond, time.Millisecond, func() { exits.Add(1) }, logrus.New())
	assert.Equal(t, domain.RestartRunRolledBack, store.Status())
	assert.Equal(t, map[string]string{"SMTP_PORT": "1025"}, store.Overrides, "the previous values are back")
	assert.Equal(t, int32(1), exits.Load(), "the API exits to start again with them")
	assert.Contains(t, store.Runs[0].Failure, "not healthy within")

	var audited bool
	for _, entry := range hook.AllEntries() {
		audited = audited || entry.Data["action"] == "system.config_restart_rolled_back"
	}
	assert.True(t, audited, "the rollback is audited")
}

func TestWatchRestartHealthStopsWhenTheServerShutsDown(t *testing.T) {
	store := instanceconfigtest.NewRestartStore()
	run := applyingRun(t, store)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var exits atomic.Int32
	WatchRestartHealth(ctx, store, run, func(context.Context) error { return errors.New("down") },
		time.Minute, time.Millisecond, func() { exits.Add(1) }, logrus.New())
	assert.Equal(t, domain.RestartRunApplying, store.Status(), "a shutdown decides nothing; the next start counts")
	assert.Zero(t, exits.Load())
}

func TestInstanceConfigListMarksRestartVariablesEditableAndShowsWhereTheyComeFrom(t *testing.T) {
	store := instanceconfigtest.NewRestartStore()
	store.Overrides["SMTP_PORT"] = "2525"
	base := restartBaseConfig()
	startup := ResolveStartup(context.Background(), store, base, true, time.Now, logrus.New())
	svc := NewInstanceConfigService(startup.Config, &memoryConfigStore{rows: map[string]domain.InstanceConfigOverride{
		"SMTP_PORT": {Name: "SMTP_PORT", Value: "2525", UpdatedAt: time.Now()},
	}})
	svc.lookup = func(string) (string, bool) { return "", false }
	svc.SetRestart(NewInstanceRestart(store, startup, true, func() {}))

	entries, err := svc.List(context.Background())
	require.NoError(t, err)
	byName := map[string]InstanceConfigEntry{}
	for _, entry := range entries {
		byName[entry.Name] = entry
	}
	port := byName["SMTP_PORT"]
	assert.True(t, port.EditableOnRestart)
	assert.Equal(t, "2525", port.Value)
	assert.Equal(t, config.SourceOverride, port.Source)
	assert.NotNil(t, port.UpdatedAt)
	assert.Equal(t, "0", port.BaseValue)
	assert.True(t, byName["PASSKEYS_ENABLED"].EditableOnRestart)
	assert.Equal(t, config.SourceDefault, byName["PASSKEYS_ENABLED"].Source)
	for _, name := range []string{"JWT_SECRET", "DATABASE_URL", "OIDC_ALLOW_PRIVATE_NETWORK", "SYSADMINS", "FRONTEND_URL", "DEV_MACHINE_DOMAIN"} {
		assert.False(t, byName[name].EditableOnRestart, name)
	}
	assert.False(t, byName["BACKUP_TIMEOUT"].EditableOnRestart, "a runtime variable is edited live")
}
