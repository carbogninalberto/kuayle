package service

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"testing"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// A public share must never show, through results or counts, a field or an
// issue that its configuration excludes: search looks only at what the share
// shows, public parameters only narrow the stored filters, and filters the
// public path cannot enforce return nothing.
func TestPublicShareFiltersPostgres(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	owner, other, ws, team := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	emails := map[uuid.UUID]string{
		owner: "owner-" + owner.String() + "@example.test",
		// Only this address contains the term, no title or description does.
		other: "zebramail-" + other.String() + "@example.test",
	}
	for id, email := range emails {
		_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Share filters','hash')`, id, email)
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, id) })
	}
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Share filters',$2,$3)`, ws, ws.String(), owner)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, ws) })
	for user, role := range map[uuid.UUID]string{owner: "owner", other: "member"} {
		_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,$3)`, ws, user, role)
		require.NoError(t, err)
	}
	_, err = db.Exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Share filters','SHR')`, team, ws)
	require.NoError(t, err)
	backlogStatus, doneStatus := uuid.New(), uuid.New()
	_, err = db.Exec(`INSERT INTO team_statuses(id,team_id,name,slug,category,position) VALUES($1,$2,'Backlog','backlog','backlog',0),($3,$2,'Done','done','completed',1)`, backlogStatus, team, doneStatus)
	require.NoError(t, err)
	insertIssue := func(number int, title, description, status string, statusID, creator uuid.UUID, priority int, due *string) uuid.UUID {
		t.Helper()
		id := uuid.New()
		_, err := db.Exec(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,description,status,status_id,creator_id,priority,due_date)
			VALUES($1,$2,$3,$4,$5,$6,NULLIF($7,''),$8,$9,$10,$11,CAST($12 AS date))`,
			id, ws, team, number, "SHR-"+string(rune('0'+number)), title, description, status, statusID, creator, priority, due)
		require.NoError(t, err)
		return id
	}
	january, march := "2026-01-10", "2026-03-10"
	alpha := insertIssue(1, "Alpha roadmap", "<p>The hiddenneedle plan</p>", "done", doneStatus, owner, 2, &january)
	insertIssue(2, "Beta 100% ready", "", "backlog", backlogStatus, other, 1, &march)
	insertIssue(3, "Gamma under_score", "", "done", doneStatus, other, 3, nil)
	_, err = db.Exec(`UPDATE issues SET assignee_id=$2 WHERE id=$1`, alpha, other)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO issue_assignees(issue_id,user_id) VALUES($1,$2)`, alpha, other)
	require.NoError(t, err)

	ownerCtx := domain.WithAccess(context.Background(), domain.Access{UserID: owner, WorkspaceID: ws})
	issues := repository.NewIssueRepository(db)
	views := repository.NewViewRepository(db)
	svc := NewSharedLinkService(repository.NewSharedLinkRepository(db), repository.NewWorkspaceRepository(db), repository.NewTeamRepository(db), repository.NewProjectRepository(db), views, issues, repository.NewUserRepository(db), repository.NewTeamStatusRepository(db), "share-filters-test-secret")

	teamID := team.String()
	teamLink := func(filters string, includeDescription bool) string {
		t.Helper()
		link, err := svc.Create(ownerCtx, ws, owner, dto.CreateSharedLinkRequest{Scope: "team", ScopeID: &teamID, Filters: json.RawMessage(filters), IncludeDescription: includeDescription})
		require.NoError(t, err)
		return link.Token
	}
	viewLink := func(viewFilters, linkFilters string) string {
		t.Helper()
		view := &domain.View{ID: uuid.New(), WorkspaceID: ws, CreatorID: owner, Name: "Share filters", Filters: json.RawMessage(viewFilters), IsShared: true}
		require.NoError(t, views.Create(ownerCtx, view))
		viewID := view.ID.String()
		link, err := svc.Create(ownerCtx, ws, owner, dto.CreateSharedLinkRequest{Scope: "view", ScopeID: &viewID, Filters: json.RawMessage(linkFilters)})
		require.NoError(t, err)
		return link.Token
	}
	// list returns the titles, sorted, and checks the count against them: a
	// count that differs from the visible rows is a leak of its own.
	list := func(token string, query dto.IssueFilterParams) []string {
		t.Helper()
		result, err := svc.ListPublicIssues(context.Background(), token, query)
		require.NoError(t, err)
		titles := make([]string, len(result.Data))
		for i, issue := range result.Data {
			titles[i] = issue.Title
		}
		sort.Strings(titles)
		require.Equal(t, len(titles), result.TotalCount, "total_count must match the visible issues")
		return titles
	}
	const alphaTitle, betaTitle, gammaTitle = "Alpha roadmap", "Beta 100% ready", "Gamma under_score"
	all := []string{alphaTitle, betaTitle, gammaTitle}

	t.Run("search looks only at what the share shows", func(t *testing.T) {
		hidden := teamLink(`{}`, false)
		require.Equal(t, all, list(hidden, dto.IssueFilterParams{}))
		require.Empty(t, list(hidden, dto.IssueFilterParams{Search: "hiddenneedle"}), "description is hidden on this link")
		require.Empty(t, list(hidden, dto.IssueFilterParams{Search: "zebramail"}), "emails are never shown")
		require.Equal(t, []string{alphaTitle}, list(hidden, dto.IssueFilterParams{Search: "alpha"}))
		require.Equal(t, []string{betaTitle}, list(hidden, dto.IssueFilterParams{Search: "shr-2"}))

		shown := teamLink(`{}`, true)
		require.Equal(t, []string{alphaTitle}, list(shown, dto.IssueFilterParams{Search: "hiddenneedle"}), "a shown description is searchable")
		require.Empty(t, list(shown, dto.IssueFilterParams{Search: "zebramail"}))
	})

	t.Run("search wildcards are literal", func(t *testing.T) {
		token := teamLink(`{}`, false)
		require.Equal(t, []string{betaTitle}, list(token, dto.IssueFilterParams{Search: "%"}))
		require.Equal(t, []string{gammaTitle}, list(token, dto.IssueFilterParams{Search: "_"}))
		require.Empty(t, list(token, dto.IssueFilterParams{Search: `\`}))
		require.Empty(t, list(token, dto.IssueFilterParams{Search: "A%a"}))
	})

	t.Run("public parameters only narrow stored filters", func(t *testing.T) {
		done := teamLink(`{"status":"done"}`, false)
		require.Equal(t, []string{alphaTitle, gammaTitle}, list(done, dto.IssueFilterParams{}))
		require.Empty(t, list(done, dto.IssueFilterParams{Status: "backlog"}), "a stored status cannot be replaced")
		require.Equal(t, []string{alphaTitle, gammaTitle}, list(done, dto.IssueFilterParams{Status: "done"}))
		require.Equal(t, []string{alphaTitle, gammaTitle}, list(done, dto.IssueFilterParams{Status: "done,backlog"}))
		require.Equal(t, []string{gammaTitle}, list(done, dto.IssueFilterParams{Priority: "3"}), "a key without a stored value narrows freely")

		byID := teamLink(`{"status":"`+doneStatus.String()+`"}`, false)
		require.Empty(t, list(byID, dto.IssueFilterParams{Status: backlogStatus.String()}))
		require.Equal(t, []string{alphaTitle, gammaTitle}, list(byID, dto.IssueFilterParams{Status: doneStatus.String()}))

		priority := teamLink(`{"priority":"1,2"}`, false)
		require.Equal(t, []string{alphaTitle, betaTitle}, list(priority, dto.IssueFilterParams{}))
		require.Equal(t, []string{betaTitle}, list(priority, dto.IssueFilterParams{Priority: "1"}))
		require.Equal(t, []string{betaTitle}, list(priority, dto.IssueFilterParams{Priority: "1,3"}))
		require.Empty(t, list(priority, dto.IssueFilterParams{Priority: "3"}))

		due := teamLink(`{"due_before":"2026-02-01"}`, false)
		require.Equal(t, []string{alphaTitle}, list(due, dto.IssueFilterParams{}))
		require.Equal(t, []string{alphaTitle}, list(due, dto.IssueFilterParams{DueBefore: "2026-12-31"}), "a later bound cannot widen the stored one")
		require.Empty(t, list(due, dto.IssueFilterParams{DueBefore: "2026-01-05"}))
		after := teamLink(`{"due_after":"2026-02-01"}`, false)
		require.Equal(t, []string{betaTitle}, list(after, dto.IssueFilterParams{DueAfter: "2025-01-01"}))
	})

	t.Run("stored view filters are all enforced", func(t *testing.T) {
		creator := viewLink(`{"view_scope":"workspace","group_by":"status","creator":"`+owner.String()+`"}`, `{}`)
		require.Equal(t, []string{alphaTitle}, list(creator, dto.IssueFilterParams{}))
		require.Empty(t, list(creator, dto.IssueFilterParams{Search: "beta"}))

		statusType := viewLink(`{"status_type":"completed"}`, `{}`)
		require.Equal(t, []string{alphaTitle, gammaTitle}, list(statusType, dto.IssueFilterParams{}))
		require.Empty(t, list(statusType, dto.IssueFilterParams{Status: backlogStatus.String()}))

		// A stored search uses the public columns too, and a public search
		// narrows it further instead of replacing it.
		search := viewLink(`{"search":"a"}`, `{}`)
		require.Equal(t, all, list(search, dto.IssueFilterParams{}))
		require.Equal(t, []string{gammaTitle}, list(search, dto.IssueFilterParams{Search: "gamma"}))
		require.Empty(t, list(viewLink(`{"search":"zebramail"}`, `{}`), dto.IssueFilterParams{}))

		// The link's own filters still apply to keys the view leaves open.
		both := viewLink(`{"creator":"`+other.String()+`"}`, `{"status":"done"}`)
		require.Equal(t, []string{gammaTitle}, list(both, dto.IssueFilterParams{}))
	})

	t.Run("filters that cannot be enforced return nothing", func(t *testing.T) {
		for name, token := range map[string]string{
			"non-flat view filters":   viewLink(`{"status":["done"]}`, `{}`),
			"view filters not a map":  viewLink(`["done"]`, `{}`),
			"unknown view key":        viewLink(`{"reviewer":"`+owner.String()+`"}`, `{}`),
			"non-flat link filters":   teamLink(`{"status":{"in":["done"]}}`, false),
			"unknown link key":        teamLink(`{"reviewer":"x"}`, false),
			"invalid stored id":       teamLink(`{"assignee":"me"}`, false),
			"invalid stored priority": teamLink(`{"priority":"high"}`, false),
			"invalid stored date":     teamLink(`{"due_before":"soon"}`, false),
			"unknown sub-issue mode":  teamLink(`{"sub_issues":"sometimes"}`, false),
		} {
			require.Empty(t, list(token, dto.IssueFilterParams{}), name)
			require.Empty(t, list(token, dto.IssueFilterParams{Search: "alpha"}), name)
		}
		// Invalid public values cannot be combined with anything either.
		open := teamLink(`{}`, false)
		require.Empty(t, list(open, dto.IssueFilterParams{Priority: "high"}))
		require.Empty(t, list(open, dto.IssueFilterParams{LabelID: "not-a-label"}))
		require.Empty(t, list(open, dto.IssueFilterParams{DueBefore: "soon"}))
	})

	t.Run("member search is unchanged", func(t *testing.T) {
		titles := func(search string) []string {
			t.Helper()
			params := dto.IssueFilterParams{Search: search}
			params.Defaults()
			found, total, err := issues.List(ownerCtx, ws, params)
			require.NoError(t, err)
			require.Equal(t, len(found), total)
			result := make([]string, len(found))
			for i, issue := range found {
				result[i] = issue.Title
			}
			sort.Strings(result)
			return result
		}
		require.Equal(t, []string{alphaTitle}, titles("hiddenneedle"))
		require.Equal(t, all, titles("zebramail"))
		require.Equal(t, all, titles("%"))
	})
}
