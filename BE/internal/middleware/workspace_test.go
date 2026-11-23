package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeWorkspaceRepo struct {
	workspace *domain.Workspace
	member    *domain.WorkspaceMember
}

func (r *fakeWorkspaceRepo) GetBySlug(ctx context.Context, slug string) (*domain.Workspace, error) {
	if r.workspace != nil && r.workspace.Slug == slug {
		return r.workspace, nil
	}
	return nil, nil
}

func (r *fakeWorkspaceRepo) MemberAccess(ctx context.Context, ws, user uuid.UUID) (*domain.RoleAccess, error) {
	if r.member == nil {
		return nil, nil
	}
	return &domain.RoleAccess{Role: r.member.Role, Permissions: domain.RolePermissions[r.member.Role]}, nil
}

func (r *fakeWorkspaceRepo) IssueTeam(context.Context, uuid.UUID, string) (uuid.UUID, error) {
	panic("unexpected issue lookup")
}

func runMembership(t *testing.T, repo *fakeWorkspaceRepo, userID uuid.UUID, tokenWorkspaces []string, markPAT bool) int {
	t.Helper()
	e := echo.New()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/acme/issues", nil)
	c := e.NewContext(req, rec)
	c.SetPathValues(echo.PathValues{{Name: "slug", Value: "acme"}})
	c.Set(string(UserIDKey), userID)
	if markPAT {
		c.Set(TokenWorkspacesKey, tokenWorkspaces)
	}
	handler := WorkspaceMembership(repo, SSONotEnforced)(func(c *echo.Context) error {
		return c.NoContent(http.StatusNoContent)
	})
	require.NoError(t, handler(c))
	return rec.Code
}

func TestWorkspaceMembershipPATWorkspaceGuard(t *testing.T) {
	userID := uuid.New()
	workspaceID := uuid.New()
	repo := &fakeWorkspaceRepo{
		workspace: &domain.Workspace{ID: workspaceID, Slug: "acme"},
		member:    &domain.WorkspaceMember{WorkspaceID: workspaceID, UserID: userID, Role: domain.RoleMember},
	}

	// Token restricted to another workspace is rejected.
	assert.Equal(t, http.StatusForbidden, runMembership(t, repo, userID, []string{"other"}, true))
	// Token covering this workspace passes.
	assert.Equal(t, http.StatusNoContent, runMembership(t, repo, userID, []string{"acme", "other"}, true))
	// Unrestricted token (nil slugs) passes.
	assert.Equal(t, http.StatusNoContent, runMembership(t, repo, userID, nil, true))
	// Plain JWT sessions are unaffected.
	assert.Equal(t, http.StatusNoContent, runMembership(t, repo, userID, nil, false))
}

func TestWorkspaceMembershipNonMember(t *testing.T) {
	repo := &fakeWorkspaceRepo{
		workspace: &domain.Workspace{ID: uuid.New(), Slug: "acme"},
		member:    nil,
	}
	assert.Equal(t, http.StatusForbidden, runMembership(t, repo, uuid.New(), nil, false))
}

type fakeSSORequirement struct {
	required map[uuid.UUID]bool
}

func (f fakeSSORequirement) RequiresSSO(ctx context.Context, workspaceID uuid.UUID) (bool, error) {
	return f.required[workspaceID], nil
}

func runMembershipWithSSO(t *testing.T, repo *fakeWorkspaceRepo, sso SSORequirementSource, userID, sessionSSOWorkspace uuid.UUID, markPAT bool) (int, string) {
	t.Helper()
	e := echo.New()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/acme/issues", nil)
	c := e.NewContext(req, rec)
	c.SetPathValues(echo.PathValues{{Name: "slug", Value: "acme"}})
	c.Set(string(UserIDKey), userID)
	if sessionSSOWorkspace != uuid.Nil {
		c.Set(SessionSSOWorkspaceKey, sessionSSOWorkspace)
	}
	if markPAT {
		c.Set(TokenScopesKey, []string{})
		c.Set(TokenWorkspacesKey, []string(nil))
	}
	handler := WorkspaceMembership(repo, sso)(func(c *echo.Context) error {
		return c.NoContent(http.StatusNoContent)
	})
	require.NoError(t, handler(c))
	return rec.Code, rec.Body.String()
}

