package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
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

	rawRelations, err := s.relationRepo.ListByIssue(ctx, issue.ID)
	if err != nil {
		return nil, err
	}

	relations := make([]domain.IssueRelation, 0, len(rawRelations))
	seen := make(map[string]struct{}, len(rawRelations))
	for _, rel := range rawRelations {
		normalized, ok := normalizeRelationForIssue(rel, issue.ID)
		if !ok {
			continue
		}

		key := fmt.Sprintf("%s:%s", normalized.RelatedIssueID, normalized.Type)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}

		relatedIssue, err := s.issueRepo.GetByID(ctx, normalized.RelatedIssueID)
		if err == nil && relatedIssue != nil && relatedIssue.WorkspaceID == workspaceID {
			normalized.RelatedIssue = relatedIssue
		}
		relations = append(relations, normalized)
	}

	return relations, nil
}

func (s *IssueRelationService) CountByIssues(ctx context.Context, issueIDs []uuid.UUID) (map[uuid.UUID]domain.IssueRelationCounts, error) {
	summaries, err := s.SummariesByIssues(ctx, issueIDs)
	if err != nil {
		return nil, err
	}

	counts := make(map[uuid.UUID]domain.IssueRelationCounts, len(summaries))
	for issueID, summary := range summaries {
		counts[issueID] = summary.Counts
	}
	return counts, nil
}

func (s *IssueRelationService) SummariesByIssues(ctx context.Context, issueIDs []uuid.UUID) (map[uuid.UUID]domain.IssueRelationSummary, error) {
	summaries := make(map[uuid.UUID]domain.IssueRelationSummary, len(issueIDs))
	if len(issueIDs) == 0 {
		return summaries, nil
	}

	relations, err := s.relationRepo.ListByIssues(ctx, issueIDs)
	if err != nil {
		return nil, err
	}

	targets := make(map[uuid.UUID]struct{}, len(issueIDs))
	for _, issueID := range issueIDs {
		targets[issueID] = struct{}{}
	}

	seen := make(map[string]struct{}, len(relations))
	issueCache := make(map[uuid.UUID]*domain.Issue)
	for _, rel := range relations {
		if _, ok := targets[rel.IssueID]; ok {
			addRelationSummary(ctx, s.issueRepo, summaries, seen, issueCache, rel.IssueID, rel)
		}
		if _, ok := targets[rel.RelatedIssueID]; ok {
			addRelationSummary(ctx, s.issueRepo, summaries, seen, issueCache, rel.RelatedIssueID, rel)
		}
	}

	return summaries, nil
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

func normalizeRelationForIssue(rel domain.IssueRelation, issueID uuid.UUID) (domain.IssueRelation, bool) {
	if rel.IssueID == issueID {
		return rel, true
	}
	if rel.RelatedIssueID != issueID {
		return domain.IssueRelation{}, false
	}

	relatedID := rel.IssueID
	rel.IssueID = issueID
	rel.RelatedIssueID = relatedID
	rel.Type = rel.Type.Inverse()
	return rel, true
}

func addRelationSummary(ctx context.Context, issueRepo repository.IssueRepo, summaries map[uuid.UUID]domain.IssueRelationSummary, seen map[string]struct{}, issueCache map[uuid.UUID]*domain.Issue, issueID uuid.UUID, rel domain.IssueRelation) {
	normalized, ok := normalizeRelationForIssue(rel, issueID)
	if !ok {
		return
	}

	key := fmt.Sprintf("%s:%s:%s", issueID, normalized.RelatedIssueID, normalized.Type)
	if _, exists := seen[key]; exists {
		return
	}
	seen[key] = struct{}{}

	summary := summaries[issueID]
	switch normalized.Type {
	case domain.RelationRelated:
		summary.Counts.Related++
		if issue := relationIssue(ctx, issueRepo, issueCache, normalized.RelatedIssueID); issue != nil {
			summary.Related = append(summary.Related, *issue)
		}
	case domain.RelationBlockedBy:
		summary.Counts.BlockedBy++
		if issue := relationIssue(ctx, issueRepo, issueCache, normalized.RelatedIssueID); issue != nil {
			summary.BlockedBy = append(summary.BlockedBy, *issue)
		}
	case domain.RelationBlocking:
		summary.Counts.Blocking++
		if issue := relationIssue(ctx, issueRepo, issueCache, normalized.RelatedIssueID); issue != nil {
			summary.Blocking = append(summary.Blocking, *issue)
		}
	case domain.RelationDuplicate:
		summary.Counts.Duplicate++
		if issue := relationIssue(ctx, issueRepo, issueCache, normalized.RelatedIssueID); issue != nil {
			summary.Duplicate = append(summary.Duplicate, *issue)
		}
	}
	summaries[issueID] = summary
}

func relationIssue(ctx context.Context, issueRepo repository.IssueRepo, issueCache map[uuid.UUID]*domain.Issue, issueID uuid.UUID) *domain.Issue {
	if issue, ok := issueCache[issueID]; ok {
		return issue
	}
	issue, err := issueRepo.GetByID(ctx, issueID)
	if err != nil {
		issueCache[issueID] = nil
		return nil
	}
	issueCache[issueID] = issue
	return issue
}
