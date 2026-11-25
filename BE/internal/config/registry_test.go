package config

import (
	"encoding/json"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// configEnvFields maps every environment variable Config reads (its envconfig
// tag, including nested structs) to its field.
func configEnvFields(t *testing.T) map[string]reflect.StructField {
	t.Helper()
	fields := map[string]reflect.StructField{}
	var walk func(reflect.Type)
	walk = func(typ reflect.Type) {
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if name := field.Tag.Get("envconfig"); name != "" {
				_, duplicate := fields[name]
				require.False(t, duplicate, "%s is read by two fields", name)
				fields[name] = field
				continue
			}
			if field.IsExported() && field.Type.Kind() == reflect.Struct && field.Type != reflect.TypeOf(time.Time{}) {
				walk(field.Type)
			}
		}
	}
	walk(reflect.TypeOf(Config{}))
	require.NotEmpty(t, fields)
	return fields
}

// A variable added to Config without a registry entry would be invisible on
// Settings -> Instance configuration, so the registry must list every one.
func TestEveryConfigVariableHasARegistryEntry(t *testing.T) {
	fields := configEnvFields(t)
	registered := map[string]Variable{}
	groups := map[string]bool{}
	for _, group := range Groups() {
		groups[group] = true
	}
	for _, variable := range Variables() {
		_, duplicate := registered[variable.Name]
		require.False(t, duplicate, "%s is registered twice", variable.Name)
		registered[variable.Name] = variable
		assert.NotEmpty(t, strings.TrimSpace(variable.Description), "%s needs a description", variable.Name)
		assert.NotContains(t, variable.Description, "\n", "%s: the description is one line", variable.Name)
		assert.True(t, groups[variable.Group], "%s has an unknown group %q", variable.Name, variable.Group)
	}
	for name := range fields {
		_, ok := registered[name]
		assert.True(t, ok, "%s is read from the environment but has no entry in registry.go", name)
	}
	for name, variable := range registered {
		_, ok := fields[name]
		assert.Equal(t, !variable.Standalone, ok, "%s: only a standalone entry may name a variable Config does not read", name)
	}
}

// Whatever its name says it holds, a credential is never returned.
func TestSecretLookingVariablesAreMarkedSecret(t *testing.T) {
	secretName := regexp.MustCompile(`SECRET|PASSWORD|PASSPHRASE|TOKEN|PRIVATE_KEY|_KEY$|_KEY_|ENCRYPTION|USERNAME|CREDENTIAL|DSN|DATABASE_URL|REDIS_URL`)
	secretField := regexp.MustCompile(`(?i)secret|password|passphrase|token|privatekey|encryptionkey|accesskey|keyfile|username|credential|dsn|databaseurl|redisurl`)
	public := map[string]string{
		"VAPID_PUBLIC_KEY": "the public half of the Web Push pair, served at /api/push/vapid-public-key",
	}
	fields := configEnvFields(t)
	for _, variable := range Variables() {
		if secretName.MatchString(variable.Name) && public[variable.Name] == "" {
			assert.True(t, variable.Secret, "%s looks like a secret and must be marked Secret", variable.Name)
		}
		if field, ok := fields[variable.Name]; ok && secretField.MatchString(field.Name) {
			assert.True(t, variable.Secret, "%s (field %s) looks like a secret and must be marked Secret", variable.Name, field.Name)
		}
		if variable.Runtime != nil {
			assert.False(t, variable.Secret, "%s: a secret is never editable at runtime", variable.Name)
		}
	}
}

// A runtime rule matches the field it overrides, and the documented default
// is a value the rule accepts.
func TestRuntimeVariablesMatchTheirFields(t *testing.T) {
	fields := configEnvFields(t)
	runtime := 0
	for _, variable := range Variables() {
		if variable.Runtime == nil {
			continue
		}
		runtime++
		field, ok := fields[variable.Name]
		require.True(t, ok, variable.Name)
		_, kind := formatValue(reflect.New(field.Type).Elem())
		assert.Equal(t, kind, variable.Runtime.Kind, variable.Name)
		normalized, err := variable.Normalize(field.Tag.Get("default"))
		require.NoError(t, err, "%s: the default must be accepted", variable.Name)
		assert.NotEmpty(t, normalized)
	}
	assert.Positive(t, runtime)
}

func TestNormalizeValidatesTypeAndRange(t *testing.T) {
	pipelines, ok := Lookup(VarWorkflowPipelines)
	require.True(t, ok)
	for raw, want := range map[string]string{"true": "true", " FALSE ": "false", "1": "true", "0": "false"} {
		got, err := pipelines.Normalize(raw)
		require.NoError(t, err, raw)
		assert.Equal(t, want, got, raw)
	}
	for _, raw := range []string{"", "yes", "maybe", "2"} {
		_, err := pipelines.Normalize(raw)
		assert.Error(t, err, raw)
	}

	timeout, ok := Lookup(VarBackupTimeout)
	require.True(t, ok)
	got, err := timeout.Normalize("90m")
	require.NoError(t, err)
	assert.Equal(t, "1h30m", got)
	for _, raw := range []string{"", "soon", "-1h", "1s", "1000h"} {
		_, err := timeout.Normalize(raw)
		assert.Error(t, err, raw)
	}

	jwt, ok := Lookup("JWT_SECRET")
	require.True(t, ok)
	_, err = jwt.Normalize(strings.Repeat("s", 40))
	assert.Error(t, err, "a variable without a runtime rule is never accepted")
}

func TestFormatDurationIsCompact(t *testing.T) {
	for d, want := range map[time.Duration]string{
		720 * time.Hour: "720h", 2 * time.Hour: "2h", 90 * time.Minute: "1h30m",
		5 * time.Minute: "5m", 30 * time.Second: "30s", 0: "0s", 1500 * time.Millisecond: "1.5s",
	} {
		assert.Equal(t, want, FormatDuration(d))
	}
}

