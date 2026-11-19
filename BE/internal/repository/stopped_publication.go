package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

const stoppedPublicationKey = "stopped-publication:"

var (
	ErrStoppedPublicationForbidden   = errors.New("Only the task creator or a workspace admin can publish these changes. Ask one of them to request and approve publication.")
	ErrStoppedPublicationState       = errors.New("Only failed, cancelled or blocked coding tasks can be published this way. For a running task, wait for it to finish.")
	ErrStoppedPublicationUnavailable = errors.New(domain.StoppedPublicationUnavailable)
	ErrStoppedPublicationBusy        = errors.New("The task's checkout or machine is busy. Wait for the other operation or cleanup to finish, then try again.")
)

func (r *ConversationRepository) StoppedPublicationTarget(ctx context.Context, ws, conversation, issue, taskID uuid.UUID) (*domain.AgentTask, error) {
	tx, chat, err := r.authorized(ctx, ws, conversation)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	task, _, err := lockStoppedPublicationTask(ctx, tx, chat, issue, taskID)
	return task, err
}

// An admin's approval belongs to their own conversation. Lock that chat and
// the task, leaving the task creator's private conversation outside this path.
func lockStoppedPublicationTask(ctx context.Context, tx *sqlx.Tx, chat *domain.Conversation, issue, taskID uuid.UUID) (*domain.AgentTask, *domain.DevMachineAgentRun, error) {
	var task domain.AgentTask
	err := tx.GetContext(ctx, &task, `SELECT t.* FROM agent_tasks t JOIN issues i ON i.id=t.issue_id AND i.workspace_id=t.workspace_id AND i.team_id=t.team_id
 WHERE t.workspace_id=$1 AND t.issue_id=$2 AND t.id=$3 AND kuayle_team_access(i.team_id,$4,$1) FOR SHARE OF i FOR UPDATE OF t`, chat.WorkspaceID, issue, taskID, chat.CreatorID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrStoppedPublicationForbidden
	}
	if err != nil {
		return nil, nil, err
	}
	var role string
	err = tx.GetContext(ctx, &role, `SELECT role FROM workspace_members WHERE workspace_id=$1 AND user_id=$2 FOR SHARE`, chat.WorkspaceID, chat.CreatorID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && (role == domain.RoleGuest || (chat.CreatorID != task.CreatorID && role != domain.RoleOwner && role != domain.RoleAdmin))) {
		return nil, nil, ErrStoppedPublicationForbidden
	}
	if err != nil {
		return nil, nil, err
	}
	if task.Kind != domain.AgentTaskKindImplement || !slices.Contains([]string{"failed", "cancelled", "blocked"}, task.State) {
		return nil, nil, ErrStoppedPublicationState
	}
	if task.MachineID == nil {
		return nil, nil, ErrStoppedPublicationUnavailable
	}
	var run domain.DevMachineAgentRun
	// A retry can stop before it starts a native run. Its existing checkout
	// still belongs to the task, so use the last run attached to that checkout.
	err = tx.GetContext(ctx, &run, `SELECT r.* FROM dev_machine_agent_runs r
 JOIN dev_machine_checkouts c ON c.id=r.checkout_id AND c.machine_id=r.machine_id AND c.workspace_id=r.workspace_id
 JOIN dev_machines m ON m.id=c.machine_id AND m.workspace_id=c.workspace_id
 WHERE r.task_id=$2 AND r.machine_id=$3 AND r.workspace_id=$4 AND c.id=$5
 AND c.scope_key=$6 AND c.status='ready' AND m.status='running' AND m.desired_status='running'
 ORDER BY (r.id=$1) DESC,r.created_at DESC,r.id LIMIT 1`, task.RunID, task.ID, task.MachineID, task.WorkspaceID, task.CheckoutID, task.ID.String())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrStoppedPublicationUnavailable
	}
	return &task, &run, err
}

