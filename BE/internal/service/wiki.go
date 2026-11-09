package service

import (
	"context"
	"errors"
	"strings"
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
	svc := &WikiService{repo: repo}
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
	}
	return err
}
