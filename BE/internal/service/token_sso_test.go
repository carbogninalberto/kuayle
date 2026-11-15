package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type fakeTokenSSORequirement map[uuid.UUID]bool

func (f fakeTokenSSORequirement) RequiresSSO(ctx context.Context, workspaceID uuid.UUID) (bool, error) {
	return f[workspaceID], nil
}

// tokenSSOFixture: the user belongs to acme (requires SSO) and other (does not).
type tokenSSOFixture struct {
	ctx           context.Context
	userID        uuid.UUID
	acme, other   domain.Workspace
	tokenRepo     *mockPersonalAccessTokenRepo
	workspaceRepo *mockWorkspaceRepo
	svc           *TokenService
}

func newTokenSSOFixture(role string) *tokenSSOFixture {
	f := &tokenSSOFixture{
		ctx:           context.Background(),
		userID:        uuid.New(),
		acme:          domain.Workspace{ID: uuid.New(), Slug: "acme"},
		other:         domain.Workspace{ID: uuid.New(), Slug: "other"},
		tokenRepo:     new(mockPersonalAccessTokenRepo),
		workspaceRepo: new(mockWorkspaceRepo),
	}
	for _, ws := range []domain.Workspace{f.acme, f.other} {
		ws := ws
		f.workspaceRepo.On("GetBySlug", f.ctx, ws.Slug).Return(&ws, nil).Maybe()
		f.workspaceRepo.On("GetMember", f.ctx, ws.ID, f.userID).Return(&domain.WorkspaceMember{WorkspaceID: ws.ID, UserID: f.userID, Role: role}, nil).Maybe()
	}
	f.workspaceRepo.On("GetBySlug", f.ctx, "missing").Return(nil, nil).Maybe()
	f.workspaceRepo.On("ListByUser", f.ctx, f.userID).Return([]domain.Workspace{f.other, f.acme}, nil).Maybe()
	f.tokenRepo.On("Create", f.ctx, mock.AnythingOfType("*domain.PersonalAccessToken")).Return(nil).Maybe()
	f.svc = NewTokenService(f.tokenRepo, f.workspaceRepo)
	f.svc.SetSSORequirement(fakeTokenSSORequirement{f.acme.ID: true})
	return f
}

func (f *tokenSSOFixture) create(ssoWorkspaceID uuid.UUID, slugs []string) (*domain.PersonalAccessToken, error) {
	token, _, err := f.svc.Create(f.ctx, f.userID, ssoWorkspaceID, dto.CreateTokenRequest{
		Name: "ci", Scopes: []string{domain.PermIssuesRead}, WorkspaceSlugs: slugs,
	})
	return token, err
}

func TestTokenService_PasswordSessionCannotReachSSORequiredWorkspace(t *testing.T) {
	for _, tc := range []struct {
		name  string
		role  string
		slugs []string
	}{
		{"named member", domain.RoleMember, []string{"acme"}},
		{"named among others", domain.RoleMember, []string{"other", "acme"}},
		{"named owner", domain.RoleOwner, []string{"acme"}},
		{"all workspaces (nil)", domain.RoleMember, nil},
		{"all workspaces (empty)", domain.RoleOwner, []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newTokenSSOFixture(tc.role)
			_, err := f.create(uuid.Nil, tc.slugs)
			require.ErrorIs(t, err, ErrTokenWorkspaceRequiresSSO)
			var required *TokenWorkspaceRequiresSSOError
			require.ErrorAs(t, err, &required)
			assert.Equal(t, "acme", required.Slug)
			f.tokenRepo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
		})
	}
}

func TestTokenService_PasswordSessionAllWorkspacesWithoutSSORequirement(t *testing.T) {
	f := newTokenSSOFixture(domain.RoleMember)
	f.svc.SetSSORequirement(fakeTokenSSORequirement{})
	token, err := f.create(uuid.Nil, []string{})
	require.NoError(t, err)
	assert.Nil(t, token.WorkspaceSlugs)
	assert.Nil(t, token.SSOWorkspaceID)

	token, err = f.create(uuid.Nil, []string{"other"})
	require.NoError(t, err)
	assert.Equal(t, []string{"other"}, []string(token.WorkspaceSlugs))
}

