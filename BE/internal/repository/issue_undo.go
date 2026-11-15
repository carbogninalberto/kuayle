package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/lib/pq"
)

// The issues relation is a view over issue_records without soft-deleted rows
// (migration 000160). Queries here name issue_records only where deleted rows
// must be seen: restore, purge and the soft delete itself.

// ErrEditedMeanwhile means an issue changed after the edit being undone, so
// the undo wrote nothing.
var ErrEditedMeanwhile = errors.New("this item was edited in the meantime, so nothing was undone")

// ErrUndoUnavailable means a value the undo would put back (a status, cycle,
// project or user) is no longer available to the issue, so nothing was written.
var ErrUndoUnavailable = errors.New("a previous value is no longer available, so nothing was undone")

// ErrRestoreExpired means a deleted issue is past its restore period.
var ErrRestoreExpired = errors.New("past the restore period")

// issueActor is the user a write is attributed to: the explicit history actor,
// else the authenticated caller.
func issueActor(ctx context.Context) uuid.UUID {
	actor, _ := ctx.Value(issueHistoryActorKey{}).(uuid.UUID)
	if actor == uuid.Nil {
		if access, ok := domain.AccessFromContext(ctx); ok {
			actor = access.UserID
		}
	}
	return actor
}

// setHistoryActor attributes the content-history trigger rows written in tx.
// The settings are transaction-local, so they never leak to the next pooled request.
func setHistoryActor(ctx context.Context, tx *sqlx.Tx) error {
	actorValue := ""
	if actor := issueActor(ctx); actor != uuid.Nil {
		actorValue = actor.String()
	}
	if _, err := tx.ExecContext(ctx, `SELECT set_config('kuayle.history_actor', $1, true)`, actorValue); err != nil {
		return err
	}
	agentValue := ""
	if agent := domain.ActingAgent(ctx); agent != nil {
		agentValue = *agent
	}
	_, err := tx.ExecContext(ctx, `SELECT set_config('kuayle.history_agent', $1, true)`, agentValue)
	return err
}

