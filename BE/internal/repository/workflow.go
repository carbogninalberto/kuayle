package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

var (
	// ErrWorkflowDenied: the actor is not a current owner or admin (writes), or
	// not a non-guest member (reads).
	ErrWorkflowDenied        = errors.New("workflow access denied")
	ErrWorkflowNotFound      = errors.New("workflow not found")
	ErrWorkflowNameTaken     = errors.New("workflow name taken")
	ErrWorkflowStale         = errors.New("workflow changed since it was loaded")
	ErrAgentProfileNotFound  = errors.New("agent profile not found")
	ErrAgentProfileNameTaken = errors.New("agent profile name taken")
	ErrAgentProfileInUse     = errors.New("agent profile is used by a workflow")
	ErrWorkflowScopeNotFound = errors.New("workflow default scope not found")
)

// WorkflowRepository stores agent profiles, workflows, their immutable
// versions and defaults. Every write re-checks that the actor is a current
// owner or admin inside the writing transaction and records an audit event.
type WorkflowRepository struct{ db *sqlx.DB }

func NewWorkflowRepository(db *sqlx.DB) *WorkflowRepository { return &WorkflowRepository{db} }

// MemberRole returns the actor's current workspace role, or "" when they are
// not a member.
func (r *WorkflowRepository) MemberRole(ctx context.Context, ws, actor uuid.UUID) (string, error) {
	var role string
	err := r.db.GetContext(ctx, &role, `SELECT role FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, ws, actor)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return role, err
}

// beginManage opens a write transaction for a current owner or admin.
func (r *WorkflowRepository) beginManage(ctx context.Context, ws, actor uuid.UUID) (*sqlx.Tx, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	var role string
	err = tx.GetContext(ctx, &role, `SELECT role FROM workspace_members WHERE workspace_id=$1 AND user_id=$2 AND role IN ('owner','admin') FOR SHARE`, ws, actor)
	if err != nil {
		tx.Rollback()
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrWorkflowDenied
		}
		return nil, err
	}
	return tx, nil
}

func workflowAudit(ctx context.Context, tx *sqlx.Tx, ws uuid.UUID, actor *uuid.UUID, entity string, id uuid.UUID, action string, detail any) error {
	raw, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO workflow_audit_events(workspace_id,actor_id,entity,entity_id,action,detail) VALUES($1,$2,$3,$4,$5,$6)`, ws, actor, entity, id, action, raw)
	return err
}

func uniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "duplicate key value")
}

// Built-in profiles every workspace starts with.
var builtinProfiles = []struct{ key, name, role, instructions string }{
	{domain.AgentRoleImplementer, "Implementer", domain.AgentRoleImplementer, "Implement the issue completely, with tests for changed behaviour. Keep changes focused."},
	{domain.AgentRoleReviewer, "Reviewer", domain.AgentRoleReviewer, "Review the change for correctness, security, tests and clarity. Request changes only for problems that matter."},
}

// EnsureBuiltinProfiles creates the workspace's built-in profiles once. They
// use Claude Code; owners and admins can change their harness and model.
func (r *WorkflowRepository) EnsureBuiltinProfiles(ctx context.Context, ws uuid.UUID) error {
	for _, p := range builtinProfiles {
		if _, err := r.db.ExecContext(ctx, `INSERT INTO agent_profiles(workspace_id,name,role,harness,instructions,builtin_key)
 SELECT $1,$2,$3,'claude-code',$4,$5 WHERE NOT EXISTS (SELECT 1 FROM agent_profiles WHERE workspace_id=$1 AND (builtin_key=$5 OR (lower(name)=lower($2) AND archived_at IS NULL)))
 ON CONFLICT DO NOTHING`, ws, p.name, p.role, p.instructions, p.key); err != nil {
			return err
		}
	}
	return nil
}

func (r *WorkflowRepository) Profiles(ctx context.Context, ws uuid.UUID) ([]domain.AgentProfile, error) {
	profiles := []domain.AgentProfile{}
	err := r.db.SelectContext(ctx, &profiles, `SELECT * FROM agent_profiles WHERE workspace_id=$1 ORDER BY archived_at IS NOT NULL, builtin_key IS NULL, lower(name)`, ws)
	return profiles, err
}

