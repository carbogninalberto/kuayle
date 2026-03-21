package service

import (
	"context"
	"fmt"

	"github.com/carbon/carbon-backend/internal/domain"
	"github.com/carbon/carbon-backend/internal/dto"
	"github.com/carbon/carbon-backend/internal/repository"
	"github.com/carbon/carbon-backend/pkg/audit"
	"github.com/google/uuid"
)

type WorkspaceService struct {
	workspaceRepo repository.WorkspaceRepo
	userRepo      repository.UserRepo
}

func NewWorkspaceService(workspaceRepo repository.WorkspaceRepo, userRepo repository.UserRepo) *WorkspaceService {
	return &WorkspaceService{workspaceRepo: workspaceRepo, userRepo: userRepo}
}

func (s *WorkspaceService) Create(ctx context.Context, userID uuid.UUID, req dto.CreateWorkspaceRequest) (*domain.Workspace, error) {
	existing, _ := s.workspaceRepo.GetBySlug(ctx, req.Slug)
	if existing != nil {
		return nil, fmt.Errorf("workspace slug already taken")
	}

	ws := &domain.Workspace{
		ID:   uuid.New(),
		Name: req.Name,
		Slug: req.Slug,
	}

	if err := s.workspaceRepo.Create(ctx, ws); err != nil {
		return nil, err
	}

	member := &domain.WorkspaceMember{
		WorkspaceID: ws.ID,
		UserID:      userID,
		Role:        domain.RoleOwner,
	}
	if err := s.workspaceRepo.AddMember(ctx, member); err != nil {
		return nil, err
	}

	audit.Log("workspace.created", userID, map[string]interface{}{
		"workspace_id": ws.ID, "slug": ws.Slug,
	})

	return ws, nil
}

func (s *WorkspaceService) GetBySlug(ctx context.Context, slug string) (*domain.Workspace, error) {
	return s.workspaceRepo.GetBySlug(ctx, slug)
}

func (s *WorkspaceService) ListByUser(ctx context.Context, userID uuid.UUID) ([]domain.Workspace, error) {
	return s.workspaceRepo.ListByUser(ctx, userID)
}

func (s *WorkspaceService) Update(ctx context.Context, slug string, req dto.UpdateWorkspaceRequest) (*domain.Workspace, error) {
	ws, err := s.workspaceRepo.GetBySlug(ctx, slug)
	if err != nil || ws == nil {
		return nil, fmt.Errorf("workspace not found")
	}

	if req.Name != nil {
		ws.Name = *req.Name
	}

	if err := s.workspaceRepo.Update(ctx, ws); err != nil {
		return nil, err
	}
	return ws, nil
}

func (s *WorkspaceService) GetMember(ctx context.Context, workspaceID, userID uuid.UUID) (*domain.WorkspaceMember, error) {
	return s.workspaceRepo.GetMember(ctx, workspaceID, userID)
}

func (s *WorkspaceService) InviteMember(ctx context.Context, workspaceID uuid.UUID, req dto.InviteMemberRequest) error {
	user, err := s.userRepo.GetByEmail(ctx, req.Email)
	if err != nil {
		return err
	}
	if user == nil {
		return fmt.Errorf("user not found")
	}

	existing, _ := s.workspaceRepo.GetMember(ctx, workspaceID, user.ID)
	if existing != nil {
		return fmt.Errorf("user is already a member")
	}

	member := &domain.WorkspaceMember{
		WorkspaceID: workspaceID,
		UserID:      user.ID,
		Role:        req.Role,
	}
	if err := s.workspaceRepo.AddMember(ctx, member); err != nil {
		return err
	}

	audit.Log("member.invited", user.ID, map[string]interface{}{
		"workspace_id": workspaceID, "role": req.Role, "email": req.Email,
	})
	return nil
}

func (s *WorkspaceService) ListMembers(ctx context.Context, workspaceID uuid.UUID) ([]domain.WorkspaceMember, error) {
	return s.workspaceRepo.ListMembers(ctx, workspaceID)
}

func (s *WorkspaceService) UpdateMemberRole(ctx context.Context, workspaceID, userID uuid.UUID, role string) error {
	member, err := s.workspaceRepo.GetMember(ctx, workspaceID, userID)
	if err != nil || member == nil {
		return fmt.Errorf("member not found")
	}

	if member.Role == domain.RoleOwner && role != domain.RoleOwner {
		count, err := s.workspaceRepo.CountMembersByRole(ctx, workspaceID, domain.RoleOwner)
		if err != nil {
			return err
		}
		if count <= 1 {
			return fmt.Errorf("cannot demote the last owner")
		}
	}

	if err := s.workspaceRepo.UpdateMemberRole(ctx, workspaceID, userID, role); err != nil {
		return err
	}

	audit.Log("member.role_changed", userID, map[string]interface{}{
		"workspace_id": workspaceID, "new_role": role, "old_role": member.Role,
	})
	return nil
}

func (s *WorkspaceService) RemoveMember(ctx context.Context, workspaceID, userID uuid.UUID) error {
	member, err := s.workspaceRepo.GetMember(ctx, workspaceID, userID)
	if err != nil || member == nil {
		return fmt.Errorf("member not found")
	}

	if member.Role == domain.RoleOwner {
		count, err := s.workspaceRepo.CountMembersByRole(ctx, workspaceID, domain.RoleOwner)
		if err != nil {
			return err
		}
		if count <= 1 {
			return fmt.Errorf("cannot remove the last owner")
		}
	}

	if err := s.workspaceRepo.RemoveMember(ctx, workspaceID, userID); err != nil {
		return err
	}

	audit.Log("member.removed", userID, map[string]interface{}{
		"workspace_id": workspaceID,
	})
	return nil
}

func (s *WorkspaceService) ListMembersWithUsers(ctx context.Context, workspaceID uuid.UUID) ([]domain.WorkspaceMemberWithUser, error) {
	return s.workspaceRepo.ListMembersWithUsers(ctx, workspaceID)
}
