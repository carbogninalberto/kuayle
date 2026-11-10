package repository

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"

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
	// ErrWikiRegistryDisabled means the scope's registry is turned off.
	ErrWikiRegistryDisabled = errors.New("wiki registry is disabled")
	// ErrWikiRegistryFull means today's registry page has no room left.
	ErrWikiRegistryFull = errors.New("wiki registry page is full")
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
	wiki_editor.name AS updated_by_name, wiki_pages.registry_date, wiki_pages.created_at, wiki_pages.updated_at`

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
		wiki_editor.name AS updated_by_name, wiki_pages.registry_date, wiki_pages.created_at, wiki_pages.updated_at
		FROM wiki_pages` + wikiEditorJoin + ` WHERE ` + wikiPageInScope(ctx, scope) + `
		ORDER BY wiki_pages.updated_at DESC, wiki_pages.id`
	err := r.db.SelectContext(ctx, &pages, query, workspaceID, scope.ID)
	return pages, err
}

// Search returns the scope's pages whose title or content contains the text
// (case-insensitive), without their content, most recently updated first.
func (r *WikiRepository) Search(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, text string) ([]domain.WikiPage, error) {
	if err := r.RequireScope(ctx, workspaceID, scope); err != nil {
		return nil, err
	}
	pages := []domain.WikiPage{}
	query := `SELECT wiki_pages.id, wiki_pages.workspace_id, wiki_pages.team_id, wiki_pages.project_id,
		wiki_pages.title, '' AS content, wiki_pages.version, wiki_pages.created_by, wiki_pages.updated_by,
		wiki_editor.name AS updated_by_name, wiki_pages.registry_date, wiki_pages.created_at, wiki_pages.updated_at
		FROM wiki_pages` + wikiEditorJoin + ` WHERE ` + wikiPageInScope(ctx, scope) + `
		AND (wiki_pages.title ILIKE $3 OR wiki_pages.content ILIKE $3)
		ORDER BY wiki_pages.updated_at DESC, wiki_pages.id`
	pattern := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(text) + "%"
	err := r.db.SelectContext(ctx, &pages, query, workspaceID, scope.ID, pattern)
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

// wikiSettingsScope returns the settings column and the ON CONFLICT target
// of a scope kind; both are trusted SQL.
func wikiSettingsScope(kind domain.WikiScopeKind) (column, conflict string) {
	if kind == domain.WikiScopeProject {
		return "wiki_settings.project_id", "(project_id) WHERE project_id IS NOT NULL"
	}
	return "wiki_settings.team_id", "(team_id) WHERE team_id IS NOT NULL"
}

// RegistryEnabled reports whether the visible scope's registry is on. A scope
// without settings has it off.
func (r *WikiRepository) RegistryEnabled(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope) (bool, error) {
	if err := r.RequireScope(ctx, workspaceID, scope); err != nil {
		return false, err
	}
	return registryEnabled(ctx, r.db, workspaceID, scope, "")
}

func registryEnabled(ctx context.Context, db sqlx.QueryerContext, workspaceID uuid.UUID, scope domain.WikiScope, lock string) (bool, error) {
	column, _ := wikiSettingsScope(scope.Kind)
	var enabled bool
	err := sqlx.GetContext(ctx, db, &enabled, `SELECT wiki_settings.registry_enabled FROM wiki_settings
		WHERE wiki_settings.workspace_id = $1 AND `+column+` = $2`+lock, workspaceID, scope.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return enabled, err
}

// SetRegistryEnabled turns the visible scope's registry on or off; the
// visibility check and the write are one statement.
func (r *WikiRepository) SetRegistryEnabled(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, userID uuid.UUID, enabled bool) error {
	if !scope.Valid() {
		return ErrWikiScopeNotFound
	}
	_, conflict := wikiSettingsScope(scope.Kind)
	var teamID, projectID *uuid.UUID
	if scope.Kind == domain.WikiScopeTeam {
		teamID = &scope.ID
	} else {
		projectID = &scope.ID
	}
	result, err := r.db.ExecContext(ctx, `INSERT INTO wiki_settings (workspace_id, team_id, project_id, registry_enabled, updated_by)
		SELECT $1, $2, $3, $4, $5 WHERE `+wikiScopeVisible(ctx, scope.Kind, uuidSQL(scope.ID), "CAST($1 AS uuid)")+`
		ON CONFLICT `+conflict+` DO UPDATE SET registry_enabled = EXCLUDED.registry_enabled, updated_by = EXCLUDED.updated_by, updated_at = NOW()
		WHERE wiki_settings.workspace_id = EXCLUDED.workspace_id`,
		workspaceID, teamID, projectID, enabled, userID)
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return err
	} else if affected == 0 {
		return ErrWikiScopeNotFound
	}
	return nil
}

// AppendRegistry appends entry (sanitized editor HTML) to the scope's
// registry page for day, creating the page when it is the day's first entry.
// It requires the scope to be visible and its registry on, holds the settings
// and page rows locked until done, and records the change as a version like
// any other save. created reports whether the page was new.
func (r *WikiRepository) AppendRegistry(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, userID uuid.UUID, day time.Time, entry string) (*domain.WikiPage, bool, error) {
	if !scope.Valid() {
		return nil, false, ErrWikiScopeNotFound
	}
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback() }()
	page, created, err := appendRegistryTx(ctx, tx, workspaceID, scope, userID, day, entry)
	if err != nil {
		return nil, false, err
	}
	return page, created, tx.Commit()
}

