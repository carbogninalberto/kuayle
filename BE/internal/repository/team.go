package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

type TeamRepository struct {
	db *sqlx.DB
}

func NewTeamRepository(db *sqlx.DB) *TeamRepository {
	return &TeamRepository{db: db}
}

func (r *TeamRepository) Create(ctx context.Context, team *domain.Team) error {
	query := `INSERT INTO teams (id, workspace_id, name, key, description, color, icon) VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING created_at, updated_at`
	return r.db.QueryRowContext(ctx, query, team.ID, team.WorkspaceID, team.Name, team.Key, team.Description, team.Color, team.Icon).Scan(&team.CreatedAt, &team.UpdatedAt)
}

func (r *TeamRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Team, error) {
	var team domain.Team
	err := r.db.GetContext(ctx, &team, `SELECT * FROM teams WHERE id = $1`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &team, err
}

func (r *TeamRepository) ListByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]domain.Team, error) {
	var teams []domain.Team
	err := r.db.SelectContext(ctx, &teams, `SELECT * FROM teams WHERE workspace_id = $1 ORDER BY name`, workspaceID)
	return teams, err
}

func (r *TeamRepository) Update(ctx context.Context, team *domain.Team) error {
	query := `UPDATE teams SET name = $1, description = $2, color = $3, icon = $4, triage_enabled = $5, updated_at = NOW() WHERE id = $6 RETURNING updated_at`
	return r.db.QueryRowContext(ctx, query, team.Name, team.Description, team.Color, team.Icon, team.TriageEnabled, team.ID).Scan(&team.UpdatedAt)
}

func (r *TeamRepository) AddMember(ctx context.Context, member *domain.TeamMember) error {
	query := `INSERT INTO team_members (team_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING RETURNING created_at`
	return r.db.QueryRowContext(ctx, query, member.TeamID, member.UserID).Scan(&member.CreatedAt)
}

func (r *TeamRepository) GetMember(ctx context.Context, teamID, userID uuid.UUID) (*domain.TeamMember, error) {
	var member domain.TeamMember
	err := r.db.GetContext(ctx, &member, `SELECT * FROM team_members WHERE team_id = $1 AND user_id = $2`, teamID, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &member, err
}
