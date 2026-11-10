package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

var ErrWorkspaceSlugTaken = errors.New("workspace slug already taken")
var ErrWorkspaceHasDevMachineRuntimes = errors.New("workspace has non-destroyed dev machine runtimes")
var ErrWorkspaceEnvironmentCleanupPending = errors.New("workspace environment cleanup is pending")

func workspaceCreateError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "workspaces_slug_key" {
		return ErrWorkspaceSlugTaken
	}
	return err
}

type WorkspaceRepository struct {
	db *sqlx.DB
}

func NewWorkspaceRepository(db *sqlx.DB) *WorkspaceRepository {
	return &WorkspaceRepository{db: db}
}

func (r *WorkspaceRepository) Create(ctx context.Context, ws *domain.Workspace) error {
	query := `INSERT INTO workspaces (id, name, slug, owner_id) VALUES ($1, $2, $3, $4) RETURNING created_at, updated_at`
	return workspaceCreateError(r.db.QueryRowContext(ctx, query, ws.ID, ws.Name, ws.Slug, ws.OwnerID).Scan(&ws.CreatedAt, &ws.UpdatedAt))
}

func (r *WorkspaceRepository) CreateWithMemberAndLabels(ctx context.Context, ws *domain.Workspace, member *domain.WorkspaceMember, labels []domain.Label) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	workspaceQuery := `INSERT INTO workspaces (id, name, slug, owner_id) VALUES ($1, $2, $3, $4) RETURNING created_at, updated_at`
	if err := tx.QueryRowContext(ctx, workspaceQuery, ws.ID, ws.Name, ws.Slug, ws.OwnerID).Scan(&ws.CreatedAt, &ws.UpdatedAt); err != nil {
		return workspaceCreateError(err)
	}

	memberQuery := `INSERT INTO workspace_members (workspace_id, user_id, role) VALUES ($1, $2, $3) RETURNING created_at`
	if err := tx.QueryRowContext(ctx, memberQuery, member.WorkspaceID, member.UserID, member.Role).Scan(&member.CreatedAt); err != nil {
		return err
	}

	labelQuery := `INSERT INTO labels (id, workspace_id, name, color, description, parent_id) VALUES ($1, $2, $3, $4, $5, $6) RETURNING created_at, updated_at`
	for i := range labels {
		label := &labels[i]
		if err := tx.QueryRowContext(ctx, labelQuery, label.ID, label.WorkspaceID, label.Name, label.Color, label.Description, label.ParentID).Scan(&label.CreatedAt, &label.UpdatedAt); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func (r *WorkspaceRepository) GetBySlug(ctx context.Context, slug string) (*domain.Workspace, error) {
	var ws domain.Workspace
	err := r.db.GetContext(ctx, &ws, `SELECT w.*,EXISTS(SELECT 1 FROM workspace_privacy p WHERE p.workspace_id=w.id) AS privacy_enabled FROM workspaces w WHERE slug = $1`, slug)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &ws, err
}

func (r *WorkspaceRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Workspace, error) {
	var ws domain.Workspace
	err := r.db.GetContext(ctx, &ws, `SELECT w.*,EXISTS(SELECT 1 FROM workspace_privacy p WHERE p.workspace_id=w.id) AS privacy_enabled FROM workspaces w WHERE id = $1`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &ws, err
}

func (r *WorkspaceRepository) Update(ctx context.Context, ws *domain.Workspace) error {
	query := `UPDATE workspaces SET name = $1, logo_url = $2, share_link_min_role = $3, updated_at = NOW() WHERE id = $4 RETURNING updated_at`
	return r.db.QueryRowContext(ctx, query, ws.Name, ws.LogoURL, ws.ShareLinkMinRole, ws.ID).Scan(&ws.UpdatedAt)
}

// Delete removes the workspace and returns its teams' image icons. They are
// read under row locks in the deleting transaction, so an icon upload that
// commits first is reported and one that waits finds its team gone.
func (r *WorkspaceRepository) Delete(ctx context.Context, id uuid.UUID) ([]domain.TeamIconImage, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))`, id); err != nil {
		return nil, err
	}
	var activeRuntimes int
	if err := tx.GetContext(ctx, &activeRuntimes, `SELECT COUNT(*) FROM dev_machines
		WHERE workspace_id=$1 AND status <> 'destroyed'`, id); err != nil {
		return nil, err
	}
	if activeRuntimes > 0 {
		return nil, fmt.Errorf("%w: %d", ErrWorkspaceHasDevMachineRuntimes, activeRuntimes)
	}
	var environments int
	if err := tx.GetContext(ctx, &environments, `SELECT COUNT(*) FROM dev_machine_environments WHERE workspace_id=$1`, id); err != nil {
		return nil, err
	}
	if environments > 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE dev_machine_operations SET status='cancelled',
			error_code='workspace_deletion_requested',error_message='snapshot cancelled because workspace deletion was requested',
			lease_owner=NULL,lease_expires_at=NULL,completed_at=NOW()
			WHERE workspace_id=$1 AND environment_id IS NOT NULL AND action='snapshot_environment'
			AND (status='pending' OR (status='leased' AND lease_expires_at<NOW()))`, id); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE dev_machine_environments environment
			SET status='delete_requested',delete_requested_at=COALESCE(delete_requested_at,NOW())
			WHERE workspace_id=$1 AND NOT EXISTS (
				SELECT 1 FROM dev_machine_operations operation WHERE operation.environment_id=environment.id
				AND operation.action='snapshot_environment' AND operation.status='leased'
				AND (operation.lease_expires_at IS NULL OR operation.lease_expires_at>=NOW())
			)`, id); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return nil, ErrWorkspaceEnvironmentCleanupPending
	}
	// The DELETE below takes the privacy barrier exclusively. Take it before the
	// team row locks: an icon upload holds the barrier shared while it waits for
	// a team row, so locking the rows first deadlocks with it.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(610046,0)`); err != nil {
		return nil, err
	}
	// Lock every team, not only those with an image now: an upload in flight
	// may be turning an emoji icon into an image.
	var teams []domain.TeamIconImage
	if err := tx.SelectContext(ctx, &teams, `SELECT id, COALESCE(icon,'') AS icon FROM teams WHERE workspace_id=$1 FOR UPDATE`, id); err != nil {
		return nil, err
	}
	var icons []domain.TeamIconImage
	for _, team := range teams {
		if strings.HasPrefix(team.Icon, domain.TeamIconImagePrefix) {
			icons = append(icons, team)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM workspaces WHERE id = $1`, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return icons, nil
}

func (r *WorkspaceRepository) ListByUser(ctx context.Context, userID uuid.UUID) ([]domain.Workspace, error) {
	var workspaces []domain.Workspace
	query := `SELECT w.*,EXISTS(SELECT 1 FROM workspace_privacy p WHERE p.workspace_id=w.id) AS privacy_enabled FROM workspaces w INNER JOIN workspace_members wm ON w.id = wm.workspace_id WHERE wm.user_id = $1 ORDER BY w.name`
	err := r.db.SelectContext(ctx, &workspaces, query, userID)
	return workspaces, err
}

func (r *WorkspaceRepository) AddMember(ctx context.Context, member *domain.WorkspaceMember) error {
	query := `INSERT INTO workspace_members (workspace_id, user_id, role) VALUES ($1, $2, $3) RETURNING created_at`
	return r.db.QueryRowContext(ctx, query, member.WorkspaceID, member.UserID, member.Role).Scan(&member.CreatedAt)
}

func (r *WorkspaceRepository) GetMember(ctx context.Context, workspaceID, userID uuid.UUID) (*domain.WorkspaceMember, error) {
	var member domain.WorkspaceMember
	err := r.db.GetContext(ctx, &member, `SELECT * FROM workspace_members WHERE workspace_id = $1 AND user_id = $2`, workspaceID, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &member, err
}

func (r *WorkspaceRepository) ListMembers(ctx context.Context, workspaceID uuid.UUID) ([]domain.WorkspaceMember, error) {
	var members []domain.WorkspaceMember
	err := r.db.SelectContext(ctx, &members, `SELECT * FROM workspace_members WHERE workspace_id = $1 ORDER BY created_at`, workspaceID)
	return members, err
}

func (r *WorkspaceRepository) UpdateMemberRole(ctx context.Context, workspaceID, userID uuid.UUID, role string) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `UPDATE workspace_members SET role = $1 WHERE workspace_id = $2 AND user_id = $3`, role, workspaceID, userID); err != nil {
		return err
	}
	// Invite links carry their creator's authority; a role that can no longer
	// invite must not leave working links behind.
	if !domain.HasPermission(role, domain.PermMemberInvite) {
		if err := revokeInviteLinksCreatedBy(ctx, tx, workspaceID, userID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *WorkspaceRepository) RemoveMember(ctx context.Context, workspaceID, userID uuid.UUID) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// Match authorized readers' workspace -> team membership lock order.
	// Take the privacy barrier before any row lock, as the DELETE statements do.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock_shared(610046,0)`); err != nil {
		return err
	}
	var member uuid.UUID
	err = tx.GetContext(ctx, &member, `SELECT user_id FROM workspace_members WHERE workspace_id=$1 AND user_id=$2 FOR UPDATE`, workspaceID, userID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	// A later workspace invitation must not revive old private-team grants or
	// subscriptions. Remove all three memberships atomically.
	// The removed user's invite links are revoked with them, after the membership
	// row lock, so they cannot be used to rejoin.
	if err := revokeInviteLinksCreatedBy(ctx, tx, workspaceID, userID); err != nil {
		return err
	}
	for _, query := range []string{
		`DELETE FROM team_members WHERE user_id=$2 AND team_id IN (SELECT id FROM teams WHERE workspace_id=$1)`,
		`DELETE FROM issue_subscribers WHERE user_id=$2 AND issue_id IN (SELECT id FROM issues WHERE workspace_id=$1)`,
		`DELETE FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`,
	} {
		if _, err := tx.ExecContext(ctx, query, workspaceID, userID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *WorkspaceRepository) CountMembersByRole(ctx context.Context, workspaceID uuid.UUID, role string) (int, error) {
	var count int
	err := r.db.GetContext(ctx, &count, `SELECT COUNT(*) FROM workspace_members WHERE workspace_id = $1 AND role = $2`, workspaceID, role)
	return count, err
}

func (r *WorkspaceRepository) ListMembersWithUsers(ctx context.Context, workspaceID uuid.UUID) ([]domain.WorkspaceMemberWithUser, error) {
	var members []domain.WorkspaceMemberWithUser
	query := `SELECT wm.workspace_id, wm.user_id, wm.role, u.email, u.name, u.display_name, u.avatar_url, wm.created_at
		FROM workspace_members wm
		JOIN users u ON u.id = wm.user_id
		WHERE wm.workspace_id = $1
		ORDER BY wm.created_at`
	err := r.db.SelectContext(ctx, &members, query, workspaceID)
	return members, err
}
