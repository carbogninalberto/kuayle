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
	issues, err := r.createSplit(ctx, tx, access, task, request, &access.UserID, map[string]any{})
	if err != nil {
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

// createSplit creates the children a lead's accepted split request proposed,
// for access (the task's creator), with issue:create on the parent's team
// applied at the write; it audits them as split_approved by actor (nil: no
// person, a grant), with detail's extra fields, and replies to the request
// in the room, in tx.
func (r *AgentTaskRepository) createSplit(ctx context.Context, tx *sqlx.Tx, access domain.Access, task *domain.AgentTask, request domain.TaskMessage, actor *uuid.UUID, detail map[string]any) ([]domain.Issue, error) {
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
	detail["request_seq"], detail["parent_id"], detail["issue_ids"] = request.Seq, task.IssueID, ids
	if err = workflowAudit(ctx, tx, task.WorkspaceID, actor, "task_workflow", task.ID, "split_approved", detail); err != nil {
		return nil, err
	}
	value, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	_, err = postTaskMessageTx(ctx, tx, task, domain.TaskRoomPost{
		AuthorKind: domain.RoomAuthorKuayle, Kind: domain.RoomFact, ReplyTo: &request.Seq,
		Body: "Created sub-issues " + strings.Join(identifiers, ", "), Data: domain.TaskRoomData{Fact: "split_approved", Value: value},
	})
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE agent_tasks SET workflow_wake_at=now() WHERE id=$1 AND pipeline AND state='awaiting_review'`, task.ID)
	return issues, err
}

// grantedSplit is a lead's accepted split request of task $2 in workspace
// $1 that the task's creator ($3) granted before it was made, with a
// structured split grant to the lead (KUA-313), and that Kuayle has neither
// carried out nor given up: the oldest one, with the latest grant before it.
const grantedSplit = `SELECT m.*,g.seq AS grant_seq FROM task_messages m
 JOIN task_members lead ON lead.id=m.author_member_id AND lead.workspace_id=m.workspace_id AND lead.task_id=m.task_id AND lead.role=m.author_role
 JOIN LATERAL (SELECT g.seq FROM task_messages g WHERE g.workspace_id=m.workspace_id AND g.task_id=m.task_id AND g.seq<m.seq
  AND g.author_kind='human' AND g.author_user_id=$3 AND g.kind='grant' AND g.reject_reason IS NULL
  AND g.data->>'role'='lead' AND g.data->>'action'='split' ORDER BY g.seq DESC LIMIT 1) g ON true
 WHERE m.workspace_id=$1 AND m.task_id=$2 AND m.reject_reason IS NULL
 AND m.author_kind='member' AND m.author_role='lead' AND m.kind='request' AND m.data->>'action'='split'
 AND NOT EXISTS(SELECT 1 FROM workflow_audit_events e WHERE e.workspace_id=$1 AND e.entity='task_workflow' AND e.entity_id=$2
  AND e.action IN ('split_approved','split_refused') AND e.detail->'request_seq'=to_jsonb(m.seq))
 ORDER BY m.seq LIMIT 1`

// CarryOutGrantedSplits creates the children of each split the lead
// requested under the creator's split grant (KUA-313), without the
// approval step: as the creator approving it would (ApproveTaskSplit),
// once each, audited with the grant's sequence and no person acting. Only a
// structured grant posted by the creator before the request counts; text
// never grants it. A request Kuayle cannot carry out for the creator (the
// creator may no longer create issues there, the proposal is no longer
// valid) is told in the room, audited as split_refused, and not tried
// again; the creator can still approve it. Nothing is done while the
// creator no longer reaches the task. It returns the issues created.
func (r *AgentTaskRepository) CarryOutGrantedSplits(ctx context.Context, ws, id uuid.UUID) ([]domain.Issue, error) {
	var creator uuid.UUID
	if err := r.db.GetContext(ctx, &creator, `SELECT creator_id FROM agent_tasks WHERE id=$1 AND workspace_id=$2`, id, ws); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrConversationNotFound
	} else if err != nil {
		return nil, err
	}
	access := domain.Access{WorkspaceID: ws, UserID: creator}
	var created []domain.Issue
	for range maxGrantedSplitsPerPass {
		// Most passes find nothing: look before taking the team's
		// numbering lock.
		var pending bool
		if err := r.db.GetContext(ctx, &pending, `SELECT EXISTS(`+grantedSplit+`)`, ws, id, creator); err != nil || !pending {
			return created, err
		}
		issues, found, err := r.carryOutGrantedSplit(ctx, access, id)
		if errors.Is(err, ErrConversationNotFound) {
			// The creator no longer reaches the task: nothing is done for
			// them, as for every other post of the task.
			return created, nil
		}
		if err != nil || !found {
			return created, err
		}
		created = append(created, issues...)
	}
	return created, nil
}

// maxGrantedSplitsPerPass bounds the granted splits one call carries out;
// the next pass takes the rest.
const maxGrantedSplitsPerPass = 10

// carryOutGrantedSplit carries out the oldest granted split, or records why
// it cannot, in a transaction of its own; found is false when there is none.
func (r *AgentTaskRepository) carryOutGrantedSplit(ctx context.Context, access domain.Access, id uuid.UUID) (issues []domain.Issue, found bool, err error) {
	ctx = domain.WithAccess(ctx, access)
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	task, err := lockTaskForSplit(ctx, tx, access, id)
	if err != nil {
		return nil, false, err
	}
	var row struct {
		domain.TaskMessage
		GrantSeq int64 `db:"grant_seq"`
	}
	if err = tx.GetContext(ctx, &row, grantedSplit, access.WorkspaceID, id, access.UserID); errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	} else if err != nil {
		return nil, false, err
	}
	request := row.TaskMessage
	pipeline, err := taskPipeline(ctx, tx, task)
	if err != nil {
		return nil, false, err
	}
	post := domain.TaskRoomPost{AuthorKind: request.AuthorKind, AuthorRole: *request.AuthorRole, AuthorMemberID: request.AuthorMemberID, Kind: request.Kind, Data: request.Data}
	why := domain.TaskRoomRefusal(*pipeline, post, false)
	detail := map[string]any{"grant_sequence": row.GrantSeq, "author_role": domain.PipelineLeadRole, "run_id": request.RunID}
	if why == "" {
		if _, err = tx.ExecContext(ctx, `SAVEPOINT granted_split`); err != nil {
			return nil, false, err
		}
		issues, err = r.createSplit(ctx, tx, access, task, request, nil, detail)
		var refused *TaskRoomRefusedError
		switch {
		case err == nil:
			return issues, true, tx.Commit()
		case errors.As(err, &refused):
			why = refused.Reason
		case errors.Is(err, ErrConversationNotFound):
			why = "the task's creator cannot create issues in this team"
		default:
			return nil, false, err
		}
		if _, err = tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT granted_split`); err != nil {
			return nil, false, err
		}
	}
	detail["request_seq"], detail["reason"] = request.Seq, why
	if err = workflowAudit(ctx, tx, task.WorkspaceID, nil, "task_workflow", task.ID, "split_refused", detail); err != nil {
		return nil, false, err
	}
	if _, err = postTaskMessageTx(ctx, tx, task, domain.TaskRoomPost{
		AuthorKind: domain.RoomAuthorKuayle, Kind: domain.RoomFact, ReplyTo: &request.Seq,
		Body: "Kuayle could not create the sub-issues under the lead's split grant: " + why + ". The task's creator can still approve the request.",
		Data: domain.TaskRoomData{Fact: "split_refused"},
	}); err != nil {
		return nil, false, err
	}
	return nil, true, tx.Commit()
}