// appendRegistryTx is AppendRegistry inside the caller's transaction. Its
// refusals (invisible scope, registry off, full page) happen before any
// write, so the transaction stays usable after them.
func appendRegistryTx(ctx context.Context, tx *sqlx.Tx, workspaceID uuid.UUID, scope domain.WikiScope, userID uuid.UUID, day time.Time, entry string) (page *domain.WikiPage, created bool, err error) {
	if !scope.Valid() {
		return nil, false, ErrWikiScopeNotFound
	}
	if err := requireVisible(ctx, tx, wikiScopeVisible(ctx, scope.Kind, uuidSQL(scope.ID), uuidSQL(workspaceID))); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, ErrWikiScopeNotFound
		}
		return nil, false, err
	}
	// Locking the settings row orders appends against turning the registry off.
	enabled, err := registryEnabled(ctx, tx, workspaceID, scope, " FOR SHARE")
	if err != nil {
		return nil, false, err
	}
	if !enabled {
		return nil, false, ErrWikiRegistryDisabled
	}

	date := day.UTC().Format(time.DateOnly)
	var teamID, projectID *uuid.UUID
	conflict := "(team_id, registry_date) WHERE team_id IS NOT NULL AND registry_date IS NOT NULL"
	if scope.Kind == domain.WikiScopeTeam {
		teamID = &scope.ID
	} else {
		projectID = &scope.ID
		conflict = "(project_id, registry_date) WHERE project_id IS NOT NULL AND registry_date IS NOT NULL"
	}
	var newID uuid.UUID
	err = tx.GetContext(ctx, &newID, `INSERT INTO wiki_pages (id, workspace_id, team_id, project_id, title, content, created_by, updated_by, registry_date)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $7, CAST($8 AS date))
		ON CONFLICT `+conflict+` DO NOTHING RETURNING id`,
		uuid.New(), workspaceID, teamID, projectID, domain.WikiRegistryTitle(day), entry, userID, date)
	switch {
	case err == nil:
		created = true
	case errors.Is(err, sql.ErrNoRows):
		var before struct {
			ID      uuid.UUID `db:"id"`
			Title   string    `db:"title"`
			Content string    `db:"content"`
		}
		err = tx.GetContext(ctx, &before, `SELECT wiki_pages.id, wiki_pages.title, wiki_pages.content FROM wiki_pages
			WHERE `+wikiPageInScope(ctx, scope)+` AND wiki_pages.registry_date = CAST($3 AS date) FOR UPDATE OF wiki_pages`,
			workspaceID, scope.ID, date)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, ErrWikiScopeNotFound
		}
		if err != nil {
			return nil, false, err
		}
		if len(before.Content)+len(entry) > domain.WikiContentMaxBytes {
			return nil, false, ErrWikiRegistryFull
		}
		newID = before.ID
		changed := &domain.WikiPage{ID: before.ID, Title: before.Title, Content: before.Content + entry, UpdatedBy: &userID}
		err = tx.QueryRowContext(ctx, `UPDATE wiki_pages SET content = $2, updated_by = $3, version = version + 1, updated_at = NOW()
			WHERE id = $1 RETURNING version`, changed.ID, changed.Content, userID).Scan(&changed.Version)
		if err != nil {
			return nil, false, err
		}
		if err := recordWikiRevision(ctx, tx, changed, before.Title, before.Content); err != nil {
			return nil, false, err
		}
	default:
		return nil, false, err
	}
	page = &domain.WikiPage{}
	err = tx.GetContext(ctx, page, `SELECT `+wikiPageColumns+` FROM wiki_pages`+wikiEditorJoin+` WHERE `+wikiPageInScope(ctx, scope)+` AND wiki_pages.id = $3`,
		workspaceID, scope.ID, newID)
	if err != nil {
		return nil, false, err
	}
	return page, created, nil
}

