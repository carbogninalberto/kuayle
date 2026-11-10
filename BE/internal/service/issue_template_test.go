package service

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
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
		Title:             "Bug Report",
		DefaultIssueTitle: strPtr("[Bug] Describe the problem"),
		Description:       strPtr("Template for bug reports"),
		LabelIDs:          []string{},
	})

	assert.NoError(t, err)
	assert.NotNil(t, tmpl)
	assert.Equal(t, "Bug Report", tmpl.Title)
	assert.Equal(t, "[Bug] Describe the problem", *tmpl.DefaultIssueTitle)
	assert.Equal(t, wsID, tmpl.WorkspaceID)
	assert.Equal(t, userID, tmpl.CreatedBy)
}

func TestIssueTemplateService_CreateNormalizesDefaultIssueTitle(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected *string
	}{
		{name: "trims whitespace", input: "  [Bug] Describe the problem  ", expected: strPtr("[Bug] Describe the problem")},
		{name: "clears whitespace only", input: "   ", expected: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := new(mockIssueTemplateRepo)
			svc := NewIssueTemplateService(repo)
			ctx := context.Background()
			repo.On("Create", ctx, mock.AnythingOfType("*domain.IssueTemplate")).Return(nil)

			tmpl, err := svc.Create(ctx, uuid.New(), uuid.New(), dto.CreateIssueTemplateRequest{
				Title:             "Bug Report",
				DefaultIssueTitle: &tt.input,
				LabelIDs:          []string{},
			})

			assert.NoError(t, err)
			if tt.expected == nil {
				assert.Nil(t, tmpl.DefaultIssueTitle)
			} else {
				assert.Equal(t, *tt.expected, *tmpl.DefaultIssueTitle)
			}
		})
	}
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

func TestIssueTemplateService_Update_ClearsOptionalFields(t *testing.T) {
	repo := new(mockIssueTemplateRepo)
	svc := NewIssueTemplateService(repo)

	ctx := context.Background()
	tmplID := uuid.New()
	teamID := uuid.New()
	assigneeID := uuid.New()
	status := "todo"
	priority := 2
	existing := &domain.IssueTemplate{
		ID:                tmplID,
		Title:             "Bug Report",
		DefaultIssueTitle: strPtr("[Bug] Existing"),
		Status:            &status,
		Priority:          &priority,
		TeamID:            &teamID,
		AssigneeID:        &assigneeID,
		LabelIDs:          json.RawMessage("[]"),
	}

	repo.On("GetByID", ctx, tmplID).Return(existing, nil)
	repo.On("Update", ctx, existing).Return(nil)

	result, err := svc.Update(ctx, tmplID, dto.UpdateIssueTemplateRequest{
		DefaultIssueTitle: dto.OptionalString{Set: true},
		Status:            dto.OptionalString{Set: true},
		Priority:          dto.OptionalInt{Set: true},
		TeamID:            dto.OptionalString{Set: true},
		AssigneeID:        dto.OptionalString{Set: true},
		LabelIDs:          []string{},
	})

	assert.NoError(t, err)
	assert.Nil(t, result.DefaultIssueTitle)
	assert.Nil(t, result.Status)
	assert.Nil(t, result.Priority)
	assert.Nil(t, result.TeamID)
	assert.Nil(t, result.AssigneeID)
	assert.Equal(t, json.RawMessage("[]"), result.LabelIDs)
}

