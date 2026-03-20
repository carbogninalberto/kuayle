package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/carbon/carbon-backend/internal/domain"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

type TeamStatusRepository struct {
	db *sqlx.DB
}

func NewTeamStatusRepository(db *sqlx.DB) *TeamStatusRepository {
	return &TeamStatusRepository{db: db}
}

func (r *TeamStatusRepository) Create(ctx context.Context, status *domain.TeamStatus) error {
	query := `INSERT INTO team_statuses (id, team_id, name, slug, category, color, position, is_default)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING created_at, updated_at`
	return r.db.QueryRowContext(ctx, query,
		status.ID, status.TeamID, status.Name, status.Slug,
		status.Category, status.Color, status.Position, status.IsDefault,
	).Scan(&status.CreatedAt, &status.UpdatedAt)
}

func (r *TeamStatusRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.TeamStatus, error) {
	var status domain.TeamStatus
	err := r.db.GetContext(ctx, &status, `SELECT * FROM team_statuses WHERE id = $1`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &status, err
}

func (r *TeamStatusRepository) GetByTeamAndSlug(ctx context.Context, teamID uuid.UUID, slug string) (*domain.TeamStatus, error) {
	var status domain.TeamStatus
	err := r.db.GetContext(ctx, &status, `SELECT * FROM team_statuses WHERE team_id = $1 AND slug = $2`, teamID, slug)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &status, err
}

func (r *TeamStatusRepository) ListByTeam(ctx context.Context, teamID uuid.UUID) ([]domain.TeamStatus, error) {
	var statuses []domain.TeamStatus
	err := r.db.SelectContext(ctx, &statuses, `SELECT * FROM team_statuses WHERE team_id = $1 ORDER BY position`, teamID)
	return statuses, err
}

func (r *TeamStatusRepository) Update(ctx context.Context, status *domain.TeamStatus) error {
	query := `UPDATE team_statuses SET name = $1, color = $2, position = $3, updated_at = NOW()
		WHERE id = $4 RETURNING updated_at`
	return r.db.QueryRowContext(ctx, query, status.Name, status.Color, status.Position, status.ID).Scan(&status.UpdatedAt)
}

func (r *TeamStatusRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM team_statuses WHERE id = $1`, id)
	return err
}

func (r *TeamStatusRepository) NextPosition(ctx context.Context, teamID uuid.UUID) (int, error) {
	var pos int
	err := r.db.GetContext(ctx, &pos, `SELECT COALESCE(MAX(position), -1) + 1 FROM team_statuses WHERE team_id = $1`, teamID)
	return pos, err
}
