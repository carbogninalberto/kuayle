package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// --- Mocks ---

type mockViewRepo struct {
	mock.Mock
}

func (m *mockViewRepo) Create(ctx context.Context, view *domain.View) error {
	args := m.Called(ctx, view)
	return args.Error(0)
}

func (m *mockViewRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.View, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.View), args.Error(1)
}

func (m *mockViewRepo) ListByWorkspace(ctx context.Context, workspaceID uuid.UUID, userID uuid.UUID) ([]domain.View, error) {
	args := m.Called(ctx, workspaceID, userID)
	return args.Get(0).([]domain.View), args.Error(1)
}

func (m *mockViewRepo) Update(ctx context.Context, view *domain.View) error {
	args := m.Called(ctx, view)
	return args.Error(0)
}

func (m *mockViewRepo) Delete(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

// --- Tests ---

func TestViewService_Create(t *testing.T) {
	repo := new(mockViewRepo)
	svc := NewViewService(repo)

	ctx := context.Background()
	wsID := uuid.New()
	userID := uuid.New()
	filters := json.RawMessage(`{"status":"todo,in_progress"}`)

	repo.On("Create", ctx, mock.AnythingOfType("*domain.View")).Return(nil)

	view, err := svc.Create(ctx, wsID, userID, dto.CreateViewRequest{
		Name:    "Active Issues",
		Filters: filters,
	})

	assert.NoError(t, err)
	assert.NotNil(t, view)
	assert.Equal(t, "Active Issues", view.Name)
	assert.Equal(t, wsID, view.WorkspaceID)
	assert.Equal(t, userID, view.CreatorID)
}

func TestViewService_List(t *testing.T) {
	repo := new(mockViewRepo)
	svc := NewViewService(repo)

	ctx := context.Background()
	wsID := uuid.New()
	userID := uuid.New()

	views := []domain.View{
		{ID: uuid.New(), Name: "View 1"},
		{ID: uuid.New(), Name: "View 2"},
	}

	repo.On("ListByWorkspace", ctx, wsID, userID).Return(views, nil)

	result, err := svc.List(ctx, wsID, userID)

	assert.NoError(t, err)
	assert.Len(t, result, 2)
}

func TestViewService_Update_OwnerOnly(t *testing.T) {
	repo := new(mockViewRepo)
	svc := NewViewService(repo)

	ctx := context.Background()
	ownerID := uuid.New()
	otherID := uuid.New()
	viewID := uuid.New()

	view := &domain.View{ID: viewID, CreatorID: ownerID, Name: "Old"}
	repo.On("GetByID", ctx, viewID).Return(view, nil)

	newName := "New Name"
	_, err := svc.Update(ctx, viewID, otherID, dto.UpdateViewRequest{Name: &newName})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "only the creator")
}

func TestViewService_Update_Happy(t *testing.T) {
	repo := new(mockViewRepo)
	svc := NewViewService(repo)

	ctx := context.Background()
	ownerID := uuid.New()
	viewID := uuid.New()

	view := &domain.View{ID: viewID, CreatorID: ownerID, Name: "Old"}
	repo.On("GetByID", ctx, viewID).Return(view, nil)
	repo.On("Update", ctx, mock.AnythingOfType("*domain.View")).Return(nil)

	newName := "Updated"
	result, err := svc.Update(ctx, viewID, ownerID, dto.UpdateViewRequest{Name: &newName})

	assert.NoError(t, err)
	assert.Equal(t, "Updated", result.Name)
}

func TestViewService_Delete_OwnerOnly(t *testing.T) {
	repo := new(mockViewRepo)
	svc := NewViewService(repo)

	ctx := context.Background()
	ownerID := uuid.New()
	otherID := uuid.New()
	viewID := uuid.New()

	view := &domain.View{ID: viewID, CreatorID: ownerID}
	repo.On("GetByID", ctx, viewID).Return(view, nil)

	err := svc.Delete(ctx, viewID, otherID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "only the creator")
}

func TestViewService_Delete_Happy(t *testing.T) {
	repo := new(mockViewRepo)
	svc := NewViewService(repo)

	ctx := context.Background()
	ownerID := uuid.New()
	viewID := uuid.New()

	view := &domain.View{ID: viewID, CreatorID: ownerID}
	repo.On("GetByID", ctx, viewID).Return(view, nil)
	repo.On("Delete", ctx, viewID).Return(nil)

	err := svc.Delete(ctx, viewID, ownerID)
	assert.NoError(t, err)
}