func (r *ConversationRepository) QueueStoppedPublication(ctx context.Context, ws, conversation, id uuid.UUID) (*domain.ConversationApproval, error) {
	tx, chat, err := r.authorized(ctx, ws, conversation)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	approval, err := lockApproval(ctx, tx, conversation, id, chat.CreatorID)
	if err != nil {
		return nil, err
	}
	var payload domain.PublishTaskChangesPayload
	if approval.Action != domain.PublishTaskChangesAction || json.Unmarshal(approval.Payload, &payload) != nil {
		return nil, ErrConversationInvalid
	}
	if approval.TaskID != nil || approval.Status == "approved" {
		return approval, nil
	}
	if approval.Status != "approving" {
		return nil, ErrConversationConflict
	}
	task, run, err := lockStoppedPublicationTask(ctx, tx, chat, approval.IssueID, payload.TaskID)
	if err != nil {
		return nil, err
	}
	if task.Generation != payload.Generation {
		return nil, ErrAgentTaskConflict
	}
	generation, err := lockTaskMachine(ctx, tx, *task)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrStoppedPublicationUnavailable
	}
	if errors.Is(err, ErrAgentTaskConflict) {
		return nil, ErrStoppedPublicationBusy
	}
	if err != nil {
		return nil, err
	}
	var busy bool
	err = tx.GetContext(ctx, &busy, `SELECT EXISTS(SELECT 1 FROM dev_machine_operations WHERE (checkout_id=$1 OR agent_run_id IN (SELECT id FROM dev_machine_agent_runs WHERE task_id=$2)) AND status IN ('pending','leased') AND action<>'scan_worktree')
 OR ($4 <> 'succeeded' AND NOT EXISTS(SELECT 1 FROM dev_machine_operations WHERE agent_run_id=$3 AND idempotency_key=$5 AND status='completed'))`, task.CheckoutID, task.ID, run.ID, run.Status, "task-cancel:"+task.ID.String()+":"+run.ID.String())
	if err != nil {
		return nil, err
	}
	if busy {
		return nil, ErrStoppedPublicationBusy
	}
	// The approval request ID keys the step and operation, so replays cannot
	// queue another publication, including under concurrent approvals.
	if _, err = tx.ExecContext(ctx, `INSERT INTO dev_machine_agent_run_steps(id,agent_run_id,sequence,step_type,name,status,command_argv,summary)
 SELECT $1,$2,GREATEST(1,COALESCE(MAX(sequence),0))+1,'publication','Publish stopped task changes','queued','[]','Waiting to publish stopped task changes'
 FROM dev_machine_agent_run_steps WHERE agent_run_id=$2`, approval.RequestID, run.ID); err != nil {
		return nil, err
	}
	op := &domain.DevMachineOperation{ID: approval.RequestID, WorkspaceID: ws, MachineID: run.MachineID, AgentRunID: &run.ID, CheckoutID: run.CheckoutID, Action: domain.DevMachineOpPublishAgent,
		Status: domain.DevMachineOpStatusPending, Generation: generation, IdempotencyKey: stoppedPublicationKey + approval.RequestID.String(), RequestedByUserID: &chat.CreatorID, MaxAttempts: 3}
	if err = enqueueOperation(ctx, tx, op); err != nil {
		return nil, err
	}
	if err = tx.GetContext(ctx, approval, `UPDATE conversation_approvals SET task_id=$2, payload=payload||'{"queued":true}'::jsonb,updated_at=now() WHERE id=$1 RETURNING *`, id, task.ID); err != nil {
		return nil, err
	}
	if _, err = conversationEvent(ctx, tx, conversation, "approval.updated", approval); err != nil {
		return nil, err
	}
	return approval, tx.Commit()
}

func isStoppedPublication(op domain.DevMachineOperation) bool {
	return op.Action == domain.DevMachineOpPublishAgent && strings.HasPrefix(op.IdempotencyKey, stoppedPublicationKey)
}

