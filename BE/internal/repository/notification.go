package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

type NotificationRepository struct {
	db *sqlx.DB
}

func NewNotificationRepository(db *sqlx.DB) *NotificationRepository {
	return &NotificationRepository{db: db}
}

func (r *NotificationRepository) Create(ctx context.Context, n *domain.Notification) error {
	if err := r.prepareDelivery(ctx, n); err != nil {
		return err
	}
	query := `INSERT INTO notifications (id, user_id, workspace_id, issue_id, type, title, source_team_ids) VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING created_at`
	return r.db.QueryRowContext(ctx, query, n.ID, n.UserID, n.WorkspaceID, n.IssueID, n.Type, n.Title, n.SourceTeamIDs).Scan(&n.CreatedAt)
}

func (r *NotificationRepository) CreateOrRefresh(ctx context.Context, n *domain.Notification, window time.Duration) error {
	if err := r.prepareDelivery(ctx, n); err != nil {
		return err
	}
	query := `
		WITH refreshed AS (
			UPDATE notifications
			SET title = $1, source_team_ids = $7, read_at = NULL, created_at = NOW()
			WHERE id = (
				SELECT id FROM notifications
				WHERE user_id = $2
					AND workspace_id = $3
					AND issue_id IS NOT DISTINCT FROM $4::uuid
					AND source_team_ids IS NOT DISTINCT FROM $7::uuid[]
					AND type = $5
					AND archived_at IS NULL
					AND created_at >= NOW() - ($6 * INTERVAL '1 second')
				ORDER BY created_at DESC
				LIMIT 1
			)
			RETURNING id, created_at
		)
		SELECT id, created_at FROM refreshed`
	err := r.db.QueryRowContext(ctx, query, n.Title, n.UserID, n.WorkspaceID, n.IssueID, n.Type, int(window.Seconds()), n.SourceTeamIDs).Scan(&n.ID, &n.CreatedAt)
	if err == nil {
		return nil
	}
	if err != sql.ErrNoRows {
		return err
	}
	return r.Create(ctx, n)
}

// Delivery and every subsequent read use the recipient's live membership. The
// triggering actor's access cannot authorize another user's notification.
func (r *NotificationRepository) prepareDelivery(ctx context.Context, n *domain.Notification) error {
	if n.SourceTeamIDs == nil && n.IssueID != nil {
		if err := r.db.GetContext(ctx, &n.SourceTeamIDs, `SELECT ARRAY[team_id] FROM issues WHERE id=$1 AND workspace_id=$2`, n.IssueID, n.WorkspaceID); err != nil {
			return err
		}
	}
	if len(n.SourceTeamIDs) == 0 {
		// New content notices must retain provenance. Only legacy reads use the
		// conservative fallback for untraceable historical rows.
		return sql.ErrNoRows
	}
	recipient := domain.WithAccess(ctx, domain.Access{UserID: n.UserID, WorkspaceID: n.WorkspaceID})
	var allowed int
	return r.db.GetContext(recipient, &allowed, `SELECT 1 FROM (SELECT $1::uuid AS user_id, $2::uuid AS workspace_id, $3::uuid AS issue_id, $4::uuid[] AS source_team_ids) n WHERE `+notificationVisible(recipient, "n"), n.UserID, n.WorkspaceID, n.IssueID, n.SourceTeamIDs)
}

func notificationVisible(ctx context.Context, alias string) string {
	actor, ok := domain.AccessFromContext(ctx)
	if !ok || actor.UserID == uuid.Nil {
		return "FALSE"
	}
	return "(" + alias + ".user_id = " + uuidSQL(actor.UserID) + " AND " + workspaceVisible(ctx, alias+".workspace_id") +
		" AND (" + alias + ".issue_id IS NULL OR " + issueVisible(ctx, alias+".issue_id") + ") AND CASE WHEN " + alias + ".source_team_ids IS NOT NULL THEN NOT EXISTS (SELECT 1 FROM unnest(" + alias + ".source_team_ids) source_team(id) WHERE NOT (" + teamVisible(ctx, "source_team.id") + " AND EXISTS (SELECT 1 FROM teams origin_team WHERE origin_team.id=source_team.id AND origin_team.workspace_id=" + alias + ".workspace_id)))" +
		" WHEN " + alias + ".issue_id IS NOT NULL THEN " + issueVisible(ctx, alias+".issue_id") +
		" ELSE NOT EXISTS (SELECT 1 FROM teams legacy_team WHERE legacy_team.workspace_id=" + alias + ".workspace_id AND legacy_team.is_private) END)"
}

