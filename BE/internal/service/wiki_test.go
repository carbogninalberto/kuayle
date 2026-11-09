package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// fakeWikiRepo keeps pages in memory and treats only `visible` scopes as
// accessible, mirroring the repository's not-found contract.
type fakeWikiRepo struct {
	visible map[domain.WikiScope]bool
	pages   map[uuid.UUID]domain.WikiPage
	writes  int
}

func newFakeWikiRepo(visible ...domain.WikiScope) *fakeWikiRepo {
	repo := &fakeWikiRepo{visible: map[domain.WikiScope]bool{}, pages: map[uuid.UUID]domain.WikiPage{}}
	for _, scope := range visible {
		repo.visible[scope] = true
	}
	return repo
}

func (f *fakeWikiRepo) RequireScope(_ context.Context, _ uuid.UUID, scope domain.WikiScope) error {
	if !f.visible[scope] {
		return repository.ErrWikiScopeNotFound
	}
	return nil
}

func (f *fakeWikiRepo) List(ctx context.Context, ws uuid.UUID, scope domain.WikiScope) ([]domain.WikiPage, error) {
	if err := f.RequireScope(ctx, ws, scope); err != nil {
		return nil, err
	}
	pages := []domain.WikiPage{}
	for _, page := range f.pages {
		if page.Scope() == scope && page.WorkspaceID == ws {
			page.Content = ""
			pages = append(pages, page)
		}
	}
	return pages, nil
}

func (f *fakeWikiRepo) Get(_ context.Context, ws uuid.UUID, scope domain.WikiScope, id uuid.UUID) (*domain.WikiPage, error) {
	page, ok := f.pages[id]
	if !ok || !f.visible[scope] || page.Scope() != scope || page.WorkspaceID != ws {
		return nil, repository.ErrWikiPageNotFound
	}
	return &page, nil
}

func (f *fakeWikiRepo) Create(_ context.Context, page *domain.WikiPage) error {
	if !f.visible[page.Scope()] {
		return repository.ErrWikiScopeNotFound
	}
	f.writes++
	page.Version = 1
	page.UpdatedBy = page.CreatedBy
	f.pages[page.ID] = *page
	return nil
}

func (f *fakeWikiRepo) Update(ctx context.Context, ws uuid.UUID, scope domain.WikiScope, page *domain.WikiPage, expected int) error {
	stored, err := f.Get(ctx, ws, scope, page.ID)
	if err != nil {
		return err
	}
	if stored.Version != expected {
		return repository.ErrWikiVersionConflict
	}
	f.writes++
	page.Version = expected + 1
	f.pages[page.ID] = *page
	return nil
}

func (f *fakeWikiRepo) Delete(ctx context.Context, ws uuid.UUID, scope domain.WikiScope, id uuid.UUID) error {
	if _, err := f.Get(ctx, ws, scope, id); err != nil {
		return err
	}
	f.writes++
	delete(f.pages, id)
	return nil
}

func TestWikiServiceCreateAndReopenPreservesContent(t *testing.T) {
	ctx, ws, user := context.Background(), uuid.New(), uuid.New()
	team := domain.TeamWiki(uuid.New())
	svc := NewWikiService(newFakeWikiRepo(team))

	created, err := svc.Create(ctx, ws, team, user, dto.CreateWikiPageRequest{Title: "  Onboarding  ", Content: "<h2>Setup</h2><p>Run <code>make dev</code>.</p>"})
	require.NoError(t, err)
	require.Equal(t, "Onboarding", created.Title)
	require.Equal(t, team.ID, *created.TeamID)
	require.Nil(t, created.ProjectID)
	require.Equal(t, ws, created.WorkspaceID)
	require.Equal(t, user, *created.CreatedBy)

	reopened, err := svc.Get(ctx, ws, team, created.ID)
	require.NoError(t, err)
	require.Equal(t, "<h2>Setup</h2><p>Run <code>make dev</code>.</p>", reopened.Content)
}

func TestWikiServiceSanitizesContent(t *testing.T) {
	ctx, ws := context.Background(), uuid.New()
	project := domain.ProjectWiki(uuid.New())
	svc := NewWikiService(newFakeWikiRepo(project))

	page, err := svc.Create(ctx, ws, project, uuid.New(), dto.CreateWikiPageRequest{Title: "Notes", Content: `<p onclick="steal()">Hi</p><script>alert(1)</script>`})
	require.NoError(t, err)
	require.Equal(t, "<p>Hi</p>", page.Content)
	require.Equal(t, project.ID, *page.ProjectID)
	require.Nil(t, page.TeamID)

	page, err = svc.Create(ctx, ws, project, uuid.New(), dto.CreateWikiPageRequest{Title: "Markdown", Content: "## Heading"})
	require.NoError(t, err)
	require.Contains(t, page.Content, "<h2>Heading</h2>")
}

