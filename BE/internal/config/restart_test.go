package config

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// What may never be edited from Settings, whatever a later change to the
// registry says: the allowlist is the registry's Restart rules, and these
// are the kinds the issue forbids forever.
func TestForbiddenKindsCanNeverBeEditedFromSettings(t *testing.T) {
	forbidden := []string{
		// secrets, tokens and keys
		"JWT_SECRET", "SYSTEM_UPDATER_TOKEN", "VOICE_INTERNAL_TOKEN", "SMTP_PASSWORD", "SMTP_USERNAME", "S3_SECRET_KEY",
		"S3_ACCESS_KEY", "VAPID_PRIVATE_KEY", "DEV_MACHINE_ENCRYPTION_KEY", "GITHUB_APP_PRIVATE_KEY", "SSO_OIDC_CLIENT_SECRET",
		"KUAYLE_BACKUP_PASSPHRASE", "SAML_SP_KEY_FILE", "GITHUB_WEBHOOK_URL", "TELEGRAM_WEBHOOK_URL",
		// connection strings
		"DATABASE_URL", "REDIS_URL",
		// SSRF guards and internal addresses
		"AI_ALLOW_PRIVATE_NETWORK", "FORGE_ALLOW_PRIVATE_NETWORK", "IMPORT_ALLOW_PRIVATE_NETWORK", "OIDC_ALLOW_PRIVATE_NETWORK",
		"BACKUP_S3_ALLOW_PRIVATE_NETWORK", "DEV_MACHINE_EGRESS_ALLOWLIST", "DEV_MACHINE_EGRESS_DENYLIST", "WEB_SEARCH_SEARXNG_URL",
		"VOICE_URL", "SYSTEM_UPDATER_URL", "DEV_MACHINE_INGEST_URL",
		// instance identity and what other services read
		"SYSADMINS", "ENVIRONMENT", "PORT", "FRONTEND_URL", "PUBLIC_DEMO_MODE", "DEV_MACHINES_ENABLED", "DEV_MACHINE_DOMAIN",
		"STORAGE_TYPE", "BACKUP_DIR", "BACKUP_TIMEOUT",
	}
	for _, name := range forbidden {
		assert.True(t, ForbiddenForRestart(name), name)
	}
	for _, name := range RestartEditableNames() {
		assert.False(t, ForbiddenForRestart(name), "%s is editable but looks forbidden", name)
	}
}

// Every restart rule is on a variable the API alone reads, that is neither a
// secret nor a forbidden kind, whose rule matches its field and accepts its
// default; and nothing forbidden carries one.
func TestRestartRulesAreOnlyOnSafeAPIVariables(t *testing.T) {
	fields := configEnvFields(t)
	editable := 0
	for _, variable := range Variables() {
		if variable.Restart == nil {
			continue
		}
		editable++
		assert.False(t, variable.Secret, "%s: a secret is never editable", variable.Name)
		assert.False(t, ForbiddenForRestart(variable.Name), "%s is a forbidden kind", variable.Name)
		assert.False(t, variable.Standalone, "%s is read outside the API process", variable.Name)
		assert.Nil(t, variable.Runtime, "%s is either a runtime or a restart setting", variable.Name)
		assert.True(t, variable.RestartEditable(), variable.Name)
		field, ok := fields[variable.Name]
		require.True(t, ok, variable.Name)
		_, kind := formatValue(reflect.New(field.Type).Elem())
		assert.Equal(t, kind, variable.Restart.Kind, variable.Name)
		_, err := variable.NormalizeRestart(field.Tag.Get("default"))
		assert.NoError(t, err, "%s: the default must be accepted", variable.Name)
	}
	assert.Positive(t, editable)
	for _, variable := range Variables() {
		if variable.Secret || ForbiddenForRestart(variable.Name) {
			assert.Nil(t, variable.Restart, variable.Name)
			assert.False(t, variable.RestartEditable(), variable.Name)
			_, err := variable.NormalizeRestart("anything")
			assert.Error(t, err, variable.Name)
		}
	}
}

