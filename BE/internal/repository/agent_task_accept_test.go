package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// acceptableTask is a task awaiting review of pull request #12 with a running
// preview, on a team with two completed statuses.
func acceptableTask(t *testing.T) (*sqlx.DB, *AgentTaskRepository, context.Context, domain.AgentTask) {
	t.Helper()
	db, repo, ctx, task := reviewableTask(t)
	_, err := db.Exec(`UPDATE agent_tasks SET preview_state='running' WHERE id=$1`, task.ID)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO team_statuses(team_id,name,slug,category,position) VALUES($1,'Shipped','shipped','completed',1),($1,'Done','done','completed',2)`, task.TeamID)
	require.NoError(t, err)
	return db, repo, ctx, previewTask(t, db, task.ID)
}

func workspaceUser(t *testing.T, db *sqlx.DB, ws uuid.UUID, name, role string) context.Context {
	t.Helper()
	id := uuid.New()
	_, err := db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,$3,'test')`, id, id.String()+"@example.test", name)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, id) })
	_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,$3)`, ws, id, role)
	require.NoError(t, err)
	return domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: id})
}

// Accepting completes the reviewed attempt in Kuayle without GitHub: the
// actor and reason are recorded, the preview stops, the issue moves to its
// team's completed status, and a later merge of the same pull request is a no-op.
func TestAcceptCompletesTaskAwaitingReviewWithoutMerge(t *testing.T) {
	db, repo, ctx, task := acceptableTask(t)

	_, _, err := repo.Accept(ctx, task.WorkspaceID, task.ID, task.Generation+1, true)
	require.ErrorIs(t, err, ErrAgentTaskConflict, "the attempt the user saw must still be current")
	member := workspaceUser(t, db, task.WorkspaceID, "Mona Member", "member")
	_, _, err = repo.Accept(member, task.WorkspaceID, task.ID, task.Generation, true)
	require.ErrorIs(t, err, ErrConversationNotFound, "issue access alone is not enough")
	guest := workspaceUser(t, db, task.WorkspaceID, "Gus Guest", "guest")
	_, _, err = repo.Accept(guest, task.WorkspaceID, task.ID, task.Generation, true)
	require.ErrorIs(t, err, ErrConversationNotFound)
	_, _, err = repo.Accept(ctx, uuid.New(), task.ID, task.Generation, true)
	require.ErrorIs(t, err, ErrConversationNotFound, "another workspace's path")
	require.Equal(t, "awaiting_review", previewTask(t, db, task.ID).State)

	accepted, issue, err := repo.Accept(ctx, task.WorkspaceID, task.ID, task.Generation, true)
	require.NoError(t, err)
	require.Equal(t, "completed", accepted.State)
	require.Equal(t, "Accepted in Kuayle by Environment Race", accepted.Reason)
	require.Equal(t, task.Generation, accepted.Generation)
	require.NotNil(t, issue)
	require.Equal(t, "done", string(issue.Status), "the team's done status is preferred")

	var actor *uuid.UUID
	require.NoError(t, db.Get(&actor, `SELECT actor_id FROM agent_task_transitions WHERE task_id=$1 AND to_state='completed'`, task.ID))
	require.Equal(t, task.CreatorID, *actor)
	require.Equal(t, 1, previewOperations(t, db, task.ID, "stop_preview"), "completion stops the task app")
	var history struct {
		UserID uuid.UUID `db:"user_id"`
		Old    string    `db:"old_value"`
		New    string    `db:"new_value"`
	}
	require.NoError(t, db.Get(&history, `SELECT user_id,old_value,new_value FROM issue_history WHERE issue_id=$1 AND field='status' ORDER BY created_at DESC LIMIT 1`, task.IssueID))
	require.Equal(t, task.CreatorID, history.UserID)
	require.Equal(t, "Done", history.New)
	var events int
	require.NoError(t, db.Get(&events, `SELECT count(*) FROM conversation_events WHERE conversation_id=$1 AND type='task.state_changed' AND payload->>'state'='completed'`, task.ConversationID))
	require.Equal(t, 1, events)

	replay, issue, err := repo.Accept(ctx, task.WorkspaceID, task.ID, task.Generation, true)
	require.NoError(t, err, "a repeated acceptance is idempotent")
	require.Equal(t, "completed", replay.State)
	require.Nil(t, issue, "the issue is moved once")
	var transitions int
	require.NoError(t, db.Get(&transitions, `SELECT count(*) FROM agent_task_transitions WHERE task_id=$1 AND to_state='completed'`, task.ID))
	require.Equal(t, 1, transitions)
	_, _, err = repo.Accept(ctx, task.WorkspaceID, task.ID, task.Generation+1, true)
	require.ErrorIs(t, err, ErrAgentTaskConflict)

	n, err := repo.CompleteMergedPullRequest(context.Background(), task.WorkspaceID, "fixture/repo", 12)
	require.NoError(t, err)
	require.Zero(t, n, "a later merge of an accepted task is a no-op")
	require.Equal(t, "Accepted in Kuayle by Environment Race", previewTask(t, db, task.ID).Reason)
}

// A workspace admin may accept another member's task; an issue already
// completed by a person keeps its status.
func TestAdminAcceptsAndKeepsCompletedIssueStatus(t *testing.T) {
	db, repo, _, task := acceptableTask(t)
	_, err := db.Exec(`UPDATE issues SET status_id=(SELECT id FROM team_statuses WHERE team_id=$2 AND slug='shipped'),status='shipped' WHERE id=$1`, task.IssueID, task.TeamID)
	require.NoError(t, err)
	admin := workspaceUser(t, db, task.WorkspaceID, "Ada Admin", "admin")
	_, _, err = repo.Accept(admin, task.WorkspaceID, task.ID, task.Generation, false)
	require.ErrorIs(t, err, ErrConversationNotFound, "only the creator unless administrators may act")
	accepted, issue, err := repo.Accept(admin, task.WorkspaceID, task.ID, task.Generation, true)
	require.NoError(t, err)
	require.Equal(t, "Accepted in Kuayle by Ada Admin", accepted.Reason)
	require.Nil(t, issue, "a completed issue is not moved")
	var status string
	require.NoError(t, db.Get(&status, `SELECT status FROM issues WHERE id=$1`, task.IssueID))
	require.Equal(t, "shipped", status)
	access, _ := domain.AccessFromContext(admin)
	var actor uuid.UUID
	require.NoError(t, db.Get(&actor, `SELECT actor_id FROM agent_task_transitions WHERE task_id=$1 AND to_state='completed'`, task.ID))
	require.Equal(t, access.UserID, actor)
}

// Only a task awaiting review can be accepted; merged tasks are not accepted again.
func TestAcceptRefusesTasksNotAwaitingReview(t *testing.T) {
	db, repo, ctx, task, claimed := publishingTask(t)
	_, _, err := repo.Accept(ctx, task.WorkspaceID, task.ID, claimed.Generation, true)
	require.ErrorIs(t, err, ErrAgentTaskConflict, "publishing has no reviewed pull request yet")
	_, err = db.Exec(`UPDATE agent_tasks SET state='completed' WHERE id=$1`, task.ID)
	require.NoError(t, err)
	_, _, err = repo.Accept(ctx, task.WorkspaceID, task.ID, claimed.Generation, true)
	require.ErrorIs(t, err, ErrAgentTaskConflict, "a task completed by a merge was not accepted")
}
