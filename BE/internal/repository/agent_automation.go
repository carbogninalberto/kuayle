package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

var ErrAutomationNotFound = errors.New("automation not found")

type AgentAutomationRepository struct{ db *sqlx.DB }

func NewAgentAutomationRepository(db *sqlx.DB) *AgentAutomationRepository {
	return &AgentAutomationRepository{db: db}
}

const runViewColumns = `r.*, a.name AS automation_name, a.owner_id, a.instructions, a.team_id, i.identifier_text AS issue_identifier, i.title AS issue_title`

func (r *AgentAutomationRepository) List(ctx context.Context, ws uuid.UUID) ([]domain.AgentAutomation, error) {
	automations := []domain.AgentAutomation{}
	err := r.db.SelectContext(ctx, &automations, `SELECT * FROM agent_automations WHERE workspace_id=$1 ORDER BY created_at, id`, ws)
	return automations, err
}

// Create stores a rule. The label must belong to the workspace and the status
// to the rule's team, which must belong to the workspace.
func (r *AgentAutomationRepository) Create(ctx context.Context, a *domain.AgentAutomation) error {
	err := r.db.GetContext(ctx, a, `INSERT INTO agent_automations(workspace_id,team_id,owner_id,name,label_id,status_id,instructions,unattended,run_hour,max_active)
 SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9,$10 FROM teams t WHERE t.id=$2 AND t.workspace_id=$1
 AND ($5::uuid IS NULL OR EXISTS(SELECT 1 FROM labels l WHERE l.id=$5 AND l.workspace_id=$1))
 AND ($6::uuid IS NULL OR EXISTS(SELECT 1 FROM team_statuses s WHERE s.id=$6 AND s.team_id=$2))
 RETURNING *`, a.WorkspaceID, a.TeamID, a.OwnerID, a.Name, a.LabelID, a.StatusID, a.Instructions, a.Unattended, a.RunHour, a.MaxActive)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrAutomationNotFound
	}
	return err
}

func (r *AgentAutomationRepository) SetEnabled(ctx context.Context, ws, id uuid.UUID, enabled bool) (*domain.AgentAutomation, error) {
	automation := &domain.AgentAutomation{}
	err := r.db.GetContext(ctx, automation, `UPDATE agent_automations SET enabled=$3,updated_at=now() WHERE id=$1 AND workspace_id=$2 RETURNING *`, id, ws, enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAutomationNotFound
	}
	return automation, err
}

