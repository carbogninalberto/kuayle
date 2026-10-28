package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// Follow-up budgets per task. CI fixes are automated, so they are kept small;
// change requests come from trusted human reviewers.
const (
	CIFollowUpBudget     = 2
	ReviewFollowUpBudget = 5
	// CommentFollowUpBudget bounds follow-ups from Kuayle issue comments, like
	// change requests from GitHub reviewers.
	CommentFollowUpBudget = 5
)

var signalNamespace = uuid.MustParse("6f1d7c2e-6a63-4c55-9e43-7a0d3f1c9b21")

// RecordSignal stores a GitHub CI or review event for every task whose current
// attempt recorded that pull request (and, for CI, published that commit). It
// returns the follow-up attempts the caller must request, including ones a
// previous delivery recorded but could not queue.
func (r *AgentTaskRepository) RecordSignal(ctx context.Context, ws uuid.UUID, repository string, number int, in domain.AgentTaskSignalInput) ([]domain.AgentTaskFollowUp, error) {
	var candidates []uuid.UUID
	if err := r.db.SelectContext(ctx, &candidates, `SELECT DISTINCT t.id FROM agent_tasks t JOIN dev_machine_git_refs g ON g.agent_run_id=t.run_id AND g.workspace_id=t.workspace_id
 WHERE t.workspace_id=$1 AND g.ref_type='pull_request' AND LOWER(g.repository_full_name)=LOWER($2) AND g.pull_request_number=$3`, ws, repository, number); err != nil {
		return nil, err
	}
	followUps := []domain.AgentTaskFollowUp{}
	for _, id := range candidates {
		followUp, err := r.recordSignal(ctx, id, ws, repository, number, in)
		if err != nil {
			return followUps, err
		}
		if followUp != nil {
			followUps = append(followUps, *followUp)
		}
	}
	return followUps, nil
}

func signalNeedsFollowUp(in domain.AgentTaskSignalInput) bool {
	if in.Kind == "ci" {
		return in.Outcome == "failure" || in.Outcome == "timed_out"
	}
	return in.Outcome == "changes_requested"
}

func (r *AgentTaskRepository) recordSignal(ctx context.Context, id, ws uuid.UUID, repository string, number int, in domain.AgentTaskSignalInput) (*domain.AgentTaskFollowUp, error) {
	tx, task, err := r.transaction(ctx, id, false)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// Re-check under the task lock that the event concerns the current attempt.
	var current bool
	if err = tx.GetContext(ctx, &current, `SELECT EXISTS(SELECT 1 FROM dev_machine_git_refs WHERE agent_run_id=$1 AND workspace_id=$2 AND ref_type='pull_request' AND LOWER(repository_full_name)=LOWER($3) AND pull_request_number=$4)
 AND ($5='' OR EXISTS(SELECT 1 FROM dev_machine_git_refs WHERE agent_run_id=$1 AND workspace_id=$2 AND ref_type='commit' AND commit_sha=$5))`, task.RunID, ws, repository, number, in.HeadSHA); err != nil {
		return nil, err
	}
	if task.WorkspaceID != ws || task.ConversationID == nil || !current || (in.Kind == "ci" && in.HeadSHA == "") {
		return nil, nil
	}
	signal := &domain.AgentTaskSignal{}
	err = tx.GetContext(ctx, signal, `SELECT * FROM agent_task_signals WHERE task_id=$1 AND kind=$2 AND external_id=$3 FOR UPDATE`, task.ID, in.Kind, in.ExternalID)
	existing := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if existing && signal.Action == "follow_up" {
		// Already decided; resume a follow-up that was recorded but not queued.
		if signal.FollowUpGeneration == nil {
			return &domain.AgentTaskFollowUp{Signal: *signal, Task: *task}, tx.Commit()
		}
		return nil, tx.Commit()
	}
	action := "recorded"
	if signalNeedsFollowUp(in) {
		budget := CIFollowUpBudget
		if in.Kind == "review" {
			budget = ReviewFollowUpBudget
		}
		var used int
		if err = tx.GetContext(ctx, &used, `SELECT count(*) FROM agent_task_signals WHERE task_id=$1 AND kind=$2 AND action='follow_up'`, task.ID, in.Kind); err != nil {
			return nil, err
		}
		switch {
		case in.Kind == "review" && !in.Trusted:
			action = "untrusted"
		case task.State != "awaiting_review":
			action = "not_reviewable"
		case used >= budget:
			action = "budget_exhausted"
		default:
			action = "follow_up"
		}
	}
	if existing {
		err = tx.GetContext(ctx, signal, `UPDATE agent_task_signals SET outcome=$2,name=$3,summary=$4,url=$5,action=$6,updated_at=now() WHERE id=$1 RETURNING *`, signal.ID, in.Outcome, in.Name, in.Summary, in.URL, action)
	} else {
		requestID := uuid.NewSHA1(signalNamespace, []byte(task.ID.String()+"/"+task.RunID.String()+"/"+in.Kind+"/"+in.ExternalID))
		err = tx.GetContext(ctx, signal, `INSERT INTO agent_task_signals(task_id,run_id,kind,external_id,outcome,name,summary,url,action,request_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING *`, task.ID, task.RunID, in.Kind, in.ExternalID, in.Outcome, in.Name, in.Summary, in.URL, action, requestID)
	}
	if err != nil {
		return nil, err
	}
	if _, err = conversationEvent(ctx, tx, *task.ConversationID, "task.signal_recorded", signal); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	if action == "follow_up" {
		return &domain.AgentTaskFollowUp{Signal: *signal, Task: *task}, nil
	}
	return nil, nil
}

