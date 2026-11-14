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

func (r *fakeWorkspaceRepo) Create(ctx context.Context, ws *domain.Workspace) error {
	panic("not implemented")
}

func (r *fakeWorkspaceRepo) CreateWithMemberAndLabels(ctx context.Context, ws *domain.Workspace, member *domain.WorkspaceMember, labels []domain.Label) error {
	panic("not implemented")
}

func (r *fakeWorkspaceRepo) GetBySlug(ctx context.Context, slug string) (*domain.Workspace, error) {
	if r.workspace != nil && r.workspace.Slug == slug {
		return r.workspace, nil
	}
	return nil, nil
}

func (r *fakeWorkspaceRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Workspace, error) {
	panic("not implemented")
}

func (r *fakeWorkspaceRepo) Update(ctx context.Context, ws *domain.Workspace) error {
	panic("not implemented")
}

func (r *fakeWorkspaceRepo) Delete(ctx context.Context, id uuid.UUID) ([]domain.TeamIconImage, error) {
	panic("not implemented")
}

func (r *fakeWorkspaceRepo) ListByUser(ctx context.Context, userID uuid.UUID) ([]domain.Workspace, error) {
	panic("not implemented")
}

func (r *fakeWorkspaceRepo) AddMember(ctx context.Context, member *domain.WorkspaceMember) error {
	panic("not implemented")
}

func (r *fakeWorkspaceRepo) GetMember(ctx context.Context, workspaceID, userID uuid.UUID) (*domain.WorkspaceMember, error) {
	return r.member, nil
}

func (r *fakeWorkspaceRepo) ListMembers(ctx context.Context, workspaceID uuid.UUID) ([]domain.WorkspaceMember, error) {
	panic("not implemented")
}

func (r *fakeWorkspaceRepo) ListMembersWithUsers(ctx context.Context, workspaceID uuid.UUID) ([]domain.WorkspaceMemberWithUser, error) {
	panic("not implemented")
}

func (r *fakeWorkspaceRepo) UpdateMemberRole(ctx context.Context, workspaceID, userID uuid.UUID, role string) error {
	panic("not implemented")
}

func (r *fakeWorkspaceRepo) RemoveMember(ctx context.Context, workspaceID, userID uuid.UUID) error {
	panic("not implemented")
}

func (r *fakeWorkspaceRepo) CountMembersByRole(ctx context.Context, workspaceID uuid.UUID, role string) (int, error) {
	panic("not implemented")
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
	handler := WorkspaceMembership(repo)(func(c *echo.Context) error {
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

	// A session minted by another workspace's SSO does not.
	code, _ = runMembershipWithSSO(t, repo, sso, userID, otherWorkspaceID, false)
	assert.Equal(t, http.StatusForbidden, code)

	// Owners keep the break-glass password path.
	repo.member.Role = domain.RoleOwner
	code, _ = runMembershipWithSSO(t, repo, sso, userID, uuid.Nil, false)
	assert.Equal(t, http.StatusNoContent, code)

	// PAT callers are unaffected in phase 1.
	repo.member.Role = domain.RoleMember
	code, _ = runMembershipWithSSO(t, repo, sso, userID, uuid.Nil, true)
	assert.Equal(t, http.StatusNoContent, code)

	// Without require-SSO a password session stays valid.
	repo.member.Role = domain.RoleMember
	code, _ = runMembershipWithSSO(t, repo, fakeSSORequirement{}, userID, uuid.Nil, false)
	assert.Equal(t, http.StatusNoContent, code)
}
