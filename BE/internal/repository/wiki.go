package repository

import (
	"context"
	"database/sql"
	"errors"
	"strconv"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

var (
	// ErrWikiScopeNotFound covers a team or project that does not exist, lives
	// in another workspace or is invisible to the caller; they are deliberately
	// indistinguishable.
	ErrWikiScopeNotFound = errors.New("wiki scope not found")
	ErrWikiPageNotFound  = errors.New("wiki page not found")
	// ErrWikiVersionConflict means the page changed since the caller read it.
	ErrWikiVersionConflict = errors.New("wiki page was changed by someone else")
)

type WikiRepository struct {
	db *sqlx.DB
}

func NewWikiRepository(db *sqlx.DB) *WikiRepository {
	return &WikiRepository{db: db}
}

// wikiScopeVisible requires the scope to exist in the workspace and be visible
// to the caller now. A project wiki follows the project's own visibility, so
// moving a project to a private team hides its wiki too. Arguments are trusted
// SQL expressions, never request strings.
func wikiScopeVisible(ctx context.Context, kind domain.WikiScopeKind, scope, workspace string) string {
	switch kind {
	case domain.WikiScopeTeam:
		return "EXISTS (SELECT 1 FROM teams wiki_team WHERE wiki_team.id = " + scope + " AND wiki_team.workspace_id = " + workspace + " AND " + teamVisible(ctx, "wiki_team.id") + ")"
	case domain.WikiScopeProject:
		return "EXISTS (SELECT 1 FROM projects wiki_project WHERE wiki_project.id = " + scope + " AND wiki_project.workspace_id = " + workspace + " AND " + projectVisible(ctx, "wiki_project.workspace_id", "wiki_project.team_id") + ")"
	}
	return "FALSE"
}

// wikiScopeColumn is the page column that holds the scope's ID.
func wikiScopeColumn(kind domain.WikiScopeKind) string {
	if kind == domain.WikiScopeProject {
		return "wiki_pages.project_id"
	}
	return "wiki_pages.team_id"
}

// wikiPageInScope matches pages of exactly this scope and workspace, and only
// while the caller may see that scope. $1 is the workspace and $2 the scope ID.
func wikiPageInScope(ctx context.Context, scope domain.WikiScope) string {
	return "wiki_pages.workspace_id = $1 AND " + wikiScopeColumn(scope.Kind) + " = $2 AND " +
		wikiScopeVisible(ctx, scope.Kind, wikiScopeColumn(scope.Kind), "wiki_pages.workspace_id")
}

const wikiPageColumns = `wiki_pages.id, wiki_pages.workspace_id, wiki_pages.team_id, wiki_pages.project_id,
	wiki_pages.title, wiki_pages.content, wiki_pages.version, wiki_pages.created_by, wiki_pages.updated_by,
	wiki_editor.name AS updated_by_name, wiki_pages.created_at, wiki_pages.updated_at`

const wikiEditorJoin = ` LEFT JOIN users wiki_editor ON wiki_editor.id = wiki_pages.updated_by`

// RequireScope reports ErrWikiScopeNotFound unless the caller can see the scope.
func (r *WikiRepository) RequireScope(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope) error {
	if !scope.Valid() {
		return ErrWikiScopeNotFound
	}
	err := requireVisible(ctx, r.db, wikiScopeVisible(ctx, scope.Kind, uuidSQL(scope.ID), uuidSQL(workspaceID)))
	if errors.Is(err, sql.ErrNoRows) {
		return ErrWikiScopeNotFound
	}
	return err
}

// List returns the scope's pages without their content, most recently
// updated first.
func (r *WikiRepository) List(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope) ([]domain.WikiPage, error) {
	if err := r.RequireScope(ctx, workspaceID, scope); err != nil {
		return nil, err
	}
	pages := []domain.WikiPage{}
	query := `SELECT wiki_pages.id, wiki_pages.workspace_id, wiki_pages.team_id, wiki_pages.project_id,
		wiki_pages.title, '' AS content, wiki_pages.version, wiki_pages.created_by, wiki_pages.updated_by,
		wiki_editor.name AS updated_by_name, wiki_pages.created_at, wiki_pages.updated_at
		FROM wiki_pages` + wikiEditorJoin + ` WHERE ` + wikiPageInScope(ctx, scope) + `
		ORDER BY wiki_pages.updated_at DESC, wiki_pages.id`
	err := r.db.SelectContext(ctx, &pages, query, workspaceID, scope.ID)
	return pages, err
}

func (r *WikiRepository) Get(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, pageID uuid.UUID) (*domain.WikiPage, error) {
	if !scope.Valid() {
		return nil, ErrWikiPageNotFound
	}
	var page domain.WikiPage
	query := `SELECT ` + wikiPageColumns + ` FROM wiki_pages` + wikiEditorJoin + ` WHERE ` + wikiPageInScope(ctx, scope) + ` AND wiki_pages.id = $3`
	err := r.db.GetContext(ctx, &page, query, workspaceID, scope.ID, pageID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrWikiPageNotFound
	}
	if err != nil {
		return nil, err
	}
	return &page, nil
}

// Create inserts the page only if its scope is visible to the caller in the
// page's workspace; the check and the insert are one statement.
func (r *WikiRepository) Create(ctx context.Context, page *domain.WikiPage) error {
	scope := page.Scope()
	if !scope.Valid() {
		return ErrWikiScopeNotFound
	}
	query := `INSERT INTO wiki_pages (id, workspace_id, team_id, project_id, title, content, created_by, updated_by)
		SELECT $1, $2, $3, $4, $5, $6, $7, $7
		WHERE ` + wikiScopeVisible(ctx, scope.Kind, uuidSQL(scope.ID), "CAST($2 AS uuid)") + `
		RETURNING version, created_at, updated_at, (SELECT wiki_editor.name FROM users wiki_editor WHERE wiki_editor.id = wiki_pages.updated_by)`
	err := r.db.QueryRowContext(ctx, query, page.ID, page.WorkspaceID, page.TeamID, page.ProjectID, page.Title, page.Content, page.CreatedBy).
		Scan(&page.Version, &page.CreatedAt, &page.UpdatedAt, &page.UpdatedByName)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrWikiScopeNotFound
	}
	if err != nil {
		return err
	}
	page.UpdatedBy = page.CreatedBy
	return nil
}

