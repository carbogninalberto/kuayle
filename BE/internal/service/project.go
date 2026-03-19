package service

import (
	"context"
	"fmt"

	"github.com/carbon/carbon-backend/internal/domain"
	"github.com/carbon/carbon-backend/internal/dto"
	"github.com/carbon/carbon-backend/internal/repository"
	"github.com/google/uuid"
)

type ProjectService struct {
	projectRepo repository.ProjectRepo
}

func NewProjectService(projectRepo repository.ProjectRepo) *ProjectService {
	return &ProjectService{projectRepo: projectRepo}
}

func (s *ProjectService) Create(ctx context.Context, workspaceID uuid.UUID, req dto.CreateProjectRequest) (*domain.Project, error) {
	project := &domain.Project{
		ID:          uuid.New(),
		WorkspaceID: workspaceID,
		Name:        req.Name,
		Description: req.Description,
		Status:      domain.ProjectStatusPlanned,
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

func (s *ProjectService) Update(ctx context.Context, id uuid.UUID, req dto.UpdateProjectRequest) (*domain.Project, error) {
	project, err := s.projectRepo.GetByID(ctx, id)
	if err != nil || project == nil {
		return nil, fmt.Errorf("project not found")
	}

	if req.Name != nil {
		project.Name = *req.Name
	}
	if req.Description != nil {
		project.Description = req.Description
	}
	if req.Status != nil {
		project.Status = domain.ProjectStatus(*req.Status)
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
