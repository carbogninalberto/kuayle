package service

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/sanitize"
	"github.com/stretchr/testify/require"
)

type fakeIssueLookup map[string]*domain.Issue

func (f fakeIssueLookup) GetByIdentifier(_ context.Context, _ uuid.UUID, identifier string) (*domain.Issue, error) {
	return f[identifier], nil
}

// A wiki with one page the caller can see (another team's) and one they
// cannot (a private team's), plus a page of another workspace.
func referenceFixture(t *testing.T) (svc *WikiService, repo *fakeWikiRepo, ws uuid.UUID, visible, hidden, foreign domain.WikiPage) {
	t.Helper()
	ws = uuid.New()
	openTeam, privateTeam := uuid.New(), uuid.New()
	repo = newFakeWikiRepo(domain.TeamWiki(openTeam))
	visible = domain.WikiPage{ID: uuid.New(), WorkspaceID: ws, TeamID: &openTeam, Title: "Deploy guide", Version: 1}
	hidden = domain.WikiPage{ID: uuid.New(), WorkspaceID: ws, TeamID: &privateTeam, Title: "Acquisition plan", Version: 1}
	foreign = domain.WikiPage{ID: uuid.New(), WorkspaceID: uuid.New(), TeamID: &openTeam, Title: "Other workspace", Version: 1}
	for _, page := range []domain.WikiPage{visible, hidden, foreign} {
		repo.pages[page.ID] = page
	}
	return NewWikiService(repo), repo, ws, visible, hidden, foreign
}

// Every requested chip gets an answer, but only a visible page carries a
// title: invisible, foreign and unknown pages are the same bare placeholder.
func TestResolveReferencesNeverNamesInvisiblePages(t *testing.T) {
	svc, _, ws, visible, hidden, foreign := referenceFixture(t)
	unknown := uuid.New()

	resolved, err := svc.ResolveReferences(context.Background(), ws, []uuid.UUID{hidden.ID, visible.ID, foreign.ID, unknown, visible.ID})
	require.NoError(t, err)
	require.Len(t, resolved, 4, "a repeated ID is answered once")
	require.Equal(t, ResolvedWikiReference{ID: hidden.ID}, resolved[0])
	require.True(t, resolved[1].Accessible)
	require.Equal(t, "Deploy guide", resolved[1].Page.Title)
	require.Equal(t, ResolvedWikiReference{ID: foreign.ID}, resolved[2])
	require.Equal(t, ResolvedWikiReference{ID: unknown}, resolved[3])

	tooMany := make([]uuid.UUID, domain.WikiReferenceResolveLimit+1)
	_, err = svc.ResolveReferences(context.Background(), ws, tooMany)
	var invalid *WikiValidationError
	require.ErrorAs(t, err, &invalid)
}

func TestSearchReferencesBoundsQueryAndLimit(t *testing.T) {
	svc, repo, ws, visible, _, _ := referenceFixture(t)

	found, err := svc.SearchReferences(context.Background(), ws, "  plan ", 0)
	require.NoError(t, err)
	require.Empty(t, found, "the private team's page is not suggested")
	found, err = svc.SearchReferences(context.Background(), ws, "DEPLOY", 500)
	require.NoError(t, err)
	require.Len(t, found, 1)
	require.Equal(t, visible.ID, found[0].ID)
	require.Equal(t, []string{"plan|20", "DEPLOY|20"}, repo.searches)

	_, err = svc.SearchReferences(context.Background(), ws, strings.Repeat("x", domain.WikiTitleMaxLength+1), 5)
	var invalid *WikiValidationError
	require.ErrorAs(t, err, &invalid)
}

