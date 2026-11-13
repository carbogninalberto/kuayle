package config

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSysadmins(t *testing.T) {
	id := uuid.New()
	ids, err := parseSysadmins("  " + id.String() + " , " + id.String() + ",")

	require.NoError(t, err)
	assert.Contains(t, ids, id)
	assert.Len(t, ids, 1)
}

func TestParseSysadminsRejectsInvalidID(t *testing.T) {
	_, err := parseSysadmins("not-a-uuid")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "SYSADMINS")
}

func TestIsSysAdmin(t *testing.T) {
	id := uuid.New()
	cfg := &Config{sysadminIDs: map[uuid.UUID]struct{}{id: {}}}

	assert.True(t, cfg.IsSysAdmin(id))
	assert.False(t, cfg.IsSysAdmin(uuid.New()))
	assert.False(t, cfg.IsSysAdmin(uuid.Nil))
}

func TestDevMachineConfigRequiresSeparateRegistrableDomain(t *testing.T) {
	config := DevMachineConfig{
		Enabled: true, Domain: "machines.example.com", EncryptionKey: strings.Repeat("x", 32),
		TicketTTLSeconds: 60, SessionTTLMinutes: 60, IngestURL: "https://app.example.com/api/dev-machine-ingest",
	}

	err := config.Validate("https://app.example.com")

	require.ErrorContains(t, err, "separate registrable domain")
	config.Domain = "machines.example.net"
	require.NoError(t, config.Validate("https://app.example.com"))
}

func TestDevMachineConfigAllowsLocalhostDevelopment(t *testing.T) {
	config := DevMachineConfig{
		Enabled: true, Domain: "machines.localhost", EncryptionKey: strings.Repeat("x", 32),
		TicketTTLSeconds: 60, SessionTTLMinutes: 60, IngestURL: "https://localhost/api/dev-machine-ingest",
	}

	require.NoError(t, config.Validate("https://localhost"))
}

func TestGatewayValidationDoesNotRequireControlPlaneSecrets(t *testing.T) {
	config := DevMachineConfig{Enabled: true, Domain: "machines.example.net", TicketTTLSeconds: 60, SessionTTLMinutes: 60}

	require.NoError(t, config.ValidateGateway("https://app.example.com"))
}

func TestLoadMachineGatewayDoesNotRequireJWTOrRedis(t *testing.T) {
	setMachineGatewayEnv(t)
	t.Setenv("DATABASE_URL", "")
	t.Setenv("DEV_MACHINE_GATEWAY_DATABASE_URL", "postgres://gateway-db/kuayle?sslmode=disable")
	t.Setenv("JWT_SECRET", "")
	t.Setenv("REDIS_URL", "")

	cfg, err := LoadMachineGateway()

	require.NoError(t, err)
	require.Equal(t, "postgres://gateway-db/kuayle?sslmode=disable", cfg.DatabaseURL)
	require.Empty(t, cfg.JWTSecret)
	require.Empty(t, cfg.RedisURL)
	require.Equal(t, "https://app.example.com", cfg.FrontendURL)
	require.Equal(t, "machines.example.net", cfg.DevMachine.Domain)
}