// Update writes title and content when the stored version still equals
// expectedVersion, then increments it and records the change as a revision.
// A stale version reports ErrWikiVersionConflict; an invisible or missing page
// ErrWikiPageNotFound. The page row stays locked until the revision is
// written, so concurrent saves cannot interleave their history.
func (r *WikiRepository) Update(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, page *domain.WikiPage, expectedVersion int) error {
	if !scope.Valid() {
		return ErrWikiPageNotFound
	}
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var before struct {
		Title   string `db:"title"`
		Content string `db:"content"`
		Version int    `db:"version"`
	}
	err = tx.GetContext(ctx, &before, `SELECT wiki_pages.title, wiki_pages.content, wiki_pages.version FROM wiki_pages
		WHERE `+wikiPageInScope(ctx, scope)+` AND wiki_pages.id = $3 FOR UPDATE OF wiki_pages`, workspaceID, scope.ID, page.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrWikiPageNotFound
	}
	if err != nil {
		return err
	}
	if before.Version != expectedVersion {
		return ErrWikiVersionConflict
	}

	query := `UPDATE wiki_pages SET title = $4, content = $5, updated_by = $6, version = wiki_pages.version + 1, updated_at = NOW()
		WHERE ` + wikiPageInScope(ctx, scope) + ` AND wiki_pages.id = $3
		RETURNING version, updated_at, (SELECT wiki_editor.name FROM users wiki_editor WHERE wiki_editor.id = wiki_pages.updated_by)`
	err = tx.QueryRowContext(ctx, query, workspaceID, scope.ID, page.ID, page.Title, page.Content, page.UpdatedBy).
		Scan(&page.Version, &page.UpdatedAt, &page.UpdatedByName)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrWikiPageNotFound
	}
	if err != nil {
		return err
	}
	if before.Title != page.Title || before.Content != page.Content {
		if err := recordWikiRevision(ctx, tx, page, before.Title, before.Content); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// recordWikiRevision extends the page's latest revision when the same person
// made it within WikiRevisionMergeWindow, and otherwise starts a new one.
func recordWikiRevision(ctx context.Context, tx *sqlx.Tx, page *domain.WikiPage, oldTitle, oldContent string) error {
	result, err := tx.ExecContext(ctx, `UPDATE wiki_page_versions SET new_title = $2, new_content = $3, version = $4, updated_at = NOW()
		WHERE id = (SELECT latest.id FROM wiki_page_versions latest WHERE latest.page_id = $1 ORDER BY latest.updated_at DESC, latest.id DESC LIMIT 1)
		AND edited_by = $5 AND updated_at > NOW() - make_interval(secs => $6)`,
		page.ID, page.Title, page.Content, page.Version, page.UpdatedBy, domain.WikiRevisionMergeWindow.Seconds())
	if err != nil {
		return err
	}
	if merged, err := result.RowsAffected(); err != nil || merged > 0 {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO wiki_page_versions (page_id, version, old_title, new_title, old_content, new_content, edited_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		page.ID, page.Version, oldTitle, page.Title, oldContent, page.Content, page.UpdatedBy)
	return err
}

// wikiRevisionLimit caps how much history one request returns.
const wikiRevisionLimit = 100

// ListRevisions returns a visible page's most recent revisions, newest first.
func (r *WikiRepository) ListRevisions(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, pageID uuid.UUID) ([]domain.WikiRevision, error) {
	if _, err := r.Get(ctx, workspaceID, scope, pageID); err != nil {
		return nil, err
	}
	revisions := []domain.WikiRevision{}
	query := `SELECT wiki_revision.id, wiki_revision.page_id, wiki_revision.version, wiki_revision.old_title, wiki_revision.new_title,
		wiki_revision.old_content, wiki_revision.new_content, wiki_revision.edited_by, wiki_editor.name AS edited_by_name,
		wiki_revision.created_at, wiki_revision.updated_at
		FROM wiki_page_versions wiki_revision
		JOIN wiki_pages ON wiki_pages.id = wiki_revision.page_id
		LEFT JOIN users wiki_editor ON wiki_editor.id = wiki_revision.edited_by
		WHERE ` + wikiPageInScope(ctx, scope) + ` AND wiki_pages.id = $3
		ORDER BY wiki_revision.updated_at DESC, wiki_revision.id DESC LIMIT ` + strconv.Itoa(wikiRevisionLimit)
	err := r.db.SelectContext(ctx, &revisions, query, workspaceID, scope.ID, pageID)
	return revisions, err
}

func (r *WikiRepository) Delete(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, pageID uuid.UUID) error {
	if !scope.Valid() {
		return ErrWikiPageNotFound
	}
	result, err := r.db.ExecContext(ctx, `DELETE FROM wiki_pages WHERE `+wikiPageInScope(ctx, scope)+` AND wiki_pages.id = $3`, workspaceID, scope.ID, pageID)
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return err
	} else if affected == 0 {
		return ErrWikiPageNotFound
	}
	return nil
}