// Attaching needs an issue and a page the caller can see; either one missing
// is the same not-found, and nothing is attached.
func TestIssueReferencesRequireVisibleIssueAndPage(t *testing.T) {
	svc, repo, ws, visible, hidden, _ := referenceFixture(t)
	issue := &domain.Issue{ID: uuid.New()}
	svc.SetIssues(fakeIssueLookup{"KUA-1": issue})
	user := uuid.New()
	ctx := context.Background()

	_, err := svc.AttachIssueReference(ctx, ws, "KUA-404", visible.ID, user)
	require.ErrorIs(t, err, ErrWikiNotFound)
	_, err = svc.AttachIssueReference(ctx, ws, "KUA-1", hidden.ID, user)
	require.ErrorIs(t, err, ErrWikiNotFound)
	require.Empty(t, repo.attached[issue.ID])

	attached, err := svc.AttachIssueReference(ctx, ws, "KUA-1", visible.ID, user)
	require.NoError(t, err)
	require.Len(t, attached, 1)
	_, err = svc.AttachIssueReference(ctx, ws, "KUA-1", visible.ID, user)
	require.NoError(t, err)
	require.Len(t, repo.attached[issue.ID], 1, "attaching twice keeps one reference")

	// Someone else attached a page this caller cannot see: it is listed as
	// an inaccessible placeholder and can still be removed.
	repo.attached[issue.ID] = append(repo.attached[issue.ID], hidden.ID)
	listed, err := svc.IssueReferences(ctx, ws, "KUA-1")
	require.NoError(t, err)
	require.Len(t, listed, 2)
	require.Equal(t, ResolvedWikiReference{ID: hidden.ID}, listed[1])
	require.NoError(t, svc.DetachIssueReference(ctx, ws, "KUA-1", hidden.ID))
	require.Equal(t, []uuid.UUID{visible.ID}, repo.attached[issue.ID])
	require.ErrorIs(t, svc.DetachIssueReference(ctx, ws, "KUA-404", visible.ID), ErrWikiNotFound)
}

func TestBacklinksOfInvisiblePageAreNotFound(t *testing.T) {
	svc, _, ws, visible, hidden, _ := referenceFixture(t)
	_, err := svc.Backlinks(context.Background(), ws, hidden.Scope(), hidden.ID)
	require.ErrorIs(t, err, ErrWikiNotFound)
	links, err := svc.Backlinks(context.Background(), ws, visible.Scope(), visible.ID)
	require.NoError(t, err)
	require.NotNil(t, links)
}

// A saved page keeps a mention's page ID and drops any title sent with it.
func TestWikiContentStoresReferencesWithoutTitles(t *testing.T) {
	svc, _, ws, visible, hidden, _ := referenceFixture(t)
	page, err := svc.Create(context.Background(), ws, visible.Scope(), uuid.New(), dto.CreateWikiPageRequest{
		Title:   "Notes",
		Content: `<p>See <span data-type="mention" data-kind="wiki" data-id="` + hidden.ID.String() + `" data-label="Acquisition plan">Acquisition plan</span></p>`,
	})
	require.NoError(t, err)
	require.NotContains(t, page.Content, "Acquisition")
	require.Contains(t, page.Content, `data-id="`+hidden.ID.String()+`"`)
}

type publicReferenceRepo struct {
	wikiPublishRepo
	asked []uuid.UUID
}

func (r *publicReferenceRepo) ResolveReferences(_ context.Context, _ uuid.UUID, ids []uuid.UUID) ([]domain.WikiReference, error) {
	r.asked = ids
	return []domain.WikiReference{}, nil
}

// A public link resolves each page its published content mentions once.
func TestPublicWikiReferencesCollectMentionedPages(t *testing.T) {
	first, second := uuid.New(), uuid.New()
	repo := &publicReferenceRepo{}
	_, err := publicWikiReferences(context.Background(), repo, uuid.New(), []repository.PublicWikiPage{
		{Content: "<p>" + sanitize.WikiMentionHTML(first) + "</p>"},
		{Content: "<p>" + sanitize.WikiMentionHTML(second) + sanitize.WikiMentionHTML(first) + "</p>"},
	})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{first, second}, repo.asked)
}
