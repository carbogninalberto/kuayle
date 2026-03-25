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
	exists, err := s.cycleRepo.ExistsByName(ctx, teamID, req.Name)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, fmt.Errorf("a cycle with this name already exists")
	}

	startDate, err := time.Parse("2006-01-02", req.StartDate)
	if err != nil {
		return nil, fmt.Errorf("invalid start_date format, expected YYYY-MM-DD")
	}
	endDate, err := time.Parse("2006-01-02", req.EndDate)
	if err != nil {
		return nil, fmt.Errorf("invalid end_date format, expected YYYY-MM-DD")
	}
	if !startDate.Before(endDate) {
		return nil, fmt.Errorf("start_date must be before end_date")
	}

	overlap, err := s.cycleRepo.HasOverlap(ctx, teamID, startDate, endDate, nil)
	if err != nil {
		return nil, err
	}
	if overlap {
		return nil, fmt.Errorf("cycle dates overlap with an existing cycle")
	}

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
		Goals:       req.Goals,
		StartDate:   &startDate,
		EndDate:     &endDate,
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
	if req.Goals != nil {
		cycle.Goals = req.Goals
	}
	if req.Retrospective != nil {
		cycle.Retrospective = req.Retrospective
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

	// Validate dates don't overlap with other cycles
	if cycle.StartDate != nil && cycle.EndDate != nil {
		if !cycle.StartDate.Before(*cycle.EndDate) {
			return nil, fmt.Errorf("start_date must be before end_date")
		}
		overlap, err := s.cycleRepo.HasOverlap(ctx, cycle.TeamID, *cycle.StartDate, *cycle.EndDate, &id)
		if err != nil {
			return nil, err
		}
		if overlap {
			return nil, fmt.Errorf("cycle dates overlap with an existing cycle")
		}
	}

	if err := s.cycleRepo.Update(ctx, cycle); err != nil {
		return nil, err
	}
	return cycle, nil
}

func (s *CycleService) Complete(ctx context.Context, id uuid.UUID, req dto.CompleteCycleRequest) (*domain.Cycle, int, error) {
	cycle, err := s.cycleRepo.GetByID(ctx, id)
	if err != nil || cycle == nil {
		return nil, 0, fmt.Errorf("cycle not found")
	}

	if cycle.Status == domain.CycleStatusCompleted {
		return nil, 0, fmt.Errorf("cycle is already completed")
	}

	now := time.Now()
	cycle.Status = domain.CycleStatusCompleted
	cycle.CompletedAt = &now

	if req.Retrospective != nil {
		cycle.Retrospective = req.Retrospective
	}

	carriedOver := 0
	if req.CarryOver {
		next, err := s.cycleRepo.GetNextUpcoming(ctx, cycle.TeamID)
		if err != nil {
			return nil, 0, err
		}
		if next != nil {
			carriedOver, err = s.cycleRepo.CarryOverIssues(ctx, id, next.ID)
			if err != nil {
				return nil, 0, err
			}
		}
	}

	if err := s.cycleRepo.Update(ctx, cycle); err != nil {
		return nil, 0, err
	}
	return cycle, carriedOver, nil
}

func (s *CycleService) GetVelocity(ctx context.Context, teamID uuid.UUID) ([]dto.VelocityPoint, error) {
	return s.cycleRepo.VelocityData(ctx, teamID, 20)
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
