package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/lib/pq"
)

type WorkspaceTransferTableSpec struct {
	Name       string
	SelectSQL  string
	IDColumn   string
	UserFields []string
}

// WorkspaceTransferTableSpecs is dependency ordered for import. Every indirect
// table is selected through a workspace-owned parent to prevent cross-tenant
// rows from entering an export.
func WorkspaceTransferTableSpecs() []WorkspaceTransferTableSpec {
	return []WorkspaceTransferTableSpec{
		{Name: "workspace_members", SelectSQL: `SELECT to_jsonb(x)::text FROM workspace_members x WHERE workspace_id=$1 ORDER BY created_at,user_id`, UserFields: []string{"user_id"}},
		{Name: "teams", SelectSQL: `SELECT to_jsonb(x)::text FROM teams x WHERE workspace_id=$1 ORDER BY created_at,id`, IDColumn: "id"},
		{Name: "team_members", SelectSQL: `SELECT to_jsonb(x)::text FROM team_members x JOIN teams p ON p.id=x.team_id WHERE p.workspace_id=$1 ORDER BY x.created_at,x.team_id,x.user_id`, UserFields: []string{"user_id"}},
		{Name: "team_statuses", SelectSQL: `SELECT to_jsonb(x)::text FROM team_statuses x JOIN teams p ON p.id=x.team_id WHERE p.workspace_id=$1 ORDER BY x.team_id,x.position,x.id`, IDColumn: "id"},
		{Name: "labels", SelectSQL: `SELECT to_jsonb(x)::text FROM labels x WHERE workspace_id=$1 ORDER BY created_at,id`, IDColumn: "id"},
		{Name: "projects", SelectSQL: `SELECT to_jsonb(x)::text FROM projects x WHERE workspace_id=$1 ORDER BY created_at,id`, IDColumn: "id", UserFields: []string{"lead_id"}},
		{Name: "project_members", SelectSQL: `SELECT to_jsonb(x)::text FROM project_members x JOIN projects p ON p.id=x.project_id WHERE p.workspace_id=$1 ORDER BY x.created_at,x.project_id,x.user_id`, UserFields: []string{"user_id"}},
		{Name: "project_status_visibility", SelectSQL: `SELECT to_jsonb(x)::text FROM project_status_visibility x JOIN projects p ON p.id=x.project_id WHERE p.workspace_id=$1 ORDER BY x.project_id,x.status_id`},
		{Name: "cycles", SelectSQL: `SELECT to_jsonb(x)::text FROM cycles x JOIN teams p ON p.id=x.team_id WHERE p.workspace_id=$1 ORDER BY x.team_id,x.number,x.id`, IDColumn: "id"},
		{Name: "issues", SelectSQL: `SELECT to_jsonb(x)::text FROM issues x WHERE workspace_id=$1 ORDER BY created_at,id`, IDColumn: "id", UserFields: []string{"creator_id", "assignee_id"}},
		{Name: "issue_labels", SelectSQL: `SELECT to_jsonb(x)::text FROM issue_labels x JOIN issues p ON p.id=x.issue_id WHERE p.workspace_id=$1 ORDER BY x.issue_id,x.label_id`},
		{Name: "issue_assignees", SelectSQL: `SELECT to_jsonb(x)::text FROM issue_assignees x JOIN issues p ON p.id=x.issue_id WHERE p.workspace_id=$1 ORDER BY x.issue_id,x.user_id`, UserFields: []string{"user_id"}},
		{Name: "issue_subscribers", SelectSQL: `SELECT to_jsonb(x)::text FROM issue_subscribers x JOIN issues p ON p.id=x.issue_id WHERE p.workspace_id=$1 ORDER BY x.issue_id,x.user_id`, UserFields: []string{"user_id"}},
		{Name: "issue_relations", SelectSQL: `SELECT to_jsonb(x)::text FROM issue_relations x JOIN issues p ON p.id=x.issue_id WHERE p.workspace_id=$1 ORDER BY x.created_at,x.id`, IDColumn: "id"},
		{Name: "comments", SelectSQL: `SELECT to_jsonb(x)::text FROM comments x JOIN issues p ON p.id=x.issue_id WHERE p.workspace_id=$1 ORDER BY x.created_at,x.id`, IDColumn: "id", UserFields: []string{"user_id"}},
		{Name: "issue_history", SelectSQL: `SELECT to_jsonb(x)::text FROM issue_history x JOIN issues p ON p.id=x.issue_id WHERE p.workspace_id=$1 ORDER BY x.created_at,x.id`, IDColumn: "id", UserFields: []string{"user_id"}},
		{Name: "issue_lifecycle_events", SelectSQL: `SELECT to_jsonb(x)::text FROM issue_lifecycle_events x WHERE workspace_id=$1 ORDER BY created_at,id`, IDColumn: "id"},
		{Name: "issue_templates", SelectSQL: `SELECT to_jsonb(x)::text FROM issue_templates x WHERE workspace_id=$1 ORDER BY created_at,id`, IDColumn: "id", UserFields: []string{"assignee_id", "created_by"}},
		{Name: "issue_groups", SelectSQL: `SELECT to_jsonb(x)::text FROM issue_groups x WHERE workspace_id=$1 ORDER BY created_at,id`, IDColumn: "id"},
		{Name: "issue_group_items", SelectSQL: `SELECT to_jsonb(x)::text FROM issue_group_items x JOIN issue_groups p ON p.id=x.group_id WHERE p.workspace_id=$1 ORDER BY x.group_id,x.position,x.issue_id`},
		{Name: "views", SelectSQL: `SELECT to_jsonb(x)::text FROM views x WHERE workspace_id=$1 ORDER BY created_at,id`, IDColumn: "id", UserFields: []string{"creator_id"}},
		{Name: "favorites", SelectSQL: `SELECT to_jsonb(x)::text FROM favorites x WHERE workspace_id=$1 ORDER BY user_id,position,id`, IDColumn: "id", UserFields: []string{"user_id"}},
		{Name: "shared_links", SelectSQL: `SELECT to_jsonb(x)::text FROM shared_links x WHERE workspace_id=$1 ORDER BY created_at,id`, IDColumn: "id", UserFields: []string{"created_by"}},
		{Name: "notifications", SelectSQL: `SELECT to_jsonb(x)::text FROM notifications x WHERE workspace_id=$1 ORDER BY created_at,id`, IDColumn: "id", UserFields: []string{"user_id"}},
		{Name: "webhooks", SelectSQL: `SELECT to_jsonb(x)::text FROM webhooks x WHERE workspace_id=$1 ORDER BY created_at,id`, IDColumn: "id"},
		{Name: "github_installations", SelectSQL: `SELECT to_jsonb(x)::text FROM github_installations x WHERE workspace_id=$1 ORDER BY created_at,id`, IDColumn: "id", UserFields: []string{"installed_by"}},
		{Name: "github_repos", SelectSQL: `SELECT to_jsonb(x)::text FROM github_repos x WHERE workspace_id=$1 ORDER BY created_at,id`, IDColumn: "id"},
		{Name: "github_pull_requests", SelectSQL: `SELECT to_jsonb(x)::text FROM github_pull_requests x WHERE workspace_id=$1 ORDER BY created_at,id`, IDColumn: "id"},
		{Name: "github_branches", SelectSQL: `SELECT to_jsonb(x)::text FROM github_branches x WHERE workspace_id=$1 ORDER BY created_at,id`, IDColumn: "id"},
		{Name: "github_commits", SelectSQL: `SELECT to_jsonb(x)::text FROM github_commits x WHERE workspace_id=$1 ORDER BY created_at,id`, IDColumn: "id"},
		{Name: "github_auto_transitions", SelectSQL: `SELECT to_jsonb(x)::text FROM github_auto_transitions x WHERE workspace_id=$1 ORDER BY event,id`, IDColumn: "id"},
		{Name: "assets", SelectSQL: `SELECT to_jsonb(x)::text FROM assets x WHERE workspace_id=$1 ORDER BY created_at,id`, IDColumn: "id", UserFields: []string{"uploaded_by"}},
		{Name: "ai_settings", SelectSQL: `SELECT to_jsonb(x)::text FROM ai_settings x WHERE workspace_id=$1`},
		{Name: "dev_machine_workspace_policies", SelectSQL: `SELECT to_jsonb(x)::text FROM dev_machine_workspace_policies x WHERE workspace_id=$1`},
		{Name: "dev_machine_scope_settings", SelectSQL: `SELECT to_jsonb(x)::text FROM dev_machine_scope_settings x WHERE workspace_id=$1 ORDER BY created_at,id`, IDColumn: "id"},
	}
}

