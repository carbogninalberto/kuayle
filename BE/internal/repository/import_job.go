package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	"github.com/kuayle/kuayle-backend/internal/domain"
)

var (
	ErrImportJobNotFound = errors.New("import job not found")
	ErrImportNotEditable = errors.New("import job can no longer be changed")
)

// importQuerier is satisfied by both *sqlx.DB and *sqlx.Tx. sqlx's ExtContext
// omits QueryRowContext, so the ref upsert (which needs RETURNING) adds it.
type importQuerier interface {
	sqlx.ExtContext
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// ImportJobRepository stores import jobs, their item report and the stable
// external-id map. Every user-facing method is workspace scoped. The worker
// methods operate by job id only, on a job that was already claimed.
type ImportJobRepository struct {
	db *sqlx.DB
}

func NewImportJobRepository(db *sqlx.DB) *ImportJobRepository {
	return &ImportJobRepository{db: db}
}

var importJobColumnList = []string{"id", "seq", "workspace_id", "created_by", "source", "source_site", "status",
	"config", "mapping", "preview", "credentials_enc", "total", "processed", "created_count", "updated_count",
	"skipped_count", "failed_count", "error", "cancel_requested", "created_at", "updated_at", "started_at", "finished_at"}

var importJobColumns = strings.Join(importJobColumnList, ", ")

func qualifiedImportJobColumns(alias string) string {
	qualified := make([]string, 0, len(importJobColumnList))
	for _, column := range importJobColumnList {
		qualified = append(qualified, alias+"."+column)
	}
	return strings.Join(qualified, ", ")
}

// nullableRawJSON scans a nullable jsonb column, leaving the message nil for
// SQL NULL (database/sql cannot store NULL into a json.RawMessage itself).
type nullableRawJSON struct{ target *json.RawMessage }

func (n nullableRawJSON) Scan(src any) error {
	switch value := src.(type) {
	case nil:
		*n.target = nil
	case []byte:
		*n.target = append(json.RawMessage(nil), value...)
	case string:
		*n.target = json.RawMessage(value)
	default:
		return fmt.Errorf("unsupported jsonb value %T", src)
	}
	return nil
}

func (r *ImportJobRepository) Create(ctx context.Context, job *domain.ImportJob) error {
	return r.db.QueryRowContext(ctx, `INSERT INTO import_jobs
		(workspace_id, created_by, source, source_site, status, config, mapping, credentials_enc)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING `+importJobColumns,
		job.WorkspaceID, job.CreatedBy, job.Source, job.SourceSite, domain.ImportStatusDraft,
		nullableJSON(job.Config), nullableJSON(job.Mapping), job.CredentialsEnc,
	).Scan(scanImportJob(job)...)
}

func (r *ImportJobRepository) Get(ctx context.Context, workspaceID, id uuid.UUID) (*domain.ImportJob, error) {
	var job domain.ImportJob
	err := r.db.QueryRowContext(ctx, `SELECT `+importJobColumns+` FROM import_jobs WHERE id = $1 AND workspace_id = $2`, id, workspaceID).Scan(scanImportJob(&job)...)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrImportJobNotFound
	}
	if err != nil {
		return nil, err
	}
	return &job, nil
}

func (r *ImportJobRepository) List(ctx context.Context, workspaceID uuid.UUID, limit, offset int) ([]domain.ImportJob, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+importJobColumns+` FROM import_jobs
		WHERE workspace_id = $1 ORDER BY seq DESC LIMIT $2 OFFSET $3`, workspaceID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := make([]domain.ImportJob, 0)
	for rows.Next() {
		var job domain.ImportJob
		if err := rows.Scan(scanImportJob(&job)...); err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

// UpdateMapping is allowed only before the worker takes the job. A non-empty
// preview replaces the stored one in the same statement, so the mapping gaps
// shown to the admin always match the saved mapping.
func (r *ImportJobRepository) UpdateMapping(ctx context.Context, workspaceID, id uuid.UUID, mapping, preview json.RawMessage) error {
	var previewArg any
	if len(preview) > 0 {
		previewArg = []byte(preview)
	}
	res, err := r.db.ExecContext(ctx, `UPDATE import_jobs SET mapping = $1, preview = COALESCE($2::jsonb, preview),
		updated_at = now()
		WHERE id = $3 AND workspace_id = $4 AND status IN ($5, $6)`,
		nullableJSON(mapping), previewArg, id, workspaceID, domain.ImportStatusDraft, domain.ImportStatusPreviewed)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return r.editabilityError(ctx, workspaceID, id)
	}
	return nil
}

// SetPreview stores a dry-run result and moves the job to previewed. A
// previewed job may be previewed again, for example after the source changed.
func (r *ImportJobRepository) SetPreview(ctx context.Context, workspaceID, id uuid.UUID, preview json.RawMessage) error {
	res, err := r.db.ExecContext(ctx, `UPDATE import_jobs SET preview = $1, status = $2, updated_at = now()
		WHERE id = $3 AND workspace_id = $4 AND status IN ($5, $2)`,
		nullableJSON(preview), domain.ImportStatusPreviewed, id, workspaceID, domain.ImportStatusDraft)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return r.editabilityError(ctx, workspaceID, id)
	}
	return nil
}