// ProfilesByID returns every profile of the workspace, archived ones
// included, keyed by ID; running tasks keep using the profiles they pinned.
func profilesByID(ctx context.Context, q sqlx.QueryerContext, ws uuid.UUID, activeOnly bool) (map[uuid.UUID]domain.AgentProfile, error) {
	var rows []domain.AgentProfile
	query := `SELECT * FROM agent_profiles WHERE workspace_id=$1`
	if activeOnly {
		query += ` AND archived_at IS NULL`
	}
	if err := sqlx.SelectContext(ctx, q, &rows, query, ws); err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]domain.AgentProfile, len(rows))
	for _, p := range rows {
		out[p.ID] = p
	}
	return out, nil
}

// ActiveProfiles are the profiles a workflow version may reference.
func (r *WorkflowRepository) ActiveProfiles(ctx context.Context, ws uuid.UUID) (map[uuid.UUID]domain.AgentProfile, error) {
	return profilesByID(ctx, r.db, ws, true)
}

// AllProfiles includes archived profiles, which running tasks may still use.
func (r *WorkflowRepository) AllProfiles(ctx context.Context, ws uuid.UUID) (map[uuid.UUID]domain.AgentProfile, error) {
	return profilesByID(ctx, r.db, ws, false)
}

func (r *WorkflowRepository) CreateProfile(ctx context.Context, ws, actor uuid.UUID, in domain.AgentProfileInput) (*domain.AgentProfile, error) {
	tx, err := r.beginManage(ctx, ws, actor)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	profile := &domain.AgentProfile{}
	err = tx.GetContext(ctx, profile, `INSERT INTO agent_profiles(workspace_id,name,role,harness,model,effort,instructions,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING *`,
		ws, in.Name, in.Role, in.Harness, in.Model, in.Effort, in.Instructions, actor)
	if uniqueViolation(err) {
		return nil, ErrAgentProfileNameTaken
	}
	if err != nil {
		return nil, err
	}
	if err = workflowAudit(ctx, tx, ws, &actor, "agent_profile", profile.ID, "created", in); err != nil {
		return nil, err
	}
	return profile, tx.Commit()
}

func (r *WorkflowRepository) UpdateProfile(ctx context.Context, ws, id, actor uuid.UUID, in domain.AgentProfileInput) (*domain.AgentProfile, error) {
	tx, err := r.beginManage(ctx, ws, actor)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	profile := &domain.AgentProfile{}
	err = tx.GetContext(ctx, profile, `UPDATE agent_profiles SET name=$3,role=$4,harness=$5,model=$6,effort=$7,instructions=$8,updated_at=now() WHERE id=$1 AND workspace_id=$2 AND archived_at IS NULL RETURNING *`,
		id, ws, in.Name, in.Role, in.Harness, in.Model, in.Effort, in.Instructions)
	if uniqueViolation(err) {
		return nil, ErrAgentProfileNameTaken
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAgentProfileNotFound
	}
	if err != nil {
		return nil, err
	}
	if err = workflowAudit(ctx, tx, ws, &actor, "agent_profile", id, "updated", in); err != nil {
		return nil, err
	}
	return profile, tx.Commit()
}

// ArchiveProfile hides a profile from new workflow versions. A profile the
// latest version of an active workflow uses cannot be archived.
func (r *WorkflowRepository) ArchiveProfile(ctx context.Context, ws, id, actor uuid.UUID) error {
	tx, err := r.beginManage(ctx, ws, actor)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var used bool
	if err = tx.GetContext(ctx, &used, `SELECT EXISTS(SELECT 1 FROM workflows w JOIN workflow_versions v ON v.workflow_id=w.id AND v.version=w.latest_version
 WHERE w.workspace_id=$1 AND w.archived_at IS NULL AND v.graph->'nodes' @> jsonb_build_array(jsonb_build_object('profile_id',$2::text)))`, ws, id); err != nil {
		return err
	}
	if used {
		return ErrAgentProfileInUse
	}
	res, err := tx.ExecContext(ctx, `UPDATE agent_profiles SET archived_at=now(),updated_at=now() WHERE id=$1 AND workspace_id=$2 AND archived_at IS NULL`, id, ws)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrAgentProfileNotFound
	}
	if err = workflowAudit(ctx, tx, ws, &actor, "agent_profile", id, "archived", map[string]any{}); err != nil {
		return err
	}
	return tx.Commit()
}

