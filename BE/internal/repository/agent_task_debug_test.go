package repository

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// The trace of a task is every source under the task's current access rules:
// its creator, or an owner/admin who can see the issue (private teams
// included). Everyone else is told the task does not exist.
func TestDebugTraceReadsEverySourceAndFollowsTaskAccess(t *testing.T) {
	db, repo, ctx, task, _ := taskRecoveryFixture(t)
	_, err := db.Exec(`INSERT INTO agent_task_transitions(task_id,from_state,to_state,actor_id,reason) VALUES($1,'queued','preparing',$2,'A test transition')`, task.ID, task.CreatorID)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO agent_task_workflow_steps(task_id,sequence,node_id,node_type,visit,reason,entered_at) VALUES($1,1,'implement','agent',1,'A test step',now())`, task.ID)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO agent_live_messages(workspace_id,task_id,direction,seq,sender,kind,body,state) VALUES($1,$2,'to_agent',1,'user','message','A test message','queued')`, task.WorkspaceID, task.ID)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO agent_task_signals(task_id,run_id,kind,external_id,outcome,name,summary,action,request_id) VALUES($1,$2,'ci','check-1','failure','CI','A test signal','recorded',$3)`, task.ID, task.RunID, uuid.New())
	require.NoError(t, err)

	rows, err := repo.DebugTrace(ctx, task.WorkspaceID, task.ID, TraceLimits{Rows: 100, Transitions: 100})
	require.NoError(t, err)
	require.Equal(t, task.ID, rows.Task.ID)
	require.Equal(t, task.WorkspaceID, rows.Task.WorkspaceID)
	require.NotEmpty(t, rows.Transitions)
	require.NotEmpty(t, rows.Steps)
	require.NotEmpty(t, rows.LiveMessages)
	require.NotEmpty(t, rows.Signals)
	require.NotNil(t, rows.Notebook)

	admin := workspaceUser(t, db, task.WorkspaceID, "Ada Admin", "admin")
	_, err = repo.DebugTrace(admin, task.WorkspaceID, task.ID, TraceLimits{Rows: 100, Transitions: 100})
	require.NoError(t, err, "a workspace admin who can see the issue")
	owner := workspaceUser(t, db, task.WorkspaceID, "Owen Owner", "owner")
	_, err = repo.DebugTrace(owner, task.WorkspaceID, task.ID, TraceLimits{Rows: 100, Transitions: 100})
	require.NoError(t, err, "a workspace owner who can see the issue")

	member := workspaceUser(t, db, task.WorkspaceID, "Mona Member", "member")
	_, err = repo.DebugTrace(member, task.WorkspaceID, task.ID, TraceLimits{Rows: 100, Transitions: 100})
	require.ErrorIs(t, err, ErrConversationNotFound, "an ordinary member is not the creator")
	guest := workspaceUser(t, db, task.WorkspaceID, "Gus Guest", "guest")
	_, err = repo.DebugTrace(guest, task.WorkspaceID, task.ID, TraceLimits{Rows: 100, Transitions: 100})
	require.ErrorIs(t, err, ErrConversationNotFound)

	elsewhere := uuid.New()
	foreign := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: elsewhere, UserID: task.CreatorID})
	_, err = repo.DebugTrace(foreign, elsewhere, task.ID, TraceLimits{Rows: 100, Transitions: 100})
	require.ErrorIs(t, err, ErrConversationNotFound, "another workspace's path")
	_, err = repo.DebugTrace(foreign, task.WorkspaceID, task.ID, TraceLimits{Rows: 100, Transitions: 100})
	require.ErrorIs(t, err, ErrConversationNotFound, "another workspace naming this one")
}

// The repository returns one row more than the limit, which is how the
// service knows there were more rows without reading them.
func TestDebugTraceRowLimitsReturnOneMoreThanKept(t *testing.T) {
	db, repo, ctx, task, _ := taskRecoveryFixture(t)
	_, err := db.Exec(`INSERT INTO agent_task_transitions(task_id,from_state,to_state,reason) VALUES($1,'queued','preparing','One'),($1,'preparing','running','Two'),($1,'running','validating','Three')`, task.ID)
	require.NoError(t, err)
	rows, err := repo.DebugTrace(ctx, task.WorkspaceID, task.ID, TraceLimits{Rows: 1, Transitions: 1})
	require.NoError(t, err)
	require.Len(t, rows.Transitions, 2, "one kept row plus the row that proves more exist")
}

// A private team's task follows the same access rules as a workflow decision:
// its creator (while in the team) and the workspace's owners and admins, who
// see private teams; an ordinary member outside the team does not.
func TestDebugTraceOfAPrivateTeamsTaskIsHiddenFromNonMembers(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db := isolatedPrivacyDatabase(t, url)
	ws, team, issue, owner, creator, admin := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	exec := func(query string, args ...any) {
		t.Helper()
		_, err := db.Exec(query, args...)
		require.NoError(t, err)
	}
	for _, id := range []uuid.UUID{owner, creator, admin} {
		exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Trace test','hash')`, id, id.String()+"@test.invalid")
	}
	exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Trace test',$2,$3)`, ws, ws.String(), owner)
	exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner'),($1,$3,'member'),($1,$4,'admin')`, ws, owner, creator, admin)
	exec(`INSERT INTO teams(id,workspace_id,name,key,is_private) VALUES($1,$2,'Private trace','PTR',true)`, team, ws)
	exec(`INSERT INTO team_members(team_id,user_id) VALUES($1,$2)`, team, creator)
	exec(`INSERT INTO team_statuses(team_id,name,slug,category) VALUES($1,'Todo','todo','unstarted')`, team)
	exec(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,creator_id,status,status_id) VALUES($1,$2,$3,1,'PTR-1','Private issue',$4,'todo',(SELECT id FROM team_statuses WHERE team_id=$3 LIMIT 1))`, issue, ws, team, creator)
	creatorCtx := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: creator})
	adminCtx := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: admin})
	ownerCtx := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: owner})
	chat, err := NewConversationRepository(db).OpenIssue(creatorCtx, ws, issue)
	require.NoError(t, err)
	repo := NewAgentTaskRepository(db)
	task, err := repo.Create(creatorCtx, ws, chat.ID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Keep this trace private", Provider: "codex"})
	require.NoError(t, err)

	_, err = repo.DebugTrace(creatorCtx, ws, task.ID, TraceLimits{Rows: 10, Transitions: 10})
	require.NoError(t, err)
	_, err = repo.DebugTrace(adminCtx, ws, task.ID, TraceLimits{Rows: 10, Transitions: 10})
	require.NoError(t, err, "a workspace admin sees private teams")
	_, err = repo.DebugTrace(ownerCtx, ws, task.ID, TraceLimits{Rows: 10, Transitions: 10})
	require.NoError(t, err, "the workspace owner sees private teams")
	memberCtx := workspaceUser(t, db, ws, "Mona Member", "member")
	_, err = repo.DebugTrace(memberCtx, ws, task.ID, TraceLimits{Rows: 10, Transitions: 10})
	require.ErrorIs(t, err, ErrConversationNotFound, "an ordinary member outside the private team")

	exec(`DELETE FROM team_members WHERE team_id=$1 AND user_id=$2`, team, creator)
	_, err = repo.DebugTrace(creatorCtx, ws, task.ID, TraceLimits{Rows: 10, Transitions: 10})
	require.ErrorIs(t, err, ErrConversationNotFound, "the creator, once out of the team")
}
