package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// wiki_pages is a view over wiki_page_records without soft-deleted pages
// (migration 000161). Only delete, restore and purge name the table.

// ErrWikiRestoreExpired means a deleted page is past its restore period.
var ErrWikiRestoreExpired = errors.New("wiki page is past its restore period")

// Delete soft-deletes a visible page. Its children move up to its parent, as
// a hard delete did, and stay there if the page is restored. A registry page
// gives up its date so the registry can start a new page for that day.
func (r *WikiRepository) Delete(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, pageID uuid.UUID) error {
	if !scope.Valid() {
		return ErrWikiPageNotFound
	}
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var parentID *uuid.UUID
	err = tx.GetContext(ctx, &parentID, `UPDATE wiki_page_records AS wiki_pages SET deleted_at = NOW(), deleted_by = $4,
			deleted_registry_date = registry_date, registry_date = NULL
		WHERE `+wikiPageInScope(ctx, scope)+` AND wiki_pages.id = $3 AND wiki_pages.deleted_at IS NULL
		RETURNING wiki_pages.parent_id`, workspaceID, scope.ID, pageID, nullableUUID(issueActor(ctx)))
	if errors.Is(err, sql.ErrNoRows) {
		return ErrWikiPageNotFound
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE wiki_page_records SET parent_id = $2 WHERE parent_id = $1`, pageID, parentID); err != nil {
		return err
	}
	return tx.Commit()
}

// Restore brings back a page deleted at or after deletedSince and returns it.
// It goes back under its parent if that page still exists (else to the top
// level), and gets its registry date back if no other page took that day.
// Restoring a page that is not deleted returns it unchanged.
func (r *WikiRepository) Restore(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, pageID uuid.UUID, deletedSince time.Time) (*domain.WikiPage, error) {
	if !scope.Valid() {
		return nil, ErrWikiPageNotFound
	}
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var deletedAt *time.Time
	err = tx.GetContext(ctx, &deletedAt, `SELECT wiki_pages.deleted_at FROM wiki_page_records AS wiki_pages
		WHERE `+wikiPageInScope(ctx, scope)+` AND wiki_pages.id = $3 FOR UPDATE OF wiki_pages`, workspaceID, scope.ID, pageID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrWikiPageNotFound
	}
	if err != nil {
		return nil, err
	}
	if deletedAt != nil {
		if deletedAt.Before(deletedSince) {
			return nil, ErrWikiRestoreExpired
		}
		if _, err := tx.ExecContext(ctx, `UPDATE wiki_page_records restored SET deleted_at = NULL, deleted_by = NULL,
				parent_id = CASE WHEN EXISTS (SELECT 1 FROM wiki_pages parent WHERE parent.id = restored.parent_id) THEN restored.parent_id END,
				registry_date = CASE WHEN NOT EXISTS (SELECT 1 FROM wiki_pages taken WHERE taken.registry_date = restored.deleted_registry_date
					AND taken.team_id IS NOT DISTINCT FROM restored.team_id AND taken.project_id IS NOT DISTINCT FROM restored.project_id)
					THEN restored.deleted_registry_date END,
				deleted_registry_date = NULL
			WHERE restored.id = $1`, pageID); err != nil {
			return nil, err
		}
	}
	var page domain.WikiPage
	if err := tx.GetContext(ctx, &page, `SELECT `+wikiPageColumns+` FROM wiki_pages`+wikiEditorJoin+` WHERE `+wikiPageInScope(ctx, scope)+` AND wiki_pages.id = $3`,
		workspaceID, scope.ID, pageID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &page, nil
}

// PurgeDeletedPages hard-deletes pages soft-deleted before deletedBefore, in
// batches. It is a background job and applies no caller visibility.
func (r *WikiRepository) PurgeDeletedPages(ctx context.Context, deletedBefore time.Time, batchSize int) (int, error) {
	if batchSize <= 0 {
		batchSize = 500
	}
	total := 0
	for {
		result, err := r.db.ExecContext(ctx, `DELETE FROM wiki_page_records WHERE id IN (
			SELECT id FROM wiki_page_records WHERE deleted_at < $1 ORDER BY deleted_at LIMIT $2 FOR UPDATE SKIP LOCKED)`,
			deletedBefore, batchSize)
		if err != nil {
			return total, err
		}
		n, _ := result.RowsAffected()
		total += int(n)
		if int(n) < batchSize {
			return total, nil
		}
	}
}
