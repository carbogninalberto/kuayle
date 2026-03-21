package service

import (
	"context"
	"fmt"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/google/uuid"
)

type IssueRelationService struct {
	relationRepo repository.IssueRelationRepo
	issueRepo    repository.IssueRepo
}

func NewIssueRelationService(relationRepo repository.IssueRelationRepo, issueRepo repository.IssueRepo) *IssueRelationService {
	return &IssueRelationService{relationRepo: relationRepo, issueRepo: issueRepo}
}

func (s *IssueRelationService) Create(ctx context.Context, workspaceID uuid.UUID, issueIdentifier string, req dto.CreateIssueRelationRequest) (*domain.IssueRelation, error) {
	issue, err := s.issueRepo.GetByIdentifier(ctx, workspaceID, issueIdentifier)
	if err != nil || issue == nil {
		return nil, fmt.Errorf("issue not found")
	}

	relatedIssue, err := s.issueRepo.GetByIdentifier(ctx, workspaceID, req.RelatedIdentifier)
	if err != nil || relatedIssue == nil {
		return nil, fmt.Errorf("related issue not found")
	}

	if issue.ID == relatedIssue.ID {
		return nil, fmt.Errorf("cannot relate an issue to itself")
	}

	relType := domain.IssueRelationType(req.Type)

	rel := &domain.IssueRelation{
		ID:             uuid.New(),
		IssueID:        issue.ID,
		RelatedIssueID: relatedIssue.ID,
		Type:           relType,
	}

	if err := s.relationRepo.Create(ctx, rel); err != nil {
		return nil, err
	}

	// Create inverse relation for blocking/blocked_by
	inverse := relType.Inverse()
	if inverse != relType {
		inverseRel := &domain.IssueRelation{
			ID:             uuid.New(),
			IssueID:        relatedIssue.ID,
			RelatedIssueID: issue.ID,
			Type:           inverse,
		}
		// Best-effort inverse creation
		_ = s.relationRepo.Create(ctx, inverseRel)
	}

	return rel, nil
}

func (s *IssueRelationService) ListByIssue(ctx context.Context, workspaceID uuid.UUID, identifier string) ([]domain.IssueRelation, error) {
	issue, err := s.issueRepo.GetByIdentifier(ctx, workspaceID, identifier)
	if err != nil || issue == nil {
		return nil, fmt.Errorf("issue not found")
	}
	return s.relationRepo.ListByIssue(ctx, issue.ID)
}

func (s *IssueRelationService) Delete(ctx context.Context, id uuid.UUID) error {
	rel, err := s.relationRepo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("relation not found")
	}

	// Delete the inverse too
	inverse := rel.Type.Inverse()
	if inverse != rel.Type {
		_ = s.relationRepo.DeleteByIssues(ctx, rel.RelatedIssueID, rel.IssueID, inverse)
	}

	return s.relationRepo.Delete(ctx, id)
}
