package service

import (
	"context"

	"github.com/carbon/carbon-backend/internal/domain"
	"github.com/carbon/carbon-backend/internal/dto"
	"github.com/carbon/carbon-backend/internal/repository"
	"github.com/google/uuid"
)

type NotificationService struct {
	notifRepo repository.NotificationRepo
}

func NewNotificationService(notifRepo repository.NotificationRepo) *NotificationService {
	return &NotificationService{notifRepo: notifRepo}
}

func (s *NotificationService) Create(ctx context.Context, userID, workspaceID uuid.UUID, issueID *uuid.UUID, notifType, title string) error {
	n := &domain.Notification{
		ID:          uuid.New(),
		UserID:      userID,
		WorkspaceID: workspaceID,
		IssueID:     issueID,
		Type:        notifType,
		Title:       title,
	}
	return s.notifRepo.Create(ctx, n)
}

func (s *NotificationService) ListByUser(ctx context.Context, userID uuid.UUID, limit, offset int) ([]domain.Notification, error) {
	return s.notifRepo.ListByUser(ctx, userID, limit, offset)
}

func (s *NotificationService) Update(ctx context.Context, id uuid.UUID, req dto.UpdateNotificationRequest) (*domain.Notification, error) {
	n, err := s.notifRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	n.ReadAt = req.ReadAt
	n.SnoozedUntil = req.SnoozedUntil
	n.ArchivedAt = req.ArchivedAt

	if err := s.notifRepo.Update(ctx, n); err != nil {
		return nil, err
	}
	return n, nil
}

func (s *NotificationService) MarkAllRead(ctx context.Context, userID uuid.UUID) error {
	return s.notifRepo.MarkAllRead(ctx, userID)
}