func (r *DevMachineRepository) stoppedPublicationTransaction(ctx context.Context, op domain.DevMachineOperation) (*sqlx.Tx, *domain.AgentTask, *domain.DevMachineAgentRunStep, error) {
	if op.AgentRunID == nil || op.CheckoutID == nil || op.LeaseOwner == nil || op.RequestedByUserID == nil || op.IdempotencyKey != stoppedPublicationKey+op.ID.String() {
		return nil, nil, nil, ErrAgentTaskUnauthorized
	}
	var conversation uuid.UUID
	if err := r.db.GetContext(ctx, &conversation, `SELECT conversation_id FROM conversation_approvals WHERE request_id=$1 AND action=$2 AND actor_id=$3`, op.ID, domain.PublishTaskChangesAction, op.RequestedByUserID); err != nil {
		return nil, nil, nil, err
	}
	ctx = domain.WithAccess(ctx, domain.Access{WorkspaceID: op.WorkspaceID, UserID: *op.RequestedByUserID})
	tx, chat, err := NewConversationRepository(r.db).authorized(ctx, op.WorkspaceID, conversation)
	if err != nil {
		return nil, nil, nil, err
	}
	fail := func(err error) (*sqlx.Tx, *domain.AgentTask, *domain.DevMachineAgentRunStep, error) {
		tx.Rollback()
		return nil, nil, nil, err
	}
	var approval domain.ConversationApproval
	if err = tx.GetContext(ctx, &approval, `SELECT * FROM conversation_approvals WHERE conversation_id=$1 AND request_id=$2 AND status='approving' FOR UPDATE`, conversation, op.ID); err != nil {
		return fail(err)
	}
	var payload domain.PublishTaskChangesPayload
	if json.Unmarshal(approval.Payload, &payload) != nil || approval.TaskID == nil || *approval.TaskID != payload.TaskID {
		return fail(ErrAgentTaskUnauthorized)
	}
	task, run, err := lockStoppedPublicationTask(ctx, tx, chat, approval.IssueID, payload.TaskID)
	if err != nil {
		return fail(err)
	}
	if task.Generation != payload.Generation || run.ID != *op.AgentRunID || run.MachineID != op.MachineID || task.CheckoutID != *op.CheckoutID {
		return fail(ErrAgentTaskConflict)
	}
	if err = fenceTaskStepOperation(ctx, tx, op, *run); err != nil {
		return fail(err)
	}
	step := &domain.DevMachineAgentRunStep{}
	if err = tx.GetContext(ctx, step, `SELECT * FROM dev_machine_agent_run_steps WHERE id=$1 AND agent_run_id=$2 AND step_type='publication' FOR UPDATE`, op.ID, run.ID); err != nil {
		return fail(err)
	}
	return tx, task, step, nil
}

type StoppedPublicationDecision struct {
	ID             uuid.UUID `db:"id"`
	WorkspaceID    uuid.UUID `db:"workspace_id"`
	ConversationID uuid.UUID `db:"conversation_id"`
	ActorID        uuid.UUID `db:"actor_id"`
}

// StoppedPublicationDecisions finds completed operations, including operations
// removed by machine teardown. Their outcome survives API process restarts.
func (r *ConversationRepository) StoppedPublicationDecisions(ctx context.Context) ([]StoppedPublicationDecision, error) {
	var decisions []StoppedPublicationDecision
	err := r.db.SelectContext(ctx, &decisions, `SELECT a.id,c.workspace_id,a.conversation_id,a.actor_id FROM conversation_approvals a
 JOIN conversations c ON c.id=a.conversation_id
 JOIN workspace_members m ON m.workspace_id=c.workspace_id AND m.user_id=a.actor_id AND m.role<>'guest'
 LEFT JOIN dev_machine_operations o ON o.id=a.request_id
 WHERE a.action=$1 AND a.status='approving' AND a.payload @> '{"queued":true}'
 AND (o.id IS NULL OR o.status IN ('completed','failed','cancelled')) AND `+conversationScopePredicate("c")+`
 ORDER BY a.updated_at,a.id LIMIT 50`, domain.PublishTaskChangesAction)
	return decisions, err
}

