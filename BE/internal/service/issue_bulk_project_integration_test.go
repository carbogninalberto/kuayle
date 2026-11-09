package service

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/realtime"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// Bulk "Set project" moves issues like the single-issue update: references are
// validated before any write, labels of the project an issue leaves are
// dropped, and history records both.
func TestIssueServiceBulkUpdateProjectPostgres(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	exec := func(query string, args ...any) {
		t.Helper()
		_, err := db.Exec(query, args...)
		require.NoError(t, err)
	}
	count := func(query string, args ...any) int {
		t.Helper()
		var n int
		require.NoError(t, db.Get(&n, query, args...))
		return n
	}

	user, workspace, otherWorkspace := uuid.New(), uuid.New(), uuid.New()
	team, privateTeam, status := uuid.New(), uuid.New(), uuid.New()
	exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Bulk project','hash')`, user, user.String()+"@example.test")
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, user) })
	for _, w := range []uuid.UUID{workspace, otherWorkspace} {
		exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Bulk project',$2,$3)`, w, w.String(), user)
		w := w
		t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, w) })
	}
	exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner')`, workspace, user)
	exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Open','BLK')`, team, workspace)
	exec(`INSERT INTO teams(id,workspace_id,name,key,is_private) VALUES($1,$2,'Secret','BSC',true)`, privateTeam, workspace)
	exec(`INSERT INTO team_statuses(id,team_id,name,slug,category) VALUES($1,$2,'Todo','todo','unstarted')`, status, team)

	projectA, projectB, foreign, secret := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	exec(`INSERT INTO projects(id,workspace_id,name) VALUES($1,$2,'A')`, projectA, workspace)
	exec(`INSERT INTO projects(id,workspace_id,name) VALUES($1,$2,'B')`, projectB, workspace)
	exec(`INSERT INTO projects(id,workspace_id,name) VALUES($1,$2,'Foreign')`, foreign, otherWorkspace)
	exec(`INSERT INTO projects(id,workspace_id,team_id,name) VALUES($1,$2,$3,'Secret')`, secret, workspace, privateTeam)

	issueRepo := repository.NewIssueRepository(db)
	historyRepo := repository.NewIssueHistoryRepository(db)
	svc := NewIssueService(issueRepo, repository.NewTeamRepository(db), repository.NewTeamStatusRepository(db), historyRepo, realtime.NewHub(), nil)
	ctx := domain.WithAccess(context.Background(), domain.Access{UserID: user, WorkspaceID: workspace})

	first, second := uuid.New(), uuid.New()
	exec(`INSERT INTO issues(id,workspace_id,team_id,status_id,number,identifier_text,title,creator_id,project_id) VALUES($1,$2,$3,$4,1,'BLK-1','One',$5,$6)`, first, workspace, team, status, user, projectA)
	exec(`INSERT INTO issues(id,workspace_id,team_id,status_id,number,identifier_text,title,creator_id) VALUES($1,$2,$3,$4,2,'BLK-2','Two',$5)`, second, workspace, team, status, user)
	global, inA := uuid.New(), uuid.New()
	exec(`INSERT INTO labels(id,workspace_id,name,color) VALUES($1,$2,'global','#112233')`, global, workspace)
	exec(`INSERT INTO labels(id,workspace_id,name,color,project_id) VALUES($1,$2,'a','#112233',$3)`, inA, workspace, projectA)
	require.NoError(t, issueRepo.SetLabels(ctx, first, []uuid.UUID{global, inA}))

	projectOf := func(id uuid.UUID) *uuid.UUID {
		issue, err := issueRepo.GetByID(ctx, id)
		require.NoError(t, err)
		return issue.ProjectID
	}
	ids := []string{first.String(), second.String()}

	// A project outside the workspace, or in a private team, moves nothing.
	for _, bad := range []uuid.UUID{foreign, secret} {
		raw := bad.String()
		_, err = svc.BulkUpdate(ctx, workspace, user, dto.BulkUpdateIssueRequest{IssueIDs: ids, ProjectID: &raw})
		require.Error(t, err)
		require.Equal(t, &projectA, projectOf(first), "a refused move leaves every issue as it was")
		require.Nil(t, projectOf(second))
	}

	raw := projectB.String()
	updated, err := svc.BulkUpdate(ctx, workspace, user, dto.BulkUpdateIssueRequest{IssueIDs: ids, ProjectID: &raw})
	require.NoError(t, err)
	require.Equal(t, 2, updated)
	require.Equal(t, &projectB, projectOf(first))
	require.Equal(t, &projectB, projectOf(second))
	require.Equal(t, 1, count(`SELECT COUNT(*) FROM issue_labels WHERE issue_id=$1`, first), "A's label is dropped, the workspace label stays")
	require.Equal(t, 1, count(`SELECT COUNT(*) FROM issue_labels WHERE issue_id=$1 AND label_id=$2`, first, global))
	require.Equal(t, 1, count(`SELECT COUNT(*) FROM issue_history WHERE issue_id=$1 AND field='project'`, first))
	require.Equal(t, 1, count(`SELECT COUNT(*) FROM issue_history WHERE issue_id=$1 AND field='labels'`, first))

	updated, err = svc.BulkUpdate(ctx, workspace, user, dto.BulkUpdateIssueRequest{IssueIDs: ids, ProjectID: &raw})
	require.NoError(t, err)
	require.Zero(t, updated, "moving to the project they are already in changes nothing")

	none := ""
	updated, err = svc.BulkUpdate(ctx, workspace, user, dto.BulkUpdateIssueRequest{IssueIDs: ids, ProjectID: &none})
	require.NoError(t, err)
	require.Equal(t, 2, updated)
	require.Nil(t, projectOf(first))
	require.Nil(t, projectOf(second))
}
