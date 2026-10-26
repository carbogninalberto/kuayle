package repository

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// Users, labels, cycles and statuses from another workspace must not become
// references in this one: responses and analytics would expose their emails,
// names and metadata, and the rows would break workspace export.
func TestCrossWorkspaceReferencesRejectedPostgres(t *testing.T) {
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

	member, foreigner := uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{member, foreigner} {
		exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Reference test','hash')`, id, id.String()+"@example.test")
		t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, id) })
	}
	workspace, foreign := uuid.New(), uuid.New()
	for id, owner := range map[uuid.UUID]uuid.UUID{workspace: member, foreign: foreigner} {
		exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Reference test',$2,$3)`, id, id.String(), owner)
		t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, id) })
		exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner')`, id, owner)
	}
	team, foreignTeam := uuid.New(), uuid.New()
	exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Ref','REF')`, team, workspace)
	exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Far','FAR')`, foreignTeam, foreign)
	status, foreignStatus, foreignCycle, foreignLabel := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	exec(`INSERT INTO team_statuses(id,team_id,name,slug,category) VALUES($1,$2,'Todo','todo','unstarted')`, status, team)
	exec(`INSERT INTO team_statuses(id,team_id,name,slug,category) VALUES($1,$2,'Foreign','foreign','started')`, foreignStatus, foreignTeam)
	exec(`INSERT INTO cycles(id,team_id,name,number,status) VALUES($1,$2,'Foreign cycle',1,'active')`, foreignCycle, foreignTeam)
	exec(`INSERT INTO labels(id,workspace_id,name,color) VALUES($1,$2,'Foreign label','#000000')`, foreignLabel, foreign)
	issueID := uuid.New()
	exec(`INSERT INTO issues(id,workspace_id,team_id,status_id,number,identifier_text,title,creator_id) VALUES($1,$2,$3,$4,1,'REF-1','Mine',$5)`, issueID, workspace, team, status, member)

	ctx := domain.WithAccess(context.Background(), domain.Access{UserID: member, WorkspaceID: workspace})
	issues := NewIssueRepository(db)
	count := func(query string, args ...any) int {
		t.Helper()
		var n int
		require.NoError(t, db.Get(&n, query, args...))
		return n
	}

	tx, err := db.BeginTxx(ctx, nil)
	require.NoError(t, err)
	assigned := &domain.Issue{ID: uuid.New(), WorkspaceID: workspace, TeamID: team, Number: 2, Identifier: "REF-2", Title: "Probe", Status: "todo", StatusID: &status, CreatorID: member, AssigneeID: &foreigner}
	createErr := issues.Create(ctx, tx, assigned)
	require.NoError(t, tx.Rollback())
	require.Error(t, createErr, "a non-member assignee must be refused")

	_ = issues.SetAssignees(ctx, issueID, []uuid.UUID{foreigner})
	require.Zero(t, count(`SELECT COUNT(*) FROM issue_assignees WHERE issue_id=$1 AND user_id=$2`, issueID, foreigner))
	require.Zero(t, count(`SELECT COUNT(*) FROM issues WHERE id=$1 AND assignee_id=$2`, issueID, foreigner))

	_ = issues.SetLabels(ctx, issueID, []uuid.UUID{foreignLabel})
	require.Zero(t, count(`SELECT COUNT(*) FROM issue_labels WHERE issue_id=$1`, issueID))

	_, _ = issues.BulkUpdate(ctx, workspace, []uuid.UUID{issueID}, nil, nil, &foreigner, nil, nil, false)
	_, _ = issues.BulkUpdate(ctx, workspace, []uuid.UUID{issueID}, nil, nil, nil, &foreignStatus, nil, false)
	_, _ = issues.BulkUpdate(ctx, workspace, []uuid.UUID{issueID}, nil, nil, nil, nil, &foreignCycle, true)
	require.Equal(t, 1, count(`SELECT COUNT(*) FROM issues WHERE id=$1 AND assignee_id IS NULL AND status_id=$2 AND cycle_id IS NULL`, issueID, status))

	project := &domain.Project{ID: uuid.New(), WorkspaceID: workspace, Name: "Probe", Status: "planned", LeadID: &foreigner}
	require.Error(t, NewProjectRepository(db).Create(ctx, project), "a non-member project lead must be refused")

	ownLabel := uuid.New()
	exec(`INSERT INTO labels(id,workspace_id,name,color) VALUES($1,$2,'Own label','#ffffff')`, ownLabel, workspace)
	require.NoError(t, issues.SetLabels(ctx, issueID, []uuid.UUID{foreignLabel, ownLabel}))
	require.NoError(t, issues.SetAssignees(ctx, issueID, []uuid.UUID{foreigner, member}))
	require.Equal(t, 1, count(`SELECT COUNT(*) FROM issue_labels WHERE issue_id=$1 AND label_id=$2`, issueID, ownLabel))
	require.Equal(t, 1, count(`SELECT COUNT(*) FROM issues WHERE id=$1 AND assignee_id=$2`, issueID, member), "the first valid assignee stays primary")

	template := &domain.IssueTemplate{ID: uuid.New(), WorkspaceID: workspace, Title: "Probe", AssigneeID: &foreigner, CreatedBy: member}
	require.Error(t, NewIssueTemplateRepository(db).Create(ctx, template), "a non-member template assignee must be refused")
}
