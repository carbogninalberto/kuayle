package service

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
)

// wikiTreeRepo is the page-tree part of the wiki repository. It is separate
// from repository.WikiRepo so stores without a tree keep compiling; the
// PostgreSQL repository implements both.
type wikiTreeRepo interface {
	Tree(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope) ([]domain.WikiTreeNode, error)
	Move(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, pageID uuid.UUID, parent *uuid.UUID, index int) ([]domain.WikiTreeNode, error)
	SetKind(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, pageID uuid.UUID, kind string) error
}

var errWikiTreeUnsupported = errors.New("wiki tree is not supported by this store")

func (s *WikiService) treeRepo() (wikiTreeRepo, error) {
	tree, ok := s.repo.(wikiTreeRepo)
	if !ok {
		return nil, errWikiTreeUnsupported
	}
	return tree, nil
}

// Tree returns every page's place in a visible scope; registry days are
// flagged so callers keep them out of the tree.
func (s *WikiService) Tree(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope) ([]domain.WikiTreeNode, error) {
	tree, err := s.treeRepo()
	if err != nil {
		return nil, err
	}
	nodes, err := tree.Tree(ctx, workspaceID, scope)
	return nodes, wikiError(err)
}

// Move reorders a page or moves it into or out of another page or folder of
// the same scope. Cycles, cross-scope parents and nesting deeper than
// domain.WikiMaxDepth are rejected by the repository under row locks.
func (s *WikiService) Move(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, pageID, userID uuid.UUID, parent *uuid.UUID, index int) ([]domain.WikiTreeNode, error) {
	tree, err := s.treeRepo()
	if err != nil {
		return nil, err
	}
	nodes, err := tree.Move(ctx, workspaceID, scope, pageID, parent, index)
	if err != nil {
		return nil, wikiMoveError(err)
	}
	s.publishTree(workspaceID, scope, pageID, userID)
	return nodes, nil
}

// CreateIn creates a page or folder, then places it last under parent. The
// page is created through Create, so validation, sanitizing and events are
// the same as for any page; if placing fails it stays at the top level.
func (s *WikiService) CreateIn(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, userID uuid.UUID, req dto.CreateWikiPageRequest, kind string, parent *uuid.UUID) (*domain.WikiPage, error) {
	if kind == "" {
		kind = domain.WikiKindPage
	}
	if kind != domain.WikiKindPage && kind != domain.WikiKindFolder {
		return nil, &WikiValidationError{Field: "kind", Message: "must be page or folder"}
	}
	tree, err := s.treeRepo()
	if err != nil {
		return nil, err
	}
	page, err := s.Create(ctx, workspaceID, scope, userID, req)
	if err != nil {
		return nil, err
	}
	if kind != domain.WikiKindPage {
		if err := tree.SetKind(ctx, workspaceID, scope, page.ID, kind); err != nil {
			return nil, wikiError(err)
		}
	}
	if _, err := tree.Move(ctx, workspaceID, scope, page.ID, parent, 1<<30); err != nil {
		return page, wikiMoveError(err)
	}
	return page, nil
}

func (s *WikiService) publishTree(workspaceID uuid.UUID, scope domain.WikiScope, pageID, userID uuid.UUID) {
	page := &domain.WikiPage{ID: pageID, WorkspaceID: workspaceID, UpdatedBy: &userID}
	if scope.Kind == domain.WikiScopeTeam {
		page.TeamID = &scope.ID
	} else {
		page.ProjectID = &scope.ID
	}
	s.publish("wiki.updated", page)
}

func wikiMoveError(err error) error {
	switch {
	case errors.Is(err, domain.ErrWikiMoveCycle), errors.Is(err, domain.ErrWikiMoveTooDeep), errors.Is(err, domain.ErrWikiMoveNotInTree):
		return &WikiValidationError{Field: "parent_id", Message: err.Error()}
	}
	return wikiError(err)
}
