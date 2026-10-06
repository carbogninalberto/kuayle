package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
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
	err := r.db.GetContext(ctx, &team, `SELECT * FROM teams WHERE id = $1 AND `+teamVisible(ctx, "teams.id"), id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &team, err
}

func (r *TeamRepository) ListByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]domain.Team, error) {
	var teams []domain.Team
	err := r.db.SelectContext(ctx, &teams, `SELECT * FROM teams WHERE workspace_id = $1 AND `+teamVisible(ctx, "teams.id")+` ORDER BY name`, workspaceID)
	return teams, err
}

func (r *TeamRepository) Update(ctx context.Context, team *domain.Team) error {
	query := `UPDATE teams SET name = $1, description = $2, color = $3, icon = $4, triage_enabled = $5, parent_auto_close_enabled = $6, sub_issue_auto_close_enabled = $7, issue_copy_prompt = $8, updated_at = NOW() WHERE id = $9 AND ` + teamVisible(ctx, "teams.id") + ` RETURNING updated_at`
	return r.db.QueryRowContext(ctx, query, team.Name, team.Description, team.Color, team.Icon, team.TriageEnabled, team.ParentAutoCloseEnabled, team.SubIssueAutoCloseEnabled, team.IssueCopyPrompt, team.ID).Scan(&team.UpdatedAt)
}

func (r *TeamRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM teams WHERE id = $1 AND `+teamVisible(ctx, "teams.id"), id)
	return err
}

func (r *TeamRepository) AddMember(ctx context.Context, member *domain.TeamMember) error {
	query := `INSERT INTO team_members (team_id, user_id) SELECT $1, $2 WHERE EXISTS(SELECT 1 FROM teams member_team JOIN workspace_members candidate ON candidate.workspace_id=member_team.workspace_id AND candidate.user_id=$2 WHERE member_team.id=$1 AND ` + workspaceAdmin(ctx, "member_team.workspace_id") + `) ON CONFLICT(team_id,user_id) DO UPDATE SET user_id=EXCLUDED.user_id RETURNING created_at`
	return r.db.QueryRowContext(ctx, query, member.TeamID, member.UserID).Scan(&member.CreatedAt)
}

func (r *TeamRepository) GetMember(ctx context.Context, teamID, userID uuid.UUID) (*domain.TeamMember, error) {
	var member domain.TeamMember
	err := r.db.GetContext(ctx, &member, `SELECT * FROM team_members WHERE team_id = $1 AND user_id = $2 AND `+teamVisible(ctx, "team_members.team_id"), teamID, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &member, err
}

func (r *TeamRepository) ListMembers(ctx context.Context, teamID uuid.UUID) ([]domain.TeamMember, error) {
	var members []domain.TeamMember
	err := r.db.SelectContext(ctx, &members, `SELECT * FROM team_members WHERE team_id = $1 AND `+teamVisible(ctx, "team_members.team_id"), teamID)
	return members, err
}

func (r *TeamRepository) RemoveMember(ctx context.Context, teamID, userID uuid.UUID) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	own := "FALSE"
	if access, ok := domain.AccessFromContext(ctx); ok && access.UserID == userID {
		own = "TRUE"
	}
	if err := requireVisible(ctx, tx, "EXISTS(SELECT 1 FROM teams removal_team WHERE removal_team.id="+uuidSQL(teamID)+" AND "+teamVisible(ctx, "removal_team.id")+" AND ("+own+" OR "+workspaceAdmin(ctx, "removal_team.workspace_id")+"))"); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM team_members WHERE team_id=$1 AND user_id=$2`, teamID, userID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM issue_subscribers WHERE user_id=$2 AND issue_id IN (SELECT id FROM issues WHERE team_id=$1)`, teamID, userID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *TeamRepository) SetVisibility(ctx context.Context, id uuid.UUID, private, confirmPublic, acknowledge bool) (*domain.Team, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	var workspaceID uuid.UUID
	if err = tx.GetContext(ctx, &workspaceID, `SELECT workspace_id FROM teams WHERE id=$1 AND `+workspaceAdmin(ctx, "teams.workspace_id"), id); err != nil {
		return nil, err
	}
	// Publication may be slow. Wait before the statement-level graph barrier or
	// any tuple locks, so ordinary edits remain available during that wait.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text,63))`, workspaceID); err != nil {
		return nil, err
	}
	var team domain.Team
	err = tx.GetContext(ctx, &team, `UPDATE teams SET is_private=$2,updated_at=NOW() WHERE id=$1 AND `+workspaceAdmin(ctx, "teams.workspace_id")+` AND (NOT is_private OR $2 OR $3) AND (is_private OR NOT $2 OR $4) RETURNING *`, id, private, confirmPublic, acknowledge)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &team, nil
}

func (r *TeamRepository) CreateWithDefaults(ctx context.Context, team *domain.Team, creator uuid.UUID, statuses []domain.TeamStatus) error {
	access, ok := domain.AccessFromContext(ctx)
	if !ok || access.UserID != creator {
		return sql.ErrNoRows
	}
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := requireVisible(ctx, tx, workspaceAdmin(ctx, uuidSQL(team.WorkspaceID))); err != nil {
		return err
	}
	if team.IsPrivate {
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text,63))`, team.WorkspaceID); err != nil {
			return err
		}
	}
	if err := tx.QueryRowContext(ctx, `INSERT INTO teams(id,workspace_id,name,key,description,color,icon,is_private) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING created_at,updated_at`, team.ID, team.WorkspaceID, team.Name, team.Key, team.Description, team.Color, team.Icon, team.IsPrivate).Scan(&team.CreatedAt, &team.UpdatedAt); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO team_members(team_id,user_id) SELECT $1,$2 WHERE EXISTS(SELECT 1 FROM workspace_members WHERE workspace_id=$3 AND user_id=$2)`, team.ID, creator, team.WorkspaceID); err != nil {
		return err
	}
	for _, status := range statuses {
		if _, err := tx.ExecContext(ctx, `INSERT INTO team_statuses(id,team_id,name,slug,category,position,is_default) VALUES($1,$2,$3,$4,$5,$6,$7)`, status.ID, team.ID, status.Name, status.Slug, status.Category, status.Position, status.IsDefault); err != nil {
			return err
		}
	}
	return tx.Commit()
}