func TestLoadMachineGatewayRejectsMissingAndInvalidGatewayValues(t *testing.T) {
	t.Run("missing database", func(t *testing.T) {
		setMachineGatewayEnv(t)
		t.Setenv("DATABASE_URL", "")
		t.Setenv("DEV_MACHINE_GATEWAY_DATABASE_URL", "")

		_, err := LoadMachineGateway()

		require.ErrorContains(t, err, "DATABASE_URL or DEV_MACHINE_GATEWAY_DATABASE_URL")
	})

	t.Run("missing frontend", func(t *testing.T) {
		setMachineGatewayEnv(t)
		t.Setenv("FRONTEND_URL", "")

		_, err := LoadMachineGateway()

		require.ErrorContains(t, err, "FRONTEND_URL")
	})

	t.Run("disabled dev machines", func(t *testing.T) {
		setMachineGatewayEnv(t)
		t.Setenv("DEV_MACHINES_ENABLED", "false")

		_, err := LoadMachineGateway()

		require.ErrorContains(t, err, "DEV_MACHINES_ENABLED")
	})

	t.Run("same registrable domain", func(t *testing.T) {
		setMachineGatewayEnv(t)
		t.Setenv("DEV_MACHINE_DOMAIN", "machines.example.com")

		_, err := LoadMachineGateway()

		require.ErrorContains(t, err, "separate registrable domain")
	})

	t.Run("invalid session ttl", func(t *testing.T) {
		setMachineGatewayEnv(t)
		t.Setenv("DEV_MACHINE_SESSION_TTL_MINUTES", "1")

		_, err := LoadMachineGateway()

		require.ErrorContains(t, err, "DEV_MACHINE_SESSION_TTL_MINUTES")
	})

	t.Run("production requires separate gateway credential", func(t *testing.T) {
		setMachineGatewayEnv(t)
		t.Setenv("ENVIRONMENT", "production")
		t.Setenv("DEV_MACHINE_GATEWAY_DATABASE_URL", "")

		_, err := LoadMachineGateway()

		require.ErrorContains(t, err, "DEV_MACHINE_GATEWAY_DATABASE_URL is required")
	})

	t.Run("production rejects application database user", func(t *testing.T) {
		setMachineGatewayEnv(t)
		t.Setenv("ENVIRONMENT", "production")
		t.Setenv("DEV_MACHINE_GATEWAY_DATABASE_URL", "postgres://postgres:gateway@postgres/kuayle?sslmode=disable")

		_, err := LoadMachineGateway()

		require.ErrorContains(t, err, "distinct from DATABASE_URL")
	})

	t.Run("production rejects malformed gateway URL", func(t *testing.T) {
		setMachineGatewayEnv(t)
		t.Setenv("ENVIRONMENT", "production")
		t.Setenv("DATABASE_URL", "")
		t.Setenv("DEV_MACHINE_GATEWAY_DATABASE_URL", "postgres://postgres/kuayle")

		_, err := LoadMachineGateway()

		require.ErrorContains(t, err, "PostgreSQL URL with a username")
	})

	t.Run("production accepts restricted gateway credential", func(t *testing.T) {
		setMachineGatewayEnv(t)
		t.Setenv("ENVIRONMENT", "production")
		t.Setenv("DEV_MACHINE_GATEWAY_DATABASE_URL", "postgres://kuayle_gateway:gateway@postgres/kuayle?sslmode=disable")

		cfg, err := LoadMachineGateway()

		require.NoError(t, err)
		require.Equal(t, "postgres://kuayle_gateway:gateway@postgres/kuayle?sslmode=disable", cfg.DatabaseURL)
	})
}

func setMachineGatewayEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://postgres@postgres/kuayle?sslmode=disable")
	t.Setenv("DEV_MACHINE_GATEWAY_DATABASE_URL", "")
	t.Setenv("FRONTEND_URL", "https://app.example.com")
	t.Setenv("DEV_MACHINES_ENABLED", "true")
	t.Setenv("DEV_MACHINE_DOMAIN", "machines.example.net")
	t.Setenv("DEV_MACHINE_ACCESS_TICKET_TTL_SECONDS", "60")
	t.Setenv("DEV_MACHINE_SESSION_TTL_MINUTES", "480")
}

func TestDemoDevMachineAllowed(t *testing.T) {
	sysAdminID := uuid.New()
	normalUserID := uuid.New()

	t.Run("off when demo mode is disabled", func(t *testing.T) {
		cfg := &Config{PublicDemoMode: false, sysadminIDs: map[uuid.UUID]struct{}{}}
		assert.True(t, cfg.DemoDevMachineAllowed(normalUserID))
		assert.True(t, cfg.DemoDevMachineAllowed(sysAdminID))
	})

	t.Run("nil config always returns true", func(t *testing.T) {
		var cfg *Config
		assert.True(t, cfg.DemoDevMachineAllowed(normalUserID))
	})

	t.Run("sysadmin allowed when demo mode on", func(t *testing.T) {
		cfg := &Config{PublicDemoMode: true, sysadminIDs: map[uuid.UUID]struct{}{sysAdminID: {}}}
		assert.True(t, cfg.DemoDevMachineAllowed(sysAdminID))
		assert.False(t, cfg.DemoDevMachineAllowed(normalUserID))
	})

	t.Run("fail closed when SYSADMINS is empty in demo mode", func(t *testing.T) {
		cfg := &Config{PublicDemoMode: true, sysadminIDs: map[uuid.UUID]struct{}{}}
		assert.False(t, cfg.DemoDevMachineAllowed(sysAdminID))
		assert.False(t, cfg.DemoDevMachineAllowed(normalUserID))
	})
}

