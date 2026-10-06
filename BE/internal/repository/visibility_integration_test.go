package repository

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/stretchr/testify/require"
)

// Exercises real SQL, including visibility before count and LIMIT. The ordinary
// CI database job supplies DATABASE_URL and applies all migrations first.
func TestPrivateTeamVisibilityPostgres(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	workspace, otherWorkspace := uuid.New(), uuid.New()
	owner, admin, member, guest, outsider := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{owner, admin, member, guest, outsider} {
		_, err := db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Visibility test','hash')`, id, id.String()+"@example.test")
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, id) })
	}
	for _, id := range []uuid.UUID{workspace, otherWorkspace} {
		_, err := db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Visibility test',$2,$3)`, id, id.String(), owner)
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, id) })
	}
	for id, role := range map[uuid.UUID]string{owner: "owner", admin: "admin", member: "member", guest: "guest"} {
		_, err := db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,$3)`, workspace, id, role)
		require.NoError(t, err)
	}
	public, privateA, privateB, otherTeam := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for i, id := range []uuid.UUID{public, privateA, privateB, otherTeam} {
		ws := workspace
		if id == otherTeam {
			ws = otherWorkspace
		}
		_, err := db.Exec(`INSERT INTO teams(id,workspace_id,name,key,is_private) VALUES($1,$2,$3,$3,$4)`, id, ws, []string{"PUB", "PRA", "PRB", "OTHER"}[i], id != public)
		require.NoError(t, err)
	}
	for _, id := range []uuid.UUID{member, guest, outsider} {
		_, err := db.Exec(`INSERT INTO team_members(team_id,user_id) VALUES($1,$2)`, privateA, id)
		require.NoError(t, err)
	}
	// Make outsider a member of a different workspace: neither that nor the stale
	// team row above grants access to this workspace.
	_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner')`, otherWorkspace, outsider)
	require.NoError(t, err)
	issueIDs := map[uuid.UUID]uuid.UUID{}
	for i, team := range []uuid.UUID{public, privateA, privateB} {
		id := uuid.New()
		issueIDs[team] = id
		status := uuid.New()
		_, err = db.Exec(`INSERT INTO team_statuses(id,team_id,name,slug,category,position) VALUES($1,$2,'Todo','todo','unstarted',0)`, status, team)
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,creator_id,sort_order,status_id) VALUES($1,$2,$3,1,$4,$5,$6,$7,$8)`, id, workspace, team, []string{"PUB-1", "PRA-1", "PRB-1"}[i], []string{"Z public", "A private", "B private"}[i], owner, []int{2, 0, 1}[i], status)
		require.NoError(t, err)
	}
	teamRepo, issueRepo := NewTeamRepository(db), NewIssueRepository(db)
	for _, tc := range []struct {
		name string
		user uuid.UUID
		want []uuid.UUID
	}{
		{"owner without team membership", owner, []uuid.UUID{public, privateA, privateB}},
		{"admin without team membership", admin, []uuid.UUID{public, privateA, privateB}},
		{"member", member, []uuid.UUID{public, privateA}},
		{"guest", guest, []uuid.UUID{public, privateA}},
		{"outsider with stale team membership", outsider, nil},
		{"missing authenticated user", uuid.Nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := domain.WithAccess(ctx, domain.Access{UserID: tc.user, WorkspaceID: workspace})
			teams, err := teamRepo.ListByWorkspace(request, workspace)
			require.NoError(t, err)
			ids := make([]uuid.UUID, 0, len(teams))
			for _, team := range teams {
				ids = append(ids, team.ID)
			}
			require.ElementsMatch(t, tc.want, ids)
			for _, team := range []uuid.UUID{public, privateA, privateB} {
				visible := false
				for _, wanted := range tc.want {
					visible = visible || team == wanted
				}
				found, err := teamRepo.GetByID(request, team)
				require.NoError(t, err)
				require.Equal(t, visible, found != nil)
				issue, err := issueRepo.GetByID(request, issueIDs[team])
				require.NoError(t, err)
				require.Equal(t, visible, issue != nil)
			}
			overview, err := NewAnalyticsRepository(db).Overview(request, workspace.String(), "")
			require.NoError(t, err)
			require.Equal(t, len(tc.want), overview.TotalIssues)
			distribution, err := NewAnalyticsRepository(db).Distribution(request, workspace.String(), "")
			require.NoError(t, err)
			require.Len(t, distribution.ByStatus, len(tc.want), "hidden status names must not appear even with zero counts")
			insights, err := NewAnalyticsRepository(db).Insights(request, workspace.String(), &dto.AnalyticsInsightsParams{Measure: "issue_count", Slice: "team", Segment: "none"})
			require.NoError(t, err)
			require.Equal(t, len(tc.want), insights.TotalCount)
			found, err := teamRepo.GetByID(request, otherTeam)
			require.NoError(t, err)
			require.Nil(t, found)
			issues, total, err := issueRepo.List(request, workspace, dto.IssueFilterParams{PaginationParams: dto.PaginationParams{Page: 1, PerPage: 1}, Sort: "sort_order", Order: "asc"})
			require.NoError(t, err)
			require.Equal(t, len(tc.want), total)
			if len(tc.want) > 0 {
				require.Len(t, issues, 1)
			} else {
				require.Empty(t, issues)
			}
		})
	}

	t.Run("nonmember mutations leave private data unchanged", func(t *testing.T) {
		request := domain.WithAccess(ctx, domain.Access{UserID: member, WorkspaceID: workspace})
		var original domain.Issue
		require.NoError(t, db.Get(&original, `SELECT * FROM issues WHERE id=$1`, issueIDs[privateB]))
		changed := original
		changed.Title = "unauthorized change"
		require.ErrorIs(t, issueRepo.Update(request, &changed), sql.ErrNoRows)
		require.NoError(t, issueRepo.Delete(request, original.ID))
		priority := 4
		count, err := issueRepo.BulkUpdate(request, workspace, []uuid.UUID{original.ID}, nil, &priority, nil, nil, nil, false)
		require.NoError(t, err)
		require.Zero(t, count)
		count, err = issueRepo.BulkDelete(request, workspace, []uuid.UUID{original.ID})
		require.NoError(t, err)
		require.Zero(t, count)
		require.ErrorIs(t, issueRepo.SetLabels(request, original.ID, nil), sql.ErrNoRows)
		require.ErrorIs(t, issueRepo.SetAssignees(request, original.ID, []uuid.UUID{member}), sql.ErrNoRows)
		cycle := &domain.Cycle{ID: uuid.New(), TeamID: privateB, Name: "injected", Number: 1, Status: domain.CycleStatusUpcoming}
		require.ErrorIs(t, NewCycleRepository(db).Create(request, cycle), sql.ErrNoRows)
		project := &domain.Project{ID: uuid.New(), WorkspaceID: workspace, TeamID: &privateB, Name: "injected", Status: domain.ProjectStatusPlanned}
		require.ErrorIs(t, NewProjectRepository(db).Create(request, project), sql.ErrNoRows)
		comment := &domain.Comment{ID: uuid.New(), IssueID: original.ID, UserID: member, Body: "injected"}
		require.ErrorIs(t, NewCommentRepository(db).Create(request, comment), sql.ErrNoRows)
		tx, err := db.BeginTxx(request, nil)
		require.NoError(t, err)
		changed.ID = uuid.New()
		changed.Number = 2
		changed.Identifier = "PRB-2"
		require.ErrorIs(t, issueRepo.Create(request, tx, &changed), sql.ErrNoRows)
		require.NoError(t, tx.Rollback())
		var stored domain.Issue
		require.NoError(t, db.Get(&stored, `SELECT * FROM issues WHERE id=$1`, original.ID))
		require.Equal(t, original.Title, stored.Title)
		require.Equal(t, original.Priority, stored.Priority)
		require.Equal(t, original.AssigneeID, stored.AssigneeID)
	})
	t.Run("authorized member can create private team content", func(t *testing.T) {
		request := domain.WithAccess(ctx, domain.Access{UserID: member, WorkspaceID: workspace})
		cycle := &domain.Cycle{ID: uuid.New(), TeamID: privateA, Name: "allowed", Number: 1, Status: domain.CycleStatusUpcoming}
		require.NoError(t, NewCycleRepository(db).Create(request, cycle))
		project := &domain.Project{ID: uuid.New(), WorkspaceID: workspace, TeamID: &privateA, Name: "allowed", Status: domain.ProjectStatusPlanned}
		require.NoError(t, NewProjectRepository(db).Create(request, project))
		comment := &domain.Comment{ID: uuid.New(), IssueID: issueIDs[privateA], UserID: member, Body: "allowed"}
		require.NoError(t, NewCommentRepository(db).Create(request, comment))
	})
	t.Run("workspace removal cannot revive private grants on reinvite", func(t *testing.T) {
		require.NoError(t, NewWorkspaceRepository(db).RemoveMember(ctx, workspace, guest))
		_, err := db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'guest')`, workspace, guest)
		require.NoError(t, err)
		present, safe, err := NewWorkspaceRepository(db).RealtimeAccess(ctx, workspace, guest)
		require.NoError(t, err)
		require.True(t, present)
		require.True(t, safe)
		request := domain.WithAccess(ctx, domain.Access{UserID: guest, WorkspaceID: workspace})
		found, err := teamRepo.GetByID(request, privateA)
		require.NoError(t, err)
		require.Nil(t, found)
	})

	t.Run("private relationships cannot cross team boundaries", func(t *testing.T) {
		request := domain.WithAccess(ctx, domain.Access{UserID: owner, WorkspaceID: workspace})
		var source domain.Issue
		require.NoError(t, db.Get(&source, `SELECT * FROM issues WHERE id=$1`, issueIDs[privateA]))
		projects := []domain.Project{
			{ID: uuid.New(), WorkspaceID: workspace, Name: "teamless", Status: domain.ProjectStatusPlanned},
			{ID: uuid.New(), WorkspaceID: workspace, TeamID: &public, Name: "public", Status: domain.ProjectStatusPlanned},
			{ID: uuid.New(), WorkspaceID: workspace, TeamID: &privateB, Name: "other private", Status: domain.ProjectStatusPlanned},
			{ID: uuid.New(), WorkspaceID: workspace, TeamID: &privateA, Name: "same private", Status: domain.ProjectStatusPlanned},
		}
		for i := range projects {
			project := &projects[i]
			require.NoError(t, NewProjectRepository(db).Create(request, project))
			t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM projects WHERE id=$1`, project.ID) })
			candidate := source
			candidate.ProjectID = &project.ID
			if i == 3 {
				require.NoError(t, issueRepo.Update(request, &candidate))
			} else {
				require.ErrorIs(t, issueRepo.Update(request, &candidate), sql.ErrNoRows)
			}
		}
		require.NoError(t, issueRepo.Update(request, &source))
		// Project moves must not implicitly publish its description or its issues.
		project := projects[3]
		project.TeamID = &public
		require.ErrorIs(t, NewProjectRepository(db).Update(request, &project), sql.ErrNoRows)
		project.TeamID = &privateB
		require.ErrorIs(t, NewProjectRepository(db).Update(request, &project), sql.ErrNoRows)
		for _, target := range []uuid.UUID{issueIDs[public], issueIDs[privateB]} {
			candidate := source
			candidate.ParentID = &target
			require.ErrorIs(t, issueRepo.Update(request, &candidate), sql.ErrNoRows)
			relation := &domain.IssueRelation{ID: uuid.New(), IssueID: source.ID, RelatedIssueID: target, Type: domain.IssueRelationType("related")}
			require.ErrorIs(t, NewIssueRelationRepository(db).Create(request, relation), sql.ErrNoRows)
			relation.IssueID, relation.RelatedIssueID = target, source.ID
			require.ErrorIs(t, NewIssueRelationRepository(db).Create(request, relation), sql.ErrNoRows)
		}
		var publicIssue domain.Issue
		require.NoError(t, db.Get(&publicIssue, `SELECT * FROM issues WHERE id=$1`, issueIDs[public]))
		publicIssue.ProjectID = &projects[3].ID
		require.ErrorIs(t, issueRepo.Update(request, &publicIssue), sql.ErrNoRows)
		sibling := source
		sibling.ID, sibling.Number, sibling.Identifier = uuid.New(), 2, "PRA-2"
		tx, err := db.BeginTxx(request, nil)
		require.NoError(t, err)
		require.NoError(t, issueRepo.Create(request, tx, &sibling))
		require.NoError(t, tx.Commit())
		t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM issues WHERE id=$1`, sibling.ID) })
		sibling.ParentID = &source.ID
		require.NoError(t, issueRepo.Update(request, &sibling))
		relation := &domain.IssueRelation{ID: uuid.New(), IssueID: source.ID, RelatedIssueID: sibling.ID, Type: domain.IssueRelationType("related")}
		require.NoError(t, NewIssueRelationRepository(db).Create(request, relation))
	})

	t.Run("notification provenance follows current recipient access", func(t *testing.T) {
		repo := NewNotificationRepository(db)
		actor := domain.WithAccess(ctx, domain.Access{UserID: owner, WorkspaceID: workspace})
		recipient := domain.WithAccess(ctx, domain.Access{UserID: member})
		privateIssue := issueIDs[privateA]
		notice := &domain.Notification{ID: uuid.New(), UserID: member, WorkspaceID: workspace, IssueID: &privateIssue, Type: "assigned", Title: "private title"}
		require.NoError(t, repo.Create(actor, notice))
		require.Equal(t, []string{privateA.String()}, []string(notice.SourceTeamIDs))
		publicIssue := issueIDs[public]
		publicNotice := &domain.Notification{ID: uuid.New(), UserID: member, WorkspaceID: workspace, IssueID: &publicIssue, Type: "assigned", Title: "public title"}
		require.NoError(t, repo.Create(actor, publicNotice))
		denied := *notice
		denied.ID, denied.UserID = uuid.New(), guest
		require.ErrorIs(t, repo.Create(actor, &denied), sql.ErrNoRows, "sender's admin access must not grant access to a recipient")
		detached := &domain.Notification{ID: uuid.New(), UserID: member, WorkspaceID: workspace, SourceTeamIDs: []string{privateA.String()}, Type: "issue_deleted", Title: "deleted private title"}
		require.NoError(t, repo.Create(actor, detached))
		summary := *detached
		summary.ID = uuid.New()
		summary.SourceTeamIDs = []string{privateA.String(), privateB.String()}
		require.ErrorIs(t, repo.Create(actor, &summary), sql.ErrNoRows, "mixed-team summary requires visibility of every source")
		items, err := repo.ListByUser(recipient, member, 10, 0)
		require.NoError(t, err)
		require.Len(t, items, 3)
		count, err := repo.UnreadCount(recipient, member)
		require.NoError(t, err)
		require.Equal(t, 3, count)
		_, err = repo.GetByID(actor, notice.ID)
		require.ErrorIs(t, err, sql.ErrNoRows, "even an admin cannot read another user's inbox")
		_, err = db.Exec(`DELETE FROM team_members WHERE team_id=$1 AND user_id=$2`, privateA, member)
		require.NoError(t, err)
		defer func() {
			_, err := db.Exec(`INSERT INTO team_members(team_id,user_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, privateA, member)
			require.NoError(t, err)
		}()
		items, err = repo.ListByUser(recipient, member, 1, 0)
		require.NoError(t, err)
		require.Len(t, items, 1)
		require.Equal(t, publicNotice.ID, items[0].ID, "inaccessible notices must not consume the first page")
		count, err = repo.UnreadCount(recipient, member)
		require.NoError(t, err)
		require.Equal(t, 1, count)
		_, err = repo.GetByID(recipient, notice.ID)
		require.ErrorIs(t, err, sql.ErrNoRows)
		require.ErrorIs(t, repo.CreateOrRefresh(actor, notice, time.Hour), sql.ErrNoRows)
		now := time.Now()
		notice.ReadAt = &now
		require.NoError(t, repo.Update(recipient, notice))
		var read sql.NullTime
		require.NoError(t, db.Get(&read, `SELECT read_at FROM notifications WHERE id=$1`, notice.ID))
		require.False(t, read.Valid)
		_, err = db.Exec(`UPDATE notifications SET snoozed_until=NOW()+INTERVAL '1 hour' WHERE id=$1`, notice.ID)
		require.NoError(t, err)
		snoozed, err := repo.ListSnoozed(recipient, member)
		require.NoError(t, err)
		require.Empty(t, snoozed)
		_, err = db.Exec(`UPDATE notifications SET archived_at=NOW() WHERE id=$1`, detached.ID)
		require.NoError(t, err)
		archived, err := repo.ListArchived(recipient, member, 10)
		require.NoError(t, err)
		require.Empty(t, archived)
	})
	t.Run("historical references redact inaccessible private IDs", func(t *testing.T) {
		project := &domain.Project{ID: uuid.New(), WorkspaceID: workspace, TeamID: &privateB, Name: "Historical private project", Status: domain.ProjectStatusPlanned}
		adminCtx := domain.WithAccess(ctx, domain.Access{UserID: admin, WorkspaceID: workspace})
		memberCtx := domain.WithAccess(ctx, domain.Access{UserID: member, WorkspaceID: workspace})
		require.NoError(t, NewProjectRepository(db).Create(adminCtx, project))
		value := project.ID.String()
		history := NewIssueHistoryRepository(db)
		require.NoError(t, history.Create(ctx, issueIDs[public], owner, "project", &value, nil))
		items, err := history.ListByIssue(memberCtx, issueIDs[public])
		require.NoError(t, err)
		require.Len(t, items, 1)
		require.Nil(t, items[0].OldValue)
		items, err = history.ListByIssue(adminCtx, issueIDs[public])
		require.NoError(t, err)
		require.Len(t, items, 1)
		require.NotNil(t, items[0].OldValue)
		require.Equal(t, value, *items[0].OldValue)
	})
	t.Run("project status metadata and cycle helpers are scoped", func(t *testing.T) {
		adminCtx := domain.WithAccess(ctx, domain.Access{UserID: admin, WorkspaceID: workspace})
		memberCtx := domain.WithAccess(ctx, domain.Access{UserID: member, WorkspaceID: workspace})
		p := &domain.Project{ID: uuid.New(), WorkspaceID: workspace, TeamID: &privateB, Name: "Private metadata", Status: domain.ProjectStatusPlanned}
		require.NoError(t, NewProjectRepository(db).Create(adminCtx, p))
		var privateStatus, publicStatus uuid.UUID
		require.NoError(t, db.Get(&privateStatus, `SELECT status_id FROM issues WHERE id=$1`, issueIDs[privateB]))
		require.NoError(t, db.Get(&publicStatus, `SELECT status_id FROM issues WHERE id=$1`, issueIDs[public]))
		visibility := NewProjectStatusVisibilityRepository(db)
		require.NoError(t, visibility.SetVisibleStatuses(adminCtx, p.ID, []uuid.UUID{privateStatus}))
		ids, err := visibility.ListVisibleStatuses(memberCtx, p.ID)
		require.NoError(t, err)
		require.Empty(t, ids)
		ids, err = visibility.ListProjectsForStatus(memberCtx, privateStatus)
		require.NoError(t, err)
		require.Empty(t, ids)
		batch, err := visibility.ListProjectIDsByStatuses(memberCtx, []uuid.UUID{privateStatus})
		require.NoError(t, err)
		require.Empty(t, batch)
		require.ErrorIs(t, visibility.SetVisibleStatuses(memberCtx, p.ID, nil), sql.ErrNoRows)
		require.ErrorIs(t, visibility.SetVisibleStatuses(adminCtx, p.ID, []uuid.UUID{publicStatus}), sql.ErrNoRows)
		ids, err = visibility.ListVisibleStatuses(adminCtx, p.ID)
		require.NoError(t, err)
		require.Equal(t, []uuid.UUID{privateStatus}, ids)
		_, err = db.Exec(`INSERT INTO project_status_visibility(project_id,status_id) VALUES($1,$2)`, p.ID, publicStatus)
		require.ErrorContains(t, err, "crosses a private-team boundary")
		start, end := time.Now().UTC(), time.Now().UTC().Add(24*time.Hour)
		cycle := &domain.Cycle{ID: uuid.New(), TeamID: privateB, Name: "Secret cycle", Number: 7, Status: domain.CycleStatusUpcoming, StartDate: &start, EndDate: &end}
		cycles := NewCycleRepository(db)
		require.NoError(t, cycles.Create(adminCtx, cycle))
		number, err := cycles.NextNumber(memberCtx, privateB)
		require.NoError(t, err)
		require.Equal(t, 1, number)
		exists, err := cycles.ExistsByName(memberCtx, privateB, cycle.Name)
		require.NoError(t, err)
		require.False(t, exists)
		overlap, err := cycles.HasOverlap(memberCtx, privateB, start, end, nil)
		require.NoError(t, err)
		require.False(t, overlap)
		_, err = db.Exec(`UPDATE issues SET cycle_id=$1 WHERE id=$2`, cycle.ID, issueIDs[privateB])
		require.NoError(t, err)
		moved, err := cycles.CarryOverIssues(memberCtx, cycle.ID, cycle.ID)
		require.NoError(t, err)
		require.Zero(t, moved)
		var retained uuid.UUID
		require.NoError(t, db.Get(&retained, `SELECT cycle_id FROM issues WHERE id=$1`, issueIDs[privateB]))
		require.Equal(t, cycle.ID, retained)
		_, err = db.Exec(`INSERT INTO user_preferences(user_id,team_workflow_sort_overrides) VALUES($1,jsonb_build_object($2::text,'{"mode":"inherit"}'::jsonb,$3::text,'{"mode":"inherit"}'::jsonb,$4::text,'{"mode":"inherit"}'::jsonb))`, member, workspace.String()+"/"+public.String(), workspace.String()+"/"+privateA.String(), workspace.String()+"/"+privateB.String())
		require.NoError(t, err)
		prefs, err := NewUserPreferencesRepository(db).Get(memberCtx, member)
		require.NoError(t, err)
		require.Len(t, prefs.TeamWorkflowSortOverrides, 2)
		require.NotContains(t, prefs.TeamWorkflowSortOverrides, workspace.String()+"/"+privateB.String())
	})
	// No actor is public-only, never an implicit background superuser.
	items, total, err := issueRepo.List(ctx, workspace, dto.IssueFilterParams{PaginationParams: dto.PaginationParams{Page: 1, PerPage: 1}})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, items, 1)
	require.Equal(t, public, items[0].TeamID)
	request := domain.WithAccess(ctx, domain.Access{UserID: member, WorkspaceID: workspace})
	_, err = db.Exec(`DELETE FROM team_members WHERE team_id=$1 AND user_id=$2`, privateA, member)
	require.NoError(t, err)
	prefs, err := NewUserPreferencesRepository(db).Get(request, member)
	require.NoError(t, err)
	require.Len(t, prefs.TeamWorkflowSortOverrides, 1)
	// Reuse the exact request identity: membership is not cached in it.
	issue, err := issueRepo.GetByID(request, issueIDs[privateA])
	require.NoError(t, err)
	require.Nil(t, issue)
	items, total, err = issueRepo.List(request, workspace, dto.IssueFilterParams{PaginationParams: dto.PaginationParams{Page: 1, PerPage: 1}, Search: "private"})
	require.NoError(t, err)
	require.Zero(t, total)
	require.Empty(t, items)
	items, total, err = issueRepo.List(request, workspace, dto.IssueFilterParams{PaginationParams: dto.PaginationParams{Page: 1, PerPage: 1}, Sort: "sort_order", Order: "asc"})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, items, 1)
	require.Equal(t, public, items[0].TeamID, "private rows must not consume the first page")
	_, err = db.Exec(`UPDATE teams SET is_private=TRUE WHERE id=$1`, public)
	require.NoError(t, err)
	_, total, err = issueRepo.List(request, workspace, dto.IssueFilterParams{PaginationParams: dto.PaginationParams{Page: 1, PerPage: 1}})
	require.NoError(t, err)
	require.Zero(t, total, "visibility changes apply to the next query")
}
