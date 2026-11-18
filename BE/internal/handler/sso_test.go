package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/labstack/echo/v5"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

type ssoHandlerRepo struct {
	config *domain.SSOConfig
	states map[string]*domain.SSOLoginState
}

func (r *ssoHandlerRepo) RequiresSSO(ctx context.Context, workspaceID uuid.UUID) (bool, error) {
	config := r.config
	return config != nil && config.Enabled && config.RequireSSO, nil
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
	if r.states == nil {
		r.states = map[string]*domain.SSOLoginState{}
	}
	copy := *state
	r.states[state.StateHash] = &copy
	return nil
}
func (r *ssoHandlerRepo) ConsumeLoginState(ctx context.Context, stateHash string) (*domain.SSOLoginState, error) {
	state := r.states[stateHash]
	if state == nil {
		return nil, nil
	}
	delete(r.states, stateHash)
	copy := *state
	return &copy, nil
}
func (r *ssoHandlerRepo) DeleteExpiredLoginStates(ctx context.Context) error {
	return nil
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
func (r *ssoHandlerRepo) LinkIdentity(ctx context.Context, userID, workspaceID uuid.UUID, provider, issuer, subject, email string) (*domain.UserIdentity, error) {
	panic("not implemented")
}
func (r *ssoHandlerRepo) HasIdentity(ctx context.Context, userID, workspaceID uuid.UUID) (bool, error) {
	panic("not implemented")
}

type ssoHandlerWorkspaces struct{ workspace *domain.Workspace }

func (w ssoHandlerWorkspaces) GetBySlug(ctx context.Context, slug string) (*domain.Workspace, error) {
	return w.workspace, nil
}

func (w ssoHandlerWorkspaces) GetMember(ctx context.Context, workspaceID, userID uuid.UUID) (*domain.WorkspaceMember, error) {
	return &domain.WorkspaceMember{WorkspaceID: workspaceID, UserID: userID, Role: domain.RoleMember}, nil
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
	repo := &ssoHandlerRepo{states: map[string]*domain.SSOLoginState{}, config: &domain.SSOConfig{
		WorkspaceID: workspace.ID, IssuerURL: "https://idp.example", ClientID: "client",
		ClientSecretEncrypted: encrypted, DefaultRole: domain.RoleMember, Enabled: true,
		RequireSSO: false, UpdatedAt: time.Now(),
	}}
	auth := service.NewAuthService(ssoHandlerUsers{}, ssoHandlerRefresh{}, "test-jwt-secret")
	sso := service.NewSSOService(repo, ssoHandlerUsers{}, ssoHandlerWorkspaces{workspace}, auth, key, "https://app.test", true, service.InstanceSSOConfig{})
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

func newSSOHandlerWithIdP(t *testing.T) (*SSOHandler, *ssoHandlerRepo, *domain.Workspace, *httptest.Server) {
	t.Helper()
	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		writeJSONHandler(w, map[string]any{
			"issuer":                                "http://" + r.Host,
			"authorization_endpoint":                "http://" + r.Host + "/authorize",
			"token_endpoint":                        "http://" + r.Host + "/token",
			"jwks_uri":                              "http://" + r.Host + "/jwks",
			"response_types_supported":              []string{"code"},
			"subject_types_supported":               []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	}))
	t.Cleanup(idp.Close)
	workspace := &domain.Workspace{ID: uuid.New(), Slug: "acme", Name: "Acme", OwnerID: uuid.New()}
	key := crypto.DeriveKey("handler-test-secret")
	encrypted, err := crypto.Encrypt("client-secret", key)
	require.NoError(t, err)
	repo := &ssoHandlerRepo{states: map[string]*domain.SSOLoginState{}, config: &domain.SSOConfig{
		WorkspaceID: workspace.ID, IssuerURL: idp.URL, ClientID: "client",
		ClientSecretEncrypted: encrypted, DefaultRole: domain.RoleMember, Enabled: true, UpdatedAt: time.Now(),
	}}
	auth := service.NewAuthService(ssoHandlerUsers{}, ssoHandlerRefresh{}, "test-jwt-secret")
	sso := service.NewSSOService(repo, ssoHandlerUsers{}, ssoHandlerWorkspaces{workspace}, auth, key, "https://app.test", true, service.InstanceSSOConfig{})
	return NewSSOHandler(sso, NewAuthHandler(auth, false, nil, nil), false), repo, workspace, idp
}

func writeJSONHandler(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func runSSOStart(t *testing.T, h *SSOHandler, slug, userID string) (*httptest.ResponseRecorder, *echo.Context) {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/sso/"+slug+"/start", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPathValues(echo.PathValues{{Name: "slug", Value: slug}})
	if userID != "" {
		parsed, err := uuid.Parse(userID)
		require.NoError(t, err)
		c.Set(string(middleware.UserIDKey), parsed)
	}
	return rec, c
}

func TestSSOStartUsesOneBindCookiePerFlow(t *testing.T) {
	h, _, _, _ := newSSOHandlerWithIdP(t)
	firstRec, first := runSSOStart(t, h, "acme", "")
	require.NoError(t, h.Start(first))
	secondRec, second := runSSOStart(t, h, "acme", "")
	require.NoError(t, h.Start(second))

	require.Equal(t, http.StatusFound, firstRec.Code)
	require.Equal(t, http.StatusFound, secondRec.Code)
	firstCookies := firstRec.Result().Cookies()
	secondCookies := secondRec.Result().Cookies()
	require.Len(t, firstCookies, 1)
	require.Len(t, secondCookies, 1)
	require.NotEqual(t, firstCookies[0].Name, secondCookies[0].Name)
	require.Equal(t, "/api/auth/sso/acme/callback", firstCookies[0].Path)
	require.Equal(t, "/api/auth/sso/acme/callback", secondCookies[0].Path)

	// Each flow completes with the cookie its own Start set; neither binding
	// overwrote the other. The fake IdP then fails the token exchange, which
	// proves state and browser binding were accepted.
	callback := func(start *httptest.ResponseRecorder, bind *http.Cookie) string {
		authURL, err := url.Parse(start.Header().Get("Location"))
		require.NoError(t, err)
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet,
			"/api/auth/sso/acme/callback?state="+url.QueryEscape(authURL.Query().Get("state"))+"&code=code", nil)
		req.AddCookie(bind)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPathValues(echo.PathValues{{Name: "slug", Value: "acme"}})
		require.NoError(t, h.Callback(c))
		return rec.Header().Get("Location")
	}
	require.Contains(t, callback(firstRec, firstCookies[0]), "sso_error=token_invalid")
	require.Contains(t, callback(secondRec, secondCookies[0]), "sso_error=token_invalid")
}

func TestSSOCallbackReadsTheCookieOfItsOwnState(t *testing.T) {
	h, _, _, _ := newSSOHandlerWithIdP(t)
	startRec, start := runSSOStart(t, h, "acme", "")
	require.NoError(t, h.Start(start))
	cookie := startRec.Result().Cookies()[0]
	authURL, err := url.Parse(startRec.Header().Get("Location"))
	require.NoError(t, err)
	state := authURL.Query().Get("state")
	require.NotEmpty(t, state)

	callback := func(bind *http.Cookie) *httptest.ResponseRecorder {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/api/auth/sso/acme/callback?state="+url.QueryEscape(state)+"&code=code", nil)
		if bind != nil {
			req.AddCookie(bind)
		}
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPathValues(echo.PathValues{{Name: "slug", Value: "acme"}})
		require.NoError(t, h.Callback(c))
		return rec
	}

	// The cookie of another (or no) state never satisfies this state.
	missing := callback(nil)
	require.Contains(t, missing.Header().Get("Location"), "sso_error=invalid_state")
	require.Contains(t, missing.Header().Get("Location"), "workspace=acme")
	wrong := callback(&http.Cookie{Name: cookie.Name + "-wrong", Value: cookie.Value})
	require.Contains(t, wrong.Header().Get("Location"), "sso_error=invalid_state")

	// The right cookie gets past state and browser binding; the fake IdP then
	// fails the token exchange, proving the binding was accepted.
	right := callback(cookie)
	require.Contains(t, right.Header().Get("Location"), "sso_error=token_invalid")
}

func TestSSOStartLinkReturnsAuthorizationURL(t *testing.T) {
	h, repo, workspace, _ := newSSOHandlerWithIdP(t)
	userID := uuid.New()
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/sso/acme/link", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPathValues(echo.PathValues{{Name: "slug", Value: "acme"}})
	c.Set(string(middleware.UserIDKey), userID)

	require.NoError(t, h.StartLink(c))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "auth_url")
	require.Len(t, rec.Result().Cookies(), 1)
	require.Equal(t, "/api/auth/sso/acme/callback", rec.Result().Cookies()[0].Path)
	for _, state := range repo.states {
		require.NotNil(t, state.LinkUserID)
		require.Equal(t, userID, *state.LinkUserID)
		require.Equal(t, workspace.ID, state.WorkspaceID)
	}
}

