package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// wikiPagePublished holds while the page is published publicly and its
// team or project is visible to anonymous readers (never a private team).
// Arguments are trusted SQL expressions.
func wikiPagePublished(page, workspace string) string {
	public := domain.PublicContext(context.Background())
	return "EXISTS (SELECT 1 FROM wiki_pages slw WHERE slw.id = " + page + " AND slw.workspace_id = " + workspace +
		" AND slw.publish_mode = 'public' AND ((slw.team_id IS NOT NULL AND " + teamVisible(public, "slw.team_id") + ") OR " +
		"(slw.project_id IS NOT NULL AND EXISTS (SELECT 1 FROM projects slwp WHERE slwp.id = slw.project_id AND " +
		projectVisible(public, "slwp.workspace_id", "slwp.team_id") + "))))"
}

// SetPublish changes a visible page's publication.
func (r *WikiRepository) SetPublish(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, pageID uuid.UUID, mode string, children bool) error {
	if !scope.Valid() {
		return ErrWikiPageNotFound
	}
	var id uuid.UUID
	err := r.db.GetContext(ctx, &id, `UPDATE wiki_pages SET publish_mode = $4, publish_children = $5 WHERE `+wikiPageInScope(ctx, scope)+
		` AND wiki_pages.id = $3 RETURNING wiki_pages.id`, workspaceID, scope.ID, pageID, mode, children)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrWikiPageNotFound
	}
	return err
}

// PublishState reads a visible page's publication and its public link token.
func (r *WikiRepository) PublishState(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, pageID uuid.UUID) (*domain.WikiPublication, error) {
	if !scope.Valid() {
		return nil, ErrWikiPageNotFound
	}
	var state domain.WikiPublication
	err := r.db.GetContext(ctx, &state, `SELECT wiki_pages.publish_mode AS mode, wiki_pages.publish_children AS children,
		(SELECT link.id FROM shared_links link WHERE link.scope = 'wiki_page' AND link.scope_id = wiki_pages.id AND link.workspace_id = wiki_pages.workspace_id) AS link_id,
		(SELECT link.token FROM shared_links link WHERE link.scope = 'wiki_page' AND link.scope_id = wiki_pages.id AND link.workspace_id = wiki_pages.workspace_id) AS token
		FROM wiki_pages WHERE `+wikiPageInScope(ctx, scope)+` AND wiki_pages.id = $3`, workspaceID, scope.ID, pageID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrWikiPageNotFound
	}
	if err != nil {
		return nil, err
	}
	return &state, nil
}

// DeleteWikiPageLinks removes a page's public links, so their tokens stop
// working at once. The page must be visible to the caller.
func (r *WikiRepository) DeleteWikiPageLinks(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, pageID uuid.UUID) error {
	if !scope.Valid() {
		return ErrWikiPageNotFound
	}
	_, err := r.db.ExecContext(ctx, `DELETE FROM shared_links link WHERE link.scope = 'wiki_page' AND link.scope_id = $3 AND link.workspace_id = $1
		AND EXISTS (SELECT 1 FROM wiki_pages WHERE `+wikiPageInScope(ctx, scope)+` AND wiki_pages.id = $3)`, workspaceID, scope.ID, pageID)
	return err
}

// PublicWikiPage is what an anonymous reader may see of a published page.
type PublicWikiPage struct {
	ID        uuid.UUID  `db:"id"`
	ParentID  *uuid.UUID `db:"parent_id"`
	Title     string     `db:"title"`
	Content   string     `db:"content"`
	Position  int        `db:"position"`
	UpdatedAt time.Time  `db:"updated_at"`
}

// PublishedPages returns a publicly published page and, when it publishes
// its children, every page nested under it, root first. Registry days are
// never included. The root must pass wikiPagePublished.
func (r *WikiRepository) PublishedPages(ctx context.Context, workspaceID, rootID uuid.UUID) ([]PublicWikiPage, error) {
	pages := []PublicWikiPage{}
	query := `WITH RECURSIVE published AS (
			SELECT wiki_pages.*, 0 AS depth FROM wiki_pages
			WHERE wiki_pages.id = $1 AND wiki_pages.workspace_id = $2 AND wiki_pages.registry_date IS NULL AND ` + wikiPagePublished("wiki_pages.id", "wiki_pages.workspace_id") + `
			UNION ALL
			SELECT child.*, published.depth + 1 FROM wiki_pages child
			JOIN published ON child.parent_id = published.id
			JOIN wiki_pages root ON root.id = $1 AND root.publish_children
			WHERE child.registry_date IS NULL AND published.depth < 16
		)
		SELECT id, CASE WHEN id = $1 THEN NULL ELSE parent_id END AS parent_id, title, content, position, updated_at
		FROM published ORDER BY depth, position, id`
	err := r.db.SelectContext(ctx, &pages, query, rootID, workspaceID)
	return pages, err
}

// PublicPageReachable reports whether a page is readable through some public
// link right now: published itself, or nested under a page that publishes
// its children.
func (r *WikiRepository) PublicPageReachable(ctx context.Context, workspaceID, pageID uuid.UUID) (bool, error) {
	var reachable bool
	err := r.db.GetContext(ctx, &reachable, `WITH RECURSIVE ancestors AS (
			SELECT wiki_pages.id, wiki_pages.parent_id, wiki_pages.publish_mode, wiki_pages.publish_children, 0 AS depth
			FROM wiki_pages WHERE wiki_pages.id = $1 AND wiki_pages.workspace_id = $2 AND wiki_pages.registry_date IS NULL
			UNION ALL
			SELECT parent.id, parent.parent_id, parent.publish_mode, parent.publish_children, ancestors.depth + 1
			FROM wiki_pages parent JOIN ancestors ON parent.id = ancestors.parent_id WHERE ancestors.depth < 16
		)
		SELECT EXISTS (SELECT 1 FROM ancestors WHERE (ancestors.depth = 0 OR ancestors.publish_children)
			AND `+wikiPagePublished("ancestors.id", "CAST($2 AS uuid)")+`)`, pageID, workspaceID)
	return reachable, err
}