// WorkflowDetail is a workflow with its latest graph.
type WorkflowDetail struct {
	domain.Workflow
	VersionID *uuid.UUID      `json:"version_id" db:"version_id"`
	Graph     json.RawMessage `json:"graph" db:"graph"`
}

const workflowDetailSelect = `SELECT w.*,v.id AS version_id,v.graph FROM workflows w LEFT JOIN workflow_versions v ON v.workflow_id=w.id AND v.version=w.latest_version`

func (r *WorkflowRepository) Workflows(ctx context.Context, ws uuid.UUID) ([]WorkflowDetail, error) {
	out := []WorkflowDetail{}
	err := r.db.SelectContext(ctx, &out, workflowDetailSelect+` WHERE w.workspace_id=$1 ORDER BY w.archived_at IS NOT NULL, lower(w.name)`, ws)
	return out, err
}

func (r *WorkflowRepository) Workflow(ctx context.Context, ws, id uuid.UUID) (*WorkflowDetail, error) {
	out := &WorkflowDetail{}
	err := r.db.GetContext(ctx, out, workflowDetailSelect+` WHERE w.workspace_id=$1 AND w.id=$2`, ws, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrWorkflowNotFound
	}
	return out, err
}

// Versions lists a workflow's saved versions, newest first.
func (r *WorkflowRepository) Versions(ctx context.Context, ws, id uuid.UUID) ([]domain.WorkflowVersion, error) {
	out := []domain.WorkflowVersion{}
	err := r.db.SelectContext(ctx, &out, `SELECT * FROM workflow_versions WHERE workspace_id=$1 AND workflow_id=$2 ORDER BY version DESC`, ws, id)
	return out, err
}

// Version reads one version of the workspace.
func (r *WorkflowRepository) Version(ctx context.Context, ws, id uuid.UUID) (*domain.WorkflowVersion, error) {
	out := &domain.WorkflowVersion{}
	err := r.db.GetContext(ctx, out, `SELECT * FROM workflow_versions WHERE workspace_id=$1 AND id=$2`, ws, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrWorkflowNotFound
	}
	return out, err
}

// validateGraphTx validates a graph against the profiles that exist now,
// read under the same transaction that saves it.
func validateGraphTx(ctx context.Context, tx *sqlx.Tx, ws uuid.UUID, raw json.RawMessage) (json.RawMessage, error) {
	graph, err := domain.ParseWorkflowGraph(raw)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `SELECT 1 FROM agent_profiles WHERE workspace_id=$1 FOR SHARE`, ws); err != nil {
		return nil, err
	}
	profiles, err := profilesByID(ctx, tx, ws, true)
	if err != nil {
		return nil, err
	}
	if issues := domain.ValidateWorkflowGraph(*graph, profiles); len(issues) > 0 {
		return nil, &domain.WorkflowInvalidError{Issues: issues}
	}
	return json.Marshal(graph)
}

// CreateWorkflow saves a new workflow and its first version. An invalid
// graph is refused with every validation issue.
func (r *WorkflowRepository) CreateWorkflow(ctx context.Context, ws, actor uuid.UUID, name, description string, graph json.RawMessage, action string) (*WorkflowDetail, error) {
	tx, err := r.beginManage(ctx, ws, actor)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	normalized, err := validateGraphTx(ctx, tx, ws, graph)
	if err != nil {
		return nil, err
	}
	var workflow domain.Workflow
	err = tx.GetContext(ctx, &workflow, `INSERT INTO workflows(workspace_id,name,description,latest_version,created_by) VALUES($1,$2,$3,1,$4) RETURNING *`, ws, name, description, actor)
	if uniqueViolation(err) {
		return nil, ErrWorkflowNameTaken
	}
	if err != nil {
		return nil, err
	}
	var versionID uuid.UUID
	if err = tx.GetContext(ctx, &versionID, `INSERT INTO workflow_versions(workflow_id,workspace_id,version,graph,created_by) VALUES($1,$2,1,$3,$4) RETURNING id`, workflow.ID, ws, normalized, actor); err != nil {
		return nil, err
	}
	if err = workflowAudit(ctx, tx, ws, &actor, "workflow", workflow.ID, action, map[string]any{"name": name, "version": 1}); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &WorkflowDetail{Workflow: workflow, VersionID: &versionID, Graph: normalized}, nil
}

