package repository

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/lib/pq"
)

// ErrWikiReferenceTarget covers an issue or page that is missing, in another
// workspace or invisible to the caller.
var ErrWikiReferenceTarget = errors.New("issue or wiki page not found")

// wikiPageVisible matches pages of any team or project the caller can see
// now, through the same scope predicate the wiki itself uses. It expects the
// page table to be named wiki_pages.
func wikiPageVisible(ctx context.Context) string {
	return "((wiki_pages.team_id IS NOT NULL AND " + wikiScopeVisible(ctx, domain.WikiScopeTeam, "wiki_pages.team_id", "wiki_pages.workspace_id") +
		") OR (wiki_pages.project_id IS NOT NULL AND " + wikiScopeVisible(ctx, domain.WikiScopeProject, "wiki_pages.project_id", "wiki_pages.workspace_id") + "))"
}

const wikiReferenceColumns = `wiki_pages.id, wiki_pages.team_id, wiki_pages.project_id, wiki_pages.title,
	COALESCE((SELECT ref_team.name FROM teams ref_team WHERE ref_team.id = wiki_pages.team_id),
		(SELECT ref_project.name FROM projects ref_project WHERE ref_project.id = wiki_pages.project_id), '') AS scope_name`

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// SearchReferences suggests pages to mention: pages of every team and
// project of the workspace the caller can see whose title contains query,
// most recently updated first. Registry days are not suggested.
func (r *WikiRepository) SearchReferences(ctx context.Context, workspaceID uuid.UUID, query string, limit int) ([]domain.WikiReference, error) {
	pages := []domain.WikiReference{}
	err := r.db.SelectContext(ctx, &pages, `SELECT `+wikiReferenceColumns+` FROM wiki_pages
		WHERE wiki_pages.workspace_id = $1 AND wiki_pages.registry_date IS NULL AND `+wikiPageVisible(ctx)+`
		AND ($2 = '' OR wiki_pages.title ILIKE '%' || $2 || '%' ESCAPE '\')
		ORDER BY wiki_pages.updated_at DESC, wiki_pages.id LIMIT $3`, workspaceID, likeEscaper.Replace(query), limit)
	return pages, err
}

// ResolveReferences returns those of the given pages the caller can see in
// the workspace. Missing, foreign and invisible pages are simply absent, so
// callers cannot tell them apart.
func (r *WikiRepository) ResolveReferences(ctx context.Context, workspaceID uuid.UUID, pageIDs []uuid.UUID) ([]domain.WikiReference, error) {
	pages := []domain.WikiReference{}
	if len(pageIDs) == 0 {
		return pages, nil
	}
	ids := make([]string, len(pageIDs))
	for i, id := range pageIDs {
		ids[i] = id.String()
	}
	err := r.db.SelectContext(ctx, &pages, `SELECT `+wikiReferenceColumns+` FROM wiki_pages
		WHERE wiki_pages.workspace_id = $1 AND wiki_pages.id = ANY($2::uuid[]) AND `+wikiPageVisible(ctx), workspaceID, pq.Array(ids))
	return pages, err
}