func TestLoadMachineGatewayReadsDemoConfig(t *testing.T) {
	setMachineGatewayEnv(t)
	t.Setenv("DATABASE_URL", "postgres://postgres/kuayle?sslmode=disable")
	sysAdminID := uuid.New()
	t.Setenv("SYSADMINS", sysAdminID.String())
	t.Setenv("PUBLIC_DEMO_MODE", "true")

	cfg, err := LoadMachineGateway()

	require.NoError(t, err)
	assert.True(t, cfg.PublicDemoMode)
	assert.True(t, cfg.IsSysAdmin(sysAdminID))
	assert.False(t, cfg.IsSysAdmin(uuid.New()))
}

func TestDevMachineDiskQuotaModeDefaultsToHardAndValidatesMonitoredFloor(t *testing.T) {
	config := DevMachineConfig{
		Enabled: true, Domain: "machines.example.net", EncryptionKey: strings.Repeat("x", 32),
		TicketTTLSeconds: 60, SessionTTLMinutes: 60, IngestURL: "https://app.example.com/api/dev-machine-ingest",
	}
	require.NoError(t, config.Validate("https://app.example.com"))
	require.Equal(t, DiskQuotaModeHard, config.EffectiveDiskQuotaMode())
	require.False(t, config.MonitoredDiskQuota())

	config.DiskQuotaMode = "soft"
	require.ErrorContains(t, config.Validate("https://app.example.com"), "DEV_MACHINE_DISK_QUOTA_MODE must be hard or monitored")

	config.DiskQuotaMode = "Monitored"
	config.HostDiskMinFreePercent, config.HostDiskMinFreeGB = 15, 25
	require.NoError(t, config.Validate("https://app.example.com"))
	require.Equal(t, DiskQuotaModeMonitored, config.EffectiveDiskQuotaMode())
	require.True(t, config.MonitoredDiskQuota())

	config.HostDiskMinFreePercent = 0
	require.ErrorContains(t, config.Validate("https://app.example.com"), "DEV_MACHINE_HOST_DISK_MIN_FREE_PERCENT must be between 1 and 90")
	config.HostDiskMinFreePercent = 91
	require.ErrorContains(t, config.Validate("https://app.example.com"), "DEV_MACHINE_HOST_DISK_MIN_FREE_PERCENT must be between 1 and 90")
	config.HostDiskMinFreePercent = 15
	config.HostDiskMinFreeGB = 0
	require.ErrorContains(t, config.Validate("https://app.example.com"), "DEV_MACHINE_HOST_DISK_MIN_FREE_GB must be at least 1")
}

func TestLoadReadsDiskQuotaModeDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/kuayle")
	t.Setenv("REDIS_URL", "redis://localhost:6379")
	t.Setenv("JWT_SECRET", strings.Repeat("j", 32))
	t.Setenv("DEV_MACHINE_DISK_QUOTA_MODE", "")
	config, err := Load()
	require.NoError(t, err)
	require.Equal(t, DiskQuotaModeHard, config.DevMachine.EffectiveDiskQuotaMode())
	require.Equal(t, 15, config.DevMachine.HostDiskMinFreePercent)
	require.Equal(t, 25, config.DevMachine.HostDiskMinFreeGB)
}

func TestDevMachineHostDiskPathMustBeAbsoluteInMonitoredMode(t *testing.T) {
	config := DevMachineConfig{
		Enabled: true, Domain: "machines.example.net", EncryptionKey: strings.Repeat("x", 32),
		TicketTTLSeconds: 60, SessionTTLMinutes: 60, IngestURL: "https://app.example.com/api/dev-machine-ingest",
		DiskQuotaMode: DiskQuotaModeMonitored, HostDiskMinFreePercent: 15, HostDiskMinFreeGB: 25,
	}
	require.NoError(t, config.Validate("https://app.example.com"))
	config.HostDiskPath = "/var/lib/kuayle-host-docker"
	require.NoError(t, config.Validate("https://app.example.com"))
	config.HostDiskPath = "relative/path"
	require.ErrorContains(t, config.Validate("https://app.example.com"), "DEV_MACHINE_HOST_DISK_PATH must be an absolute path")
}

