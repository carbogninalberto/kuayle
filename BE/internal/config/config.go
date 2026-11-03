package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"github.com/kelseyhightower/envconfig"
	"github.com/kuayle/kuayle-backend/pkg/mail"
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
	Port        int    `envconfig:"PORT" default:"8080"`
	DatabaseURL string `envconfig:"DATABASE_URL" required:"true"`
	RedisURL    string `envconfig:"REDIS_URL" required:"true"`
	JWTSecret   string `envconfig:"JWT_SECRET" required:"true"`
	FrontendURL string `envconfig:"FRONTEND_URL" default:"http://localhost:5173"`
	Environment string `envconfig:"ENVIRONMENT" default:"development"`
	Storage     storage.Config
	// SMTP is optional; password reset emails need it (the link is logged in development otherwise).
	SMTP           mail.Config
	Sysadmins      string `envconfig:"SYSADMINS"`
	PublicDemoMode bool   `envconfig:"PUBLIC_DEMO_MODE"`

	// DisableRegistration gates public registration behind a valid invite-link
	// token. Defaults to false, preserving open registration.
	DisableRegistration bool `envconfig:"DISABLE_REGISTRATION" default:"false"`

	// AIAllowPrivateNetwork lets workspace AI settings target providers on
	// private or loopback addresses. Off by default: any workspace owner can
	// set the base URL, so enabling it trusts every owner with internal access.
	AIAllowPrivateNetwork bool `envconfig:"AI_ALLOW_PRIVATE_NETWORK" default:"false"`

	// Voice transcription service (optional). Both are operator settings:
	// VOICE_URL is the internal address of the voice container and
	// VOICE_INTERNAL_TOKEN the shared secret it requires. Voice stays off
	// for every workspace unless both are set.
	VoiceURL           string `envconfig:"VOICE_URL"`
	VoiceInternalToken string `envconfig:"VOICE_INTERNAL_TOKEN"`

	SystemUpdaterURL   string `envconfig:"SYSTEM_UPDATER_URL"`
	SystemUpdaterToken string `envconfig:"SYSTEM_UPDATER_TOKEN"`

	sysadminIDs map[uuid.UUID]struct{}

	// GitHub webhook URL (optional — for dev with smee.io or private networks)
	// If not set, auto-derived from FRONTEND_URL for public domains, or disabled for localhost.
	GitHubWebhookURL string `envconfig:"GITHUB_WEBHOOK_URL"`

	// Telegram webhook URL (optional — for dev with a relay such as smee.io).
	// If not set, FRONTEND_URL/api/telegram/webhook/:workspaceId is used.
	TelegramWebhookURL string `envconfig:"TELEGRAM_WEBHOOK_URL"`

	// Global GitHub App (SaaS mode — shared across all workspaces)
	GitHubApp GitHubAppConfig

	// Dev machine configuration
	DevMachine DevMachineConfig
}

