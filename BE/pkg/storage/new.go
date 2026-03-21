package storage

import "fmt"

// Type represents a storage backend type.
type Type string

const (
	TypeLocal Type = "local"
	TypeS3    Type = "s3"
)

// Config holds all storage configuration, loaded via envconfig.
type Config struct {
	Type        Type   `envconfig:"STORAGE_TYPE" default:"local"`
	LocalDir    string `envconfig:"STORAGE_LOCAL_DIR" default:"./uploads"`
	LocalURL    string `envconfig:"STORAGE_LOCAL_URL" default:"/uploads"`
	S3Endpoint  string `envconfig:"S3_ENDPOINT"`
	S3Bucket    string `envconfig:"S3_BUCKET"`
	S3Region    string `envconfig:"S3_REGION" default:"us-east-1"`
	S3AccessKey string `envconfig:"S3_ACCESS_KEY"`
	S3SecretKey string `envconfig:"S3_SECRET_KEY"`
	S3Public    bool   `envconfig:"S3_PUBLIC" default:"true"`
	S3CDNBase   string `envconfig:"S3_CDN_BASE_URL"`
}

// New creates a storage backend from the given configuration.
func New(cfg Config) (Backend, error) {
	switch cfg.Type {
	case TypeLocal, "":
		return NewLocalBackend(cfg.LocalDir, cfg.LocalURL)
	case TypeS3:
		return NewS3Backend(S3Config{
			Endpoint:   cfg.S3Endpoint,
			Bucket:     cfg.S3Bucket,
			Region:     cfg.S3Region,
			AccessKey:  cfg.S3AccessKey,
			SecretKey:  cfg.S3SecretKey,
			Public:     cfg.S3Public,
			CDNBaseURL: cfg.S3CDNBase,
		})
	default:
		return nil, fmt.Errorf("unsupported storage type: %s", cfg.Type)
	}
}
