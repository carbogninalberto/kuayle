package repository

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// A label with a project_id is only usable on issues of that project; labels
// without one stay workspace-wide.
func TestProjectScopedLabelsPostgres(t *testing.T) {
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

	user, workspace, team, status := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Label scope','hash')`, user, user.String()+"@example.test")
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, user) })
	exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Label scope',$2,$3)`, workspace, workspace.String(), user)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, workspace) })
	exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner')`, workspace, user)
	exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Scope','SCP')`, team, workspace)
	exec(`INSERT INTO team_statuses(id,team_id,name,slug,category) VALUES($1,$2,'Todo','todo','unstarted')`, status, team)

	projectA, projectB := uuid.New(), uuid.New()
	exec(`INSERT INTO projects(id,workspace_id,name) VALUES($1,$2,'A')`, projectA, workspace)
	exec(`INSERT INTO projects(id,workspace_id,name) VALUES($1,$2,'B')`, projectB, workspace)

	ctx := domain.WithAccess(context.Background(), domain.Access{UserID: user, WorkspaceID: workspace})
	labels, issues := NewLabelRepository(db), NewIssueRepository(db)
	newLabel := func(name string, project *uuid.UUID) *domain.Label {
		l := &domain.Label{ID: uuid.New(), WorkspaceID: workspace, Name: name, Color: "#112233", ProjectID: project}
		require.NoError(t, labels.Create(ctx, l))
		return l
	}
	global, inA, inB := newLabel("global", nil), newLabel("a", &projectA), newLabel("b", &projectB)

	names := func(project *uuid.UUID) []string {
		list, err := labels.ListByWorkspace(ctx, workspace, project)
		require.NoError(t, err)
		out := make([]string, len(list))
		for i, l := range list {
			out[i] = l.Name
		}
		return out
	}
	require.ElementsMatch(t, []string{"global", "a", "b"}, names(nil))
	require.ElementsMatch(t, []string{"global", "a"}, names(&projectA))

	exists := func(project *uuid.UUID, name string) bool {
		ok, err := labels.ExistsByName(ctx, workspace, project, name)
		require.NoError(t, err)
		return ok
	}
	require.True(t, exists(nil, "GLOBAL"))
	require.False(t, exists(&projectA, "global"), "a project may reuse a workspace label's name")
	require.True(t, exists(&projectA, "A"))
	require.False(t, exists(&projectB, "a"))

	issueID := uuid.New()
	exec(`INSERT INTO issues(id,workspace_id,team_id,status_id,number,identifier_text,title,creator_id,project_id) VALUES($1,$2,$3,$4,1,'SCP-1','Scoped',$5,$6)`, issueID, workspace, team, status, user, projectA)

	require.NoError(t, issues.ValidateLabelScope(ctx, &projectA, []uuid.UUID{global.ID, inA.ID}))
	require.ErrorIs(t, issues.ValidateLabelScope(ctx, &projectA, []uuid.UUID{inB.ID}), ErrLabelProjectMismatch)
	require.ErrorIs(t, issues.ValidateLabelScope(ctx, nil, []uuid.UUID{inA.ID}), ErrLabelProjectMismatch, "an issue without a project takes no project label")

	require.NoError(t, issues.SetLabels(ctx, issueID, []uuid.UUID{global.ID, inA.ID, inB.ID}))
	require.Equal(t, 2, count(`SELECT COUNT(*) FROM issue_labels WHERE issue_id=$1`, issueID))
	require.Zero(t, count(`SELECT COUNT(*) FROM issue_labels WHERE issue_id=$1 AND label_id=$2`, issueID, inB.ID), "the other project's label is not attached")

	moved, err := issues.GetByID(ctx, issueID)
	require.NoError(t, err)
	moved.ProjectID = &projectB
	require.NoError(t, issues.Update(ctx, moved))
	require.Equal(t, 1, count(`SELECT COUNT(*) FROM issue_labels WHERE issue_id=$1`, issueID), "moving to project B drops A's label and keeps the global one")
	require.Equal(t, 1, count(`SELECT COUNT(*) FROM issue_labels WHERE issue_id=$1 AND label_id=$2`, issueID, global.ID))

	exec(`DELETE FROM projects WHERE id=$1`, projectB)
	require.Zero(t, count(`SELECT COUNT(*) FROM labels WHERE id=$1`, inB.ID), "deleting a project deletes its labels")
}

