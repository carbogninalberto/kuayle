package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
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

func (m *mockWorkspaceRepo) CreateWithMemberAndLabels(ctx context.Context, ws *domain.Workspace, member *domain.WorkspaceMember, labels []domain.Label) error {
	args := m.Called(ctx, ws, member, labels)
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

func (m *mockWorkspaceRepo) Delete(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
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
	wsRepo.On("CreateWithMemberAndLabels", ctx, mock.AnythingOfType("*domain.Workspace"), mock.AnythingOfType("*domain.WorkspaceMember"), mock.AnythingOfType("[]domain.Label")).Run(func(args mock.Arguments) {
		workspace := args.Get(1).(*domain.Workspace)
		member := args.Get(2).(*domain.WorkspaceMember)
		labels := args.Get(3).([]domain.Label)

		assert.Equal(t, workspace.ID, member.WorkspaceID)
		assert.Equal(t, userID, member.UserID)
		assert.Equal(t, domain.RoleOwner, member.Role)

		assert.Len(t, labels, len(defaultWorkspaceLabelSpecs()))
		for i, spec := range defaultWorkspaceLabelSpecs() {
			assert.Equal(t, workspace.ID, labels[i].WorkspaceID)
			assert.Equal(t, spec.name, labels[i].Name)
			assert.Equal(t, spec.color, labels[i].Color)
			assert.NotNil(t, labels[i].Description)
			assert.Equal(t, spec.description, *labels[i].Description)
		}
	}).Return(nil)

	ws, err := svc.Create(ctx, userID, req)

	assert.NoError(t, err)
	assert.NotNil(t, ws)
	assert.Equal(t, "My Workspace", ws.Name)
	assert.Equal(t, "my-workspace", ws.Slug)
	assert.Equal(t, domain.RoleAdmin, ws.ShareLinkMinRole)

	wsRepo.AssertCalled(t, "CreateWithMemberAndLabels", ctx, mock.AnythingOfType("*domain.Workspace"), mock.AnythingOfType("*domain.WorkspaceMember"), mock.AnythingOfType("[]domain.Label"))
	wsRepo.AssertNotCalled(t, "Create", ctx, mock.AnythingOfType("*domain.Workspace"))
	wsRepo.AssertNotCalled(t, "AddMember", ctx, mock.AnythingOfType("*domain.WorkspaceMember"))
}

func TestWorkspaceService_Create_TransactionalCreateFails(t *testing.T) {
	wsRepo := new(mockWorkspaceRepo)
	userRepo := new(mockUserRepo)
	svc := NewWorkspaceService(wsRepo, userRepo)

	ctx := context.Background()
	errCreate := errors.New("label insert failed")

	wsRepo.On("GetBySlug", ctx, "my-workspace").Return(nil, nil)
	wsRepo.On("CreateWithMemberAndLabels", ctx, mock.AnythingOfType("*domain.Workspace"), mock.AnythingOfType("*domain.WorkspaceMember"), mock.AnythingOfType("[]domain.Label")).Return(errCreate)

	ws, err := svc.Create(ctx, uuid.New(), dto.CreateWorkspaceRequest{Name: "My Workspace", Slug: "my-workspace"})

	assert.ErrorIs(t, err, errCreate)
	assert.Nil(t, ws)
	wsRepo.AssertCalled(t, "CreateWithMemberAndLabels", ctx, mock.AnythingOfType("*domain.Workspace"), mock.AnythingOfType("*domain.WorkspaceMember"), mock.AnythingOfType("[]domain.Label"))
	wsRepo.AssertNotCalled(t, "Create", ctx, mock.AnythingOfType("*domain.Workspace"))
	wsRepo.AssertNotCalled(t, "AddMember", ctx, mock.AnythingOfType("*domain.WorkspaceMember"))
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
	ownerID := uuid.New()
	ws := &domain.Workspace{ID: uuid.New(), Name: "Old Name", Slug: "test", OwnerID: ownerID}
	wsRepo.On("GetBySlug", ctx, "test").Return(ws, nil)
	wsRepo.On("Update", ctx, mock.AnythingOfType("*domain.Workspace")).Return(nil)

	newName := "New Name"
	result, err := svc.Update(ctx, "test", ownerID, dto.UpdateWorkspaceRequest{Name: &newName})

	assert.NoError(t, err)
	assert.Equal(t, "New Name", result.Name)
}

func TestWorkspaceService_Update_NotOwner(t *testing.T) {
	wsRepo := new(mockWorkspaceRepo)
	userRepo := new(mockUserRepo)
	svc := NewWorkspaceService(wsRepo, userRepo)

	ctx := context.Background()
	ownerID := uuid.New()
	requesterID := uuid.New()
	ws := &domain.Workspace{ID: uuid.New(), Name: "Old Name", Slug: "test", OwnerID: ownerID}
	wsRepo.On("GetBySlug", ctx, "test").Return(ws, nil)

	newName := "New Name"
	_, err := svc.Update(ctx, "test", requesterID, dto.UpdateWorkspaceRequest{Name: &newName})

	assert.ErrorIs(t, err, ErrNotWorkspaceOwner)
	wsRepo.AssertNotCalled(t, "Update", ctx, mock.AnythingOfType("*domain.Workspace"))
}

func TestWorkspaceService_Update_Fields(t *testing.T) {
	wsRepo := new(mockWorkspaceRepo)
	userRepo := new(mockUserRepo)
	svc := NewWorkspaceService(wsRepo, userRepo)

	ctx := context.Background()
	ownerID := uuid.New()
	ws := &domain.Workspace{ID: uuid.New(), Name: "Name", Slug: "test", OwnerID: ownerID, ShareLinkMinRole: "admin"}
	wsRepo.On("GetBySlug", ctx, "test").Return(ws, nil)
	wsRepo.On("Update", ctx, mock.MatchedBy(func(w *domain.Workspace) bool {
		return w.ShareLinkMinRole == "member"
	})).Return(nil)

	minRole := "member"
	result, err := svc.Update(ctx, "test", ownerID, dto.UpdateWorkspaceRequest{ShareLinkMinRole: &minRole})

	assert.NoError(t, err)
	assert.Equal(t, "member", result.ShareLinkMinRole)
}

func TestWorkspaceService_Update_ClearLogoURL(t *testing.T) {
	wsRepo := new(mockWorkspaceRepo)
	userRepo := new(mockUserRepo)
	svc := NewWorkspaceService(wsRepo, userRepo)

	ctx := context.Background()
	ownerID := uuid.New()
	oldLogo := "https://example.com/logo.png"
	ws := &domain.Workspace{ID: uuid.New(), Name: "Name", Slug: "test", OwnerID: ownerID, LogoURL: &oldLogo}
	wsRepo.On("GetBySlug", ctx, "test").Return(ws, nil)
	wsRepo.On("Update", ctx, mock.MatchedBy(func(w *domain.Workspace) bool {
		return w.LogoURL == nil
	})).Return(nil)

	result, err := svc.Update(ctx, "test", ownerID, dto.UpdateWorkspaceRequest{LogoURL: dto.OptionalString{Set: true}})

	assert.NoError(t, err)
	assert.Nil(t, result.LogoURL)
}

func TestWorkspaceService_Update_TrimLogoURL(t *testing.T) {
	wsRepo := new(mockWorkspaceRepo)
	userRepo := new(mockUserRepo)
	svc := NewWorkspaceService(wsRepo, userRepo)

	ctx := context.Background()
	ownerID := uuid.New()
	ws := &domain.Workspace{ID: uuid.New(), Name: "Name", Slug: "test", OwnerID: ownerID}
	wsRepo.On("GetBySlug", ctx, "test").Return(ws, nil)
	wsRepo.On("Update", ctx, mock.MatchedBy(func(w *domain.Workspace) bool {
		return w.LogoURL != nil && *w.LogoURL == "https://example.com/logo.png"
	})).Return(nil)

	logo := "  https://example.com/logo.png  "
	result, err := svc.Update(ctx, "test", ownerID, dto.UpdateWorkspaceRequest{LogoURL: dto.OptionalString{Set: true, Value: &logo}})

	assert.NoError(t, err)
	assert.NotNil(t, result.LogoURL)
	assert.Equal(t, "https://example.com/logo.png", *result.LogoURL)
}

func TestWorkspaceService_Delete_Owner(t *testing.T) {
	wsRepo := new(mockWorkspaceRepo)
	userRepo := new(mockUserRepo)
	svc := NewWorkspaceService(wsRepo, userRepo)

	ctx := context.Background()
	ownerID := uuid.New()
	ws := &domain.Workspace{ID: uuid.New(), Slug: "test", OwnerID: ownerID}
	wsRepo.On("GetBySlug", ctx, "test").Return(ws, nil)
	wsRepo.On("Delete", ctx, ws.ID).Return(nil)

	err := svc.Delete(ctx, "test", ownerID)

	assert.NoError(t, err)
	wsRepo.AssertCalled(t, "Delete", ctx, ws.ID)
}

func TestWorkspaceService_Delete_NotOwner(t *testing.T) {
	wsRepo := new(mockWorkspaceRepo)
	userRepo := new(mockUserRepo)
	svc := NewWorkspaceService(wsRepo, userRepo)

	ctx := context.Background()
	ownerID := uuid.New()
	requesterID := uuid.New()
	ws := &domain.Workspace{ID: uuid.New(), Slug: "test", OwnerID: ownerID}
	wsRepo.On("GetBySlug", ctx, "test").Return(ws, nil)

	err := svc.Delete(ctx, "test", requesterID)

	assert.ErrorIs(t, err, ErrNotWorkspaceOwner)
	wsRepo.AssertNotCalled(t, "Delete", ctx, ws.ID)
}

func TestWorkspaceService_Delete_NotFound(t *testing.T) {
	wsRepo := new(mockWorkspaceRepo)
	userRepo := new(mockUserRepo)
	svc := NewWorkspaceService(wsRepo, userRepo)

	ctx := context.Background()
	wsRepo.On("GetBySlug", ctx, "missing").Return(nil, nil)

	err := svc.Delete(ctx, "missing", uuid.New())

	assert.ErrorIs(t, err, ErrWorkspaceNotFound)
}

func TestWorkspaceService_Delete_BlocksActiveDevMachines(t *testing.T) {
	wsRepo := new(mockWorkspaceRepo)
	userRepo := new(mockUserRepo)
	svc := NewWorkspaceService(wsRepo, userRepo)
	ctx := context.Background()
	ownerID := uuid.New()
	ws := &domain.Workspace{ID: uuid.New(), Slug: "test", OwnerID: ownerID}
	wsRepo.On("GetBySlug", ctx, "test").Return(ws, nil)
	wsRepo.On("Delete", ctx, ws.ID).Return(repository.ErrWorkspaceHasDevMachineRuntimes)

	err := svc.Delete(ctx, "test", ownerID)

	assert.ErrorIs(t, err, ErrWorkspaceHasDevMachineRuntimes)
}

func TestWorkspaceService_UpdateMemberRole_PreventsOwnerDemotion(t *testing.T) {
	wsRepo := new(mockWorkspaceRepo)
	userRepo := new(mockUserRepo)
	svc := NewWorkspaceService(wsRepo, userRepo)

	ctx := context.Background()
	workspaceID := uuid.New()
	ownerID := uuid.New()
	wsRepo.On("GetByID", ctx, workspaceID).Return(&domain.Workspace{ID: workspaceID, OwnerID: ownerID}, nil)
	wsRepo.On("GetMember", ctx, workspaceID, ownerID).Return(&domain.WorkspaceMember{WorkspaceID: workspaceID, UserID: ownerID, Role: domain.RoleOwner}, nil)

	err := svc.UpdateMemberRole(ctx, workspaceID, ownerID, domain.RoleAdmin)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "workspace owner cannot be demoted")
	wsRepo.AssertNotCalled(t, "UpdateMemberRole", ctx, workspaceID, ownerID, domain.RoleAdmin)
}