type WorkspaceTransferRepository struct {
	db       *sqlx.DB
	exportTx *sqlx.Tx
}

// ExportReader reuses the lock transaction's connection for all archive reads.
// Concurrent exports must not exhaust the pool holding one connection while
// waiting for a second connection to perform their raw table queries.
func (r *WorkspaceTransferRepository) ExportReader(tx *sqlx.Tx) *WorkspaceTransferRepository {
	return &WorkspaceTransferRepository{db: r.db, exportTx: tx}
}

func (r *WorkspaceTransferRepository) reader() sqlx.QueryerContext {
	if r.exportTx != nil {
		return r.exportTx
	}
	return r.db
}

var ErrPrivateWorkspaceTransfer = ErrPrivateWorkspaceOperation

// Hold the same workspace lock as privacy transitions until the archive has
// been assembled. A preflight check alone could race raw table/asset reads.
func (r *WorkspaceTransferRepository) BeginPublicExport(ctx context.Context, workspaceID uuid.UUID) (*sqlx.Tx, error) {
	return beginPublicWorkspaceOperation(ctx, r.db, workspaceID)
}

func NewWorkspaceTransferRepository(db *sqlx.DB) *WorkspaceTransferRepository {
	return &WorkspaceTransferRepository{db: db}
}

func decodeTransferRow(raw string) (dto.WorkspaceTransferRow, error) {
	var row dto.WorkspaceTransferRow
	if err := dto.DecodeTransferJSON([]byte(raw), &row); err != nil {
		return nil, err
	}
	return row, nil
}

