package service

import (
	"context"
	"errors"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/audit"
)

var ErrInvalidRole = errors.New("invalid role name, description, template or permission set")

func (s *WorkspaceService) ListRoles(ctx context.Context, ws uuid.UUID) ([]domain.WorkspaceRole, error) {
	return s.workspaceRepo.ListRoles(ctx, ws)
}

func (s *WorkspaceService) SaveRole(ctx context.Context, ws, actor uuid.UUID, key string, req dto.SaveWorkspaceRoleRequest) (*domain.WorkspaceRole, error) {
	create := key == ""
	if domain.BuiltinRole(key) {
		return nil, repository.ErrRoleProtected
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || utf8.RuneCountInString(name) > 100 || utf8.RuneCountInString(req.Description) > 2000 {
		return nil, ErrInvalidRole
	}
	perms := req.Permissions
	if req.Template != "" {
		if !create || !domain.BuiltinRole(req.Template) {
			return nil, ErrInvalidRole
		}
		if perms == nil {
			perms = domain.RolePermissions[req.Template]
		}
	}
	if perms == nil {
		return nil, ErrInvalidRole
	}
	for _, code := range perms {
		if !domain.IsValidScope(code) {
			return nil, ErrInvalidRole
		}
	}
	perms = append([]string{}, perms...)
	slices.Sort(perms)
	action := "role.updated"
	if create {
		key = uuid.NewString()
		action = "role.created"
	}
	role := domain.WorkspaceRole{Key: key, Name: name, Description: strings.TrimSpace(req.Description), Permissions: slices.Compact(perms)}
	if err := s.workspaceRepo.SaveRole(ctx, ws, &role, create); err != nil {
		return nil, err
	}
	audit.Log(action, actor, map[string]any{"workspace_id": ws, "role": key, "name": name, "description": role.Description, "permissions": role.Permissions})
	s.refreshAccess(ws)
	return &role, nil
}

func (s *WorkspaceService) DeleteRole(ctx context.Context, ws, actor uuid.UUID, key string) error {
	if err := s.workspaceRepo.DeleteRole(ctx, ws, key); err != nil {
		return err
	}
	audit.Log("role.deleted", actor, map[string]any{"workspace_id": ws, "role": key})
	s.refreshAccess(ws)
	return nil
}

func (s *WorkspaceService) SetTeamRole(ctx context.Context, ws, actor, user, team uuid.UUID, key string) error {
	if err := s.workspaceRepo.SetTeamRole(ctx, ws, user, team, key); err != nil {
		return err
	}
	audit.Log("member.team_role_changed", actor, map[string]any{"workspace_id": ws, "user_id": user, "team_id": team, "role": key})
	s.refreshAccess(ws)
	return nil
}

func (s *WorkspaceService) TeamRoles(ctx context.Context, ws, user uuid.UUID) ([]domain.TeamRole, error) {
	return s.workspaceRepo.TeamRoles(ctx, ws, user)
}