// Scoped labels follow their project's visibility, and attaching labels waits
// for a project move in flight instead of racing it.
func TestProjectScopedLabelVisibilityAndMoveLockPostgres(t *testing.T) {
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

	owner, member, workspace, privateTeam, team, status := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, u := range []uuid.UUID{owner, member} {
		exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Label vis','hash')`, u, u.String()+"@example.test")
		u := u
		t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, u) })
	}
	exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Label vis',$2,$3)`, workspace, workspace.String(), owner)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, workspace) })
	exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner'),($1,$3,'member')`, workspace, owner, member)
	exec(`INSERT INTO teams(id,workspace_id,name,key,is_private) VALUES($1,$2,'Secret','SEC',true)`, privateTeam, workspace)
	exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Open','OPN')`, team, workspace)
	exec(`INSERT INTO team_members(team_id,user_id) VALUES($1,$2)`, privateTeam, owner)
	exec(`INSERT INTO team_statuses(id,team_id,name,slug,category) VALUES($1,$2,'Todo','todo','unstarted')`, status, team)

	secretProject, openProject := uuid.New(), uuid.New()
	exec(`INSERT INTO projects(id,workspace_id,name,team_id) VALUES($1,$2,'Secret',$3)`, secretProject, workspace, privateTeam)
	exec(`INSERT INTO projects(id,workspace_id,name) VALUES($1,$2,'Open')`, openProject, workspace)

	ownerCtx := domain.WithAccess(context.Background(), domain.Access{UserID: owner, WorkspaceID: workspace})
	memberCtx := domain.WithAccess(context.Background(), domain.Access{UserID: member, WorkspaceID: workspace})
	labels, issues := NewLabelRepository(db), NewIssueRepository(db)
	newLabel := func(name string, project *uuid.UUID) *domain.Label {
		l := &domain.Label{ID: uuid.New(), WorkspaceID: workspace, Name: name, Color: "#112233", ProjectID: project}
		require.NoError(t, labels.Create(ownerCtx, l))
		return l
	}
	global, secret, open := newLabel("global", nil), newLabel("secret", &secretProject), newLabel("open", &openProject)

	t.Run("a member outside the private team does not see its project labels", func(t *testing.T) {
		list, err := labels.ListByWorkspace(memberCtx, workspace, nil)
		require.NoError(t, err)
		var names []string
		for _, l := range list {
			names = append(names, l.Name)
		}
		require.ElementsMatch(t, []string{"global", "open"}, names)

		hidden, err := labels.GetByID(memberCtx, secret.ID)
		require.NoError(t, err)
		require.Nil(t, hidden)

		secret := *secret
		secret.Name = "renamed"
		require.Error(t, labels.Update(memberCtx, &secret), "the update matches no visible row")
		require.NoError(t, labels.Delete(memberCtx, secret.ID))
		var stored domain.Label
		require.NoError(t, db.Get(&stored, `SELECT * FROM labels WHERE id=$1`, secret.ID))
		require.Equal(t, "secret", stored.Name)
		require.Nil(t, stored.DeletedAt, "the hidden label is not deleted")
	})

	t.Run("a team member sees and edits them", func(t *testing.T) {
		list, err := labels.ListByWorkspace(ownerCtx, workspace, nil)
		require.NoError(t, err)
		require.Len(t, list, 3)
		got, err := labels.GetByID(ownerCtx, secret.ID)
		require.NoError(t, err)
		require.NotNil(t, got)
	})

	t.Run("SetLabels waits for a project move in flight", func(t *testing.T) {
		issueID := uuid.New()
		exec(`INSERT INTO issues(id,workspace_id,team_id,status_id,number,identifier_text,title,creator_id,project_id) VALUES($1,$2,$3,$4,1,'OPN-1','Race',$5,$6)`, issueID, workspace, team, status, owner, openProject)

		// The move: project changes, not yet committed.
		move, err := db.BeginTxx(context.Background(), nil)
		require.NoError(t, err)
		defer move.Rollback()
		_, err = move.Exec(`UPDATE issues SET project_id=NULL WHERE id=$1`, issueID)
		require.NoError(t, err)

		done := make(chan error, 1)
		go func() { done <- issues.SetLabels(ownerCtx, issueID, []uuid.UUID{global.ID, open.ID}) }()
		select {
		case err := <-done:
			t.Fatalf("SetLabels did not wait for the move: %v", err)
		case <-time.After(500 * time.Millisecond):
		}

		_, err = move.Exec(`DELETE FROM issue_labels il USING labels l WHERE il.issue_id=$1 AND l.id=il.label_id AND l.project_id IS NOT NULL`, issueID)
		require.NoError(t, err)
		require.NoError(t, move.Commit())
		require.NoError(t, <-done)

		var attached []uuid.UUID
		require.NoError(t, db.Select(&attached, `SELECT label_id FROM issue_labels WHERE issue_id=$1`, issueID))
		require.Equal(t, []uuid.UUID{global.ID}, attached, "the old project's label is not attached to the moved issue")
	})
}
