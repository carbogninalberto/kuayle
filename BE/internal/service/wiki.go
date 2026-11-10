package service

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/realtime"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/sanitize"
)

var (
	// ErrWikiNotFound hides whether a scope or page is missing or invisible.
	ErrWikiNotFound = errors.New("wiki page not found")
	ErrWikiConflict = errors.New("wiki page was changed by someone else; reload it before saving")
	// ErrWikiRegistryDisabled: the scope's registry is off; turn it on in the wiki first.
	ErrWikiRegistryDisabled = errors.New("the registry of this wiki is turned off")
)

// WikiValidationError rejects a request field without touching storage.
type WikiValidationError struct {
	Field   string
	Message string
}

func (e *WikiValidationError) Error() string { return e.Field + ": " + e.Message }

// WikiService manages team and project wiki pages. Visibility of the scope is
// enforced by the repository for every operation, using the caller's access
// in the request context; the scope always comes from the route.
type WikiService struct {
	repo   repository.WikiRepo
	events WikiEvents
	// now is the clock that picks the registry day; tests replace it.
	now func() time.Time
	// overviewAI writes registry overviews; nil until SetOverviewAI.
	overviewAI WikiOverviewAI
	// issues resolves issue identifiers for attached references; nil until SetIssues.
	issues WikiIssueLookup
}

// WikiEvents receives realtime events; the hub withholds them from members
// of workspaces with private teams, who get a generic refresh instead.
type WikiEvents interface {
	Broadcast(workspaceID uuid.UUID, event realtime.Event)
}

// WikiEventPayload tells open wikis that a page changed. It carries no title
// or content: clients reload the page through the API, which checks access.
type WikiEventPayload struct {
	ID        string  `json:"id"`
	TeamID    *string `json:"team_id"`
	ProjectID *string `json:"project_id"`
	Version   int     `json:"version"`
	UpdatedBy *string `json:"updated_by"`
}

func NewWikiService(repo repository.WikiRepo, events ...WikiEvents) *WikiService {
	svc := &WikiService{repo: repo, now: time.Now}
	if len(events) > 0 {
		svc.events = events[0]
	}
	return svc
}

func (s *WikiService) publish(kind string, page *domain.WikiPage) {
	if s.events == nil {
		return
	}
	payload := WikiEventPayload{ID: page.ID.String(), Version: page.Version, TeamID: uuidString(page.TeamID), ProjectID: uuidString(page.ProjectID), UpdatedBy: uuidString(page.UpdatedBy)}
	s.events.Broadcast(page.WorkspaceID, realtime.Event{Type: kind, Payload: payload})
}

func uuidString(id *uuid.UUID) *string {
	if id == nil {
		return nil
	}
	value := id.String()
	return &value
}

func (s *WikiService) List(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope) ([]domain.WikiPage, error) {
	pages, err := s.repo.List(ctx, workspaceID, scope)
	return pages, wikiError(err)
}

func (s *WikiService) Get(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, pageID uuid.UUID) (*domain.WikiPage, error) {
	page, err := s.repo.Get(ctx, workspaceID, scope, pageID)
	if err != nil {
		return nil, wikiError(err)
	}
	return page, nil
}

func (s *WikiService) Create(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, userID uuid.UUID, req dto.CreateWikiPageRequest) (*domain.WikiPage, error) {
	if !scope.Valid() {
		return nil, ErrWikiNotFound
	}
	title, err := wikiTitle(req.Title)
	if err != nil {
		return nil, err
	}
	content, err := wikiContent(req.Content)
	if err != nil {
		return nil, err
	}
	page := &domain.WikiPage{ID: uuid.New(), WorkspaceID: workspaceID, Title: title, Content: content, CreatedBy: &userID}
	if scope.Kind == domain.WikiScopeTeam {
		page.TeamID = &scope.ID
	} else {
		page.ProjectID = &scope.ID
	}
	if err := s.repo.Create(ctx, page); err != nil {
		return nil, wikiError(err)
	}
	s.publish("wiki.created", page)
	return page, nil
}

// Update replaces the provided fields if the page is still at req.Version.
func (s *WikiService) Update(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, pageID, userID uuid.UUID, req dto.UpdateWikiPageRequest) (*domain.WikiPage, error) {
	if req.Title == nil && req.Content == nil {
		return nil, &WikiValidationError{Field: "title", Message: "provide a title or content to update"}
	}
	if req.Version < 1 {
		return nil, &WikiValidationError{Field: "version", Message: "is required"}
	}
	page, err := s.repo.Get(ctx, workspaceID, scope, pageID)
	if err != nil {
		return nil, wikiError(err)
	}
	if req.Title != nil {
		if page.Title, err = wikiTitle(*req.Title); err != nil {
			return nil, err
		}
	}
	if req.Content != nil {
		if page.Content, err = wikiContent(*req.Content); err != nil {
			return nil, err
		}
	}
	page.UpdatedBy = &userID
	if err := s.repo.Update(ctx, workspaceID, scope, page, req.Version); err != nil {
		return nil, wikiError(err)
	}
	s.publish("wiki.updated", page)
	return page, nil
}

