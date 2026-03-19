package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/carbon/carbon-backend/internal/domain"
	"github.com/carbon/carbon-backend/internal/dto"
	"github.com/carbon/carbon-backend/internal/repository"
	"github.com/google/uuid"
)

type IssueTemplateService struct {
	templateRepo repository.IssueTemplateRepo
}

func NewIssueTemplateService(templateRepo repository.IssueTemplateRepo) *IssueTemplateService {
	return &IssueTemplateService{templateRepo: templateRepo}
}

func (s *IssueTemplateService) Create(ctx context.Context, workspaceID, creatorID uuid.UUID, req dto.CreateIssueTemplateRequest) (*domain.IssueTemplate, error) {
	labelIDsJSON, _ := json.Marshal(req.LabelIDs)
	if req.LabelIDs == nil {
		labelIDsJSON = []byte("[]")
	}

	tmpl := &domain.IssueTemplate{
		ID:             uuid.New(),
		WorkspaceID:    workspaceID,
		Title:          req.Title,
		Description:    req.Description,
		Status:         req.Status,
		Priority:       req.Priority,
		LabelIDs:       labelIDsJSON,
		RecurrenceRule: req.RecurrenceRule,
		IsActive:       true,
		CreatedBy:      creatorID,
	}

	if req.TeamID != nil {
		tid, _ := uuid.Parse(*req.TeamID)
		tmpl.TeamID = &tid
	}
	if req.AssigneeID != nil {
		aid, _ := uuid.Parse(*req.AssigneeID)
		tmpl.AssigneeID = &aid
	}

	if err := s.templateRepo.Create(ctx, tmpl); err != nil {
		return nil, err
	}
	return tmpl, nil
}

func (s *IssueTemplateService) GetByID(ctx context.Context, id uuid.UUID) (*domain.IssueTemplate, error) {
	return s.templateRepo.GetByID(ctx, id)
}

func (s *IssueTemplateService) ListByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]domain.IssueTemplate, error) {
	return s.templateRepo.ListByWorkspace(ctx, workspaceID)
}

func (s *IssueTemplateService) Update(ctx context.Context, id uuid.UUID, req dto.UpdateIssueTemplateRequest) (*domain.IssueTemplate, error) {
	tmpl, err := s.templateRepo.GetByID(ctx, id)
	if err != nil || tmpl == nil {
		return nil, fmt.Errorf("template not found")
	}

	if req.Title != nil {
		tmpl.Title = *req.Title
	}
	if req.Description != nil {
		tmpl.Description = req.Description
	}
	if req.Status != nil {
		tmpl.Status = req.Status
	}
	if req.Priority != nil {
		tmpl.Priority = req.Priority
	}
	if req.TeamID != nil {
		tid, _ := uuid.Parse(*req.TeamID)
		tmpl.TeamID = &tid
	}
	if req.AssigneeID != nil {
		aid, _ := uuid.Parse(*req.AssigneeID)
		tmpl.AssigneeID = &aid
	}
	if req.LabelIDs != nil {
		labelIDsJSON, _ := json.Marshal(req.LabelIDs)
		tmpl.LabelIDs = labelIDsJSON
	}
	if req.RecurrenceRule != nil {
		tmpl.RecurrenceRule = req.RecurrenceRule
	}
	if req.IsActive != nil {
		tmpl.IsActive = *req.IsActive
	}

	if err := s.templateRepo.Update(ctx, tmpl); err != nil {
		return nil, err
	}
	return tmpl, nil
}

func (s *IssueTemplateService) Delete(ctx context.Context, id uuid.UUID) error {
	return s.templateRepo.Delete(ctx, id)
}
