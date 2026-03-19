package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/carbon/carbon-backend/internal/domain"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

type CycleRepository struct {
	db *sqlx.DB
}

func NewCycleRepository(db *sqlx.DB) *CycleRepository {
	return &CycleRepository{db: db}
}

func (r *CycleRepository) Create(ctx context.Context, cycle *domain.Cycle) error {
	query := `INSERT INTO cycles (id, team_id, name, number, status, description, start_date, end_date) VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING created_at, updated_at`
	return r.db.QueryRowContext(ctx, query, cycle.ID, cycle.TeamID, cycle.Name, cycle.Number, cycle.Status, cycle.Description, cycle.StartDate, cycle.EndDate).Scan(&cycle.CreatedAt, &cycle.UpdatedAt)
}

func (r *CycleRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Cycle, error) {
	var cycle domain.Cycle
	err := r.db.GetContext(ctx, &cycle, `SELECT * FROM cycles WHERE id = $1`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &cycle, err
}

func (r *CycleRepository) ListByTeam(ctx context.Context, teamID uuid.UUID) ([]domain.Cycle, error) {
	var cycles []domain.Cycle
	err := r.db.SelectContext(ctx, &cycles, `SELECT * FROM cycles WHERE team_id = $1 ORDER BY number DESC`, teamID)
	return cycles, err
}

func (r *CycleRepository) NextNumber(ctx context.Context, teamID uuid.UUID) (int, error) {
	var num int
	err := r.db.GetContext(ctx, &num, `SELECT COALESCE(MAX(number), 0) + 1 FROM cycles WHERE team_id = $1`, teamID)
	return num, err
}

func (r *CycleRepository) Update(ctx context.Context, cycle *domain.Cycle) error {
	query := `UPDATE cycles SET name = $1, description = $2, status = $3, start_date = $4, end_date = $5, completed_at = $6, updated_at = NOW() WHERE id = $7 RETURNING updated_at`
	return r.db.QueryRowContext(ctx, query, cycle.Name, cycle.Description, cycle.Status, cycle.StartDate, cycle.EndDate, cycle.CompletedAt, cycle.ID).Scan(&cycle.UpdatedAt)
}

func (r *CycleRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM cycles WHERE id = $1`, id)
	return err
}

// IssueStats returns total, completed, and cancelled issue counts for a cycle.
func (r *CycleRepository) IssueStats(ctx context.Context, cycleID uuid.UUID) (total int, completed int, cancelled int, err error) {
	err = r.db.QueryRowContext(ctx,
		`SELECT COUNT(*), COUNT(*) FILTER (WHERE status = 'done'), COUNT(*) FILTER (WHERE status = 'cancelled') FROM issues WHERE cycle_id = $1`,
		cycleID,
	).Scan(&total, &completed, &cancelled)
	return
}