func (s *WikiService) Delete(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, pageID uuid.UUID, userID uuid.UUID) error {
	if err := s.repo.Delete(ctx, workspaceID, scope, pageID); err != nil {
		return wikiError(err)
	}
	page := &domain.WikiPage{ID: pageID, WorkspaceID: workspaceID, UpdatedBy: &userID}
	if scope.Kind == domain.WikiScopeTeam {
		page.TeamID = &scope.ID
	} else {
		page.ProjectID = &scope.ID
	}
	s.publish("wiki.deleted", page)
	return nil
}

// Revisions returns the recorded changes of a visible page, newest first.
func (s *WikiService) Revisions(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, pageID uuid.UUID) ([]domain.WikiRevision, error) {
	revisions, err := s.repo.ListRevisions(ctx, workspaceID, scope, pageID)
	return revisions, wikiError(err)
}

// RegistryEnabled reports whether the scope's registry is on.
func (s *WikiService) RegistryEnabled(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope) (bool, error) {
	enabled, err := s.repo.RegistryEnabled(ctx, workspaceID, scope)
	return enabled, wikiError(err)
}

// SetRegistryEnabled turns the scope's registry on or off.
func (s *WikiService) SetRegistryEnabled(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, userID uuid.UUID, enabled bool) error {
	return wikiError(s.repo.SetRegistryEnabled(ctx, workspaceID, scope, userID, enabled))
}

// AppendRegistry adds a timestamped plain-text entry to today's (UTC)
// registry page of the scope, creating the page for the day's first entry.
func (s *WikiService) AppendRegistry(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, userID uuid.UUID, text string) (*domain.WikiPage, error) {
	entry, err := wikiRegistryEntry(text, s.now())
	if err != nil {
		return nil, err
	}
	page, created, err := s.repo.AppendRegistry(ctx, workspaceID, scope, userID, s.now(), entry)
	if err != nil {
		return nil, wikiError(err)
	}
	if created {
		s.publish("wiki.created", page)
	} else {
		s.publish("wiki.updated", page)
	}
	return page, nil
}

// Registry feed page sizes, in days.
const (
	WikiRegistryDaysDefault = 7
	WikiRegistryDaysMax     = 31
)

// RegistryDays returns a page of the scope's registry, newest day first, and
// the cursor for the next (older) page, nil when there is none.
func (s *WikiService) RegistryDays(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, before *time.Time, limit int) ([]domain.WikiPage, *time.Time, error) {
	if limit <= 0 {
		limit = WikiRegistryDaysDefault
	}
	if limit > WikiRegistryDaysMax {
		return nil, nil, &WikiValidationError{Field: "limit", Message: "must be at most 31"}
	}
	pages, err := s.repo.RegistryDays(ctx, workspaceID, scope, before, limit+1)
	if err != nil {
		return nil, nil, wikiError(err)
	}
	if len(pages) <= limit {
		return pages, nil, nil
	}
	pages = pages[:limit]
	return pages, pages[limit-1].RegistryDate, nil
}

// wikiRegistryEntry renders one plain-text registry entry as editor HTML.
func wikiRegistryEntry(raw string, at time.Time) (string, error) {
	entry, err := domain.WikiRegistryEntryHTML(sanitize.PlainText(raw), at)
	if err != nil {
		return "", &WikiValidationError{Field: "text", Message: "must be 1 to 2000 characters"}
	}
	return entry, nil
}

// AppendIssueRegistry logs text about an issue in today's registry of its
// project, or of its team when the project's registry is off. It returns
// ErrWikiRegistryDisabled when neither registry takes it.
func (s *WikiService) AppendIssueRegistry(ctx context.Context, workspaceID, issueID, userID uuid.UUID, text string) error {
	page, created, err := s.repo.AppendIssueRegistry(ctx, workspaceID, issueID, userID, s.now(), sanitize.PlainText(text))
	if errors.Is(err, domain.ErrWikiRegistryEntry) {
		return &WikiValidationError{Field: "text", Message: "must be 1 to 2000 characters"}
	}
	if err != nil {
		return wikiError(err)
	}
	if created {
		s.publish("wiki.created", page)
	} else {
		s.publish("wiki.updated", page)
	}
	return nil
}

func wikiTitle(raw string) (string, error) {
	title := strings.TrimSpace(sanitize.PlainText(raw))
	if title == "" {
		return "", &WikiValidationError{Field: "title", Message: "is required"}
	}
	if utf8.RuneCountInString(title) > domain.WikiTitleMaxLength {
		return "", &WikiValidationError{Field: "title", Message: "must be at most 200 characters"}
	}
	return title, nil
}

func wikiContent(raw string) (string, error) {
	tooLarge := &WikiValidationError{Field: "content", Message: "must be at most 512 KB"}
	// Reject oversized input before sanitizing it; sanitizing only shrinks it.
	if len(raw) > 2*domain.WikiContentMaxBytes {
		return "", tooLarge
	}
	content := sanitize.SanitizeEditorContent(raw)
	if len(content) > domain.WikiContentMaxBytes {
		return "", tooLarge
	}
	return content, nil
}

func wikiError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, repository.ErrWikiScopeNotFound), errors.Is(err, repository.ErrWikiPageNotFound):
		return ErrWikiNotFound
	case errors.Is(err, repository.ErrWikiVersionConflict):
		return ErrWikiConflict
	case errors.Is(err, repository.ErrWikiRegistryDisabled):
		return ErrWikiRegistryDisabled
	case errors.Is(err, repository.ErrWikiRegistryFull):
		return &WikiValidationError{Field: "text", Message: "today's registry page is full"}
	}
	return err
}