// AppendIssueRegistry appends text, prefixed with the issue's identifier, to
// today's registry of the issue's project when that registry is on, and
// otherwise of its team. The caller must see the issue; registries that are
// off or invisible are skipped, and ErrWikiRegistryDisabled means none took it.
func (r *WikiRepository) AppendIssueRegistry(ctx context.Context, workspaceID, issueID, userID uuid.UUID, now time.Time, text string) (*domain.WikiPage, bool, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback() }()
	page, created, err := appendIssueRegistryTx(ctx, tx, workspaceID, issueID, userID, now, text)
	if err != nil {
		return nil, false, err
	}
	return page, created, tx.Commit()
}

func appendIssueRegistryTx(ctx context.Context, tx *sqlx.Tx, workspaceID, issueID, userID uuid.UUID, now time.Time, text string) (*domain.WikiPage, bool, error) {
	var issue struct {
		Identifier string     `db:"identifier_text"`
		TeamID     uuid.UUID  `db:"team_id"`
		ProjectID  *uuid.UUID `db:"project_id"`
	}
	err := tx.GetContext(ctx, &issue, `SELECT issues.identifier_text, issues.team_id, issues.project_id FROM issues
		WHERE issues.id = $1 AND issues.workspace_id = $2 AND `+teamVisible(ctx, "issues.team_id"), issueID, workspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, ErrWikiScopeNotFound
	}
	if err != nil {
		return nil, false, err
	}
	entry, err := domain.WikiRegistryEntryHTML(issue.Identifier+": "+text, now)
	if err != nil {
		return nil, false, err
	}
	scopes := []domain.WikiScope{domain.TeamWiki(issue.TeamID)}
	if issue.ProjectID != nil {
		scopes = append([]domain.WikiScope{domain.ProjectWiki(*issue.ProjectID)}, scopes...)
	}
	for _, scope := range scopes {
		page, created, err := appendRegistryTx(ctx, tx, workspaceID, scope, userID, now, entry)
		if errors.Is(err, ErrWikiRegistryDisabled) || errors.Is(err, ErrWikiScopeNotFound) {
			continue
		}
		return page, created, err
	}
	return nil, false, ErrWikiRegistryDisabled
}

// RegistryDays returns up to limit registry pages of a visible scope, newest
// day first, before the given day (exclusive) when one is set.
func (r *WikiRepository) RegistryDays(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, before *time.Time, limit int) ([]domain.WikiPage, error) {
	if err := r.RequireScope(ctx, workspaceID, scope); err != nil {
		return nil, err
	}
	var cursor *string
	if before != nil {
		day := before.UTC().Format(time.DateOnly)
		cursor = &day
	}
	pages := []domain.WikiPage{}
	err := r.db.SelectContext(ctx, &pages, `SELECT `+wikiPageColumns+` FROM wiki_pages`+wikiEditorJoin+`
		WHERE `+wikiPageInScope(ctx, scope)+` AND wiki_pages.registry_date IS NOT NULL
		AND (CAST($3 AS date) IS NULL OR wiki_pages.registry_date < CAST($3 AS date))
		ORDER BY wiki_pages.registry_date DESC LIMIT $4`, workspaceID, scope.ID, cursor, limit)
	return pages, err
}
