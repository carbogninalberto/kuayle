package service

import (
	"context"
	"fmt"

	"github.com/carbon/carbon-backend/internal/domain"
	"github.com/carbon/carbon-backend/internal/dto"
	"github.com/carbon/carbon-backend/internal/repository"
	"github.com/carbon/carbon-backend/pkg/sanitize"
	"github.com/google/uuid"
)

type ProjectService struct {
	projectRepo repository.ProjectRepo
}

func NewProjectService(projectRepo repository.ProjectRepo) *ProjectService {
	return &ProjectService{projectRepo: projectRepo}
}

func (s *ProjectService) Create(ctx context.Context, workspaceID uuid.UUID, req dto.CreateProjectRequest) (*domain.Project, error) {
	req.Name = sanitize.StripHTML(req.Name)
	if req.Description != nil {
		clean := sanitize.SanitizeHTML(*req.Description)
		req.Description = &clean
	}

	project := &domain.Project{
		ID:          uuid.New(),
		WorkspaceID: workspaceID,
		Name:        req.Name,
		Description: req.Description,
		Status:      domain.ProjectStatusPlanned,
	}
	if req.TeamID != nil {
		tid, _ := uuid.Parse(*req.TeamID)
		project.TeamID = &tid
	}
	if req.LeadID != nil {
		lid, _ := uuid.Parse(*req.LeadID)
		project.LeadID = &lid
	}
	if err := s.projectRepo.Create(ctx, project); err != nil {
		return nil, err
	}
	return project, nil
}

func (s *ProjectService) GetByID(ctx context.Context, id uuid.UUID) (*domain.Project, error) {
	return s.projectRepo.GetByID(ctx, id)
}

func (s *ProjectService) ListByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]domain.Project, error) {
	return s.projectRepo.ListByWorkspace(ctx, workspaceID)
}

func (s *ProjectService) ListByTeam(ctx context.Context, teamID uuid.UUID) ([]domain.Project, error) {
	return s.projectRepo.ListByTeam(ctx, teamID)
}

func (s *ProjectService) Update(ctx context.Context, workspaceID, id uuid.UUID, req dto.UpdateProjectRequest) (*domain.Project, error) {
	project, err := s.projectRepo.GetByID(ctx, id)
	if err != nil || project == nil {
		return nil, fmt.Errorf("project not found")
	}
	if project.WorkspaceID != workspaceID {
		return nil, fmt.Errorf("project not found")
	}

	if req.Name != nil {
		clean := sanitize.StripHTML(*req.Name)
		req.Name = &clean
		project.Name = clean
	}
	if req.Description != nil {
		clean := sanitize.SanitizeHTML(*req.Description)
		req.Description = &clean
		project.Description = req.Description
	}
	if req.Status != nil {
		project.Status = domain.ProjectStatus(*req.Status)
	}
	if req.TeamID != nil {
		tid, _ := uuid.Parse(*req.TeamID)
		project.TeamID = &tid
	}
	if req.LeadID != nil {
		lid, _ := uuid.Parse(*req.LeadID)
		project.LeadID = &lid
	}
	if req.SortOrder != nil {
		project.SortOrder = *req.SortOrder
	}

	if err := s.projectRepo.Update(ctx, project); err != nil {
		return nil, err
	}
	return project, nil
}

func (s *ProjectService) Delete(ctx context.Context, workspaceID, id uuid.UUID) error {
	project, err := s.projectRepo.GetByID(ctx, id)
	if err != nil || project == nil {
		return fmt.Errorf("project not found")
	}
	if project.WorkspaceID != workspaceID {
		return fmt.Errorf("project not found")
	}
	return s.projectRepo.Delete(ctx, id)
}

func (s *ProjectService) GetStats(ctx context.Context, projectID uuid.UUID) (*dto.ProjectProgressResponse, error) {
	total, completed, cancelled, err := s.projectRepo.IssueStats(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return &dto.ProjectProgressResponse{
		Total:     total,
		Completed: completed,
		Cancelled: cancelled,
	}, nil
}
