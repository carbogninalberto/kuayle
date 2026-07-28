package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// --- Mocks ---

type mockInviteLinkRepo struct {
	mock.Mock
}

func (m *mockInviteLinkRepo) Create(ctx context.Context, link *domain.WorkspaceInviteLink) error {
	args := m.Called(ctx, link)
	return args.Error(0)
}

func (m *mockInviteLinkRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.WorkspaceInviteLink, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.WorkspaceInviteLink), args.Error(1)
}

func (m *mockInviteLinkRepo) GetByTokenHash(ctx context.Context, hash string) (*domain.WorkspaceInviteLink, error) {
	args := m.Called(ctx, hash)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.WorkspaceInviteLink), args.Error(1)
}

func (m *mockInviteLinkRepo) ListByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]domain.WorkspaceInviteLink, error) {
	args := m.Called(ctx, workspaceID)
	return args.Get(0).([]domain.WorkspaceInviteLink), args.Error(1)
}

func (m *mockInviteLinkRepo) Revoke(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *mockInviteLinkRepo) TryConsumeUse(ctx context.Context, id uuid.UUID) (bool, error) {
	args := m.Called(ctx, id)
	return args.Bool(0), args.Error(1)
}

func (m *mockInviteLinkRepo) ReleaseUse(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

// --- Helpers ---

func validInviteLink(workspaceID uuid.UUID) *domain.WorkspaceInviteLink {
	return &domain.WorkspaceInviteLink{
		ID:          uuid.New(),
		WorkspaceID: workspaceID,
		TokenHash:   "hash",
		Role:        domain.RoleMember,
		CreatedBy:   uuid.New(),
		ExpiresAt:   time.Now().Add(7 * 24 * time.Hour),
	}
}

// --- Create ---

func TestInviteLinkService_Create_Happy(t *testing.T) {
	linkRepo := new(mockInviteLinkRepo)
	wsRepo := new(mockWorkspaceRepo)
	svc := NewInviteLinkService(linkRepo, wsRepo)

	ctx := context.Background()
	wsID := uuid.New()
	creatorID := uuid.New()

	linkRepo.On("Create", ctx, mock.AnythingOfType("*domain.WorkspaceInviteLink")).Return(nil)

	link, rawToken, err := svc.Create(ctx, wsID, creatorID, dto.CreateInviteLinkRequest{Role: "member"})

	assert.NoError(t, err)
	assert.NotNil(t, link)
	assert.NotEmpty(t, rawToken)
	assert.Equal(t, wsID, link.WorkspaceID)
	assert.Equal(t, creatorID, link.CreatedBy)
	assert.Equal(t, domain.RoleMember, link.Role)
	assert.Equal(t, hashInviteToken(rawToken), link.TokenHash)
	assert.NotEqual(t, rawToken, link.TokenHash)
	assert.Nil(t, link.MaxUses)
	// default expiry: 7 days
	assert.WithinDuration(t, time.Now().Add(7*24*time.Hour), link.ExpiresAt, time.Minute)
}

func TestInviteLinkService_Create_CustomExpiryAndMaxUses(t *testing.T) {
	linkRepo := new(mockInviteLinkRepo)
	wsRepo := new(mockWorkspaceRepo)
	svc := NewInviteLinkService(linkRepo, wsRepo)

	ctx := context.Background()
	days := 30
	maxUses := 5

	linkRepo.On("Create", ctx, mock.AnythingOfType("*domain.WorkspaceInviteLink")).Return(nil)

	link, _, err := svc.Create(ctx, uuid.New(), uuid.New(), dto.CreateInviteLinkRequest{
		Role: "guest", ExpiresInDays: &days, MaxUses: &maxUses,
	})

	assert.NoError(t, err)
	assert.Equal(t, domain.RoleGuest, link.Role)
	assert.WithinDuration(t, time.Now().Add(30*24*time.Hour), link.ExpiresAt, time.Minute)
	assert.NotNil(t, link.MaxUses)
	assert.Equal(t, 5, *link.MaxUses)
}

func TestInviteLinkService_Create_RejectsPrivilegedRole(t *testing.T) {
	linkRepo := new(mockInviteLinkRepo)
	wsRepo := new(mockWorkspaceRepo)
	svc := NewInviteLinkService(linkRepo, wsRepo)

	for _, role := range []string{"admin", "owner"} {
		link, token, err := svc.Create(context.Background(), uuid.New(), uuid.New(), dto.CreateInviteLinkRequest{Role: role})
		assert.Error(t, err)
		assert.Nil(t, link)
		assert.Empty(t, token)
	}
	linkRepo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
}

// --- Revoke ---

func TestInviteLinkService_Revoke_Happy(t *testing.T) {
	linkRepo := new(mockInviteLinkRepo)
	wsRepo := new(mockWorkspaceRepo)
	svc := NewInviteLinkService(linkRepo, wsRepo)

	ctx := context.Background()
	wsID := uuid.New()
	link := validInviteLink(wsID)

	linkRepo.On("GetByID", ctx, link.ID).Return(link, nil)
	linkRepo.On("Revoke", ctx, link.ID).Return(nil)

	err := svc.Revoke(ctx, wsID, link.ID, uuid.New())

	assert.NoError(t, err)
	linkRepo.AssertCalled(t, "Revoke", ctx, link.ID)
}

func TestInviteLinkService_Revoke_WrongWorkspace(t *testing.T) {
	linkRepo := new(mockInviteLinkRepo)
	wsRepo := new(mockWorkspaceRepo)
	svc := NewInviteLinkService(linkRepo, wsRepo)

	ctx := context.Background()
	link := validInviteLink(uuid.New())

	linkRepo.On("GetByID", ctx, link.ID).Return(link, nil)

	err := svc.Revoke(ctx, uuid.New(), link.ID, uuid.New())

	assert.ErrorIs(t, err, ErrInviteLinkInvalid)
	linkRepo.AssertNotCalled(t, "Revoke", mock.Anything, mock.Anything)
}

func TestInviteLinkService_Revoke_NotFound(t *testing.T) {
	linkRepo := new(mockInviteLinkRepo)
	wsRepo := new(mockWorkspaceRepo)
	svc := NewInviteLinkService(linkRepo, wsRepo)

	ctx := context.Background()
	linkID := uuid.New()
	linkRepo.On("GetByID", ctx, linkID).Return(nil, nil)

	err := svc.Revoke(ctx, uuid.New(), linkID, uuid.New())

	assert.ErrorIs(t, err, ErrInviteLinkInvalid)
}

// --- Preview ---

func TestInviteLinkService_Preview_Valid(t *testing.T) {
	linkRepo := new(mockInviteLinkRepo)
	wsRepo := new(mockWorkspaceRepo)
	svc := NewInviteLinkService(linkRepo, wsRepo)

	ctx := context.Background()
	wsID := uuid.New()
	link := validInviteLink(wsID)
	ws := &domain.Workspace{ID: wsID, Name: "Acme", Slug: "acme"}

	linkRepo.On("GetByTokenHash", ctx, hashInviteToken("tok")).Return(link, nil)
	wsRepo.On("GetByID", ctx, wsID).Return(ws, nil)

	preview, err := svc.Preview(ctx, "tok")

	assert.NoError(t, err)
	assert.Equal(t, "Acme", preview.WorkspaceName)
	assert.Equal(t, "acme", preview.WorkspaceSlug)
	assert.Equal(t, domain.RoleMember, preview.Role)
	assert.Equal(t, dto.InviteStatusValid, preview.Status)
}

func TestInviteLinkService_Preview_UnknownToken(t *testing.T) {
	linkRepo := new(mockInviteLinkRepo)
	wsRepo := new(mockWorkspaceRepo)
	svc := NewInviteLinkService(linkRepo, wsRepo)

	ctx := context.Background()
	linkRepo.On("GetByTokenHash", ctx, mock.Anything).Return(nil, nil)

	_, err := svc.Preview(ctx, "nope")

	assert.ErrorIs(t, err, ErrInviteLinkInvalid)
}

func TestInviteLinkService_Preview_Statuses(t *testing.T) {
	ctx := context.Background()
	wsID := uuid.New()
	ws := &domain.Workspace{ID: wsID, Name: "Acme", Slug: "acme"}
	now := time.Now()
	maxUses := 3

	cases := []struct {
		name   string
		mutate func(*domain.WorkspaceInviteLink)
		status string
	}{
		{"revoked", func(l *domain.WorkspaceInviteLink) { l.RevokedAt = &now }, dto.InviteStatusRevoked},
		{"expired", func(l *domain.WorkspaceInviteLink) { l.ExpiresAt = now.Add(-time.Hour) }, dto.InviteStatusExpired},
		{"exhausted", func(l *domain.WorkspaceInviteLink) { l.MaxUses = &maxUses; l.UseCount = 3 }, dto.InviteStatusExhausted},
		{"below max uses still valid", func(l *domain.WorkspaceInviteLink) { l.MaxUses = &maxUses; l.UseCount = 2 }, dto.InviteStatusValid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			linkRepo := new(mockInviteLinkRepo)
			wsRepo := new(mockWorkspaceRepo)
			svc := NewInviteLinkService(linkRepo, wsRepo)

			link := validInviteLink(wsID)
			tc.mutate(link)
			linkRepo.On("GetByTokenHash", ctx, mock.Anything).Return(link, nil)
			wsRepo.On("GetByID", ctx, wsID).Return(ws, nil)

			preview, err := svc.Preview(ctx, "tok")

			assert.NoError(t, err)
			assert.Equal(t, tc.status, preview.Status)
		})
	}
}

