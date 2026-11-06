package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
)

// ErrLabelProject is returned when a label's project does not exist in the workspace.
var ErrLabelProject = errors.New("project not found")

// ErrLabelNameTaken is returned when another label of the same scope has the name.
var ErrLabelNameTaken = errors.New("a label with this name already exists")

type LabelService struct {
	labelRepo   repository.LabelRepo
	projectRepo repository.ProjectRepo
}

func NewLabelService(labelRepo repository.LabelRepo, projectRepo repository.ProjectRepo) *LabelService {
	return &LabelService{labelRepo: labelRepo, projectRepo: projectRepo}
}

// workspaceProjectID parses a project_id and checks the project belongs to the workspace.
func (s *LabelService) workspaceProjectID(ctx context.Context, workspaceID uuid.UUID, raw string) (*uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return nil, ErrLabelProject
	}
	project, err := s.projectRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if project == nil || project.WorkspaceID != workspaceID {
		return nil, ErrLabelProject
	}
	return &id, nil
}

func (s *LabelService) Create(ctx context.Context, workspaceID uuid.UUID, req dto.CreateLabelRequest) (*domain.Label, error) {
	var projectID *uuid.UUID
	if req.ProjectID != nil {
		var err error
		if projectID, err = s.workspaceProjectID(ctx, workspaceID, *req.ProjectID); err != nil {
			return nil, err
		}
	}

	exists, err := s.labelRepo.ExistsByName(ctx, workspaceID, projectID, req.Name, uuid.Nil)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, ErrLabelNameTaken
	}

	label := &domain.Label{
		ID:          uuid.New(),
		WorkspaceID: workspaceID,
		Name:        req.Name,
		Color:       req.Color,
		Description: req.Description,
		ProjectID:   projectID,
	}
	if req.ParentID != nil {
		pid, _ := uuid.Parse(*req.ParentID)
		label.ParentID = &pid
	}
	if err := s.labelRepo.Create(ctx, label); err != nil {
		if errors.Is(err, repository.ErrLabelProjectNotVisible) {
			return nil, ErrLabelProject
		}
		if errors.Is(err, repository.ErrLabelNameTaken) {
			return nil, ErrLabelNameTaken
		}
		return nil, err
	}
	return label, nil
}

// ListByWorkspace lists the workspace's labels. With a project it lists the
// workspace-wide labels plus that project's own.
func (s *LabelService) ListByWorkspace(ctx context.Context, workspaceID uuid.UUID, rawProjectID *string) ([]domain.Label, error) {
	var projectID *uuid.UUID
	if rawProjectID != nil {
		var err error
		if projectID, err = s.workspaceProjectID(ctx, workspaceID, *rawProjectID); err != nil {
			return nil, err
		}
	}
	return s.labelRepo.ListByWorkspace(ctx, workspaceID, projectID)
}

func (s *LabelService) Update(ctx context.Context, workspaceID, id uuid.UUID, req dto.UpdateLabelRequest) (*domain.Label, error) {
	label, err := s.labelRepo.GetByID(ctx, id)
	if err != nil || label == nil {
		return nil, fmt.Errorf("label not found")
	}
	if label.WorkspaceID != workspaceID {
		return nil, fmt.Errorf("label not found")
	}

	if req.Name != nil {
		if *req.Name != label.Name {
			taken, err := s.labelRepo.ExistsByName(ctx, workspaceID, label.ProjectID, *req.Name, label.ID)
			if err != nil {
				return nil, err
			}
			if taken {
				return nil, ErrLabelNameTaken
			}
		}
		label.Name = *req.Name
	}
	if req.Color != nil {
		label.Color = *req.Color
	}
	if req.Description != nil {
		label.Description = req.Description
	}

	if err := s.labelRepo.Update(ctx, label); err != nil {
		if errors.Is(err, repository.ErrLabelNameTaken) {
			return nil, ErrLabelNameTaken
		}
		return nil, err
	}
	return label, nil
}

func (s *LabelService) Delete(ctx context.Context, workspaceID, id uuid.UUID) error {
	label, err := s.labelRepo.GetByID(ctx, id)
	if err != nil || label == nil {
		return fmt.Errorf("label not found")
	}
	if label.WorkspaceID != workspaceID {
		return fmt.Errorf("label not found")
	}
	return s.labelRepo.Delete(ctx, id)
}