func nullableUUID(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

// ListByIDs returns the visible issues of the workspace among ids, ordered by id.
func (r *IssueRepository) ListByIDs(ctx context.Context, workspaceID uuid.UUID, ids []uuid.UUID) ([]domain.Issue, error) {
	issues := []domain.Issue{}
	if len(ids) == 0 {
		return issues, nil
	}
	err := r.db.SelectContext(ctx, &issues, `SELECT * FROM issues WHERE workspace_id = $1 AND id = ANY($2) AND `+teamVisible(ctx, "issues.team_id")+` ORDER BY id`,
		workspaceID, pq.Array(ids))
	return issues, err
}

// ApplyUndo writes the previous values of every item in one transaction. Each
// issue must still be at its expected version: if any changed since, or is no
// longer visible, or a previous value is no longer available, nothing is
// written. It returns the issues as they are after the undo, ordered by id.
func (r *IssueRepository) ApplyUndo(ctx context.Context, workspaceID uuid.UUID, items []domain.IssueUndo) ([]domain.Issue, error) {
	if len(items) == 0 {
		return []domain.Issue{}, nil
	}
	ids := make([]uuid.UUID, len(items))
	for i, item := range items {
		ids[i] = item.IssueID
	}
	// The privacy graph barrier comes before the issue row locks, as in SetLabels.
	tx, err := beginConversationTransaction(ctx, r.db)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := setHistoryActor(ctx, tx); err != nil {
		return nil, err
	}
	var locked []domain.Issue
	// Rows are locked in ID order so concurrent bulk writes cannot deadlock.
	if err := tx.SelectContext(ctx, &locked, `SELECT * FROM issues WHERE id = ANY($1) AND workspace_id = $2 AND `+teamVisible(ctx, "issues.team_id")+`
		ORDER BY id FOR UPDATE`, pq.Array(ids), workspaceID); err != nil {
		return nil, err
	}
	current := make(map[uuid.UUID]domain.Issue, len(locked))
	for _, issue := range locked {
		current[issue.ID] = issue
	}
	if len(current) != len(uuidSet(ids)) {
		return nil, ErrBulkIssueNotFound
	}
	for _, item := range items {
		if current[item.IssueID].Version != item.ExpectedVersion {
			return nil, ErrEditedMeanwhile
		}
	}

	for _, item := range items {
		candidate := current[item.IssueID]
		prev := item.Previous
		if prev.StatusID != nil {
			candidate.StatusID = prev.StatusID
		}
		if prev.Priority != nil {
			candidate.Priority = *prev.Priority
		}
		if prev.AssigneeSet {
			candidate.AssigneeID = prev.AssigneeID
		}
		if prev.AssigneeIDs != nil {
			candidate.AssigneeID = nil
			if len(*prev.AssigneeIDs) > 0 {
				first := (*prev.AssigneeIDs)[0]
				candidate.AssigneeID = &first
			}
		}
		if prev.ProjectSet {
			candidate.ProjectID = prev.ProjectID
		}
		if prev.CycleSet {
			candidate.CycleID = prev.CycleID
		}
		if prev.SortOrder != nil {
			candidate.SortOrder = *prev.SortOrder
		}
		// The status, cycle, project and assignee must still belong to the
		// issue's team and workspace and be visible to the caller, as for any
		// edit (issueReferencesVisible); the status slug follows status_id.
		var updated int
		err := tx.GetContext(ctx, &updated, `UPDATE issues SET
				status_id = $2, status = COALESCE((SELECT slug FROM team_statuses WHERE id = $2), status),
				priority = $3, assignee_id = $4, project_id = $5, cycle_id = $6, sort_order = $7, updated_at = NOW()
			WHERE id = $1 AND `+issueReferencesVisible(ctx, &candidate)+` RETURNING 1`,
			candidate.ID, candidate.StatusID, candidate.Priority, candidate.AssigneeID, candidate.ProjectID, candidate.CycleID, candidate.SortOrder)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUndoUnavailable
		}
		if err != nil {
			return nil, err
		}
		if prev.ProjectSet {
			// Labels scoped to a project the issue no longer is in stop applying,
			// as in UpdateContent; a label undo below puts back the earlier set.
			if _, err := tx.ExecContext(ctx, `DELETE FROM issue_labels il USING labels l WHERE il.issue_id = $1 AND l.id = il.label_id
				AND l.project_id IS NOT NULL AND l.project_id IS DISTINCT FROM $2::uuid`, candidate.ID, candidate.ProjectID); err != nil {
				return nil, err
			}
		}
		if prev.LabelIDs != nil {
			if _, err := tx.ExecContext(ctx, `DELETE FROM issue_labels WHERE issue_id = $1`, candidate.ID); err != nil {
				return nil, err
			}
			// Labels deleted since, or scoped to another project, are skipped as in SetLabels.
			if _, err := tx.ExecContext(ctx, `INSERT INTO issue_labels (issue_id, label_id)
				SELECT $1, l.id FROM labels l JOIN issues label_issue ON label_issue.id = $1 AND l.workspace_id = label_issue.workspace_id
				WHERE l.id = ANY($2) AND l.deleted_at IS NULL AND (l.project_id IS NULL OR l.project_id = label_issue.project_id)
				ON CONFLICT DO NOTHING`, candidate.ID, pq.Array(*prev.LabelIDs)); err != nil {
				return nil, err
			}
		}
		if prev.AssigneeIDs != nil {
			if _, err := tx.ExecContext(ctx, `DELETE FROM issue_assignees WHERE issue_id = $1`, candidate.ID); err != nil {
				return nil, err
			}
			// Kept in their earlier order; users who left the workspace are skipped.
			if _, err := tx.ExecContext(ctx, `INSERT INTO issue_assignees (issue_id, user_id, created_at)
				SELECT $1, assignee.user_id, statement_timestamp() + assignee.position * INTERVAL '1 microsecond'
				FROM unnest($2::uuid[]) WITH ORDINALITY AS assignee(user_id, position)
				JOIN issues assignee_issue ON assignee_issue.id = $1
				JOIN workspace_members am ON am.workspace_id = assignee_issue.workspace_id AND am.user_id = assignee.user_id
				ON CONFLICT DO NOTHING`, candidate.ID, pq.Array(*prev.AssigneeIDs)); err != nil {
				return nil, err
			}
		}
	}
	var result []domain.Issue
	if err := tx.SelectContext(ctx, &result, `SELECT * FROM issues WHERE id = ANY($1) ORDER BY id`, pq.Array(ids)); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

// GetDeletedByIdentifier returns a soft-deleted, visible issue of the
// workspace, or nil.
func (r *IssueRepository) GetDeletedByIdentifier(ctx context.Context, workspaceID uuid.UUID, identifier string) (*domain.DeletedIssue, error) {
	var issue domain.DeletedIssue
	err := r.db.GetContext(ctx, &issue, `SELECT * FROM issue_records WHERE workspace_id = $1 AND identifier_text = $2 AND deleted_at IS NOT NULL AND `+teamVisible(ctx, "issue_records.team_id"),
		workspaceID, identifier)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &issue, nil
}

// ListDeletedByIDs returns the soft-deleted, visible issues of the workspace
// among ids, ordered by id.
func (r *IssueRepository) ListDeletedByIDs(ctx context.Context, workspaceID uuid.UUID, ids []uuid.UUID) ([]domain.DeletedIssue, error) {
	issues := []domain.DeletedIssue{}
	if len(ids) == 0 {
		return issues, nil
	}
	err := r.db.SelectContext(ctx, &issues, `SELECT * FROM issue_records WHERE workspace_id = $1 AND id = ANY($2) AND deleted_at IS NOT NULL AND `+teamVisible(ctx, "issue_records.team_id")+` ORDER BY id`,
		workspaceID, pq.Array(ids))
	return issues, err
}

// Restore brings back soft-deleted issues in one transaction and returns
// them, ordered by id. Issues that are not deleted are left alone. If any
// issue is missing or hidden nothing is written (ErrBulkIssueNotFound); if any
// was deleted before deletedSince nothing is written (ErrRestoreExpired).
func (r *IssueRepository) Restore(ctx context.Context, workspaceID uuid.UUID, ids []uuid.UUID, deletedSince time.Time) ([]domain.Issue, error) {
	if len(ids) == 0 {
		return []domain.Issue{}, nil
	}
	tx, err := beginConversationTransaction(ctx, r.db)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	type lockedIssue struct {
		ID        uuid.UUID  `db:"id"`
		DeletedAt *time.Time `db:"deleted_at"`
	}
	var locked []lockedIssue
	if err := tx.SelectContext(ctx, &locked, `SELECT id, deleted_at FROM issue_records
		WHERE id = ANY($1) AND workspace_id = $2 AND `+teamVisible(ctx, "issue_records.team_id")+` ORDER BY id FOR UPDATE`,
		pq.Array(ids), workspaceID); err != nil {
		return nil, err
	}
	if len(locked) != len(uuidSet(ids)) {
		return nil, ErrBulkIssueNotFound
	}
	restore := make([]uuid.UUID, 0, len(locked))
	for _, issue := range locked {
		if issue.DeletedAt == nil {
			continue
		}
		if issue.DeletedAt.Before(deletedSince) {
			return nil, ErrRestoreExpired
		}
		restore = append(restore, issue.ID)
	}
	restored := []domain.Issue{}
	if len(restore) > 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE issue_records SET deleted_at = NULL, deleted_by = NULL WHERE id = ANY($1)`, pq.Array(restore)); err != nil {
			return nil, err
		}
		if err := tx.SelectContext(ctx, &restored, `SELECT * FROM issues WHERE id = ANY($1) ORDER BY id`, pq.Array(restore)); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return restored, nil
}

// PurgeDeletedIssues hard-deletes issues soft-deleted before deletedBefore, in
// batches so that each statement's cascades stay small. It returns how many
// rows it removed. It is a background job and applies no caller visibility.
func (r *IssueRepository) PurgeDeletedIssues(ctx context.Context, deletedBefore time.Time, batchSize int) (int, error) {
	if batchSize <= 0 {
		batchSize = 500
	}
	total := 0
	for {
		result, err := r.db.ExecContext(ctx, `DELETE FROM issue_records WHERE id IN (
			SELECT id FROM issue_records WHERE deleted_at < $1 ORDER BY deleted_at LIMIT $2 FOR UPDATE SKIP LOCKED)`,
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

func uuidSet(ids []uuid.UUID) map[uuid.UUID]struct{} {
	set := make(map[uuid.UUID]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}
	return set
}
