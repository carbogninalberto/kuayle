package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// --- Mock ---

type mockIssueRelationRepo struct {
	mock.Mock
}

func (m *mockIssueRelationRepo) Create(ctx context.Context, rel *domain.IssueRelation) error {
	args := m.Called(ctx, rel)
	return args.Error(0)
}

func (m *mockIssueRelationRepo) ListByIssue(ctx context.Context, issueID uuid.UUID) ([]domain.IssueRelation, error) {
	args := m.Called(ctx, issueID)
	return args.Get(0).([]domain.IssueRelation), args.Error(1)
}

func (m *mockIssueRelationRepo) Delete(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *mockIssueRelationRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.IssueRelation, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.IssueRelation), args.Error(1)
}

func (m *mockIssueRelationRepo) DeleteByIssues(ctx context.Context, issueID, relatedIssueID uuid.UUID, relType domain.IssueRelationType) error {
	args := m.Called(ctx, issueID, relatedIssueID, relType)
	return args.Error(0)
}

// --- Tests ---

func TestIssueRelationService_Create_Related(t *testing.T) {
	relRepo := new(mockIssueRelationRepo)
	issueRepo := new(mockIssueRepo)
	svc := NewIssueRelationService(relRepo, issueRepo)

	ctx := context.Background()
	wsID := uuid.New()
	issueA := &domain.Issue{ID: uuid.New(), Identifier: "ENG-1"}
	issueB := &domain.Issue{ID: uuid.New(), Identifier: "ENG-2"}

	issueRepo.On("GetByIdentifier", ctx, wsID, "ENG-1").Return(issueA, nil)
	issueRepo.On("GetByIdentifier", ctx, wsID, "ENG-2").Return(issueB, nil)
	relRepo.On("Create", ctx, mock.AnythingOfType("*domain.IssueRelation")).Return(nil)

	rel, err := svc.Create(ctx, wsID, "ENG-1", dto.CreateIssueRelationRequest{
		RelatedIdentifier: "ENG-2",
		Type:              "related",
	})

	assert.NoError(t, err)
	assert.NotNil(t, rel)
	assert.Equal(t, domain.RelationRelated, rel.Type)
	// "related" is symmetric, so only one Create call (not inverse)
	relRepo.AssertNumberOfCalls(t, "Create", 1)
}

func TestIssueRelationService_Create_Blocking(t *testing.T) {
	relRepo := new(mockIssueRelationRepo)
	issueRepo := new(mockIssueRepo)
	svc := NewIssueRelationService(relRepo, issueRepo)

	ctx := context.Background()
	wsID := uuid.New()
	issueA := &domain.Issue{ID: uuid.New(), Identifier: "ENG-1"}
	issueB := &domain.Issue{ID: uuid.New(), Identifier: "ENG-2"}

	issueRepo.On("GetByIdentifier", ctx, wsID, "ENG-1").Return(issueA, nil)
	issueRepo.On("GetByIdentifier", ctx, wsID, "ENG-2").Return(issueB, nil)
	relRepo.On("Create", ctx, mock.AnythingOfType("*domain.IssueRelation")).Return(nil)

	rel, err := svc.Create(ctx, wsID, "ENG-1", dto.CreateIssueRelationRequest{
		RelatedIdentifier: "ENG-2",
		Type:              "blocking",
	})

	assert.NoError(t, err)
	assert.Equal(t, domain.RelationBlocking, rel.Type)
	// Should create inverse "blocked_by"
	relRepo.AssertNumberOfCalls(t, "Create", 2)
}

func TestIssueRelationService_Create_SelfReference(t *testing.T) {
	relRepo := new(mockIssueRelationRepo)
	issueRepo := new(mockIssueRepo)
	svc := NewIssueRelationService(relRepo, issueRepo)

	ctx := context.Background()
	wsID := uuid.New()
	issue := &domain.Issue{ID: uuid.New(), Identifier: "ENG-1"}

	issueRepo.On("GetByIdentifier", ctx, wsID, "ENG-1").Return(issue, nil)

	_, err := svc.Create(ctx, wsID, "ENG-1", dto.CreateIssueRelationRequest{
		RelatedIdentifier: "ENG-1",
		Type:              "related",
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot relate an issue to itself")
}

func TestIssueRelationService_Create_IssueNotFound(t *testing.T) {
	relRepo := new(mockIssueRelationRepo)
	issueRepo := new(mockIssueRepo)
	svc := NewIssueRelationService(relRepo, issueRepo)

	ctx := context.Background()
	wsID := uuid.New()

	issueRepo.On("GetByIdentifier", ctx, wsID, "ENG-999").Return(nil, nil)

	_, err := svc.Create(ctx, wsID, "ENG-999", dto.CreateIssueRelationRequest{
		RelatedIdentifier: "ENG-2",
		Type:              "related",
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "issue not found")
}

func TestIssueRelationService_ListByIssue(t *testing.T) {
	relRepo := new(mockIssueRelationRepo)
	issueRepo := new(mockIssueRepo)
	svc := NewIssueRelationService(relRepo, issueRepo)

	ctx := context.Background()
	wsID := uuid.New()
	issueID := uuid.New()
	issue := &domain.Issue{ID: issueID, Identifier: "ENG-1"}

	issueRepo.On("GetByIdentifier", ctx, wsID, "ENG-1").Return(issue, nil)
	relations := []domain.IssueRelation{
		{ID: uuid.New(), IssueID: issueID, Type: domain.RelationRelated},
	}
	relRepo.On("ListByIssue", ctx, issueID).Return(relations, nil)

	result, err := svc.ListByIssue(ctx, wsID, "ENG-1")

	assert.NoError(t, err)
	assert.Len(t, result, 1)
}

func TestIssueRelationService_Delete(t *testing.T) {
	relRepo := new(mockIssueRelationRepo)
	issueRepo := new(mockIssueRepo)
	svc := NewIssueRelationService(relRepo, issueRepo)

	ctx := context.Background()
	relID := uuid.New()
	rel := &domain.IssueRelation{
		ID:             relID,
		IssueID:        uuid.New(),
		RelatedIssueID: uuid.New(),
		Type:           domain.RelationBlocking,
	}

	relRepo.On("GetByID", ctx, relID).Return(rel, nil)
	relRepo.On("DeleteByIssues", ctx, rel.RelatedIssueID, rel.IssueID, domain.RelationBlockedBy).Return(nil)
	relRepo.On("Delete", ctx, relID).Return(nil)

	err := svc.Delete(ctx, relID)

	assert.NoError(t, err)
	relRepo.AssertCalled(t, "DeleteByIssues", ctx, rel.RelatedIssueID, rel.IssueID, domain.RelationBlockedBy)
}

func TestIssueRelationService_Delete_NotFound(t *testing.T) {
	relRepo := new(mockIssueRelationRepo)
	issueRepo := new(mockIssueRepo)
	svc := NewIssueRelationService(relRepo, issueRepo)

	ctx := context.Background()
	relID := uuid.New()

	relRepo.On("GetByID", ctx, relID).Return(nil, fmt.Errorf("not found"))

	err := svc.Delete(ctx, relID)

	assert.Error(t, err)
}
