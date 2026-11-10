package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// Usage limits: an agent whose provider refused work until a quota resets
// (Claude Code's 5-hour limit, Codex's usage limit) does not fail its task.
// The attempt goes through the usual runtime cleanup, which keeps the
// checkout and the worktree, and then the task is blocked with the provider's
// message instead of failed. At usage_limit_resume_at the worker starts the
// next attempt, which continues the same native session.

// PauseForUsageLimit ends the claimed attempt because of a usage limit. reason
// is the task's reason while paused; waiting is set on the creator's other
// queued tasks for the same agent, which now wait for the reset.
func (r *AgentTaskRepository) PauseForUsageLimit(ctx context.Context, claimed domain.AgentTask, limit domain.UsageLimit, reason, waiting string) error {
	tx, task, err := r.transaction(ctx, claimed.ID, false)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = fenceAgentTask(ctx, tx, *task, claimed); err != nil {
		return err
	}
	now := time.Now()
	resume := limit.ResumeAt(now)
	var resumeAt *time.Time
	// After MaxUsageLimitAutoResumes in a row the user resumes it.
	if task.UsageLimitResumes < domain.MaxUsageLimitAutoResumes {
		resumeAt = &resume
	}
	if _, err = tx.ExecContext(ctx, `UPDATE agent_tasks SET usage_limit_message=$2,usage_limit_resets_at=$3,usage_limit_resume_at=$4 WHERE id=$1`,
		task.ID, limit.Message, limit.ResetsAt, resumeAt); err != nil {
		return err
	}
	// A model server is an API account, not the creator's own subscription.
	if task.ModelServerID == nil {
		if _, err = tx.ExecContext(ctx, `INSERT INTO agent_usage_limit_holds(user_id,provider,resets_at,message) VALUES($1,$2,$3,$4)
 ON CONFLICT (user_id,provider) DO UPDATE SET resets_at=EXCLUDED.resets_at,message=EXCLUDED.message,created_at=now()`,
			task.CreatorID, task.Provider, resume, limit.Message); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE agent_tasks SET reason=$4 WHERE creator_id=$1 AND provider=$2 AND id<>$3 AND state='queued' AND model_server_id IS NULL`,
			task.CreatorID, task.Provider, task.ID, waiting); err != nil {
			return err
		}
	}
	task.UsageLimitMessage = &limit.Message
	if err = beginAgentTaskFailure(ctx, tx, task, reason); err != nil {
		return err
	}
	return tx.Commit()
}

// ResumeDueUsageLimits starts the next attempt of every paused task whose
// resume time has come, and forgets expired holds. It returns how many
// tasks resumed.
func (r *AgentTaskRepository) ResumeDueUsageLimits(ctx context.Context, reason string) (int, error) {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM agent_usage_limit_holds WHERE resets_at<=now()`); err != nil {
		return 0, err
	}
	var due []uuid.UUID
	if err := r.db.SelectContext(ctx, &due, `SELECT id FROM agent_tasks WHERE state='blocked' AND usage_limit_resume_at<=now() ORDER BY usage_limit_resume_at,id LIMIT 20`); err != nil {
		return 0, err
	}
	resumed := 0
	for _, id := range due {
		_, err := r.resumeAfterUsageLimit(ctx, id, true, nil, reason)
		if err == nil {
			resumed++
		} else if err != ErrAgentTaskConflict {
			return resumed, err
		}
	}
	return resumed, nil
}

// ResumeUsageLimitNow is the creator's "Resume now": the next attempt starts
// at once, and the creator's hold for this agent is lifted (they may have
// raised their plan).
func (r *AgentTaskRepository) ResumeUsageLimitNow(ctx context.Context, ws, id uuid.UUID, reason string) (*domain.AgentTask, error) {
	actor, err := conversationActor(ctx, ws)
	if err != nil {
		return nil, err
	}
	return r.resumeAfterUsageLimit(ctx, id, false, &actor, reason)
}

