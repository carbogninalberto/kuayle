package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// AcceptedReasonPrefix starts the reason of a task accepted in Kuayle.
const AcceptedReasonPrefix = "Accepted in Kuayle by "

// Accept completes a task awaiting review without merging its pull request.
// The actor in ctx must be the task's creator or, when administrators is set,
// a workspace owner/admin who can see the issue; generation must be the
// attempt they reviewed. HTTP callers pass false: the task payload belongs to
// the creator's private conversation. The
// pull request is left untouched; a later merge of it changes nothing because
// only tasks awaiting review complete on merge. Like a merge, the completion
// stops the task's preview. An unfinished issue moves to its team's completed
// status in the same transaction; the returned issue is non-nil only then, for
// the caller to publish after commit. Repeating an acceptance of the same
// attempt returns the completed task and no issue.
func (r *AgentTaskRepository) Accept(ctx context.Context, ws, id uuid.UUID, generation int64, administrators bool) (*domain.AgentTask, *domain.Issue, error) {
	actor, err := conversationActor(ctx, ws)
	if err != nil {
		return nil, nil, err
	}
	if generation < 1 {
		return nil, nil, ErrConversationInvalid
	}
	tx, err := beginConversationTransaction(ctx, r.db)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	// The issue write lock precedes the conversation and task locks (see Start).
	var issueID uuid.UUID
	err = tx.GetContext(ctx, &issueID, `SELECT i.id FROM issues i JOIN agent_tasks t ON t.issue_id=i.id AND t.workspace_id=i.workspace_id WHERE t.id=$1 AND t.workspace_id=$2 FOR UPDATE OF i`, id, ws)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrConversationNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	var creator uuid.UUID
	if err = tx.GetContext(ctx, &creator, `SELECT creator_id FROM agent_tasks WHERE id=$1`, id); err != nil {
		return nil, nil, err
	}
	// The creator goes through the ordinary task authorization (their private
	// issue conversation and its team scope). An administrator acts on another
	// member's task, so their own role and issue visibility are checked instead.
	if actor != creator && !administrators {
		return nil, nil, ErrConversationNotFound
	}
	task, err := lockAgentTask(ctx, tx, id, actor == creator)
	if err != nil {
		return nil, nil, err
	}
	if task.WorkspaceID != ws || task.Kind != domain.AgentTaskKindImplement {
		return nil, nil, ErrConversationNotFound
	}
	if actor != creator {
		var role string
		err = tx.GetContext(ctx, &role, `SELECT role FROM workspace_members WHERE workspace_id=$1 AND user_id=$2 FOR SHARE`, ws, actor)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && role != domain.RoleOwner && role != domain.RoleAdmin) {
			return nil, nil, ErrConversationNotFound
		}
		if err != nil {
			return nil, nil, err
		}
		var visible bool
		if err = tx.GetContext(ctx, &visible, `SELECT EXISTS(SELECT 1 FROM issues i WHERE i.id=$1 AND i.workspace_id=$2 AND i.team_id=$3 AND `+teamVisible(ctx, "i.team_id")+`)`, task.IssueID, ws, task.TeamID); err != nil {
			return nil, nil, err
		}
		if !visible {
			return nil, nil, ErrConversationNotFound
		}
	}
	if task.Generation != generation {
		return nil, nil, ErrAgentTaskConflict
	}
	if task.State == "completed" {
		// Only an earlier acceptance of this attempt replays; a merge does not.
		var accepted bool
		if err = tx.GetContext(ctx, &accepted, `SELECT EXISTS(SELECT 1 FROM agent_task_transitions WHERE task_id=$1 AND to_state='completed' AND actor_id IS NOT NULL AND starts_with(reason,$2))`, task.ID, AcceptedReasonPrefix); err != nil {
			return nil, nil, err
		}
		if !accepted {
			return nil, nil, ErrAgentTaskConflict
		}
		return task, nil, nil
	}
	if task.State != "awaiting_review" {
		return nil, nil, ErrAgentTaskConflict
	}
	var name string
	if err = tx.GetContext(ctx, &name, `SELECT name FROM users WHERE id=$1`, actor); err != nil {
		return nil, nil, err
	}
	if err = transitionAgentTask(ctx, tx, task, "completed", AcceptedReasonPrefix+acceptorName(name), &actor); err != nil {
		return nil, nil, err
	}
	issue, err := completeAcceptedIssue(ctx, tx, *task, actor)
	if err != nil {
		return nil, nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, nil, err
	}
	return task, issue, nil
}

// completeAcceptedIssue moves an issue that is not yet completed or cancelled
// to its team's completed status, preferring the "done" slug, as the default
// pull-request-merged automation would.
func completeAcceptedIssue(ctx context.Context, tx *sqlx.Tx, task domain.AgentTask, actor uuid.UUID) (*domain.Issue, error) {
	var target struct {
		ID      uuid.UUID `db:"id"`
		Slug    string    `db:"slug"`
		Name    string    `db:"name"`
		OldName string    `db:"old_name"`
	}
	err := tx.GetContext(ctx, &target, `SELECT target.id,target.slug,target.name,previous.name AS old_name FROM issues i
 JOIN team_statuses previous ON previous.id=i.status_id AND previous.team_id=i.team_id
 JOIN team_statuses target ON target.team_id=i.team_id AND target.category='completed'
 WHERE i.id=$1 AND i.workspace_id=$2 AND i.team_id=$3 AND previous.category NOT IN ('completed','cancelled')
 ORDER BY (target.slug='done') DESC,target.position,target.id LIMIT 1`, task.IssueID, task.WorkspaceID, task.TeamID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var issue domain.Issue
	if err = tx.GetContext(ctx, &issue, `UPDATE issues SET status_id=$2,status=$3,updated_at=now() WHERE id=$1 RETURNING *`, task.IssueID, target.ID, target.Slug); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO issue_history(id,issue_id,user_id,field,old_value,new_value) VALUES($1,$2,$3,'status',$4,$5)`, uuid.New(), task.IssueID, actor, target.OldName, target.Name); err != nil {
		return nil, err
	}
	return &issue, nil
}

// acceptorName bounds a display name to 200 characters for the task reason.
func acceptorName(name string) string {
	name = strings.TrimSpace(name)
	if runes := []rune(name); len(runes) > 200 {
		name = string(runes[:200])
	}
	if name == "" {
		return "a workspace member"
	}
	return name
}
