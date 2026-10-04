package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

type tokenManagementRepoSpy struct {
	repository.PersonalAccessTokenRepo
	listCalls, createCalls, revokeCalls int
}

func (r *tokenManagementRepoSpy) ListByUser(context.Context, uuid.UUID) ([]domain.PersonalAccessToken, error) {
	r.listCalls++
	return []domain.PersonalAccessToken{{Name: "existing secret token"}}, nil
}

func (r *tokenManagementRepoSpy) Create(context.Context, *domain.PersonalAccessToken) error {
	r.createCalls++
	return nil
}

func (r *tokenManagementRepoSpy) Revoke(context.Context, uuid.UUID, uuid.UUID) error {
	r.revokeCalls++
	return nil
}

// Exercise the real service behind each handler. A forbidden response must
// return immediately, even though writing JSON normally returns a nil error.
func TestTokenManagementRejectsPATBeforeServiceAccess(t *testing.T) {
	for _, scopes := range [][]string{{domain.PermAccountRead, domain.PermWorkspaceManage}, {}, nil} {
		for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
			t.Run(method+" with PAT", func(t *testing.T) {
				repo := &tokenManagementRepoSpy{}
				h := NewTokenHandler(service.NewTokenService(repo, nil))
				e := echo.New()
				req := httptest.NewRequest(method, "/api/tokens", strings.NewReader(`{"name":"unauthorized token","scopes":["issues:read"]}`))
				req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
				rec := httptest.NewRecorder()
				c := e.NewContext(req, rec)
				c.Set(string(middleware.UserIDKey), uuid.New())
				c.Set(middleware.TokenScopesKey, scopes)
				c.SetParamNames("id")
				c.SetParamValues(uuid.NewString())
				var err error
				switch method {
				case http.MethodGet:
					err = h.List(c)
				case http.MethodPost:
					err = h.Create(c)
				case http.MethodDelete:
					err = h.Revoke(c)
				}
				require.NoError(t, err)
				require.Equal(t, http.StatusForbidden, rec.Code)
				require.True(t, json.Valid(rec.Body.Bytes()), "response must contain exactly one JSON value: %s", rec.Body.String())
				require.Contains(t, rec.Body.String(), `"code":"FORBIDDEN"`)
				require.NotContains(t, rec.Body.String(), "existing secret token")
				require.NotContains(t, rec.Body.String(), domain.TokenPrefix)
				require.Zero(t, repo.listCalls, "PAT must not inspect other tokens")
				require.Zero(t, repo.createCalls, "PAT must not mint new credentials")
				require.Zero(t, repo.revokeCalls, "PAT must not revoke credentials")
			})
		}
	}
}

func TestTokenManagementSessionStillReachesService(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			repo := &tokenManagementRepoSpy{}
			h := NewTokenHandler(service.NewTokenService(repo, nil))
			e := echo.New()
			req := httptest.NewRequest(method, "/api/tokens", strings.NewReader(`{"name":"session token","scopes":["issues:read"]}`))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.Set(string(middleware.UserIDKey), uuid.New())
			c.SetParamNames("id")
			c.SetParamValues(uuid.NewString())
			switch method {
			case http.MethodGet:
				require.NoError(t, h.List(c))
				require.Equal(t, http.StatusOK, rec.Code)
				require.Equal(t, 1, repo.listCalls)
			case http.MethodPost:
				require.NoError(t, h.Create(c))
				require.Equal(t, http.StatusCreated, rec.Code)
				require.Equal(t, 1, repo.createCalls)
			case http.MethodDelete:
				require.NoError(t, h.Revoke(c))
				require.Equal(t, http.StatusNoContent, rec.Code)
				require.Equal(t, 1, repo.revokeCalls)
			}
		})
	}
}
