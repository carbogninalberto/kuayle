package service

import (
	"context"
	"fmt"

	"github.com/carbon/carbon-backend/internal/domain"
	"github.com/carbon/carbon-backend/internal/dto"
	"github.com/carbon/carbon-backend/internal/repository"
	"github.com/google/uuid"
)

type TeamService struct {
	teamRepo repository.TeamRepo
}

func NewTeamService(teamRepo repository.TeamRepo) *TeamService {
	return &TeamService{teamRepo: teamRepo}
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
	if req.EstimateScale != nil {
		team.EstimateScale = *req.EstimateScale
	}

	if err := s.teamRepo.Update(ctx, team); err != nil {
		return nil, err
	}
	return team, nil
}
