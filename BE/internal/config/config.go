package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/kelseyhightower/envconfig"
	"github.com/kuayle/kuayle-backend/pkg/storage"
	"golang.org/x/net/publicsuffix"
)

// GitHubAppConfig holds optional global GitHub App credentials.
// When set, all workspaces share this app (SaaS mode) instead of creating per-workspace apps.
type GitHubAppConfig struct {
	AppID         int64  `envconfig:"GITHUB_APP_ID"`
	PrivateKey    string `envconfig:"GITHUB_APP_PRIVATE_KEY"` // base64-encoded PEM
	ClientID      string `envconfig:"GITHUB_APP_CLIENT_ID"`
	ClientSecret  string `envconfig:"GITHUB_APP_CLIENT_SECRET"`
	WebhookSecret string `envconfig:"GITHUB_APP_WEBHOOK_SECRET"`
	Slug          string `envconfig:"GITHUB_APP_SLUG"`
}

// IsConfigured returns true if the minimum required fields are set.
func (g GitHubAppConfig) IsConfigured() bool {
	return g.AppID != 0 && g.PrivateKey != "" && g.WebhookSecret != ""
}

type Config struct {
	Port           int    `envconfig:"PORT" default:"8080"`
	DatabaseURL    string `envconfig:"DATABASE_URL" required:"true"`
	RedisURL       string `envconfig:"REDIS_URL" required:"true"`
	JWTSecret      string `envconfig:"JWT_SECRET" required:"true"`
	FrontendURL    string `envconfig:"FRONTEND_URL" default:"http://localhost:5173"`
	Environment    string `envconfig:"ENVIRONMENT" default:"development"`
	Storage        storage.Config
	Sysadmins      string `envconfig:"SYSADMINS"`
	PublicDemoMode bool   `envconfig:"PUBLIC_DEMO_MODE"`

	SystemUpdaterURL   string `envconfig:"SYSTEM_UPDATER_URL"`
	SystemUpdaterToken string `envconfig:"SYSTEM_UPDATER_TOKEN"`

	sysadminIDs map[uuid.UUID]struct{}

	// GitHub webhook URL (optional — for dev with smee.io or private networks)
	// If not set, auto-derived from FRONTEND_URL for public domains, or disabled for localhost.
	GitHubWebhookURL string `envconfig:"GITHUB_WEBHOOK_URL"`

	// Global GitHub App (SaaS mode — shared across all workspaces)
	GitHubApp GitHubAppConfig

	// Dev machine configuration
	DevMachine DevMachineConfig
}

type DevMachineConfig struct {
	Enabled              bool   `envconfig:"DEV_MACHINES_ENABLED" default:"false"`
	Domain               string `envconfig:"DEV_MACHINE_DOMAIN"`
	EncryptionKey        string `envconfig:"DEV_MACHINE_ENCRYPTION_KEY"`
	TicketTTLSeconds     int    `envconfig:"DEV_MACHINE_ACCESS_TICKET_TTL_SECONDS" default:"60"`
	SessionTTLMinutes    int    `envconfig:"DEV_MACHINE_SESSION_TTL_MINUTES" default:"480"`
	GatewayContainerName string `envconfig:"DEV_MACHINE_GATEWAY_CONTAINER" default:"kuayle-machine-gateway"`
	IngestURL            string `envconfig:"DEV_MACHINE_INGEST_URL"`
	EgressAllowlist      string `envconfig:"DEV_MACHINE_EGRESS_ALLOWLIST"`
	EgressDenylist       string `envconfig:"DEV_MACHINE_EGRESS_DENYLIST"`
	DockerHost           string `envconfig:"DEV_MACHINE_DOCKER_HOST" default:"unix:///var/run/docker.sock"`
	SeccompProfile       string `envconfig:"DEV_MACHINE_SECCOMP_PROFILE" default:"default"`
	AppArmorProfile      string `envconfig:"DEV_MACHINE_APPARMOR_PROFILE" default:"default"`
	IDEImage             string `envconfig:"DEV_MACHINE_IDE_IMAGE" default:"ghcr.io/kuayle/dev-machine-ide:0.1.0"`
	BrowserImage         string `envconfig:"DEV_MACHINE_BROWSER_IMAGE" default:"ghcr.io/kuayle/dev-machine-browser:0.1.0"`
	CollectorImage       string `envconfig:"DEV_MACHINE_COLLECTOR_IMAGE" default:"ghcr.io/kuayle/dev-machine-collector:0.1.0"`
	EgressImage          string `envconfig:"DEV_MACHINE_EGRESS_IMAGE" default:"ghcr.io/kuayle/dev-machine-egress:0.1.0"`
	ClaudeCodeImage      string `envconfig:"DEV_MACHINE_CLAUDE_IMAGE" default:"ghcr.io/kuayle/dev-machine-agent-claude:0.1.0"`
	OpenCodeImage        string `envconfig:"DEV_MACHINE_OPENCODE_IMAGE" default:"ghcr.io/kuayle/dev-machine-agent-opencode:0.1.0"`
	CodexImage           string `envconfig:"DEV_MACHINE_CODEX_IMAGE" default:"ghcr.io/kuayle/dev-machine-agent-codex:0.1.0"`
	CustomImage          string `envconfig:"DEV_MACHINE_CUSTOM_IMAGE"`
}