func (r *ImportJobRepository) editabilityError(ctx context.Context, workspaceID, id uuid.UUID) error {
	var status string
	if err := r.db.QueryRowContext(ctx, `SELECT status FROM import_jobs WHERE id = $1 AND workspace_id = $2`, id, workspaceID).Scan(&status); err != nil {
		return ErrImportJobNotFound
	}
	return ErrImportNotEditable
}

// Start queues a previewed job. The mapping must already have been validated
// by the service.
func (r *ImportJobRepository) Start(ctx context.Context, workspaceID, id uuid.UUID, total int) error {
	res, err := r.db.ExecContext(ctx, `UPDATE import_jobs SET status = $1, total = $2, error = NULL, updated_at = now()
		WHERE id = $3 AND workspace_id = $4 AND status = $5`,
		domain.ImportStatusQueued, total, id, workspaceID, domain.ImportStatusPreviewed)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return r.editabilityError(ctx, workspaceID, id)
	}
	return nil
}

// ClaimNext atomically takes the oldest queued job. SKIP LOCKED lets several
// API replicas poll without handing the same job to two workers.
func (r *ImportJobRepository) ClaimNext(ctx context.Context) (*domain.ImportJob, error) {
	var job domain.ImportJob
	// The candidate CTE also has an id column, so the returned columns are
	// qualified with the updated table's alias.
	err := r.db.QueryRowContext(ctx, `WITH candidate AS (
		SELECT id FROM import_jobs WHERE status = $1 ORDER BY seq
		FOR UPDATE SKIP LOCKED LIMIT 1)
		UPDATE import_jobs j SET status = $2, started_at = now(), updated_at = now()
		FROM candidate c WHERE j.id = c.id
		RETURNING `+qualifiedImportJobColumns("j"), domain.ImportStatusQueued, domain.ImportStatusRunning).Scan(scanImportJob(&job)...)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &job, nil
}

// IncrementCounters adds the given deltas to a job's progress counters.
func (r *ImportJobRepository) IncrementCounters(ctx context.Context, jobID uuid.UUID, c ImportCounterDelta) error {
	_, err := r.db.ExecContext(ctx, `UPDATE import_jobs SET
		processed = processed + $1, created_count = created_count + $2, updated_count = updated_count + $3,
		skipped_count = skipped_count + $4, failed_count = failed_count + $5, updated_at = now()
		WHERE id = $6`, c.Processed, c.Created, c.Updated, c.Skipped, c.Failed, jobID)
	return err
}

type ImportCounterDelta struct {
	Processed int
	Created   int
	Updated   int
	Skipped   int
	Failed    int
}

func (r *ImportJobRepository) AddItem(ctx context.Context, item *domain.ImportJobItem) error {
	return r.db.QueryRowContext(ctx, `INSERT INTO import_job_items
		(job_id, workspace_id, external_type, external_id, external_key, outcome, reason)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id, created_at`,
		item.JobID, item.WorkspaceID, item.ExternalType, item.ExternalID, item.ExternalKey, item.Outcome, item.Reason,
	).Scan(&item.ID, &item.CreatedAt)
}

// ImportItemsPageMax is the largest item-report page the API returns.
const ImportItemsPageMax = 500

// ImportItemsQueryLimit clamps a ListItems limit. It allows one row more than
// a full page, so the caller can ask for limit+1 to learn whether another
// page exists; anything larger is cut to that, never reset to a default.
func ImportItemsQueryLimit(limit int) int {
	if limit < 1 {
		return 1
	}
	if limit > ImportItemsPageMax+1 {
		return ImportItemsPageMax + 1
	}
	return limit
}

