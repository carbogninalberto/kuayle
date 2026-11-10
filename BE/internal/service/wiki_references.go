package service

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/sanitize"
)

// WikiIssueLookup finds an issue by its identifier (or a retired alias of
// it) when the caller can see it; nil when they cannot.
type WikiIssueLookup interface {
	GetByIdentifier(ctx context.Context, workspaceID uuid.UUID, identifier string) (*domain.Issue, error)
}

// SetIssues lets issues hold attached page references.
func (s *WikiService) SetIssues(issues WikiIssueLookup) { s.issues = issues }

// ResolvedWikiReference is one requested page reference. Accessible is false
// for a page that is missing, in another workspace or invisible to the
// caller; those are indistinguishable and carry nothing but the ID asked for.
type ResolvedWikiReference struct {
	ID         uuid.UUID
	Accessible bool
	Page       domain.WikiReference
}

// SearchReferences suggests pages to mention from every wiki of the
// workspace the caller can see.
func (s *WikiService) SearchReferences(ctx context.Context, workspaceID uuid.UUID, query string, limit int) ([]domain.WikiReference, error) {
	query = strings.TrimSpace(query)
	if utf8.RuneCountInString(query) > domain.WikiTitleMaxLength {
		return nil, &WikiValidationError{Field: "q", Message: "must be at most 200 characters"}
	}
	if limit <= 0 || limit > domain.WikiReferenceSearchLimit {
		limit = domain.WikiReferenceSearchLimit
	}
	return s.repo.SearchReferences(ctx, workspaceID, query, limit)
}

// ResolveReferences answers every requested ID, in order: with the page's
// title and scope when the caller can see it, and as inaccessible otherwise.
func (s *WikiService) ResolveReferences(ctx context.Context, workspaceID uuid.UUID, pageIDs []uuid.UUID) ([]ResolvedWikiReference, error) {
	if len(pageIDs) > domain.WikiReferenceResolveLimit {
		return nil, &WikiValidationError{Field: "ids", Message: "must hold at most 200 page IDs"}
	}
	visible, err := s.repo.ResolveReferences(ctx, workspaceID, pageIDs)
	if err != nil {
		return nil, err
	}
	byID := make(map[uuid.UUID]domain.WikiReference, len(visible))
	for _, page := range visible {
		byID[page.ID] = page
	}
	resolved := make([]ResolvedWikiReference, 0, len(pageIDs))
	seen := make(map[uuid.UUID]bool, len(pageIDs))
	for _, id := range pageIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		page, ok := byID[id]
		resolved = append(resolved, ResolvedWikiReference{ID: id, Accessible: ok, Page: page})
	}
	return resolved, nil
}

// Backlinks lists the issues and pages that reference a visible page.
func (s *WikiService) Backlinks(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, pageID uuid.UUID) (*domain.WikiBacklinks, error) {
	links, err := s.repo.Backlinks(ctx, workspaceID, scope, pageID)
	return links, wikiError(err)
}

func (s *WikiService) visibleIssue(ctx context.Context, workspaceID uuid.UUID, identifier string) (*domain.Issue, error) {
	if s.issues == nil {
		return nil, ErrWikiNotFound
	}
	issue, err := s.issues.GetByIdentifier(ctx, workspaceID, identifier)
	if err != nil {
		return nil, err
	}
	if issue == nil {
		return nil, ErrWikiNotFound
	}
	return issue, nil
}

// IssueReferences returns the pages attached to an issue the caller can see.
// Attached pages the caller cannot see come back as inaccessible.
func (s *WikiService) IssueReferences(ctx context.Context, workspaceID uuid.UUID, identifier string) ([]ResolvedWikiReference, error) {
	issue, err := s.visibleIssue(ctx, workspaceID, identifier)
	if err != nil {
		return nil, err
	}
	ids, err := s.repo.IssueReferences(ctx, workspaceID, issue.ID)
	if err != nil {
		return nil, err
	}
	if len(ids) > domain.WikiReferenceResolveLimit {
		ids = ids[:domain.WikiReferenceResolveLimit]
	}
	return s.ResolveReferences(ctx, workspaceID, ids)
}

// AttachIssueReference attaches a page to an issue; the caller must see both.
func (s *WikiService) AttachIssueReference(ctx context.Context, workspaceID uuid.UUID, identifier string, pageID, userID uuid.UUID) ([]ResolvedWikiReference, error) {
	issue, err := s.visibleIssue(ctx, workspaceID, identifier)
	if err != nil {
		return nil, err
	}
	if err := s.repo.AttachIssueReference(ctx, workspaceID, issue.ID, pageID, userID); err != nil {
		if errors.Is(err, repository.ErrWikiReferenceTarget) {
			return nil, ErrWikiNotFound
		}
		return nil, err
	}
	return s.IssueReferences(ctx, workspaceID, identifier)
}

// DetachIssueReference removes an attached page from an issue the caller
// can see.
func (s *WikiService) DetachIssueReference(ctx context.Context, workspaceID uuid.UUID, identifier string, pageID uuid.UUID) error {
	issue, err := s.visibleIssue(ctx, workspaceID, identifier)
	if err != nil {
		return err
	}
	return s.repo.DetachIssueReference(ctx, workspaceID, issue.ID, pageID)
}

// publicWikiReferences returns the pages that published content references
// and an anonymous reader may know by title. ctx must be a public context.
func publicWikiReferences(ctx context.Context, repo wikiPublishRepo, workspaceID uuid.UUID, pages []repository.PublicWikiPage) ([]domain.WikiReference, error) {
	ids := []uuid.UUID{}
	seen := map[uuid.UUID]bool{}
	for _, page := range pages {
		for _, id := range sanitize.WikiMentionIDs(page.Content) {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	if len(ids) > domain.WikiReferenceResolveLimit {
		ids = ids[:domain.WikiReferenceResolveLimit]
	}
	return repo.ResolveReferences(ctx, workspaceID, ids)
}
