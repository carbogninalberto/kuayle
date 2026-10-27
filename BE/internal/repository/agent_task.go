package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

var ErrAgentTaskUnauthorized = errors.New("agent task execution is no longer authorized")

var ErrAgentTaskConflict = errors.New("agent task changed or execution lease expired")

type AgentTaskRepository struct{ db *sqlx.DB }

func NewAgentTaskRepository(db *sqlx.DB) *AgentTaskRepository { return &AgentTaskRepository{db} }

func (r *AgentTaskRepository) Create(ctx context.Context, ws, conversation uuid.UUID, input domain.AgentTaskRequest) (*domain.AgentTask, error) {
	input.Prompt = strings.TrimSpace(input.Prompt)
	if input.RequestID == uuid.Nil || len(input.Prompt) == 0 || len(input.Prompt) > 32768 || !utf8.ValidString(input.Prompt) || len(input.Provider) > 64 || input.Provider == "" || len(input.Model) > 128 || len(input.Thinking) > 32 {
		return nil, ErrConversationInvalid
	}
	tx, c, err := NewConversationRepository(r.db).authorized(ctx, ws, conversation)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if c.IssueID == nil || c.TeamID == nil {
		return nil, ErrConversationInvalid
	}
	// Serialize a caller's request ID across distinct conversations too.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "agent-task-request:"+ws.String()+":"+c.CreatorID.String()+":"+input.RequestID.String()); err != nil {
		return nil, err
	}
	if existing, err := replayAgentTaskCreate(ctx, tx, c, input); err != nil || existing != nil {
		return existing, err
	}
	requested, command, err := resolveAgentTaskCommand(ctx, tx, ws, *c.IssueID, input.TestCommand)
	if err != nil {
		return nil, err
	}
	var task domain.AgentTask
	instructions, err := resolveScopeDefault(ctx, tx, ws, *c.IssueID, "agent_instructions")
	if err != nil {
		return nil, err
	}
	compose, err := resolveScopeDefault(ctx, tx, ws, *c.IssueID, "compose_file")
	if err != nil {
		return nil, err
	}
	preview, previewPort, err := resolvePreview(ctx, tx, ws, *c.IssueID)
	if err != nil {
		return nil, err
	}
	err = tx.GetContext(ctx, &task, `INSERT INTO agent_tasks(workspace_id,conversation_id,creator_id,issue_id,team_id,request_id,prompt,provider,model,thinking,reason,issue_status_id,issue_updated_at,test_command,validation_command,agent_instructions,compose_file,preview_command,preview_port,model_server_id) SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'Waiting for execution',i.status_id,i.updated_at,$11,$12,$13,$14,$15,$16,$17 FROM issues i WHERE i.id=$4 AND i.workspace_id=$1 AND i.team_id=$5 RETURNING *`, ws, conversation, c.CreatorID, c.IssueID, c.TeamID, input.RequestID, input.Prompt, input.Provider, input.Model, input.Thinking, requested, command, instructions, compose, preview, previewPort, input.ModelServerID)
	if err != nil {
		return nil, err
	}
	// Name what the new task waits for; the worker starts it once unblocked.
	if reason, err := waitingReason(ctx, tx, task.IssueID); err != nil {
		return nil, err
	} else if reason != "" {
		if err = tx.GetContext(ctx, &task, `UPDATE agent_tasks SET reason=$2 WHERE id=$1 RETURNING *`, task.ID, reason); err != nil {
			return nil, err
		}
	}
	if err = recordAgentTaskTransition(ctx, tx, task, "", &c.CreatorID); err != nil {
		return nil, err
	}
	return &task, tx.Commit()
}

