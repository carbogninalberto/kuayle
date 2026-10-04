package handler

import (
	"context"
	"encoding/json"
	"errors"
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

type workspaceCreateRepoStub struct {
	repository.WorkspaceRepo
	existing  *domain.Workspace
	lookupErr error
	createErr error
}

func (r *workspaceCreateRepoStub) GetBySlug(context.Context, string) (*domain.Workspace, error) {
	return r.existing, r.lookupErr
}

func (r *workspaceCreateRepoStub) CreateWithMemberAndLabels(context.Context, *domain.Workspace, *domain.WorkspaceMember, []domain.Label) error {
	return r.createErr
}

func performCreateWorkspaceRequest(t *testing.T, repo repository.WorkspaceRepo) *httptest.ResponseRecorder {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces", strings.NewReader(`{"name":"Test","slug":"test"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", uuid.New())
	h := NewWorkspaceHandler(service.NewWorkspaceService(repo, nil))

	require.NoError(t, h.Create(c))
	return rec
}

func TestWorkspaceHandler_CreateReturnsTypedSlugConflict(t *testing.T) {
	rec := performCreateWorkspaceRequest(t, &workspaceCreateRepoStub{
		existing: &domain.Workspace{ID: uuid.New(), Slug: "test"},
	})

	require.Equal(t, http.StatusConflict, rec.Code)
	require.Contains(t, rec.Body.String(), `"code":"WORKSPACE_SLUG_TAKEN"`)
}

func TestWorkspaceHandler_CreateReturnsInternalErrorForRepositoryFailure(t *testing.T) {
	rec := performCreateWorkspaceRequest(t, &workspaceCreateRepoStub{
		lookupErr: errors.New("database unavailable"),
	})

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Contains(t, rec.Body.String(), `"code":"INTERNAL_ERROR"`)
}

type workspaceListRepoStub struct{ repository.WorkspaceRepo }

func (*workspaceListRepoStub) ListByUser(context.Context, uuid.UUID) ([]domain.Workspace, error) {
	return []domain.Workspace{{Slug: "allowed"}, {Slug: "private"}}, nil
}

func TestWorkspaceListHonorsTokenWorkspaceRestriction(t *testing.T) {
	for _, tc := range []struct {
		name  string
		pat   bool
		slugs []string
		want  []string
	}{
		{"session", false, nil, []string{"allowed", "private"}},
		{"unrestricted", true, nil, []string{"allowed", "private"}},
		{"restricted", true, []string{"allowed"}, []string{"allowed"}},
		{"no accessible matches", true, []string{"other"}, []string{}},
		{"stored empty restriction", true, []string{}, []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := echo.New()
			rec := httptest.NewRecorder()
			c := e.NewContext(httptest.NewRequest(http.MethodGet, "/api/workspaces", nil), rec)
			c.Set("user_id", uuid.New())
			if tc.pat {
				c.Set(middleware.TokenWorkspacesKey, tc.slugs)
			}
			h := NewWorkspaceHandler(service.NewWorkspaceService(&workspaceListRepoStub{}, nil))
			require.NoError(t, h.List(c))
			var response []struct {
				Slug string `json:"slug"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
			got := make([]string, 0, len(response))
			for _, ws := range response {
				got = append(got, ws.Slug)
			}
			require.Equal(t, tc.want, got)
		})
	}
}
