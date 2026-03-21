package service

import (
	"context"

	"github.com/carbon/carbon-backend/internal/domain"
	"github.com/carbon/carbon-backend/internal/dto"
	"github.com/carbon/carbon-backend/internal/repository"
	"github.com/carbon/carbon-backend/pkg/sanitize"
	"github.com/google/uuid"
)

type CommentService struct {
	commentRepo repository.CommentRepo
}

func NewCommentService(commentRepo repository.CommentRepo) *CommentService {
	return &CommentService{commentRepo: commentRepo}
}

func (s *CommentService) Create(ctx context.Context, issueID, userID uuid.UUID, req dto.CreateCommentRequest) (*domain.Comment, error) {
	// Sanitize HTML in comment body
	req.Body = sanitize.SanitizeHTML(req.Body)

	comment := &domain.Comment{
		ID:      uuid.New(),
		IssueID: issueID,
		UserID:  userID,
		Body:    req.Body,
	}
	if err := s.commentRepo.Create(ctx, comment); err != nil {
		return nil, err
	}
	return comment, nil
}

func (s *CommentService) ListByIssue(ctx context.Context, issueID uuid.UUID) ([]domain.Comment, error) {
	return s.commentRepo.ListByIssue(ctx, issueID)
}
