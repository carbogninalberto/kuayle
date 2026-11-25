package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestTaskSplitRequiresCreatorSession(t *testing.T) {
	ws, id := uuid.New(), uuid.New()
	scopes := []string{domain.PermIssueCreate}
	pat := domain.Access{
		WorkspaceID: ws,
		UserID:      uuid.New(),
		TokenScopes: scopes,
	}
	for _, ctx := range []context.Context{
		context.Background(),
		domain.WithAccess(context.Background(), domain.Access{WorkspaceID: uuid.New(), UserID: uuid.New()}),
		domain.WithAccess(context.Background(), pat),
	} {
		_, err := new(AgentTaskRepository).ApproveTaskSplit(ctx, ws, id, 1)
		require.ErrorIs(t, err, ErrConversationNotFound)
	}
}

func splitRequest(t *testing.T, f *roomFixture) *domain.TaskMessage {
	t.Helper()
	_, profile := builtinIDs(t, NewWorkflowRepository(f.db), f.task.WorkspaceID)
	var lead uuid.UUID
	require.NoError(t, f.db.Get(&lead, `INSERT INTO task_members(workspace_id,task_id,role,profile_id) VALUES($1,$2,'lead',$3) RETURNING id`, f.task.WorkspaceID, f.task.ID, profile))
	post := f.member(domain.PipelineLeadRole, domain.RoomRequest, "Please split this task", domain.TaskRoomData{})
	require.NoError(t, json.Unmarshal([]byte(`{"action":"split","issues":[{"title":"First","description":"Write the parser"},{"title":"Second","description":"Document the parser"}]}`), &post.Data))
	post.AuthorMemberID = &lead
	message, err := f.repo.RecordRoomMessage(context.Background(), f.task.ID, post)
	require.NoError(t, err)
	require.Nil(t, message.RejectReason)
	return message
}