func TestWorkspaceMembershipRequiresSSO(t *testing.T) {
	userID := uuid.New()
	workspaceID := uuid.New()
	otherWorkspaceID := uuid.New()
	repo := &fakeWorkspaceRepo{
		workspace: &domain.Workspace{ID: workspaceID, Slug: "acme"},
		member:    &domain.WorkspaceMember{WorkspaceID: workspaceID, UserID: userID, Role: domain.RoleMember},
	}
	sso := fakeSSORequirement{required: map[uuid.UUID]bool{workspaceID: true}}

	code, body := runMembershipWithSSO(t, repo, sso, userID, uuid.Nil, false)
	assert.Equal(t, http.StatusForbidden, code)
	assert.Contains(t, body, `"code":"SSO_REQUIRED"`)
	assert.Contains(t, body, `"workspace_slug":"acme"`)

	// A session minted by this workspace's SSO passes.
	code, _ = runMembershipWithSSO(t, repo, sso, userID, workspaceID, false)
	assert.Equal(t, http.StatusNoContent, code)

	// A session minted by another workspace's SSO is refused as scoped.
	code, body = runMembershipWithSSO(t, repo, sso, userID, otherWorkspaceID, false)
	assert.Equal(t, http.StatusForbidden, code)
	assert.Contains(t, body, `"code":"SSO_SESSION_SCOPED"`)

	// Owners keep the break-glass password path.
	repo.member.Role = domain.RoleOwner
	code, _ = runMembershipWithSSO(t, repo, sso, userID, uuid.Nil, false)
	assert.Equal(t, http.StatusNoContent, code)

	// Without require-SSO a password session stays valid.
	repo.member.Role = domain.RoleMember
	code, _ = runMembershipWithSSO(t, repo, fakeSSORequirement{}, userID, uuid.Nil, false)
	assert.Equal(t, http.StatusNoContent, code)
}

func TestWorkspaceMembershipRequiresSSOSource(t *testing.T) {
	require.Panics(t, func() { WorkspaceMembership(&fakeWorkspaceRepo{}, nil) })
}

func TestWorkspaceMembershipScopesSSOSessionToItsWorkspace(t *testing.T) {
	userID := uuid.New()
	workspaceID := uuid.New()
	otherWorkspaceID := uuid.New()
	repo := &fakeWorkspaceRepo{
		workspace: &domain.Workspace{ID: workspaceID, Slug: "acme"},
		member:    &domain.WorkspaceMember{WorkspaceID: workspaceID, UserID: userID, Role: domain.RoleMember},
	}
	sso := fakeSSORequirement{}

	// The session's own workspace passes.
	code, _ := runMembershipWithSSO(t, repo, sso, userID, workspaceID, false)
	assert.Equal(t, http.StatusNoContent, code)

	// Another workspace is refused even when it does not require SSO.
	code, body := runMembershipWithSSO(t, repo, sso, userID, otherWorkspaceID, false)
	assert.Equal(t, http.StatusForbidden, code)
	assert.Contains(t, body, `"code":"SSO_SESSION_SCOPED"`)

	// Personal access tokens are unaffected by the session scope.
	code, _ = runMembershipWithSSO(t, repo, sso, userID, otherWorkspaceID, true)
	assert.Equal(t, http.StatusNoContent, code)
}

func runMembershipWithPAT(t *testing.T, repo *fakeWorkspaceRepo, sso SSORequirementSource, userID, tokenSSOWorkspace uuid.UUID) (int, string) {
	t.Helper()
	e := echo.New()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/acme/issues", nil)
	c := e.NewContext(req, rec)
	c.SetPathValues(echo.PathValues{{Name: "slug", Value: "acme"}})
	c.Set(string(UserIDKey), userID)
	c.Set(TokenScopesKey, []string{})
	c.Set(TokenWorkspacesKey, []string(nil))
	c.Set(TokenSSOWorkspaceKey, tokenSSOWorkspace)
	handler := WorkspaceMembership(repo, sso)(func(c *echo.Context) error {
		return c.NoContent(http.StatusNoContent)
	})
	require.NoError(t, handler(c))
	return rec.Code, rec.Body.String()
}

// A personal access token reaches a require-SSO workspace only when it was
// minted from a session SSO-scoped to that workspace. Owners get no
// break-glass for tokens, and older tokens stop once require-SSO is on.
func TestWorkspaceMembershipRequiresSSOForPATs(t *testing.T) {
	userID := uuid.New()
	workspaceID := uuid.New()
	otherWorkspaceID := uuid.New()
	required := fakeSSORequirement{required: map[uuid.UUID]bool{workspaceID: true}}
	for _, tc := range []struct {
		name              string
		role              string
		sso               SSORequirementSource
		tokenSSOWorkspace uuid.UUID
		want              int
	}{
		{"password-minted token", domain.RoleMember, required, uuid.Nil, http.StatusForbidden},
		{"password-minted owner token", domain.RoleOwner, required, uuid.Nil, http.StatusForbidden},
		{"token minted by another workspace SSO", domain.RoleMember, required, otherWorkspaceID, http.StatusForbidden},
		{"token minted by this workspace SSO", domain.RoleMember, required, workspaceID, http.StatusNoContent},
		{"workspace without require-SSO", domain.RoleMember, fakeSSORequirement{}, uuid.Nil, http.StatusNoContent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeWorkspaceRepo{
				workspace: &domain.Workspace{ID: workspaceID, Slug: "acme"},
				member:    &domain.WorkspaceMember{WorkspaceID: workspaceID, UserID: userID, Role: tc.role},
			}
			code, body := runMembershipWithPAT(t, repo, tc.sso, userID, tc.tokenSSOWorkspace)
			assert.Equal(t, tc.want, code)
			if tc.want == http.StatusForbidden {
				assert.Contains(t, body, `"code":"SSO_REQUIRED"`)
				assert.Contains(t, body, `"workspace_slug":"acme"`)
			}
		})
	}
}
