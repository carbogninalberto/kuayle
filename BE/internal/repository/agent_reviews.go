package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// ErrNoReviewTarget means the task has no pull request Kuayle published that
// is still awaiting review.
var ErrNoReviewTarget = errors.New("no published pull request awaiting review")

// ErrReviewTargetChanged means a newer commit was published after the review
// was approved.
var ErrReviewTargetChanged = errors.New("the published commit changed")

// reviewTarget is the pull request and commit the current attempt of an
// implementing task published, as MergeTarget pins them.
func reviewTarget(ctx context.Context, q sqlx.QueryerContext, task domain.AgentTask) (*domain.AgentReviewTarget, error) {
	var row struct {
		Identifier string `db:"identifier_text"`
		Title      string `db:"title"`
		Repository string `db:"repository_full_name"`
		Number     int    `db:"pull_request_number"`
		URL        string `db:"url"`
		Branch     string `db:"ref_name"`
		Commit     string `db:"commit_sha"`
		Base       string `db:"base_branch"`
	}
	err := sqlx.GetContext(ctx, q, &row, `SELECT i.identifier_text,i.title,p.repository_full_name,p.pull_request_number,p.url,c.ref_name,c.commit_sha,COALESCE(k.base_branch,'') AS base_branch
 FROM issues i
 JOIN LATERAL (SELECT g.repository_full_name,g.pull_request_number,g.url FROM dev_machine_git_refs g WHERE g.agent_run_id=$1 AND g.workspace_id=$2 AND g.ref_type='pull_request' AND g.source='published' AND g.pull_request_number IS NOT NULL AND g.url IS NOT NULL ORDER BY g.created_at DESC LIMIT 1) p ON true
 JOIN LATERAL (SELECT g.ref_name,g.commit_sha FROM dev_machine_git_refs g WHERE g.agent_run_id=$1 AND g.workspace_id=$2 AND g.ref_type='commit' AND g.source='published' AND g.commit_sha IS NOT NULL ORDER BY g.created_at DESC LIMIT 1) c ON true
 LEFT JOIN dev_machine_checkouts k ON k.id=$4 AND k.workspace_id=$2
 WHERE i.id=$3 AND i.workspace_id=$2`, task.RunID, task.WorkspaceID, task.IssueID, task.CheckoutID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoReviewTarget
	}
	if err != nil {
		return nil, err
	}
	if row.Base == "" {
		row.Base = "main"
	}
	return &domain.AgentReviewTarget{IssueIdentifier: row.Identifier, IssueTitle: row.Title, Repository: row.Repository, PullRequestNumber: row.Number,
		PullRequestURL: row.URL, Branch: row.Branch, BaseBranch: row.Base, CommitSHA: row.Commit}, nil
}