type machineGatewayEnvConfig struct {
	DatabaseURL        string `envconfig:"DATABASE_URL"`
	GatewayDatabaseURL string `envconfig:"DEV_MACHINE_GATEWAY_DATABASE_URL"`
	FrontendURL        string `envconfig:"FRONTEND_URL"`
	Environment        string `envconfig:"ENVIRONMENT" default:"development"`
	Sysadmins          string `envconfig:"SYSADMINS"`
	PublicDemoMode     bool   `envconfig:"PUBLIC_DEMO_MODE"`
	DevMachine         DevMachineConfig
}

func (c DevMachineConfig) Validate(frontendURL string) error {
	if !c.Enabled {
		return nil
	}
	if err := c.ValidateGateway(frontendURL); err != nil {
		return err
	}
	if len(c.EncryptionKey) < 32 {
		return fmt.Errorf("DEV_MACHINE_ENCRYPTION_KEY must be at least 32 characters when dev machines are enabled")
	}
	ingestURL, err := url.Parse(c.IngestURL)
	if err != nil || ingestURL.Scheme != "https" || ingestURL.Hostname() == "" || ingestURL.User != nil {
		return fmt.Errorf("DEV_MACHINE_INGEST_URL must be an HTTPS URL without embedded credentials")
	}
	if c.SeccompProfile != "" && c.SeccompProfile != "default" && c.SeccompProfile != "unconfined" && !json.Valid([]byte(c.SeccompProfile)) {
		return fmt.Errorf("DEV_MACHINE_SECCOMP_PROFILE must be default, unconfined, or an inline JSON profile")
	}
	if strings.ContainsAny(c.AppArmorProfile, "\x00\r\n\t ") {
		return fmt.Errorf("DEV_MACHINE_APPARMOR_PROFILE must be a profile name without whitespace")
	}
	return nil
}

func (c DevMachineConfig) ValidateGateway(frontendURL string) error {
	if !validHostnameSuffix(c.Domain) {
		return fmt.Errorf("DEV_MACHINE_DOMAIN must be a hostname suffix when dev machines are enabled")
	}
	frontend, err := url.Parse(frontendURL)
	if err != nil || frontend.Hostname() == "" {
		return fmt.Errorf("FRONTEND_URL must be an absolute URL when dev machines are enabled")
	}
	if !separateMachineSite(frontend.Hostname(), c.Domain) {
		return fmt.Errorf("DEV_MACHINE_DOMAIN must use a separate registrable domain from FRONTEND_URL")
	}
	if c.TicketTTLSeconds < 15 || c.TicketTTLSeconds > 300 {
		return fmt.Errorf("DEV_MACHINE_ACCESS_TICKET_TTL_SECONDS must be between 15 and 300")
	}
	if c.SessionTTLMinutes < 5 || c.SessionTTLMinutes > 1440 {
		return fmt.Errorf("DEV_MACHINE_SESSION_TTL_MINUTES must be between 5 and 1440")
	}
	return nil
}

func separateMachineSite(frontendHost, machineDomain string) bool {
	frontendHost = strings.Trim(strings.ToLower(frontendHost), ".")
	machineDomain = strings.Trim(strings.ToLower(machineDomain), ".")
	if (frontendHost == "localhost" || strings.HasSuffix(frontendHost, ".localhost")) &&
		(machineDomain == "localhost" || strings.HasSuffix(machineDomain, ".localhost")) {
		return true
	}
	frontendSite, frontendErr := publicsuffix.EffectiveTLDPlusOne(frontendHost)
	machineSite, machineErr := publicsuffix.EffectiveTLDPlusOne(machineDomain)
	return frontendErr == nil && machineErr == nil && !strings.EqualFold(frontendSite, machineSite)
}

func validHostnameSuffix(value string) bool {
	value = strings.Trim(strings.ToLower(value), ".")
	if value == "" || len(value) > 253 || strings.ContainsAny(value, "/:@\x00\r\n\t ") {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
				return false
			}
		}
	}
	return true
}