func (r *NotificationRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Notification, error) {
	var n domain.Notification
	err := r.db.GetContext(ctx, &n, `SELECT * FROM notifications WHERE id = $1 AND `+notificationVisible(ctx, "notifications"), id)
	if err != nil {
		return nil, err
	}
	return &n, nil
}

const notifSelectCols = `n.id, n.user_id, n.workspace_id, n.issue_id, n.type, n.title, n.source_team_ids, n.read_at, n.snoozed_until, n.archived_at, n.created_at, i.identifier_text AS issue_identifier`

func (r *NotificationRepository) ListByUser(ctx context.Context, userID uuid.UUID, limit, offset int) ([]domain.Notification, error) {
	var notifications []domain.Notification
	err := r.db.SelectContext(ctx, &notifications,
		`SELECT `+notifSelectCols+` FROM notifications n LEFT JOIN issues i ON n.issue_id = i.id WHERE n.user_id = $1 AND `+notificationVisible(ctx, "n")+` AND n.archived_at IS NULL AND (n.snoozed_until IS NULL OR n.snoozed_until <= NOW()) ORDER BY n.created_at DESC LIMIT $2 OFFSET $3`,
		userID, limit, offset)
	return notifications, err
}

func (r *NotificationRepository) ListSnoozed(ctx context.Context, userID uuid.UUID) ([]domain.Notification, error) {
	var notifications []domain.Notification
	err := r.db.SelectContext(ctx, &notifications,
		`SELECT `+notifSelectCols+` FROM notifications n LEFT JOIN issues i ON n.issue_id = i.id WHERE n.user_id = $1 AND `+notificationVisible(ctx, "n")+` AND n.snoozed_until IS NOT NULL AND n.snoozed_until > NOW() AND n.archived_at IS NULL ORDER BY n.snoozed_until ASC`,
		userID)
	return notifications, err
}

func (r *NotificationRepository) ListArchived(ctx context.Context, userID uuid.UUID, limit int) ([]domain.Notification, error) {
	var notifications []domain.Notification
	err := r.db.SelectContext(ctx, &notifications,
		`SELECT `+notifSelectCols+` FROM notifications n LEFT JOIN issues i ON n.issue_id = i.id WHERE n.user_id = $1 AND `+notificationVisible(ctx, "n")+` AND n.archived_at IS NOT NULL ORDER BY n.archived_at DESC LIMIT $2`,
		userID, limit)
	return notifications, err
}

func (r *NotificationRepository) Update(ctx context.Context, n *domain.Notification) error {
	query := `UPDATE notifications SET read_at = $1, snoozed_until = $2, archived_at = $3 WHERE id = $4 AND ` + notificationVisible(ctx, "notifications")
	_, err := r.db.ExecContext(ctx, query, n.ReadAt, n.SnoozedUntil, n.ArchivedAt, n.ID)
	return err
}

func (r *NotificationRepository) MarkAllRead(ctx context.Context, userID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `UPDATE notifications SET read_at = NOW() WHERE user_id = $1 AND `+notificationVisible(ctx, "notifications")+` AND read_at IS NULL`, userID)
	return err
}

func (r *NotificationRepository) UnreadCount(ctx context.Context, userID uuid.UUID) (int, error) {
	var count int
	err := r.db.GetContext(ctx, &count,
		`SELECT COUNT(*) FROM notifications WHERE user_id = $1 AND `+notificationVisible(ctx, "notifications")+` AND read_at IS NULL AND archived_at IS NULL AND (snoozed_until IS NULL OR snoozed_until <= NOW())`,
		userID)
	return count, err
}