func TestSameSiteMachinesRequireExplicitHTTPSOptIn(t *testing.T) {
	c := DevMachineConfig{Domain: "app.example.com", TicketTTLSeconds: 60, SessionTTLMinutes: 60}
	require.Error(t, c.ValidateGateway("https://app.example.com"))
	c.AllowSameSite = true
	require.NoError(t, c.ValidateGateway("https://app.example.com"))
	require.Error(t, c.ValidateGateway("http://app.example.com"))
	require.Error(t, c.ValidateGateway("https://app.example.com?"))
	c.Domain = "APP.EXAMPLE.COM."
	require.NoError(t, c.ValidateGateway("https://app.example.com"))
	c.Domain = "com"
	require.Error(t, c.ValidateGateway("https://app.example.com"))
	c.Domain = "example.com"
	require.Error(t, c.ValidateGateway("https://abcdefgh1234-ide.example.com"), "frontend must not overlap workload hosts")
}

func TestDevMachineAgentGitIdentityRejectsHeaderInjection(t *testing.T) {
	config := DevMachineConfig{
		Enabled: true, Domain: "machines.example.net", EncryptionKey: strings.Repeat("x", 32),
		TicketTTLSeconds: 60, SessionTTLMinutes: 60, IngestURL: "https://app.example.com/api/dev-machine-ingest",
		AgentGitName: "carbninai", AgentGitEmail: "agent@example.invalid",
	}
	require.NoError(t, config.Validate("https://app.example.com"))
	config.AgentGitName = "agent\nSigned-off-by: someone"
	require.ErrorContains(t, config.Validate("https://app.example.com"), "DEV_MACHINE_AGENT_GIT_NAME")
	config.AgentGitName, config.AgentGitEmail = "agent", "a@b> <c@d"
	require.ErrorContains(t, config.Validate("https://app.example.com"), "DEV_MACHINE_AGENT_GIT_EMAIL")
}

func TestLoadValidatesTheSearXNGURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://db/kuayle?sslmode=disable")
	t.Setenv("REDIS_URL", "redis://redis:6379")
	t.Setenv("JWT_SECRET", "01234567890123456789012345678901")
	t.Setenv("DEV_MACHINES_ENABLED", "false")
	t.Setenv("WEB_SEARCH_SEARXNG_URL", "")
	cfg, err := Load()
	if err != nil || cfg.SearXNGURL() != nil {
		t.Fatalf("unset SearXNG: %v %v", cfg, err)
	}
	t.Setenv("WEB_SEARCH_SEARXNG_URL", " http://SearXNG:8080/ ")
	cfg, err = Load()
	if err != nil || cfg.SearXNGURL().String() != "http://searxng:8080" {
		t.Fatalf("configured SearXNG: %v", err)
	}
	for _, bad := range []string{"searxng:8080", "http://user:pw@searxng:8080", "http://searxng:8080/../x", "http://searxng:8080?format=json", "gopher://searxng"} {
		t.Setenv("WEB_SEARCH_SEARXNG_URL", bad)
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "WEB_SEARCH_SEARXNG_URL") {
			t.Fatalf("%s: want a WEB_SEARCH_SEARXNG_URL error, got %v", bad, err)
		}
	}
}

func TestLoadValidatesForgeWebhookBaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://db/kuayle?sslmode=disable")
	t.Setenv("REDIS_URL", "redis://redis:6379")
	t.Setenv("JWT_SECRET", "01234567890123456789012345678901")
	t.Setenv("DEV_MACHINES_ENABLED", "false")
	t.Setenv("FORGE_WEBHOOK_BASE_URL", "")
	cfg, err := Load()
	if err != nil || cfg.ForgeWebhookBaseURL != "" {
		t.Fatalf("unset forge webhook base URL: %v %v", cfg, err)
	}
	t.Setenv("FORGE_WEBHOOK_BASE_URL", " https://API.Example.com/kuayle/ ")
	cfg, err = Load()
	if err != nil || cfg.ForgeWebhookBaseURL != "https://api.example.com/kuayle" {
		t.Fatalf("configured forge webhook base URL: %v %v", cfg, err)
	}
	for _, bad := range []string{"api.example.com", "ftp://api.example.com", "https://user:pw@api.example.com", "https://api.example.com?x=1"} {
		t.Setenv("FORGE_WEBHOOK_BASE_URL", bad)
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "FORGE_WEBHOOK_BASE_URL") {
			t.Fatalf("%s: want a FORGE_WEBHOOK_BASE_URL error, got %v", bad, err)
		}
	}
}

// exampleEnvValues reads KEY=value pairs from an example env file, including
// commented-out examples, so the tests follow the placeholders actually shipped.
func exampleEnvValues(t *testing.T, relativePath string) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", filepath.FromSlash(relativePath)))
	if err != nil {
		t.Fatalf("reading %s: %v", relativePath, err)
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "#"))
		key, value, found := strings.Cut(line, "=")
		if !found || key == "" || strings.ContainsAny(key, " \t") {
			continue
		}
		values[key] = strings.TrimSpace(value)
	}
	return values
}

// generatedFixture stands in for an operator-generated 48-character value.
func generatedFixture(seed string) string {
	return strings.Repeat(seed, 48/len(seed))
}

func setProductionLoadEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"SYSTEM_UPDATER_URL", "SYSTEM_UPDATER_TOKEN", "VOICE_URL", "VOICE_INTERNAL_TOKEN",
		"DEV_MACHINE_ENCRYPTION_KEY", "SMTP_PASSWORD", "S3_SECRET_KEY", "WEB_SEARCH_SEARXNG_URL",
		"GITHUB_APP_CLIENT_SECRET", "GITHUB_APP_WEBHOOK_SECRET", "SYSADMINS",
	} {
		t.Setenv(name, "")
	}
	t.Setenv("ENVIRONMENT", "production")
	t.Setenv("DATABASE_URL", "postgres://kuayle:"+generatedFixture("dbfixture-12")+"@postgres:5432/kuayle?sslmode=disable")
	t.Setenv("REDIS_URL", "redis://redis:6379")
	t.Setenv("JWT_SECRET", generatedFixture("jwtfixture-0"))
	t.Setenv("FRONTEND_URL", "https://app.example.com")
	t.Setenv("DEV_MACHINES_ENABLED", "false")
	t.Setenv("DEV_MACHINE_ALLOW_SAME_SITE", "false")
}

func databaseURLWithPassword(password string) string {
	return (&url.URL{Scheme: "postgres", User: url.UserPassword("kuayle", password), Host: "postgres:5432", Path: "/kuayle", RawQuery: "sslmode=disable"}).String()
}

func TestLoadAcceptsGeneratedSecretsInProduction(t *testing.T) {
	setProductionLoadEnv(t)
	t.Setenv("SYSTEM_UPDATER_TOKEN", generatedFixture("updaterfix-0"))
	t.Setenv("VOICE_URL", "http://voice:8000")
	t.Setenv("VOICE_INTERNAL_TOKEN", generatedFixture("voicefixt-00"))
	t.Setenv("DEV_MACHINE_ENCRYPTION_KEY", generatedFixture("machinekey-0"))
	if _, err := Load(); err != nil {
		t.Fatalf("Load rejected generated production values: %v", err)
	}
}

