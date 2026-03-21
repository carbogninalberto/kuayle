package service

import (
	"context"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// --- Mocks ---

type mockCycleRepo struct {
	mock.Mock
}

func (m *mockCycleRepo) Create(ctx context.Context, cycle *domain.Cycle) error {
	args := m.Called(ctx, cycle)
	return args.Error(0)
}

func (m *mockCycleRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Cycle, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Cycle), args.Error(1)
}

func (m *mockCycleRepo) ListByTeam(ctx context.Context, teamID uuid.UUID) ([]domain.Cycle, error) {
	args := m.Called(ctx, teamID)
	return args.Get(0).([]domain.Cycle), args.Error(1)
}

func (m *mockCycleRepo) NextNumber(ctx context.Context, teamID uuid.UUID) (int, error) {
	args := m.Called(ctx, teamID)
	return args.Int(0), args.Error(1)
}

func (m *mockCycleRepo) Update(ctx context.Context, cycle *domain.Cycle) error {
	args := m.Called(ctx, cycle)
	return args.Error(0)
}

func (m *mockCycleRepo) Delete(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *mockCycleRepo) IssueStats(ctx context.Context, cycleID uuid.UUID) (int, int, int, error) {
	args := m.Called(ctx, cycleID)
	return args.Int(0), args.Int(1), args.Int(2), args.Error(3)
}

// --- Tests ---

func TestCycleService_Create(t *testing.T) {
	repo := new(mockCycleRepo)
	svc := NewCycleService(repo)

	ctx := context.Background()
	teamID := uuid.New()

	repo.On("NextNumber", ctx, teamID).Return(1, nil)
	repo.On("Create", ctx, mock.AnythingOfType("*domain.Cycle")).Return(nil)

	startDate := "2026-04-01"
	endDate := "2026-04-14"
	cycle, err := svc.Create(ctx, teamID, dto.CreateCycleRequest{
		Name:      "Sprint 1",
		StartDate: &startDate,
		EndDate:   &endDate,
	})

	assert.NoError(t, err)
	assert.NotNil(t, cycle)
	assert.Equal(t, "Sprint 1", cycle.Name)
	assert.Equal(t, 1, cycle.Number)
	assert.Equal(t, domain.CycleStatusUpcoming, cycle.Status)
	assert.NotNil(t, cycle.StartDate)
	assert.NotNil(t, cycle.EndDate)
}

func TestCycleService_List(t *testing.T) {
	repo := new(mockCycleRepo)
	svc := NewCycleService(repo)

	ctx := context.Background()
	teamID := uuid.New()

	cycles := []domain.Cycle{
		{ID: uuid.New(), Name: "Sprint 1", Number: 1},
		{ID: uuid.New(), Name: "Sprint 2", Number: 2},
	}
	repo.On("ListByTeam", ctx, teamID).Return(cycles, nil)

	result, err := svc.ListByTeam(ctx, teamID)
	assert.NoError(t, err)
	assert.Len(t, result, 2)
}

func TestCycleService_Complete(t *testing.T) {
	repo := new(mockCycleRepo)
	svc := NewCycleService(repo)

	ctx := context.Background()
	cycleID := uuid.New()

	cycle := &domain.Cycle{ID: cycleID, Status: domain.CycleStatusActive}
	repo.On("GetByID", ctx, cycleID).Return(cycle, nil)
	repo.On("Update", ctx, mock.AnythingOfType("*domain.Cycle")).Return(nil)

	result, err := svc.Complete(ctx, cycleID)

	assert.NoError(t, err)
	assert.Equal(t, domain.CycleStatusCompleted, result.Status)
	assert.NotNil(t, result.CompletedAt)
}

func TestCycleService_Complete_AlreadyCompleted(t *testing.T) {
	repo := new(mockCycleRepo)
	svc := NewCycleService(repo)

	ctx := context.Background()
	cycleID := uuid.New()

	cycle := &domain.Cycle{ID: cycleID, Status: domain.CycleStatusCompleted}
	repo.On("GetByID", ctx, cycleID).Return(cycle, nil)

	_, err := svc.Complete(ctx, cycleID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already completed")
}

func TestCycleService_GetStats(t *testing.T) {
	repo := new(mockCycleRepo)
	svc := NewCycleService(repo)

	ctx := context.Background()
	cycleID := uuid.New()

	repo.On("IssueStats", ctx, cycleID).Return(10, 6, 1, nil)

	stats, err := svc.GetStats(ctx, cycleID)
	assert.NoError(t, err)
	assert.Equal(t, 10, stats.Total)
	assert.Equal(t, 6, stats.Completed)
	assert.Equal(t, 1, stats.Cancelled)
}

func TestCycleService_Delete(t *testing.T) {
	repo := new(mockCycleRepo)
	svc := NewCycleService(repo)

	ctx := context.Background()
	cycleID := uuid.New()

	cycle := &domain.Cycle{ID: cycleID}
	repo.On("GetByID", ctx, cycleID).Return(cycle, nil)
	repo.On("Delete", ctx, cycleID).Return(nil)

	err := svc.Delete(ctx, cycleID)
	assert.NoError(t, err)
}
