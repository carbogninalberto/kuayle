package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// ErrBackupRunNotFound reports an unknown backup run.
var ErrBackupRunNotFound = errors.New("backup run not found")

// BackupRepository stores the instance backup settings (a single row) and
// the backup run history.
type BackupRepository struct {
	db *sqlx.DB
}

func NewBackupRepository(db *sqlx.DB) *BackupRepository {
	return &BackupRepository{db: db}
}

const backupSettingsColumns = `enabled, schedule_kind, schedule_time, schedule_weekday, schedule_cron,
	retention_kind, retention_value, s3_enabled, s3_endpoint, s3_bucket, s3_region, s3_prefix,
	s3_access_key_encrypted, s3_secret_key_encrypted, encryption_enabled, passphrase_encrypted,
	next_run_at, updated_at, updated_by`

const backupRunColumns = `id, kind, status, upload_status, started_at, finished_at, duration_ms,
	size_bytes, checksum_sha256, local_path, s3_key, s3_endpoint, s3_bucket, destination, encrypted, error, verified_at,
	verify_error, deleted_at, created_by`

func (r *BackupRepository) GetSettings(ctx context.Context) (*domain.BackupSettings, error) {
	var settings domain.BackupSettings
	err := r.db.GetContext(ctx, &settings, `SELECT `+backupSettingsColumns+` FROM backup_settings WHERE id = 1`)
	if err != nil {
		return nil, err
	}
	return &settings, nil
}

// UpdateSettings saves the configuration and the next scheduled run. It does
// not touch the lease.
func (r *BackupRepository) UpdateSettings(ctx context.Context, s *domain.BackupSettings) error {
	return r.db.QueryRowContext(ctx, `UPDATE backup_settings SET
		enabled = $1, schedule_kind = $2, schedule_time = $3, schedule_weekday = $4, schedule_cron = $5,
		retention_kind = $6, retention_value = $7, s3_enabled = $8, s3_endpoint = $9, s3_bucket = $10,
		s3_region = $11, s3_prefix = $12, s3_access_key_encrypted = $13, s3_secret_key_encrypted = $14,
		encryption_enabled = $15, passphrase_encrypted = $16, next_run_at = $17, updated_by = $18,
		updated_at = NOW()
		WHERE id = 1 RETURNING updated_at`,
		s.Enabled, s.BackupSchedule.Kind, s.Time, s.Weekday, s.Cron,
		s.BackupRetention.Kind, s.Value, s.S3Enabled, s.S3Endpoint, s.S3Bucket,
		s.S3Region, s.S3Prefix, s.S3AccessKeyEncrypted, s.S3SecretKeyEncrypted,
		s.EncryptionEnabled, s.PassphraseEncrypted, s.NextRunAt, s.UpdatedBy,
	).Scan(&s.UpdatedAt)
}

// ClaimSchedule moves next_run_at from expected to next and reports whether
// this caller won the slot; with several replicas exactly one wins.
func (r *BackupRepository) ClaimSchedule(ctx context.Context, expected *time.Time, next time.Time) (bool, error) {
	result, err := r.db.ExecContext(ctx, `UPDATE backup_settings SET next_run_at = $2
		WHERE id = 1 AND enabled AND next_run_at IS NOT DISTINCT FROM $1`, expected, next)
	return affectedOne(result, err)
}

// AcquireLease takes the instance-wide backup lease when it is free or has
// expired, so only one replica runs a backup at a time.
func (r *BackupRepository) AcquireLease(ctx context.Context, token uuid.UUID, ttl time.Duration) (bool, error) {
	result, err := r.db.ExecContext(ctx, `UPDATE backup_settings
		SET lease_token = $1, lease_expires_at = NOW() + ($2 * INTERVAL '1 millisecond')
		WHERE id = 1 AND (lease_token IS NULL OR lease_expires_at < NOW())`, token, ttl.Milliseconds())
	return affectedOne(result, err)
}

// RenewLease extends the lease while token still holds it.
func (r *BackupRepository) RenewLease(ctx context.Context, token uuid.UUID, ttl time.Duration) (bool, error) {
	result, err := r.db.ExecContext(ctx, `UPDATE backup_settings
		SET lease_expires_at = NOW() + ($2 * INTERVAL '1 millisecond')
		WHERE id = 1 AND lease_token = $1`, token, ttl.Milliseconds())
	return affectedOne(result, err)
}

// ReleaseLease frees the lease if token still holds it.
func (r *BackupRepository) ReleaseLease(ctx context.Context, token uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `UPDATE backup_settings SET lease_token = NULL, lease_expires_at = NULL
		WHERE id = 1 AND lease_token = $1`, token)
	return err
}

