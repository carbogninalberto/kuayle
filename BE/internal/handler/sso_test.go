package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/labstack/echo/v5"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

type ssoHandlerRepo struct {
	config *domain.SSOConfig
}

func (r *ssoHandlerRepo) GetConfig(ctx context.Context, workspaceID uuid.UUID) (*domain.SSOConfig, error) {
	return r.config, nil
}
func (r *ssoHandlerRepo) SaveConfig(ctx context.Context, config *domain.SSOConfig) error {
	panic("not implemented")
}
func (r *ssoHandlerRepo) DeleteConfig(ctx context.Context, workspaceID uuid.UUID) error {
	panic("not implemented")
}
func (r *ssoHandlerRepo) CreateLoginState(ctx context.Context, state *domain.SSOLoginState) error {
	panic("not implemented")
}
func (r *ssoHandlerRepo) ConsumeLoginState(ctx context.Context, stateHash string) (*domain.SSOLoginState, error) {
	panic("not implemented")
}
func (r *ssoHandlerRepo) DeleteExpiredLoginStates(ctx context.Context) error {
	panic("not implemented")
}
func (r *ssoHandlerRepo) GetIdentity(ctx context.Context, workspaceID uuid.UUID, issuer, subject string) (*domain.UserIdentity, error) {
	panic("not implemented")
}
func (r *ssoHandlerRepo) TouchIdentity(ctx context.Context, id uuid.UUID) error {
	panic("not implemented")
}
func (r *ssoHandlerRepo) ProvisionIdentity(ctx context.Context, params repository.SSOProvisionParams) (*domain.User, error) {
	panic("not implemented")
}
func (r *ssoHandlerRepo) UserWorkspaceRoles(ctx context.Context, userID uuid.UUID) ([]domain.SSOWorkspaceRole, error) {
	panic("not implemented")
}

type ssoHandlerWorkspaces struct{ workspace *domain.Workspace }

func (w ssoHandlerWorkspaces) GetBySlug(ctx context.Context, slug string) (*domain.Workspace, error) {
	return w.workspace, nil
}

type ssoHandlerUsers struct{}

func (ssoHandlerUsers) Create(ctx context.Context, user *domain.User) error { return nil }
func (ssoHandlerUsers) GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	return nil, nil
}
func (ssoHandlerUsers) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	return nil, nil
}
func (ssoHandlerUsers) Update(ctx context.Context, user *domain.User) error { return nil }

type ssoHandlerRefresh struct{}

func (ssoHandlerRefresh) Create(ctx context.Context, token *repository.RefreshToken) error {
	return nil
}
func (ssoHandlerRefresh) GetByHash(ctx context.Context, hash string) (*repository.RefreshToken, error) {
	return nil, nil
}
func (ssoHandlerRefresh) DeleteByHash(ctx context.Context, hash string) error      { return nil }
func (ssoHandlerRefresh) DeleteByUser(ctx context.Context, userID uuid.UUID) error { return nil }
func (ssoHandlerRefresh) DeleteExpired(ctx context.Context) error                  { return nil }

type logCapture struct{ entries []string }

func (h *logCapture) Levels() []log.Level { return log.AllLevels }
func (h *logCapture) Fire(entry *log.Entry) error {
	data, _ := json.Marshal(entry.Data)
	h.entries = append(h.entries, entry.Message+" "+string(data))
	return nil
}

func newSSOHandlerForTest(t *testing.T) (*SSOHandler, *ssoHandlerRepo, *domain.Workspace) {
	t.Helper()
	workspace := &domain.Workspace{ID: uuid.New(), Slug: "acme", Name: "Acme", OwnerID: uuid.New()}
	key := crypto.DeriveKey("handler-test-secret")
	encrypted, err := crypto.Encrypt("super-secret-client-value", key)
	require.NoError(t, err)
	repo := &ssoHandlerRepo{config: &domain.SSOConfig{
		WorkspaceID: workspace.ID, IssuerURL: "https://idp.example", ClientID: "client",
		ClientSecretEncrypted: encrypted, DefaultRole: domain.RoleMember, Enabled: true,
		RequireSSO: false, UpdatedAt: time.Now(),
	}}
	auth := service.NewAuthService(ssoHandlerUsers{}, ssoHandlerRefresh{}, "test-jwt-secret")
	sso := service.NewSSOService(repo, ssoHandlerUsers{}, ssoHandlerWorkspaces{workspace}, auth, key, "https://app.test", false, service.InstanceSSOConfig{})
	return NewSSOHandler(sso, NewAuthHandler(auth, false, nil, nil), false), repo, workspace
}

func runSSOHandler(t *testing.T, h *SSOHandler, method, path string, body string) (int, string) {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPathValues(echo.PathValues{{Name: "slug", Value: "acme"}})
	var err error
	if method == http.MethodGet {
		err = h.Get(c)
	} else {
		err = h.Save(c)
	}
	require.NoError(t, err)
	return rec.Code, rec.Body.String()
}

func TestSSOConfigResponseNeverContainsTheSecret(t *testing.T) {
	h, _, _ := newSSOHandlerForTest(t)
	status, body := runSSOHandler(t, h, http.MethodGet, "/api/workspaces/acme/sso", "")
	require.Equal(t, http.StatusOK, status)
	require.NotContains(t, body, "super-secret-client-value")
	require.Contains(t, body, `"has_client_secret":true`)
	require.Contains(t, body, `/api/auth/sso/acme/callback`)

	var response dto.SSOConfigResponse
	require.NoError(t, json.Unmarshal([]byte(body), &response))
	require.True(t, response.HasClientSecret)
	require.Equal(t, "https://idp.example", response.IssuerURL)
}

func TestSSOConfigValidationDoesNotLogTheSecret(t *testing.T) {
	capture := &logCapture{}
	hook := log.StandardLogger().ReplaceHooks(log.LevelHooks{})
	log.AddHook(capture)
	defer log.StandardLogger().ReplaceHooks(hook)

	h, _, _ := newSSOHandlerForTest(t)
	body := `{"issuer_url":"http://insecure.example","client_id":"client","client_secret":"do-not-log-this","default_role":"member","enabled":true}`
	status, response := runSSOHandler(t, h, http.MethodPut, "/api/workspaces/acme/sso", body)
	require.Equal(t, http.StatusBadRequest, status)
	require.NotContains(t, response, "do-not-log-this")
	for _, entry := range capture.entries {
		require.NotContains(t, entry, "do-not-log-this")
	}
}