// CancelUsageLimitResume is the creator's "Don't resume": the task stays
// paused, and retrying it is up to them.
func (r *AgentTaskRepository) CancelUsageLimitResume(ctx context.Context, ws, id uuid.UUID) (*domain.AgentTask, error) {
	if _, err := conversationActor(ctx, ws); err != nil {
		return nil, err
	}
	tx, task, err := r.transaction(ctx, id, true)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if task.State != "blocked" || task.UsageLimitMessage == nil {
		return nil, ErrAgentTaskConflict
	}
	if err = tx.GetContext(ctx, task, `UPDATE agent_tasks SET usage_limit_resume_at=NULL,updated_at=now() WHERE id=$1 RETURNING *`, id); err != nil {
		return nil, err
	}
	// Other open views of the chat drop "Resumes at …" too.
	if task.ConversationID != nil {
		if _, err = conversationEvent(ctx, tx, *task.ConversationID, "task.state_changed", *task); err != nil {
			return nil, err
		}
	}
	return task, tx.Commit()
}

func (r *AgentTaskRepository) resumeAfterUsageLimit(ctx context.Context, id uuid.UUID, automatic bool, actor *uuid.UUID, reason string) (*domain.AgentTask, error) {
	tx, task, err := r.transaction(ctx, id, actor != nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if task.State != "blocked" || task.UsageLimitMessage == nil {
		return nil, ErrAgentTaskConflict
	}
	// Checked again under the task lock: the user may have turned it off.
	if automatic && (task.UsageLimitResumeAt == nil || task.UsageLimitResumeAt.After(time.Now())) {
		return nil, ErrAgentTaskConflict
	}
	var pending bool
	if err = tx.GetContext(ctx, &pending, `SELECT EXISTS(SELECT 1 FROM dev_machine_agent_runs WHERE task_id=$1 AND status IN ('queued','starting','running','waiting_input'))
 OR EXISTS(SELECT 1 FROM dev_machine_operations o WHERE (o.checkout_id=$2 OR o.agent_run_id IN (SELECT id FROM dev_machine_agent_runs WHERE task_id=$1)) AND o.status IN ('pending','leased'))`, task.ID, task.CheckoutID); err != nil {
		return nil, err
	}
	if pending {
		return nil, ErrAgentTaskConflict
	}
	if err = tx.GetContext(ctx, task, `UPDATE agent_tasks t SET generation=generation+1,run_id=gen_random_uuid(),failure_count=0,failure_pending=false,sign_in_required=false,
 lease_token=NULL,lease_expires_at=NULL,next_poll_at=now(),issue_status_id=i.status_id,issue_updated_at=i.updated_at,
 usage_limit_resumes=CASE WHEN $2 THEN t.usage_limit_resumes+1 ELSE 0 END,
 usage_limit_message=NULL,usage_limit_resets_at=NULL,usage_limit_resume_at=NULL,resume_native_session=true
 FROM issues i WHERE t.id=$1 AND i.id=t.issue_id AND i.workspace_id=t.workspace_id AND i.team_id=t.team_id RETURNING t.*`, id, automatic); err != nil {
		return nil, err
	}
	if !automatic {
		if _, err = tx.ExecContext(ctx, `DELETE FROM agent_usage_limit_holds WHERE user_id=$1 AND provider=$2`, task.CreatorID, task.Provider); err != nil {
			return nil, err
		}
	}
	// A workflow task continues on its implementing step, as a retry does.
	if err = retryWorkflow(ctx, tx, task, task.CreatorID); err != nil {
		return nil, err
	}
	if err = transitionAgentTask(ctx, tx, task, "queued", reason, actor); err != nil {
		return nil, err
	}
	return task, tx.Commit()
}

// usageLimitHeld is the Claim condition that keeps a queued task waiting
// while its creator's subscription for the agent is limited.
const usageLimitHeld = `(state='queued' AND model_server_id IS NULL AND EXISTS(SELECT 1 FROM agent_usage_limit_holds h
 WHERE h.user_id=agent_tasks.creator_id AND h.provider=agent_tasks.provider AND h.resets_at>now()))`