// SaveVersion stores the graph as the next version, provided the workflow is
// still at expectedVersion, and optionally renames it.
func (r *WorkflowRepository) SaveVersion(ctx context.Context, ws, id, actor uuid.UUID, expectedVersion int, name, description string, graph json.RawMessage) (*WorkflowDetail, error) {
	tx, err := r.beginManage(ctx, ws, actor)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var workflow domain.Workflow
	err = tx.GetContext(ctx, &workflow, `SELECT * FROM workflows WHERE id=$1 AND workspace_id=$2 AND archived_at IS NULL FOR UPDATE`, id, ws)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrWorkflowNotFound
	}
	if err != nil {
		return nil, err
	}
	if workflow.LatestVersion != expectedVersion {
		return nil, ErrWorkflowStale
	}
	normalized, err := validateGraphTx(ctx, tx, ws, graph)
	if err != nil {
		return nil, err
	}
	next := workflow.LatestVersion + 1
	var versionID uuid.UUID
	if err = tx.GetContext(ctx, &versionID, `INSERT INTO workflow_versions(workflow_id,workspace_id,version,graph,created_by) VALUES($1,$2,$3,$4,$5) RETURNING id`, id, ws, next, normalized, actor); err != nil {
		return nil, err
	}
	err = tx.GetContext(ctx, &workflow, `UPDATE workflows SET latest_version=$3,name=$4,description=$5,updated_at=now() WHERE id=$1 AND workspace_id=$2 RETURNING *`, id, ws, next, name, description)
	if uniqueViolation(err) {
		return nil, ErrWorkflowNameTaken
	}
	if err != nil {
		return nil, err
	}
	if err = workflowAudit(ctx, tx, ws, &actor, "workflow", id, "version_saved", map[string]any{"name": name, "version": next}); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &WorkflowDetail{Workflow: workflow, VersionID: &versionID, Graph: normalized}, nil
}

