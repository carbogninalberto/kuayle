package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// IssueAgentWork is one coding task on an issue as the assistant reports it:
// recorded state, the published pull request and the latest review decision.
type IssueAgentWork struct {
	TaskID            uuid.UUID `db:"id" json:"task_id"`
	State             string    `db:"state" json:"state"`
	Reason            string    `db:"reason" json:"reason"`
	Provider          string    `db:"provider" json:"agent"`
	Attempt           int64     `db:"generation" json:"attempt"`
	PullRequestURL    *string   `db:"pull_request_url" json:"pull_request_url,omitempty"`
	PullRequestNumber *int      `db:"pull_request_number" json:"pull_request_number,omitempty"`
	Review            *string   `db:"review" json:"review,omitempty"`
	// AgentReview is the latest requested agent review: its state, harness,
	// verdict and the start of its untrusted findings.
	AgentReview *json.RawMessage `db:"agent_review" json:"agent_review,omitempty"`
	UpdatedAt   time.Time        `db:"updated_at" json:"updated_at"`
}

// IssueAgentWork lists an issue's latest coding tasks. Pull requests and reviews
// come only from Kuayle's records of the current attempt, never agent output.
// Callers authorize the issue first.
func (r *AgentTaskRepository) IssueAgentWork(ctx context.Context, ws, issue uuid.UUID) ([]IssueAgentWork, error) {
	work := []IssueAgentWork{}
	err := r.db.SelectContext(ctx, &work, `SELECT t.id,t.state,t.reason,t.provider,t.generation,t.updated_at,
 (SELECT g.url FROM dev_machine_git_refs g WHERE g.agent_run_id=t.run_id AND g.workspace_id=t.workspace_id AND g.ref_type='pull_request' AND g.source='published' ORDER BY g.created_at DESC LIMIT 1) AS pull_request_url,
 (SELECT g.pull_request_number FROM dev_machine_git_refs g WHERE g.agent_run_id=t.run_id AND g.workspace_id=t.workspace_id AND g.ref_type='pull_request' AND g.source='published' ORDER BY g.created_at DESC LIMIT 1) AS pull_request_number,
 (SELECT s.outcome FROM agent_task_signals s WHERE s.task_id=t.id AND s.run_id=t.run_id AND s.kind='review' AND s.action<>'untrusted' AND s.outcome<>'commented' ORDER BY s.created_at DESC LIMIT 1) AS review,
 `+latestAgentReview(2000)+`
 FROM agent_tasks t WHERE t.workspace_id=$1 AND t.issue_id=$2 AND t.kind='implement' ORDER BY t.created_at DESC LIMIT 5`, ws, issue)
	return work, err
}

// AgentWorkItem is one coding task in the workspace-wide list, with its issue.
type AgentWorkItem struct {
	Identifier string `db:"identifier" json:"identifier"`
	Title      string `db:"title" json:"title"`
	IssueAgentWork
}

// RecentAgentWork lists the workspace's most recently updated coding tasks on
// issues the caller can see, newest first. Like IssueAgentWork, visibility
// follows the issue's team, not the task's creator. An empty state lists all.
func (r *AgentTaskRepository) RecentAgentWork(ctx context.Context, ws uuid.UUID, state string, limit int) ([]AgentWorkItem, error) {
	actor, err := conversationActor(ctx, ws)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 50 {
		limit = 50
	}
	tx, err := beginConversationTransaction(ctx, r.db)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = conversationMember(ctx, tx, ws, actor); err != nil {
		return nil, err
	}
	work := []AgentWorkItem{}
	err = tx.SelectContext(ctx, &work, `SELECT i.identifier_text AS identifier,i.title,t.id,t.state,t.reason,t.provider,t.generation,t.updated_at,
 (SELECT g.url FROM dev_machine_git_refs g WHERE g.agent_run_id=t.run_id AND g.workspace_id=t.workspace_id AND g.ref_type='pull_request' AND g.source='published' ORDER BY g.created_at DESC LIMIT 1) AS pull_request_url,
 (SELECT g.pull_request_number FROM dev_machine_git_refs g WHERE g.agent_run_id=t.run_id AND g.workspace_id=t.workspace_id AND g.ref_type='pull_request' AND g.source='published' ORDER BY g.created_at DESC LIMIT 1) AS pull_request_number,
 (SELECT s.outcome FROM agent_task_signals s WHERE s.task_id=t.id AND s.run_id=t.run_id AND s.kind='review' AND s.action<>'untrusted' AND s.outcome<>'commented' ORDER BY s.created_at DESC LIMIT 1) AS review,
 `+latestAgentReview(300)+`
 FROM agent_tasks t JOIN issues i ON i.id=t.issue_id AND i.workspace_id=t.workspace_id
 WHERE t.workspace_id=$1 AND t.kind='implement' AND ($3='' OR t.state=$3) AND `+teamVisible(ctx, "i.team_id")+`
 ORDER BY t.updated_at DESC,t.id LIMIT $2`, ws, limit, state)
	return work, err
}

