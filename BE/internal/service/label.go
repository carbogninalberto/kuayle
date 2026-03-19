package service

import (
	"context"
	"fmt"

	"github.com/carbon/carbon-backend/internal/domain"
	"github.com/carbon/carbon-backend/internal/dto"
	"github.com/carbon/carbon-backend/internal/repository"
	"github.com/google/uuid"
)

type LabelService struct {
	labelRepo repository.LabelRepo
}

func NewLabelService(labelRepo repository.LabelRepo) *LabelService {
	return &LabelService{labelRepo: labelRepo}
}

func (s *LabelService) Create(ctx context.Context, workspaceID uuid.UUID, req dto.CreateLabelRequest) (*domain.Label, error) {
	label := &domain.Label{
		ID:          uuid.New(),
		WorkspaceID: workspaceID,
		Name:        req.Name,
		Color:       req.Color,
		Description: req.Description,
	}
	if req.ParentID != nil {
		pid, _ := uuid.Parse(*req.ParentID)
		label.ParentID = &pid
	}
	if err := s.labelRepo.Create(ctx, label); err != nil {
		return nil, err
	}
	return label, nil
}

func (s *LabelService) ListByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]domain.Label, error) {
	return s.labelRepo.ListByWorkspace(ctx, workspaceID)
}

func (s *LabelService) Update(ctx context.Context, id uuid.UUID, req dto.UpdateLabelRequest) (*domain.Label, error) {
	label, err := s.labelRepo.GetByID(ctx, id)
	if err != nil || label == nil {
		return nil, fmt.Errorf("label not found")
	}

	if req.Name != nil {
		label.Name = *req.Name
	}
	if req.Color != nil {
		label.Color = *req.Color
	}
	if req.Description != nil {
		label.Description = req.Description
	}

	if err := s.labelRepo.Update(ctx, label); err != nil {
		return nil, err
	}
	return label, nil
}

func (s *LabelService) Delete(ctx context.Context, id uuid.UUID) error {
	return s.labelRepo.Delete(ctx, id)
}