type DevMachineConfig struct {
	AllowSameSite        bool   `envconfig:"DEV_MACHINE_ALLOW_SAME_SITE" default:"false"`
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
	HeadlessImage        string `envconfig:"DEV_MACHINE_HEADLESS_IMAGE" default:"ghcr.io/kuayle/dev-machine-headless:0.1.0"`
	BrowserImage         string `envconfig:"DEV_MACHINE_BROWSER_IMAGE" default:"ghcr.io/kuayle/dev-machine-browser:0.1.0"`
	CollectorImage       string `envconfig:"DEV_MACHINE_COLLECTOR_IMAGE" default:"ghcr.io/kuayle/dev-machine-collector:0.1.0"`
	EgressImage          string `envconfig:"DEV_MACHINE_EGRESS_IMAGE" default:"ghcr.io/kuayle/dev-machine-egress:0.1.0"`
	ClaudeCodeImage      string `envconfig:"DEV_MACHINE_CLAUDE_IMAGE" default:"ghcr.io/kuayle/dev-machine-agent-claude:0.1.0"`
	OpenCodeImage        string `envconfig:"DEV_MACHINE_OPENCODE_IMAGE" default:"ghcr.io/kuayle/dev-machine-agent-opencode:0.1.0"`
	CodexImage           string `envconfig:"DEV_MACHINE_CODEX_IMAGE" default:"ghcr.io/kuayle/dev-machine-agent-codex:0.1.0"`
	CustomImage          string `envconfig:"DEV_MACHINE_CUSTOM_IMAGE"`
	// DockerImage is the per-machine Docker daemon (rootless docker:dind with
	// Kuayle's start script), used when a workspace policy allows Docker.
	DockerImage string `envconfig:"DEV_MACHINE_DOCKER_IMAGE" default:"ghcr.io/kuayle/dev-machine-docker:0.1.0"`
	// DockerRuntime is an optional OCI runtime for the daemon container, such
	// as sysbox-runc, which isolates it without the relaxations below.
	DockerRuntime string `envconfig:"DEV_MACHINE_DOCKER_RUNTIME"`
	// DockerSeccompProfile and DockerAppArmorProfile apply to the daemon
	// container only (without DockerRuntime). Rootless Docker needs the user
	// namespace and mount system calls the defaults deny.
	DockerSeccompProfile  string `envconfig:"DEV_MACHINE_DOCKER_SECCOMP_PROFILE" default:"unconfined"`
	DockerAppArmorProfile string `envconfig:"DEV_MACHINE_DOCKER_APPARMOR_PROFILE" default:"unconfined"`
	// DiskQuotaMode selects how workspace disk limits are enforced: "hard" uses
	// Docker local-volume size quotas (XFS project quotas); "monitored" measures
	// usage, warns and stops machines, and guards the host free-space floor.
	DiskQuotaMode          string `envconfig:"DEV_MACHINE_DISK_QUOTA_MODE" default:"hard"`
	HostDiskMinFreePercent int    `envconfig:"DEV_MACHINE_HOST_DISK_MIN_FREE_PERCENT" default:"15"`
	HostDiskMinFreeGB      int    `envconfig:"DEV_MACHINE_HOST_DISK_MIN_FREE_GB" default:"25"`
	// HostDiskPath is an optional path inside the manager container on the same
	// filesystem as Docker's data root (e.g. a read-only bind mount of it), used
	// to measure host free space even when no machine is running.
	HostDiskPath string `envconfig:"DEV_MACHINE_HOST_DISK_PATH"`
	// AgentGitName and AgentGitEmail are the commit author for agent task
	// publications. Pull requests are still opened by the GitHub App.
	AgentGitName  string `envconfig:"DEV_MACHINE_AGENT_GIT_NAME" default:"Kuayle Agent"`
	AgentGitEmail string `envconfig:"DEV_MACHINE_AGENT_GIT_EMAIL" default:"agent@kuayle.invalid"`
	// Host admission: resources kept for the host and control plane, and the
	// allowed CPU overcommit. Machine memory is never overcommitted.
	HostReservedCPUMillis int     `envconfig:"DEV_MACHINE_HOST_RESERVED_CPU_MILLIS" default:"1000"`
	HostReservedMemoryMB  int     `envconfig:"DEV_MACHINE_HOST_RESERVED_MEMORY_MB" default:"2048"`
	CPUOvercommit         float64 `envconfig:"DEV_MACHINE_CPU_OVERCOMMIT" default:"2"`
}

const (
	DiskQuotaModeHard      = "hard"
	DiskQuotaModeMonitored = "monitored"
)

// EffectiveDiskQuotaMode normalizes the configured mode; empty means hard.
func (c DevMachineConfig) EffectiveDiskQuotaMode() string {
	mode := strings.ToLower(strings.TrimSpace(c.DiskQuotaMode))
	if mode == "" {
		return DiskQuotaModeHard
	}
	return mode
}