// MergeTarget is the pull request of a task awaiting review, pinned to the
// commit Kuayle published, so a merge never includes later unreviewed pushes.
type MergeTarget struct {
	TaskID     uuid.UUID `db:"id"`
	IssueID    uuid.UUID `db:"issue_id"`
	CreatorID  uuid.UUID `db:"creator_id"`
	Provider   string    `db:"provider"`
	Repository string    `db:"repository_full_name"`
	Number     int       `db:"pull_request_number"`
	URL        string    `db:"url"`
	Commit     string    `db:"commit_sha"`
}

// ErrNoMergeTarget means no task is awaiting review with a published pull request.
var ErrNoMergeTarget = errors.New("no pull request awaiting review")

// MergeTarget finds the newest task awaiting review on an issue, or the given
// task, with the pull request and commit its current attempt published.
func (r *AgentTaskRepository) MergeTarget(ctx context.Context, ws uuid.UUID, issue, task *uuid.UUID) (*MergeTarget, error) {
	var target MergeTarget
	err := r.db.GetContext(ctx, &target, `SELECT t.id,t.issue_id,t.creator_id,t.provider,p.repository_full_name,p.pull_request_number,p.url,c.commit_sha
 FROM agent_tasks t
 JOIN LATERAL (SELECT g.repository_full_name,g.pull_request_number,g.url FROM dev_machine_git_refs g WHERE g.agent_run_id=t.run_id AND g.workspace_id=t.workspace_id AND g.ref_type='pull_request' AND g.source='published' AND g.pull_request_number IS NOT NULL AND g.url IS NOT NULL ORDER BY g.created_at DESC LIMIT 1) p ON true
 JOIN LATERAL (SELECT g.commit_sha FROM dev_machine_git_refs g WHERE g.agent_run_id=t.run_id AND g.workspace_id=t.workspace_id AND g.ref_type='commit' AND g.source='published' AND g.commit_sha IS NOT NULL ORDER BY g.created_at DESC LIMIT 1) c ON true
 WHERE t.workspace_id=$1 AND t.kind='implement' AND t.state='awaiting_review' AND ($2::uuid IS NULL OR t.issue_id=$2) AND ($3::uuid IS NULL OR t.id=$3)
 ORDER BY t.updated_at DESC LIMIT 1`, ws, issue, task)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoMergeTarget
	}
	if err != nil {
		return nil, err
	}
	return &target, nil
}

// FollowUpTarget is the newest implementing task on an issue, which a
// follow-up continues on its branch and pull request.
type FollowUpTarget struct {
	TaskID     uuid.UUID `db:"id"`
	CreatorID  uuid.UUID `db:"creator_id"`
	State      string    `db:"state"`
	Generation int64     `db:"generation"`
}

// FollowUpTarget returns the issue's newest implementing task. Callers
// authorize the issue first; Retry re-checks the task's own authorization.
func (r *AgentTaskRepository) FollowUpTarget(ctx context.Context, ws, issue uuid.UUID) (*FollowUpTarget, error) {
	var target FollowUpTarget
	err := r.db.GetContext(ctx, &target, `SELECT id,creator_id,state,generation FROM agent_tasks WHERE workspace_id=$1 AND issue_id=$2 AND kind='implement' ORDER BY created_at DESC,id LIMIT 1`, ws, issue)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoMergeTarget
	}
	if err != nil {
		return nil, err
	}
	return &target, nil
}

// latestAgentReview selects a task's newest requested agent review as JSON,
// with its findings shortened to the given number of characters.
func latestAgentReview(findings int) string {
	return fmt.Sprintf(`(SELECT json_build_object('review_task_id',ar.task_id,'state',rt.state,'agent',ar.provider,'verdict',ar.verdict,'commit',ar.commit_sha,'findings',left(ar.findings,%d),'pull_request_comment',ar.comment_state)
 FROM agent_reviews ar JOIN agent_tasks rt ON rt.id=ar.task_id WHERE ar.reviewed_task_id=t.id AND ar.workspace_id=t.workspace_id ORDER BY ar.created_at DESC LIMIT 1) AS agent_review`, findings)
}