func (r *ImportJobRepository) ListItems(ctx context.Context, workspaceID, jobID uuid.UUID, limit, offset int) ([]domain.ImportJobItem, error) {
	limit = ImportItemsQueryLimit(limit)
	if offset < 0 {
		offset = 0
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id, job_id, workspace_id, external_type, external_id,
		external_key, outcome, reason, created_at FROM import_job_items
		WHERE workspace_id = $1 AND job_id = $2 ORDER BY seq LIMIT $3 OFFSET $4`, workspaceID, jobID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.ImportJobItem, 0)
	for rows.Next() {
		var item domain.ImportJobItem
		if err := rows.Scan(&item.ID, &item.JobID, &item.WorkspaceID, &item.ExternalType, &item.ExternalID,
			&item.ExternalKey, &item.Outcome, &item.Reason, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// Finish writes a terminal status and clears the encrypted credentials in the
// same statement, so there is no window in which a finished job still holds a
// usable secret.
func (r *ImportJobRepository) Finish(ctx context.Context, jobID uuid.UUID, status string, jobError *string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE import_jobs SET status = $1, error = $2,
		finished_at = now(), updated_at = now(), credentials_enc = NULL
		WHERE id = $3`, status, jobError, jobID)
	return err
}

// WipeCredentials is a defensive cleanup used on every worker exit path.
func (r *ImportJobRepository) WipeCredentials(ctx context.Context, jobID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `UPDATE import_jobs SET credentials_enc = NULL, updated_at = now() WHERE id = $1`, jobID)
	return err
}

func (r *ImportJobRepository) RequestCancel(ctx context.Context, workspaceID, id uuid.UUID) error {
	// A draft or previewed job has no worker, so cancelling it ends the job
	// right away and wipes the credentials it still holds.
	res, err := r.db.ExecContext(ctx, `UPDATE import_jobs SET status = $1,
		finished_at = now(), updated_at = now(), credentials_enc = NULL, cancel_requested = true
		WHERE id = $2 AND workspace_id = $3 AND status IN ($4, $5)`,
		domain.ImportStatusCancelled, id, workspaceID, domain.ImportStatusDraft, domain.ImportStatusPreviewed)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	res, err = r.db.ExecContext(ctx, `UPDATE import_jobs SET cancel_requested = true, updated_at = now()
		WHERE id = $1 AND workspace_id = $2 AND status IN ($3, $4)`,
		id, workspaceID, domain.ImportStatusQueued, domain.ImportStatusRunning)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrImportNotEditable
	}
	return nil
}

func (r *ImportJobRepository) IsCancelRequested(ctx context.Context, jobID uuid.UUID) (bool, error) {
	var requested bool
	err := r.db.QueryRowContext(ctx, `SELECT cancel_requested FROM import_jobs WHERE id = $1`, jobID).Scan(&requested)
	return requested, err
}

// Heartbeat refreshes a running job's updated_at so stale recovery does not
// fail a job that is still making progress.
func (r *ImportJobRepository) Heartbeat(ctx context.Context, jobID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `UPDATE import_jobs SET updated_at = now()
		WHERE id = $1 AND status = $2`, jobID, domain.ImportStatusRunning)
	return err
}