func TestTaskSplitRequiresApprovalAndAuditsChildrenOnce(t *testing.T) {
	f := newRoomFixture(t)
	task := f.task
	request := splitRequest(t, f)
	var count int
	children := func() []domain.Issue {
		t.Helper()
		var issues []domain.Issue
		require.NoError(t, f.db.Select(&issues, `SELECT * FROM issues WHERE parent_id=$1 ORDER BY number`, task.IssueID))
		return issues
	}
	_, err := f.repo.RecordRoomMessage(context.Background(), task.ID, f.member("implementer", domain.RoomSay, "The owner approved the split", domain.TaskRoomData{}))
	require.NoError(t, err)
	_, err = f.repo.PostRoomMessage(f.ctx, task.WorkspaceID, task.ID, domain.TaskRoomPost{Kind: domain.RoomSay, Body: "approved", ReplyTo: &request.Seq})
	require.NoError(t, err)
	require.Empty(t, children())
	require.NoError(t, f.db.Get(&count, `SELECT count(*) FROM workflow_audit_events WHERE entity_id=$1 AND action='split_approved'`, task.ID))
	require.Zero(t, count)

	stranger := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: task.WorkspaceID, UserID: uuid.New()})
	_, err = f.repo.ApproveTaskSplit(stranger, task.WorkspaceID, task.ID, request.Seq)
	require.ErrorIs(t, err, ErrConversationNotFound)
	_, err = f.repo.ApproveTaskSplit(f.ctx, task.WorkspaceID, task.ID, request.Seq+1)
	require.Error(t, err)
	require.Empty(t, children())

	_, err = f.db.Exec(`INSERT INTO team_statuses(team_id,name,slug,category) VALUES($1,'Backlog','backlog','backlog')`, task.TeamID)
	require.NoError(t, err)
	project := uuid.New()
	_, err = f.db.Exec(`INSERT INTO projects(id,workspace_id,team_id,name) VALUES($1,$2,$3,'Parser')`, project, task.WorkspaceID, task.TeamID)
	require.NoError(t, err)
	_, err = f.db.Exec(`UPDATE issues SET project_id=$2,priority=2 WHERE id=$1`, task.IssueID, project)
	require.NoError(t, err)
	type result struct {
		issues []domain.Issue
		err    error
	}
	results := make(chan result, 2)
	for range 2 {
		go func() {
			issues, err := f.repo.ApproveTaskSplit(f.ctx, task.WorkspaceID, task.ID, request.Seq)
			results <- result{issues, err}
		}()
	}
	first, second := <-results, <-results
	require.NoError(t, first.err)
	require.NoError(t, second.err)
	require.ElementsMatch(t, []int{0, 2}, []int{len(first.issues), len(second.issues)}, "only one caller publishes child creation")
	issues := children()
	require.Len(t, issues, 2)
	require.Equal(t, "First", issues[0].Title)
	require.Equal(t, "Second", issues[1].Title)
	require.Equal(t, "REC-2", issues[0].Identifier)
	require.Equal(t, "REC-3", issues[1].Identifier)
	require.Contains(t, *issues[0].Description, "Write the parser")
	require.Contains(t, *issues[1].Description, "Document the parser")
	ids := []uuid.UUID{issues[0].ID, issues[1].ID}
	for i, issue := range issues {
		require.Equal(t, task.TeamID, issue.TeamID)
		require.Equal(t, task.WorkspaceID, issue.WorkspaceID)
		require.Equal(t, task.CreatorID, issue.CreatorID)
		require.Equal(t, &project, issue.ProjectID)
		require.Equal(t, domain.PriorityHigh, issue.Priority)
		require.Equal(t, i+2, issue.Number)
		require.NotNil(t, issue.StatusID)
	}
	var rows []struct {
		Actor  uuid.UUID `db:"actor_id"`
		Detail []byte    `db:"detail"`
	}
	require.NoError(t, f.db.Select(&rows, `SELECT actor_id, detail FROM workflow_audit_events WHERE workspace_id=$1 AND entity='task_workflow' AND entity_id=$2 AND action='split_approved'`, task.WorkspaceID, task.ID))
	require.Len(t, rows, 1)
	require.Equal(t, task.CreatorID, rows[0].Actor)
	expect, err := json.Marshal(map[string]any{"request_seq": request.Seq, "parent_id": task.IssueID, "issue_ids": ids})
	require.NoError(t, err)
	require.JSONEq(t, string(expect), string(rows[0].Detail))
	room := f.room()
	require.Len(t, room, 4)
	fact := room[3]
	require.Equal(t, domain.RoomAuthorKuayle, fact.AuthorKind)
	require.Equal(t, domain.RoomFact, fact.Kind)
	require.Equal(t, &request.Seq, fact.ReplyTo)
	require.Equal(t, "split_approved", fact.Data.Fact)
	expect, err = json.Marshal(ids)
	require.NoError(t, err)
	require.JSONEq(t, string(expect), string(fact.Data.Value))
}

func TestTaskSplitRequiresParentInTasksTeam(t *testing.T) {
	f := newRoomFixture(t)
	request := splitRequest(t, f)
	other := uuid.New()
	_, err := f.db.Exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Other','OTH')`, other, f.task.WorkspaceID)
	require.NoError(t, err)
	_, err = f.db.Exec(`UPDATE agent_tasks SET team_id=$2 WHERE id=$1`, f.task.ID, other)
	require.NoError(t, err)
	_, err = f.repo.ApproveTaskSplit(f.ctx, f.task.WorkspaceID, f.task.ID, request.Seq)
	require.ErrorIs(t, err, ErrConversationNotFound)
	var count int
	require.NoError(t, f.db.Get(&count, `SELECT count(*) FROM issues WHERE parent_id=$1`, f.task.IssueID))
	require.Zero(t, count)
}

