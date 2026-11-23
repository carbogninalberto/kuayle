package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/lib/pq"
)

func (r *WorkspaceRepository) MemberAccess(ctx context.Context, ws, user uuid.UUID) (*domain.RoleAccess, error) {
	var grants []struct {
		Team        *uuid.UUID     `db:"team"`
		Key         string         `db:"key"`
		Permissions pq.StringArray `db:"permissions"`
	}
	err := r.db.SelectContext(ctx, &grants, `SELECT assignment.team, r.key, r.permissions FROM (
		SELECT NULL::uuid AS team, role_key FROM workspace_members WHERE workspace_id=$1 AND user_id=$2
		UNION ALL
		SELECT team_id, role_key FROM team_role_overrides WHERE workspace_id=$1 AND user_id=$2
	) assignment JOIN workspace_roles r ON r.workspace_id=$1 AND r.key=assignment.role_key`, ws, user)
	if err != nil || len(grants) == 0 {
		return nil, err
	}
	access := &domain.RoleAccess{}
	for _, grant := range grants {
		if grant.Team == nil {
			access.Role, access.Permissions = grant.Key, grant.Permissions
		} else {
			if access.Teams == nil {
				access.Teams = make(map[uuid.UUID][]string)
			}
			access.Teams[*grant.Team] = grant.Permissions
		}
	}
	return access, nil
}

var ErrRoleInUse = errors.New("role has workspace or team assignments")
var ErrRoleNameTaken = errors.New("a role with that name already exists")
var ErrRoleGrant = errors.New("cannot grant permissions outside your current access")
var ErrRoleProtected = errors.New("this role is protected")
var ErrUnknownRole = errors.New("unknown role")

func membershipRoleError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" && pgErr.ConstraintName == "workspace_members_workspace_id_role_key_fkey" {
		return ErrUnknownRole
	}
	return err
}

