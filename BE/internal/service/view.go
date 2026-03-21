package service

import (
	"context"
	"fmt"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/google/uuid"
)

type ViewService struct {
	viewRepo repository.ViewRepo
}

func NewViewService(viewRepo repository.ViewRepo) *ViewService {
	return &ViewService{viewRepo: viewRepo}
}

func (s *ViewService) Create(ctx context.Context, workspaceID, creatorID uuid.UUID, req dto.CreateViewRequest) (*domain.View, error) {
	view := &domain.View{
		ID:          uuid.New(),
		WorkspaceID: workspaceID,
		CreatorID:   creatorID,
		Name:        req.Name,
		Description: req.Description,
		Filters:     req.Filters,
		IsShared:    req.IsShared,
	}

	if err := s.viewRepo.Create(ctx, view); err != nil {
		return nil, err
	}

	return view, nil
}

func (s *ViewService) GetByID(ctx context.Context, id uuid.UUID) (*domain.View, error) {
	return s.viewRepo.GetByID(ctx, id)
}

func (s *ViewService) List(ctx context.Context, workspaceID, userID uuid.UUID) ([]domain.View, error) {
	return s.viewRepo.ListByWorkspace(ctx, workspaceID, userID)
}

func (s *ViewService) Update(ctx context.Context, id, userID uuid.UUID, req dto.UpdateViewRequest) (*domain.View, error) {
	view, err := s.viewRepo.GetByID(ctx, id)
	if err != nil || view == nil {
		return nil, fmt.Errorf("view not found")
	}

	if view.CreatorID != userID {
		return nil, fmt.Errorf("only the creator can update this view")
	}

	if req.Name != nil {
		view.Name = *req.Name
	}
	if req.Description != nil {
		view.Description = req.Description
	}
	if req.Filters != nil {
		view.Filters = req.Filters
	}
	if req.IsShared != nil {
		view.IsShared = *req.IsShared
	}

	if err := s.viewRepo.Update(ctx, view); err != nil {
		return nil, err
	}

	return view, nil
}

func (s *ViewService) Delete(ctx context.Context, id, userID uuid.UUID) error {
	view, err := s.viewRepo.GetByID(ctx, id)
	if err != nil || view == nil {
		return fmt.Errorf("view not found")
	}

	if view.CreatorID != userID {
		return fmt.Errorf("only the creator can delete this view")
	}

	return s.viewRepo.Delete(ctx, id)
}