// Always acquire the privacy graph and conversation locks before the task row.
// Claim only locks a task row, and never waits for a conversation while holding it.
func lockAgentTask(ctx context.Context, tx *sqlx.Tx, id uuid.UUID, authorize bool) (*domain.AgentTask, error) {
	var task domain.AgentTask
	if err := tx.GetContext(ctx, &task, `SELECT * FROM agent_tasks WHERE id=$1`, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrConversationNotFound
		}
		return nil, err
	}
	if authorize {
		a, ok := domain.AccessFromContext(ctx)
		if !ok || a.UserID != task.CreatorID || a.WorkspaceID != task.WorkspaceID {
			return nil, ErrConversationNotFound
		}
	}
	if task.ConversationID != nil {
		var c *domain.Conversation
		var err error
		if authorize {
			c, err = lockConversation(ctx, tx, task.WorkspaceID, *task.ConversationID, task.CreatorID, false)
		} else {
			// Cleanup must remain possible after an issue moves to another team.
			// Pin the live issue without authorizing its content, then its chat,
			// preserving the same issue -> conversation -> task lock order.
			var pinned []uuid.UUID
			err = tx.SelectContext(ctx, &pinned, `SELECT id FROM issues WHERE id=$1 AND workspace_id=$2 FOR SHARE`, task.IssueID, task.WorkspaceID)
			if err == nil {
				c = &domain.Conversation{}
				err = tx.GetContext(ctx, c, `SELECT * FROM conversations WHERE id=$1 AND workspace_id=$2 FOR UPDATE`, task.ConversationID, task.WorkspaceID)
			}
		}
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrConversationNotFound
		}
		if err != nil {
			return nil, err
		}
		if authorize {
			if err = conversationMember(ctx, tx, task.WorkspaceID, task.CreatorID); err != nil {
				return nil, err
			}
			if err = conversationScope(ctx, tx, *c); err != nil {
				return nil, err
			}
			if c.IssueID == nil || *c.IssueID != task.IssueID || c.TeamID == nil || *c.TeamID != task.TeamID {
				return nil, ErrConversationNotFound
			}
		}
	} else if authorize {
		return nil, ErrConversationNotFound
	}
	if err := tx.GetContext(ctx, &task, `SELECT * FROM agent_tasks WHERE id=$1 FOR UPDATE`, id); err != nil {
		return nil, err
	}
	return &task, nil
}
func (r *AgentTaskRepository) transaction(ctx context.Context, id uuid.UUID, authorize bool) (*sqlx.Tx, *domain.AgentTask, error) {
	tx, err := beginConversationTransaction(ctx, r.db)
	if err != nil {
		return nil, nil, err
	}
	task, err := lockAgentTask(ctx, tx, id, authorize)
	if err != nil {
		tx.Rollback()
		return nil, nil, err
	}
	return tx, task, nil
}
func (r *AgentTaskRepository) Get(ctx context.Context, ws, id uuid.UUID) (*domain.AgentTask, error) {
	if _, err := conversationActor(ctx, ws); err != nil {
		return nil, err
	}
	tx, task, err := r.transaction(ctx, id, true)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	return task, nil
}
func (r *AgentTaskRepository) List(ctx context.Context, ws, conversation uuid.UUID) ([]domain.AgentTask, error) {
	tx, _, err := NewConversationRepository(r.db).authorized(ctx, ws, conversation)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	tasks := []domain.AgentTask{}
	err = tx.SelectContext(ctx, &tasks, `SELECT * FROM agent_tasks WHERE conversation_id=$1 ORDER BY created_at DESC,id LIMIT 100`, conversation)
	return tasks, err
}
func (r *AgentTaskRepository) Transitions(ctx context.Context, ws, id uuid.UUID) ([]domain.AgentTaskTransition, error) {
	if _, err := conversationActor(ctx, ws); err != nil {
		return nil, err
	}
	tx, _, err := r.transaction(ctx, id, true)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	entries := []domain.AgentTaskTransition{}
	err = tx.SelectContext(ctx, &entries, `SELECT * FROM agent_task_transitions WHERE task_id=$1 ORDER BY sequence`, id)
	return entries, err
}
func (r *AgentTaskRepository) Claim(ctx context.Context) (*domain.AgentTask, error) {
	var task domain.AgentTask
	// A queued task whose issue has unfinished blockers waits for them.
	err := r.db.GetContext(ctx, &task, `WITH candidate AS (
 SELECT id FROM agent_tasks WHERE state NOT IN ('completed','cancelled','failed','blocked','awaiting_review')
 AND next_poll_at<=now() AND (lease_expires_at IS NULL OR lease_expires_at<=now())
 AND NOT (state='queued' AND `+openBlockersExist("agent_tasks.issue_id")+`)
 ORDER BY next_poll_at,created_at,id FOR UPDATE SKIP LOCKED LIMIT 1)
 UPDATE agent_tasks t SET lease_token=gen_random_uuid(),lease_expires_at=now()+interval '30 seconds'
 FROM candidate c WHERE t.id=c.id RETURNING t.*`)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &task, err
}
func fenceAgentTask(ctx context.Context, tx *sqlx.Tx, current, claimed domain.AgentTask) error {
	if current.Generation != claimed.Generation || current.LeaseToken == nil || claimed.LeaseToken == nil || *current.LeaseToken != *claimed.LeaseToken {
		return ErrAgentTaskConflict
	}
	var valid bool
	if err := tx.GetContext(ctx, &valid, `SELECT lease_expires_at>clock_timestamp() FROM agent_tasks WHERE id=$1`, current.ID); err != nil {
		return err
	}
	if !valid {
		return ErrAgentTaskConflict
	}
	return nil
}
func recordAgentTaskTransition(ctx context.Context, tx *sqlx.Tx, task domain.AgentTask, from string, actor *uuid.UUID) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO agent_task_transitions(task_id,from_state,to_state,actor_id,reason) VALUES($1,$2,$3,$4,$5)`, task.ID, from, task.State, actor, task.Reason)
	if err != nil {
		return err
	}
	if task.ConversationID != nil {
		if _, err = conversationEvent(ctx, tx, *task.ConversationID, "task.state_changed", task); err != nil {
			return err
		}
		if err = notifyTaskTelegram(ctx, tx, task); err != nil {
			return err
		}
	}
	if err := startTaskPreview(ctx, tx, task); err != nil {
		return err
	}
	return purgeTaskSession(ctx, tx, task)
}

// startTaskPreview queues the project's QA preview for a task that reached
// review, once per attempt, so reviewers can open the running change.
func startTaskPreview(ctx context.Context, tx *sqlx.Tx, task domain.AgentTask) error {
	if task.State != "awaiting_review" || task.MachineID == nil || task.PreviewPort == nil || len(task.PreviewCommand) < 3 {
		return nil
	}
	var generation int64
	err := tx.GetContext(ctx, &generation, `SELECT m.generation FROM dev_machines m JOIN dev_machine_checkouts c ON c.machine_id=m.id AND c.id=$3
 WHERE m.id=$1 AND m.workspace_id=$2 AND m.status<>'destroyed'`, task.MachineID, task.WorkspaceID, task.CheckoutID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return enqueueOperation(ctx, tx, &domain.DevMachineOperation{
		WorkspaceID: task.WorkspaceID, MachineID: *task.MachineID, CheckoutID: &task.CheckoutID, AgentRunID: &task.RunID,
		Action: domain.DevMachineOpStartPreview, Status: domain.DevMachineOpStatusPending, Generation: generation,
		IdempotencyKey: "task-preview:" + task.RunID.String(), RequestedByUserID: &task.CreatorID, MaxAttempts: 3,
	})
}

// TaskPreview returns the preview to start for a task checkout still in review.
func (r *DevMachineRepository) TaskPreview(ctx context.Context, checkout uuid.UUID) (argv []string, port int, creator uuid.UUID, ok bool, err error) {
	var raw []byte
	var p sql.NullInt64
	err = r.db.QueryRowContext(ctx, `SELECT preview_command,preview_port,creator_id FROM agent_tasks WHERE checkout_id=$1 AND state='awaiting_review'`, checkout).Scan(&raw, &p, &creator)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, 0, uuid.Nil, false, nil
	}
	if err != nil || !p.Valid || json.Unmarshal(raw, &argv) != nil || len(argv) == 0 {
		return nil, 0, uuid.Nil, false, err
	}
	return argv, int(p.Int64), creator, true, nil
}

// purgeTaskSession queues removal of a finished task's native harness
// transcripts. Failed tasks keep them, since an explicit retry may resume.
func purgeTaskSession(ctx context.Context, tx *sqlx.Tx, task domain.AgentTask) error {
	if (task.State != "completed" && task.State != "cancelled") || task.MachineID == nil {
		return nil
	}
	// Sessions exist only after an attempt ran in the task's checkout; a task
	// cancelled before its checkout was created has nothing to purge.
	var generation int64
	err := tx.GetContext(ctx, &generation, `SELECT m.generation FROM dev_machines m JOIN dev_machine_checkouts c ON c.machine_id=m.id AND c.id=$3
 WHERE m.id=$1 AND m.workspace_id=$2 AND m.status<>'destroyed'`, task.MachineID, task.WorkspaceID, task.CheckoutID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return enqueueOperation(ctx, tx, &domain.DevMachineOperation{
		WorkspaceID: task.WorkspaceID, MachineID: *task.MachineID, CheckoutID: &task.CheckoutID,
		Action: domain.DevMachineOpPurgeTaskSession, Status: domain.DevMachineOpStatusPending, Generation: generation,
		IdempotencyKey: "task-session-purge:" + task.ID.String(), RequestedByUserID: &task.CreatorID, MaxAttempts: 5,
	})
}
func transitionAgentTask(ctx context.Context, tx *sqlx.Tx, task *domain.AgentTask, to, reason string, actor *uuid.UUID) error {
	if !domain.ValidAgentTaskTransition(task.State, to) || len(reason) > 2048 || strings.TrimSpace(reason) == "" {
		return ErrAgentTaskConflict
	}
	from := task.State
	if err := tx.GetContext(ctx, task, `UPDATE agent_tasks SET state=$2,reason=$3,updated_at=now() WHERE id=$1 RETURNING *`, task.ID, to, reason); err != nil {
		return err
	}
	return recordAgentTaskTransition(ctx, tx, *task, from, actor)
}
func (r *AgentTaskRepository) Transition(ctx context.Context, claimed domain.AgentTask, to, reason string) error {
	if to == "cancelled" {
		return r.FinishCancellation(ctx, claimed, reason)
	}
	tx, task, err := r.transaction(ctx, claimed.ID, true)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = fenceAgentTask(ctx, tx, *task, claimed); err != nil {
		return err
	}
	if to == "awaiting_review" {
		var published bool
		if err = tx.GetContext(ctx, &published, `SELECT EXISTS(SELECT 1 FROM dev_machine_agent_run_steps s JOIN dev_machine_operations o ON o.agent_run_id=s.agent_run_id AND o.action='publish_agent'
         WHERE s.agent_run_id=$1 AND s.sequence=1 AND s.step_type='publication' AND s.status='succeeded' AND s.completed_at IS NOT NULL AND o.status='completed')`, task.RunID); err != nil {
			return err
		}
		if !published {
			return ErrAgentTaskConflict
		}
	}
	if to == "publishing" {
		var passed bool
		if err = tx.GetContext(ctx, &passed, `SELECT EXISTS(SELECT 1 FROM dev_machine_agent_run_steps s JOIN dev_machine_operations o ON o.agent_run_id=s.agent_run_id AND o.action='validate_agent'
         WHERE s.agent_run_id=$1 AND s.sequence=0 AND s.step_type='validation' AND s.status='succeeded' AND s.exit_code=0 AND s.started_at IS NOT NULL AND s.completed_at IS NOT NULL AND o.status='completed')`, task.RunID); err != nil {
			return err
		}
		if !passed {
			return ErrAgentTaskConflict
		}
	}
	if err = transitionAgentTask(ctx, tx, task, to, reason, nil); err != nil {
		return err
	}
	return tx.Commit()
}
func (r *AgentTaskRepository) RequestCancel(ctx context.Context, ws, id uuid.UUID) error {
	if _, err := conversationActor(ctx, ws); err != nil {
		return err
	}
	tx, task, err := r.transaction(ctx, id, true)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if task.State == "cancelled" {
		return nil
	}
	if task.State == "cancelling" {
		result, err := tx.ExecContext(ctx, `UPDATE dev_machine_operations SET status='pending',attempts=0,available_at=now(),error_code=NULL,error_message=NULL WHERE workspace_id=$1 AND status='failed' AND ((agent_run_id=$2 AND idempotency_key=$3) OR (machine_id=$4 AND idempotency_key=$5))`, task.WorkspaceID, task.RunID, "task-cancel:"+task.ID.String()+":"+task.RunID.String(), task.ReservedMachineID, fmt.Sprintf("task-idle:%s:%d", task.ID, task.Generation))
		if err != nil {
			return err
		}
		if count, _ := result.RowsAffected(); count > 0 {
			if err = tx.GetContext(ctx, task, `UPDATE agent_tasks SET failure_count=0,reason='Runtime cleanup retry requested',next_poll_at=now() WHERE id=$1 RETURNING *`, id); err != nil {
				return err
			}
			if task.ConversationID != nil {
				if _, err = conversationEvent(ctx, tx, *task.ConversationID, "task.updated", task); err != nil {
					return err
				}
			}
		}
		return tx.Commit()
	}
	if err = transitionAgentTask(ctx, tx, task, "cancelling", "Cancellation requested", &task.CreatorID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE agent_tasks SET lease_token=NULL,lease_expires_at=NULL,next_poll_at=now() WHERE id=$1`, id)
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (r *AgentTaskRepository) Revoke(ctx context.Context, claimed domain.AgentTask) error {
	tx, task, err := r.transaction(ctx, claimed.ID, false)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = fenceAgentTask(ctx, tx, *task, claimed); err != nil {
		return err
	}
	// Cleanup authority is not execution authority. Refuse to revoke a live grant.
	var authorized bool
	err = tx.GetContext(ctx, &authorized, `SELECT EXISTS(SELECT 1 FROM conversations c JOIN workspace_members wm ON wm.workspace_id=c.workspace_id AND wm.user_id=c.creator_id AND wm.role<>'guest' WHERE c.id=$1 AND `+conversationScopePredicate("c")+`)`, task.ConversationID)
	if err != nil {
		return err
	}
	if authorized {
		return ErrAgentTaskConflict
	}
	if task.State != "cancelling" {
		if err = transitionAgentTask(ctx, tx, task, "cancelling", "Issue access was revoked", nil); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE agent_tasks SET lease_token=NULL,lease_expires_at=NULL,next_poll_at=now() WHERE id=$1`, task.ID)
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (r *AgentTaskRepository) FinishCancellation(ctx context.Context, claimed domain.AgentTask, reason string) error {
	tx, task, err := r.transaction(ctx, claimed.ID, false)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = fenceAgentTask(ctx, tx, *task, claimed); err != nil {
		return err
	}
	var active bool
	err = tx.GetContext(ctx, &active, `SELECT EXISTS(SELECT 1 FROM dev_machine_agent_runs r WHERE r.id=$1 AND
 (r.status IN ('queued','starting','running','waiting_input') OR NOT EXISTS(
  SELECT 1 FROM dev_machine_operations o WHERE o.agent_run_id=r.id AND o.idempotency_key=$2 AND o.status='completed')))
 OR EXISTS(SELECT 1 FROM dev_machine_operations WHERE (agent_run_id=$1 OR checkout_id=$3 OR (machine_id=$4 AND $4=$5)) AND status IN ('pending','leased'))`, task.RunID, "task-cancel:"+task.ID.String()+":"+task.RunID.String(), task.CheckoutID, task.MachineID, task.ReservedMachineID)
	if err != nil {
		return err
	}
	if active {
		return ErrAgentTaskConflict
	}
	settled, err := settleTaskMachine(ctx, tx, *task)
	if err != nil {
		return err
	}
	if !settled {
		if err = tx.Commit(); err != nil {
			return err
		}
		return ErrAgentTaskConflict
	}
	if _, err = tx.ExecContext(ctx, `UPDATE dev_machine_agent_run_steps SET status='cancelled',completed_at=now(),summary='Runtime cancellation confirmed' WHERE agent_run_id=$1 AND step_type IN ('validation','publication') AND status IN ('queued','running')`, task.RunID); err != nil {
		return err
	}
	target := "cancelled"
	if task.FailurePending {
		target = "failed"
		reason = "Execution failed; runtime cleanup confirmed"
	}
	if err = transitionAgentTask(ctx, tx, task, target, reason, nil); err != nil {
		return err
	}
	return tx.Commit()
}
func (r *AgentTaskRepository) Release(ctx context.Context, claimed domain.AgentTask) error {
	result, err := r.db.ExecContext(ctx, `UPDATE agent_tasks SET lease_token=NULL,lease_expires_at=NULL,next_poll_at=now()+make_interval(secs=>power(2,failure_count)::int) WHERE id=$1 AND lease_token=$2 AND generation=$3`, claimed.ID, claimed.LeaseToken, claimed.Generation)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return ErrAgentTaskConflict
	}
	return nil
}

// Runtime writes carry task authority through existing services. Fencing occurs
// inside their transaction, so cancellation/revocation cannot race a queued effect.
func beginTaskRuntimeTransaction(ctx context.Context, db *sqlx.DB, kind string, ws, machine, object uuid.UUID) (*sqlx.Tx, error) {
	authority, ok := domain.TaskExecutionFromContext(ctx)
	if !ok {
		return db.BeginTxx(ctx, nil)
	}
	tx, err := beginConversationTransaction(ctx, db)
	if err != nil {
		return nil, err
	}
	task, err := lockAgentTask(ctx, tx, authority.TaskID, true)
	if err == nil {
		claimed := *task
		claimed.LeaseToken = &authority.LeaseToken
		claimed.Generation = authority.Generation
		err = fenceAgentTask(ctx, tx, *task, claimed)
	}
	if err == nil {
		valid := task.WorkspaceID == ws && task.MachineID != nil && *task.MachineID == machine
		switch kind {
		case "machine":
			valid = valid && task.State == "preparing" && task.ReservedMachineID == machine
		case "start":
			valid = valid && task.State == "preparing" && task.ReservedMachineID == machine && object == machine
		case "checkout":
			valid = valid && task.State == "preparing" && task.CheckoutID == object
		case "run":
			valid = valid && (task.State == "preparing" || task.State == "running") && task.RunID == object
		default:
			valid = false
		}
		if !valid {
			err = ErrAgentTaskConflict
		}
	}
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	return tx, nil
}

// ReserveMachine serializes compatible-machine selection with other task
// reservations. Running attempts still use the runtime's own machine-wide guard.
func (r *AgentTaskRepository) ReserveMachine(ctx context.Context, claimed domain.AgentTask, repositoryID uuid.UUID, environmentID *uuid.UUID) (uuid.UUID, error) {
	tx, task, err := r.transaction(ctx, claimed.ID, true)
	if err != nil {
		return uuid.Nil, err
	}
	defer tx.Rollback()
	if err = fenceAgentTask(ctx, tx, *task, claimed); err != nil {
		return uuid.Nil, err
	}
	if task.State != "preparing" {
		return uuid.Nil, ErrAgentTaskConflict
	}
	if task.MachineID != nil {
		// A reservation for a machine that was never created (e.g. blocked by
		// the machine limit) is revisited, so an idle machine can be reused.
		var pending bool
		if err = tx.GetContext(ctx, &pending, `SELECT $1::uuid=$2::uuid AND NOT EXISTS(SELECT 1 FROM dev_machines WHERE id=$1)`, *task.MachineID, task.ReservedMachineID); err != nil {
			return uuid.Nil, err
		}
		if !pending {
			return *task.MachineID, nil
		}
	}
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text,0))`, task.WorkspaceID); err != nil {
		return uuid.Nil, err
	}
	var machineID uuid.UUID
	err = tx.GetContext(ctx, &machineID, `SELECT m.id FROM dev_machines m
 WHERE m.workspace_id=$1 AND m.created_by_user_id=$2 AND m.status='running' AND m.desired_status='running'
 AND m.expires_at>now()+interval '5 minutes' AND NOT m.environment_builder
 AND (m.repository_affinity_id IS NULL OR m.repository_affinity_id=$3)
 AND m.environment_id IS NOT DISTINCT FROM $4::uuid
 AND NOT EXISTS(SELECT 1 FROM dev_machine_agent_runs r WHERE r.machine_id=m.id AND r.status IN ('queued','starting','running','waiting_input'))
 AND NOT EXISTS(SELECT 1 FROM agent_tasks t WHERE t.machine_id=m.id AND t.id<>$6 AND t.state NOT IN ('completed','cancelled','failed','blocked','awaiting_review'))
 ORDER BY EXISTS(SELECT 1 FROM dev_machine_agent_providers p WHERE p.machine_id=m.id AND p.provider_id=$5 AND p.enabled) DESC,
  m.last_activity_at DESC NULLS LAST,m.id FOR UPDATE OF m SKIP LOCKED LIMIT 1`, task.WorkspaceID, task.CreatorID, repositoryID, environmentID, task.Provider, task.ID)
	if errors.Is(err, sql.ErrNoRows) {
		machineID = task.ReservedMachineID
	} else if err != nil {
		return uuid.Nil, err
	}
	if err = tx.GetContext(ctx, task, `UPDATE agent_tasks SET machine_id=$2,updated_at=now() WHERE id=$1 RETURNING *`, task.ID, machineID); err != nil {
		return uuid.Nil, err
	}
	if _, err = conversationEvent(ctx, tx, *task.ConversationID, "task.updated", task); err != nil {
		return uuid.Nil, err
	}
	return machineID, tx.Commit()
}

