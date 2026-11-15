package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

type restoringWikiRepo struct {
	repository.WikiRepo
	since time.Time
	err   error
	page  *domain.WikiPage
}

func (r *restoringWikiRepo) Restore(_ context.Context, _ uuid.UUID, _ domain.WikiScope, _ uuid.UUID, since time.Time) (*domain.WikiPage, error) {
	r.since = since
	return r.page, r.err
}

func TestWikiServiceRestore(t *testing.T) {
	ctx, ws := context.Background(), uuid.New()
	team := domain.TeamWiki(uuid.New())
	page := &domain.WikiPage{ID: uuid.New(), WorkspaceID: ws, TeamID: &team.ID, Title: "Back", Version: 3}
	repo := &restoringWikiRepo{page: page}
	events := &recordedWikiEvents{}
	svc := NewWikiService(repo, events)
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	svc.SetDeleteGracePeriod(48 * time.Hour)

	restored, err := svc.Restore(ctx, ws, team, page.ID)
	require.NoError(t, err)
	require.Equal(t, page.ID, restored.ID)
	require.Equal(t, now.Add(-48*time.Hour), repo.since, "only pages deleted within the grace period come back")
	require.Len(t, events.events, 1)
	require.Equal(t, "wiki.created", events.events[0].Type, "open wikis show the page again")

	repo.err = repository.ErrWikiRestoreExpired
	_, err = svc.Restore(ctx, ws, team, page.ID)
	require.ErrorIs(t, err, ErrWikiRestoreExpired)
	repo.err = repository.ErrWikiPageNotFound
	_, err = svc.Restore(ctx, ws, team, page.ID)
	require.ErrorIs(t, err, ErrWikiNotFound)
}