// Serialize changes to the workspace and team grants used by a delegation.
func beginRoleChange(ctx context.Context, db *sqlx.DB, ws uuid.UUID) (*sqlx.Tx, error) {
	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text,202))`, ws); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	return tx, nil
}

const roleAssignmentCounts = `(SELECT count(*) FROM workspace_members m WHERE (m.workspace_id,m.role_key)=(r.workspace_id,r.key)) AS member_count,
		(SELECT count(*) FROM team_role_overrides t WHERE (t.workspace_id,t.role_key)=(r.workspace_id,r.key)) AS team_assignments`

func (r *WorkspaceRepository) ListRoles(ctx context.Context, ws uuid.UUID) ([]domain.WorkspaceRole, error) {
	roles := []domain.WorkspaceRole{}
	err := r.db.SelectContext(ctx, &roles, `SELECT r.key,r.name,r.description,r.permissions,
		`+roleAssignmentCounts+`
		FROM workspace_roles r WHERE workspace_id=$1 ORDER BY lower(name),key`, ws)
	return roles, err
}

func (r *WorkspaceRepository) SaveRole(ctx context.Context, ws uuid.UUID, role *domain.WorkspaceRole, create bool) error {
	if domain.BuiltinRole(role.Key) {
		return ErrRoleProtected
	}
	tx, err := beginRoleChange(ctx, r.db, ws)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	query := `UPDATE workspace_roles AS r SET name=$3,description=$4,permissions=$5 WHERE workspace_id=$1 AND key=$2`
	if create {
		query = `INSERT INTO workspace_roles AS r(workspace_id,key,name,description,permissions) VALUES ($1,$2,$3,$4,$5)`
	}
	err = tx.QueryRowContext(ctx, query+" RETURNING "+roleAssignmentCounts, ws, role.Key, role.Name, role.Description, role.Permissions).Scan(&role.MemberCount, &role.TeamAssignments)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.ConstraintName == "workspace_roles_name" {
		return ErrRoleNameTaken
	}
	if err != nil {
		return err
	}
	if !create {
		if err := revokeInvalidRoleLinks(ctx, tx, ws, nil, &role.Key); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *WorkspaceRepository) DeleteRole(ctx context.Context, ws uuid.UUID, key string) error {
	if domain.BuiltinRole(key) {
		return ErrRoleProtected
	}
	tx, err := beginRoleChange(ctx, r.db, ws)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	err = tx.QueryRowContext(ctx, `DELETE FROM workspace_roles WHERE workspace_id=$1 AND key=$2 RETURNING key`, ws, key).Scan(&key)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		return ErrRoleInUse
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

const roleGrantQuery = `SELECT CASE WHEN target.key IS NOT NULL THEN kuayle_can_grant_role(m.workspace_id,m.user_id,$3,$4) END
	FROM workspace_members m LEFT JOIN workspace_roles target ON target.workspace_id=m.workspace_id AND target.key=$3
	WHERE m.workspace_id=$1 AND m.user_id=$2`

func canGrantRole(ctx context.Context, q sqlx.QueryerContext, ws, user uuid.UUID, role string, scopes []string, lock bool) (bool, error) {
	query := roleGrantQuery
	if lock {
		query += " FOR SHARE OF m"
	}
	var allowed sql.NullBool
	err := sqlx.GetContext(ctx, q, &allowed, query, ws, user, role, pq.Array(scopes))
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err == nil && !allowed.Valid {
		return false, ErrUnknownRole
	}
	return allowed.Bool, err
}

func (r *WorkspaceRepository) CanGrantRole(ctx context.Context, ws, user uuid.UUID, role string) (bool, error) {
	return canGrantRole(ctx, r.db, ws, user, role, nil, false)
}

func authorizeRoleGrant(ctx context.Context, tx *sqlx.Tx, ws, user uuid.UUID, role string, scopes []string) error {
	allowed, err := canGrantRole(ctx, tx, ws, user, role, scopes, true)
	if err == nil && !allowed {
		return ErrRoleGrant
	}
	return err
}

func memberChangeActor(ctx context.Context, workspaceID uuid.UUID) (domain.Access, bool) {
	actor, ok := domain.AccessFromContext(ctx)
	return actor, ok && actor.UserID != uuid.Nil && actor.WorkspaceID == workspaceID
}

// The caller holds the role-change lock and the target membership row. Check
// both members' effective grants wherever either has an exception.
func authorizeMemberChange(ctx context.Context, tx *sqlx.Tx, ws, target uuid.UUID, current string, actor domain.Access) error {
	var allowed bool
	err := tx.GetContext(ctx, &allowed, `WITH teams(team) AS (
		SELECT NULL::uuid UNION SELECT team_id FROM team_role_overrides
		WHERE workspace_id=$1 AND user_id IN ($2,$3)
	)
	SELECT NOT EXISTS (
		SELECT 1 FROM teams
		LEFT JOIN team_role_overrides o ON o.workspace_id=$1 AND o.user_id=$2 AND o.team_id=teams.team
		JOIN workspace_roles r ON r.workspace_id=$1 AND r.key=coalesce(o.role_key,$4)
		WHERE ($5::text[] IS NOT NULL AND NOT r.permissions <@ $5)
		   OR EXISTS (SELECT 1 FROM unnest(r.permissions) p WHERE NOT kuayle_role_allows($1,$3,teams.team,p))
	) FROM workspace_members WHERE workspace_id=$1 AND user_id=$3
	  AND kuayle_role_allows($1,$3,NULL,'member:invite') FOR SHARE`,
		ws, target, actor.UserID, current, pq.Array(actor.TokenScopes))
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !allowed) {
		return ErrRoleGrant
	}
	return err
}

func (r *WorkspaceRepository) SetTeamRole(ctx context.Context, ws, user, team uuid.UUID, key string) error {
	if key == domain.RoleOwner {
		return ErrRoleProtected
	}
	tx, err := beginRoleChange(ctx, r.db, ws)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var current string
	err = tx.GetContext(ctx, &current, `SELECT m.role_key FROM workspace_members m JOIN teams t ON t.workspace_id=m.workspace_id
		WHERE m.workspace_id=$1 AND m.user_id=$2 AND t.id=$3 FOR UPDATE OF m`, ws, user, team)
	if err != nil {
		return err
	}
	if current == domain.RoleOwner {
		return ErrRoleProtected
	}
	if key == "" {
		_, err = tx.ExecContext(ctx, `DELETE FROM team_role_overrides WHERE workspace_id=$1 AND user_id=$2 AND team_id=$3`, ws, user, team)
	} else {
		err = tx.QueryRowContext(ctx, `INSERT INTO team_role_overrides(workspace_id,user_id,team_id,role_key)
			SELECT workspace_id,$2,$3,key FROM workspace_roles WHERE workspace_id=$1 AND key=$4
			ON CONFLICT(workspace_id,user_id,team_id) DO UPDATE SET role_key=EXCLUDED.role_key RETURNING role_key`, ws, user, team, key).Scan(&key)
	}
	if err != nil {
		return err
	}
	if err := revokeInvalidRoleLinks(ctx, tx, ws, &user, nil); err != nil {
		return err
	}
	return tx.Commit()
}

// IssueTeam resolves both the current identifier and a retired team key. The
// authorization stays bound to this team if the issue moves before the handler.
func (r *WorkspaceRepository) IssueTeam(ctx context.Context, ws uuid.UUID, identifier string) (uuid.UUID, error) {
	key, number := splitIdentifier(identifier)
	var team uuid.UUID
	err := r.db.GetContext(ctx, &team, `SELECT i.team_id FROM issue_records i
		WHERE i.workspace_id=$1 AND (i.identifier_text=$2 OR (i.number=$4 AND EXISTS
		(SELECT 1 FROM team_key_aliases a WHERE a.workspace_id=$1 AND a.team_id=i.team_id AND a.old_key=$3)))
		AND `+teamVisible(ctx, "i.team_id")+` ORDER BY (i.identifier_text=$2) DESC LIMIT 1`, ws, identifier, key, number)
	return team, err
}

func (r *WorkspaceRepository) TeamRoles(ctx context.Context, ws, user uuid.UUID) ([]domain.TeamRole, error) {
	roles := []domain.TeamRole{}
	err := r.db.SelectContext(ctx, &roles, `SELECT team_id,role_key FROM team_role_overrides WHERE workspace_id=$1 AND user_id=$2 ORDER BY team_id`, ws, user)
	return roles, err
}

func revokeInvalidRoleLinks(ctx context.Context, tx *sqlx.Tx, ws uuid.UUID, user *uuid.UUID, role *string) error {
	_, err := tx.ExecContext(ctx, `UPDATE workspace_invite_links l SET revoked_at=now()
 FROM workspace_members m WHERE l.workspace_id=$1 AND l.workspace_id=m.workspace_id AND l.created_by=m.user_id
 AND ($2::uuid IS NULL OR m.user_id=$2) AND ($3::text IS NULL OR m.role_key=$3 OR EXISTS(
 SELECT 1 FROM team_role_overrides o WHERE o.workspace_id=m.workspace_id AND o.user_id=m.user_id AND o.role_key=$3))
 AND l.revoked_at IS NULL AND NOT kuayle_can_grant_role(m.workspace_id,m.user_id,l.role,NULL)`, ws, user, role)
	return err
}
