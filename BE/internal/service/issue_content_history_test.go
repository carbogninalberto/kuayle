package service

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/realtime"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// Pause one request immediately after its read, before it can persist that snapshot.
type pausedIssueReader struct {
	repository.IssueRepo
	loaded chan struct{}
	resume chan struct{}
}

func (r *pausedIssueReader) GetByIdentifier(ctx context.Context, workspaceID uuid.UUID, identifier string) (*domain.Issue, error) {
	issue, err := r.IssueRepo.GetByIdentifier(ctx, workspaceID, identifier)
	close(r.loaded)
	select {
	case <-r.resume:
		return issue, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestIssueServiceConcurrentContentPostgres(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	owner, ws, team, status, id := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'History editor','hash')`, owner, owner.String()+"@test.invalid")
	require.NoError(t, err)
	t.Cleanup(func() { _, e := db.Exec(`DELETE FROM users WHERE id=$1`, owner); require.NoError(t, e) })
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'History',$2,$3)`, ws, ws.String(), owner)
	require.NoError(t, err)
	t.Cleanup(func() { _, e := db.Exec(`DELETE FROM workspaces WHERE id=$1`, ws); require.NoError(t, e) })
	_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner')`, ws, owner)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO teams(id,workspace_id,name,key,is_private) VALUES($1,$2,'History','HIS',true)`, team, ws)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO team_statuses(id,team_id,name,slug,category) VALUES($1,$2,'Todo','todo','unstarted')`, status, team)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO issues(id,workspace_id,team_id,status_id,number,identifier_text,title,creator_id) VALUES($1,$2,$3,$4,1,'HIS-1','Original',$5)`, id, ws, team, status, owner)
	require.NoError(t, err)
	ctx := domain.WithAccess(context.Background(), domain.Access{UserID: owner, WorkspaceID: ws})
	repo, history := repository.NewIssueRepository(db), repository.NewIssueHistoryRepository(db)
	makeService := func(store repository.IssueRepo) *IssueService {
		return NewIssueService(store, repository.NewTeamRepository(db), repository.NewTeamStatusRepository(db), history, realtime.NewHub(), newTestNotifSvc())
	}
	contentHistory := func() []domain.IssueHistory {
		entries, e := history.ListByIssue(ctx, id)
		require.NoError(t, e)
		var content []domain.IssueHistory
		for _, entry := range entries {
			if entry.Field == "title" || entry.Field == "description" {
				content = append(content, entry)
			}
		}
		return content
	}
	for _, kind := range []string{"priority only", "title only", "description only", "unchanged content"} {
		t.Run(kind, func(t *testing.T) {
			before, e := repo.GetByID(ctx, id)
			require.NoError(t, e)
			count := len(contentHistory())
			request := dto.UpdateIssueRequest{}
			wantAdditional := 2
			explicitTitle, explicitDescription := "Explicit title", "<p>Explicit description</p>"
			switch kind {
			case "priority only":
				priority := int(domain.PriorityHigh)
				request.Priority = &priority
			case "title only":
				request.Title = &explicitTitle
				wantAdditional++
			case "description only":
				request.Description = &explicitDescription
				wantAdditional++
			case "unchanged content":
				request.Title = &before.Title
				request.Description = before.Description
			}
			paused := &pausedIssueReader{IssueRepo: repo, loaded: make(chan struct{}), resume: make(chan struct{})}
			timeout, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			type result struct {
				issue *domain.Issue
				err   error
			}
			done := make(chan result, 1)
			go func() {
				saved, e := makeService(paused).Update(timeout, ws, owner, "HIS-1", request)
				done <- result{saved, e}
			}()
			select {
			case <-paused.loaded:
			case <-timeout.Done():
				t.Fatal("request never loaded the issue")
			}
			title := fmt.Sprintf("Concurrent %s", kind)
			description := fmt.Sprintf("<p>Concurrent %s</p>", kind)
			_, e = makeService(repo).Update(ctx, ws, owner, "HIS-1", dto.UpdateIssueRequest{Title: &title, Description: &description})
			require.NoError(t, e)
			close(paused.resume)
			var saved result
			select {
			case saved = <-done:
			case <-timeout.Done():
				t.Fatal("paused update never finished")
			}
			require.NoError(t, saved.err)
			if kind == "title only" {
				title = explicitTitle
			}
			if kind == "description only" {
				description = explicitDescription
			}
			after, e := repo.GetByID(ctx, id)
			require.NoError(t, e)
			require.Equal(t, title, after.Title)
			require.Equal(t, &description, after.Description)
			require.Equal(t, after.Title, saved.issue.Title, "response must use persisted content, not the stale snapshot")
			require.Equal(t, after.Description, saved.issue.Description)
			require.Len(t, contentHistory(), count+wantAdditional, "unrelated or no-op edits must not create content revisions")
		})
	}
	// GitHub/status automation uses Update, which must never save stale content.
	stale, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE issues SET title='Newer than automation',description='Latest description' WHERE id=$1`, id)
	require.NoError(t, err)
	count := len(contentHistory())
	stale.Priority = domain.PriorityLow
	require.NoError(t, repo.Update(ctx, stale))
	require.Equal(t, "Newer than automation", stale.Title)
	require.Equal(t, "Latest description", *stale.Description)
	require.Len(t, contentHistory(), count)
}
