package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
)

var (
	ErrTeamNotFound       = errors.New("team not found")
	ErrTeamMemberNotFound = errors.New("team member not found")
)

type TeamService struct {
	teamRepo       repository.TeamRepo
	teamStatusRepo repository.TeamStatusRepo
}

func NewTeamService(teamRepo repository.TeamRepo, teamStatusRepo repository.TeamStatusRepo) *TeamService {
	return &TeamService{teamRepo: teamRepo, teamStatusRepo: teamStatusRepo}
}

func (s *TeamService) Create(ctx context.Context, workspaceID uuid.UUID, creatorID uuid.UUID, req dto.CreateTeamRequest) (*domain.Team, error) {
	team := &domain.Team{
		ID:          uuid.New(),
		WorkspaceID: workspaceID,
		Name:        req.Name,
		Key:         req.Key,
		Description: req.Description,
		Color:       req.Color,
		Icon:        req.Icon,
	}

	if err := s.teamRepo.Create(ctx, team); err != nil {
		return nil, err
	}

	// Add creator as team member
	member := &domain.TeamMember{
		TeamID: team.ID,
		UserID: creatorID,
	}
	_ = s.teamRepo.AddMember(ctx, member)

	// Create default statuses for the new team
	defaultStatuses := []struct {
		Name     string
		Slug     string
		Category domain.StatusCategory
		Position int
	}{
		{"Backlog", "backlog", domain.StatusCategoryBacklog, 0},
		{"Todo", "todo", domain.StatusCategoryUnstarted, 1},
		{"In Progress", "in_progress", domain.StatusCategoryStarted, 2},
		{"In Review", "in_review", domain.StatusCategoryStarted, 3},
		{"Done", "done", domain.StatusCategoryCompleted, 4},
		{"Cancelled", "cancelled", domain.StatusCategoryCancelled, 5},
	}
	for _, ds := range defaultStatuses {
		ts := &domain.TeamStatus{
			ID:        uuid.New(),
			TeamID:    team.ID,
			Name:      ds.Name,
			Slug:      ds.Slug,
			Category:  ds.Category,
			Position:  ds.Position,
			IsDefault: true,
		}
		_ = s.teamStatusRepo.Create(ctx, ts)
	}

	return team, nil
}

func (s *TeamService) GetByID(ctx context.Context, id uuid.UUID) (*domain.Team, error) {
	return s.teamRepo.GetByID(ctx, id)
}

func (s *TeamService) ListByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]domain.Team, error) {
	return s.teamRepo.ListByWorkspace(ctx, workspaceID)
}

func (s *TeamService) Update(ctx context.Context, id uuid.UUID, req dto.UpdateTeamRequest) (*domain.Team, error) {
	team, err := s.teamRepo.GetByID(ctx, id)
	if err != nil || team == nil {
		return nil, fmt.Errorf("team not found")
	}

	if req.Name != nil {
		team.Name = *req.Name
	}
	if req.Description != nil {
		team.Description = req.Description
	}
	if req.Color != nil {
		team.Color = req.Color
	}
	if req.Icon != nil {
		team.Icon = req.Icon
	}
	if req.TriageEnabled != nil {
		team.TriageEnabled = *req.TriageEnabled
	}

	if err := s.teamRepo.Update(ctx, team); err != nil {
		return nil, err
	}
	return team, nil
}

func (s *TeamService) Delete(ctx context.Context, workspaceID, id uuid.UUID) error {
	team, err := s.teamRepo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if team == nil || team.WorkspaceID != workspaceID {
		return ErrTeamNotFound
	}

	return s.teamRepo.Delete(ctx, id)
}

func (s *TeamService) Leave(ctx context.Context, workspaceID, teamID, userID uuid.UUID, workspaceRole string) (bool, error) {
	team, err := s.teamRepo.GetByID(ctx, teamID)
	if err != nil {
		return false, err
	}
	if team == nil || team.WorkspaceID != workspaceID {
		return false, ErrTeamNotFound
	}

	if workspaceRole == domain.RoleOwner {
		return true, s.teamRepo.Delete(ctx, teamID)
	}

	member, err := s.teamRepo.GetMember(ctx, teamID, userID)
	if err != nil {
		return false, err
	}
	if member == nil {
		return false, ErrTeamMemberNotFound
	}

	members, err := s.teamRepo.ListMembers(ctx, teamID)
	if err != nil {
		return false, err
	}
	if len(members) <= 1 {
		return true, s.teamRepo.Delete(ctx, teamID)
	}

	return false, s.teamRepo.RemoveMember(ctx, teamID, userID)
}