func TestWikiServiceValidatesInput(t *testing.T) {
	ctx, ws := context.Background(), uuid.New()
	team := domain.TeamWiki(uuid.New())
	repo := newFakeWikiRepo(team)
	svc := NewWikiService(repo)

	for _, req := range []dto.CreateWikiPageRequest{
		{Title: "   "},
		{Title: strings.Repeat("é", domain.WikiTitleMaxLength+1)},
		{Title: "Large", Content: "<p>" + strings.Repeat("x", domain.WikiContentMaxBytes) + "</p>"},
	} {
		_, err := svc.Create(ctx, ws, team, uuid.New(), req)
		var invalid *WikiValidationError
		require.ErrorAs(t, err, &invalid, req.Title)
	}
	_, err := svc.Create(ctx, ws, team, uuid.New(), dto.CreateWikiPageRequest{Title: strings.Repeat("é", domain.WikiTitleMaxLength)})
	require.NoError(t, err, "the limit counts characters, not bytes")
	require.Equal(t, 1, repo.writes)
}

func TestWikiServiceUpdateUsesOptimisticVersion(t *testing.T) {
	ctx, ws, author, editor := context.Background(), uuid.New(), uuid.New(), uuid.New()
	team := domain.TeamWiki(uuid.New())
	svc := NewWikiService(newFakeWikiRepo(team))
	page, err := svc.Create(ctx, ws, team, author, dto.CreateWikiPageRequest{Title: "Runbook", Content: "<p>v1</p>"})
	require.NoError(t, err)

	updated, err := svc.Update(ctx, ws, team, page.ID, editor, dto.UpdateWikiPageRequest{Content: ptr("<p>v2</p>"), Version: 1})
	require.NoError(t, err)
	require.Equal(t, 2, updated.Version)
	require.Equal(t, "Runbook", updated.Title, "absent fields are preserved")
	require.Equal(t, editor, *updated.UpdatedBy)
	require.Equal(t, author, *updated.CreatedBy)

	// A second editor still holding version 1 must not overwrite v2.
	_, err = svc.Update(ctx, ws, team, page.ID, author, dto.UpdateWikiPageRequest{Content: ptr("<p>stale</p>"), Version: 1})
	require.ErrorIs(t, err, ErrWikiConflict)
	reopened, err := svc.Get(ctx, ws, team, page.ID)
	require.NoError(t, err)
	require.Equal(t, "<p>v2</p>", reopened.Content)

	_, err = svc.Update(ctx, ws, team, page.ID, editor, dto.UpdateWikiPageRequest{Version: 2})
	var invalid *WikiValidationError
	require.ErrorAs(t, err, &invalid, "an update must change something")
	_, err = svc.Update(ctx, ws, team, page.ID, editor, dto.UpdateWikiPageRequest{Title: ptr(" "), Version: 2})
	require.ErrorAs(t, err, &invalid)
}

// Pages are only reachable through their own scope and workspace.
func TestWikiServiceKeepsScopesApart(t *testing.T) {
	ctx, ws := context.Background(), uuid.New()
	teamA, teamB := domain.TeamWiki(uuid.New()), domain.TeamWiki(uuid.New())
	project := domain.ProjectWiki(teamA.ID) // same UUID, different kind
	hidden := domain.TeamWiki(uuid.New())
	svc := NewWikiService(newFakeWikiRepo(teamA, teamB, project))

	page, err := svc.Create(ctx, ws, teamA, uuid.New(), dto.CreateWikiPageRequest{Title: "Team A only"})
	require.NoError(t, err)

	for _, scope := range []domain.WikiScope{teamB, project, hidden} {
		_, err = svc.Get(ctx, ws, scope, page.ID)
		require.ErrorIs(t, err, ErrWikiNotFound, scope.String())
		_, err = svc.Update(ctx, ws, scope, page.ID, uuid.New(), dto.UpdateWikiPageRequest{Title: ptr("x"), Version: 1})
		require.ErrorIs(t, err, ErrWikiNotFound, scope.String())
		require.ErrorIs(t, svc.Delete(ctx, ws, scope, page.ID), ErrWikiNotFound, scope.String())
	}
	_, err = svc.Get(ctx, uuid.New(), teamA, page.ID)
	require.ErrorIs(t, err, ErrWikiNotFound, "another workspace")

	pages, err := svc.List(ctx, ws, teamB)
	require.NoError(t, err)
	require.Empty(t, pages)
	_, err = svc.List(ctx, ws, hidden)
	require.ErrorIs(t, err, ErrWikiNotFound)
	_, err = svc.Create(ctx, ws, hidden, uuid.New(), dto.CreateWikiPageRequest{Title: "Leak"})
	require.ErrorIs(t, err, ErrWikiNotFound)

	require.NoError(t, svc.Delete(ctx, ws, teamA, page.ID))
	_, err = svc.Get(ctx, ws, teamA, page.ID)
	require.ErrorIs(t, err, ErrWikiNotFound)
}

func TestWikiServicePassesThroughUnexpectedErrors(t *testing.T) {
	boom := errors.New("database down")
	svc := NewWikiService(failingWikiRepo{err: boom})
	_, err := svc.List(context.Background(), uuid.New(), domain.TeamWiki(uuid.New()))
	require.ErrorIs(t, err, boom)
	require.NotErrorIs(t, err, ErrWikiNotFound)
}

type failingWikiRepo struct {
	repository.WikiRepo
	err error
}

func (f failingWikiRepo) List(context.Context, uuid.UUID, domain.WikiScope) ([]domain.WikiPage, error) {
	return nil, f.err
}
