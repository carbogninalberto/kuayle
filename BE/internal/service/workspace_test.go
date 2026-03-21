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

type mockWorkspaceRepo struct {
	mock.Mock
}

func (m *mockWorkspaceRepo) Create(ctx context.Context, ws *domain.Workspace) error {
	args := m.Called(ctx, ws)
	return args.Error(0)
}

func (m *mockWorkspaceRepo) GetBySlug(ctx context.Context, slug string) (*domain.Workspace, error) {
	args := m.Called(ctx, slug)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Workspace), args.Error(1)
}

func (m *mockWorkspaceRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Workspace, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Workspace), args.Error(1)
}

func (m *mockWorkspaceRepo) Update(ctx context.Context, ws *domain.Workspace) error {
	args := m.Called(ctx, ws)
	return args.Error(0)
}

func (m *mockWorkspaceRepo) ListByUser(ctx context.Context, userID uuid.UUID) ([]domain.Workspace, error) {
	args := m.Called(ctx, userID)
	return args.Get(0).([]domain.Workspace), args.Error(1)
}

func (m *mockWorkspaceRepo) AddMember(ctx context.Context, member *domain.WorkspaceMember) error {
	args := m.Called(ctx, member)
	return args.Error(0)
}

func (m *mockWorkspaceRepo) GetMember(ctx context.Context, workspaceID, userID uuid.UUID) (*domain.WorkspaceMember, error) {
	args := m.Called(ctx, workspaceID, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.WorkspaceMember), args.Error(1)
}

func (m *mockWorkspaceRepo) ListMembers(ctx context.Context, workspaceID uuid.UUID) ([]domain.WorkspaceMember, error) {
	args := m.Called(ctx, workspaceID)
	return args.Get(0).([]domain.WorkspaceMember), args.Error(1)
}

func (m *mockWorkspaceRepo) ListMembersWithUsers(ctx context.Context, workspaceID uuid.UUID) ([]domain.WorkspaceMemberWithUser, error) {
	args := m.Called(ctx, workspaceID)
	return args.Get(0).([]domain.WorkspaceMemberWithUser), args.Error(1)
}

func (m *mockWorkspaceRepo) UpdateMemberRole(ctx context.Context, workspaceID, userID uuid.UUID, role string) error {
	args := m.Called(ctx, workspaceID, userID, role)
	return args.Error(0)
}

func (m *mockWorkspaceRepo) RemoveMember(ctx context.Context, workspaceID, userID uuid.UUID) error {
	args := m.Called(ctx, workspaceID, userID)
	return args.Error(0)
}

func (m *mockWorkspaceRepo) CountMembersByRole(ctx context.Context, workspaceID uuid.UUID, role string) (int, error) {
	args := m.Called(ctx, workspaceID, role)
	return args.Int(0), args.Error(1)
}

// --- Tests ---

func TestWorkspaceService_Create_Happy(t *testing.T) {
	wsRepo := new(mockWorkspaceRepo)
	userRepo := new(mockUserRepo)
	svc := NewWorkspaceService(wsRepo, userRepo)

	ctx := context.Background()
	userID := uuid.New()
	req := dto.CreateWorkspaceRequest{
		Name: "My Workspace",
		Slug: "my-workspace",
	}

	wsRepo.On("GetBySlug", ctx, "my-workspace").Return(nil, nil)
	wsRepo.On("Create", ctx, mock.AnythingOfType("*domain.Workspace")).Return(nil)
	wsRepo.On("AddMember", ctx, mock.AnythingOfType("*domain.WorkspaceMember")).Return(nil)

	ws, err := svc.Create(ctx, userID, req)

	assert.NoError(t, err)
	assert.NotNil(t, ws)
	assert.Equal(t, "My Workspace", ws.Name)
	assert.Equal(t, "my-workspace", ws.Slug)

	// Verify owner membership was added
	wsRepo.AssertCalled(t, "AddMember", ctx, mock.MatchedBy(func(m *domain.WorkspaceMember) bool {
		return m.UserID == userID && m.Role == domain.RoleOwner
	}))
}