func TestTokenService_SSOSessionMintsForItsOwnWorkspaceOnly(t *testing.T) {
	f := newTokenSSOFixture(domain.RoleMember)
	token, err := f.create(f.acme.ID, []string{"acme"})
	require.NoError(t, err)
	require.NotNil(t, token.SSOWorkspaceID)
	assert.Equal(t, f.acme.ID, *token.SSOWorkspaceID)

	for _, slugs := range [][]string{{"other"}, {"acme", "other"}, {}, nil, {"missing"}} {
		f := newTokenSSOFixture(domain.RoleMember)
		_, err := f.create(f.acme.ID, slugs)
		assert.ErrorIs(t, err, ErrTokenSSOSessionScoped, "slugs %v", slugs)
		f.tokenRepo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
	}
}

func TestCLILinkService_CreateCannotReachSSORequiredWorkspace(t *testing.T) {
	for _, slugs := range [][]string{{"acme"}, nil} {
		f := newTokenSSOFixture(domain.RoleMember)
		linkRepo := new(mockCLILinkRepo)
		svc := NewCLILinkService(linkRepo, f.svc, "https://app.example.com")
		_, _, err := svc.Create(f.ctx, f.userID, uuid.Nil, dto.CreateCLILinkRequest{Scopes: []string{domain.PermIssuesRead}, WorkspaceSlugs: slugs})
		assert.ErrorIs(t, err, ErrTokenWorkspaceRequiresSSO, "slugs %v", slugs)
		linkRepo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
	}

	// An SSO-scoped session may create a link for its own workspace, and the
	// link records the workspace.
	f := newTokenSSOFixture(domain.RoleMember)
	linkRepo := new(mockCLILinkRepo)
	linkRepo.On("Create", f.ctx, mock.AnythingOfType("*domain.CLIConnectionLink")).Return(nil)
	svc := NewCLILinkService(linkRepo, f.svc, "https://app.example.com")
	link, _, err := svc.Create(f.ctx, f.userID, f.acme.ID, dto.CreateCLILinkRequest{Scopes: []string{domain.PermIssuesRead}, WorkspaceSlugs: []string{"acme"}})
	require.NoError(t, err)
	require.NotNil(t, link.SSOWorkspaceID)
	assert.Equal(t, f.acme.ID, *link.SSOWorkspaceID)

	_, _, err = svc.Create(f.ctx, f.userID, f.acme.ID, dto.CreateCLILinkRequest{Scopes: []string{domain.PermIssuesRead}})
	assert.ErrorIs(t, err, ErrTokenSSOSessionScoped)
}

func TestCLILinkService_RedeemRechecksRequireSSO(t *testing.T) {
	code := domain.CLILinkPrefix + "abcdefghijklmnopqrstuvwxyz0123456789ABCD"
	for _, tc := range []struct {
		name    string
		slugs   []string
		sso     bool
		wantErr error
	}{
		// Links created from a password session before acme turned
		// require-SSO on mint nothing.
		{"password link naming the workspace", []string{"acme"}, false, ErrTokenWorkspaceRequiresSSO},
		{"password link for all workspaces", nil, false, ErrTokenWorkspaceRequiresSSO},
		{"SSO link for its workspace", []string{"acme"}, true, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newTokenSSOFixture(domain.RoleMember)
			linkRepo := new(mockCLILinkRepo)
			svc := NewCLILinkService(linkRepo, f.svc, "https://app.example.com")
			link := &domain.CLIConnectionLink{
				ID: uuid.New(), UserID: f.userID, CodeHash: domain.HashToken(code), CodePrefix: "kuayle_cl_abcdefgh",
				Scopes: []string{domain.PermIssuesRead}, WorkspaceSlugs: tc.slugs,
				ExpiresAt: time.Now().Add(time.Minute), MaxUses: 1,
			}
			if tc.sso {
				link.SSOWorkspaceID = &f.acme.ID
			}
			linkRepo.On("GetByHash", f.ctx, domain.HashToken(code)).Return(link, nil)
			linkRepo.On("Redeem", f.ctx, domain.HashToken(code)).Return(nil, link).Maybe()

			token, _, err := svc.Redeem(f.ctx, code, "")
			if tc.wantErr != nil {
				assert.ErrorIs(t, err, tc.wantErr)
				linkRepo.AssertNotCalled(t, "Redeem", mock.Anything, mock.Anything)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, token.SSOWorkspaceID)
			assert.Equal(t, f.acme.ID, *token.SSOWorkspaceID)
		})
	}
}
