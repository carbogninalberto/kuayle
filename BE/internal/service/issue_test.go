package service

import (
	"context"
	"testing"

	"github.com/carbon/carbon-backend/internal/domain"
	"github.com/carbon/carbon-backend/internal/dto"
	"github.com/carbon/carbon-backend/internal/realtime"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// --- Mocks ---

type mockIssueRepo struct {
	mock.Mock
}

func (m *mockIssueRepo) Create(ctx context.Context, tx *sqlx.Tx, issue *domain.Issue) error {
	args := m.Called(ctx, tx, issue)
	return args.Error(0)
}

func (m *mockIssueRepo) NextNumber(ctx context.Context, tx *sqlx.Tx, teamID uuid.UUID) (int, error) {
	args := m.Called(ctx, tx, teamID)
	return args.Int(0), args.Error(1)
}

func (m *mockIssueRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Issue, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Issue), args.Error(1)
}

func (m *mockIssueRepo) GetByIdentifier(ctx context.Context, workspaceID uuid.UUID, identifier string) (*domain.Issue, error) {
	args := m.Called(ctx, workspaceID, identifier)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Issue), args.Error(1)
}

func (m *mockIssueRepo) List(ctx context.Context, workspaceID uuid.UUID, params dto.IssueFilterParams) ([]domain.Issue, int, error) {
	args := m.Called(ctx, workspaceID, params)
	return args.Get(0).([]domain.Issue), args.Int(1), args.Error(2)
}

func (m *mockIssueRepo) Update(ctx context.Context, issue *domain.Issue) error {
	args := m.Called(ctx, issue)
	return args.Error(0)
}

func (m *mockIssueRepo) Delete(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *mockIssueRepo) SetLabels(ctx context.Context, issueID uuid.UUID, labelIDs []uuid.UUID) error {
	args := m.Called(ctx, issueID, labelIDs)
	return args.Error(0)
}

func (m *mockIssueRepo) GetLabels(ctx context.Context, issueID uuid.UUID) ([]domain.Label, error) {
	args := m.Called(ctx, issueID)
	return args.Get(0).([]domain.Label), args.Error(1)
}

func (m *mockIssueRepo) ListSubIssues(ctx context.Context, parentID uuid.UUID) ([]domain.Issue, error) {
	args := m.Called(ctx, parentID)
	return args.Get(0).([]domain.Issue), args.Error(1)
}

func (m *mockIssueRepo) CountSubIssues(ctx context.Context, parentID uuid.UUID) (int, int, error) {
	args := m.Called(ctx, parentID)
	return args.Int(0), args.Int(1), args.Error(2)
}

func (m *mockIssueRepo) BulkUpdate(ctx context.Context, workspaceID uuid.UUID, issueIDs []uuid.UUID, status *string, priority *int, assigneeID *uuid.UUID) (int, error) {
	args := m.Called(ctx, workspaceID, issueIDs, status, priority, assigneeID)
	return args.Int(0), args.Error(1)
}

func (m *mockIssueRepo) BeginTx(ctx context.Context) (*sqlx.Tx, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*sqlx.Tx), args.Error(1)
}

type mockTeamRepo struct {
	mock.Mock
}

func (m *mockTeamRepo) Create(ctx context.Context, team *domain.Team) error {
	args := m.Called(ctx, team)
	return args.Error(0)
}

func (m *mockTeamRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Team, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Team), args.Error(1)
}

func (m *mockTeamRepo) ListByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]domain.Team, error) {
	args := m.Called(ctx, workspaceID)
	return args.Get(0).([]domain.Team), args.Error(1)
}

func (m *mockTeamRepo) Update(ctx context.Context, team *domain.Team) error {
	args := m.Called(ctx, team)
	return args.Error(0)
}

func (m *mockTeamRepo) AddMember(ctx context.Context, member *domain.TeamMember) error {
	args := m.Called(ctx, member)
	return args.Error(0)
}

func (m *mockTeamRepo) GetMember(ctx context.Context, teamID, userID uuid.UUID) (*domain.TeamMember, error) {
	args := m.Called(ctx, teamID, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.TeamMember), args.Error(1)
}

type mockIssueHistoryRepo struct {
	mock.Mock
}

func (m *mockIssueHistoryRepo) Create(ctx context.Context, issueID, userID uuid.UUID, field string, oldValue, newValue *string) error {
	args := m.Called(ctx, issueID, userID, field, oldValue, newValue)
	return args.Error(0)
}

func (m *mockIssueHistoryRepo) ListByIssue(ctx context.Context, issueID uuid.UUID) ([]domain.IssueHistory, error) {
	args := m.Called(ctx, issueID)
	return args.Get(0).([]domain.IssueHistory), args.Error(1)
}

// --- Tests ---

func TestIssueService_GetByIdentifier(t *testing.T) {
	issueRepo := new(mockIssueRepo)
	teamRepo := new(mockTeamRepo)
	historyRepo := new(mockIssueHistoryRepo)
	hub := realtime.NewHub()
	svc := NewIssueService(issueRepo, teamRepo, historyRepo, hub)

	ctx := context.Background()
	wsID := uuid.New()
	issue := &domain.Issue{
		ID:         uuid.New(),
		Identifier: "ENG-1",
		Title:      "Test Issue",
	}

	issueRepo.On("GetByIdentifier", ctx, wsID, "ENG-1").Return(issue, nil)

	result, err := svc.GetByIdentifier(ctx, wsID, "ENG-1")

	assert.NoError(t, err)
	assert.Equal(t, issue, result)
}