// CreateReview queues a review of an implementing task's published pull
// request as the actor in ctx, from the actor's own issue conversation. The
// actor must currently see the issue as a non-guest member (the conversation
// checks), and the task must await review with a pull request Kuayle
// published. A request is idempotent per task, published commit and harness:
// it returns the existing review unless that one failed or was cancelled.
func (r *AgentTaskRepository) CreateReview(ctx context.Context, ws, conversation, reviewed uuid.UUID, input domain.AgentReviewRequest) (*domain.AgentTask, *domain.AgentReview, error) {
	if input.RequestID == uuid.Nil || input.Provider == "" || len(input.Provider) > 64 || len(input.Model) > 128 || len(input.Thinking) > 32 || len(input.Focus) > 4000 {
		return nil, nil, ErrConversationInvalid
	}
	tx, c, err := NewConversationRepository(r.db).authorized(ctx, ws, conversation)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	if c.IssueID == nil || c.TeamID == nil {
		return nil, nil, ErrConversationInvalid
	}
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "agent-review:"+ws.String()+":"+reviewed.String()); err != nil {
		return nil, nil, err
	}
	// Replaying the same request returns its review, whatever happened since.
	var replay domain.AgentTask
	err = tx.GetContext(ctx, &replay, `SELECT * FROM agent_tasks WHERE workspace_id=$1 AND creator_id=$2 AND request_id=$3`, ws, c.CreatorID, input.RequestID)
	if err == nil {
		if replay.Kind != domain.AgentTaskKindReview || replay.ReviewOfTaskID == nil || *replay.ReviewOfTaskID != reviewed {
			return nil, nil, ErrConversationConflict
		}
		review, err := getReview(ctx, tx, replay.ID)
		return &replay, review, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, nil, err
	}
	var target domain.AgentTask
	err = tx.GetContext(ctx, &target, `SELECT * FROM agent_tasks WHERE id=$1 AND workspace_id=$2 AND issue_id=$3 AND team_id=$4 FOR SHARE`, reviewed, ws, *c.IssueID, *c.TeamID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrConversationNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	if target.Kind != domain.AgentTaskKindImplement || target.State != "awaiting_review" {
		return nil, nil, ErrNoReviewTarget
	}
	published, err := reviewTarget(ctx, tx, target)
	if err != nil {
		return nil, nil, err
	}
	if input.ExpectedCommit != "" && input.ExpectedCommit != published.CommitSHA {
		return nil, nil, ErrReviewTargetChanged
	}
	attempt := 1
	var previous struct {
		TaskID  uuid.UUID `db:"task_id"`
		Attempt int       `db:"attempt"`
		State   string    `db:"state"`
	}
	err = tx.GetContext(ctx, &previous, `SELECT r.task_id,r.attempt,t.state FROM agent_reviews r JOIN agent_tasks t ON t.id=r.task_id
 WHERE r.reviewed_task_id=$1 AND r.commit_sha=$2 AND r.provider=$3 ORDER BY r.attempt DESC LIMIT 1`, reviewed, published.CommitSHA, input.Provider)
	switch {
	case err == nil && previous.State != "failed" && previous.State != "cancelled":
		var existing domain.AgentTask
		if err = tx.GetContext(ctx, &existing, `SELECT * FROM agent_tasks WHERE id=$1`, previous.TaskID); err != nil {
			return nil, nil, err
		}
		review, err := getReview(ctx, tx, existing.ID)
		return &existing, review, err
	case err == nil:
		attempt = previous.Attempt + 1
	case !errors.Is(err, sql.ErrNoRows):
		return nil, nil, err
	}
	prompt := domain.AgentReviewPrompt(*published, input.Focus)
	var task domain.AgentTask
	err = tx.GetContext(ctx, &task, `INSERT INTO agent_tasks(workspace_id,conversation_id,creator_id,issue_id,team_id,request_id,prompt,provider,model,thinking,model_server_id,reason,issue_status_id,issue_updated_at,kind,review_of_task_id)
 SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'Waiting for a machine to review the pull request',i.status_id,i.updated_at,'review',$12 FROM issues i WHERE i.id=$4 AND i.workspace_id=$1 AND i.team_id=$5 RETURNING *`,
		ws, conversation, c.CreatorID, c.IssueID, c.TeamID, input.RequestID, prompt, input.Provider, input.Model, input.Thinking, input.ModelServerID, reviewed)
	if err != nil {
		return nil, nil, err
	}
	comment := "not_requested"
	if input.PostToPullRequest {
		comment = "pending"
	}
	// A requested comment is posted only once the review completed with findings.
	review := &domain.AgentReview{}
	err = tx.GetContext(ctx, review, `INSERT INTO agent_reviews(task_id,workspace_id,reviewed_task_id,requested_by,provider,commit_sha,repository_full_name,pull_request_number,pull_request_url,base_branch,focus,attempt,post_to_pull_request,comment_state)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING *`,
		task.ID, ws, reviewed, c.CreatorID, input.Provider, published.CommitSHA, published.Repository, published.PullRequestNumber, published.PullRequestURL, published.BaseBranch, input.Focus, attempt, input.PostToPullRequest, comment)
	if err != nil {
		return nil, nil, err
	}
	if err = recordAgentTaskTransition(ctx, tx, task, "", &c.CreatorID); err != nil {
		return nil, nil, err
	}
	return &task, review, tx.Commit()
}

func getReview(ctx context.Context, q sqlx.QueryerContext, task uuid.UUID) (*domain.AgentReview, error) {
	review := &domain.AgentReview{}
	if err := sqlx.GetContext(ctx, q, review, `SELECT * FROM agent_reviews WHERE task_id=$1`, task); err != nil {
		return nil, err
	}
	return review, nil
}

