package repository

import (
	"context"
	"database/sql"
	"errors"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"strings"

	"github.com/kuayle/kuayle-backend/internal/domain"
)

// Public teams retain cross-team relationships. If either endpoint is private,
// both must belong to the same team. NULL denotes a workspace-public resource.
// The arguments are trusted SQL expressions, never user-provided identifiers.
func compatibleTeams(left, right string) string {
	return "(" + left + " IS NOT DISTINCT FROM " + right + " OR NOT EXISTS (SELECT 1 FROM teams boundary_team WHERE boundary_team.is_private AND boundary_team.id IN (" + left + ", " + right + ")))"
}

func issueReferencesVisible(ctx context.Context, issue *domain.Issue) string {
	team := uuidSQL(issue.TeamID)
	workspace := uuidSQL(issue.WorkspaceID)
	checks := []string{
		teamVisible(ctx, team),
		"EXISTS (SELECT 1 FROM teams reference_team WHERE reference_team.id = " + team + " AND reference_team.workspace_id = " + workspace + ")",
	}
	if issue.ProjectID != nil {
		checks = append(checks, "EXISTS (SELECT 1 FROM projects reference_project WHERE reference_project.id = "+uuidSQL(*issue.ProjectID)+" AND reference_project.workspace_id = "+workspace+" AND "+projectVisible(ctx, "reference_project.workspace_id", "reference_project.team_id")+" AND "+compatibleTeams(team, "reference_project.team_id")+")")
	}
	if issue.ParentID != nil {
		checks = append(checks, "EXISTS (SELECT 1 FROM issues reference_parent WHERE reference_parent.id = "+uuidSQL(*issue.ParentID)+" AND reference_parent.workspace_id = "+workspace+" AND "+teamVisible(ctx, "reference_parent.team_id")+" AND "+compatibleTeams(team, "reference_parent.team_id")+")")
	}
	if issue.CycleID != nil {
		checks = append(checks, "EXISTS (SELECT 1 FROM cycles reference_cycle JOIN teams reference_cycle_team ON reference_cycle_team.id = reference_cycle.team_id WHERE reference_cycle.id = "+uuidSQL(*issue.CycleID)+" AND reference_cycle_team.workspace_id = "+workspace+" AND "+teamVisible(ctx, "reference_cycle.team_id")+" AND "+compatibleTeams(team, "reference_cycle.team_id")+")")
	}
	if issue.StatusID != nil {
		checks = append(checks, "EXISTS (SELECT 1 FROM team_statuses reference_status WHERE reference_status.id = "+uuidSQL(*issue.StatusID)+" AND reference_status.team_id = "+team+")")
	}
	return "(" + strings.Join(checks, " AND ") + ")"
}

// ValidateIssueReferences runs before service history/notification side effects.
// Create and Update also use the same predicate in the actual mutation statement.
func (r *IssueRepository) ValidateIssueReferences(ctx context.Context, issue *domain.Issue) error {
	return requireVisible(ctx, r.db, issueReferencesVisible(ctx, issue))
}

var ErrPrivateWorkspaceOperation = errors.New("this operation is unavailable after private teams are enabled")

// privacyDenial reports an empty guarded write as the sticky privacy-mode
// restriction when that is its cause, so clients can explain the limitation.
func privacyDenial(ctx context.Context, db sqlx.QueryerContext, workspaceID uuid.UUID, err error) error {
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var private bool
	if sqlx.GetContext(ctx, db, &private, `SELECT EXISTS(SELECT 1 FROM workspace_privacy WHERE workspace_id=$1)`, workspaceID) == nil && private {
		return ErrPrivateWorkspaceOperation
	}
	return err
}

// Holds the shared publication barrier, independent of ordinary graph writes.
// Visibility transactions acquire its exclusive side before mutating teams. Callers must perform
// database reads before acquiring this guard or reuse its transaction connection.
func beginPublicWorkspaceOperation(ctx context.Context, db *sqlx.DB, workspaceID uuid.UUID) (*sqlx.Tx, error) {
	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock_shared(hashtextextended($1::text,63))`, workspaceID); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err = requireVisible(ctx, tx, workspaceVisible(ctx, uuidSQL(workspaceID))+" AND "+publicMetadataAllowed(uuidSQL(workspaceID))); err != nil {
		_ = tx.Rollback()
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrPrivateWorkspaceOperation
		}
		return nil, err
	}
	return tx, nil
}
