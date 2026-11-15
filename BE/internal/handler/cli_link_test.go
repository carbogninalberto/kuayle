package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

type cliLinkRepoSpy struct {
	repository.CLILinkRepo
	link                     *domain.CLIConnectionLink
	createCalls, redeemCalls int
}

func (r *cliLinkRepoSpy) Create(ctx context.Context, link *domain.CLIConnectionLink) error {
	r.createCalls++
	return nil
}

func (r *cliLinkRepoSpy) GetByHash(ctx context.Context, hash string) (*domain.CLIConnectionLink, error) {
	if r.link != nil && r.link.CodeHash == hash {
		return r.link, nil
	}
	return nil, nil
}

func (r *cliLinkRepoSpy) Redeem(ctx context.Context, codeHash string, build func(*domain.CLIConnectionLink) (*domain.PersonalAccessToken, error)) (*domain.PersonalAccessToken, error) {
	r.redeemCalls++
	return build(r.link)
}

func newCLILinkSSOHandler(workspace *domain.Workspace, repo *cliLinkRepoSpy) *CLILinkHandler {
	tokenSvc := service.NewTokenService(&tokenManagementRepoSpy{}, tokenScopeWorkspaceRepo{workspace: workspace})
	tokenSvc.SetSSORequirement(tokenSSORequirement{workspace.ID: true})
	return NewCLILinkHandler(service.NewCLILinkService(repo, tokenSvc, "https://app.example.com"))
}

func TestCLILinkCreateRespectsRequireSSO(t *testing.T) {
	workspace := &domain.Workspace{ID: uuid.New(), Slug: "acme"}
	for _, tc := range []struct {
		name       string
		slugsJSON  string
		ssoSession bool
		status     int
		code       string
	}{
		{"password session naming the workspace", `,"workspace_slugs":["acme"]`, false, http.StatusForbidden, "SSO_REQUIRED"},
		{"password session for all workspaces", ``, false, http.StatusForbidden, "SSO_REQUIRED"},
		{"SSO session for all workspaces", ``, true, http.StatusForbidden, "SSO_SESSION_SCOPED"},
		{"SSO session for its workspace", `,"workspace_slugs":["acme"]`, true, http.StatusCreated, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &cliLinkRepoSpy{}
			h := newCLILinkSSOHandler(workspace, repo)
			req := httptest.NewRequest(http.MethodPost, "/api/cli-links", strings.NewReader(`{"scopes":["issues:read"]`+tc.slugsJSON+`}`))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			rec := httptest.NewRecorder()
			c := echo.New().NewContext(req, rec)
			c.Set(string(middleware.UserIDKey), uuid.New())
			if tc.ssoSession {
				c.Set(middleware.SessionSSOWorkspaceKey, workspace.ID)
			}
			require.NoError(t, h.Create(c))
			require.Equal(t, tc.status, rec.Code, rec.Body.String())
			if tc.code != "" {
				require.Contains(t, rec.Body.String(), `"code":"`+tc.code+`"`)
				require.Zero(t, repo.createCalls)
			} else {
				require.Equal(t, 1, repo.createCalls)
			}
		})
	}
}

// A link created from a password session before the workspace turned
// require-SSO on must not mint a token for it.
func TestCLILinkRedeemRefusesWorkspaceThatNowRequiresSSO(t *testing.T) {
	workspace := &domain.Workspace{ID: uuid.New(), Slug: "acme"}
	code := domain.CLILinkPrefix + "abcdefghijklmnopqrstuvwxyz0123456789ABCD"
	repo := &cliLinkRepoSpy{link: &domain.CLIConnectionLink{
		ID: uuid.New(), UserID: uuid.New(), CodeHash: domain.HashToken(code), CodePrefix: "kuayle_cl_abcdefgh",
		Scopes: []string{domain.PermIssuesRead}, WorkspaceSlugs: []string{"acme"},
		ExpiresAt: time.Now().Add(time.Minute), MaxUses: 1,
	}}
	h := newCLILinkSSOHandler(workspace, repo)
	req := httptest.NewRequest(http.MethodPost, "/api/cli/connect", strings.NewReader(`{"code":"`+code+`"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	require.NoError(t, h.Connect(echo.New().NewContext(req, rec)))
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), `"code":"SSO_REQUIRED"`)
	require.NotContains(t, rec.Body.String(), domain.TokenPrefix)
	require.Zero(t, repo.redeemCalls)
}
