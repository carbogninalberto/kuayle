package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// SwitchWorkflow says what moving the task to the newest version of its
// workflow would do, and with apply does it. It is for the task's creator,
// like pause and cancel, and is never automatic.
//
// A task is moved only while nothing acts on its workflow (paused, or
// waiting on a step) and only onto the step it is on, when the newest
// version still has that step doing the same kind of work. The task then
// follows the new prompts, profiles, transitions and limits. Visits of steps
// that still exist, the iterations used and the time already spent are
// kept, so the new limits and budget count what the task already did. The
// worker's lease is dropped: whatever it decided on the old version is
// refused, and it decides again on the new one.
//
// The returned task is nil unless the task was moved.
func (r *AgentTaskRepository) SwitchWorkflow(ctx context.Context, ws, id uuid.UUID, apply bool) (*domain.WorkflowSwitch, *domain.AgentTask, error) {
	actor, err := conversationActor(ctx, ws)
	if err != nil {
		return nil, nil, err
	}
	tx, task, err := r.transaction(ctx, id, true)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	plan := &domain.WorkflowSwitch{}
	if !task.RunsWorkflow() {
		plan.Refused = domain.WorkflowSwitchNoWorkflow
		return plan, nil, nil
	}
	var pinned, newest domain.WorkflowVersion
	if err = tx.GetContext(ctx, &pinned, `SELECT * FROM workflow_versions WHERE id=$1 AND workspace_id=$2`, *task.WorkflowVersionID, task.WorkspaceID); err != nil {
		return nil, nil, err
	}
	plan.FromVersion, plan.ToVersion = pinned.Version, pinned.Version
	// The workflow row is locked against a save, so the version read here is
	// the newest one when the task is moved. An archived workflow has no
	// newer version to move to.
	err = tx.GetContext(ctx, &newest, `SELECT v.* FROM workflows w JOIN workflow_versions v ON v.workflow_id=w.id AND v.version=w.latest_version
 WHERE w.id=$1 AND w.workspace_id=$2 AND w.archived_at IS NULL FOR SHARE OF w`, pinned.WorkflowID, task.WorkspaceID)
	if errors.Is(err, sql.ErrNoRows) || err == nil && newest.ID == pinned.ID {
		plan.Refused = domain.WorkflowSwitchNoNewer
		return plan, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	plan.ToVersion = newest.Version
	if plan.Refused = domain.WorkflowSwitchBlocked(*task); plan.Refused != "" {
		return plan, nil, nil
	}
	from, err := domain.ParseWorkflowGraph(pinned.Graph)
	if err != nil {
		return nil, nil, err
	}
	to, err := domain.ParseWorkflowGraph(newest.Graph)
	if err != nil {
		return nil, nil, err
	}
	var rows []domain.AgentProfile
	if err = tx.SelectContext(ctx, &rows, `SELECT * FROM agent_profiles WHERE workspace_id=$1`, task.WorkspaceID); err != nil {
		return nil, nil, err
	}
	profiles := make(map[uuid.UUID]domain.AgentProfile, len(rows))
	for _, profile := range rows {
		profiles[profile.ID] = profile
	}
	cursor := task.Cursor()
	node, refused := domain.MatchWorkflowNode(cursor.Node, *from, *to, profiles)
	if refused != "" {
		plan.Refused = refused
		return plan, nil, nil
	}
	plan.Node, plan.NodeLabel = node.ID, node.Label
	if !apply {
		return plan, nil, nil
	}
	visits, err := json.Marshal(domain.CarriedWorkflowVisits(cursor.Visits, *to))
	if err != nil {
		return nil, nil, err
	}
	if err = tx.GetContext(ctx, task, `UPDATE agent_tasks SET workflow_version_id=$2,workflow_visits=$3,
 lease_token=NULL,lease_expires_at=NULL,next_poll_at=now(),updated_at=now() WHERE id=$1 RETURNING *`, id, newest.ID, visits); err != nil {
		return nil, nil, err
	}
	detail := map[string]any{"from_version": pinned.Version, "to_version": newest.Version, "node": node.ID, "sequence": task.WorkflowSequence}
	if err = workflowAudit(ctx, tx, task.WorkspaceID, &actor, "task_workflow", task.ID, domain.WorkflowSwitchAction, detail); err != nil {
		return nil, nil, err
	}
	if err = workflowPauseChanged(ctx, tx, *task); err != nil {
		return nil, nil, err
	}
	return plan, task, tx.Commit()
}