func TestNormalizeRestartChecksTypeRangeEnumAndFormat(t *testing.T) {
	check := func(name string, accepted map[string]string, rejected ...string) {
		t.Helper()
		variable, ok := Lookup(name)
		require.True(t, ok, name)
		for raw, want := range accepted {
			got, err := variable.NormalizeRestart(raw)
			require.NoError(t, err, "%s=%q", name, raw)
			assert.Equal(t, want, got, "%s=%q", name, raw)
		}
		for _, raw := range rejected {
			_, err := variable.NormalizeRestart(raw)
			assert.Error(t, err, "%s=%q must be rejected", name, raw)
		}
	}
	check("PASSKEYS_ENABLED", map[string]string{"false": "false", " TRUE ": "true", "0": "false"}, "", "yes", "2")
	check("SMTP_PORT", map[string]string{"587": "587", " 25 ": "25", "65535": "65535"}, "", "0", "65536", "-1", "smtp", "25.5")
	check("ISSUE_DELETE_GRACE_PERIOD", map[string]string{"90m": "1h30m", "720h": "720h", "8760h": "8760h"}, "", "soon", "30s", "8761h", "-2h")
	check("SSO_OIDC_DEFAULT_ROLE", map[string]string{"Guest": "guest", "member": "member"}, "", "owner", "admin")
	check("S3_CDN_BASE_URL", map[string]string{"": "", "https://CDN.example.com/uploads": "https://cdn.example.com/uploads"},
		"cdn.example.com", "ftp://cdn.example.com", "https://user:pw@cdn.example.com", "https://cdn.example.com?x=1",
		"https://cdn.example.com#frag", "https://", "javascript:alert(1)")
	check("SMTP_HOST", map[string]string{"": "", "SMTP.Example.com": "smtp.example.com", "10.0.0.5": "10.0.0.5"},
		"smtp.example.com:587", "https://smtp.example.com", "bad host", "-bad.example.com", "a..b")
	check("SMTP_FROM", map[string]string{"": "", "noreply@example.com": "<noreply@example.com>"},
		"not an address", "a@b@c", "x\ny@example.com")
	check("PASSKEY_RP_DISPLAY_NAME", map[string]string{"Acme": "Acme"}, "", strings.Repeat("a", 65), "two\nlines")
	check("FORGE_WEBHOOK_BASE_URL", map[string]string{"https://kuayle.example.com": "https://kuayle.example.com"}, "kuayle", "ftp://x.example.com")

	secret, _ := Lookup("JWT_SECRET")
	_, err := secret.NormalizeRestart(strings.Repeat("s", 40))
	assert.Error(t, err)
}

func restartTestBase() *Config {
	return &Config{
		JWTSecret: strings.Repeat("j", 40), IssueDeleteGracePeriod: 720 * time.Hour, PasskeysEnabled: true,
		PasskeyRPDisplayName: "Kuayle", SSOInstanceDefaultRole: "member",
	}
}

func TestWithOverridesLaysValuesOverACopy(t *testing.T) {
	base := restartTestBase()
	next, err := base.WithOverrides(map[string]string{
		"PASSKEYS_ENABLED": "false", "ISSUE_DELETE_GRACE_PERIOD": "48h", "SMTP_HOST": "smtp.example.com", "SMTP_PORT": "2525",
		"S3_PUBLIC": "false", "SSO_OIDC_DEFAULT_ROLE": "guest", "PASSKEY_RP_DISPLAY_NAME": "Acme",
	})
	require.NoError(t, err)
	assert.False(t, next.PasskeysEnabled)
	assert.Equal(t, 48*time.Hour, next.IssueDeleteGracePeriod)
	assert.Equal(t, "smtp.example.com", next.SMTP.Host)
	assert.Equal(t, 2525, next.SMTP.Port)
	assert.False(t, next.Storage.S3Public)
	assert.Equal(t, "guest", next.SSOInstanceDefaultRole)
	assert.Equal(t, "Acme", next.PasskeyRPDisplayName)

	assert.True(t, base.PasskeysEnabled, "the configuration being replaced is not changed")
	assert.Equal(t, 720*time.Hour, base.IssueDeleteGracePeriod)
	assert.Empty(t, base.SMTP.Host)
}

func TestWithOverridesRefusesWhatSettingsMayNotEdit(t *testing.T) {
	for _, name := range []string{"JWT_SECRET", "DATABASE_URL", "REDIS_URL", "SYSADMINS", "OIDC_ALLOW_PRIVATE_NETWORK",
		"DEV_MACHINE_EGRESS_ALLOWLIST", "WEB_SEARCH_SEARXNG_URL", "SMTP_PASSWORD", "WORKFLOW_PIPELINES_ENABLED", "NOT_A_VARIABLE"} {
		base := restartTestBase()
		_, err := base.WithOverrides(map[string]string{name: "x"})
		assert.Error(t, err, name)
		assert.Equal(t, restartTestBase().JWTSecret, base.JWTSecret)
	}
}

// A value can pass its own rule and still break the configuration as a
// whole; the dry run must see it, as the next start would.
func TestWithOverridesAppliesTheWholeConfigurationRules(t *testing.T) {
	_, err := restartTestBase().WithOverrides(map[string]string{"FORGE_WEBHOOK_BASE_URL": "https://example.com/a/../b"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "FORGE_WEBHOOK_BASE_URL")

	got, err := restartTestBase().WithOverrides(map[string]string{"FORGE_WEBHOOK_BASE_URL": "https://hooks.example.com/"})
	require.NoError(t, err)
	assert.Equal(t, "https://hooks.example.com", got.ForgeWebhookBaseURL)
}