// RecordCommentSignal records a Kuayle issue comment as feedback on the
// issue's latest task while it awaits review. Only current, non-guest members
// who can see the issue may steer the agent; other comments are not recorded.
// A comment is recorded once, and replaying it resumes the same request ID.
func (r *AgentTaskRepository) RecordCommentSignal(ctx context.Context, ws, issue, comment, author uuid.UUID, summary string) (*domain.AgentTaskFollowUp, error) {
	var id uuid.UUID
	err := r.db.GetContext(ctx, &id, `SELECT id FROM agent_tasks WHERE workspace_id=$1 AND issue_id=$2 AND kind='implement' ORDER BY created_at DESC,id LIMIT 1`, ws, issue)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	tx, task, err := r.transaction(ctx, id, false)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if task.WorkspaceID != ws || task.IssueID != issue || task.ConversationID == nil || task.State != "awaiting_review" {
		return nil, nil
	}
	// Re-check the author's current role and issue visibility under the task lock.
	authorAccess := domain.WithAccess(ctx, domain.Access{WorkspaceID: ws, UserID: author})
	var name string
	err = tx.GetContext(ctx, &name, `SELECT left(COALESCE(NULLIF(u.display_name,''),u.name),200) FROM workspace_members wm JOIN users u ON u.id=wm.user_id
 JOIN issues i ON i.id=$3 AND i.workspace_id=wm.workspace_id
 WHERE wm.workspace_id=$1 AND wm.user_id=$2 AND wm.role<>'guest' AND `+teamVisible(authorAccess, "i.team_id")+` FOR SHARE OF wm`, ws, author, issue)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	signal := &domain.AgentTaskSignal{}
	err = tx.GetContext(ctx, signal, `SELECT * FROM agent_task_signals WHERE task_id=$1 AND kind='comment' AND external_id=$2 FOR UPDATE`, task.ID, comment.String())
	if err == nil {
		if signal.Action == "follow_up" && signal.FollowUpGeneration == nil {
			return &domain.AgentTaskFollowUp{Signal: *signal, Task: *task}, tx.Commit()
		}
		return nil, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	var used int
	if err = tx.GetContext(ctx, &used, `SELECT count(*) FROM agent_task_signals WHERE task_id=$1 AND kind='comment' AND action='follow_up'`, task.ID); err != nil {
		return nil, err
	}
	action := "follow_up"
	if used >= CommentFollowUpBudget {
		action = "budget_exhausted"
	}
	requestID := uuid.NewSHA1(signalNamespace, []byte(task.ID.String()+"/"+task.RunID.String()+"/comment/"+comment.String()))
	if err = tx.GetContext(ctx, signal, `INSERT INTO agent_task_signals(task_id,run_id,kind,external_id,outcome,name,summary,action,request_id) VALUES($1,$2,'comment',$3,'commented',$4,$5,$6,$7) RETURNING *`, task.ID, task.RunID, comment.String(), name, summary, action, requestID); err != nil {
		return nil, err
	}
	if _, err = conversationEvent(ctx, tx, *task.ConversationID, "task.signal_recorded", signal); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	if action == "follow_up" {
		return &domain.AgentTaskFollowUp{Signal: *signal, Task: *task}, nil
	}
	return nil, nil
}

// FinishFollowUp records the attempt a signal queued, or that it could not be
// queued. It is idempotent for the same outcome.
func (r *AgentTaskRepository) FinishFollowUp(ctx context.Context, signal uuid.UUID, generation *int64) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var updated domain.AgentTaskSignal
	if generation != nil {
		err = tx.GetContext(ctx, &updated, `UPDATE agent_task_signals SET follow_up_generation=$2,updated_at=now() WHERE id=$1 AND action='follow_up' RETURNING *`, signal, *generation)
	} else {
		err = tx.GetContext(ctx, &updated, `UPDATE agent_task_signals SET action='follow_up_failed',updated_at=now() WHERE id=$1 AND action='follow_up' AND follow_up_generation IS NULL RETURNING *`, signal)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var conversation *uuid.UUID
	if err = tx.GetContext(ctx, &conversation, `SELECT conversation_id FROM agent_tasks WHERE id=$1`, updated.TaskID); err != nil {
		return err
	}
	if conversation != nil {
		if _, err = conversationEvent(ctx, tx, *conversation, "task.signal_recorded", updated); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Signals lists a task's CI and review signals under current task permissions.
func (r *AgentTaskRepository) Signals(ctx context.Context, ws, id uuid.UUID) ([]domain.AgentTaskSignal, error) {
	if _, err := conversationActor(ctx, ws); err != nil {
		return nil, err
	}
	tx, _, err := r.transaction(ctx, id, true)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	signals := []domain.AgentTaskSignal{}
	err = tx.SelectContext(ctx, &signals, `SELECT * FROM agent_task_signals WHERE task_id=$1 ORDER BY created_at DESC,id LIMIT 100`, id)
	return signals, err
}

// PendingFollowUps returns follow-ups that were recorded but not queued, for
// example after a transient failure. GitHub does not redeliver failed webhooks
// automatically, so a background loop resumes them with the same request ID.
func (r *AgentTaskRepository) PendingFollowUps(ctx context.Context, limit int) ([]domain.AgentTaskFollowUp, error) {
	signals := []domain.AgentTaskSignal{}
	if err := r.db.SelectContext(ctx, &signals, `SELECT * FROM agent_task_signals WHERE action='follow_up' AND follow_up_generation IS NULL AND updated_at<now()-interval '30 seconds' ORDER BY updated_at LIMIT $1`, limit); err != nil {
		return nil, err
	}
	followUps := make([]domain.AgentTaskFollowUp, 0, len(signals))
	for _, signal := range signals {
		var task domain.AgentTask
		if err := r.db.GetContext(ctx, &task, `SELECT * FROM agent_tasks WHERE id=$1 AND run_id=$2`, signal.TaskID, signal.RunID); errors.Is(err, sql.ErrNoRows) {
			// The attempt was replaced by other means; this follow-up is obsolete.
			if err = r.FinishFollowUp(ctx, signal.ID, nil); err != nil {
				return nil, err
			}
			continue
		} else if err != nil {
			return nil, err
		}
		followUps = append(followUps, domain.AgentTaskFollowUp{Signal: signal, Task: task})
	}
	return followUps, nil
}
