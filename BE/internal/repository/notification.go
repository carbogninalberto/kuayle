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

func (r *NotificationRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Notification, error) {
	var n domain.Notification
	err := r.db.GetContext(ctx, &n, `SELECT * FROM notifications WHERE id = $1`, id)
	if err != nil {
		return nil, err
	}
	return &n, nil
}

func (r *NotificationRepository) ListByUser(ctx context.Context, userID uuid.UUID, limit, offset int) ([]domain.Notification, error) {
	var notifications []domain.Notification
	// Default list: exclude archived and currently-snoozed
	err := r.db.SelectContext(ctx, &notifications,
		`SELECT * FROM notifications WHERE user_id = $1 AND archived_at IS NULL AND (snoozed_until IS NULL OR snoozed_until <= NOW()) ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
		userID, limit, offset)
	return notifications, err
}

func (r *NotificationRepository) ListSnoozed(ctx context.Context, userID uuid.UUID) ([]domain.Notification, error) {
	var notifications []domain.Notification
	err := r.db.SelectContext(ctx, &notifications,
		`SELECT * FROM notifications WHERE user_id = $1 AND snoozed_until IS NOT NULL AND snoozed_until > NOW() AND archived_at IS NULL ORDER BY snoozed_until ASC`,
		userID)
	return notifications, err
}

func (r *NotificationRepository) ListArchived(ctx context.Context, userID uuid.UUID, limit int) ([]domain.Notification, error) {
	var notifications []domain.Notification
	err := r.db.SelectContext(ctx, &notifications,
		`SELECT * FROM notifications WHERE user_id = $1 AND archived_at IS NOT NULL ORDER BY archived_at DESC LIMIT $2`,
		userID, limit)
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

func (r *NotificationRepository) UnreadCount(ctx context.Context, userID uuid.UUID) (int, error) {
	var count int
	err := r.db.GetContext(ctx, &count,
		`SELECT COUNT(*) FROM notifications WHERE user_id = $1 AND read_at IS NULL AND archived_at IS NULL AND (snoozed_until IS NULL OR snoozed_until <= NOW())`,
		userID)
	return count, err
}
