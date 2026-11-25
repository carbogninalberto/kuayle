package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/pkg/sanitize"
)

// ApproveTaskSplit consumes the exact proposal in the creator's room. The task
// lock serializes approvals. A replay returns no newly created issues.
func (r *AgentTaskRepository) ApproveTaskSplit(ctx context.Context, ws, id uuid.UUID, requestSeq int64) ([]domain.Issue, error) {
	access, ok := domain.AccessFromContext(ctx)
	if !ok || access.UserID == uuid.Nil || access.WorkspaceID != ws || access.TokenScopes != nil {
		return nil, ErrConversationNotFound
	}
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	task, err := lockTaskForSplit(ctx, tx, access, id)
	if err != nil {
		return nil, err
	}
	pipeline, err := taskPipeline(ctx, tx, task)
	if err != nil {
		return nil, err
	}
	var request domain.TaskMessage
	err = tx.GetContext(ctx, &request, `SELECT m.* FROM task_messages m
 JOIN task_members lead ON lead.id=m.author_member_id AND lead.workspace_id=m.workspace_id AND lead.task_id=m.task_id AND lead.role=m.author_role
 WHERE m.workspace_id=$1 AND m.task_id=$2 AND m.seq=$3 AND m.reject_reason IS NULL
 AND m.author_kind='member' AND m.author_role='lead' AND m.kind='request' AND m.data->>'action'='split'`, ws, id, requestSeq)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, &TaskRoomRefusedError{"the message is not an accepted lead split request"}
	}
	if err != nil {
		return nil, err
	}
	post := domain.TaskRoomPost{AuthorKind: request.AuthorKind, AuthorRole: *request.AuthorRole, AuthorMemberID: request.AuthorMemberID, Kind: request.Kind, Data: request.Data}
	if reason := domain.TaskRoomRefusal(*pipeline, post, false); reason != "" {
		return nil, &TaskRoomRefusedError{reason}
	}
	var approved bool
	err = tx.GetContext(ctx, &approved, `SELECT EXISTS(SELECT 1 FROM workflow_audit_events WHERE workspace_id=$1 AND entity='task_workflow'
 AND entity_id=$2 AND action='split_approved' AND detail->'request_seq'=to_jsonb($3::bigint))`, ws, id, requestSeq)
	if err != nil || approved {
		return nil, err
	}
	// The route is session-only. Apply issue:create on the parent's stored team
	// at the write as the normal issue creation path does for scoped roles.
	access.TeamPermission = domain.PermIssueCreate
	ctx = domain.WithAccess(ctx, access)
	issues, err := r.createSplitIssues(ctx, tx, task, request.Data.Issues)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, len(issues))
	identifiers := make([]string, len(issues))
	for i, issue := range issues {
		ids[i], identifiers[i] = issue.ID, issue.Identifier
	}
	detail := map[string]any{"request_seq": requestSeq, "parent_id": task.IssueID, "issue_ids": ids}
	if err = workflowAudit(ctx, tx, ws, &access.UserID, "task_workflow", id, "split_approved", detail); err != nil {
		return nil, err
	}
	value, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	_, err = postTaskMessageTx(ctx, tx, task, domain.TaskRoomPost{
		AuthorKind: domain.RoomAuthorKuayle, Kind: domain.RoomFact, ReplyTo: &requestSeq,
		Body: "Created sub-issues " + strings.Join(identifiers, ", "), Data: domain.TaskRoomData{Fact: "split_approved", Value: value},
	})
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE agent_tasks SET workflow_wake_at=now() WHERE id=$1 AND pipeline AND state='awaiting_review'`, id); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return issues, nil
}

// Match team key renames: numbering lock, privacy barrier, then parent row.
// A task's scoped team is looked up without row locks and checked again after
// lockAgentTask has pinned the parent, conversation and task.
func lockTaskForSplit(ctx context.Context, tx *sqlx.Tx, access domain.Access, id uuid.UUID) (*domain.AgentTask, error) {
	var team uuid.UUID
	err := tx.GetContext(ctx, &team, `SELECT team_id FROM agent_tasks WHERE id=$1 AND workspace_id=$2 AND creator_id=$3`, id, access.WorkspaceID, access.UserID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrConversationNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := lockIssueNumber(ctx, tx, team); err != nil {
		return nil, err
	}
	if err := lockConversationPrivacy(ctx, tx); err != nil {
		return nil, err
	}
	task, err := lockAgentTask(ctx, tx, id, true)
	if err == nil && task.TeamID != team {
		return nil, ErrConversationNotFound
	}
	return task, err
}

func (r *AgentTaskRepository) createSplitIssues(ctx context.Context, tx *sqlx.Tx, task *domain.AgentTask, proposals []domain.TaskSplitIssue) ([]domain.Issue, error) {
	var defaults struct {
		ProjectID *uuid.UUID           `db:"project_id"`
		CycleID   *uuid.UUID           `db:"cycle_id"`
		Priority  domain.IssuePriority `db:"priority"`
		Triaged   bool                 `db:"triaged"`
		StatusID  uuid.UUID            `db:"status_id"`
		Status    domain.IssueStatus   `db:"status"`
	}
	// lockAgentTask already holds the live parent row for share. Its workspace
	// and team must still match the pinned task scope.
	err := tx.GetContext(ctx, &defaults, `SELECT p.project_id,c.id AS cycle_id,p.priority,NOT t.triage_enabled AS triaged,s.id AS status_id,s.slug AS status
 FROM issues p JOIN teams t ON t.id=p.team_id AND t.workspace_id=p.workspace_id
 LEFT JOIN cycles c ON c.id=p.cycle_id AND c.team_id=p.team_id AND c.status='active'
 JOIN LATERAL (SELECT id,slug FROM team_statuses WHERE team_id=t.id ORDER BY (slug='backlog') DESC,is_default DESC,position,id LIMIT 1) s ON true
 WHERE p.id=$1 AND p.workspace_id=$2 AND p.team_id=$3 AND `+teamVisible(ctx, "p.team_id"), task.IssueID, task.WorkspaceID, task.TeamID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrConversationNotFound
	}
	if err != nil {
		return nil, err
	}
	repo := NewIssueRepository(r.db)
	issues := make([]domain.Issue, 0, len(proposals))
	for _, proposal := range proposals {
		title := sanitize.PlainText(proposal.Title)
		description := sanitize.SanitizeEditorContent(proposal.Description)
		if strings.TrimSpace(title) == "" || strings.TrimSpace(sanitize.StripHTML(description)) == "" {
			return nil, &TaskRoomRefusedError{"each sub-issue needs a title and description after sanitizing"}
		}
		issue := domain.Issue{
			ID: uuid.New(), WorkspaceID: task.WorkspaceID, TeamID: task.TeamID, ParentID: &task.IssueID,
			CreatorID: task.CreatorID, Title: title, Description: &description,
			ProjectID: defaults.ProjectID, CycleID: defaults.CycleID, Priority: defaults.Priority, Triaged: defaults.Triaged,
			Status: defaults.Status, StatusID: &defaults.StatusID,
		}
		if err := CreateNumberedIssue(ctx, repo, tx, &issue, ""); err != nil {
			return nil, err
		}
		issues = append(issues, issue)
	}
	return issues, nil
}
