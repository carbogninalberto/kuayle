package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// These predicates accept only developer-owned column expressions. UUID literals
// are generated from typed UUIDs, never request strings; this keeps predicates
// usable with both positional and sqlx named queries without shifting bindings.
// Apply predicates before pagination, grouping and aggregation, not to results.
func workspaceVisible(ctx context.Context, workspaceColumn string) string {
	access, ok := domain.AccessFromContext(ctx)
	if !ok {
		// Public endpoints and uncredentialed jobs have public-only team access.
		// Workspace-level public-share authorization remains the caller's job.
		return "TRUE"
	}
	if access.UserID == uuid.Nil {
		return "FALSE"
	}
	scope := ""
	if access.WorkspaceID != uuid.Nil {
		scope = workspaceColumn + " = '" + access.WorkspaceID.String() + "' AND "
	}
	return "(" + scope + "kuayle_workspace_access(" + workspaceColumn + "," + uuidSQL(access.UserID) + ",FALSE))"
}

func teamVisible(ctx context.Context, teamColumn string) string {
	access, ok := domain.AccessFromContext(ctx)
	actor, scope := "CAST(NULL AS uuid)", "CAST(NULL AS uuid)"
	if ok {
		if access.UserID == uuid.Nil {
			return "FALSE"
		}
		actor = uuidSQL(access.UserID)
		if access.WorkspaceID != uuid.Nil {
			scope = uuidSQL(access.WorkspaceID)
		}
	}
	// The function takes a fresh snapshot after any statement-level lock wait.
	return "kuayle_team_access(" + teamColumn + "," + actor + "," + scope + ")"
}

func projectVisible(ctx context.Context, workspaceColumn, teamColumn string) string {
	return "(" + workspaceVisible(ctx, workspaceColumn) + " AND (" + teamColumn + " IS NULL OR " + teamVisible(ctx, teamColumn) + "))"
}

func issueVisible(ctx context.Context, issueColumn string) string {
	return "EXISTS (SELECT 1 FROM issues access_issue WHERE access_issue.id = " + issueColumn + " AND " + teamVisible(ctx, "access_issue.team_id") + ")"
}

func uuidSQL(id uuid.UUID) string { return "CAST('" + id.String() + "' AS uuid)" }

// requireVisible is also used inside transactions before replacing junction
// rows, so denied requests cannot delete labels or assignees as a side effect.
func requireVisible(ctx context.Context, db sqlx.QueryerContext, predicate string) error {
	var allowed int
	return sqlx.GetContext(ctx, db, &allowed, "SELECT 1 WHERE "+predicate)
}

func workspaceAdmin(ctx context.Context, workspace string) string {
	access, ok := domain.AccessFromContext(ctx)
	if !ok || access.UserID == uuid.Nil {
		return "FALSE"
	}
	return "(" + workspaceVisible(ctx, workspace) + " AND kuayle_workspace_access(" + workspace + "," + uuidSQL(access.UserID) + ",TRUE))"
}