// fillStrings sets every string field of cfg, nested ones included, to
// value(name) for its environment variable.
func fillStrings(cfg *Config, value func(string) string) {
	var walk func(reflect.Value)
	walk = func(v reflect.Value) {
		for i := 0; i < v.NumField(); i++ {
			field, info := v.Field(i), v.Type().Field(i)
			if !info.IsExported() {
				continue
			}
			if name := info.Tag.Get("envconfig"); name != "" && field.Kind() == reflect.String {
				field.SetString(value(name))
			} else if field.Kind() == reflect.Struct {
				walk(field)
			}
		}
	}
	walk(reflect.ValueOf(cfg).Elem())
}

// Secrets come back only as set or not set, never a character of the value.
func TestDescribeNeverReturnsSecretValues(t *testing.T) {
	cfg := &Config{}
	fillStrings(cfg, func(name string) string { return "SENTINEL" + name + "SECRETMATERIAL" })
	cfg.GitHubApp.AppID = 42
	env := func(name string) (string, bool) { return "SENTINEL" + name + "SECRETMATERIAL", true }

	states := Describe(cfg, env)
	raw, err := json.Marshal(states)
	require.NoError(t, err)
	secrets := 0
	byName := map[string]VariableState{}
	for _, state := range states {
		byName[state.Name] = state
		if state.Secret {
			secrets++
			assert.True(t, state.Set, state.Name)
			assert.Empty(t, state.Value, state.Name)
			assert.Empty(t, state.Default, state.Name)
			assert.NotContains(t, string(raw), "SENTINEL"+state.Name, state.Name)
		}
	}
	assert.GreaterOrEqual(t, secrets, 20)
	assert.Contains(t, byName, "KUAYLE_BACKUP_PASSPHRASE", "standalone secrets are listed too")
	assert.True(t, byName["KUAYLE_BACKUP_PASSPHRASE"].Set)
	assert.Equal(t, "SENTINELSMTP_HOSTSECRETMATERIAL", byName["SMTP_HOST"].Value, "plain values are shown")
	assert.Equal(t, "42", byName["GITHUB_APP_ID"].Value)

	empty := Describe(&Config{}, func(string) (string, bool) { return "", false })
	for _, state := range empty {
		if state.Secret {
			assert.False(t, state.Set, state.Name)
		}
	}
}

// A plain setting that carries credentials in a URL (a user, a password or a
// query string) is withheld as a whole.
func TestDescribeWithholdsURLsThatCarryCredentials(t *testing.T) {
	cfg := &Config{
		VoiceURL:            "http://voice:hunter2@voice:8000",
		SystemUpdaterURL:    "http://updater:8081/?token=abcdefgh",
		WebSearchSearXNGURL: "http://searxng:8080",
		PasskeyRPOrigins:    "https://a.example.com,https://user:pw@b.example.com",
		FrontendURL:         "https://kuayle.example.com",
	}
	states := Describe(cfg, func(string) (string, bool) { return "", false })
	raw, err := json.Marshal(states)
	require.NoError(t, err)
	for _, leaked := range []string{"hunter2", "abcdefgh", "user:pw", "a.example.com"} {
		assert.NotContains(t, string(raw), leaked)
	}
	byName := map[string]VariableState{}
	for _, state := range states {
		byName[state.Name] = state
	}
	for _, name := range []string{"VOICE_URL", "SYSTEM_UPDATER_URL", "PASSKEY_RP_ORIGINS"} {
		assert.True(t, byName[name].Withheld, name)
		assert.True(t, byName[name].Set, name)
		assert.Empty(t, byName[name].Value, name)
	}
	assert.Equal(t, "http://searxng:8080", byName["WEB_SEARCH_SEARXNG_URL"].Value)
	assert.Equal(t, "https://kuayle.example.com", byName["FRONTEND_URL"].Value)
}

func TestDescribeReportsSourceAndDefault(t *testing.T) {
	cfg := &Config{BackupTimeout: 2 * time.Hour, IssueDeleteGracePeriod: 48 * time.Hour, WorkflowPipelines: true}
	env := map[string]string{"ISSUE_DELETE_GRACE_PERIOD": "48h", "SMTP_HOST": "  "}
	states := Describe(cfg, func(name string) (string, bool) {
		value, ok := env[name]
		return value, ok
	})
	byName := map[string]VariableState{}
	for _, state := range states {
		byName[state.Name] = state
	}
	assert.Equal(t, SourceEnvironment, byName["ISSUE_DELETE_GRACE_PERIOD"].Source)
	assert.Equal(t, "48h", byName["ISSUE_DELETE_GRACE_PERIOD"].Value)
	assert.Equal(t, "720h", byName["ISSUE_DELETE_GRACE_PERIOD"].Default)
	assert.Equal(t, KindDuration, byName["ISSUE_DELETE_GRACE_PERIOD"].Kind)
	assert.Equal(t, SourceDefault, byName["BACKUP_TIMEOUT"].Source)
	assert.Equal(t, "2h", byName["BACKUP_TIMEOUT"].Value)
	assert.Equal(t, SourceDefault, byName["SMTP_HOST"].Source, "a blank variable is not a value")
	assert.False(t, byName["SMTP_HOST"].Set)
	assert.Equal(t, "true", byName["WORKFLOW_PIPELINES_ENABLED"].Value)
	assert.Equal(t, KindBool, byName["WORKFLOW_PIPELINES_ENABLED"].Kind)
	assert.NotNil(t, byName["WORKFLOW_PIPELINES_ENABLED"].Runtime)
	assert.Nil(t, byName["JWT_SECRET"].Runtime)
}
