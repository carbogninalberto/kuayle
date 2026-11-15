package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
)

// ErrWikiRestoreExpired: the deleted page is past its restore period.
var ErrWikiRestoreExpired = errors.New("wiki page is past its restore period")

// wikiRestoreRepo is the restore part of the wiki repository, separate from
// repository.WikiRepo for the same reason as wikiTreeRepo.
type wikiRestoreRepo interface {
	Restore(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, pageID uuid.UUID, deletedSince time.Time) (*domain.WikiPage, error)
}

// SetDeleteGracePeriod sets how long deleted pages stay restorable.
func (s *WikiService) SetDeleteGracePeriod(grace time.Duration) {
	s.deleteGrace = grace
}

// DeleteGracePeriod is how long deleted pages stay restorable; it is the
// same period as for issues.
func (s *WikiService) DeleteGracePeriod() time.Duration {
	if s.deleteGrace <= 0 {
		return DefaultIssueDeleteGracePeriod
	}
	return s.deleteGrace
}

// Restore brings back a deleted page of a visible scope within the grace
// period and tells open wikis, as for a new page.
func (s *WikiService) Restore(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, pageID uuid.UUID) (*domain.WikiPage, error) {
	store, ok := s.repo.(wikiRestoreRepo)
	if !ok {
		return nil, ErrWikiNotFound
	}
	page, err := store.Restore(ctx, workspaceID, scope, pageID, s.now().Add(-s.DeleteGracePeriod()))
	if errors.Is(err, repository.ErrWikiRestoreExpired) {
		return nil, ErrWikiRestoreExpired
	}
	if err != nil {
		return nil, wikiError(err)
	}
	s.publish("wiki.created", page)
	return page, nil
}