// --- Accept ---

func TestInviteLinkService_Accept_Happy(t *testing.T) {
	linkRepo := new(mockInviteLinkRepo)
	wsRepo := new(mockWorkspaceRepo)
	svc := NewInviteLinkService(linkRepo, wsRepo)

	ctx := context.Background()
	wsID := uuid.New()
	userID := uuid.New()
	link := validInviteLink(wsID)
	ws := &domain.Workspace{ID: wsID, Name: "Acme", Slug: "acme"}

	linkRepo.On("GetByTokenHash", ctx, mock.Anything).Return(link, nil)
	wsRepo.On("GetByID", ctx, wsID).Return(ws, nil)
	wsRepo.On("GetMember", ctx, wsID, userID).Return(nil, nil)
	linkRepo.On("TryConsumeUse", ctx, link.ID).Return(true, nil)
	wsRepo.On("AddMember", ctx, mock.MatchedBy(func(m *domain.WorkspaceMember) bool {
		return m.WorkspaceID == wsID && m.UserID == userID && m.Role == domain.RoleMember
	})).Return(nil)

	gotWS, role, err := svc.Accept(ctx, "tok", userID)

	assert.NoError(t, err)
	assert.Equal(t, ws, gotWS)
	assert.Equal(t, domain.RoleMember, role)
	linkRepo.AssertCalled(t, "TryConsumeUse", ctx, link.ID)
}