func TestWorkspaceService_Create_SlugTaken(t *testing.T) {
	wsRepo := new(mockWorkspaceRepo)
	userRepo := new(mockUserRepo)
	svc := NewWorkspaceService(wsRepo, userRepo)

	ctx := context.Background()
	userID := uuid.New()

	existing := &domain.Workspace{ID: uuid.New(), Slug: "taken"}
	wsRepo.On("GetBySlug", ctx, "taken").Return(existing, nil)

	ws, err := svc.Create(ctx, userID, dto.CreateWorkspaceRequest{Name: "Test", Slug: "taken"})

	assert.Error(t, err)
	assert.Nil(t, ws)
	assert.Contains(t, err.Error(), "slug already taken")
}

func TestWorkspaceService_ListByUser(t *testing.T) {
	wsRepo := new(mockWorkspaceRepo)
	userRepo := new(mockUserRepo)
	svc := NewWorkspaceService(wsRepo, userRepo)

	ctx := context.Background()
	userID := uuid.New()
	workspaces := []domain.Workspace{
		{ID: uuid.New(), Name: "WS1", Slug: "ws1"},
		{ID: uuid.New(), Name: "WS2", Slug: "ws2"},
	}

	wsRepo.On("ListByUser", ctx, userID).Return(workspaces, nil)

	result, err := svc.ListByUser(ctx, userID)

	assert.NoError(t, err)
	assert.Len(t, result, 2)
}

func TestWorkspaceService_GetBySlug(t *testing.T) {
	wsRepo := new(mockWorkspaceRepo)
	userRepo := new(mockUserRepo)
	svc := NewWorkspaceService(wsRepo, userRepo)

	ctx := context.Background()
	ws := &domain.Workspace{ID: uuid.New(), Name: "Test", Slug: "test"}
	wsRepo.On("GetBySlug", ctx, "test").Return(ws, nil)

	result, err := svc.GetBySlug(ctx, "test")

	assert.NoError(t, err)
	assert.Equal(t, ws, result)
}

func TestWorkspaceService_Update(t *testing.T) {
	wsRepo := new(mockWorkspaceRepo)
	userRepo := new(mockUserRepo)
	svc := NewWorkspaceService(wsRepo, userRepo)

	ctx := context.Background()
	ws := &domain.Workspace{ID: uuid.New(), Name: "Old Name", Slug: "test"}
	wsRepo.On("GetBySlug", ctx, "test").Return(ws, nil)
	wsRepo.On("Update", ctx, mock.AnythingOfType("*domain.Workspace")).Return(nil)

	newName := "New Name"
	result, err := svc.Update(ctx, "test", dto.UpdateWorkspaceRequest{Name: &newName})

	assert.NoError(t, err)
	assert.Equal(t, "New Name", result.Name)
}

func TestWorkspaceService_InviteMember_Happy(t *testing.T) {
	wsRepo := new(mockWorkspaceRepo)
	userRepo := new(mockUserRepo)
	svc := NewWorkspaceService(wsRepo, userRepo)

	ctx := context.Background()
	wsID := uuid.New()
	invitedUser := &domain.User{ID: uuid.New(), Email: "invite@example.com"}

	userRepo.On("GetByEmail", ctx, "invite@example.com").Return(invitedUser, nil)
	wsRepo.On("GetMember", ctx, wsID, invitedUser.ID).Return(nil, nil)
	wsRepo.On("AddMember", ctx, mock.AnythingOfType("*domain.WorkspaceMember")).Return(nil)

	err := svc.InviteMember(ctx, wsID, dto.InviteMemberRequest{
		Email: "invite@example.com",
		Role:  "member",
	})

	assert.NoError(t, err)
}

func TestWorkspaceService_InviteMember_AlreadyMember(t *testing.T) {
	wsRepo := new(mockWorkspaceRepo)
	userRepo := new(mockUserRepo)
	svc := NewWorkspaceService(wsRepo, userRepo)

	ctx := context.Background()
	wsID := uuid.New()
	invitedUser := &domain.User{ID: uuid.New(), Email: "existing@example.com"}

	userRepo.On("GetByEmail", ctx, "existing@example.com").Return(invitedUser, nil)
	existing := &domain.WorkspaceMember{UserID: invitedUser.ID}
	wsRepo.On("GetMember", ctx, wsID, invitedUser.ID).Return(existing, nil)

	err := svc.InviteMember(ctx, wsID, dto.InviteMemberRequest{
		Email: "existing@example.com",
		Role:  "member",
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already a member")
}
