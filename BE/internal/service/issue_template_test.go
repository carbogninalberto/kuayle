package service

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/carbon/carbon-backend/internal/domain"
	"github.com/carbon/carbon-backend/internal/dto"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// --- Mock ---

type mockIssueTemplateRepo struct {
	mock.Mock
}

func (m *mockIssueTemplateRepo) Create(ctx context.Context, tmpl *domain.IssueTemplate) error {
	args := m.Called(ctx, tmpl)
	return args.Error(0)
}

func (m *mockIssueTemplateRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.IssueTemplate, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.IssueTemplate), args.Error(1)
}

func (m *mockIssueTemplateRepo) ListByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]domain.IssueTemplate, error) {
	args := m.Called(ctx, workspaceID)
	return args.Get(0).([]domain.IssueTemplate), args.Error(1)
}

func (m *mockIssueTemplateRepo) Update(ctx context.Context, tmpl *domain.IssueTemplate) error {
	args := m.Called(ctx, tmpl)
	return args.Error(0)
}

func (m *mockIssueTemplateRepo) Delete(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *mockIssueTemplateRepo) ListDueForRecurrence(ctx context.Context) ([]domain.IssueTemplate, error) {
	args := m.Called(ctx)
	return args.Get(0).([]domain.IssueTemplate), args.Error(1)
}

// --- Tests ---

func TestIssueTemplateService_Create(t *testing.T) {
	repo := new(mockIssueTemplateRepo)
	svc := NewIssueTemplateService(repo)

	ctx := context.Background()
	wsID := uuid.New()
	userID := uuid.New()

	repo.On("Create", ctx, mock.AnythingOfType("*domain.IssueTemplate")).Return(nil)

	tmpl, err := svc.Create(ctx, wsID, userID, dto.CreateIssueTemplateRequest{
		Title:       "Bug Report",
		Description: strPtr("Template for bug reports"),
		LabelIDs:    []string{},
	})

	assert.NoError(t, err)
	assert.NotNil(t, tmpl)
	assert.Equal(t, "Bug Report", tmpl.Title)
	assert.Equal(t, wsID, tmpl.WorkspaceID)
	assert.Equal(t, userID, tmpl.CreatedBy)
}

func TestIssueTemplateService_ListByWorkspace(t *testing.T) {
	repo := new(mockIssueTemplateRepo)
	svc := NewIssueTemplateService(repo)

	ctx := context.Background()
	wsID := uuid.New()
	templates := []domain.IssueTemplate{
		{ID: uuid.New(), Title: "Bug Report"},
		{ID: uuid.New(), Title: "Feature Request"},
	}

	repo.On("ListByWorkspace", ctx, wsID).Return(templates, nil)

	result, err := svc.ListByWorkspace(ctx, wsID)

	assert.NoError(t, err)
	assert.Len(t, result, 2)
}

func TestIssueTemplateService_Update(t *testing.T) {
	repo := new(mockIssueTemplateRepo)
	svc := NewIssueTemplateService(repo)

	ctx := context.Background()
	tmplID := uuid.New()
	existing := &domain.IssueTemplate{
		ID:       tmplID,
		Title:    "Old Title",
		LabelIDs: json.RawMessage("[]"),
	}

	repo.On("GetByID", ctx, tmplID).Return(existing, nil)
	repo.On("Update", ctx, mock.AnythingOfType("*domain.IssueTemplate")).Return(nil)

	newTitle := "New Title"
	result, err := svc.Update(ctx, tmplID, dto.UpdateIssueTemplateRequest{
		Title: &newTitle,
	})

	assert.NoError(t, err)
	assert.Equal(t, "New Title", result.Title)
}

func TestIssueTemplateService_Update_NotFound(t *testing.T) {
	repo := new(mockIssueTemplateRepo)
	svc := NewIssueTemplateService(repo)

	ctx := context.Background()
	tmplID := uuid.New()

	repo.On("GetByID", ctx, tmplID).Return(nil, fmt.Errorf("not found"))

	_, err := svc.Update(ctx, tmplID, dto.UpdateIssueTemplateRequest{})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "template not found")
}

func TestIssueTemplateService_Delete(t *testing.T) {
	repo := new(mockIssueTemplateRepo)
	svc := NewIssueTemplateService(repo)

	ctx := context.Background()
	tmplID := uuid.New()

	repo.On("Delete", ctx, tmplID).Return(nil)

	err := svc.Delete(ctx, tmplID)

	assert.NoError(t, err)
}

func strPtr(s string) *string {
	return &s
}
