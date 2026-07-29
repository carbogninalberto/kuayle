package service

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockPersonalAccessTokenRepo struct {
	mock.Mock
}

func (m *mockPersonalAccessTokenRepo) Create(ctx context.Context, token *domain.PersonalAccessToken) error {
	args := m.Called(ctx, token)
	return args.Error(0)
}

func (m *mockPersonalAccessTokenRepo) GetByHash(ctx context.Context, hash string) (*domain.PersonalAccessToken, error) {
	args := m.Called(ctx, hash)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.PersonalAccessToken), args.Error(1)
}

func (m *mockPersonalAccessTokenRepo) ListByUser(ctx context.Context, userID uuid.UUID) ([]domain.PersonalAccessToken, error) {
	args := m.Called(ctx, userID)
	return args.Get(0).([]domain.PersonalAccessToken), args.Error(1)
}

func (m *mockPersonalAccessTokenRepo) Revoke(ctx context.Context, id, userID uuid.UUID) error {
	args := m.Called(ctx, id, userID)
	return args.Error(0)
}

func (m *mockPersonalAccessTokenRepo) UpdateLastUsed(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func TestTokenService_Create(t *testing.T) {
	tokenRepo := new(mockPersonalAccessTokenRepo)
	workspaceRepo := new(mockWorkspaceRepo)
	svc := NewTokenService(tokenRepo, workspaceRepo)
	ctx := context.Background()
	userID := uuid.New()
	workspaceID := uuid.New()
	expires := time.Now().Add(24 * time.Hour)

	workspaceRepo.On("GetBySlug", ctx, "acme").Return(&domain.Workspace{ID: workspaceID, Slug: "acme"}, nil)
	workspaceRepo.On("GetMember", ctx, workspaceID, userID).Return(&domain.WorkspaceMember{WorkspaceID: workspaceID, UserID: userID, Role: domain.RoleMember}, nil)
	tokenRepo.On("Create", ctx, mock.AnythingOfType("*domain.PersonalAccessToken")).Return(nil)

	token, plaintext, err := svc.Create(ctx, userID, dto.CreateTokenRequest{
		Name:           "ci",
		Scopes:         []string{"issues:read", "issue:create"},
		WorkspaceSlugs: []string{"acme"},
		ExpiresAt:      &expires,
	})

	require.NoError(t, err)
	assert.Equal(t, userID, token.UserID)
	assert.Equal(t, "ci", token.Name)
	assert.Equal(t, domain.HashToken(plaintext), token.TokenHash)
	assert.NotContains(t, plaintext, token.TokenHash)
	tokenRepo.AssertExpectations(t)
	workspaceRepo.AssertExpectations(t)
}

func TestTokenService_Create_InvalidScope(t *testing.T) {
	tokenRepo := new(mockPersonalAccessTokenRepo)
	svc := NewTokenService(tokenRepo, new(mockWorkspaceRepo))

	_, _, err := svc.Create(context.Background(), uuid.New(), dto.CreateTokenRequest{
		Name:   "ci",
		Scopes: []string{"issues:read", "root:everything"},
	})

	assert.ErrorIs(t, err, ErrInvalidTokenScope)
	tokenRepo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
}

func TestTokenService_Create_UnknownWorkspace(t *testing.T) {
	tokenRepo := new(mockPersonalAccessTokenRepo)
	workspaceRepo := new(mockWorkspaceRepo)
	svc := NewTokenService(tokenRepo, workspaceRepo)
	ctx := context.Background()

	workspaceRepo.On("GetBySlug", ctx, "ghost").Return(nil, nil)

	_, _, err := svc.Create(ctx, uuid.New(), dto.CreateTokenRequest{
		Name:           "ci",
		Scopes:         []string{"issues:read"},
		WorkspaceSlugs: []string{"ghost"},
	})

	assert.ErrorIs(t, err, ErrInvalidTokenWorkspace)
	tokenRepo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
}

func TestTokenService_Create_NotWorkspaceMember(t *testing.T) {
	tokenRepo := new(mockPersonalAccessTokenRepo)
	workspaceRepo := new(mockWorkspaceRepo)
	svc := NewTokenService(tokenRepo, workspaceRepo)
	ctx := context.Background()
	userID := uuid.New()
	workspaceID := uuid.New()

	workspaceRepo.On("GetBySlug", ctx, "acme").Return(&domain.Workspace{ID: workspaceID, Slug: "acme"}, nil)
	workspaceRepo.On("GetMember", ctx, workspaceID, userID).Return(nil, nil)

	_, _, err := svc.Create(ctx, userID, dto.CreateTokenRequest{
		Name:           "ci",
		Scopes:         []string{"issues:read"},
		WorkspaceSlugs: []string{"acme"},
	})

	assert.ErrorIs(t, err, ErrInvalidTokenWorkspace)
	tokenRepo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
}

func TestTokenService_Create_PastExpiry(t *testing.T) {
	tokenRepo := new(mockPersonalAccessTokenRepo)
	svc := NewTokenService(tokenRepo, new(mockWorkspaceRepo))
	past := time.Now().Add(-time.Hour)

	_, _, err := svc.Create(context.Background(), uuid.New(), dto.CreateTokenRequest{
		Name:      "ci",
		Scopes:    []string{"issues:read"},
		ExpiresAt: &past,
	})

	assert.ErrorIs(t, err, ErrInvalidTokenExpiry)
	tokenRepo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
}

func TestTokenService_Revoke_NotFound(t *testing.T) {
	tokenRepo := new(mockPersonalAccessTokenRepo)
	svc := NewTokenService(tokenRepo, new(mockWorkspaceRepo))
	ctx := context.Background()
	id := uuid.New()
	userID := uuid.New()

	// Unknown id or another user's token: both surface as "token not found".
	tokenRepo.On("Revoke", ctx, id, userID).Return(sql.ErrNoRows)

	err := svc.Revoke(ctx, id, userID)

	assert.EqualError(t, err, "token not found")
}
