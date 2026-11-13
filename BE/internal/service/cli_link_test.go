package service

import (
	"context"
	"database/sql"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockCLILinkRepo struct {
	mock.Mock
}

func (m *mockCLILinkRepo) Create(ctx context.Context, link *domain.CLIConnectionLink) error {
	return m.Called(ctx, link).Error(0)
}

func (m *mockCLILinkRepo) GetByHash(ctx context.Context, hash string) (*domain.CLIConnectionLink, error) {
	args := m.Called(ctx, hash)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.CLIConnectionLink), args.Error(1)
}

func (m *mockCLILinkRepo) ListActiveByUser(ctx context.Context, userID uuid.UUID) ([]domain.CLIConnectionLink, error) {
	args := m.Called(ctx, userID)
	return args.Get(0).([]domain.CLIConnectionLink), args.Error(1)
}

func (m *mockCLILinkRepo) Revoke(ctx context.Context, id, userID uuid.UUID) error {
	return m.Called(ctx, id, userID).Error(0)
}

func (m *mockCLILinkRepo) Redeem(ctx context.Context, codeHash string, build func(link *domain.CLIConnectionLink) (*domain.PersonalAccessToken, error)) (*domain.PersonalAccessToken, error) {
	args := m.Called(ctx, codeHash)
	if err := args.Error(0); err != nil {
		return nil, err
	}
	link := args.Get(1).(*domain.CLIConnectionLink)
	return build(link)
}

func TestCLILinkService_CreateStoresOnlyTheHashAndBuildsTheFragmentLink(t *testing.T) {
	linkRepo := new(mockCLILinkRepo)
	workspaceRepo := new(mockWorkspaceRepo)
	tokenSvc := NewTokenService(new(mockPersonalAccessTokenRepo), workspaceRepo)
	svc := NewCLILinkService(linkRepo, tokenSvc, "https://app.example.com")
	ctx := context.Background()
	userID := uuid.New()
	workspaceID := uuid.New()

	workspaceRepo.On("GetBySlug", ctx, "acme").Return(&domain.Workspace{ID: workspaceID, Slug: "acme"}, nil)
	workspaceRepo.On("GetMember", ctx, workspaceID, userID).Return(&domain.WorkspaceMember{Role: domain.RoleMember}, nil)
	var saved *domain.CLIConnectionLink
	linkRepo.On("Create", ctx, mock.AnythingOfType("*domain.CLIConnectionLink")).Run(func(args mock.Arguments) {
		saved = args.Get(1).(*domain.CLIConnectionLink)
	}).Return(nil)

	link, linkURL, err := svc.Create(ctx, userID, dto.CreateCLILinkRequest{
		Name:           "Laptop",
		Scopes:         []string{domain.PermIssuesRead, domain.PermIssueCreate},
		WorkspaceSlugs: []string{"acme"},
	})
	require.NoError(t, err)
	require.NotNil(t, saved)
	assert.Equal(t, "Laptop", saved.Name)
	ttl := saved.ExpiresAt.Sub(time.Now())
	assert.Greater(t, ttl, 14*time.Minute)
	assert.LessOrEqual(t, ttl, 15*time.Minute)
	assert.Equal(t, 1, saved.MaxUses)
	assert.NotContains(t, saved.CodeHash, domain.CLILinkPrefix)
	parsed, err := url.Parse(linkURL)
	require.NoError(t, err)
	assert.Equal(t, "https://app.example.com/cli/connect", parsed.Scheme+"://"+parsed.Host+parsed.Path)
	fragment, err := url.ParseQuery(parsed.Fragment)
	require.NoError(t, err)
	code := fragment.Get("code")
	assert.True(t, strings.HasPrefix(code, domain.CLILinkPrefix))
	assert.Equal(t, "acme", fragment.Get("workspace"))
	assert.Equal(t, domain.HashToken(code), saved.CodeHash, "the link carries the plaintext whose hash is stored")
	assert.NotContains(t, linkURL, saved.CodeHash)
	assert.False(t, saved.ExpiresAt.IsZero())
	assert.Equal(t, link.ID, saved.ID)
	assert.Equal(t, link.CodePrefix, saved.CodePrefix)
}

