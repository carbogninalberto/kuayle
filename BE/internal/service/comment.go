package service

import (
	"context"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/sanitize"
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
	if req.ParentID != nil {
		pid, err := uuid.Parse(*req.ParentID)
		if err != nil {
			return nil, err
		}
		comment.ParentID = &pid
	}
	if err := s.commentRepo.Create(ctx, comment); err != nil {
		return nil, err
	}
	return comment, nil
}

func (s *CommentService) ListByIssue(ctx context.Context, issueID uuid.UUID) ([]domain.Comment, error) {
	return s.commentRepo.ListByIssue(ctx, issueID)
}

func (s *CommentService) ListReplies(ctx context.Context, parentID uuid.UUID) ([]domain.Comment, error) {
	return s.commentRepo.ListReplies(ctx, parentID)
}

func (s *CommentService) GetByID(ctx context.Context, id uuid.UUID) (*domain.Comment, error) {
	return s.commentRepo.GetByID(ctx, id)
}

func (s *CommentService) Resolve(ctx context.Context, id uuid.UUID) error {
	return s.commentRepo.Resolve(ctx, id)
}

func (s *CommentService) Reopen(ctx context.Context, id uuid.UUID) error {
	return s.commentRepo.Reopen(ctx, id)
}