func (r *ConversationRepository) FinishStoppedPublication(ctx context.Context, decision StoppedPublicationDecision) (*domain.ConversationApproval, error) {
	ctx = domain.WithAccess(ctx, domain.Access{WorkspaceID: decision.WorkspaceID, UserID: decision.ActorID})
	tx, chat, err := r.authorized(ctx, decision.WorkspaceID, decision.ConversationID)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	approval, err := lockApproval(ctx, tx, chat.ID, decision.ID, chat.CreatorID)
	if err != nil {
		return nil, err
	}
	if approval.Action != domain.PublishTaskChangesAction || approval.Status != "approving" {
		return nil, nil
	}
	var result struct {
		Status  sql.NullString `db:"status"`
		Code    sql.NullString `db:"error_code"`
		Message *string        `db:"error_message"`
		Step    sql.NullString `db:"step_status"`
		Link    sql.NullString `db:"summary"`
		Visible bool           `db:"visible"`
	}
	err = tx.GetContext(ctx, &result, `SELECT o.status,o.error_code,o.error_message,s.status AS step_status,s.summary,
 EXISTS (SELECT 1 FROM issues i WHERE i.id=a.issue_id AND i.workspace_id=$2 AND kuayle_team_access(i.team_id,a.actor_id,$2)) AS visible
 FROM conversation_approvals a LEFT JOIN dev_machine_operations o ON o.id=a.request_id AND o.workspace_id=$2
 LEFT JOIN dev_machine_agent_run_steps s ON s.id=a.request_id WHERE a.id=$1`, approval.ID, decision.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if result.Status.String == "pending" || result.Status.String == "leased" {
		return nil, nil
	}
	status, failure := "failed", ErrStoppedPublicationUnavailable.Error()
	var outcome string
	var pullRequestURL *string
	checkoutReason := domain.PublicationCheckoutBlockReason(result.Message)
	switch {
	case !result.Visible || result.Code.String == "publication_forbidden":
		failure = ErrStoppedPublicationForbidden.Error()
	case result.Status.String == "completed" && result.Step.String == "succeeded":
		outcome, pullRequestURL = "published", &result.Link.String
	case result.Status.String == "completed" && result.Step.String == "skipped":
		outcome = "empty"
	case result.Status.String == "failed" && result.Code.String == "publication_github_unavailable":
		failure = "The repository's GitHub App credentials are unavailable. Ask a workspace admin to check the GitHub integration, then try again."
	case result.Status.String == "failed" && result.Code.String == domain.PublicationCheckoutBlockedCode && checkoutReason != "":
		failure = checkoutReason
	case result.Status.String == "failed" && result.Code.String != "publication_unavailable" && result.Code.String != "publication_stale":
		failure = "Kuayle could not publish the draft. Ask the task creator to check its checkout and GitHub branch."
	}
	var errorText *string
	if outcome != "" {
		status = "approved"
		if _, err = tx.ExecContext(ctx, `UPDATE conversation_approvals
 SET payload=payload||jsonb_strip_nulls(jsonb_build_object('outcome',$2::text,'pull_request_url',$3::text))
 WHERE id=$1`, approval.ID, outcome, pullRequestURL); err != nil {
			return nil, err
		}
	} else {
		errorText = &failure
		if _, err = tx.ExecContext(ctx, `UPDATE dev_machine_agent_run_steps SET status='failed',completed_at=now(),summary=$2
 WHERE id=$1 AND status IN ('queued','running')`, approval.RequestID, failure); err != nil {
			return nil, err
		}
	}
	if err = resolveApproval(ctx, tx, approval, status, nil, errorText); err != nil {
		return nil, err
	}
	return approval, tx.Commit()
}
