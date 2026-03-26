package dto

import "time"

type UpdateNotificationRequest struct {
	ReadAt       *time.Time `json:"read_at"`
	SnoozedUntil *time.Time `json:"snoozed_until"`
	ArchivedAt   *time.Time `json:"archived_at"`
}

type NotificationResponse struct {
	ID              string     `json:"id"`
	IssueID         *string    `json:"issue_id"`
	IssueIdentifier *string    `json:"issue_identifier"`
	Type            string     `json:"type"`
	Title           string     `json:"title"`
	ReadAt          *time.Time `json:"read_at"`
	SnoozedUntil    *time.Time `json:"snoozed_until"`
	ArchivedAt      *time.Time `json:"archived_at"`
	CreatedAt       time.Time  `json:"created_at"`
}

type NotificationListResponse struct {
	Notifications []NotificationResponse `json:"notifications"`
	UnreadCount   int                    `json:"unread_count"`
}