func (r *BackupRepository) CreateRun(ctx context.Context, run *domain.BackupRun) error {
	return r.db.QueryRowContext(ctx, `INSERT INTO backup_runs
		(id, kind, status, upload_status, local_path, s3_key, s3_endpoint, s3_bucket, destination, encrypted, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) RETURNING started_at`,
		run.ID, run.Kind, run.Status, run.UploadStatus, run.LocalPath, run.S3Key, run.S3Endpoint, run.S3Bucket, run.Destination,
		run.Encrypted, run.CreatedBy).Scan(&run.StartedAt)
}

// UpdateRun saves a run's progress and outcome.
func (r *BackupRepository) UpdateRun(ctx context.Context, run *domain.BackupRun) error {
	result, err := r.db.ExecContext(ctx, `UPDATE backup_runs SET status = $2, upload_status = $3,
		finished_at = $4, duration_ms = $5, size_bytes = $6, checksum_sha256 = $7, local_path = $8,
		s3_key = $9, s3_endpoint = $10, s3_bucket = $11, destination = $12, encrypted = $13, error = $14,
		verified_at = $15, verify_error = $16, deleted_at = $17
		WHERE id = $1`,
		run.ID, run.Status, run.UploadStatus, run.FinishedAt, run.DurationMS, run.SizeBytes,
		run.ChecksumSHA256, run.LocalPath, run.S3Key, run.S3Endpoint, run.S3Bucket, run.Destination, run.Encrypted, run.Error,
		run.VerifiedAt, run.VerifyError, run.DeletedAt)
	won, err := affectedOne(result, err)
	if err == nil && !won {
		return ErrBackupRunNotFound
	}
	return err
}

// ListRuns returns the history, newest first.
func (r *BackupRepository) ListRuns(ctx context.Context, limit, offset int) ([]domain.BackupRun, error) {
	runs := []domain.BackupRun{}
	err := r.db.SelectContext(ctx, &runs, `SELECT `+backupRunColumns+` FROM backup_runs
		ORDER BY started_at DESC, id LIMIT $1 OFFSET $2`, limit, offset)
	return runs, err
}

// ListRetainable returns the successful backups made by the backend that
// still have files, which retention chooses from.
func (r *BackupRepository) ListRetainable(ctx context.Context) ([]domain.BackupRun, error) {
	runs := []domain.BackupRun{}
	err := r.db.SelectContext(ctx, &runs, `SELECT `+backupRunColumns+` FROM backup_runs
		WHERE status = 'succeeded' AND deleted_at IS NULL AND kind <> 'pre_upgrade'
		ORDER BY started_at DESC`)
	return runs, err
}

func (r *BackupRepository) GetRun(ctx context.Context, id uuid.UUID) (*domain.BackupRun, error) {
	var run domain.BackupRun
	err := r.db.GetContext(ctx, &run, `SELECT `+backupRunColumns+` FROM backup_runs WHERE id = $1`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrBackupRunNotFound
	}
	if err != nil {
		return nil, err
	}
	return &run, nil
}

// MarkDeleted records that retention removed a backup's files.
func (r *BackupRepository) MarkDeleted(ctx context.Context, id uuid.UUID, at time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE backup_runs SET deleted_at = $2 WHERE id = $1 AND deleted_at IS NULL`, id, at)
	return err
}

// MarkStaleRunning fails runs left "running" by a replica that stopped. It
// only acts while nobody holds a live lease, because a live lease means the
// run may still be in progress.
func (r *BackupRepository) MarkStaleRunning(ctx context.Context, before time.Time) (int64, error) {
	result, err := r.db.ExecContext(ctx, `UPDATE backup_runs SET status = 'failed',
		upload_status = CASE WHEN upload_status = 'uploading' THEN 'failed' ELSE upload_status END,
		error = 'interrupted', finished_at = NOW()
		WHERE status = 'running' AND started_at < $1
		AND NOT EXISTS (SELECT 1 FROM backup_settings WHERE id = 1 AND lease_expires_at >= NOW())`, before)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func affectedOne(result sql.Result, err error) (bool, error) {
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected == 1, err
}

// NotificationTarget returns a workspace and a team of that workspace the
// user belongs to, oldest membership first, so an instance-level notice can
// be delivered to their inbox with a source team they can see. ok is false
// when the user is in no team.
func (r *BackupRepository) NotificationTarget(ctx context.Context, userID uuid.UUID) (workspaceID, teamID uuid.UUID, ok bool, err error) {
	var target struct {
		WorkspaceID uuid.UUID `db:"workspace_id"`
		TeamID      uuid.UUID `db:"team_id"`
	}
	err = r.db.GetContext(ctx, &target, `SELECT t.workspace_id, t.id AS team_id
		FROM team_members tm
		JOIN teams t ON t.id = tm.team_id
		JOIN workspace_members wm ON wm.workspace_id = t.workspace_id AND wm.user_id = tm.user_id
		WHERE tm.user_id = $1
		ORDER BY wm.created_at, tm.created_at, t.id
		LIMIT 1`, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, uuid.Nil, false, err
	}
	return target.WorkspaceID, target.TeamID, true, nil
}