func TestCLILinkService_RedeemMintsAScopedToken(t *testing.T) {
	linkRepo := new(mockCLILinkRepo)
	workspaceRepo := new(mockWorkspaceRepo)
	tokenSvc := NewTokenService(new(mockPersonalAccessTokenRepo), workspaceRepo)
	svc := NewCLILinkService(linkRepo, tokenSvc, "https://app.example.com")
	ctx := context.Background()
	code := domain.CLILinkPrefix + "abcdefghijklmnopqrstuvwxyz0123456789ABCD"
	expires := time.Now().Add(time.Hour)
	link := &domain.CLIConnectionLink{
		ID: uuid.New(), UserID: uuid.New(), CodeHash: domain.HashToken(code), CodePrefix: "kuayle_cl_abcdefgh",
		Scopes: []string{domain.PermIssuesRead}, TokenExpiresAt: &expires,
		ExpiresAt: time.Now().Add(15 * time.Minute), MaxUses: 2,
	}

	linkRepo.On("GetByHash", ctx, domain.HashToken(code)).Return(link, nil)
	linkRepo.On("Redeem", ctx, domain.HashToken(code)).Return(nil, link)

	token, plaintext, err := svc.Redeem(ctx, code, "laptop")
	require.NoError(t, err)
	require.NotEmpty(t, plaintext)
	assert.True(t, len(plaintext) > len(domain.TokenPrefix))
	assert.Equal(t, domain.HashToken(plaintext), token.TokenHash)
	assert.Equal(t, link.UserID, token.UserID)
	assert.Equal(t, []string(link.Scopes), []string(token.Scopes))
	assert.Equal(t, &link.ID, token.ConnectionLinkID)
	assert.Equal(t, link.TokenExpiresAt, token.ExpiresAt)
	assert.Contains(t, token.Name, "kuayle_cl_abcdefgh")
	linkRepo.AssertExpectations(t)
}

func TestCLILinkService_RedeemRejectsBadLinkStates(t *testing.T) {
	code := domain.CLILinkPrefix + "abcdefghijklmnopqrstuvwxyz0123456789ABCD"
	ctx := context.Background()
	now := time.Now()
	for _, tc := range []struct {
		name string
		link *domain.CLIConnectionLink
		want error
	}{
		{"unknown", nil, repository.ErrCLILinkInvalid},
		{"expired", &domain.CLIConnectionLink{ExpiresAt: now.Add(-time.Minute), MaxUses: 1}, repository.ErrCLILinkExpired},
		{"used", &domain.CLIConnectionLink{ExpiresAt: now.Add(time.Minute), MaxUses: 1, Uses: 1}, repository.ErrCLILinkUsed},
		{"revoked", &domain.CLIConnectionLink{ExpiresAt: now.Add(time.Minute), MaxUses: 1, RevokedAt: &now}, repository.ErrCLILinkRevoked},
	} {
		t.Run(tc.name, func(t *testing.T) {
			linkRepo := new(mockCLILinkRepo)
			tokenSvc := NewTokenService(new(mockPersonalAccessTokenRepo), new(mockWorkspaceRepo))
			svc := NewCLILinkService(linkRepo, tokenSvc, "https://app.example.com")
			linkRepo.On("GetByHash", ctx, domain.HashToken(code)).Return(tc.link, nil)
			_, _, err := svc.Redeem(ctx, code, "")
			assert.ErrorIs(t, err, tc.want)
			linkRepo.AssertNotCalled(t, "Redeem", mock.Anything, mock.Anything)
		})
	}
}

func TestCLILinkService_RedeemRejectsMalformedCode(t *testing.T) {
	svc := NewCLILinkService(new(mockCLILinkRepo), NewTokenService(new(mockPersonalAccessTokenRepo), new(mockWorkspaceRepo)), "https://app.example.com")
	_, _, err := svc.Redeem(context.Background(), "not-a-code", "")
	assert.ErrorIs(t, err, repository.ErrCLILinkInvalid)
}

func TestCLILinkService_RedeemRechecksMembership(t *testing.T) {
	linkRepo := new(mockCLILinkRepo)
	workspaceRepo := new(mockWorkspaceRepo)
	tokenSvc := NewTokenService(new(mockPersonalAccessTokenRepo), workspaceRepo)
	svc := NewCLILinkService(linkRepo, tokenSvc, "https://app.example.com")
	ctx := context.Background()
	code := domain.CLILinkPrefix + "abcdefghijklmnopqrstuvwxyz0123456789ABCD"
	link := &domain.CLIConnectionLink{
		ID: uuid.New(), UserID: uuid.New(), CodeHash: domain.HashToken(code),
		WorkspaceSlugs: []string{"gone"}, ExpiresAt: time.Now().Add(time.Minute), MaxUses: 1,
	}
	linkRepo.On("GetByHash", ctx, domain.HashToken(code)).Return(link, nil)
	workspaceRepo.On("GetBySlug", ctx, "gone").Return(&domain.Workspace{ID: uuid.New(), Slug: "gone"}, nil)
	workspaceRepo.On("GetMember", ctx, mock.Anything, link.UserID).Return(nil, nil)

	_, _, err := svc.Redeem(ctx, code, "")
	assert.ErrorIs(t, err, ErrCLILinkWorkspace)
	linkRepo.AssertNotCalled(t, "Redeem", mock.Anything, mock.Anything)
}

func TestCLILinkService_RevokeUnknown(t *testing.T) {
	linkRepo := new(mockCLILinkRepo)
	svc := NewCLILinkService(linkRepo, NewTokenService(new(mockPersonalAccessTokenRepo), new(mockWorkspaceRepo)), "https://app.example.com")
	ctx := context.Background()
	id, userID := uuid.New(), uuid.New()
	linkRepo.On("Revoke", ctx, id, userID).Return(sql.ErrNoRows)
	assert.ErrorIs(t, svc.Revoke(ctx, id, userID), ErrCLILinkNotFound)
}