// ArchiveWorkflow hides a workflow from new tasks and removes the defaults
// that chose it (those scopes fall back to the next default, then Standard).
// Tasks already running keep their pinned version.
func (r *WorkflowRepository) ArchiveWorkflow(ctx context.Context, ws, id, actor uuid.UUID) error {
	tx, err := r.beginManage(ctx, ws, actor)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE workflows SET archived_at=now(),updated_at=now() WHERE id=$1 AND workspace_id=$2 AND archived_at IS NULL`, id, ws)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrWorkflowNotFound
	}
	var cleared []domain.WorkflowDefault
	if err = tx.SelectContext(ctx, &cleared, `DELETE FROM workflow_defaults WHERE workspace_id=$1 AND workflow_id=$2 RETURNING workspace_id,scope,scope_id,workflow_id`, ws, id); err != nil {
		return err
	}
	if err = workflowAudit(ctx, tx, ws, &actor, "workflow", id, "archived", map[string]any{"cleared_defaults": cleared}); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *WorkflowRepository) Defaults(ctx context.Context, ws uuid.UUID) ([]domain.WorkflowDefault, error) {
	out := []domain.WorkflowDefault{}
	err := r.db.SelectContext(ctx, &out, `SELECT workspace_id,scope,scope_id,workflow_id FROM workflow_defaults WHERE workspace_id=$1 ORDER BY scope,scope_id`, ws)
	return out, err
}

// SetDefault chooses a scope's workflow: a nil workflow chooses Standard for
// the workspace, or clears a team or project override (inherit).
func (r *WorkflowRepository) SetDefault(ctx context.Context, ws, actor uuid.UUID, scope string, scopeID uuid.UUID, workflow *uuid.UUID, inherit bool) error {
	tx, err := r.beginManage(ctx, ws, actor)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists bool
	switch scope {
	case domain.WorkflowScopeWorkspace:
		exists = scopeID == ws
	case domain.WorkflowScopeTeam:
		err = tx.GetContext(ctx, &exists, `SELECT EXISTS(SELECT 1 FROM teams WHERE id=$1 AND workspace_id=$2)`, scopeID, ws)
	case domain.WorkflowScopeProject:
		err = tx.GetContext(ctx, &exists, `SELECT EXISTS(SELECT 1 FROM projects WHERE id=$1 AND workspace_id=$2)`, scopeID, ws)
	}
	if err != nil {
		return err
	}
	if !exists {
		return ErrWorkflowScopeNotFound
	}
	if workflow != nil {
		var active bool
		if err = tx.GetContext(ctx, &active, `SELECT EXISTS(SELECT 1 FROM workflows WHERE id=$1 AND workspace_id=$2 AND archived_at IS NULL FOR SHARE)`, workflow, ws); err != nil {
			return err
		}
		if !active {
			return ErrWorkflowNotFound
		}
	}
	if inherit {
		_, err = tx.ExecContext(ctx, `DELETE FROM workflow_defaults WHERE workspace_id=$1 AND scope=$2 AND scope_id=$3`, ws, scope, scopeID)
	} else {
		_, err = tx.ExecContext(ctx, `INSERT INTO workflow_defaults(workspace_id,scope,scope_id,workflow_id,updated_by) VALUES($1,$2,$3,$4,$5)
 ON CONFLICT (workspace_id,scope,scope_id) DO UPDATE SET workflow_id=EXCLUDED.workflow_id,updated_by=EXCLUDED.updated_by,updated_at=now()`, ws, scope, scopeID, workflow, actor)
	}
	if err != nil {
		return err
	}
	if err = workflowAudit(ctx, tx, ws, &actor, "workflow_default", scopeID, "default_set", map[string]any{"scope": scope, "workflow_id": workflow, "inherit": inherit}); err != nil {
		return err
	}
	return tx.Commit()
}

// AuditEvents lists the newest audit events of an entity.
func (r *WorkflowRepository) AuditEvents(ctx context.Context, ws uuid.UUID, entity string, id uuid.UUID) ([]WorkflowAuditEvent, error) {
	out := []WorkflowAuditEvent{}
	err := r.db.SelectContext(ctx, &out, `SELECT id,actor_id,action,detail,created_at FROM workflow_audit_events WHERE workspace_id=$1 AND entity=$2 AND entity_id=$3 ORDER BY id DESC LIMIT 100`, ws, entity, id)
	return out, err
}

type WorkflowAuditEvent struct {
	ID        int64           `json:"id" db:"id"`
	ActorID   *uuid.UUID      `json:"actor_id" db:"actor_id"`
	Action    string          `json:"action" db:"action"`
	Detail    json.RawMessage `json:"detail" db:"detail"`
	CreatedAt sql.NullTime    `json:"-" db:"created_at"`
}

// ResolveWorkflow returns the workflow version a new task on the issue runs:
// the explicitly chosen workflow, else the issue's project, team and
// workspace defaults, the most specific first. nil means Standard.
func resolveWorkflowVersion(ctx context.Context, q sqlx.QueryerContext, ws, issue uuid.UUID, chosen *string) (*domain.WorkflowVersion, error) {
	var workflowID *uuid.UUID
	if chosen != nil {
		if *chosen == domain.StandardWorkflowID || *chosen == "" {
			return nil, nil
		}
		id, err := uuid.Parse(*chosen)
		if err != nil {
			return nil, ErrWorkflowNotFound
		}
		workflowID = &id
	} else {
		var row struct {
			Found      bool       `db:"found"`
			WorkflowID *uuid.UUID `db:"workflow_id"`
		}
		err := sqlx.GetContext(ctx, q, &row, `SELECT true AS found,d.workflow_id FROM issues i
 JOIN workflow_defaults d ON d.workspace_id=i.workspace_id AND ((d.scope='project' AND d.scope_id=i.project_id) OR (d.scope='team' AND d.scope_id=i.team_id) OR (d.scope='workspace' AND d.scope_id=i.workspace_id))
 WHERE i.id=$1 AND i.workspace_id=$2
 ORDER BY CASE d.scope WHEN 'project' THEN 0 WHEN 'team' THEN 1 ELSE 2 END LIMIT 1`, issue, ws)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && row.WorkflowID == nil) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		workflowID = row.WorkflowID
	}
	version := &domain.WorkflowVersion{}
	err := sqlx.GetContext(ctx, q, version, `SELECT v.* FROM workflows w JOIN workflow_versions v ON v.workflow_id=w.id AND v.version=w.latest_version
 WHERE w.id=$1 AND w.workspace_id=$2 AND w.archived_at IS NULL`, workflowID, ws)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrWorkflowNotFound
	}
	return version, err
}

// ResolveWorkflow is resolveWorkflowVersion outside a transaction.
func (r *WorkflowRepository) ResolveWorkflow(ctx context.Context, ws, issue uuid.UUID, chosen *string) (*domain.WorkflowVersion, error) {
	return resolveWorkflowVersion(ctx, r.db, ws, issue, chosen)
}
