package dto

import "time"

type CreateCommentRequest struct {
	Body string `json:"body" validate:"required,min=1"`
}

type UpdateCommentRequest struct {
	Body string `json:"body" validate:"required,min=1"`
}

type CommentResponse struct {
	ID        string        `json:"id"`
	IssueID   string        `json:"issue_id"`
	UserID    string        `json:"user_id"`
	Body      string        `json:"body"`
	User      *UserResponse `json:"user,omitempty"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
}
