package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/realtime"
	"github.com/kuayle/kuayle-backend/internal/repository"
)

type ViewService struct {
	viewRepo repository.ViewRepo
	hub      *realtime.Hub
}

func NewViewService(viewRepo repository.ViewRepo, hub ...*realtime.Hub) *ViewService {
	var h *realtime.Hub
	if len(hub) > 0 {
		h = hub[0]
	}
	return &ViewService{viewRepo: viewRepo, hub: h}
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

	s.broadcastChange(workspaceID, creatorID, "view.created", view.ID, view.IsShared)

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
	wasShared := view.IsShared

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

	s.broadcastChange(view.WorkspaceID, userID, "view.updated", view.ID, wasShared || view.IsShared)

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

	if err := s.viewRepo.Delete(ctx, id); err != nil {
		return err
	}

	s.broadcastChange(view.WorkspaceID, userID, "view.deleted", view.ID, view.IsShared)

	return nil
}

func (s *ViewService) broadcastChange(workspaceID, userID uuid.UUID, eventType string, viewID uuid.UUID, broadcastWorkspace bool) {
	if s.hub == nil {
		return
	}

	event := realtime.Event{
		Type: eventType,
		Payload: map[string]string{
			"id": viewID.String(),
		},
	}

	if broadcastWorkspace {
		s.hub.Broadcast(workspaceID, event)
		return
	}

	s.hub.BroadcastToUser(workspaceID, userID, event)
}