func TestWorkspaceService_UpdateMemberRole_PreventsOwnershipTransfer(t *testing.T) {
	wsRepo := new(mockWorkspaceRepo)
	userRepo := new(mockUserRepo)
	svc := NewWorkspaceService(wsRepo, userRepo)

	ctx := context.Background()
	workspaceID := uuid.New()
	ownerID := uuid.New()
	memberID := uuid.New()
	wsRepo.On("GetByID", ctx, workspaceID).Return(&domain.Workspace{ID: workspaceID, OwnerID: ownerID}, nil)
	wsRepo.On("GetMember", ctx, workspaceID, memberID).Return(&domain.WorkspaceMember{WorkspaceID: workspaceID, UserID: memberID, Role: domain.RoleAdmin}, nil)

	err := svc.UpdateMemberRole(ctx, workspaceID, memberID, domain.RoleOwner)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ownership transfer is not supported")
	wsRepo.AssertNotCalled(t, "UpdateMemberRole", ctx, workspaceID, memberID, domain.RoleOwner)
}

func TestWorkspaceService_UpdateMemberRole_AllowsNonOwnerRoleChange(t *testing.T) {
	wsRepo := new(mockWorkspaceRepo)
	userRepo := new(mockUserRepo)
	svc := NewWorkspaceService(wsRepo, userRepo)

	ctx := context.Background()
	workspaceID := uuid.New()
	ownerID := uuid.New()
	memberID := uuid.New()
	wsRepo.On("GetByID", ctx, workspaceID).Return(&domain.Workspace{ID: workspaceID, OwnerID: ownerID}, nil)
	wsRepo.On("GetMember", ctx, workspaceID, memberID).Return(&domain.WorkspaceMember{WorkspaceID: workspaceID, UserID: memberID, Role: domain.RoleMember}, nil)
	wsRepo.On("UpdateMemberRole", ctx, workspaceID, memberID, domain.RoleAdmin).Return(nil)

	err := svc.UpdateMemberRole(ctx, workspaceID, memberID, domain.RoleAdmin)

	assert.NoError(t, err)
	wsRepo.AssertCalled(t, "UpdateMemberRole", ctx, workspaceID, memberID, domain.RoleAdmin)
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
