package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

type WorkspaceRepository struct {
	db *sqlx.DB
}

func NewWorkspaceRepository(db *sqlx.DB) *WorkspaceRepository {
	return &WorkspaceRepository{db: db}
}

func (r *WorkspaceRepository) Create(ctx context.Context, ws *domain.Workspace) error {
	query := `INSERT INTO workspaces (id, name, slug) VALUES ($1, $2, $3) RETURNING created_at, updated_at`
	return r.db.QueryRowContext(ctx, query, ws.ID, ws.Name, ws.Slug).Scan(&ws.CreatedAt, &ws.UpdatedAt)
}

func (r *WorkspaceRepository) GetBySlug(ctx context.Context, slug string) (*domain.Workspace, error) {
	var ws domain.Workspace
	err := r.db.GetContext(ctx, &ws, `SELECT * FROM workspaces WHERE slug = $1`, slug)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &ws, err
}

func (r *WorkspaceRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Workspace, error) {
	var ws domain.Workspace
	err := r.db.GetContext(ctx, &ws, `SELECT * FROM workspaces WHERE id = $1`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &ws, err
}

func (r *WorkspaceRepository) Update(ctx context.Context, ws *domain.Workspace) error {
	query := `UPDATE workspaces SET name = $1, updated_at = NOW() WHERE id = $2 RETURNING updated_at`
	return r.db.QueryRowContext(ctx, query, ws.Name, ws.ID).Scan(&ws.UpdatedAt)
}

func (r *WorkspaceRepository) ListByUser(ctx context.Context, userID uuid.UUID) ([]domain.Workspace, error) {
	var workspaces []domain.Workspace
	query := `SELECT w.* FROM workspaces w INNER JOIN workspace_members wm ON w.id = wm.workspace_id WHERE wm.user_id = $1 ORDER BY w.name`
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
	_, err := r.db.ExecContext(ctx, `UPDATE workspace_members SET role = $1 WHERE workspace_id = $2 AND user_id = $3`, role, workspaceID, userID)
	return err
}

func (r *WorkspaceRepository) RemoveMember(ctx context.Context, workspaceID, userID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM workspace_members WHERE workspace_id = $1 AND user_id = $2`, workspaceID, userID)
	return err
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
