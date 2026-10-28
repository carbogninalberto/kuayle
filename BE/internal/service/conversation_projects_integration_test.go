package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

func (f toolFixture) project(t *testing.T, ws uuid.UUID, team *uuid.UUID, name string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := f.db.Exec(`INSERT INTO projects(id,workspace_id,team_id,name) VALUES($1,$2,$3,$4)`, id, ws, team, name)
	require.NoError(t, err)
	return id
}

func listIDs(list []domain.Conversation) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(list))
	for _, c := range list {
		ids = append(ids, c.ID)
	}
	return ids
}

func TestConversationProjectsTagAndFilterOnlyTheOwnersChats(t *testing.T) {
	f := newToolFixture(t)
	var team uuid.UUID
	require.NoError(t, f.db.Get(&team, `SELECT id FROM teams WHERE workspace_id=$1`, f.ws))
	launch := f.project(t, f.ws, &team, "Launch")
	wide := f.project(t, f.ws, nil, "Company")

	tagged, err := f.repo.CreateWithProject(f.ctx, f.ws, "Launch plan", &launch)
	require.NoError(t, err)
	require.NotNil(t, tagged.Project)
	require.Equal(t, "Launch", tagged.Project.Name)
	plain, err := f.repo.Create(f.ctx, f.ws, "Ideas")
	require.NoError(t, err)
	require.Nil(t, plain.Project)

	moved, err := f.repo.SetConversationProject(f.ctx, f.ws, plain.ID, &wide)
	require.NoError(t, err)
	require.Equal(t, wide, *moved.ProjectID)
	require.Equal(t, "Company", moved.Project.Name)
	moved, err = f.repo.SetConversationProject(f.ctx, f.ws, plain.ID, nil)
	require.NoError(t, err)
	require.Nil(t, moved.ProjectID)
	require.Nil(t, moved.Project)

	all, err := f.repo.List(f.ctx, f.ws)
	require.NoError(t, err)
	require.ElementsMatch(t, []uuid.UUID{tagged.ID, plain.ID}, listIDs(all))
	for _, c := range all {
		if c.ID == tagged.ID {
			require.Equal(t, launch, c.Project.ID, "list items carry their project")
		}
	}
	inLaunch, err := f.repo.ListFiltered(f.ctx, f.ws, domain.ConversationFilter{ProjectID: &launch})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{tagged.ID}, listIDs(inLaunch))
	none, err := f.repo.ListFiltered(f.ctx, f.ws, domain.ConversationFilter{NoProject: true})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{plain.ID}, listIDs(none))

	// Tagging never shares: another member sees neither the chat nor its project filter.
	other := uuid.New()
	_, err = f.db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Other','hash')`, other, other.String()+"@test.invalid")
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = f.db.Exec(`DELETE FROM users WHERE id=$1`, other) })
	_, err = f.db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'member')`, f.ws, other)
	require.NoError(t, err)
	otherCtx := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: f.ws, UserID: other})
	theirs, err := f.repo.ListFiltered(otherCtx, f.ws, domain.ConversationFilter{ProjectID: &launch})
	require.NoError(t, err)
	require.Empty(t, theirs, "a project filter never exposes other members' chats")
	_, err = f.repo.SetConversationProject(otherCtx, f.ws, tagged.ID, &wide)
	require.ErrorIs(t, err, repository.ErrConversationNotFound, "only the owner moves a chat")

	// Projects from another workspace are refused, at create and move.
	foreign := newToolFixture(t)
	foreignProject := foreign.project(t, foreign.ws, nil, "Foreign")
	_, err = f.repo.SetConversationProject(f.ctx, f.ws, plain.ID, &foreignProject)
	require.ErrorIs(t, err, repository.ErrConversationNotFound)
	_, err = f.repo.CreateWithProject(f.ctx, f.ws, "Elsewhere", &foreignProject)
	require.ErrorIs(t, err, repository.ErrConversationNotFound)

	// Deleting the project untags its chats without deleting them.
	_, err = f.db.Exec(`DELETE FROM projects WHERE id=$1`, launch)
	require.NoError(t, err)
	none, err = f.repo.ListFiltered(f.ctx, f.ws, domain.ConversationFilter{NoProject: true})
	require.NoError(t, err)
	require.ElementsMatch(t, []uuid.UUID{tagged.ID, plain.ID}, listIDs(none))
}

func TestConversationProjectFromAPrivateTeamIsHiddenAfterLosingAccess(t *testing.T) {
	f := newToolFixture(t)
	private := uuid.New()
	_, err := f.db.Exec(`INSERT INTO teams(id,workspace_id,name,key,is_private) VALUES($1,$2,'Secret','SEC',true)`, private, f.ws)
	require.NoError(t, err)
	secret := f.project(t, f.ws, &private, "Secret roadmap")

	c, err := f.repo.Create(f.ctx, f.ws, "Notes")
	require.NoError(t, err)
	_, err = f.repo.SetConversationProject(f.ctx, f.ws, c.ID, &secret)
	require.ErrorIs(t, err, repository.ErrConversationNotFound, "a project the user cannot see cannot be assigned")

	_, err = f.db.Exec(`INSERT INTO team_members(team_id,user_id) VALUES($1,$2)`, private, f.user)
	require.NoError(t, err)
	tagged, err := f.repo.SetConversationProject(f.ctx, f.ws, c.ID, &secret)
	require.NoError(t, err)
	require.Equal(t, "Secret roadmap", tagged.Project.Name)

	_, err = f.db.Exec(`DELETE FROM team_members WHERE team_id=$1 AND user_id=$2`, private, f.user)
	require.NoError(t, err)
	list, err := f.repo.List(f.ctx, f.ws)
	require.NoError(t, err)
	require.Len(t, list, 1, "the chat stays visible to its owner")
	require.Nil(t, list[0].Project, "a project the user can no longer see is never shown")
	require.Nil(t, list[0].ProjectID)
	hidden, err := f.repo.ListFiltered(f.ctx, f.ws, domain.ConversationFilter{ProjectID: &secret})
	require.NoError(t, err)
	require.Empty(t, hidden)
	none, err := f.repo.ListFiltered(f.ctx, f.ws, domain.ConversationFilter{NoProject: true})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{c.ID}, listIDs(none))
	history, err := f.repo.History(f.ctx, f.ws, c.ID, 0)
	require.NoError(t, err)
	require.Nil(t, history.Conversation.Project)
	require.Nil(t, history.Conversation.ProjectID)
}

func TestIssueConversationsFollowTheirIssuesProject(t *testing.T) {
	f := newToolFixture(t)
	var team uuid.UUID
	require.NoError(t, f.db.Get(&team, `SELECT id FROM teams WHERE workspace_id=$1`, f.ws))
	first := f.project(t, f.ws, &team, "First")
	second := f.project(t, f.ws, &team, "Second")
	issue := uuid.New()
	_, err := f.db.Exec(`INSERT INTO issues(id,workspace_id,team_id,project_id,number,identifier_text,title,creator_id,status_id) VALUES($1,$2,$3,$4,1,'ENG-1','Task',$5,(SELECT id FROM team_statuses WHERE team_id=$3 LIMIT 1))`, issue, f.ws, team, first, f.user)
	require.NoError(t, err)

	chat, err := f.repo.OpenIssue(f.ctx, f.ws, issue)
	require.NoError(t, err)
	require.Equal(t, first, chat.Project.ID, "an issue chat inherits its issue's project")
	inFirst, err := f.repo.ListFiltered(f.ctx, f.ws, domain.ConversationFilter{ProjectID: &first})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{chat.ID}, listIDs(inFirst))

	_, err = f.db.Exec(`UPDATE issues SET project_id=$2 WHERE id=$1`, issue, second)
	require.NoError(t, err)
	inSecond, err := f.repo.ListFiltered(f.ctx, f.ws, domain.ConversationFilter{ProjectID: &second})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{chat.ID}, listIDs(inSecond), "it follows the issue to its new project")
	inFirst, err = f.repo.ListFiltered(f.ctx, f.ws, domain.ConversationFilter{ProjectID: &first})
	require.NoError(t, err)
	require.Empty(t, inFirst)

	_, err = f.repo.SetConversationProject(f.ctx, f.ws, chat.ID, &first)
	require.ErrorIs(t, err, repository.ErrConversationConflict, "an issue chat's project is its issue's")
	_, err = f.db.Exec(`UPDATE issues SET project_id=NULL WHERE id=$1`, issue)
	require.NoError(t, err)
	none, err := f.repo.ListFiltered(f.ctx, f.ws, domain.ConversationFilter{NoProject: true})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{chat.ID}, listIDs(none))
}
