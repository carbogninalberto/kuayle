package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/handler"
	mw "github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

type memoryViewRepo struct{ views map[uuid.UUID]*domain.View }

func (r *memoryViewRepo) Create(_ context.Context, view *domain.View) error {
	r.views[view.ID] = view
	return nil
}

func (r *memoryViewRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.View, error) {
	view, ok := r.views[id]
	if !ok {
		return nil, nil
	}
	clone := *view
	return &clone, nil
}

func (r *memoryViewRepo) ListByWorkspace(context.Context, uuid.UUID, uuid.UUID) ([]domain.View, error) {
	return nil, nil
}

func (r *memoryViewRepo) Update(_ context.Context, view *domain.View) error {
	r.views[view.ID] = view
	return nil
}

func (r *memoryViewRepo) Delete(_ context.Context, id uuid.UUID) error {
	delete(r.views, id)
	return nil
}

// Saved-view writes were session-only, which does not constrain a guest: a
// guest could publish a shared view to every member. They need view:manage,
// which members hold and guests do not. The requests run through the real
// handler and service so a rejected write is shown to change nothing.
func TestViewWritesRequireViewManage(t *testing.T) {
	for _, tc := range []struct {
		role    string
		allowed bool
	}{
		{domain.RoleOwner, true},
		{domain.RoleAdmin, true},
		{domain.RoleMember, true},
		{domain.RoleGuest, false},
	} {
		t.Run(tc.role, func(t *testing.T) {
			ws := &domain.Workspace{ID: uuid.New(), Slug: "acme"}
			userID := uuid.New()
			// The caller's own view, so only the role decides the outcome.
			own := &domain.View{ID: uuid.New(), WorkspaceID: ws.ID, CreatorID: userID, Name: "Mine"}
			repo := &memoryViewRepo{views: map[uuid.UUID]*domain.View{own.ID: own}}
			e := echo.New()
			auth := func(next echo.HandlerFunc) echo.HandlerFunc {
				return func(c *echo.Context) error {
					c.Set(string(mw.UserIDKey), userID)
					c.Set("workspace", ws)
					c.Set("workspace_role", tc.role)
					return next(c)
				}
			}
			registerRoutes(e, &appHandlers{view: handler.NewViewHandler(service.NewViewService(repo))}, testMiddleware(auth))
			send := func(method, path, body string) int {
				req := httptest.NewRequest(method, "/api/workspaces/acme"+path, strings.NewReader(body))
				req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
				rec := httptest.NewRecorder()
				e.ServeHTTP(rec, req)
				return rec.Code
			}

			created := send(http.MethodPost, "/views", `{"name":"Everyone","filters":{},"is_shared":true}`)
			updated := send(http.MethodPatch, "/views/"+own.ID.String(), `{"is_shared":true}`)
			shared := repo.views[own.ID].IsShared
			deleted := send(http.MethodDelete, "/views/"+own.ID.String(), "")

			if tc.allowed {
				require.Equal(t, http.StatusCreated, created)
				require.Equal(t, http.StatusOK, updated)
				require.True(t, shared)
				require.Equal(t, http.StatusOK, deleted)
				require.Len(t, repo.views, 1, "the created view remains and the deleted one is gone")
				require.NotContains(t, repo.views, own.ID)
				return
			}
			require.Equal(t, http.StatusForbidden, created)
			require.Equal(t, http.StatusForbidden, updated)
			require.Equal(t, http.StatusForbidden, deleted)
			require.False(t, shared)
			require.Len(t, repo.views, 1, "a rejected guest must not create or delete views")
			require.Contains(t, repo.views, own.ID)
		})
	}
}

// Personal access tokens stay excluded from view writes even with the scope.
func TestViewWritesRejectTokens(t *testing.T) {
	for _, target := range []struct{ method, path string }{
		{http.MethodPost, "/views"}, {http.MethodPatch, "/views/example"}, {http.MethodDelete, "/views/example"},
	} {
		e := echo.New()
		auth := func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c *echo.Context) error {
				c.Set(mw.TokenScopesKey, []string{domain.PermViewManage, domain.PermViewsRead})
				c.Set("workspace_role", domain.RoleOwner)
				return next(c)
			}
		}
		registerRoutes(e, &appHandlers{}, testMiddleware(auth))
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(target.method, "/api/workspaces/acme"+target.path, nil))
		require.Equal(t, http.StatusForbidden, rec.Code, target.method+" "+target.path)
	}
}
