package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// ErrTeamKeyTaken is returned when a team key is already another team's key or
// one of its retired keys (an alias) in the workspace.
var ErrTeamKeyTaken = errors.New("team key already in use")

type TeamRepository struct {
	db *sqlx.DB
}

func NewTeamRepository(db *sqlx.DB) *TeamRepository {
	return &TeamRepository{db: db}
}

func (r *TeamRepository) Create(ctx context.Context, team *domain.Team) error {
	var aliased bool
	if err := r.db.GetContext(ctx, &aliased, `SELECT EXISTS(SELECT 1 FROM team_key_aliases WHERE workspace_id=$1 AND old_key=$2)`, team.WorkspaceID, team.Key); err != nil {
		return err
	}
	if aliased {
		return ErrTeamKeyTaken
	}
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
	if err := lockTeamKeys(ctx, tx, team.WorkspaceID); err != nil {
		return err
	}
	if err := rejectAliasedKey(ctx, tx, team.WorkspaceID, team.Key); err != nil {
		return err
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

// lockTeamKeys serializes key changes and team creation in a workspace, so a
// key cannot be taken and retired at the same moment.
func lockTeamKeys(ctx context.Context, tx *sqlx.Tx, workspaceID uuid.UUID) error {
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('team-keys:'||$1::text,63))`, workspaceID)
	return err
}

func rejectAliasedKey(ctx context.Context, tx *sqlx.Tx, workspaceID uuid.UUID, key string) error {
	var aliased bool
	if err := tx.GetContext(ctx, &aliased, `SELECT EXISTS(SELECT 1 FROM team_key_aliases WHERE workspace_id=$1 AND old_key=$2)`, workspaceID, key); err != nil {
		return err
	}
	if aliased {
		return ErrTeamKeyTaken
	}
	return nil
}

// UpdateWithKey saves the team and renames its key in one transaction: the old
// key becomes an alias, every issue identifier of the team is rewritten to the
// new key, and a key the team itself retired earlier is released again. The
// caller has checked the new key's format.
func (r *TeamRepository) UpdateWithKey(ctx context.Context, team *domain.Team, newKey string) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := lockTeamKeys(ctx, tx, team.WorkspaceID); err != nil {
		return err
	}
	// The lock issue creation holds while it numbers a new issue (NextNumber):
	// an issue being created finishes under the old key before the rename runs.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1::text))`, team.ID); err != nil {
		return err
	}
	var oldKey string
	err = tx.GetContext(ctx, &oldKey, `SELECT key FROM teams WHERE id=$1 AND workspace_id=$2 AND `+teamVisible(ctx, "teams.id")+` FOR UPDATE`, team.ID, team.WorkspaceID)
	if err != nil {
		return err
	}
	if oldKey != newKey {
		var other uuid.UUID
		switch err := tx.GetContext(ctx, &other, `SELECT team_id FROM team_key_aliases WHERE workspace_id=$1 AND old_key=$2`, team.WorkspaceID, newKey); {
		case err == nil:
			if other != team.ID {
				return ErrTeamKeyTaken
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM team_key_aliases WHERE workspace_id=$1 AND old_key=$2`, team.WorkspaceID, newKey); err != nil {
				return err
			}
		case !errors.Is(err, sql.ErrNoRows):
			return err
		}
		var taken bool
		if err := tx.GetContext(ctx, &taken, `SELECT EXISTS(SELECT 1 FROM teams WHERE workspace_id=$1 AND key=$2 AND id<>$3)`, team.WorkspaceID, newKey, team.ID); err != nil {
			return err
		}
		if taken {
			return ErrTeamKeyTaken
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO team_key_aliases(workspace_id,old_key,team_id) VALUES($1,$2,$3)`, team.WorkspaceID, oldKey, team.ID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE issues SET identifier_text=$1||'-'||number WHERE team_id=$2`, newKey, team.ID); err != nil {
			return fmt.Errorf("rewrite issue identifiers: %w", err)
		}
	}
	err = tx.QueryRowContext(ctx, `UPDATE teams SET name=$1,key=$2,description=$3,color=$4,icon=$5,triage_enabled=$6,parent_auto_close_enabled=$7,sub_issue_auto_close_enabled=$8,issue_copy_prompt=$9,updated_at=NOW() WHERE id=$10 RETURNING updated_at`,
		team.Name, newKey, team.Description, team.Color, team.Icon, team.TriageEnabled, team.ParentAutoCloseEnabled, team.SubIssueAutoCloseEnabled, team.IssueCopyPrompt, team.ID).Scan(&team.UpdatedAt)
	if err != nil {
		return err
	}
	team.Key = newKey
	return tx.Commit()
}
