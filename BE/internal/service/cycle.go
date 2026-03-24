package service

import (
	"context"
	"fmt"
	"time"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/google/uuid"
)

type CycleService struct {
	cycleRepo repository.CycleRepo
}

func NewCycleService(cycleRepo repository.CycleRepo) *CycleService {
	return &CycleService{cycleRepo: cycleRepo}
}

func (s *CycleService) Create(ctx context.Context, teamID uuid.UUID, req dto.CreateCycleRequest) (*domain.Cycle, error) {
	number, err := s.cycleRepo.NextNumber(ctx, teamID)
	if err != nil {
		return nil, err
	}

	cycle := &domain.Cycle{
		ID:          uuid.New(),
		TeamID:      teamID,
		Name:        req.Name,
		Number:      number,
		Status:      domain.CycleStatusUpcoming,
		Description: req.Description,
	}

	if req.StartDate != nil {
		t, err := time.Parse("2006-01-02", *req.StartDate)
		if err == nil {
			cycle.StartDate = &t
		}
	}
	if req.EndDate != nil {
		t, err := time.Parse("2006-01-02", *req.EndDate)
		if err == nil {
			cycle.EndDate = &t
		}
	}

	if err := s.cycleRepo.Create(ctx, cycle); err != nil {
		return nil, err
	}

	return cycle, nil
}

func (s *CycleService) GetByID(ctx context.Context, id uuid.UUID) (*domain.Cycle, error) {
	return s.cycleRepo.GetByID(ctx, id)
}

func (s *CycleService) ListByTeam(ctx context.Context, teamID uuid.UUID) ([]domain.Cycle, error) {
	return s.cycleRepo.ListByTeam(ctx, teamID)
}

func (s *CycleService) Update(ctx context.Context, id uuid.UUID, req dto.UpdateCycleRequest) (*domain.Cycle, error) {
	cycle, err := s.cycleRepo.GetByID(ctx, id)
	if err != nil || cycle == nil {
		return nil, fmt.Errorf("cycle not found")
	}

	if req.Name != nil {
		cycle.Name = *req.Name
	}
	if req.Description != nil {
		cycle.Description = req.Description
	}
	if req.Status != nil {
		cycle.Status = domain.CycleStatus(*req.Status)
	}
	if req.StartDate != nil {
		t, err := time.Parse("2006-01-02", *req.StartDate)
		if err == nil {
			cycle.StartDate = &t
		}
	}
	if req.EndDate != nil {
		t, err := time.Parse("2006-01-02", *req.EndDate)
		if err == nil {
			cycle.EndDate = &t
		}
	}

	if err := s.cycleRepo.Update(ctx, cycle); err != nil {
		return nil, err
	}
	return cycle, nil
}

func (s *CycleService) Complete(ctx context.Context, id uuid.UUID) (*domain.Cycle, error) {
	cycle, err := s.cycleRepo.GetByID(ctx, id)
	if err != nil || cycle == nil {
		return nil, fmt.Errorf("cycle not found")
	}

	if cycle.Status == domain.CycleStatusCompleted {
		return nil, fmt.Errorf("cycle is already completed")
	}

	now := time.Now()
	cycle.Status = domain.CycleStatusCompleted
	cycle.CompletedAt = &now

	if err := s.cycleRepo.Update(ctx, cycle); err != nil {
		return nil, err
	}
	return cycle, nil
}

func (s *CycleService) Delete(ctx context.Context, id uuid.UUID) error {
	cycle, err := s.cycleRepo.GetByID(ctx, id)
	if err != nil || cycle == nil {
		return fmt.Errorf("cycle not found")
	}
	return s.cycleRepo.Delete(ctx, id)
}

func (s *CycleService) GetStats(ctx context.Context, cycleID uuid.UUID) (*dto.CycleProgressResponse, error) {
	total, completed, cancelled, err := s.cycleRepo.IssueStats(ctx, cycleID)
	if err != nil {
		return nil, err
	}
	return &dto.CycleProgressResponse{
		Total:     total,
		Completed: completed,
		Cancelled: cancelled,
	}, nil
}

func (s *CycleService) GetBurndown(ctx context.Context, cycleID uuid.UUID) ([]dto.BurndownPoint, error) {
	cycle, err := s.cycleRepo.GetByID(ctx, cycleID)
	if err != nil || cycle == nil {
		return nil, fmt.Errorf("cycle not found")
	}
	if cycle.StartDate == nil || cycle.EndDate == nil {
		return nil, fmt.Errorf("cycle must have start and end dates")
	}
	return s.cycleRepo.BurndownData(ctx, cycleID, *cycle.StartDate, *cycle.EndDate)
}
