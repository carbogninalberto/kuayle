package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestRecentAgentWorkFollowsIssueVisibility(t *testing.T) {
	db, repo, ctx, task := reviewableTask(t)
	work, err := repo.RecentAgentWork(ctx, task.WorkspaceID, "", 50)
	require.NoError(t, err)
	require.Len(t, work, 1)
	item := work[0]
	require.Equal(t, task.ID, item.TaskID)
	require.Equal(t, "REC-1", item.Identifier)
	require.Equal(t, "Recovery", item.Title)
	require.Equal(t, "awaiting_review", item.State)
	require.Equal(t, "codex", item.Provider)
	require.NotNil(t, item.PullRequestURL)
	require.Equal(t, "https://github.com/fixture/repo/pull/12", *item.PullRequestURL)

	work, err = repo.RecentAgentWork(ctx, task.WorkspaceID, "running", 50)
	require.NoError(t, err)
	require.Empty(t, work, "the state filter applies before the limit")
	work, err = repo.RecentAgentWork(ctx, task.WorkspaceID, "awaiting_review", 1)
	require.NoError(t, err)
	require.Len(t, work, 1)

	member := uuid.New()
	_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Member','hash')`, member, member.String()+"@test.invalid")
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'member')`, task.WorkspaceID, member)
	require.NoError(t, err)
	memberCtx := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: task.WorkspaceID, UserID: member})
	work, err = repo.RecentAgentWork(memberCtx, task.WorkspaceID, "", 50)
	require.NoError(t, err)
	require.Len(t, work, 1, "workspace members see tasks on issues they can see, not only their own")

	// Another workspace the member belongs to never lists this task.
	otherWS := uuid.New()
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Other',$2,$3)`, otherWS, otherWS.String(), member)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner')`, otherWS, member)
	require.NoError(t, err)
	work, err = repo.RecentAgentWork(domain.WithAccess(context.Background(), domain.Access{WorkspaceID: otherWS, UserID: member}), otherWS, "", 50)
	require.NoError(t, err)
	require.Empty(t, work)

	_, err = db.Exec(`UPDATE workspace_members SET role='guest' WHERE workspace_id=$1 AND user_id=$2`, task.WorkspaceID, member)
	require.NoError(t, err)
	_, err = repo.RecentAgentWork(memberCtx, task.WorkspaceID, "", 50)
	require.ErrorIs(t, err, ErrConversationNotFound, "guests do not see agent work")

	_, err = repo.RecentAgentWork(context.Background(), task.WorkspaceID, "", 50)
	require.ErrorIs(t, err, ErrConversationNotFound, "an uncredentialed caller sees nothing")
	_, err = repo.RecentAgentWork(domain.WithAccess(context.Background(), domain.Access{WorkspaceID: otherWS, UserID: member}), task.WorkspaceID, "", 50)
	require.ErrorIs(t, err, ErrConversationNotFound)
	outsider := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: task.WorkspaceID, UserID: uuid.New()})
	_, err = repo.RecentAgentWork(outsider, task.WorkspaceID, "", 50)
	require.ErrorIs(t, err, ErrConversationNotFound, "non-members are refused")
}