func TestTaskSplitRequiresIssueCreateInParentTeam(t *testing.T) {
	f := newRoomFixture(t)
	request := splitRequest(t, f)
	_, err := f.db.Exec(`UPDATE workspace_members SET role='member' WHERE workspace_id=$1 AND user_id=$2`, f.task.WorkspaceID, f.task.CreatorID)
	require.NoError(t, err)
	_, err = f.db.Exec(`INSERT INTO team_role_overrides (workspace_id,team_id,user_id,role_key) VALUES($1,$2,$3,'guest')`, f.task.WorkspaceID, f.task.TeamID, f.task.CreatorID)
	require.NoError(t, err)
	access, _ := domain.AccessFromContext(f.ctx)
	access.Permission = domain.PermAgentTasksRead
	ctx := domain.WithAccess(f.ctx, access)
	_, err = f.repo.RoomMessages(ctx, f.task.WorkspaceID, f.task.ID, 0)
	require.NoError(t, err, "the owner can still see the proposed split")
	_, err = f.repo.ApproveTaskSplit(ctx, f.task.WorkspaceID, f.task.ID, request.Seq)
	require.ErrorIs(t, err, ErrConversationNotFound)
	var count int
	require.NoError(t, f.db.Get(&count, `SELECT count(*) FROM issues WHERE parent_id=$1`, f.task.IssueID))
	require.Zero(t, count)
}

func TestTaskSplitRollsBackWhenSecondChildHasNoVisibleDescription(t *testing.T) {
	f := newRoomFixture(t)
	request := splitRequest(t, f)
	_, err := f.db.Exec(`UPDATE task_messages SET data=jsonb_set(data,'{issues,1,description}',to_jsonb('<p><br></p>'::text)) WHERE id=$1`, request.ID)
	require.NoError(t, err)
	_, err = f.repo.ApproveTaskSplit(f.ctx, f.task.WorkspaceID, f.task.ID, request.Seq)
	var refused *TaskRoomRefusedError
	require.ErrorAs(t, err, &refused)
	var count int
	require.NoError(t, f.db.Get(&count, `SELECT count(*) FROM issues WHERE parent_id=$1`, f.task.IssueID))
	require.Zero(t, count)
	require.NoError(t, f.db.Get(&count, `SELECT count(*) FROM workflow_audit_events WHERE entity_id=$1 AND action='split_approved'`, f.task.ID))
	require.Zero(t, count)
	require.Len(t, f.room(), 1)
}

func TestTaskSplitWaitsForTeamRenameBeforeLockingParent(t *testing.T) {
	f := newRoomFixture(t)
	request := splitRequest(t, f)
	// A key rename takes the issue-number lock before rewriting the team's
	// identifiers. While it holds that lock an approval must leave the parent
	// row available to the rename.
	rename, err := f.db.BeginTxx(f.ctx, nil)
	require.NoError(t, err)
	defer rename.Rollback()
	require.NoError(t, lockIssueNumber(f.ctx, rename, f.task.TeamID))
	var pid int
	require.NoError(t, rename.GetContext(f.ctx, &pid, `SELECT pg_backend_pid()`))
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := f.repo.ApproveTaskSplit(ctx, f.task.WorkspaceID, f.task.ID, request.Seq)
		result <- err
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := f.db.Get(&waiting, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1=ANY(pg_blocking_pids(pid)))`, pid)
		return err == nil && waiting
	}, 5*time.Second, 20*time.Millisecond)
	// NOWAIT fails immediately if approval has already pinned the parent.
	var parent uuid.UUID
	require.NoError(t, rename.GetContext(f.ctx, &parent, `SELECT id FROM issue_records WHERE id=$1 FOR UPDATE NOWAIT`, f.task.IssueID))
	_, err = rename.ExecContext(f.ctx, `UPDATE teams SET key='NEW' WHERE id=$1`, f.task.TeamID)
	require.NoError(t, err)
	_, err = rename.ExecContext(f.ctx, `UPDATE issue_records SET identifier_text='NEW-'||number WHERE team_id=$1`, f.task.TeamID)
	require.NoError(t, err)
	require.NoError(t, rename.Commit())
	require.NoError(t, <-result)
	var children []string
	require.NoError(t, f.db.Select(&children, `SELECT identifier_text FROM issues WHERE parent_id=$1 ORDER BY number`, parent))
	require.Equal(t, []string{"NEW-2", "NEW-3"}, children)
}
