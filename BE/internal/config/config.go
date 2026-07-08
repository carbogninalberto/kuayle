package config

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/kelseyhightower/envconfig"
	"github.com/kuayle/kuayle-backend/pkg/storage"
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
	Sysadmins   string `envconfig:"SYSADMINS"`

	SystemUpdaterURL   string `envconfig:"SYSTEM_UPDATER_URL"`
	SystemUpdaterToken string `envconfig:"SYSTEM_UPDATER_TOKEN"`

	sysadminIDs map[uuid.UUID]struct{}

	// GitHub webhook URL (optional — for dev with smee.io or private networks)
	// If not set, auto-derived from FRONTEND_URL for public domains, or disabled for localhost.
	GitHubWebhookURL string `envconfig:"GITHUB_WEBHOOK_URL"`

	// Global GitHub App (SaaS mode — shared across all workspaces)
	GitHubApp GitHubAppConfig
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
	return &cfg, nil
}

func (c *Config) IsSysAdmin(userID uuid.UUID) bool {
	if c == nil || userID == uuid.Nil {
		return false
	}
	_, ok := c.sysadminIDs[userID]
	return ok
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