func TestIssueService_List(t *testing.T) {
	issueRepo := new(mockIssueRepo)
	teamRepo := new(mockTeamRepo)
	historyRepo := new(mockIssueHistoryRepo)
	hub := realtime.NewHub()
	svc := NewIssueService(issueRepo, teamRepo, historyRepo, hub)

	ctx := context.Background()
	wsID := uuid.New()
	params := dto.IssueFilterParams{Status: "todo"}
	issues := []domain.Issue{
		{ID: uuid.New(), Title: "Issue 1"},
		{ID: uuid.New(), Title: "Issue 2"},
	}

	issueRepo.On("List", ctx, wsID, params).Return(issues, 2, nil)

	result, total, err := svc.List(ctx, wsID, params)

	assert.NoError(t, err)
	assert.Len(t, result, 2)
	assert.Equal(t, 2, total)
}

func TestIssueService_Delete(t *testing.T) {
	issueRepo := new(mockIssueRepo)
	teamRepo := new(mockTeamRepo)
	historyRepo := new(mockIssueHistoryRepo)
	hub := realtime.NewHub()
	svc := NewIssueService(issueRepo, teamRepo, historyRepo, hub)

	ctx := context.Background()
	wsID := uuid.New()
	issueID := uuid.New()
	issue := &domain.Issue{
		ID:          issueID,
		WorkspaceID: wsID,
		Identifier:  "ENG-1",
	}

	issueRepo.On("GetByIdentifier", ctx, wsID, "ENG-1").Return(issue, nil)
	issueRepo.On("Delete", ctx, issueID).Return(nil)

	err := svc.Delete(ctx, wsID, "ENG-1")

	assert.NoError(t, err)
	issueRepo.AssertExpectations(t)
}

func TestIssueService_Delete_NotFound(t *testing.T) {
	issueRepo := new(mockIssueRepo)
	teamRepo := new(mockTeamRepo)
	historyRepo := new(mockIssueHistoryRepo)
	hub := realtime.NewHub()
	svc := NewIssueService(issueRepo, teamRepo, historyRepo, hub)

	ctx := context.Background()
	wsID := uuid.New()

	issueRepo.On("GetByIdentifier", ctx, wsID, "ENG-999").Return(nil, nil)

	err := svc.Delete(ctx, wsID, "ENG-999")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "issue not found")
}

func TestIssueService_Update(t *testing.T) {
	issueRepo := new(mockIssueRepo)
	teamRepo := new(mockTeamRepo)
	historyRepo := new(mockIssueHistoryRepo)
	hub := realtime.NewHub()
	svc := NewIssueService(issueRepo, teamRepo, historyRepo, hub)

	ctx := context.Background()
	wsID := uuid.New()
	userID := uuid.New()
	issueID := uuid.New()
	issue := &domain.Issue{
		ID:          issueID,
		WorkspaceID: wsID,
		Identifier:  "ENG-1",
		Title:       "Old Title",
		Status:      domain.IssueStatusBacklog,
		Priority:    domain.PriorityNone,
	}

	issueRepo.On("GetByIdentifier", ctx, wsID, "ENG-1").Return(issue, nil)
	issueRepo.On("Update", ctx, mock.AnythingOfType("*domain.Issue")).Return(nil)

	newTitle := "New Title"
	newStatus := "todo"
	historyRepo.On("Create", ctx, issueID, userID, "title", mock.AnythingOfType("*string"), &newTitle).Return(nil)
	historyRepo.On("Create", ctx, issueID, userID, "status", mock.AnythingOfType("*string"), &newStatus).Return(nil)

	req := dto.UpdateIssueRequest{
		Title:  &newTitle,
		Status: &newStatus,
	}

	result, err := svc.Update(ctx, wsID, userID, "ENG-1", req)

	assert.NoError(t, err)
	assert.Equal(t, "New Title", result.Title)
	assert.Equal(t, domain.IssueStatusTodo, result.Status)
}

func TestIssueService_GetLabels(t *testing.T) {
	issueRepo := new(mockIssueRepo)
	teamRepo := new(mockTeamRepo)
	historyRepo := new(mockIssueHistoryRepo)
	hub := realtime.NewHub()
	svc := NewIssueService(issueRepo, teamRepo, historyRepo, hub)

	ctx := context.Background()
	issueID := uuid.New()
	labels := []domain.Label{
		{ID: uuid.New(), Name: "Bug", Color: "#ff0000"},
	}

	issueRepo.On("GetLabels", ctx, issueID).Return(labels, nil)

	result, err := svc.GetLabels(ctx, issueID)

	assert.NoError(t, err)
	assert.Len(t, result, 1)
	assert.Equal(t, "Bug", result[0].Name)
}

func TestIssueService_GetHistory(t *testing.T) {
	issueRepo := new(mockIssueRepo)
	teamRepo := new(mockTeamRepo)
	historyRepo := new(mockIssueHistoryRepo)
	hub := realtime.NewHub()
	svc := NewIssueService(issueRepo, teamRepo, historyRepo, hub)

	ctx := context.Background()
	issueID := uuid.New()
	history := []domain.IssueHistory{
		{ID: uuid.New(), Field: "status"},
	}

	historyRepo.On("ListByIssue", ctx, issueID).Return(history, nil)

	result, err := svc.GetHistory(ctx, issueID)

	assert.NoError(t, err)
	assert.Len(t, result, 1)
}