func TestLoadRejectsPlaceholderSecretsOutsideDevelopment(t *testing.T) {
	selfhost := exampleEnvValues(t, "selfhosting/.env.example")
	development := exampleEnvValues(t, ".env.example")
	if selfhost["POSTGRES_PASSWORD"] == "" {
		t.Fatal("the self-hosting example no longer provides POSTGRES_PASSWORD; update this test")
	}

	cases := []struct {
		name     string
		variable string // set to value
		value    string
		reported string // variable the error must name
	}{
		{"self-hosting JWT placeholder", "JWT_SECRET", selfhost["JWT_SECRET"], "JWT_SECRET"},
		{"self-hosting database password placeholder", "DATABASE_URL", databaseURLWithPassword(selfhost["POSTGRES_PASSWORD"]), "DATABASE_URL"},
		{"self-hosting updater token placeholder", "SYSTEM_UPDATER_TOKEN", selfhost["SYSTEM_UPDATER_TOKEN"], "SYSTEM_UPDATER_TOKEN"},
		{"development JWT default", "JWT_SECRET", development["JWT_SECRET"], "JWT_SECRET"},
		{"development encryption key default", "DEV_MACHINE_ENCRYPTION_KEY", development["DEV_MACHINE_ENCRYPTION_KEY"], "DEV_MACHINE_ENCRYPTION_KEY"},
		{"development voice token default", "VOICE_INTERNAL_TOKEN", development["VOICE_INTERNAL_TOKEN"], "VOICE_INTERNAL_TOKEN"},
		{"documentation angle-bracket placeholder", "JWT_SECRET", "<" + strings.Repeat("random-string-", 3) + ">", "JWT_SECRET"},
		{"placeholder SMTP password", "SMTP_PASSWORD", "CHANGE_ME", "SMTP_PASSWORD"},
		{"placeholder S3 secret", "S3_SECRET_KEY", "change_me_later", "S3_SECRET_KEY"},
	}
	for _, tc := range cases {
		if tc.value == "" {
			t.Fatalf("%s: the example file no longer provides a value; update this test", tc.name)
		}
		for _, environment := range []string{"production", " Production ", "staging"} {
			t.Run(tc.name+" in "+strings.TrimSpace(environment), func(t *testing.T) {
				setProductionLoadEnv(t)
				t.Setenv("ENVIRONMENT", environment)
				t.Setenv(tc.variable, tc.value)
				_, err := Load()
				if err == nil {
					t.Fatalf("Load accepted a placeholder %s", tc.variable)
				}
				if !strings.Contains(err.Error(), tc.reported) || !strings.Contains(err.Error(), "placeholder") {
					t.Fatalf("error does not name %s as a placeholder: %v", tc.reported, err)
				}
				for _, secret := range []string{tc.value, selfhost["POSTGRES_PASSWORD"]} {
					if strings.Contains(err.Error(), secret) {
						t.Fatalf("error leaks the configured value: %v", err)
					}
				}
			})
		}
		t.Run(tc.name+" in development", func(t *testing.T) {
			setProductionLoadEnv(t)
			t.Setenv("ENVIRONMENT", "development")
			t.Setenv(tc.variable, tc.value)
			if _, err := Load(); err != nil {
				t.Fatalf("development must keep accepting example values: %v", err)
			}
		})
	}
}

func TestLoadMachineGatewayRejectsPlaceholderDatabasePasswordInProduction(t *testing.T) {
	selfhost := exampleEnvValues(t, "selfhosting/.env.example")
	development := exampleEnvValues(t, ".env.example")
	gatewayURL := func(password string) string {
		return (&url.URL{Scheme: "postgres", User: url.UserPassword("kuayle_gateway", password), Host: "postgres:5432", Path: "/kuayle"}).String()
	}
	for name, password := range map[string]string{
		"self-hosting placeholder": selfhost["DEV_MACHINE_GATEWAY_DB_PASSWORD"],
		"development default":      development["DEV_MACHINE_GATEWAY_DB_PASSWORD"],
	} {
		if password == "" {
			t.Fatalf("%s: the example file no longer provides a value; update this test", name)
		}
		t.Run(name, func(t *testing.T) {
			setMachineGatewayEnv(t)
			t.Setenv("DATABASE_URL", "")
			t.Setenv("ENVIRONMENT", "production")
			t.Setenv("DEV_MACHINE_GATEWAY_DATABASE_URL", gatewayURL(password))
			_, err := LoadMachineGateway()
			if err == nil {
				t.Fatal("LoadMachineGateway accepted a placeholder database password")
			}
			if !strings.Contains(err.Error(), "DEV_MACHINE_GATEWAY_DATABASE_URL") || !strings.Contains(err.Error(), "placeholder") {
				t.Fatalf("error does not name the variable as a placeholder: %v", err)
			}
			if strings.Contains(err.Error(), password) {
				t.Fatalf("error leaks the configured value: %v", err)
			}

			t.Setenv("ENVIRONMENT", "development")
			if _, err := LoadMachineGateway(); err != nil {
				t.Fatalf("development must keep accepting example values: %v", err)
			}
		})
	}
	t.Run("generated password", func(t *testing.T) {
		setMachineGatewayEnv(t)
		t.Setenv("DATABASE_URL", "")
		t.Setenv("ENVIRONMENT", "production")
		t.Setenv("DEV_MACHINE_GATEWAY_DATABASE_URL", gatewayURL(generatedFixture("gatewayfix-0")))
		if _, err := LoadMachineGateway(); err != nil {
			t.Fatalf("LoadMachineGateway rejected a generated password: %v", err)
		}
	})
}