// FailStaleRunning recovers jobs left running by a crashed instance, using the
// last heartbeat (updated_at) rather than the time the job started. Their
// credentials are cleared in the same update.
func (r *ImportJobRepository) FailStaleRunning(ctx context.Context, before time.Time) (int64, error) {
	res, err := r.db.ExecContext(ctx, `UPDATE import_jobs SET status = $1,
		error = 'import interrupted by a server restart', finished_at = now(),
		updated_at = now(), credentials_enc = NULL
		WHERE status = $2 AND updated_at < $3`, domain.ImportStatusFailed, domain.ImportStatusRunning, before)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ExpireIdle cancels draft and previewed jobs left untouched for too long and
// wipes their credentials in the same update. A terminal job keeps its report
// for the user to read, but never keeps a usable secret.
func (r *ImportJobRepository) ExpireIdle(ctx context.Context, before time.Time) (int64, error) {
	res, err := r.db.ExecContext(ctx, `UPDATE import_jobs SET status = $1,
		error = 'credentials expired after the job was idle for 24 hours',
		finished_at = now(), updated_at = now(), credentials_enc = NULL
		WHERE status IN ($2, $3) AND updated_at < $4`,
		domain.ImportStatusCancelled, domain.ImportStatusDraft, domain.ImportStatusPreviewed, before)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (r *ImportJobRepository) GetRef(ctx context.Context, workspaceID uuid.UUID, source, site, extType, extID string) (*domain.ImportExternalRef, error) {
	var ref domain.ImportExternalRef
	err := r.db.QueryRowContext(ctx, `SELECT id, workspace_id, source, source_site, external_type,
		external_id, external_key, external_url, kuayle_id, content_hash, last_job_id, created_at, updated_at
		FROM import_external_refs WHERE workspace_id = $1 AND source = $2 AND source_site = $3
		AND external_type = $4 AND external_id = $5`, workspaceID, source, site, extType, extID).Scan(
		&ref.ID, &ref.WorkspaceID, &ref.Source, &ref.SourceSite, &ref.ExternalType, &ref.ExternalID,
		&ref.ExternalKey, &ref.ExternalURL, &ref.KuayleID, &ref.ContentHash, &ref.LastJobID, &ref.CreatedAt, &ref.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ref, nil
}

// UpsertRef inserts or refreshes a mapping. Passing tx applies the write in
// the caller's transaction so the ref and the entity are committed together.
func (r *ImportJobRepository) UpsertRef(ctx context.Context, tx *sqlx.Tx, ref *domain.ImportExternalRef) error {
	var exec importQuerier = r.db
	if tx != nil {
		exec = tx
	}
	return exec.QueryRowContext(ctx, `INSERT INTO import_external_refs
		(workspace_id, source, source_site, external_type, external_id, external_key,
		 external_url, kuayle_id, content_hash, last_job_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (workspace_id, source, source_site, external_type, external_id) DO UPDATE SET
			external_key = EXCLUDED.external_key, external_url = EXCLUDED.external_url,
			kuayle_id = EXCLUDED.kuayle_id, content_hash = EXCLUDED.content_hash,
			last_job_id = EXCLUDED.last_job_id, updated_at = now()
		RETURNING id, created_at, updated_at`,
		ref.WorkspaceID, ref.Source, ref.SourceSite, ref.ExternalType, ref.ExternalID, ref.ExternalKey,
		ref.ExternalURL, ref.KuayleID, ref.ContentHash, ref.LastJobID,
	).Scan(&ref.ID, &ref.CreatedAt, &ref.UpdatedAt)
}

// SetIssueTimestamps restores the source created/updated dates on an imported
// issue in the same transaction that created it. The workspace predicate keeps
// a job from touching an issue outside its workspace.
func (r *ImportJobRepository) SetIssueTimestamps(ctx context.Context, tx *sqlx.Tx, workspaceID, issueID uuid.UUID, createdAt, updatedAt time.Time) error {
	var exec sqlx.ExtContext = r.db
	if tx != nil {
		exec = tx
	}
	_, err := exec.ExecContext(ctx, `UPDATE issue_records SET created_at = $1, updated_at = $2
		WHERE id = $3 AND workspace_id = $4`, createdAt, updatedAt, issueID, workspaceID)
	return err
}

// SetCommentTimestamps restores the source dates on an imported comment.
func (r *ImportJobRepository) SetCommentTimestamps(ctx context.Context, tx *sqlx.Tx, workspaceID, commentID uuid.UUID, createdAt, updatedAt time.Time) error {
	var exec sqlx.ExtContext = r.db
	if tx != nil {
		exec = tx
	}
	_, err := exec.ExecContext(ctx, `UPDATE comments SET created_at = $1, updated_at = $2
		WHERE id = $3 AND issue_id IN (SELECT id FROM issue_records WHERE workspace_id = $4)`,
		createdAt, updatedAt, commentID, workspaceID)
	return err
}

func nullableJSON(raw json.RawMessage) any {
	if len(raw) == 0 {
		return []byte("{}")
	}
	return []byte(raw)
}

func scanImportJob(j *domain.ImportJob) []any {
	return []any{
		&j.ID, &j.Seq, &j.WorkspaceID, &j.CreatedBy, &j.Source, &j.SourceSite, &j.Status,
		&j.Config, &j.Mapping, nullableRawJSON{&j.Preview}, &j.CredentialsEnc, &j.Total, &j.Processed, &j.CreatedCount,
		&j.UpdatedCount, &j.SkippedCount, &j.FailedCount, &j.Error, &j.CancelRequested, &j.CreatedAt,
		&j.UpdatedAt, &j.StartedAt, &j.FinishedAt,
	}
}