func (r *AgentAutomationRepository) Delete(ctx context.Context, ws, id uuid.UUID) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM agent_automations WHERE id=$1 AND workspace_id=$2`, id, ws)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return ErrAutomationNotFound
	}
	return nil
}

// Runs lists a workspace's recent runs, newest first.
func (r *AgentAutomationRepository) Runs(ctx context.Context, ws uuid.UUID, limit int) ([]domain.AgentAutomationRunView, error) {
	runs := []domain.AgentAutomationRunView{}
	err := r.db.SelectContext(ctx, &runs, `SELECT `+runViewColumns+` FROM agent_automation_runs r JOIN agent_automations a ON a.id=r.automation_id JOIN issues i ON i.id=r.issue_id
 WHERE r.workspace_id=$1 ORDER BY r.created_at DESC, r.id LIMIT $2`, ws, limit)
	return runs, err
}

// Claim records new runs for every enabled rule whose matching issues have no
// run yet and no active agent task, within each rule's active-task budget and
// schedule. Rules are locked with SKIP LOCKED so concurrent API instances claim
// disjoint work; the (rule, issue) key makes every match happen at most once.
func (r *AgentAutomationRepository) Claim(ctx context.Context, limit int) ([]domain.AgentAutomationRunView, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var automations []domain.AgentAutomation
	if err = tx.SelectContext(ctx, &automations, `SELECT a.* FROM agent_automations a JOIN dev_machine_workspace_policies p ON p.workspace_id=a.workspace_id AND p.enabled
 WHERE a.enabled AND (a.run_hour IS NULL OR a.run_hour=extract(hour FROM now() AT TIME ZONE 'UTC'))
 ORDER BY a.updated_at LIMIT $1 FOR UPDATE OF a SKIP LOCKED`, limit); err != nil {
		return nil, err
	}
	claimed := []domain.AgentAutomationRunView{}
	for _, a := range automations {
		var active int
		if err = tx.GetContext(ctx, &active, `SELECT count(*) FROM agent_automation_runs r LEFT JOIN agent_tasks t ON t.id=r.task_id
 WHERE r.automation_id=$1 AND (r.status IN ('pending_approval','starting') OR (r.status='started' AND t.state NOT IN ('completed','cancelled','failed')))`, a.ID); err != nil {
			return nil, err
		}
		if active >= a.MaxActive {
			continue
		}
		var ids []uuid.UUID
		if err = tx.SelectContext(ctx, &ids, `INSERT INTO agent_automation_runs(automation_id,workspace_id,issue_id,status,actor_id)
 SELECT a.id,a.workspace_id,i.id,
  CASE WHEN a.unattended AND p.allow_unattended_automations THEN 'starting' ELSE 'pending_approval' END,
  CASE WHEN a.unattended AND p.allow_unattended_automations THEN a.owner_id END
 FROM agent_automations a
 JOIN dev_machine_workspace_policies p ON p.workspace_id=a.workspace_id
 JOIN issues i ON i.team_id=a.team_id AND i.workspace_id=a.workspace_id
 JOIN team_statuses s ON s.id=i.status_id
 WHERE a.id=$1
 AND (a.status_id IS NULL OR i.status_id=a.status_id)
 AND (a.status_id IS NOT NULL OR s.category NOT IN ('completed','cancelled'))
 AND (a.label_id IS NULL OR EXISTS(SELECT 1 FROM issue_labels l WHERE l.issue_id=i.id AND l.label_id=a.label_id))
 AND NOT EXISTS(SELECT 1 FROM agent_automation_runs r WHERE r.automation_id=a.id AND r.issue_id=i.id)
 AND NOT EXISTS(SELECT 1 FROM agent_tasks t WHERE t.issue_id=i.id AND t.state NOT IN ('completed','cancelled','failed'))
 ORDER BY i.created_at, i.id LIMIT $2
 ON CONFLICT (automation_id, issue_id) DO NOTHING RETURNING id`, a.ID, a.MaxActive-active); err != nil {
			return nil, err
		}
		if len(ids) == 0 {
			continue
		}
		query, args, err := sqlx.In(`SELECT `+runViewColumns+` FROM agent_automation_runs r JOIN agent_automations a ON a.id=r.automation_id JOIN issues i ON i.id=r.issue_id WHERE r.id IN (?)`, ids)
		if err != nil {
			return nil, err
		}
		var runs []domain.AgentAutomationRunView
		if err = tx.SelectContext(ctx, &runs, tx.Rebind(query), args...); err != nil {
			return nil, err
		}
		claimed = append(claimed, runs...)
	}
	return claimed, tx.Commit()
}

// Starting returns runs waiting to start, including ones a crash interrupted.
func (r *AgentAutomationRepository) Starting(ctx context.Context, limit int) ([]domain.AgentAutomationRunView, error) {
	runs := []domain.AgentAutomationRunView{}
	err := r.db.SelectContext(ctx, &runs, `SELECT `+runViewColumns+` FROM agent_automation_runs r JOIN agent_automations a ON a.id=r.automation_id JOIN issues i ON i.id=r.issue_id
 WHERE r.status='starting' ORDER BY r.updated_at LIMIT $1`, limit)
	return runs, err
}

// Decide approves (acting as the approver) or dismisses a pending run. Only
// the rule's owner or a workspace administrator may decide.
func (r *AgentAutomationRepository) Decide(ctx context.Context, ws, id, actor uuid.UUID, admin, approve bool) (*domain.AgentAutomationRunView, error) {
	status := "dismissed"
	if approve {
		status = "starting"
	}
	var updated uuid.UUID
	err := r.db.GetContext(ctx, &updated, `UPDATE agent_automation_runs r SET status=$4,actor_id=CASE WHEN $4='starting' THEN $3::uuid ELSE r.actor_id END,updated_at=now()
 FROM agent_automations a WHERE r.id=$1 AND r.workspace_id=$2 AND a.id=r.automation_id AND r.status='pending_approval' AND ($5 OR a.owner_id=$3)
 RETURNING r.id`, id, ws, actor, status, admin)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAutomationNotFound
	}
	if err != nil {
		return nil, err
	}
	return r.Run(ctx, ws, updated)
}

func (r *AgentAutomationRepository) Run(ctx context.Context, ws, id uuid.UUID) (*domain.AgentAutomationRunView, error) {
	run := &domain.AgentAutomationRunView{}
	err := r.db.GetContext(ctx, run, `SELECT `+runViewColumns+` FROM agent_automation_runs r JOIN agent_automations a ON a.id=r.automation_id JOIN issues i ON i.id=r.issue_id WHERE r.id=$1 AND r.workspace_id=$2`, id, ws)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAutomationNotFound
	}
	return run, err
}

// Finish settles a starting run once; a concurrent finisher is a no-op.
func (r *AgentAutomationRepository) Finish(ctx context.Context, id uuid.UUID, task *uuid.UUID, failure string) error {
	status, message := "started", sql.NullString{}
	if task == nil {
		status, message = "failed", sql.NullString{String: failure, Valid: true}
	}
	_, err := r.db.ExecContext(ctx, `UPDATE agent_automation_runs SET status=$2,task_id=$3,error=left($4,1024),updated_at=now() WHERE id=$1 AND status='starting'`, id, status, task, message)
	return err
}