func TestInviteLinkService_Accept_IdempotentForMember(t *testing.T) {
	linkRepo := new(mockInviteLinkRepo)
	wsRepo := new(mockWorkspaceRepo)
	svc := NewInviteLinkService(linkRepo, wsRepo)

	ctx := context.Background()
	wsID := uuid.New()
	userID := uuid.New()
	link := validInviteLink(wsID)
	ws := &domain.Workspace{ID: wsID, Name: "Acme", Slug: "acme"}

	linkRepo.On("GetByTokenHash", ctx, mock.Anything).Return(link, nil)
	wsRepo.On("GetByID", ctx, wsID).Return(ws, nil)
	wsRepo.On("GetMember", ctx, wsID, userID).Return(&domain.WorkspaceMember{
		WorkspaceID: wsID, UserID: userID, Role: domain.RoleAdmin,
	}, nil)

	gotWS, role, err := svc.Accept(ctx, "tok", userID)

	assert.NoError(t, err)
	assert.Equal(t, ws, gotWS)
	assert.Equal(t, domain.RoleAdmin, role) // keeps the existing role
	linkRepo.AssertNotCalled(t, "TryConsumeUse", mock.Anything, mock.Anything)
	wsRepo.AssertNotCalled(t, "AddMember", mock.Anything, mock.Anything)
}

func TestInviteLinkService_Accept_Expired(t *testing.T) {
	linkRepo := new(mockInviteLinkRepo)
	wsRepo := new(mockWorkspaceRepo)
	svc := NewInviteLinkService(linkRepo, wsRepo)

	ctx := context.Background()
	link := validInviteLink(uuid.New())
	link.ExpiresAt = time.Now().Add(-time.Hour)

	linkRepo.On("GetByTokenHash", ctx, mock.Anything).Return(link, nil)

	_, _, err := svc.Accept(ctx, "tok", uuid.New())

	assert.ErrorIs(t, err, ErrInviteLinkExpired)
	linkRepo.AssertNotCalled(t, "TryConsumeUse", mock.Anything, mock.Anything)
}