func TestSSOHandlerErrorMapping(t *testing.T) {
	for err, want := range map[error]string{
		service.ErrSSOLinkRequired:     "link_required",
		service.ErrSSONotMember:        "not_member",
		service.ErrSSOIdentityTaken:    "identity_taken",
		service.ErrSSOEmailUnverified:  "email_unverified",
		service.ErrSSODomainNotAllowed: "domain_not_allowed",
		service.ErrSSOTokenInvalid:     "token_invalid",
		service.ErrSSOStateInvalid:     "invalid_state",
		service.ErrSSONotConfigured:    "not_configured",
	} {
		require.Equal(t, want, ssoErrorCode(err))
	}
}

func TestSAMLRejectsSensitiveInputWithoutDisclosure(t *testing.T) {
	capture := &logCapture{}
	hooks := log.StandardLogger().ReplaceHooks(log.LevelHooks{})
	log.AddHook(capture)
	defer log.StandardLogger().ReplaceHooks(hooks)
	h, repo, _ := newSSOHandlerForTest(t)
	secret := "private-key-canary"
	req := dto.SSOConfigRequest{Protocol: "saml", MetadataXML: "-----BEGIN PRIVATE KEY-----" + secret, DefaultRole: "member"}
	raw, err := json.Marshal(req)
	require.NoError(t, err)
	status, body := runSSOHandler(t, h, http.MethodPut, "/api/workspaces/acme/sso", string(raw))
	require.Equal(t, http.StatusBadRequest, status)
	require.NotContains(t, body, secret)
	repo.config.SAMLMetadata = secret
	_, body = runSSOHandler(t, h, http.MethodGet, "/api/workspaces/acme/sso", "")
	require.NotContains(t, body, secret)
	e := echo.New()
	e.POST("/api/auth/sso/:slug/callback", h.Callback)
	r := httptest.NewRequest(http.MethodPost, "/api/auth/sso/acme/callback", strings.NewReader(url.Values{"RelayState": {"spent"}, "SAMLResponse": {secret}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, r)
	require.Equal(t, http.StatusFound, rec.Code)
	require.NotContains(t, rec.Header().Get("Location")+rec.Body.String(), secret)
	require.NotEmpty(t, capture.entries)
	for _, entry := range capture.entries {
		require.NotContains(t, entry, secret)
	}
}

func TestSAMLPostUsesCrossSiteBinding(t *testing.T) {
	h, repo, _ := newSSOHandlerForTest(t)
	repo.config.SAMLMetadata = `<EntityDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata" entityID="https://idp.example">
 <IDPSSODescriptor protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">
 <SingleSignOnService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect" Location="https://idp.example/signin"/>
 </IDPSSODescriptor></EntityDescriptor>`
	started, ctx := runSSOStart(t, h, "acme", "")
	require.NoError(t, h.Start(ctx))
	redirect, err := url.Parse(started.Header().Get("Location"))
	require.NoError(t, err)
	relay := redirect.Query().Get("RelayState")
	require.NotEmpty(t, relay)
	cookies := started.Result().Cookies()
	require.Len(t, cookies, 1)
	cookie := cookies[0]
	require.Equal(t, http.SameSiteNoneMode, cookie.SameSite)
	require.True(t, cookie.Secure && cookie.HttpOnly)
	require.Equal(t, "/api/auth/sso/acme/callback", cookie.Path)
	e := echo.New()
	e.POST("/api/auth/sso/:slug/callback", h.Callback)
	form := url.Values{"RelayState": {relay}, "SAMLResponse": {"invalid"}}
	r := httptest.NewRequest(http.MethodPost, cookie.Path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(cookie)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, r)
	require.Contains(t, rec.Header().Get("Location"), "sso_error=token_invalid")
	require.Empty(t, repo.states)
}
