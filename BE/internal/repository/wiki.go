package repository

import (
	"context"
	"database/sql"
	"errors"

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
// expectedVersion, then increments it. A stale version reports
// ErrWikiVersionConflict; an invisible or missing page ErrWikiPageNotFound.
func (r *WikiRepository) Update(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, page *domain.WikiPage, expectedVersion int) error {
	if !scope.Valid() {
		return ErrWikiPageNotFound
	}
	query := `UPDATE wiki_pages SET title = $4, content = $5, updated_by = $6, version = wiki_pages.version + 1, updated_at = NOW()
		WHERE ` + wikiPageInScope(ctx, scope) + ` AND wiki_pages.id = $3 AND wiki_pages.version = $7
		RETURNING version, updated_at, (SELECT wiki_editor.name FROM users wiki_editor WHERE wiki_editor.id = wiki_pages.updated_by)`
	err := r.db.QueryRowContext(ctx, query, workspaceID, scope.ID, page.ID, page.Title, page.Content, page.UpdatedBy, expectedVersion).
		Scan(&page.Version, &page.UpdatedAt, &page.UpdatedByName)
	if errors.Is(err, sql.ErrNoRows) {
		if _, getErr := r.Get(ctx, workspaceID, scope, page.ID); getErr != nil {
			return getErr
		}
		return ErrWikiVersionConflict
	}
	return err
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
