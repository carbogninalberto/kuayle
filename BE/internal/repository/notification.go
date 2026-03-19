package repository

import (
	"context"

	"github.com/carbon/carbon-backend/internal/domain"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

type NotificationRepository struct {
	db *sqlx.DB
}

func NewNotificationRepository(db *sqlx.DB) *NotificationRepository {
	return &NotificationRepository{db: db}
}

func (r *NotificationRepository) Create(ctx context.Context, n *domain.Notification) error {
	query := `INSERT INTO notifications (id, user_id, workspace_id, issue_id, type, title) VALUES ($1, $2, $3, $4, $5, $6) RETURNING created_at`
	return r.db.QueryRowContext(ctx, query, n.ID, n.UserID, n.WorkspaceID, n.IssueID, n.Type, n.Title).Scan(&n.CreatedAt)
}

func (r *NotificationRepository) ListByUser(ctx context.Context, userID uuid.UUID, limit, offset int) ([]domain.Notification, error) {
	var notifications []domain.Notification
	err := r.db.SelectContext(ctx, &notifications, `SELECT * FROM notifications WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`, userID, limit, offset)
	return notifications, err
}

func (r *NotificationRepository) Update(ctx context.Context, n *domain.Notification) error {
	query := `UPDATE notifications SET read_at = $1, snoozed_until = $2, archived_at = $3 WHERE id = $4`
	_, err := r.db.ExecContext(ctx, query, n.ReadAt, n.SnoozedUntil, n.ArchivedAt, n.ID)
	return err
}

func (r *NotificationRepository) MarkAllRead(ctx context.Context, userID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `UPDATE notifications SET read_at = NOW() WHERE user_id = $1 AND read_at IS NULL`, userID)
	return err
}

func (r *NotificationRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Notification, error) {
	var n domain.Notification
	err := r.db.GetContext(ctx, &n, `SELECT * FROM notifications WHERE id = $1`, id)
	if err != nil {
		return nil, err
	}
	return &n, nil
}
