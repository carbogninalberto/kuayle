package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

type LabelRepository struct {
	db *sqlx.DB
}

func NewLabelRepository(db *sqlx.DB) *LabelRepository {
	return &LabelRepository{db: db}
}

// ErrLabelProjectNotVisible is returned by Create when the label's project
// does not exist in the workspace or the caller cannot see it.
var ErrLabelProjectNotVisible = errors.New("label project not found")

// Create checks the project's visibility in the same statement as the insert,
// after the privacy barrier, so a project that became hidden meanwhile is not
// written to.
func (r *LabelRepository) Create(ctx context.Context, label *domain.Label) error {
	tx, err := beginConversationTransaction(ctx, r.db)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	query := `INSERT INTO labels (id, workspace_id, name, color, description, parent_id, project_id)
		SELECT $1, $2, $3, $4, $5, $6, $7 WHERE $7::uuid IS NULL OR EXISTS (
			SELECT 1 FROM projects label_project WHERE label_project.id = $7::uuid AND label_project.workspace_id = $2 AND ` + projectVisible(ctx, "label_project.workspace_id", "label_project.team_id") + `)
		RETURNING created_at, updated_at`
	err = tx.QueryRowContext(ctx, query, label.ID, label.WorkspaceID, label.Name, label.Color, label.Description, label.ParentID, label.ProjectID).Scan(&label.CreatedAt, &label.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrLabelProjectNotVisible
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (r *LabelRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Label, error) {
	var label domain.Label
	err := r.db.GetContext(ctx, &label, `SELECT * FROM labels WHERE id = $1 AND deleted_at IS NULL AND `+labelVisible(ctx, "labels"), id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &label, err
}

// ListByWorkspace returns every label of the workspace. With a projectID it
// returns the workspace-wide labels plus that project's own.
func (r *LabelRepository) ListByWorkspace(ctx context.Context, workspaceID uuid.UUID, projectID *uuid.UUID) ([]domain.Label, error) {
	var labels []domain.Label
	if projectID == nil {
		err := r.db.SelectContext(ctx, &labels, `SELECT * FROM labels WHERE workspace_id = $1 AND deleted_at IS NULL AND `+labelVisible(ctx, "labels")+` ORDER BY name`, workspaceID)
		return labels, err
	}
	err := r.db.SelectContext(ctx, &labels, `SELECT * FROM labels WHERE workspace_id = $1 AND (project_id IS NULL OR project_id = $2) AND deleted_at IS NULL AND `+labelVisible(ctx, "labels")+` ORDER BY name`, workspaceID, *projectID)
	return labels, err
}

func (r *LabelRepository) Update(ctx context.Context, label *domain.Label) error {
	query := `UPDATE labels SET name = $1, color = $2, description = $3, updated_at = NOW() WHERE id = $4 AND ` + labelVisible(ctx, "labels") + ` RETURNING updated_at`
	return r.db.QueryRowContext(ctx, query, label.Name, label.Color, label.Description, label.ID).Scan(&label.UpdatedAt)
}

func (r *LabelRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `UPDATE labels SET deleted_at = NOW() WHERE id = $1 AND `+labelVisible(ctx, "labels"), id)
	return err
}

// ExistsByName reports whether the name is taken within one scope: the
// workspace-wide labels (projectID nil) or a single project's labels. A label
// being renamed passes its own ID as excludeID (uuid.Nil excludes nothing).
func (r *LabelRepository) ExistsByName(ctx context.Context, workspaceID uuid.UUID, projectID *uuid.UUID, name string, excludeID uuid.UUID) (bool, error) {
	var exists bool
	err := r.db.GetContext(ctx, &exists, `SELECT EXISTS(SELECT 1 FROM labels WHERE workspace_id = $1 AND project_id IS NOT DISTINCT FROM $2::uuid AND LOWER(name) = LOWER($3) AND id <> $4 AND deleted_at IS NULL)`, workspaceID, projectID, name, excludeID)
	return exists, err
}
