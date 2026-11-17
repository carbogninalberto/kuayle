package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// ImportSource identifies a foreign tracker. Only Jira ships in this
// deliverable; the column is a CHECK-constrained enum so new importers can be
// added without changing the schema.
const (
	ImportSourceJira = "jira"
)

// Import job lifecycle. draft jobs hold credentials and may be previewed;
// previewed jobs may be mapped and started; queued/running are worker states
// and the rest are terminal.
const (
	ImportStatusDraft     = "draft"
	ImportStatusPreviewed = "previewed"
	ImportStatusQueued    = "queued"
	ImportStatusRunning   = "running"
	ImportStatusCompleted = "completed"
	ImportStatusFailed    = "failed"
	ImportStatusCancelled = "cancelled"
)

// External entity types stored in import_external_refs.
const (
	ImportExternalIssue      = "issue"
	ImportExternalComment    = "comment"
	ImportExternalAttachment = "attachment"
	ImportExternalRelation   = "relation"
	ImportExternalCycle      = "cycle"
	ImportExternalProject    = "project"
	ImportExternalLabel      = "label"
)

// Item report outcomes.
const (
	ImportOutcomeFailed  = "failed"
	ImportOutcomeSkipped = "skipped"
)

// ImportJob is one background import. Config, mapping and preview are opaque
// JSON interpreted by the importer; credentials_enc is AES-GCM ciphertext of
// the source credentials and is NULL in every terminal state.
type ImportJob struct {
	ID              uuid.UUID       `db:"id" json:"id"`
	Seq             int64           `db:"seq" json:"-"`
	WorkspaceID     uuid.UUID       `db:"workspace_id" json:"workspace_id"`
	CreatedBy       *uuid.UUID      `db:"created_by" json:"created_by"`
	Source          string          `db:"source" json:"source"`
	SourceSite      string          `db:"source_site" json:"source_site"`
	Status          string          `db:"status" json:"status"`
	Config          json.RawMessage `db:"config" json:"config"`
	Mapping         json.RawMessage `db:"mapping" json:"mapping"`
	Preview         json.RawMessage `db:"preview" json:"preview"`
	CredentialsEnc  *string         `db:"credentials_enc" json:"-"`
	Total           int             `db:"total" json:"total"`
	Processed       int             `db:"processed" json:"processed"`
	CreatedCount    int             `db:"created_count" json:"created_count"`
	UpdatedCount    int             `db:"updated_count" json:"updated_count"`
	SkippedCount    int             `db:"skipped_count" json:"skipped_count"`
	FailedCount     int             `db:"failed_count" json:"failed_count"`
	Error           *string         `db:"error" json:"error"`
	CancelRequested bool            `db:"cancel_requested" json:"cancel_requested"`
	CreatedAt       time.Time       `db:"created_at" json:"created_at"`
	UpdatedAt       time.Time       `db:"updated_at" json:"updated_at"`
	StartedAt       *time.Time      `db:"started_at" json:"started_at"`
	FinishedAt      *time.Time      `db:"finished_at" json:"finished_at"`
}

func (j *ImportJob) IsTerminal() bool {
	switch j.Status {
	case ImportStatusCompleted, ImportStatusFailed, ImportStatusCancelled:
		return true
	default:
		return false
	}
}

// ImportJobItem is one row of the per-item error report. Only skipped and
// failed items get a row; a successful import is silent.
type ImportJobItem struct {
	ID           uuid.UUID `db:"id" json:"id"`
	JobID        uuid.UUID `db:"job_id" json:"job_id"`
	WorkspaceID  uuid.UUID `db:"workspace_id" json:"workspace_id"`
	ExternalType string    `db:"external_type" json:"external_type"`
	ExternalID   string    `db:"external_id" json:"external_id"`
	ExternalKey  string    `db:"external_key" json:"external_key"`
	Outcome      string    `db:"outcome" json:"outcome"`
	Reason       string    `db:"reason" json:"reason"`
	CreatedAt    time.Time `db:"created_at" json:"created_at"`
}

// ImportExternalRef maps a source entity to the Kuayle entity it produced.
// It is deliberately independent of the job so a re-run can find and update
// the same issue after the original job has finished.
type ImportExternalRef struct {
	ID           uuid.UUID  `db:"id" json:"id"`
	WorkspaceID  uuid.UUID  `db:"workspace_id" json:"workspace_id"`
	Source       string     `db:"source" json:"source"`
	SourceSite   string     `db:"source_site" json:"source_site"`
	ExternalType string     `db:"external_type" json:"external_type"`
	ExternalID   string     `db:"external_id" json:"external_id"`
	ExternalKey  string     `db:"external_key" json:"external_key"`
	ExternalURL  *string    `db:"external_url" json:"external_url"`
	KuayleID     uuid.UUID  `db:"kuayle_id" json:"kuayle_id"`
	ContentHash  *string    `db:"content_hash" json:"content_hash"`
	LastJobID    *uuid.UUID `db:"last_job_id" json:"last_job_id"`
	CreatedAt    time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt    time.Time  `db:"updated_at" json:"updated_at"`
}
