package service

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/realtime"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// fakeWikiRepo keeps pages in memory and treats only `visible` scopes as
// accessible, mirroring the repository's not-found contract.
type fakeWikiRepo struct {
	visible  map[domain.WikiScope]bool
	pages    map[uuid.UUID]domain.WikiPage
	writes   int
	registry map[domain.WikiScope]bool
	// appended records each registry append's scope, day and entry.
	appended []string
	// schedules and overviews back the overview methods.
	schedules map[domain.WikiScope]domain.WikiSettings
	overviews []domain.WikiOverview
	claims    map[uuid.UUID]time.Time
	runs      map[uuid.UUID]*string
}

func newFakeWikiRepo(visible ...domain.WikiScope) *fakeWikiRepo {
	repo := &fakeWikiRepo{visible: map[domain.WikiScope]bool{}, pages: map[uuid.UUID]domain.WikiPage{}, registry: map[domain.WikiScope]bool{}}
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

func (f *fakeWikiRepo) ListRevisions(ctx context.Context, ws uuid.UUID, scope domain.WikiScope, id uuid.UUID) ([]domain.WikiRevision, error) {
	if _, err := f.Get(ctx, ws, scope, id); err != nil {
		return nil, err
	}
	return []domain.WikiRevision{{PageID: id, Version: 2}}, nil
}

func (f *fakeWikiRepo) RegistryEnabled(ctx context.Context, ws uuid.UUID, scope domain.WikiScope) (bool, error) {
	if err := f.RequireScope(ctx, ws, scope); err != nil {
		return false, err
	}
	return f.registry[scope], nil
}

func (f *fakeWikiRepo) SetRegistryEnabled(ctx context.Context, ws uuid.UUID, scope domain.WikiScope, _ uuid.UUID, enabled bool) error {
	if err := f.RequireScope(ctx, ws, scope); err != nil {
		return err
	}
	f.registry[scope] = enabled
	return nil
}

// AppendRegistry keeps one page per scope and day, like the unique index.
func (f *fakeWikiRepo) AppendRegistry(ctx context.Context, ws uuid.UUID, scope domain.WikiScope, user uuid.UUID, day time.Time, entry string) (*domain.WikiPage, bool, error) {
	if err := f.RequireScope(ctx, ws, scope); err != nil {
		return nil, false, err
	}
	if !f.registry[scope] {
		return nil, false, repository.ErrWikiRegistryDisabled
	}
	f.appended = append(f.appended, scope.String()+" "+day.UTC().Format(time.DateOnly)+" "+entry)
	date := time.Date(day.UTC().Year(), day.UTC().Month(), day.UTC().Day(), 0, 0, 0, 0, time.UTC)
	for id, page := range f.pages {
		if page.Scope() == scope && page.RegistryDate != nil && page.RegistryDate.Equal(date) {
			page.Content += entry
			page.Version++
			f.pages[id] = page
			return &page, false, nil
		}
	}
	page := domain.WikiPage{ID: uuid.New(), WorkspaceID: ws, Title: domain.WikiRegistryTitle(day), Content: entry, Version: 1, CreatedBy: &user, UpdatedBy: &user, RegistryDate: &date}
	if scope.Kind == domain.WikiScopeTeam {
		page.TeamID = &scope.ID
	} else {
		page.ProjectID = &scope.ID
	}
	f.pages[page.ID] = page
	return &page, true, nil
}

func (f *fakeWikiRepo) AppendIssueRegistry(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, time.Time, string) (*domain.WikiPage, bool, error) {
	return nil, false, repository.ErrWikiRegistryDisabled
}

// RegistryDays returns registry pages of the scope, newest day first.
func (f *fakeWikiRepo) RegistryDays(ctx context.Context, ws uuid.UUID, scope domain.WikiScope, before *time.Time, limit int) ([]domain.WikiPage, error) {
	if err := f.RequireScope(ctx, ws, scope); err != nil {
		return nil, err
	}
	pages := []domain.WikiPage{}
	for _, page := range f.pages {
		if page.Scope() == scope && page.RegistryDate != nil && (before == nil || page.RegistryDate.Before(*before)) {
			pages = append(pages, page)
		}
	}
	sort.Slice(pages, func(i, j int) bool { return pages[i].RegistryDate.After(*pages[j].RegistryDate) })
	if len(pages) > limit {
		pages = pages[:limit]
	}
	return pages, nil
}

func (f *fakeWikiRepo) Settings(ctx context.Context, ws uuid.UUID, scope domain.WikiScope) (domain.WikiSettings, error) {
	if err := f.RequireScope(ctx, ws, scope); err != nil {
		return domain.WikiSettings{}, err
	}
	settings, ok := f.schedules[scope]
	if !ok {
		settings.WikiOverviewSchedule = domain.DefaultWikiOverviewSchedule()
	}
	settings.RegistryEnabled = f.registry[scope]
	return settings, nil
}

func (f *fakeWikiRepo) SetOverviewSchedule(ctx context.Context, ws uuid.UUID, scope domain.WikiScope, _ uuid.UUID, schedule domain.WikiOverviewSchedule, next *time.Time) error {
	if err := f.RequireScope(ctx, ws, scope); err != nil {
		return err
	}
	if f.schedules == nil {
		f.schedules = map[domain.WikiScope]domain.WikiSettings{}
	}
	f.schedules[scope] = domain.WikiSettings{WikiOverviewSchedule: schedule, NextRunAt: next}
	return nil
}

func (f *fakeWikiRepo) RegistryRange(ctx context.Context, ws uuid.UUID, scope domain.WikiScope, start, end time.Time) ([]domain.WikiPage, error) {
	pages, err := f.RegistryDays(ctx, ws, scope, nil, 1000)
	in := []domain.WikiPage{}
	for i := len(pages) - 1; i >= 0; i-- {
		if !pages[i].RegistryDate.Before(start) && !pages[i].RegistryDate.After(end) {
			in = append(in, pages[i])
		}
	}
	return in, err
}

func (f *fakeWikiRepo) CreateOverview(_ context.Context, overview *domain.WikiOverview) error {
	overview.ID, overview.GeneratedAt = uuid.New(), time.Now()
	f.overviews = append(f.overviews, *overview)
	return nil
}

func (f *fakeWikiRepo) ListOverviews(ctx context.Context, ws uuid.UUID, scope domain.WikiScope, _ *time.Time, limit int) ([]domain.WikiOverview, error) {
	if err := f.RequireScope(ctx, ws, scope); err != nil {
		return nil, err
	}
	return f.overviews[:min(limit, len(f.overviews))], nil
}

func (f *fakeWikiRepo) DueOverviewSchedules(_ context.Context, now time.Time, _ int) ([]domain.DueWikiOverview, error) {
	due := []domain.DueWikiOverview{}
	for scope, settings := range f.schedules {
		if settings.Enabled && settings.NextRunAt != nil && !settings.NextRunAt.After(now) {
			id := scope.ID
			due = append(due, domain.DueWikiOverview{SettingsID: id, TeamID: &id, NextRunAt: *settings.NextRunAt, Actor: &id, WikiOverviewSchedule: settings.WikiOverviewSchedule})
		}
	}
	return due, nil
}

func (f *fakeWikiRepo) ClaimOverviewRun(_ context.Context, id uuid.UUID, slot, next time.Time) (bool, error) {
	if f.claims == nil {
		f.claims = map[uuid.UUID]time.Time{}
	}
	if claimed, ok := f.claims[id]; ok && !claimed.Equal(slot) {
		return false, nil
	}
	f.claims[id] = next
	return true, nil
}

func (f *fakeWikiRepo) RecordOverviewRun(_ context.Context, id uuid.UUID, _ time.Time, runErr *string) error {
	if f.runs == nil {
		f.runs = map[uuid.UUID]*string{}
	}
	f.runs[id] = runErr
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
		require.ErrorIs(t, svc.Delete(ctx, ws, scope, page.ID, uuid.New()), ErrWikiNotFound, scope.String())
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

	for _, scope := range []domain.WikiScope{teamB, project, hidden} {
		_, err = svc.Revisions(ctx, ws, scope, page.ID)
		require.ErrorIs(t, err, ErrWikiNotFound, scope.String())
	}
	revisions, err := svc.Revisions(ctx, ws, teamA, page.ID)
	require.NoError(t, err)
	require.Len(t, revisions, 1)

	require.NoError(t, svc.Delete(ctx, ws, teamA, page.ID, uuid.New()))
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

type recordedWikiEvents struct{ events []realtime.Event }

func (r *recordedWikiEvents) Broadcast(_ uuid.UUID, event realtime.Event) {
	r.events = append(r.events, event)
}

// Open wikis learn about changes, but events never carry page text: clients
// reload through the API, which applies visibility.
func TestWikiServicePublishesChangesWithoutContent(t *testing.T) {
	ctx, ws, user := context.Background(), uuid.New(), uuid.New()
	team := domain.TeamWiki(uuid.New())
	events := &recordedWikiEvents{}
	svc := NewWikiService(newFakeWikiRepo(team), events)

	page, err := svc.Create(ctx, ws, team, user, dto.CreateWikiPageRequest{Title: "Secret plan", Content: "<p>classified</p>"})
	require.NoError(t, err)
	_, err = svc.Update(ctx, ws, team, page.ID, user, dto.UpdateWikiPageRequest{Content: ptr("<p>v2</p>"), Version: 1})
	require.NoError(t, err)
	_, err = svc.Update(ctx, ws, team, page.ID, user, dto.UpdateWikiPageRequest{Content: ptr("<p>stale</p>"), Version: 1})
	require.ErrorIs(t, err, ErrWikiConflict)
	require.NoError(t, svc.Delete(ctx, ws, team, page.ID, user))

	require.Len(t, events.events, 3, "failed writes publish nothing")
	for i, kind := range []string{"wiki.created", "wiki.updated", "wiki.deleted"} {
		require.Equal(t, kind, events.events[i].Type)
		payload := events.events[i].Payload.(WikiEventPayload)
		require.Equal(t, page.ID.String(), payload.ID)
		require.Equal(t, team.ID.String(), *payload.TeamID)
	}
	require.Equal(t, 2, events.events[1].Payload.(WikiEventPayload).Version)
}

// The registry is opt-in, entries land on the current UTC day's page with
// their time, and the text cannot inject markup.
func TestWikiServiceRegistryAppendsToToday(t *testing.T) {
	ctx, ws, user := context.Background(), uuid.New(), uuid.New()
	team, hidden := domain.TeamWiki(uuid.New()), domain.TeamWiki(uuid.New())
	repo := newFakeWikiRepo(team)
	events := &recordedWikiEvents{}
	svc := NewWikiService(repo, events)
	clock := time.Date(2026, 11, 9, 23, 58, 0, 0, time.FixedZone("CET", 3600))
	svc.now = func() time.Time { return clock }

	_, err := svc.AppendRegistry(ctx, ws, team, user, "KUA-20 implemented")
	require.ErrorIs(t, err, ErrWikiRegistryDisabled, "off by default")
	enabled, err := svc.RegistryEnabled(ctx, ws, team)
	require.NoError(t, err)
	require.False(t, enabled)

	require.NoError(t, svc.SetRegistryEnabled(ctx, ws, team, user, true))
	page, err := svc.AppendRegistry(ctx, ws, team, user, "  KUA-20 implemented,\n PR #161 <b>opened</b> ")
	require.NoError(t, err)
	require.Equal(t, "Registry 2026-11-09", page.Title)
	require.Equal(t, "<p><strong>22:58</strong> KUA-20 implemented, PR #161 &lt;b&gt;opened&lt;/b&gt;</p>", page.Content)

	page, err = svc.AppendRegistry(ctx, ws, team, user, "merged")
	require.NoError(t, err)
	require.Equal(t, 2, page.Version, "same day: appended to the same page")
	require.Contains(t, page.Content, "<p><strong>22:58</strong> merged</p>")

	clock = clock.Add(2 * time.Hour) // next UTC day
	page, err = svc.AppendRegistry(ctx, ws, team, user, "released")
	require.NoError(t, err)
	require.Equal(t, "Registry 2026-11-10", page.Title)
	require.Equal(t, 1, page.Version)

	require.Len(t, events.events, 3)
	require.Equal(t, []string{"wiki.created", "wiki.updated", "wiki.created"}, []string{events.events[0].Type, events.events[1].Type, events.events[2].Type})

	for _, text := range []string{"   ", strings.Repeat("x", domain.WikiRegistryEntryMaxRunes+1)} {
		_, err = svc.AppendRegistry(ctx, ws, team, user, text)
		var invalid *WikiValidationError
		require.ErrorAs(t, err, &invalid)
	}
	_, err = svc.AppendRegistry(ctx, ws, hidden, user, "leak")
	require.ErrorIs(t, err, ErrWikiNotFound)
	require.ErrorIs(t, svc.SetRegistryEnabled(ctx, ws, hidden, user, true), ErrWikiNotFound)
	require.Len(t, repo.appended, 3)
}

func TestWikiServiceRegistryFeedPagesByDay(t *testing.T) {
	ctx, ws, user := context.Background(), uuid.New(), uuid.New()
	team := domain.TeamWiki(uuid.New())
	repo := newFakeWikiRepo(team)
	repo.registry[team] = true
	svc := NewWikiService(repo)
	day := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	for i := range 3 {
		svc.now = func() time.Time { return day.AddDate(0, 0, i) }
		_, err := svc.AppendRegistry(ctx, ws, team, user, "entry")
		require.NoError(t, err)
	}
	pages, next, err := svc.RegistryDays(ctx, ws, team, nil, 2)
	require.NoError(t, err)
	require.Equal(t, []string{"Registry 2026-11-03", "Registry 2026-11-02"}, []string{pages[0].Title, pages[1].Title})
	require.Equal(t, "2026-11-02", next.Format(time.DateOnly))
	pages, next, err = svc.RegistryDays(ctx, ws, team, next, 2)
	require.NoError(t, err)
	require.Len(t, pages, 1)
	require.Nil(t, next, "the last page has no cursor")
	_, _, err = svc.RegistryDays(ctx, ws, team, nil, 32)
	var invalid *WikiValidationError
	require.ErrorAs(t, err, &invalid)
	_, _, err = svc.RegistryDays(ctx, ws, domain.TeamWiki(uuid.New()), nil, 0)
	require.ErrorIs(t, err, ErrWikiNotFound)
}

type fakeOverviewAI struct {
	prompts []string
	err     error
}

func (f *fakeOverviewAI) SummarizeRegistry(_ context.Context, _ uuid.UUID, _, start, end, entries string) (string, error) {
	f.prompts = append(f.prompts, start+".."+end+"\n"+entries)
	return `<h3>Shipped</h3><ul><li>KUA-20</li></ul><script>x</script>`, f.err
}

// Overviews summarise only the period's registry text, are sanitized, and
// report an empty period instead of calling the model.
func TestWikiServiceGeneratesOverviews(t *testing.T) {
	ctx, ws, user := context.Background(), uuid.New(), uuid.New()
	team := domain.TeamWiki(uuid.New())
	repo := newFakeWikiRepo(team)
	repo.registry[team] = true
	svc := NewWikiService(repo)
	day := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	for i, text := range []string{"KUA-19 merged", "KUA-20 implemented", "later"} {
		svc.now = func() time.Time { return day.AddDate(0, 0, i) }
		_, err := svc.AppendRegistry(ctx, ws, team, user, text)
		require.NoError(t, err)
	}
	_, err := svc.GenerateOverview(ctx, ws, team, &user, day, day, domain.WikiOverviewTriggerManual)
	require.ErrorIs(t, err, ErrAISettingsNotConfigured)
	ai := &fakeOverviewAI{}
	svc.SetOverviewAI(ai)

	overview, err := svc.GenerateOverview(ctx, ws, team, &user, day, day.AddDate(0, 0, 1), domain.WikiOverviewTriggerManual)
	require.NoError(t, err)
	require.Equal(t, "<h3>Shipped</h3><ul><li>KUA-20</li></ul>", overview.Content)
	require.Equal(t, team.ID, *overview.TeamID)
	require.Equal(t, "2026-11-02..2026-11-03\n2026-11-02:\n- 09:00 KUA-19 merged\n\n2026-11-03:\n- 09:00 KUA-20 implemented\n", ai.prompts[0])

	_, err = svc.GenerateOverview(ctx, ws, team, &user, day.AddDate(0, 0, 10), day.AddDate(0, 0, 11), domain.WikiOverviewTriggerManual)
	require.ErrorIs(t, err, ErrNoRegistryEntries)
	var invalid *WikiValidationError
	_, err = svc.GenerateOverview(ctx, ws, team, &user, day, day.AddDate(0, 0, 31), domain.WikiOverviewTriggerManual)
	require.ErrorAs(t, err, &invalid)
	_, err = svc.GenerateOverview(ctx, ws, team, &user, day, day.AddDate(0, 0, -1), domain.WikiOverviewTriggerManual)
	require.ErrorAs(t, err, &invalid)
	_, err = svc.GenerateOverview(ctx, ws, domain.TeamWiki(uuid.New()), &user, day, day, domain.WikiOverviewTriggerManual)
	require.ErrorIs(t, err, ErrWikiNotFound)
	require.Len(t, ai.prompts, 1)
}

// Saving a schedule computes its next run; a due slot is claimed once and
// its outcome recorded, with a readable error when AI fails.
func TestWikiServiceSchedulesOverviews(t *testing.T) {
	ctx, ws, user := context.Background(), uuid.New(), uuid.New()
	team := domain.TeamWiki(uuid.New())
	repo := newFakeWikiRepo(team)
	svc := NewWikiService(repo)
	now := time.Date(2026, 11, 9, 8, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }

	_, err := svc.UpdateSettings(ctx, ws, team, user, nil, &domain.WikiOverviewSchedule{Enabled: true, Frequency: "hourly", Time: "09:00", PeriodDays: 7})
	var invalid *WikiValidationError
	require.ErrorAs(t, err, &invalid)
	_, err = svc.UpdateSettings(ctx, ws, team, user, nil, nil)
	require.ErrorAs(t, err, &invalid)
	on := true
	settings, err := svc.UpdateSettings(ctx, ws, team, user, &on, &domain.WikiOverviewSchedule{Enabled: true, Frequency: domain.WikiOverviewDaily, Time: "09:00", PeriodDays: 1})
	require.NoError(t, err)
	require.True(t, settings.RegistryEnabled)
	require.Equal(t, time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC), *settings.NextRunAt)

	require.Zero(t, svc.runDueOverviews(ctx), "nothing is due before the slot")
	now = now.Add(2 * time.Hour)
	repo.schedules[team] = domain.WikiSettings{WikiOverviewSchedule: settings.WikiOverviewSchedule, NextRunAt: settings.NextRunAt}
	ai := &fakeOverviewAI{err: ErrAISettingsNotConfigured}
	svc.SetOverviewAI(ai)
	_, err = svc.AppendRegistry(ctx, ws, team, user, "KUA-20 implemented")
	require.NoError(t, err)
	require.Equal(t, 1, svc.runDueOverviews(ctx))
	require.Contains(t, *repo.runs[team.ID], "Settings → AI")
	require.Equal(t, time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC), repo.claims[team.ID], "claimed for the next slot")
	require.Zero(t, svc.runDueOverviews(ctx), "a slot another claimer moved is not run again")
}
