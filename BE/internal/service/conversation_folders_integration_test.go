package service

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

func TestConversationFoldersArePrivateAndDeletingThemUngroups(t *testing.T) {
	f := newToolFixture(t)
	other := uuid.New()
	_, err := f.db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Other','hash')`, other, other.String()+"@test.invalid")
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = f.db.Exec(`DELETE FROM users WHERE id=$1`, other) })
	_, err = f.db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'member')`, f.ws, other)
	require.NoError(t, err)
	otherCtx := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: f.ws, UserID: other})

	_, err = f.repo.CreateFolder(f.ctx, f.ws, "   ")
	require.ErrorIs(t, err, repository.ErrConversationInvalid)
	folder, err := f.repo.CreateFolder(f.ctx, f.ws, " Releases ")
	require.NoError(t, err)
	require.Equal(t, "Releases", folder.Name)
	theirs, err := f.repo.CreateFolder(otherCtx, f.ws, "Theirs")
	require.NoError(t, err)
	mine, err := f.repo.Folders(f.ctx, f.ws)
	require.NoError(t, err)
	require.Len(t, mine, 1, "folders are private to their owner")

	c, err := f.repo.Create(f.ctx, f.ws, "Planning")
	require.NoError(t, err)
	_, err = f.repo.MoveConversation(f.ctx, f.ws, c.ID, &theirs.ID)
	require.ErrorIs(t, err, repository.ErrConversationNotFound, "cannot file into someone else's folder")
	_, err = f.repo.MoveConversation(otherCtx, f.ws, c.ID, &theirs.ID)
	require.ErrorIs(t, err, repository.ErrConversationNotFound, "cannot move someone else's conversation")
	moved, err := f.repo.MoveConversation(f.ctx, f.ws, c.ID, &folder.ID)
	require.NoError(t, err)
	require.Equal(t, folder.ID, *moved.FolderID)
	renamed, err := f.repo.RenameFolder(f.ctx, f.ws, folder.ID, "Shipping")
	require.NoError(t, err)
	require.Equal(t, "Shipping", renamed.Name)
	require.ErrorIs(t, f.repo.DeleteFolder(otherCtx, f.ws, folder.ID), repository.ErrConversationNotFound)
	require.NoError(t, f.repo.DeleteFolder(f.ctx, f.ws, folder.ID))
	list, err := f.repo.List(f.ctx, f.ws)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Nil(t, list[0].FolderID, "deleting a folder keeps its conversations at the root")
}

func TestRenamingAConversationIsLimitedToItsOwner(t *testing.T) {
	f := newToolFixture(t)
	c, err := f.repo.Create(f.ctx, f.ws, "Planning")
	require.NoError(t, err)
	for _, bad := range []string{"   ", strings.Repeat("x", 201), "a\x00b"} {
		_, err = f.repo.RenameConversation(f.ctx, f.ws, c.ID, bad)
		require.ErrorIs(t, err, repository.ErrConversationInvalid, "%q", bad)
	}
	stranger := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: f.ws, UserID: uuid.New()})
	_, err = f.repo.RenameConversation(stranger, f.ws, c.ID, "Hijacked")
	require.Error(t, err, "only the owner renames a conversation")
	renamed, err := f.repo.RenameConversation(f.ctx, f.ws, c.ID, "  Release plan  ")
	require.NoError(t, err)
	require.Equal(t, "Release plan", renamed.Title)
	list, err := f.repo.List(f.ctx, f.ws)
	require.NoError(t, err)
	require.Equal(t, "Release plan", list[0].Title)
}

func TestDeletingAConversationWaitsForRepliesAndActiveAgentWork(t *testing.T) {
	f := newToolFixture(t)
	a := f.attempt(t, "Plan the release")
	require.ErrorIs(t, f.repo.Delete(f.ctx, f.ws, a.Conversation.ID), repository.ErrConversationBusy, "a reply is still streaming")
	_, err := f.db.Exec(`UPDATE conversation_messages SET status='completed',lease_token=NULL,lease_expires_at=NULL WHERE conversation_id=$1`, a.Conversation.ID)
	require.NoError(t, err)
	stranger := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: f.ws, UserID: uuid.New()})
	require.Error(t, f.repo.Delete(stranger, f.ws, a.Conversation.ID))
	require.NoError(t, f.repo.Delete(f.ctx, f.ws, a.Conversation.ID))
	_, err = f.repo.History(f.ctx, f.ws, a.Conversation.ID, 0)
	require.ErrorIs(t, err, repository.ErrConversationNotFound)
}

func TestDeletingAConversationIsRefusedWhileItsAgentTaskIsActive(t *testing.T) {
	f := newToolFixture(t)
	var team uuid.UUID
	require.NoError(t, f.db.Get(&team, `SELECT id FROM teams WHERE workspace_id=$1`, f.ws))
	issue := uuid.New()
	_, err := f.db.Exec(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,creator_id,status_id) VALUES($1,$2,$3,1,'ENG-1','Task',$4,(SELECT id FROM team_statuses WHERE team_id=$3 LIMIT 1))`, issue, f.ws, team, f.user)
	require.NoError(t, err)
	chat, err := f.repo.OpenIssue(f.ctx, f.ws, issue)
	require.NoError(t, err)
	task, err := repository.NewAgentTaskRepository(f.db).Create(f.ctx, f.ws, chat.ID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Implement", Provider: "codex"})
	require.NoError(t, err)
	require.ErrorIs(t, f.repo.Delete(f.ctx, f.ws, chat.ID), repository.ErrConversationConflict, "queued agent work keeps its conversation")
	_, err = f.db.Exec(`UPDATE agent_tasks SET state='completed' WHERE id=$1`, task.ID)
	require.NoError(t, err)
	require.NoError(t, f.repo.Delete(f.ctx, f.ws, chat.ID))
	var conversation *uuid.UUID
	require.NoError(t, f.db.Get(&conversation, `SELECT conversation_id FROM agent_tasks WHERE id=$1`, task.ID))
	require.Nil(t, conversation, "the finished task keeps its history without the conversation")
}