// reviewUpdated tells the reviewed task's conversation that one of its
// reviews changed, in the transition's own transaction.
func reviewUpdated(ctx context.Context, tx *sqlx.Tx, task domain.AgentTask) error {
	if task.Kind != domain.AgentTaskKindReview || task.ReviewOfTaskID == nil {
		return nil
	}
	var conversation *uuid.UUID
	err := tx.GetContext(ctx, &conversation, `SELECT conversation_id FROM agent_tasks WHERE id=$1 AND workspace_id=$2`, *task.ReviewOfTaskID, task.WorkspaceID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && conversation == nil) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = conversationEvent(ctx, tx, *conversation, "task.review_updated", map[string]any{"task_id": *task.ReviewOfTaskID, "review_task_id": task.ID, "state": task.State})
	return err
}

// CompleteReview records the reviewer's findings from its succeeded attempt
// and completes the review task, fenced by the worker lease. The findings are
// the harness's final output, parsed from Kuayle's own record of the run.
func (r *AgentTaskRepository) CompleteReview(ctx context.Context, claimed domain.AgentTask) error {
	tx, task, err := r.transaction(ctx, claimed.ID, true)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = fenceAgentTask(ctx, tx, *task, claimed); err != nil {
		return err
	}
	if task.Kind != domain.AgentTaskKindReview || task.State != "running" {
		return ErrAgentTaskConflict
	}
	var summary *string
	err = tx.GetContext(ctx, &summary, `SELECT summary FROM dev_machine_agent_runs WHERE id=$1 AND task_id=$2 AND workspace_id=$3 AND status='succeeded' FOR SHARE`, task.RunID, task.ID, task.WorkspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrAgentTaskConflict
	}
	if err != nil {
		return err
	}
	output := ""
	if summary != nil {
		output = *summary
	}
	verdict, findings := domain.ParseAgentReview(output)
	if findings == "" {
		findings = "The reviewer reported no findings."
	}
	if _, err = tx.ExecContext(ctx, `UPDATE agent_reviews SET verdict=$2,findings=$3,completed_at=now(),updated_at=now() WHERE task_id=$1`, task.ID, verdict, findings); err != nil {
		return err
	}
	if err = transitionAgentTask(ctx, tx, task, "completed", "Review finished: "+verdict, nil); err != nil {
		return err
	}
	return tx.Commit()
}

// TaskSessions lists a task's own agent session and the review sessions of
// its published work, newest first, under the task's current authorization.
func (r *AgentTaskRepository) TaskSessions(ctx context.Context, ws, id uuid.UUID) (*domain.AgentTaskSessions, error) {
	if _, err := conversationActor(ctx, ws); err != nil {
		return nil, err
	}
	tx, task, err := r.transaction(ctx, id, true)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var rows []struct {
		domain.AgentSession
		TaskState string `db:"task_state"`
	}
	if err = tx.SelectContext(ctx, &rows, `SELECT s.*,t.state AS task_state FROM agent_sessions s JOIN agent_tasks t ON t.id=s.task_id AND t.workspace_id=s.workspace_id
 WHERE s.workspace_id=$1 AND (t.id=$2 OR t.review_of_task_id=$2) ORDER BY s.created_at DESC,s.id LIMIT 50`, task.WorkspaceID, task.ID); err != nil {
		return nil, err
	}
	reviews := []domain.AgentReview{}
	if err = tx.SelectContext(ctx, &reviews, `SELECT * FROM agent_reviews WHERE workspace_id=$1 AND reviewed_task_id=$2`, task.WorkspaceID, task.ID); err != nil {
		return nil, err
	}
	byTask := make(map[uuid.UUID]*domain.AgentReview, len(reviews))
	for index := range reviews {
		byTask[reviews[index].TaskID] = &reviews[index]
	}
	result := &domain.AgentTaskSessions{Sessions: make([]domain.AgentSessionView, 0, len(rows))}
	for _, row := range rows {
		result.Sessions = append(result.Sessions, domain.AgentSessionView{AgentSession: row.AgentSession, TaskState: row.TaskState, Review: byTask[row.TaskID]})
	}
	return result, nil
}