func TestIssueTemplateService_Update_RejectsInvalidOptionalFields(t *testing.T) {
	invalidStatus := "unknown"
	invalidPriority := 9

	tests := []dto.UpdateIssueTemplateRequest{
		{Title: strPtr("   ")},
		{Status: dto.OptionalString{Set: true, Value: &invalidStatus}},
		{Priority: dto.OptionalInt{Set: true, Value: &invalidPriority}},
	}

	for _, req := range tests {
		repo := new(mockIssueTemplateRepo)
		svc := NewIssueTemplateService(repo)
		tmplID := uuid.New()
		existing := &domain.IssueTemplate{
			ID:       tmplID,
			Title:    "Existing",
			LabelIDs: json.RawMessage("[]"),
		}
		repo.On("GetByID", mock.Anything, tmplID).Return(existing, nil)

		_, err := svc.Update(context.Background(), tmplID, req)

		assert.Error(t, err)
		repo.AssertNotCalled(t, "Update", mock.Anything, mock.Anything)
	}
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

// Template descriptions are rendered as rich text and copied into issue
// descriptions, so they follow the rule issue descriptions follow.
func TestIssueTemplateService_CreateSanitizesDescription(t *testing.T) {
	repo := new(mockIssueTemplateRepo)
	svc := NewIssueTemplateService(repo)
	ctx := context.Background()

	var stored *domain.IssueTemplate
	repo.On("Create", ctx, mock.AnythingOfType("*domain.IssueTemplate")).
		Run(func(args mock.Arguments) { stored = args.Get(1).(*domain.IssueTemplate) }).
		Return(nil)

	tmpl, err := svc.Create(ctx, uuid.New(), uuid.New(), dto.CreateIssueTemplateRequest{
		Title:       "Bug Report",
		Description: strPtr(`<p>Steps</p><script>alert(1)</script><img src="x" onerror="alert(2)"><a href="javascript:alert(3)" class="fixed inset-0">go</a>`),
	})

	assert.NoError(t, err)
	if assert.NotNil(t, stored) && assert.NotNil(t, stored.Description) {
		assert.Contains(t, *stored.Description, "<p>Steps</p>")
		for _, unsafe := range []string{"<script", "alert(", "onerror", "javascript:", "inset-0"} {
			assert.NotContains(t, *stored.Description, unsafe)
		}
		assert.Equal(t, *stored.Description, *tmpl.Description)
	}
}

func TestIssueTemplateService_CreateConvertsMarkdownDescriptionLikeIssues(t *testing.T) {
	repo := new(mockIssueTemplateRepo)
	svc := NewIssueTemplateService(repo)
	ctx := context.Background()
	repo.On("Create", ctx, mock.AnythingOfType("*domain.IssueTemplate")).Return(nil)

	tmpl, err := svc.Create(ctx, uuid.New(), uuid.New(), dto.CreateIssueTemplateRequest{
		Title:       "Bug Report",
		Description: strPtr("## Steps\n\n- [ ] Reproduce"),
	})

	assert.NoError(t, err)
	if assert.NotNil(t, tmpl.Description) {
		assert.Contains(t, *tmpl.Description, "<h2>Steps</h2>")
		assert.Contains(t, *tmpl.Description, `data-type="taskList"`)
	}
}

func TestIssueTemplateService_UpdateSanitizesDescription(t *testing.T) {
	repo := new(mockIssueTemplateRepo)
	svc := NewIssueTemplateService(repo)
	ctx := context.Background()
	id := uuid.New()

	existing := &domain.IssueTemplate{ID: id, Title: "Bug Report", Description: strPtr("<p>Old</p>")}
	repo.On("GetByID", ctx, id).Return(existing, nil)
	var stored *domain.IssueTemplate
	repo.On("Update", ctx, mock.AnythingOfType("*domain.IssueTemplate")).
		Run(func(args mock.Arguments) { stored = args.Get(1).(*domain.IssueTemplate) }).
		Return(nil)

	_, err := svc.Update(ctx, id, dto.UpdateIssueTemplateRequest{
		Description: strPtr(`<p>New</p><script>alert(1)</script><span class="fixed inset-0" onclick="alert(2)">x</span>`),
	})

	assert.NoError(t, err)
	if assert.NotNil(t, stored) && assert.NotNil(t, stored.Description) {
		assert.Contains(t, *stored.Description, "<p>New</p>")
		for _, unsafe := range []string{"<script", "alert(", "onclick", "inset-0"} {
			assert.NotContains(t, *stored.Description, unsafe)
		}
	}
}