func TestDatabaseURLPasswordHandlesUnencodedPasswords(t *testing.T) {
	withPassword := func(password string) string {
		return "postgres://kuayle" + ":" + password + "@db:5432/kuayle"
	}
	assert.Equal(t, "", databaseURLPassword("postgres://postgres@db/kuayle"))
	assert.Equal(t, "", databaseURLPassword(""))
	assert.Equal(t, "fixture@value here", databaseURLPassword(withPassword("fixture%40value%20here")))
	// Compose interpolates POSTGRES_PASSWORD verbatim, so it may not be a valid URL.
	invalid := withPassword("CHANGE_ME%zz")
	assert.Equal(t, "CHANGE_ME%zz", databaseURLPassword(invalid))
	assert.True(t, isPlaceholderSecret(databaseURLPassword(invalid)))
}

// shippedComposeFallback returns the password a Compose file uses
// when POSTGRES_PASSWORD is not set. It is published in the repository, so it
// is as public as the example placeholders.
func shippedComposeFallback(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", path))
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	const marker = "POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:-"
	_, rest, found := strings.Cut(string(raw), marker)
	if !found {
		t.Fatalf("%s no longer defines a fallback database password; update this test", path)
	}
	fallback, _, _ := strings.Cut(rest, "}")
	if fallback == "" {
		t.Fatalf("%s has an empty fallback database password; update this test", path)
	}
	return fallback
}

func TestLoadRejectsComposeFallbackDatabasePasswordOutsideDevelopment(t *testing.T) {
	for _, path := range []string{"selfhosting/docker-compose.yml", "docker-compose.yml"} {
		fallback := shippedComposeFallback(t, path)
		for _, environment := range []string{"production", "staging"} {
			t.Run(path+" in "+environment, func(t *testing.T) {
				setProductionLoadEnv(t)
				t.Setenv("ENVIRONMENT", environment)
				t.Setenv("DATABASE_URL", databaseURLWithPassword(fallback))
				_, err := Load()
				if err == nil {
					t.Fatal("Load accepted the Compose fallback database password")
				}
				if !strings.Contains(err.Error(), "POSTGRES_PASSWORD") {
					t.Fatalf("error does not name POSTGRES_PASSWORD: %v", err)
				}
			})
		}
		t.Run(path+" in development", func(t *testing.T) {
			setProductionLoadEnv(t)
			t.Setenv("ENVIRONMENT", "development")
			t.Setenv("DATABASE_URL", databaseURLWithPassword(fallback))
			if _, err := Load(); err != nil {
				t.Fatalf("development must keep accepting the Compose fallback: %v", err)
			}
		})
	}
}

func TestLoadMachineGatewayRejectsComposeFallbackDatabasePasswordInProduction(t *testing.T) {
	fallback := shippedComposeFallback(t, "selfhosting/docker-compose.yml")
	setMachineGatewayEnv(t)
	t.Setenv("ENVIRONMENT", "production")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("DEV_MACHINE_GATEWAY_DATABASE_URL", databaseURLWithPassword(fallback))
	_, err := LoadMachineGateway()
	if err == nil {
		t.Fatal("LoadMachineGateway accepted the Compose fallback database password")
	}
	if !strings.Contains(err.Error(), "DEV_MACHINE_GATEWAY_DATABASE_URL password") {
		t.Fatalf("error does not name the gateway database password: %v", err)
	}
}