func Load() (*Config, error) {
	var cfg Config
	if err := envconfig.Process("", &cfg); err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}
	if len(cfg.JWTSecret) < 32 {
		return nil, fmt.Errorf("JWT_SECRET must be at least 32 characters")
	}
	if cfg.SystemUpdaterToken != "" && len(cfg.SystemUpdaterToken) < 32 {
		return nil, fmt.Errorf("SYSTEM_UPDATER_TOKEN must be at least 32 characters")
	}
	sysadmins, err := parseSysadmins(cfg.Sysadmins)
	if err != nil {
		return nil, err
	}
	cfg.sysadminIDs = sysadmins
	if err := cfg.DevMachine.Validate(cfg.FrontendURL); err != nil {
		return nil, fmt.Errorf("dev machine config: %w", err)
	}
	cfg.DevMachine.Domain = strings.Trim(strings.ToLower(cfg.DevMachine.Domain), ".")
	return &cfg, nil
}

func LoadMachineGateway() (*Config, error) {
	var gatewayCfg machineGatewayEnvConfig
	if err := envconfig.Process("", &gatewayCfg); err != nil {
		return nil, fmt.Errorf("loading gateway config: %w", err)
	}
	applicationDatabaseURL := strings.TrimSpace(gatewayCfg.DatabaseURL)
	gatewayDatabaseURL := strings.TrimSpace(gatewayCfg.GatewayDatabaseURL)
	production := strings.EqualFold(strings.TrimSpace(gatewayCfg.Environment), "production")
	if production && gatewayDatabaseURL == "" {
		return nil, fmt.Errorf("DEV_MACHINE_GATEWAY_DATABASE_URL is required for the machine gateway in production")
	}
	if production {
		gatewayUser, gatewayErr := databaseURLUsername(gatewayDatabaseURL)
		if gatewayErr != nil {
			return nil, fmt.Errorf("DEV_MACHINE_GATEWAY_DATABASE_URL must be a PostgreSQL URL with a username in production")
		}
		if applicationDatabaseURL != "" {
			applicationUser, appErr := databaseURLUsername(applicationDatabaseURL)
			if appErr != nil {
				return nil, fmt.Errorf("DATABASE_URL must be a PostgreSQL URL with a username in production")
			}
			if applicationUser == gatewayUser {
				return nil, fmt.Errorf("DEV_MACHINE_GATEWAY_DATABASE_URL must use a database user distinct from DATABASE_URL in production")
			}
		}
	}
	databaseURL := applicationDatabaseURL
	if gatewayDatabaseURL != "" {
		databaseURL = gatewayDatabaseURL
	}
	if strings.TrimSpace(databaseURL) == "" {
		return nil, fmt.Errorf("DATABASE_URL or DEV_MACHINE_GATEWAY_DATABASE_URL is required for the machine gateway")
	}
	if strings.TrimSpace(gatewayCfg.FrontendURL) == "" {
		return nil, fmt.Errorf("FRONTEND_URL is required for the machine gateway")
	}
	sysadmins, err := parseSysadmins(gatewayCfg.Sysadmins)
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		DatabaseURL:    databaseURL,
		FrontendURL:    gatewayCfg.FrontendURL,
		Environment:    gatewayCfg.Environment,
		DevMachine:     gatewayCfg.DevMachine,
		PublicDemoMode: gatewayCfg.PublicDemoMode,
		sysadminIDs:    sysadmins,
	}
	if !cfg.DevMachine.Enabled {
		return nil, fmt.Errorf("DEV_MACHINES_ENABLED must be true for the machine gateway")
	}
	if err := cfg.DevMachine.ValidateGateway(cfg.FrontendURL); err != nil {
		return nil, fmt.Errorf("dev machine gateway config: %w", err)
	}
	cfg.DevMachine.Domain = strings.Trim(strings.ToLower(cfg.DevMachine.Domain), ".")
	return cfg, nil
}

func databaseURLUsername(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || parsed.Hostname() == "" || parsed.User == nil || parsed.User.Username() == "" {
		return "", fmt.Errorf("invalid PostgreSQL URL")
	}
	return parsed.User.Username(), nil
}

func (c *Config) IsSysAdmin(userID uuid.UUID) bool {
	if c == nil || userID == uuid.Nil {
		return false
	}
	_, ok := c.sysadminIDs[userID]
	return ok
}

// DemoDevMachineAllowed returns true when the demo-mode restriction is
// inactive, or when the restriction is active and userID is a sysadmin.
// In demo mode with an empty SYSADMINS list this always returns false
// (fail closed).
func (c *Config) DemoDevMachineAllowed(userID uuid.UUID) bool {
	if c == nil || !c.PublicDemoMode {
		return true
	}
	return c.IsSysAdmin(userID)
}

func parseSysadmins(raw string) (map[uuid.UUID]struct{}, error) {
	ids := make(map[uuid.UUID]struct{})
	for _, part := range strings.Split(raw, ",") {
		value := strings.TrimSpace(part)
		if value == "" {
			continue
		}
		id, err := uuid.Parse(value)
		if err != nil {
			return nil, fmt.Errorf("SYSADMINS contains invalid user ID %q: %w", value, err)
		}
		ids[id] = struct{}{}
	}
	return ids, nil
}