func TestInviteLinkService_Accept_Revoked(t *testing.T) {
	linkRepo := new(mockInviteLinkRepo)
	wsRepo := new(mockWorkspaceRepo)
	svc := NewInviteLinkService(linkRepo, wsRepo)

	ctx := context.Background()
	now := time.Now()
	link := validInviteLink(uuid.New())
	link.RevokedAt = &now

	linkRepo.On("GetByTokenHash", ctx, mock.Anything).Return(link, nil)

	_, _, err := svc.Accept(ctx, "tok", uuid.New())

	assert.ErrorIs(t, err, ErrInviteLinkRevoked)
}

func TestInviteLinkService_Accept_MaxUsesReached(t *testing.T) {
	linkRepo := new(mockInviteLinkRepo)
	wsRepo := new(mockWorkspaceRepo)
	svc := NewInviteLinkService(linkRepo, wsRepo)

	ctx := context.Background()
	maxUses := 2
	link := validInviteLink(uuid.New())
	link.MaxUses = &maxUses
	link.UseCount = 2

	linkRepo.On("GetByTokenHash", ctx, mock.Anything).Return(link, nil)

	_, _, err := svc.Accept(ctx, "tok", uuid.New())

	assert.ErrorIs(t, err, ErrInviteLinkExhausted)
}

func TestInviteLinkService_Accept_UseCountRace(t *testing.T) {
	linkRepo := new(mockInviteLinkRepo)
	wsRepo := new(mockWorkspaceRepo)
	svc := NewInviteLinkService(linkRepo, wsRepo)

	ctx := context.Background()
	wsID := uuid.New()
	userID := uuid.New()
	// passes the stale-read check but loses the atomic consume race
	link := validInviteLink(wsID)
	ws := &domain.Workspace{ID: wsID, Name: "Acme", Slug: "acme"}

	linkRepo.On("GetByTokenHash", ctx, mock.Anything).Return(link, nil)
	wsRepo.On("GetByID", ctx, wsID).Return(ws, nil)
	wsRepo.On("GetMember", ctx, wsID, userID).Return(nil, nil)
	linkRepo.On("TryConsumeUse", ctx, link.ID).Return(false, nil)

	_, _, err := svc.Accept(ctx, "tok", userID)

	assert.ErrorIs(t, err, ErrInviteLinkExhausted)
	wsRepo.AssertNotCalled(t, "AddMember", mock.Anything, mock.Anything)
}

func TestInviteLinkService_Accept_AddMemberFailureReleasesUse(t *testing.T) {
	linkRepo := new(mockInviteLinkRepo)
	wsRepo := new(mockWorkspaceRepo)
	svc := NewInviteLinkService(linkRepo, wsRepo)

	ctx := context.Background()
	wsID := uuid.New()
	userID := uuid.New()
	link := validInviteLink(wsID)
	ws := &domain.Workspace{ID: wsID, Name: "Acme", Slug: "acme"}
	addErr := errors.New("db boom")

	linkRepo.On("GetByTokenHash", ctx, mock.Anything).Return(link, nil)
	wsRepo.On("GetByID", ctx, wsID).Return(ws, nil)
	wsRepo.On("GetMember", ctx, wsID, userID).Return(nil, nil)
	linkRepo.On("TryConsumeUse", ctx, link.ID).Return(true, nil)
	wsRepo.On("AddMember", ctx, mock.AnythingOfType("*domain.WorkspaceMember")).Return(addErr)
	linkRepo.On("ReleaseUse", ctx, link.ID).Return(nil)

	_, _, err := svc.Accept(ctx, "tok", userID)

	assert.ErrorIs(t, err, addErr)
	linkRepo.AssertCalled(t, "ReleaseUse", ctx, link.ID)
}

func TestInviteLinkService_Accept_InvalidToken(t *testing.T) {
	linkRepo := new(mockInviteLinkRepo)
	wsRepo := new(mockWorkspaceRepo)
	svc := NewInviteLinkService(linkRepo, wsRepo)

	ctx := context.Background()
	linkRepo.On("GetByTokenHash", ctx, mock.Anything).Return(nil, nil)

	_, _, err := svc.Accept(ctx, "nope", uuid.New())

	assert.ErrorIs(t, err, ErrInviteLinkInvalid)
}
