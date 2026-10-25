package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
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

	var recurrence *json.RawMessage
	if len(req.RecurrenceRule) > 0 {
		recurrence = &req.RecurrenceRule
	}

	tmpl := &domain.IssueTemplate{
		ID:                uuid.New(),
		WorkspaceID:       workspaceID,
		Title:             req.Title,
		DefaultIssueTitle: req.DefaultIssueTitle,
		Description:       req.Description,
		Status:            req.Status,
		Priority:          req.Priority,
		LabelIDs:          labelIDsJSON,
		RecurrenceRule:    recurrence,
		IsActive:          true,
		CreatedBy:         creatorID,
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
		title := strings.TrimSpace(*req.Title)
		if title == "" || utf8.RuneCountInString(title) > 500 {
			return nil, fmt.Errorf("title must be between 1 and 500 characters")
		}
		tmpl.Title = title
	}
	if req.DefaultIssueTitle.Set {
		if req.DefaultIssueTitle.Value == nil || strings.TrimSpace(*req.DefaultIssueTitle.Value) == "" {
			tmpl.DefaultIssueTitle = nil
		} else {
			title := strings.TrimSpace(*req.DefaultIssueTitle.Value)
			if utf8.RuneCountInString(title) > 500 {
				return nil, fmt.Errorf("default issue title must be between 1 and 500 characters")
			}
			tmpl.DefaultIssueTitle = &title
		}
	}
	if req.Description != nil {
		tmpl.Description = req.Description
	}
	if req.Status.Set {
		if req.Status.Value != nil && !validTemplateStatus(*req.Status.Value) {
			return nil, fmt.Errorf("invalid template status")
		}
		tmpl.Status = req.Status.Value
	}
	if req.Priority.Set {
		if req.Priority.Value != nil && (*req.Priority.Value < 0 || *req.Priority.Value > 4) {
			return nil, fmt.Errorf("invalid template priority")
		}
		tmpl.Priority = req.Priority.Value
	}
	if req.TeamID.Set {
		if req.TeamID.Value == nil {
			tmpl.TeamID = nil
		} else {
			tid, err := uuid.Parse(*req.TeamID.Value)
			if err != nil {
				return nil, fmt.Errorf("invalid team ID")
			}
			tmpl.TeamID = &tid
		}
	}
	if req.AssigneeID.Set {
		if req.AssigneeID.Value == nil {
			tmpl.AssigneeID = nil
		} else {
			aid, err := uuid.Parse(*req.AssigneeID.Value)
			if err != nil {
				return nil, fmt.Errorf("invalid assignee ID")
			}
			tmpl.AssigneeID = &aid
		}
	}
	if req.LabelIDs != nil {
		labelIDsJSON, _ := json.Marshal(req.LabelIDs)
		tmpl.LabelIDs = labelIDsJSON
	}
	if len(req.RecurrenceRule) > 0 {
		tmpl.RecurrenceRule = &req.RecurrenceRule
	}
	if req.IsActive != nil {
		tmpl.IsActive = *req.IsActive
	}

	if err := s.templateRepo.Update(ctx, tmpl); err != nil {
		return nil, err
	}
	return tmpl, nil
}

func validTemplateStatus(status string) bool {
	switch status {
	case "backlog", "todo", "in_progress", "in_review", "done", "cancelled":
		return true
	default:
		return false
	}
}

func (s *IssueTemplateService) Delete(ctx context.Context, id uuid.UUID) error {
	return s.templateRepo.Delete(ctx, id)
}