// ReviewComment is a requested pull request comment that is due.
type ReviewComment struct {
	Review          domain.AgentReview
	IssueIdentifier string
}

// ClaimReviewComment leases one due pull request comment of a finished
// review. The requester must still be a non-guest member; otherwise the
// comment fails without posting. A lease expires, so a crash retries; the
// poster's marker keeps a retried comment from being duplicated.
func (r *AgentTaskRepository) ClaimReviewComment(ctx context.Context, lease time.Duration) (*ReviewComment, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var claimed ReviewComment
	err = tx.GetContext(ctx, &claimed.Review, `SELECT r.* FROM agent_reviews r JOIN agent_tasks t ON t.id=r.task_id AND t.workspace_id=r.workspace_id
 WHERE r.comment_state='pending' AND t.state='completed' AND r.findings IS NOT NULL AND (r.comment_lease_expires_at IS NULL OR r.comment_lease_expires_at<=now())
 ORDER BY r.completed_at,r.task_id FOR UPDATE OF r SKIP LOCKED LIMIT 1`)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var allowed bool
	if err = tx.GetContext(ctx, &allowed, `SELECT EXISTS(SELECT 1 FROM workspace_members WHERE workspace_id=$1 AND user_id=$2 AND role<>'guest')`, claimed.Review.WorkspaceID, claimed.Review.RequestedBy); err != nil {
		return nil, err
	}
	if !allowed || claimed.Review.CommentAttempts >= 5 {
		if _, err = tx.ExecContext(ctx, `UPDATE agent_reviews SET comment_state='failed',comment_lease_expires_at=NULL,updated_at=now() WHERE task_id=$1`, claimed.Review.TaskID); err != nil {
			return nil, err
		}
		if err = reviewCommentEvent(ctx, tx, claimed.Review); err != nil {
			return nil, err
		}
		return nil, tx.Commit()
	}
	if err = tx.GetContext(ctx, &claimed.Review, `UPDATE agent_reviews SET comment_attempts=comment_attempts+1,comment_lease_expires_at=now()+make_interval(secs=>$2),updated_at=now() WHERE task_id=$1 RETURNING *`, claimed.Review.TaskID, lease.Seconds()); err != nil {
		return nil, err
	}
	if err = tx.GetContext(ctx, &claimed.IssueIdentifier, `SELECT i.identifier_text FROM agent_tasks t JOIN issues i ON i.id=t.issue_id AND i.workspace_id=t.workspace_id WHERE t.id=$1`, claimed.Review.ReviewedTaskID); err != nil {
		return nil, err
	}
	return &claimed, tx.Commit()
}

// FinishReviewComment records a posting attempt. A failed attempt stays due
// until its attempts are exhausted.
func (r *AgentTaskRepository) FinishReviewComment(ctx context.Context, review domain.AgentReview, posted bool) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current domain.AgentReview
	err = tx.GetContext(ctx, &current, `UPDATE agent_reviews SET comment_state=CASE WHEN $3 THEN 'posted' WHEN comment_attempts>=5 THEN 'failed' ELSE 'pending' END,
 comment_lease_expires_at=CASE WHEN $3 OR comment_attempts>=5 THEN NULL ELSE now()+interval '30 seconds' END,updated_at=now()
 WHERE task_id=$1 AND comment_state='pending' AND comment_attempts=$2 RETURNING *`, review.TaskID, review.CommentAttempts, posted)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrAgentTaskConflict
	}
	if err != nil {
		return err
	}
	if err = reviewCommentEvent(ctx, tx, current); err != nil {
		return err
	}
	return tx.Commit()
}

func reviewCommentEvent(ctx context.Context, tx *sqlx.Tx, review domain.AgentReview) error {
	if review.CommentState == "pending" {
		return nil
	}
	var conversation *uuid.UUID
	err := tx.GetContext(ctx, &conversation, `SELECT conversation_id FROM agent_tasks WHERE id=$1`, review.ReviewedTaskID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && conversation == nil) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = conversationEvent(ctx, tx, *conversation, "task.review_updated", map[string]any{"task_id": review.ReviewedTaskID, "review_task_id": review.TaskID, "comment_state": review.CommentState})
	return err
}
