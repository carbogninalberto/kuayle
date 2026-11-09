package repository

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/stretchr/testify/require"
)

func TestStatusCategoryRankSQL(t *testing.T) {
	require.Equal(t, "", statusCategoryRankSQL("", "c"))
	require.Equal(t, "", statusCategoryRankSQL("nope,'; DROP TABLE issues;--", "c"))
	require.Equal(t,
		"(CASE c WHEN 'started' THEN 0 WHEN 'backlog' THEN 1 ELSE 2 END)",
		statusCategoryRankSQL(" started,bogus,backlog,started", "c"),
	)
}

// With group_by=status the first page follows the requested category order, so
// a custom workflow order does not start with the backlog.
func TestListStatusCategoryOrderPostgres(t *testing.T) {
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

	user, workspace, team := uuid.New(), uuid.New(), uuid.New()
	exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Category order','hash')`, user, user.String()+"@example.test")
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, user) })
	exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Category order',$2,$3)`, workspace, workspace.String(), user)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, workspace) })
	exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner')`, workspace, user)
	exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Order','ORD')`, team, workspace)

	categories := []string{"backlog", "unstarted", "started", "completed", "cancelled"}
	statuses := map[string]uuid.UUID{}
	for i, category := range categories {
		id := uuid.New()
		statuses[category] = id
		exec(`INSERT INTO team_statuses(id,team_id,name,slug,category,position) VALUES($1,$2,$3,$3,$4,$5)`, id, team, category, category, i)
	}
	number := 0
	for _, category := range categories {
		for n := 0; n < 2; n++ {
			number++
			exec(`INSERT INTO issues(id,workspace_id,team_id,status_id,number,identifier_text,title,creator_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`,
				uuid.New(), workspace, team, statuses[category], number, "ORD-"+uuid.NewString()[:6], category, user)
		}
	}

	ctx := domain.WithAccess(context.Background(), domain.Access{UserID: user, WorkspaceID: workspace})
	repo := NewIssueRepository(db)
	firstPage := func(order string) []string {
		t.Helper()
		params := dto.IssueFilterParams{TeamID: team.String(), GroupBy: "status", Sort: "sort_order", Order: "asc", StatusCategoryOrder: order}
		params.PerPage = 4
		list, total, err := repo.List(ctx, workspace, params)
		require.NoError(t, err)
		require.Equal(t, 10, total)
		titles := make([]string, len(list))
		for i, issue := range list {
			titles[i] = issue.Title
		}
		return titles
	}

	require.Equal(t, []string{"backlog", "backlog", "unstarted", "unstarted"}, firstPage(""), "without an order the team's status positions apply")
	require.Equal(t, []string{"started", "started", "unstarted", "unstarted"}, firstPage("started,unstarted,backlog,completed,cancelled"))
	require.Equal(t, []string{"completed", "completed", "started", "started"}, firstPage("completed,started"), "unlisted categories follow the listed ones")
}
