package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// SwitchWorkflow plans and optionally applies a creator's requested move.
//
// An explicit destination enters a fresh step. An upgrade in place keeps the
// current attempt and the counts of steps that still exist. Both drop the
// worker's lease so decisions planned on the old version are refused.
//
// The returned task is nil unless the task was moved.
func (r *AgentTaskRepository) SwitchWorkflow(ctx context.Context, ws, id uuid.UUID, target domain.WorkflowSwitchTarget, apply bool) (*domain.WorkflowSwitch, *domain.AgentTask, error) {
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
	if target.WorkflowID != "" {
		return continueWorkflow(ctx, tx, task, pinned, target, plan, actor, apply)
	}
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
	if domain.WorkflowKind(newest.Graph) != domain.WorkflowKind(pinned.Graph) {
		plan.Refused = domain.WorkflowSwitchKind
		return plan, nil, nil
	}
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
	// A waiting task is looked at again at once (workflow_wake_at), so the
	// new version's timeouts and budget apply now and not at the old wake-up.
	if err = tx.GetContext(ctx, task, `UPDATE agent_tasks SET workflow_version_id=$2,workflow_visits=$3,
 workflow_wake_at=CASE WHEN workflow_paused_at IS NULL THEN now() ELSE workflow_wake_at END,
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

func continueWorkflow(ctx context.Context, tx *sqlx.Tx, task *domain.AgentTask, pinned domain.WorkflowVersion, target domain.WorkflowSwitchTarget, plan *domain.WorkflowSwitch, actor uuid.UUID, apply bool) (*domain.WorkflowSwitch, *domain.AgentTask, error) {
	if plan.Refused = domain.WorkflowSwitchBlocked(*task); plan.Refused != "" {
		return plan, nil, nil
	}
	var destination domain.Workflow
	var err error
	if target.WorkflowID == domain.StandardWorkflowID {
		err = tx.GetContext(ctx, &destination, `SELECT * FROM workflows WHERE workspace_id=$1 AND builtin_key=$2 AND archived_at IS NULL FOR SHARE`, task.WorkspaceID, target.WorkflowID)
	} else {
		workflowID, parseErr := uuid.Parse(target.WorkflowID)
		if parseErr != nil {
			plan.Refused = domain.WorkflowSwitchUnavailable
			return plan, nil, nil
		}
		err = tx.GetContext(ctx, &destination, `SELECT * FROM workflows WHERE workspace_id=$1 AND id=$2 FOR SHARE`, task.WorkspaceID, workflowID)
	}
	if errors.Is(err, sql.ErrNoRows) {
		plan.Refused = domain.WorkflowSwitchUnavailable
		return plan, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if destination.ArchivedAt != nil {
		plan.Refused = domain.WorkflowSwitchArchived
		return plan, nil, nil
	}
	if destination.ID == pinned.WorkflowID {
		plan.Refused = domain.WorkflowSwitchUnavailable
		return plan, nil, nil
	}
	version, err := publishedWorkflowVersion(ctx, tx, task.WorkspaceID, destination.ID, nil)
	if err != nil {
		return nil, nil, err
	}
	if target.ExpectedVersionID != nil && *target.ExpectedVersionID != version.ID {
		plan.Refused = domain.WorkflowSwitchTargetStale
		return plan, nil, nil
	}
	if domain.WorkflowKind(version.Graph) != domain.WorkflowKind(pinned.Graph) {
		plan.Refused = domain.WorkflowSwitchKind
		return plan, nil, nil
	}
	graph, err := domain.ParseWorkflowGraph(version.Graph)
	if err != nil {
		return nil, nil, err
	}
	node, ok := graph.Node(target.Node)
	if !ok {
		plan.Refused = domain.WorkflowSwitchStepMissing
		return plan, nil, nil
	}
	if !node.CanContinueFrom() {
		plan.Refused = domain.WorkflowSwitchStepInvalid
		return plan, nil, nil
	}
	var sourceName string
	if err = tx.GetContext(ctx, &sourceName, `SELECT name FROM workflows WHERE id=$1 AND workspace_id=$2`, pinned.WorkflowID, task.WorkspaceID); err != nil {
		return nil, nil, err
	}
	now := time.Now()
	cursor := task.Cursor()
	plan.ToVersion, plan.Node, plan.NodeLabel = version.Version, node.ID, node.Label
	plan.Continuation = &domain.WorkflowContinuation{
		FromWorkflowID: pinned.WorkflowID, FromWorkflowName: sourceName, FromNode: cursor.Node,
		ToWorkflowID: destination.ID, ToWorkflowName: destination.Name, ToVersionID: version.ID, NodeType: node.Type,
		Carries: []string{"task", "machine", "worktree", "checkout", "branch", "pull_request"},
		Budget:  graph.Budget, MaxVisits: node.MaxVisits, TimeoutMinutes: node.TimeoutMinutes,
		Resets:           []string{"visits", "iterations", "budget_extensions", "time_budget"},
		TimeSpentSeconds: workflowSwitchTimeSpent(*task, now),
	}
	if !apply {
		return plan, nil, nil
	}
	if err = applyWorkflowSwitch(ctx, tx, task, *plan, actor, now); err != nil {
		return nil, nil, err
	}
	return plan, task, tx.Commit()
}

func workflowSwitchTimeSpent(task domain.AgentTask, now time.Time) int64 {
	cursor := task.Cursor()
	from := cursor.BudgetFrom
	if from.IsZero() {
		from = cursor.StartedAt
	}
	if from.IsZero() {
		return 0
	}
	if task.WorkflowPausedAt != nil && task.WorkflowPausedAt.Before(now) {
		now = *task.WorkflowPausedAt
	}
	if !cursor.HeldSince.IsZero() && cursor.HeldSince.Before(now) {
		now = cursor.HeldSince
	}
	return int64(max(now.Sub(from), 0) / time.Second)
}

func applyWorkflowSwitch(ctx context.Context, tx *sqlx.Tx, task *domain.AgentTask, plan domain.WorkflowSwitch, actor uuid.UUID, now time.Time) error {
	target := plan.Continuation
	if err := cancelWorkflowReviews(ctx, tx, task.ID, task.WorkflowSequence); err != nil {
		return err
	}
	var pausedSeconds int64
	if task.WorkflowPausedAt != nil {
		pausedSeconds = int64(max(now.Sub(*task.WorkflowPausedAt), 0) / time.Second)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE agent_task_workflow_steps
 SET outcome=COALESCE(outcome,'workflow_switched'),finished_at=now(),paused_seconds=paused_seconds+$3
 WHERE task_id=$1 AND sequence=$2 AND finished_at IS NULL`, task.ID, task.WorkflowSequence, pausedSeconds); err != nil {
		return err
	}
	sequence := task.WorkflowSequence + 1
	reason := domain.WorkflowReasonPrefix + "Continue on " + plan.NodeLabel
	if _, err := tx.ExecContext(ctx, `INSERT INTO agent_task_workflow_steps(task_id,sequence,node_id,node_type,visit,from_node,reason,decided_by)
 VALUES($1,$2,$3,$4,1,$5,$6,$7)`, task.ID, sequence, plan.Node, target.NodeType, target.FromNode, reason, actor); err != nil {
		return err
	}
	if err := tx.GetContext(ctx, task, `UPDATE agent_tasks SET workflow_version_id=$2,workflow_node=$3,workflow_sequence=$4,
 workflow_switch_sequence=$4,workflow_visits=jsonb_build_object($3::text,1),workflow_iterations=0,workflow_budget_extensions=0,
 workflow_status='entering',workflow_paused_at=NULL,workflow_pause_request=NULL,workflow_budget_from=now(),workflow_held_since=now(),
 workflow_wake_at=now(),resume_native_session=false,lease_token=NULL,lease_expires_at=NULL,next_poll_at=now(),updated_at=now()
 WHERE id=$1 RETURNING *`, task.ID, target.ToVersionID, plan.Node, sequence); err != nil {
		return err
	}
	if err := workflowAudit(ctx, tx, task.WorkspaceID, &actor, "task_workflow", task.ID, domain.WorkflowContinueAction, map[string]any{
		"from_workflow_id": target.FromWorkflowID, "from_version": plan.FromVersion, "from_node": target.FromNode,
		"to_workflow_id": target.ToWorkflowID, "to_workflow_name": target.ToWorkflowName, "to_version": plan.ToVersion,
		"to_node": plan.Node, "to_node_label": plan.NodeLabel, "sequence": sequence, "time_spent_seconds": target.TimeSpentSeconds,
	}); err != nil {
		return err
	}
	if err := transitionWorkflowTask(ctx, tx, task, "awaiting_review", reason, &actor); err != nil {
		return err
	}
	if err := markWorkflowReviewNotice(ctx, tx, *task, false, &actor); err != nil {
		return err
	}
	return workflowEvent(ctx, tx, *task)
}
