package service

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/realtime"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

func TestIssueTemplateRecurrence(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL is not set")
	}
	admin, err := sqlx.Connect("pgx", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { admin.Close() })
	database := "recurrence_" + uuid.New().String()
	_, err = admin.Exec(`CREATE DATABASE "` + database + `"`)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = admin.Exec(`DROP DATABASE "` + database + `"`) })
	parsed, err := url.Parse(dsn)
	require.NoError(t, err)
	parsed.Path = "/" + database
	migrator, err := migrate.New("file://../../migrations", parsed.String())
	require.NoError(t, err)
	t.Cleanup(func() { migrator.Close() })
	require.NoError(t, migrator.Migrate(199))
	db, err := sqlx.Connect("pgx", parsed.String())
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	owner, assignee, ws, team, status, label := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	exec := func(query string, params ...any) {
		t.Helper()
		_, err := db.ExecContext(ctx, query, params...)
		require.NoError(t, err)
	}
	for _, user := range []uuid.UUID{owner, assignee} {
		exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Recurrence','hash')`, user, user.String()+"@test.invalid")
	}
	exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Recurrence',$2,$3)`, ws, ws.String(), owner)
	exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner'),($1,$3,'member')`, ws, owner, assignee)
	exec(`INSERT INTO teams(id,workspace_id,name,key,is_private) VALUES($1,$2,'Recurrence','REC',true)`, team, ws)
	exec(`INSERT INTO team_members(team_id,user_id) VALUES($1,$2),($1,$3)`, team, owner, assignee)
	exec(`INSERT INTO team_statuses(id,team_id,name,slug,category) VALUES($1,$2,'Todo','todo','unstarted')`, status, team)
	exec(`INSERT INTO labels(id,workspace_id,name,color) VALUES($1,$2,'Periodic','#000000')`, label, ws)
	legacy, invalidLegacy := uuid.New(), uuid.New()
	rule := json.RawMessage(`{"cron":"0 9 * * *","timezone":"America/New_York"}`)
	exec(`INSERT INTO issue_templates(id,workspace_id,team_id,title,created_by,recurrence_rule) VALUES($1,$2,$3,'Legacy schedule',$4,$5)`, legacy, ws, team, owner, rule)
	exec(`INSERT INTO issue_templates(id,workspace_id,team_id,title,created_by,recurrence_rule) VALUES($1,$2,$3,'Invalid legacy schedule',$4,'{}')`, invalidLegacy, ws, team, owner)
	require.NoError(t, migrator.Up())

	issueRepo := repository.NewIssueRepository(db)
	templateRepo := repository.NewIssueTemplateRepository(db)
	templateSvc := NewIssueTemplateService(templateRepo)
	issueSvc := NewIssueService(issueRepo, repository.NewTeamRepository(db), repository.NewTeamStatusRepository(db), nil,
		realtime.NewHub(), NewNotificationService(repository.NewNotificationRepository(db)))
	worker := NewIssueTemplateRecurrenceWorker(templateRepo, issueRepo, issueSvc)
	otherDB, err := sqlx.Connect("pgx", parsed.String())
	require.NoError(t, err)
	t.Cleanup(func() { otherDB.Close() })
	other := NewIssueTemplateRecurrenceWorker(repository.NewIssueTemplateRepository(otherDB), repository.NewIssueRepository(otherDB),
		NewIssueService(repository.NewIssueRepository(otherDB), repository.NewTeamRepository(otherDB), repository.NewTeamStatusRepository(otherDB), nil,
			realtime.NewHub(), NewNotificationService(repository.NewNotificationRepository(otherDB))))
	actor := domain.WithAccess(ctx, domain.Access{UserID: owner, WorkspaceID: ws})
	create := func(title string, labels ...string) *domain.IssueTemplate {
		t.Helper()
		priority := 2
		tmpl, err := templateSvc.Create(actor, ws, owner, dto.CreateIssueTemplateRequest{
			Title: title, DefaultIssueTitle: strPtr("Check release images"), Description: strPtr("<p>Scan the release.</p>"),
			TeamID: strPtr(team.String()), Status: strPtr("todo"), Priority: &priority, AssigneeID: strPtr(assignee.String()),
			LabelIDs: labels, RecurrenceRule: rule,
		})
		require.NoError(t, err)
		require.NotNil(t, tmpl.NextRunAt)
		require.True(t, tmpl.NextRunAt.After(time.Now()))
		return tmpl
	}
	issueCount := func() int {
		t.Helper()
		var count int
		require.NoError(t, db.GetContext(ctx, &count, `SELECT count(*) FROM issues WHERE workspace_id=$1`, ws))
		return count
	}
	get := func(id uuid.UUID) *domain.IssueTemplate {
		t.Helper()
		tmpl, err := templateRepo.GetByID(actor, id)
		require.NoError(t, err)
		require.NotNil(t, tmpl)
		return tmpl
	}

	tmpl := create("Daily scan", label.String())
	require.NoError(t, worker.runOnce(ctx))
	require.Zero(t, issueCount(), "future templates wait and legacy schedules are initialized")
	require.NotNil(t, get(legacy).NextRunAt)
	require.Nil(t, get(invalidLegacy).NextRunAt)
	require.NotNil(t, get(invalidLegacy).LastError)
	due := time.Now().UTC().Truncate(24 * time.Hour).Add(-22 * time.Hour)
	exec(`UPDATE issue_templates SET next_run_at=$2 WHERE id=$1`, tmpl.ID, due)
	listed, err := templateRepo.ListDueForRecurrence(ctx)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	start := make(chan struct{})
	done := make(chan error, 2)
	for _, runner := range []*IssueTemplateRecurrenceWorker{worker, other} {
		go func() { <-start; done <- runner.runTemplate(ctx, listed[0]) }()
	}
	close(start)
	for range 2 {
		require.NoError(t, <-done)
	}
	require.Equal(t, 1, issueCount())
	var issue domain.Issue
	require.NoError(t, db.GetContext(ctx, &issue, `SELECT * FROM issues WHERE workspace_id=$1`, ws))
	loc, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	require.Equal(t, "Check release images - "+due.In(loc).Format(time.DateOnly), issue.Title)
	require.Equal(t, "<p>Scan the release.</p>", *issue.Description)
	require.Equal(t, status, *issue.StatusID)
	require.Equal(t, domain.IssuePriority(2), issue.Priority)
	require.Equal(t, assignee, *issue.AssigneeID)
	assignees, err := issueRepo.GetAssignees(actor, issue.ID)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{assignee}, assignees)
	labels, err := issueRepo.GetLabels(actor, issue.ID)
	require.NoError(t, err)
	require.Len(t, labels, 1)
	require.Equal(t, label, labels[0].ID)
	var noticeCount int
	require.NoError(t, db.GetContext(ctx, &noticeCount, `SELECT count(*) FROM notifications WHERE issue_id=$1 AND user_id=$2 AND type='assigned'`, issue.ID, assignee))
	require.Equal(t, 1, noticeCount)
	current := get(tmpl.ID)
	require.True(t, current.NextRunAt.After(time.Now()))
	require.NotNil(t, current.LastRunAt)
	require.Nil(t, current.LastError)

	// Reoffering a completed slot under a fresh lease must still produce one issue.
	exec(`UPDATE issue_templates SET next_run_at=$2,recurrence_lease_until=now()-interval '1 second' WHERE id=$1`, tmpl.ID, due)
	require.NoError(t, worker.runOnce(ctx))
	require.Equal(t, 1, issueCount())
	require.True(t, get(tmpl.ID).NextRunAt.After(time.Now()))

	broken := create("Missing label", uuid.NewString())
	exec(`UPDATE issue_templates SET next_run_at=$2 WHERE id=$1`, broken.ID, due)
	require.NoError(t, worker.runOnce(ctx))
	require.Equal(t, 1, issueCount(), "label failures roll back the issue")
	failed := get(broken.ID)
	require.NotNil(t, failed.LastError)
	require.Contains(t, *failed.LastError, "label")
	require.NotNil(t, failed.LastRunAt)
	require.True(t, failed.NextRunAt.After(time.Now()), "a failed slot is advanced")
	require.NoError(t, worker.runOnce(ctx))
	require.Equal(t, failed.LastRunAt, get(broken.ID).LastRunAt, "the failed slot is not repeatedly attempted")
	updated, err := templateSvc.Update(actor, broken.ID, dto.UpdateIssueTemplateRequest{Title: strPtr("New title")})
	require.NoError(t, err)
	require.Equal(t, failed.NextRunAt, updated.NextRunAt)
	require.Equal(t, failed.LastError, updated.LastError)
	active := false
	updated, err = templateSvc.Update(actor, broken.ID, dto.UpdateIssueTemplateRequest{IsActive: &active})
	require.NoError(t, err)
	require.Nil(t, updated.NextRunAt)
	active = true
	updated, err = templateSvc.Update(actor, broken.ID, dto.UpdateIssueTemplateRequest{IsActive: &active})
	require.NoError(t, err)
	require.True(t, updated.NextRunAt.After(time.Now()))
	updated, err = templateSvc.Update(actor, broken.ID, dto.UpdateIssueTemplateRequest{RecurrenceRule: json.RawMessage(`null`)})
	require.NoError(t, err)
	require.Nil(t, updated.NextRunAt)
	require.Nil(t, updated.LastError)

	revoked := create("Revoked creator")
	exec(`UPDATE issue_templates SET created_by=$2, next_run_at=$3 WHERE id=$1`, revoked.ID, assignee, due)
	exec(`DELETE FROM team_members WHERE team_id=$1 AND user_id=$2`, team, assignee)
	require.NoError(t, worker.runOnce(ctx))
	require.Equal(t, 1, issueCount(), "a template does not grant its creator access to a private team")
	require.Contains(t, *get(revoked.ID).LastError, "creator can access")
}