func (r *WorkspaceTransferRepository) WorkspaceRow(ctx context.Context, workspaceID uuid.UUID) (dto.WorkspaceTransferRow, error) {
	var raw string
	if err := sqlx.GetContext(ctx, r.reader(), &raw, `SELECT to_jsonb(x)::text FROM workspaces x WHERE id=$1`, workspaceID); err != nil {
		return nil, err
	}
	return decodeTransferRow(raw)
}

func (r *WorkspaceTransferRepository) TableRows(ctx context.Context, spec WorkspaceTransferTableSpec, workspaceID uuid.UUID) ([]dto.WorkspaceTransferRow, error) {
	var rawRows []string
	if err := sqlx.SelectContext(ctx, r.reader(), &rawRows, spec.SelectSQL, workspaceID); err != nil {
		return nil, err
	}
	rows := make([]dto.WorkspaceTransferRow, 0, len(rawRows))
	for _, raw := range rawRows {
		row, err := decodeTransferRow(raw)
		if err != nil {
			return nil, fmt.Errorf("decode %s: %w", spec.Name, err)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func (r *WorkspaceTransferRepository) UsersByIDs(ctx context.Context, ids []uuid.UUID) ([]dto.WorkspaceTransferRow, error) {
	if len(ids) == 0 {
		return []dto.WorkspaceTransferRow{}, nil
	}
	query, args, err := sqlx.In(`SELECT jsonb_build_object(
		'id',id,'email',email,'name',name,'display_name',display_name,'avatar_url',avatar_url
	)::text FROM users WHERE id IN (?) ORDER BY email,id`, ids)
	if err != nil {
		return nil, err
	}
	var rawRows []string
	if err := sqlx.SelectContext(ctx, r.reader(), &rawRows, r.db.Rebind(query), args...); err != nil {
		return nil, err
	}
	rows := make([]dto.WorkspaceTransferRow, 0, len(rawRows))
	for _, raw := range rawRows {
		row, err := decodeTransferRow(raw)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func (r *WorkspaceTransferRepository) UsersByEmails(ctx context.Context, emails []string) (map[string]uuid.UUID, error) {
	result := make(map[string]uuid.UUID)
	if len(emails) == 0 {
		return result, nil
	}
	query, args, err := sqlx.In(`SELECT id,LOWER(email) AS email FROM users WHERE LOWER(email) IN (?)`, emails)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		ID    uuid.UUID `db:"id"`
		Email string    `db:"email"`
	}
	if err := r.db.SelectContext(ctx, &rows, r.db.Rebind(query), args...); err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.Email] = row.ID
	}
	return result, nil
}

func (r *WorkspaceTransferRepository) UserEmail(ctx context.Context, id uuid.UUID) (string, error) {
	var email string
	err := r.db.GetContext(ctx, &email, `SELECT LOWER(email) FROM users WHERE id=$1`, id)
	return email, err
}

func (r *WorkspaceTransferRepository) SlugExists(ctx context.Context, slug string) (bool, error) {
	var exists bool
	err := r.db.GetContext(ctx, &exists, `SELECT EXISTS(SELECT 1 FROM workspaces WHERE slug=$1)`, slug)
	return exists, err
}

func (r *WorkspaceTransferRepository) SchemaVersion(ctx context.Context) uint {
	var version uint
	if err := sqlx.GetContext(ctx, r.reader(), &version, `SELECT version FROM schema_migrations LIMIT 1`); err != nil {
		return 0
	}
	return version
}

func (r *WorkspaceTransferRepository) Begin(ctx context.Context) (*sqlx.Tx, error) {
	tx, err := r.db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, err
	}
	// Import creates teams and assets after other rows. Acquire classification
	// exclusivity before its first write instead of upgrading a shared barrier.
	// Asset bytes have already been staged before the service begins this tx.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(610046,0)`); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	return tx, nil
}

func (r *WorkspaceTransferRepository) Columns(ctx context.Context, tx *sqlx.Tx, table string) (map[string]bool, error) {
	var columns []string
	err := tx.SelectContext(ctx, &columns, `SELECT column_name FROM information_schema.columns
		WHERE table_schema='public' AND table_name=$1 AND is_generated='NEVER' AND is_identity='NO'`, table)
	if err != nil {
		return nil, err
	}
	result := make(map[string]bool, len(columns))
	for _, column := range columns {
		result[column] = true
	}
	return result, nil
}

func (r *WorkspaceTransferRepository) InsertRow(ctx context.Context, tx *sqlx.Tx, table string, row dto.WorkspaceTransferRow, allowed map[string]bool) error {
	columns := make([]string, 0, len(row))
	for column := range row {
		if allowed[column] {
			columns = append(columns, column)
		}
	}
	if len(columns) == 0 {
		return fmt.Errorf("%s row has no importable columns", table)
	}
	sort.Strings(columns)
	quoted := make([]string, len(columns))
	values := make([]string, len(columns))
	args := make([]any, len(columns))
	for i, column := range columns {
		quoted[i] = `"` + column + `"`
		values[i] = fmt.Sprintf("$%d", i+1)
		args[i] = transferDBValue(table, column, row[column])
	}
	query := fmt.Sprintf(`INSERT INTO "%s" (%s) VALUES (%s)`, table, strings.Join(quoted, ","), strings.Join(values, ","))
	_, err := tx.ExecContext(ctx, query, args...)
	if table == "workspaces" && err != nil {
		return workspaceCreateError(err)
	}
	return err
}

func transferDBValue(table, column string, value any) any {
	if value == nil {
		return nil
	}
	if number, ok := value.(json.Number); ok {
		return number.String()
	}
	if table == "webhooks" && column == "events" {
		items, _ := value.([]any)
		parts := make([]string, 0, len(items))
		for _, item := range items {
			parts = append(parts, fmt.Sprint(item))
		}
		return "{" + strings.Join(parts, ",") + "}"
	}
	if table == "notifications" && column == "source_team_ids" {
		items, _ := value.([]any)
		parts := make([]string, 0, len(items))
		for _, item := range items {
			parts = append(parts, fmt.Sprint(item))
		}
		return pq.Array(parts)
	}
	switch value.(type) {
	case map[string]any, []any:
		encoded, _ := json.Marshal(value)
		return string(encoded)
	default:
		return value
	}
}

func (r *WorkspaceTransferRepository) DeleteGeneratedLifecycle(ctx context.Context, tx *sqlx.Tx, workspaceID uuid.UUID) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM issue_lifecycle_events WHERE workspace_id=$1`, workspaceID)
	return err
}

func (r *WorkspaceTransferRepository) UpdateReference(ctx context.Context, tx *sqlx.Tx, table, idColumn string, id any, refColumn string, ref any) error {
	allowedTables := map[string]bool{"labels": true, "issues": true, "comments": true}
	allowedRefs := map[string]bool{"parent_id": true}
	if !allowedTables[table] || !allowedRefs[refColumn] || idColumn != "id" {
		return errors.New("unsupported deferred reference")
	}
	query := fmt.Sprintf(`UPDATE "%s" SET "%s"=$1 WHERE "%s"=$2`, table, refColumn, idColumn)
	_, err := tx.ExecContext(ctx, query, ref, id)
	return err
}
