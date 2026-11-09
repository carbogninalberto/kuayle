package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

const wikiTreeColumns = `wiki_pages.id, wiki_pages.parent_id, wiki_pages.position, wiki_pages.kind,
	(wiki_pages.registry_date IS NOT NULL) AS registry`

// Tree returns where every page of a visible scope sits, registry days
// included (flagged) so callers can keep them out of the tree.
func (r *WikiRepository) Tree(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope) ([]domain.WikiTreeNode, error) {
	if err := r.RequireScope(ctx, workspaceID, scope); err != nil {
		return nil, err
	}
	return wikiTree(ctx, r.db, scope, workspaceID, "")
}

func wikiTree(ctx context.Context, db sqlx.QueryerContext, scope domain.WikiScope, workspaceID uuid.UUID, lock string) ([]domain.WikiTreeNode, error) {
	nodes := []domain.WikiTreeNode{}
	query := `SELECT ` + wikiTreeColumns + ` FROM wiki_pages WHERE ` + wikiPageInScope(ctx, scope) +
		` ORDER BY wiki_pages.position, wiki_pages.id` + lock
	err := sqlx.SelectContext(ctx, db, &nodes, query, workspaceID, scope.ID)
	return nodes, err
}

// Move places a page under parent (nil for the top level) at index among
// its new siblings. The scope's pages are locked for the duration, so the
// validation (same scope, no cycle, depth limit) and the renumbering of both
// sibling lists see one consistent tree. A page or parent outside the
// caller's visible scope reports ErrWikiPageNotFound.
func (r *WikiRepository) Move(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, pageID uuid.UUID, parent *uuid.UUID, index int) ([]domain.WikiTreeNode, error) {
	if !scope.Valid() {
		return nil, ErrWikiPageNotFound
	}
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	nodes, err := wikiTree(ctx, tx, scope, workspaceID, " FOR UPDATE OF wiki_pages")
	if err != nil {
		return nil, err
	}
	changes, err := domain.PlanWikiMove(nodes, pageID, parent, index)
	if errors.Is(err, domain.ErrWikiMoveNotFound) {
		return nil, ErrWikiPageNotFound
	}
	if err != nil {
		return nil, err
	}
	for _, node := range changes {
		// The rows are locked and in scope; the id alone identifies them.
		if _, err := tx.ExecContext(ctx, `UPDATE wiki_pages SET parent_id = $2, position = $3 WHERE id = $1`, node.ID, node.ParentID, node.Position); err != nil {
			return nil, err
		}
	}
	nodes, err = wikiTree(ctx, tx, scope, workspaceID, "")
	if err != nil {
		return nil, err
	}
	return nodes, tx.Commit()
}

// SetKind makes a visible page a folder or a plain page.
func (r *WikiRepository) SetKind(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, pageID uuid.UUID, kind string) error {
	if !scope.Valid() {
		return ErrWikiPageNotFound
	}
	var id uuid.UUID
	err := r.db.GetContext(ctx, &id, `UPDATE wiki_pages SET kind = $4 WHERE `+wikiPageInScope(ctx, scope)+
		` AND wiki_pages.id = $3 AND wiki_pages.registry_date IS NULL RETURNING wiki_pages.id`, workspaceID, scope.ID, pageID, kind)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrWikiPageNotFound
	}
	return err
}
