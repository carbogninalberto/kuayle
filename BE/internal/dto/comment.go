package dto

import "time"

type CreateCommentRequest struct {
	Body     string  `json:"body" validate:"required,min=1"`
	ParentID *string `json:"parent_id" validate:"omitempty,uuid"`
}

type UpdateCommentRequest struct {
	Body string `json:"body" validate:"required,min=1"`
}

type CommentResponse struct {
	ID         string            `json:"id"`
	IssueID    string            `json:"issue_id"`
	UserID     string            `json:"user_id"`
	Body       string            `json:"body"`
	ParentID   *string           `json:"parent_id,omitempty"`
	ResolvedAt *time.Time        `json:"resolved_at,omitempty"`
	User       *UserResponse     `json:"user,omitempty"`
	Replies    []CommentResponse `json:"replies,omitempty"`
	CreatedAt  time.Time         `json:"created_at"`
	UpdatedAt  time.Time         `json:"updated_at"`
}