// CancelAttempt always queues physical cleanup, including for native attempts
// already marked failed/cancelled. Their status alone says nothing about a live
// container. The operation key makes crash recovery and repeated calls harmless.
func (r *AgentTaskRepository) CancelAttempt(ctx context.Context, claimed domain.AgentTask) error {
	tx, task, err := r.transaction(ctx, claimed.ID, false)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = fenceAgentTask(ctx, tx, *task, claimed); err != nil {
		return err
	}
	if task.State != "cancelling" {
		return ErrAgentTaskConflict
	}
	var run domain.DevMachineAgentRun
	err = tx.GetContext(ctx, &run, `SELECT * FROM dev_machine_agent_runs WHERE id=$1 AND workspace_id=$2`, task.RunID, task.WorkspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if task.MachineID == nil || run.MachineID != *task.MachineID {
		return ErrAgentTaskConflict
	}
	var generation int64
	if err = tx.GetContext(ctx, &generation, `SELECT generation FROM dev_machines WHERE id=$1 AND workspace_id=$2 FOR SHARE`, run.MachineID, task.WorkspaceID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE dev_machine_agent_runs SET status='cancelled',cancelled_at=now(),completed_at=now() WHERE id=$1 AND status IN ('queued','starting','running','waiting_input')`, run.ID)
	if err != nil {
		return err
	}
	operation := &domain.DevMachineOperation{ID: uuid.New(), WorkspaceID: task.WorkspaceID, MachineID: run.MachineID, AgentRunID: &run.ID, Action: domain.DevMachineOpCancelAgent, Status: domain.DevMachineOpStatusPending, Generation: generation, IdempotencyKey: "task-cancel:" + task.ID.String() + ":" + run.ID.String(), MaxAttempts: 5}
	if err = enqueueOperation(ctx, tx, operation); err != nil {
		return err
	}
	if operation.Status == domain.DevMachineOpStatusFailed {
		return ErrAgentTaskCleanupFailed
	}
	return tx.Commit()
}

// Native attempts keep their task identity even after a retry changes run_id.
// Missing, moved, cancelled and superseded tasks must never fall back to manual
// run authorization when a queued infrastructure operation is recovered.
func beginAuthorizedAgentRun(ctx context.Context, db *sqlx.DB, runID uuid.UUID) (*sqlx.Tx, error) {
	var run domain.DevMachineAgentRun
	if err := db.GetContext(ctx, &run, `SELECT * FROM dev_machine_agent_runs WHERE id=$1`, runID); err != nil {
		return nil, err
	}
	if run.TaskID == nil {
		return db.BeginTxx(ctx, nil)
	}
	var creator uuid.UUID
	if err := db.GetContext(ctx, &creator, `SELECT creator_id FROM agent_tasks WHERE id=$1 AND workspace_id=$2`, run.TaskID, run.WorkspaceID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrAgentTaskUnauthorized
		}
		return nil, err
	}
	ctx = domain.WithAccess(ctx, domain.Access{WorkspaceID: run.WorkspaceID, UserID: creator})
	tx, err := beginConversationTransaction(ctx, db)
	if err != nil {
		return nil, err
	}
	task, err := lockAgentTask(ctx, tx, *run.TaskID, true)
	if errors.Is(err, ErrConversationNotFound) {
		err = ErrAgentTaskUnauthorized
	}
	if err == nil {
		allowed := task.RunID == run.ID && task.MachineID != nil && *task.MachineID == run.MachineID && run.RequestedByUserID != nil && *run.RequestedByUserID == task.CreatorID
		allowed = allowed && (task.State == "preparing" || task.State == "running" || task.State == "waiting_input")
		if !allowed {
			err = ErrAgentTaskUnauthorized
		}
	}
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	return tx, nil
}
func (r *DevMachineRepository) AuthorizeAgentRun(ctx context.Context, runID uuid.UUID) error {
	tx, err := beginAuthorizedAgentRun(ctx, r.db, runID)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var active bool
	if err = tx.GetContext(ctx, &active, `SELECT status IN ('queued','starting','running','waiting_input') FROM dev_machine_agent_runs WHERE id=$1 FOR SHARE`, runID); err != nil {
		return err
	}
	if !active {
		return sql.ErrNoRows
	}
	return nil
}

// RecordStepResult counts unexpected worker failures, not ordinary capacity or
// cleanup waits. No provider/SQL error text is persisted into the conversation.
func (r *AgentTaskRepository) RecordStepResult(ctx context.Context, claimed domain.AgentTask, failed bool) error {
	tx, task, err := r.transaction(ctx, claimed.ID, false)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = fenceAgentTask(ctx, tx, *task, claimed); err != nil {
		return err
	}
	count := 0
	if failed {
		count = min(task.FailureCount+1, 5)
	}
	if err = tx.GetContext(ctx, task, `UPDATE agent_tasks SET failure_count=$2 WHERE id=$1 RETURNING *`, task.ID, count); err != nil {
		return err
	}
	if count == 5 && task.State != "cancelling" {
		if err = beginAgentTaskFailure(ctx, tx, task, "Worker recovery exhausted; stopping execution"); err != nil {
			return err
		}
	}
	if count == 5 && task.State == "cancelling" && task.Reason != "Cleanup has not completed; retry cancellation after restoring the machine manager" {
		if err = tx.GetContext(ctx, task, `UPDATE agent_tasks SET reason='Cleanup has not completed; retry cancellation after restoring the machine manager' WHERE id=$1 RETURNING *`, task.ID); err != nil {
			return err
		}
		if task.ConversationID != nil {
			if _, err = conversationEvent(ctx, tx, *task.ConversationID, "task.updated", task); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
func beginAgentTaskFailure(ctx context.Context, tx *sqlx.Tx, task *domain.AgentTask, reason string) error {
	if _, err := tx.ExecContext(ctx, `UPDATE agent_tasks SET failure_pending=true WHERE id=$1`, task.ID); err != nil {
		return err
	}
	task.FailurePending = true
	return transitionAgentTask(ctx, tx, task, "cancelling", reason, nil)
}
func (r *AgentTaskRepository) Fail(ctx context.Context, claimed domain.AgentTask, reason string) error {
	tx, task, err := r.transaction(ctx, claimed.ID, false)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = fenceAgentTask(ctx, tx, *task, claimed); err != nil {
		return err
	}
	if err = beginAgentTaskFailure(ctx, tx, task, reason); err != nil {
		return err
	}
	return tx.Commit()
}

var ErrAgentTaskCleanupFailed = errors.New("task runtime cleanup needs an explicit retry")

// Pause an automatically allocated environment only after execution/preparation
// settles. Reused manual machines and another task's active environment stay up.
func settleTaskMachine(ctx context.Context, tx *sqlx.Tx, task domain.AgentTask) (bool, error) {
	if task.MachineID == nil || *task.MachineID != task.ReservedMachineID {
		return true, nil
	}
	var machine domain.DevMachine
	err := tx.GetContext(ctx, &machine, `SELECT * FROM dev_machines WHERE id=$1 AND workspace_id=$2 FOR UPDATE`, *task.MachineID, task.WorkspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	var shared bool
	err = tx.GetContext(ctx, &shared, `SELECT EXISTS(SELECT 1 FROM agent_tasks WHERE machine_id=$1 AND id<>$2 AND state NOT IN ('completed','cancelled','failed','blocked')) OR EXISTS(SELECT 1 FROM dev_machine_agent_runs WHERE machine_id=$1 AND id<>$3 AND status IN ('queued','starting','running','waiting_input'))`, machine.ID, task.ID, task.RunID)
	if err != nil {
		return false, err
	}
	if shared {
		return true, nil
	}
	if machine.Status == domain.DevMachineStatusPaused || machine.Status == domain.DevMachineStatusStopped || machine.Status == domain.DevMachineStatusDestroyed {
		return true, nil
	}
	key := fmt.Sprintf("task-idle:%s:%d", task.ID, task.Generation)
	var previous domain.DevMachineOperation
	err = tx.GetContext(ctx, &previous, `SELECT * FROM dev_machine_operations WHERE machine_id=$1 AND idempotency_key=$2`, machine.ID, key)
	if err == nil {
		if previous.Status == domain.DevMachineOpStatusFailed {
			return false, ErrAgentTaskCleanupFailed
		}
		// A later manual start may supersede a completed pause. Respect that action.
		return previous.Status == domain.DevMachineOpStatusCompleted, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	desired, action := domain.DevMachineStatusPaused, domain.DevMachineOpPause
	if machine.Status != domain.DevMachineStatusRunning {
		desired, action = domain.DevMachineStatusStopped, domain.DevMachineOpStop
	}
	generation := machine.Generation + 1
	_, err = tx.ExecContext(ctx, `UPDATE dev_machines SET desired_status=$2,generation=$3 WHERE id=$1`, machine.ID, desired, generation)
	if err != nil {
		return false, err
	}
	op := &domain.DevMachineOperation{ID: uuid.New(), WorkspaceID: task.WorkspaceID, MachineID: machine.ID, Action: action, Status: domain.DevMachineOpStatusPending, Generation: generation, IdempotencyKey: key, MaxAttempts: 5}
	if err = enqueueOperation(ctx, tx, op); err != nil {
		return false, err
	}
	return false, nil
}

func (r *AgentTaskRepository) Retry(ctx context.Context, ws, id uuid.UUID, req domain.AgentTaskRetryRequest) (*domain.AgentTask, error) {
	if _, err := conversationActor(ctx, ws); err != nil {
		return nil, err
	}
	req.Prompt = strings.TrimSpace(req.Prompt)
	if req.RequestID == uuid.Nil || req.Generation < 1 || len(req.Prompt) == 0 || len(req.Prompt) > 32768 || !utf8.ValidString(req.Prompt) {
		return nil, ErrConversationInvalid
	}
	tx, task, err := r.transaction(ctx, id, true)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if previous, err := replayAgentTaskRetry(ctx, tx, task, req); err != nil || previous != nil {
		return previous, err
	}
	if task.Generation != req.Generation || (task.State != "failed" && task.State != "blocked" && task.State != "awaiting_review") {
		return nil, ErrAgentTaskConflict
	}
	var pending bool
	err = tx.GetContext(ctx, &pending, `SELECT EXISTS(SELECT 1 FROM dev_machine_agent_runs WHERE task_id=$1 AND status IN ('queued','starting','running','waiting_input'))
 OR EXISTS(SELECT 1 FROM dev_machine_operations o WHERE (o.checkout_id=$2 OR o.agent_run_id IN (SELECT id FROM dev_machine_agent_runs WHERE task_id=$1)) AND o.status IN ('pending','leased'))
 OR EXISTS(SELECT 1 FROM dev_machine_agent_runs r WHERE r.id=$3 AND r.status<>'succeeded' AND NOT EXISTS(SELECT 1 FROM dev_machine_operations o WHERE o.agent_run_id=r.id AND o.idempotency_key=$4 AND o.status='completed'))`, task.ID, task.CheckoutID, task.RunID, "task-cancel:"+task.ID.String()+":"+task.RunID.String())
	if err != nil {
		return nil, err
	}
	if pending {
		return nil, ErrAgentTaskConflict
	}
	requested, command, err := resolveAgentTaskCommand(ctx, tx, ws, task.IssueID, req.TestCommand)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO agent_task_retries(task_id,request_id,expected_generation,prompt) VALUES($1,$2,$3,$4)`, id, req.RequestID, req.Generation, req.Prompt); err != nil {
		return nil, err
	}
	instructions, err := resolveScopeDefault(ctx, tx, ws, task.IssueID, "agent_instructions")
	if err != nil {
		return nil, err
	}
	compose, err := resolveScopeDefault(ctx, tx, ws, task.IssueID, "compose_file")
	if err != nil {
		return nil, err
	}
	preview, previewPort, err := resolvePreview(ctx, tx, ws, task.IssueID)
	if err != nil {
		return nil, err
	}
	if err = tx.GetContext(ctx, task, `UPDATE agent_tasks t SET prompt=$2,test_command=$3,validation_command=$4,agent_instructions=$5,compose_file=$6,preview_command=$7,preview_port=$8,generation=generation+1,run_id=gen_random_uuid(),failure_count=0,failure_pending=false,lease_token=NULL,lease_expires_at=NULL,next_poll_at=now(),issue_status_id=i.status_id,issue_updated_at=i.updated_at FROM issues i WHERE t.id=$1 AND i.id=t.issue_id AND i.workspace_id=t.workspace_id AND i.team_id=t.team_id RETURNING t.*`, id, req.Prompt, requested, command, instructions, compose, preview, previewPort); err != nil {
		return nil, err
	}
	if err = transitionAgentTask(ctx, tx, task, "queued", "New execution attempt requested", &task.CreatorID); err != nil {
		return nil, err
	}
	return task, tx.Commit()
}

// RelatedWork is the coordination context placed in a task's native prompt.
func (r *AgentTaskRepository) RelatedWork(ctx context.Context, task domain.AgentTask) ([]domain.RelatedWork, error) {
	return NewIssueDependencyRepository(r.db).RelatedWork(ctx, task.IssueID)
}

// IssueBrief is the issue text placed in a task's native prompt.
func (r *AgentTaskRepository) IssueBrief(ctx context.Context, task domain.AgentTask) (*domain.IssueBrief, error) {
	brief := &domain.IssueBrief{}
	err := r.db.GetContext(ctx, brief, `SELECT identifier_text, title, description FROM issues WHERE id=$1 AND workspace_id=$2`, task.IssueID, task.WorkspaceID)
	return brief, err
}

// NoteWaiting records why a preparing task cannot proceed yet, so the user
// sees it instead of a stale reason. It never changes the task's state.
func (r *AgentTaskRepository) NoteWaiting(ctx context.Context, claimed domain.AgentTask, reason string) error {
	tx, task, err := r.transaction(ctx, claimed.ID, true)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = fenceAgentTask(ctx, tx, *task, claimed); err != nil {
		return err
	}
	if task.State != "preparing" || task.Reason == reason {
		return nil
	}
	if err = tx.GetContext(ctx, task, `UPDATE agent_tasks SET reason=$2,updated_at=now() WHERE id=$1 RETURNING *`, task.ID, reason); err != nil {
		return err
	}
	if task.ConversationID != nil {
		if _, err = conversationEvent(ctx, tx, *task.ConversationID, "task.updated", task); err != nil {
			return err
		}
	}
	return tx.Commit()
}
