package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/carbon/carbon-backend/internal/domain"
	"github.com/carbon/carbon-backend/internal/dto"
	"github.com/carbon/carbon-backend/internal/repository"
	"github.com/google/uuid"
)

type TeamStatusService struct {
	statusRepo repository.TeamStatusRepo
}

func NewTeamStatusService(statusRepo repository.TeamStatusRepo) *TeamStatusService {
	return &TeamStatusService{statusRepo: statusRepo}
}

func (s *TeamStatusService) List(ctx context.Context, teamID uuid.UUID) ([]domain.TeamStatus, error) {
	return s.statusRepo.ListByTeam(ctx, teamID)
}

func (s *TeamStatusService) Create(ctx context.Context, teamID uuid.UUID, req dto.CreateTeamStatusRequest) (*domain.TeamStatus, error) {
	pos, err := s.statusRepo.NextPosition(ctx, teamID)
	if err != nil {
		return nil, err
	}

	slug := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(req.Name), " ", "_"))

	status := &domain.TeamStatus{
		ID:        uuid.New(),
		TeamID:    teamID,
		Name:      req.Name,
		Slug:      slug,
		Category:  domain.StatusCategory(req.Category),
		Color:     req.Color,
		Position:  pos,
		IsDefault: false,
	}

	if err := s.statusRepo.Create(ctx, status); err != nil {
		return nil, err
	}
	return status, nil
}

func (s *TeamStatusService) Update(ctx context.Context, id uuid.UUID, req dto.UpdateTeamStatusRequest) (*domain.TeamStatus, error) {
	status, err := s.statusRepo.GetByID(ctx, id)
	if err != nil || status == nil {
		return nil, fmt.Errorf("status not found")
	}

	if req.Name != nil {
		status.Name = *req.Name
	}
	if req.Color != nil {
		status.Color = req.Color
	}
	if req.Position != nil {
		status.Position = *req.Position
	}

	if err := s.statusRepo.Update(ctx, status); err != nil {
		return nil, err
	}
	return status, nil
}

func (s *TeamStatusService) Delete(ctx context.Context, id uuid.UUID) error {
	status, err := s.statusRepo.GetByID(ctx, id)
	if err != nil || status == nil {
		return fmt.Errorf("status not found")
	}
	if status.IsDefault {
		return fmt.Errorf("cannot delete the default status")
	}
	return s.statusRepo.Delete(ctx, id)
}
