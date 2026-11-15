package dto

import (
	"time"

	"github.com/google/uuid"
)

// BackupSettingsResponse is the instance backup configuration. Secrets are
// write-only: only whether one is stored is reported.
type BackupSettingsResponse struct {
	Enabled           bool       `json:"enabled"`
	ScheduleKind      string     `json:"schedule_kind"`
	ScheduleTime      string     `json:"schedule_time"`
	ScheduleWeekday   int        `json:"schedule_weekday"`
	ScheduleCron      string     `json:"schedule_cron"`
	RetentionKind     string     `json:"retention_kind"`
	RetentionValue    int        `json:"retention_value"`
	S3Enabled         bool       `json:"s3_enabled"`
	S3Endpoint        string     `json:"s3_endpoint"`
	S3Bucket          string     `json:"s3_bucket"`
	S3Region          string     `json:"s3_region"`
	S3Prefix          string     `json:"s3_prefix"`
	HasAccessKey      bool       `json:"has_access_key"`
	HasSecretKey      bool       `json:"has_secret_key"`
	EncryptionEnabled bool       `json:"encryption_enabled"`
	HasPassphrase     bool       `json:"has_passphrase"`
	NextRunAt         *time.Time `json:"next_run_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	BackupDir         string     `json:"backup_dir"`
}

// UpdateBackupSettingsRequest saves the configuration. An omitted or empty
// secret keeps the stored one; clear_s3_credentials and clear_passphrase
// remove them.
type UpdateBackupSettingsRequest struct {
	Enabled            bool    `json:"enabled"`
	ScheduleKind       string  `json:"schedule_kind"`
	ScheduleTime       string  `json:"schedule_time"`
	ScheduleWeekday    int     `json:"schedule_weekday"`
	ScheduleCron       string  `json:"schedule_cron"`
	RetentionKind      string  `json:"retention_kind"`
	RetentionValue     int     `json:"retention_value"`
	S3Enabled          bool    `json:"s3_enabled"`
	S3Endpoint         string  `json:"s3_endpoint"`
	S3Bucket           string  `json:"s3_bucket"`
	S3Region           string  `json:"s3_region"`
	S3Prefix           string  `json:"s3_prefix"`
	S3AccessKey        *string `json:"s3_access_key"`
	S3SecretKey        *string `json:"s3_secret_key"`
	ClearS3Credentials bool    `json:"clear_s3_credentials"`
	EncryptionEnabled  bool    `json:"encryption_enabled"`
	Passphrase         *string `json:"passphrase"`
	ClearPassphrase    bool    `json:"clear_passphrase"`
}

// TestBackupConnectionRequest tests a destination, possibly unsaved. Empty
// keys fall back to the stored ones.
type TestBackupConnectionRequest struct {
	S3Endpoint  string `json:"s3_endpoint"`
	S3Bucket    string `json:"s3_bucket"`
	S3Region    string `json:"s3_region"`
	S3Prefix    string `json:"s3_prefix"`
	S3AccessKey string `json:"s3_access_key"`
	S3SecretKey string `json:"s3_secret_key"`
}

// BackupRunResponse is one entry of the backup history.
type BackupRunResponse struct {
	ID             uuid.UUID  `json:"id"`
	Kind           string     `json:"kind"`
	Status         string     `json:"status"`
	UploadStatus   string     `json:"upload_status"`
	StartedAt      time.Time  `json:"started_at"`
	FinishedAt     *time.Time `json:"finished_at"`
	DurationMS     *int64     `json:"duration_ms"`
	SizeBytes      *int64     `json:"size_bytes"`
	ChecksumSHA256 *string    `json:"checksum_sha256"`
	FileName       string     `json:"file_name"`
	S3Key          string     `json:"s3_key"`
	S3Bucket       string     `json:"s3_bucket"`
	Destination    string     `json:"destination"`
	Encrypted      bool       `json:"encrypted"`
	Error          *string    `json:"error"`
	VerifiedAt     *time.Time `json:"verified_at"`
	VerifyError    *string    `json:"verify_error"`
	DeletedAt      *time.Time `json:"deleted_at"`
}

// BackupRunsResponse is a page of the history.
type BackupRunsResponse struct {
	Runs []BackupRunResponse `json:"runs"`
}
