package config

import (
	"fmt"

	"github.com/kuayle/kuayle-backend/pkg/storage"
	"github.com/kelseyhightower/envconfig"
)

// GitHubAppConfig holds optional global GitHub App credentials.
// When set, all workspaces share this app (SaaS mode) instead of creating per-workspace apps.
type GitHubAppConfig struct {
	AppID         int64  `envconfig:"GITHUB_APP_ID"`
	PrivateKey    string `envconfig:"GITHUB_APP_PRIVATE_KEY"`    // base64-encoded PEM
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
	Environment string         `envconfig:"ENVIRONMENT" default:"development"`
	Storage     storage.Config

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
	return &cfg, nil
}