func (c DevMachineConfig) MonitoredDiskQuota() bool {
	return c.EffectiveDiskQuotaMode() == DiskQuotaModeMonitored
}

func (c DevMachineConfig) validateDiskQuota() error {
	switch c.EffectiveDiskQuotaMode() {
	case DiskQuotaModeHard:
		return nil
	case DiskQuotaModeMonitored:
		if c.HostDiskMinFreePercent < 1 || c.HostDiskMinFreePercent > 90 {
			return fmt.Errorf("DEV_MACHINE_HOST_DISK_MIN_FREE_PERCENT must be between 1 and 90")
		}
		if c.HostDiskMinFreeGB < 1 {
			return fmt.Errorf("DEV_MACHINE_HOST_DISK_MIN_FREE_GB must be at least 1")
		}
		if c.HostDiskPath != "" && !path.IsAbs(c.HostDiskPath) {
			return fmt.Errorf("DEV_MACHINE_HOST_DISK_PATH must be an absolute path")
		}
		return nil
	default:
		return fmt.Errorf("DEV_MACHINE_DISK_QUOTA_MODE must be hard or monitored")
	}
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
	if c.AllowSameSite {
		if err := c.validateSameSiteOrigin(frontendURL); err != nil {
			return err
		}
	}
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
	if c.HostReservedCPUMillis < 0 || c.HostReservedMemoryMB < 0 {
		return fmt.Errorf("DEV_MACHINE_HOST_RESERVED_CPU_MILLIS and DEV_MACHINE_HOST_RESERVED_MEMORY_MB must not be negative")
	}
	if c.CPUOvercommit != 0 && (c.CPUOvercommit < 1 || c.CPUOvercommit > 10) {
		return fmt.Errorf("DEV_MACHINE_CPU_OVERCOMMIT must be between 1 and 10")
	}
	for _, value := range []string{c.AgentGitName, c.AgentGitEmail} {
		if strings.ContainsFunc(value, func(r rune) bool { return unicode.IsControl(r) || r == '<' || r == '>' }) {
			return fmt.Errorf("DEV_MACHINE_AGENT_GIT_NAME and DEV_MACHINE_AGENT_GIT_EMAIL must not contain control characters or angle brackets")
		}
	}
	return c.validateDiskQuota()
}

func (c DevMachineConfig) ValidateGateway(frontendURL string) error {
	if !validHostnameSuffix(c.Domain) {
		return fmt.Errorf("DEV_MACHINE_DOMAIN must be a hostname suffix when dev machines are enabled")
	}
	frontend, err := url.Parse(frontendURL)
	if err != nil || frontend.Hostname() == "" {
		return fmt.Errorf("FRONTEND_URL must be an absolute URL when dev machines are enabled")
	}
	if c.AllowSameSite {
		if err := c.validateSameSiteOrigin(frontendURL); err != nil {
			return err
		}
		if _, err := publicsuffix.EffectiveTLDPlusOne(strings.Trim(strings.ToLower(c.Domain), ".")); err != nil {
			return fmt.Errorf("DEV_MACHINE_DOMAIN must be a registrable hostname")
		}
		if strings.HasSuffix(strings.Trim(strings.ToLower(frontend.Hostname()), "."), "."+strings.Trim(strings.ToLower(c.Domain), ".")) {
			return fmt.Errorf("FRONTEND_URL must not overlap machine hostnames")
		}
	}
	if !c.AllowSameSite && !separateMachineSite(frontend.Hostname(), c.Domain) {
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
	if cfg.VoiceURL != "" && cfg.VoiceInternalToken != "" && len(cfg.VoiceInternalToken) < 32 {
		return nil, fmt.Errorf("VOICE_INTERNAL_TOKEN must be at least 32 characters")
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

func (c DevMachineConfig) validateSameSiteOrigin(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || raw != "https://"+u.Host {
		return fmt.Errorf("DEV_MACHINE_ALLOW_SAME_SITE requires FRONTEND_URL to be an exact HTTPS origin")
	}
	return nil
}