// Backlinks lists where a visible page is referenced: issues that mention it
// in their description or a comment or have it attached, and other pages
// that mention it. Only issues and pages the caller can see are returned;
// without withIssues (a caller that may not read issues) only the pages.
func (r *WikiRepository) Backlinks(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, pageID uuid.UUID, withIssues bool) (*domain.WikiBacklinks, error) {
	if !scope.Valid() {
		return nil, ErrWikiPageNotFound
	}
	if err := requireVisible(ctx, r.db, "EXISTS (SELECT 1 FROM wiki_pages WHERE wiki_pages.id = "+uuidSQL(pageID)+
		" AND wiki_pages.workspace_id = "+uuidSQL(workspaceID)+" AND "+wikiScopeColumn(scope.Kind)+" = "+uuidSQL(scope.ID)+
		" AND "+wikiScopeVisible(ctx, scope.Kind, wikiScopeColumn(scope.Kind), "wiki_pages.workspace_id")+")"); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrWikiPageNotFound
		}
		return nil, err
	}
	// Mentions in content are indexed in wiki_page_mentions by database
	// triggers (migration 140), so the referencing issues and pages are found
	// by page ID first and only those few rows are checked for visibility.
	limit := strconv.Itoa(domain.WikiBacklinkLimit)
	links := &domain.WikiBacklinks{Issues: []domain.WikiBacklinkIssue{}, Pages: []domain.WikiReference{}}
	var err error
	if withIssues {
		err = r.db.SelectContext(ctx, &links.Issues, `WITH referencing AS MATERIALIZED (
			SELECT mention.issue_id FROM wiki_page_mentions mention WHERE mention.page_id = $2 AND mention.issue_id IS NOT NULL
			UNION SELECT backlink_comment.issue_id FROM wiki_page_mentions mention
				JOIN comments backlink_comment ON backlink_comment.id = mention.comment_id WHERE mention.page_id = $2
			UNION SELECT attached.issue_id FROM issue_wiki_references attached WHERE attached.page_id = $2
		)
		SELECT backlink_issue.id, backlink_issue.identifier_text, backlink_issue.title
		FROM referencing JOIN issues backlink_issue ON backlink_issue.id = referencing.issue_id
		WHERE backlink_issue.workspace_id = $1 AND `+teamVisible(ctx, "backlink_issue.team_id")+`
		ORDER BY backlink_issue.updated_at DESC, backlink_issue.id LIMIT `+limit, workspaceID, pageID)
		if err != nil {
			return nil, err
		}
	}
	err = r.db.SelectContext(ctx, &links.Pages, `WITH referencing AS MATERIALIZED (
			SELECT mention.source_page_id FROM wiki_page_mentions mention
			WHERE mention.page_id = $2 AND mention.source_page_id IS NOT NULL AND mention.source_page_id <> $2
		)
		SELECT `+wikiReferenceColumns+` FROM referencing JOIN wiki_pages ON wiki_pages.id = referencing.source_page_id
		WHERE wiki_pages.workspace_id = $1 AND `+wikiPageVisible(ctx)+`
		ORDER BY wiki_pages.updated_at DESC, wiki_pages.id LIMIT `+limit, workspaceID, pageID)
	if err != nil {
		return nil, err
	}
	return links, nil
}

// IssueReferences returns the IDs of the pages attached to a visible issue,
// oldest first. The IDs alone say nothing about a page; resolve them with
// ResolveReferences to learn which ones the caller can see.
func (r *WikiRepository) IssueReferences(ctx context.Context, workspaceID, issueID uuid.UUID) ([]uuid.UUID, error) {
	ids := []uuid.UUID{}
	err := r.db.SelectContext(ctx, &ids, `SELECT attached.page_id FROM issue_wiki_references attached
		WHERE attached.issue_id = $1 AND attached.workspace_id = $2 AND `+issueVisible(ctx, "attached.issue_id")+`
		ORDER BY attached.created_at, attached.page_id`, issueID, workspaceID)
	return ids, err
}

// AttachIssueReference attaches a page to an issue. Both must be in the
// workspace and visible to the caller; the checks and the insert are one
// statement. Attaching twice is a no-op.
func (r *WikiRepository) AttachIssueReference(ctx context.Context, workspaceID, issueID, pageID, userID uuid.UUID) error {
	var found bool
	err := r.db.GetContext(ctx, &found, `WITH target AS (
			SELECT target_issue.id AS issue_id, wiki_pages.id AS page_id FROM issues target_issue, wiki_pages
			WHERE target_issue.id = $1 AND target_issue.workspace_id = $3 AND `+teamVisible(ctx, "target_issue.team_id")+`
			AND wiki_pages.id = $2 AND wiki_pages.workspace_id = $3 AND `+wikiPageVisible(ctx)+`
		), attached AS (
			INSERT INTO issue_wiki_references (issue_id, page_id, workspace_id, created_by)
			SELECT target.issue_id, target.page_id, CAST($3 AS uuid), CAST($4 AS uuid) FROM target
			ON CONFLICT (issue_id, page_id) DO NOTHING
		)
		SELECT EXISTS (SELECT 1 FROM target)`, issueID, pageID, workspaceID, userID)
	if err != nil {
		return err
	}
	if !found {
		return ErrWikiReferenceTarget
	}
	return nil
}

// DetachIssueReference removes an attached page from a visible issue. The
// page itself need not be visible: anyone who may edit the issue can remove
// a reference they cannot open.
func (r *WikiRepository) DetachIssueReference(ctx context.Context, workspaceID, issueID, pageID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM issue_wiki_references attached
		WHERE attached.issue_id = $1 AND attached.page_id = $2 AND attached.workspace_id = $3 AND `+issueVisible(ctx, "attached.issue_id"),
		issueID, pageID, workspaceID)
	return err
}
