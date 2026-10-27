package repository

import (
	"context"
	"database/sql"
	"errors"
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
	UpdatedAt         time.Time `db:"updated_at" json:"updated_at"`
}

// IssueAgentWork lists an issue's latest coding tasks. Pull requests and reviews
// come only from Kuayle's records of the current attempt, never agent output.
// Callers authorize the issue first.
func (r *AgentTaskRepository) IssueAgentWork(ctx context.Context, ws, issue uuid.UUID) ([]IssueAgentWork, error) {
	work := []IssueAgentWork{}
	err := r.db.SelectContext(ctx, &work, `SELECT t.id,t.state,t.reason,t.provider,t.generation,t.updated_at,
 (SELECT g.url FROM dev_machine_git_refs g WHERE g.agent_run_id=t.run_id AND g.workspace_id=t.workspace_id AND g.ref_type='pull_request' AND g.source='published' ORDER BY g.created_at DESC LIMIT 1) AS pull_request_url,
 (SELECT g.pull_request_number FROM dev_machine_git_refs g WHERE g.agent_run_id=t.run_id AND g.workspace_id=t.workspace_id AND g.ref_type='pull_request' AND g.source='published' ORDER BY g.created_at DESC LIMIT 1) AS pull_request_number,
 (SELECT s.outcome FROM agent_task_signals s WHERE s.task_id=t.id AND s.run_id=t.run_id AND s.kind='review' AND s.action<>'untrusted' AND s.outcome<>'commented' ORDER BY s.created_at DESC LIMIT 1) AS review
 FROM agent_tasks t WHERE t.workspace_id=$1 AND t.issue_id=$2 ORDER BY t.created_at DESC LIMIT 5`, ws, issue)
	return work, err
}

// MergeTarget is the pull request of a task awaiting review, pinned to the
// commit Kuayle published, so a merge never includes later unreviewed pushes.
type MergeTarget struct {
	TaskID     uuid.UUID `db:"id"`
	IssueID    uuid.UUID `db:"issue_id"`
	CreatorID  uuid.UUID `db:"creator_id"`
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
	err := r.db.GetContext(ctx, &target, `SELECT t.id,t.issue_id,t.creator_id,p.repository_full_name,p.pull_request_number,p.url,c.commit_sha
 FROM agent_tasks t
 JOIN LATERAL (SELECT g.repository_full_name,g.pull_request_number,g.url FROM dev_machine_git_refs g WHERE g.agent_run_id=t.run_id AND g.workspace_id=t.workspace_id AND g.ref_type='pull_request' AND g.source='published' AND g.pull_request_number IS NOT NULL AND g.url IS NOT NULL ORDER BY g.created_at DESC LIMIT 1) p ON true
 JOIN LATERAL (SELECT g.commit_sha FROM dev_machine_git_refs g WHERE g.agent_run_id=t.run_id AND g.workspace_id=t.workspace_id AND g.ref_type='commit' AND g.source='published' AND g.commit_sha IS NOT NULL ORDER BY g.created_at DESC LIMIT 1) c ON true
 WHERE t.workspace_id=$1 AND t.state='awaiting_review' AND ($2::uuid IS NULL OR t.issue_id=$2) AND ($3::uuid IS NULL OR t.id=$3)
 ORDER BY t.updated_at DESC LIMIT 1`, ws, issue, task)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoMergeTarget
	}
	if err != nil {
		return nil, err
	}
	return &target, nil
}
