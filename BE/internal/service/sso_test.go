package service

import (
	"context"
	stdcrypto "crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	kcrypto "github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/stretchr/testify/require"
)

// --- in-memory SSO storage ---

type ssoFakeRepo struct {
	configs     map[uuid.UUID]*domain.SSOConfig
	states      map[string]*domain.SSOLoginState
	identities  map[string]*domain.UserIdentity
	users       map[uuid.UUID]*domain.User
	memberships map[string]string
	roles       []domain.SSOWorkspaceRole
	provisioned []repository.SSOProvisionParams
}

func newSSOFakeRepo() *ssoFakeRepo {
	return &ssoFakeRepo{
		configs:     map[uuid.UUID]*domain.SSOConfig{},
		states:      map[string]*domain.SSOLoginState{},
		identities:  map[string]*domain.UserIdentity{},
		users:       map[uuid.UUID]*domain.User{},
		memberships: map[string]string{},
	}
}

func (r *ssoFakeRepo) GetConfig(ctx context.Context, workspaceID uuid.UUID) (*domain.SSOConfig, error) {
	config := r.configs[workspaceID]
	if config == nil {
		return nil, nil
	}
	copy := *config
	return &copy, nil
}

func (r *ssoFakeRepo) SaveConfig(ctx context.Context, config *domain.SSOConfig) error {
	copy := *config
	now := time.Now()
	if existing := r.configs[config.WorkspaceID]; existing != nil {
		copy.CreatedAt = existing.CreatedAt
	} else {
		copy.CreatedAt = now
	}
	copy.UpdatedAt = now
	r.configs[config.WorkspaceID] = &copy
	*config = copy
	return nil
}

func (r *ssoFakeRepo) DeleteConfig(ctx context.Context, workspaceID uuid.UUID) error {
	delete(r.configs, workspaceID)
	return nil
}

func (r *ssoFakeRepo) CreateLoginState(ctx context.Context, state *domain.SSOLoginState) error {
	copy := *state
	copy.CreatedAt = time.Now()
	r.states[state.StateHash] = &copy
	return nil
}

func (r *ssoFakeRepo) ConsumeLoginState(ctx context.Context, stateHash string) (*domain.SSOLoginState, error) {
	state := r.states[stateHash]
	if state == nil {
		return nil, nil
	}
	delete(r.states, stateHash)
	copy := *state
	return &copy, nil
}

func (r *ssoFakeRepo) DeleteExpiredLoginStates(ctx context.Context) error {
	for key, state := range r.states {
		if time.Now().After(state.ExpiresAt) {
			delete(r.states, key)
		}
	}
	return nil
}

func identityKey(workspaceID uuid.UUID, issuer, subject string) string {
	return workspaceID.String() + "|" + issuer + "|" + subject
}

func (r *ssoFakeRepo) GetIdentity(ctx context.Context, workspaceID uuid.UUID, issuer, subject string) (*domain.UserIdentity, error) {
	identity := r.identities[identityKey(workspaceID, issuer, subject)]
	if identity == nil {
		return nil, nil
	}
	copy := *identity
	return &copy, nil
}

func (r *ssoFakeRepo) TouchIdentity(ctx context.Context, id uuid.UUID) error {
	for _, identity := range r.identities {
		if identity.ID == id {
			now := time.Now()
			identity.LastLoginAt = &now
		}
	}
	return nil
}

func (r *ssoFakeRepo) ProvisionIdentity(ctx context.Context, params repository.SSOProvisionParams) (*domain.User, error) {
	r.provisioned = append(r.provisioned, params)
	key := identityKey(params.WorkspaceID, params.Issuer, params.Subject)
	identity := r.identities[key]
	var user *domain.User
	if identity != nil {
		user = r.users[identity.UserID]
	}
	if user == nil {
		for _, candidate := range r.users {
			if strings.EqualFold(candidate.Email, params.Email) {
				user = candidate
				break
			}
		}
	}
	if user == nil {
		user = &domain.User{ID: uuid.New(), Email: params.Email, Name: params.Name, DisplayName: params.Name, PasswordHash: params.PasswordHash}
		r.users[user.ID] = user
	}
	if identity == nil {
		r.identities[key] = &domain.UserIdentity{
			ID: uuid.New(), UserID: user.ID, WorkspaceID: params.WorkspaceID,
			Provider: params.Provider, Issuer: params.Issuer, Subject: params.Subject, Email: params.Email,
		}
	}
	memberKey := params.WorkspaceID.String() + "|" + user.ID.String()
	if _, ok := r.memberships[memberKey]; !ok {
		r.memberships[memberKey] = params.DefaultRole
	}
	return user, nil
}

func (r *ssoFakeRepo) UserWorkspaceRoles(ctx context.Context, userID uuid.UUID) ([]domain.SSOWorkspaceRole, error) {
	roles := make([]domain.SSOWorkspaceRole, 0)
	for _, role := range r.roles {
		roles = append(roles, role)
	}
	return roles, nil
}

// --- user and refresh storage ---

type ssoFakeUsers struct{ users map[uuid.UUID]*domain.User }

func (r *ssoFakeUsers) Create(ctx context.Context, user *domain.User) error {
	r.users[user.ID] = user
	return nil
}
func (r *ssoFakeUsers) GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	return r.users[id], nil
}
func (r *ssoFakeUsers) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	for _, user := range r.users {
		if strings.EqualFold(user.Email, email) {
			return user, nil
		}
	}
	return nil, nil
}
func (r *ssoFakeUsers) Update(ctx context.Context, user *domain.User) error { return nil }

type ssoFakeRefresh struct{ created []*repository.RefreshToken }

func (r *ssoFakeRefresh) Create(ctx context.Context, token *repository.RefreshToken) error {
	r.created = append(r.created, token)
	return nil
}
func (r *ssoFakeRefresh) GetByHash(ctx context.Context, hash string) (*repository.RefreshToken, error) {
	return nil, nil
}
func (r *ssoFakeRefresh) DeleteByHash(ctx context.Context, hash string) error { return nil }
func (r *ssoFakeRefresh) DeleteByUser(ctx context.Context, userID uuid.UUID) error {
	return nil
}
func (r *ssoFakeRefresh) DeleteExpired(ctx context.Context) error { return nil }

type ssoFakeWorkspaces map[string]*domain.Workspace

func (w ssoFakeWorkspaces) GetBySlug(ctx context.Context, slug string) (*domain.Workspace, error) {
	return w[slug], nil
}

// --- fake identity provider ---

type fakeIDPCode struct {
	challenge string
	nonce     string
	claims    map[string]any
	signer    *rsa.PrivateKey
	algNone   bool
}

type fakeIDP struct {
	server   *httptest.Server
	key      *rsa.PrivateKey
	otherKey *rsa.PrivateKey
	mu       sync.Mutex
	codes    map[string]*fakeIDPCode
}

func newFakeIDP(t *testing.T) *fakeIDP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	idp := &fakeIDP{key: key, otherKey: otherKey, codes: map[string]*fakeIDPCode{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"issuer":                                idp.server.URL,
			"authorization_endpoint":                idp.server.URL + "/authorize",
			"token_endpoint":                        idp.server.URL + "/token",
			"jwks_uri":                              idp.server.URL + "/jwks",
			"response_types_supported":              []string{"code"},
			"subject_types_supported":               []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
			"code_challenge_methods_supported":      []string{"S256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"keys": []any{rsaJWK(&idp.key.PublicKey)}})
	})
	mux.HandleFunc("/token", idp.token)
	idp.server = httptest.NewServer(mux)
	t.Cleanup(idp.server.Close)
	return idp
}

func (f *fakeIDP) authorize(t *testing.T, authURL string, mutate func(*fakeIDPCode)) string {
	t.Helper()
	parsed, err := url.Parse(authURL)
	require.NoError(t, err)
	require.Equal(t, f.server.URL+"/authorize", parsed.Scheme+"://"+parsed.Host+parsed.Path)
	query := parsed.Query()
	require.Equal(t, "S256", query.Get("code_challenge_method"))
	code := &fakeIDPCode{
		challenge: query.Get("code_challenge"),
		nonce:     query.Get("nonce"),
		claims: map[string]any{
			"sub":            "user-1",
			"email":          "person@example.com",
			"email_verified": true,
			"name":           "SSO Person",
		},
		signer: f.key,
	}
	if mutate != nil {
		mutate(code)
	}
	value := uuid.NewString()
	f.mu.Lock()
	f.codes[value] = code
	f.mu.Unlock()
	return value
}

func (f *fakeIDP) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	code, ok := f.codes[r.Form.Get("code")]
	f.mu.Unlock()
	verifier := r.Form.Get("code_verifier")
	sum := sha256.Sum256([]byte(verifier))
	if !ok || r.Form.Get("grant_type") != "authorization_code" ||
		base64.RawURLEncoding.EncodeToString(sum[:]) != code.challenge {
		http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
		return
	}
	claims := map[string]any{
		"iss":   f.server.URL,
		"aud":   "test-client",
		"exp":   time.Now().Add(5 * time.Minute).Unix(),
		"iat":   time.Now().Unix(),
		"nonce": code.nonce,
	}
	for key, value := range code.claims {
		claims[key] = value
	}
	token, err := signIDToken(claims, code.signer, code.algNone)
	if err != nil {
		http.Error(w, "signing failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"access_token": "access", "token_type": "Bearer", "id_token": token})
}

func rsaJWK(key *rsa.PublicKey) map[string]any {
	return map[string]any{
		"kty": "RSA",
		"kid": "test",
		"use": "sig",
		"alg": "RS256",
		"n":   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
	}
}

func signIDToken(claims map[string]any, key *rsa.PrivateKey, algNone bool) (string, error) {
	if algNone {
		header, _ := json.Marshal(map[string]any{"alg": "none", "typ": "JWT"})
		payload, _ := json.Marshal(claims)
		return base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload) + ".", nil
	}
	header, _ := json.Marshal(map[string]any{"alg": "RS256", "typ": "JWT", "kid": "test"})
	payload, _ := json.Marshal(claims)
	signingInput := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, stdcrypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

// --- test rig ---

type ssoTestRig struct {
	service   *SSOService
	repo      *ssoFakeRepo
	users     *ssoFakeUsers
	refresh   *ssoFakeRefresh
	idp       *fakeIDP
	workspace *domain.Workspace
	key       []byte
}

func newSSOTestRig(t *testing.T) *ssoTestRig {
	t.Helper()
	repo := newSSOFakeRepo()
	users := &ssoFakeUsers{users: map[uuid.UUID]*domain.User{}}
	refresh := &ssoFakeRefresh{}
	workspace := &domain.Workspace{ID: uuid.New(), Slug: "acme", Name: "Acme", OwnerID: uuid.New()}
	workspaces := ssoFakeWorkspaces{"acme": workspace}
	idp := newFakeIDP(t)
	key := kcrypto.DeriveKey("sso-test-secret")
	secret, err := kcrypto.Encrypt("client-secret", key)
	require.NoError(t, err)
	repo.configs[workspace.ID] = &domain.SSOConfig{
		WorkspaceID: workspace.ID, IssuerURL: idp.server.URL, ClientID: "test-client",
		ClientSecretEncrypted: secret, AllowedDomains: nil, ProviderName: "Test IdP",
		DefaultRole: domain.RoleMember, Enabled: true, UpdatedAt: time.Now(),
	}
	auth := NewAuthService(users, refresh, "test-jwt-secret")
	service := NewSSOService(repo, users, workspaces, auth, key, "https://app.test", true, InstanceSSOConfig{})
	return &ssoTestRig{service: service, repo: repo, users: users, refresh: refresh, idp: idp, workspace: workspace, key: key}
}

// callback runs Start, registers an authorization code with the fake IdP and
// returns the callback result.
func (r *ssoTestRig) callback(t *testing.T, mutate func(*fakeIDPCode)) (*domain.User, string, string, string, error) {
	t.Helper()
	authURL, bind, err := r.service.Start(context.Background(), "acme", "/acme/inbox")
	require.NoError(t, err)
	code := r.idp.authorize(t, authURL, mutate)
	state := stateFromAuthURL(t, authURL)
	user, access, refresh, redirect, err := r.service.CallbackRedirect(context.Background(), "acme", state, code, bind)
	return user, access, refresh, redirect, err
}

func stateFromAuthURL(t *testing.T, authURL string) string {
	t.Helper()
	parsed, err := url.Parse(authURL)
	require.NoError(t, err)
	state := parsed.Query().Get("state")
	require.NotEmpty(t, state)
	return state
}

// --- flows ---

func TestSSOAuthorizationCodeFlowProvisionsAndSignsIn(t *testing.T) {
	rig := newSSOTestRig(t)
	require.NoError(t, rig.repo.SaveConfig(context.Background(), &domain.SSOConfig{
		WorkspaceID: rig.workspace.ID, IssuerURL: rig.idp.server.URL, ClientID: "test-client",
		AllowedDomains: []string{"example.com"}, ProviderName: "Test IdP",
		DefaultRole: domain.RoleMember, Enabled: true, UpdatedAt: time.Now(),
	}))

	user, access, refresh, redirect, err := rig.callback(t, nil)
	require.NoError(t, err)
	require.NotNil(t, user)
	require.Equal(t, "person@example.com", user.Email)
	require.NotEmpty(t, access)
	require.NotEmpty(t, refresh)
	require.Equal(t, "/acme/inbox", redirect)
	require.Len(t, rig.repo.provisioned, 1)
	require.Equal(t, domain.RoleMember, rig.repo.provisioned[0].DefaultRole)
	require.True(t, HasUsablePassword(user))
	require.NotEmpty(t, rig.refresh.created)
	require.NotNil(t, rig.refresh.created[0].SSOWorkspaceID)
	require.Equal(t, rig.workspace.ID, *rig.refresh.created[0].SSOWorkspaceID)
}

func TestSSOGuestDefaultRole(t *testing.T) {
	rig := newSSOTestRig(t)
	rig.repo.configs[rig.workspace.ID].DefaultRole = domain.RoleGuest
	rig.callback(t, nil)
	require.Len(t, rig.repo.provisioned, 1)
	require.Equal(t, domain.RoleGuest, rig.repo.provisioned[0].DefaultRole)
}

func TestSSOPKCEMismatchIsRejected(t *testing.T) {
	rig := newSSOTestRig(t)
	authURL, bind, err := rig.service.Start(context.Background(), "acme", "")
	require.NoError(t, err)
	code := rig.idp.authorize(t, authURL, func(c *fakeIDPCode) {
		c.challenge = sha256Challenge("another-verifier")
	})
	_, _, _, _, err = rig.service.CallbackRedirect(context.Background(), "acme", stateFromAuthURL(t, authURL), code, bind)
	require.ErrorIs(t, err, ErrSSOTokenInvalid)
}

func TestSSOStateAndBrowserBinding(t *testing.T) {
	rig := newSSOTestRig(t)
	ctx := context.Background()

	t.Run("unknown state", func(t *testing.T) {
		_, _, _, _, err := rig.service.CallbackRedirect(ctx, "acme", "unknown", "code", "bind")
		require.ErrorIs(t, err, ErrSSOStateInvalid)
	})

	t.Run("missing binding cookie", func(t *testing.T) {
		authURL, _, err := rig.service.Start(ctx, "acme", "")
		require.NoError(t, err)
		code := rig.idp.authorize(t, authURL, nil)
		_, _, _, _, err = rig.service.CallbackRedirect(ctx, "acme", stateFromAuthURL(t, authURL), code, "")
		require.ErrorIs(t, err, ErrSSOStateInvalid)
	})

	t.Run("wrong binding value", func(t *testing.T) {
		authURL, _, err := rig.service.Start(ctx, "acme", "")
		require.NoError(t, err)
		code := rig.idp.authorize(t, authURL, nil)
		_, _, _, _, err = rig.service.CallbackRedirect(ctx, "acme", stateFromAuthURL(t, authURL), code, "wrong")
		require.ErrorIs(t, err, ErrSSOStateInvalid)
	})

	t.Run("expired state", func(t *testing.T) {
		authURL, bind, err := rig.service.Start(ctx, "acme", "")
		require.NoError(t, err)
		code := rig.idp.authorize(t, authURL, nil)
		state := stateFromAuthURL(t, authURL)
		for _, stored := range rig.repo.states {
			stored.ExpiresAt = time.Now().Add(-time.Second)
		}
		_, _, _, _, err = rig.service.CallbackRedirect(ctx, "acme", state, code, bind)
		require.ErrorIs(t, err, ErrSSOStateInvalid)
	})

	t.Run("reused state", func(t *testing.T) {
		authURL, bind, err := rig.service.Start(ctx, "acme", "")
		require.NoError(t, err)
		code := rig.idp.authorize(t, authURL, nil)
		state := stateFromAuthURL(t, authURL)
		_, _, _, _, err = rig.service.CallbackRedirect(ctx, "acme", state, code, bind)
		require.NoError(t, err)
		_, _, _, _, err = rig.service.CallbackRedirect(ctx, "acme", state, code, bind)
		require.ErrorIs(t, err, ErrSSOStateInvalid)
	})
}

func TestSSOIDTokenValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*fakeIDPCode)
	}{
		{"wrong audience", func(c *fakeIDPCode) { c.claims["aud"] = "another-client" }},
		{"wrong issuer", func(c *fakeIDPCode) { c.claims["iss"] = "https://evil.example" }},
		{"expired", func(c *fakeIDPCode) { c.claims["exp"] = time.Now().Add(-time.Minute).Unix() }},
		{"wrong signature key", nil},
		{"unsigned", func(c *fakeIDPCode) { c.algNone = true }},
		{"nonce mismatch", func(c *fakeIDPCode) { c.nonce = "not-the-nonce" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rig := newSSOTestRig(t)
			if tc.name == "wrong signature key" {
				tc.mutate = func(c *fakeIDPCode) { c.signer = rig.idp.otherKey }
			}
			_, _, _, _, err := rig.callback(t, tc.mutate)
			require.ErrorIs(t, err, ErrSSOTokenInvalid)
			require.Empty(t, rig.repo.provisioned)
		})
	}
}

func TestSSOUnverifiedEmailNeverLinksOrCreates(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*fakeIDPCode)
	}{
		{"false", func(c *fakeIDPCode) { c.claims["email_verified"] = false }},
		{"missing", func(c *fakeIDPCode) { delete(c.claims, "email_verified") }},
		{"string false", func(c *fakeIDPCode) { c.claims["email_verified"] = "false" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rig := newSSOTestRig(t)
			_, _, _, _, err := rig.callback(t, tc.mutate)
			require.ErrorIs(t, err, ErrSSOEmailUnverified)
			require.Empty(t, rig.repo.provisioned)
			require.Empty(t, rig.repo.identities)
		})
	}
}

func TestSSODomainNotAllowed(t *testing.T) {
	rig := newSSOTestRig(t)
	rig.repo.configs[rig.workspace.ID].AllowedDomains = []string{"company.example"}
	_, _, _, _, err := rig.callback(t, func(c *fakeIDPCode) { c.claims["email"] = "person@other.example" })
	require.ErrorIs(t, err, ErrSSODomainNotAllowed)
	require.Empty(t, rig.repo.provisioned)
}

func TestSSOExistingPasswordUserIsLinkedOnlyWithVerifiedEmail(t *testing.T) {
	rig := newSSOTestRig(t)
	existing := &domain.User{ID: uuid.New(), Email: "person@example.com", Name: "Existing", PasswordHash: "hash"}
	rig.users.users[existing.ID] = existing
	rig.repo.users[existing.ID] = existing

	user, _, _, _, err := rig.callback(t, nil)
	require.NoError(t, err)
	require.Equal(t, existing.ID, user.ID)
	require.Len(t, rig.repo.provisioned, 1)
	require.Equal(t, existing.Email, rig.repo.provisioned[0].Email)
	require.Equal(t, domain.RoleMember, rig.repo.memberships[rig.workspace.ID.String()+"|"+existing.ID.String()])

	second := newSSOTestRig(t)
	secondExisting := &domain.User{ID: uuid.New(), Email: "person@example.com", Name: "Existing", PasswordHash: "hash"}
	second.users.users[secondExisting.ID] = secondExisting
	second.repo.users[secondExisting.ID] = secondExisting
	_, _, _, _, err = second.callback(t, func(c *fakeIDPCode) { c.claims["email_verified"] = false })
	require.ErrorIs(t, err, ErrSSOEmailUnverified)
	require.Empty(t, second.repo.identities)
	require.Empty(t, second.repo.memberships)
}

func TestSSOIdentityInAnotherWorkspaceDoesNotAuthenticate(t *testing.T) {
	rig := newSSOTestRig(t)
	other := &domain.Workspace{ID: uuid.New(), Slug: "beta", Name: "Beta", OwnerID: uuid.New()}
	rig.service.workspaces.(ssoFakeWorkspaces)["beta"] = other
	secret, err := kcrypto.Encrypt("client-secret", rig.key)
	require.NoError(t, err)
	rig.repo.configs[other.ID] = &domain.SSOConfig{
		WorkspaceID: other.ID, IssuerURL: rig.idp.server.URL, ClientID: "test-client",
		ClientSecretEncrypted: secret, DefaultRole: domain.RoleMember, Enabled: true, UpdatedAt: time.Now(),
	}

	// Bind an identity to workspace A.
	user, _, _, _, err := rig.callback(t, nil)
	require.NoError(t, err)
	require.Len(t, rig.repo.identities, 1)

	// The same issuer/subject in workspace B must not find A's identity. An
	// unverified email in B is refused instead of falling back to A's user.
	authURL, bind, err := rig.service.Start(context.Background(), "beta", "")
	require.NoError(t, err)
	code := rig.idp.authorize(t, authURL, func(c *fakeIDPCode) { c.claims["email_verified"] = false })
	_, _, _, _, err = rig.service.CallbackRedirect(context.Background(), "beta", stateFromAuthURL(t, authURL), code, bind)
	require.ErrorIs(t, err, ErrSSOEmailUnverified)
	require.Len(t, rig.repo.identities, 1, "no identity may be created in workspace B")

	// A state started for A replayed on B's callback is rejected outright.
	authURL, bind, err = rig.service.Start(context.Background(), "acme", "")
	require.NoError(t, err)
	code = rig.idp.authorize(t, authURL, nil)
	_, _, _, _, err = rig.service.CallbackRedirect(context.Background(), "beta", stateFromAuthURL(t, authURL), code, bind)
	require.ErrorIs(t, err, ErrSSOStateInvalid)
	require.NotNil(t, user)
}

func TestSSOInstanceConfigWinsAndIsReadOnly(t *testing.T) {
	rig := newSSOTestRig(t)
	rig.service.instance = InstanceSSOConfig{
		WorkspaceSlug: "acme", IssuerURL: rig.idp.server.URL, ClientID: "instance-client",
		ClientSecret: "instance-secret", AllowedDomains: []string{"example.com"},
		DefaultRole: domain.RoleGuest, RequireSSO: true,
	}
	config, err := rig.service.GetConfig(context.Background(), "acme")
	require.NoError(t, err)
	require.True(t, config.ManagedByInstance)
	require.Equal(t, "instance-client", config.ClientID)
	require.True(t, config.RequireSSO)
	require.True(t, config.HasClientSecret)
	require.Equal(t, domain.RoleGuest, config.DefaultRole)

	_, err = rig.service.SaveConfig(context.Background(), "acme", dto.SSOConfigRequest{})
	require.ErrorIs(t, err, ErrSSOInstanceManaged)
	require.ErrorIs(t, rig.service.DeleteConfig(context.Background(), "acme"), ErrSSOInstanceManaged)
}

func TestSSOSaveConfigKeepsStoredSecretAndRejectsInvalidIssuer(t *testing.T) {
	rig := newSSOTestRig(t)
	ctx := context.Background()
	_, err := rig.service.SaveConfig(ctx, "acme", dto.SSOConfigRequest{
		IssuerURL: rig.idp.server.URL, ClientID: "test-client", DefaultRole: domain.RoleMember,
		Enabled: true, AllowedDomains: []string{"Example.COM", "@example.com"},
	})
	require.NoError(t, err)
	stored := rig.repo.configs[rig.workspace.ID]
	require.Equal(t, []string{"example.com"}, []string(stored.AllowedDomains))
	previous := stored.ClientSecretEncrypted
	require.NotEmpty(t, previous)

	_, err = rig.service.SaveConfig(ctx, "acme", dto.SSOConfigRequest{
		IssuerURL: rig.idp.server.URL, ClientID: "test-client", DefaultRole: domain.RoleMember, Enabled: true,
	})
	require.NoError(t, err)
	require.Equal(t, previous, rig.repo.configs[rig.workspace.ID].ClientSecretEncrypted)

	_, err = rig.service.SaveConfig(ctx, "acme", dto.SSOConfigRequest{
		IssuerURL: "http://insecure.example", ClientID: "test-client", DefaultRole: domain.RoleMember, Enabled: true,
	})
	require.ErrorIs(t, err, ErrSSOInvalidInput)
	require.NotContains(t, err.Error(), "client-secret")
}

func TestSanitizeRedirectAfter(t *testing.T) {
	for input, want := range map[string]string{
		"/acme/inbox":              "/acme/inbox",
		"https://evil.example":     "",
		"//evil.example":           "",
		"/\\evil.example":          "",
		"javascript:alert(1)":      "",
		"/acme\r\nSet-Cookie: x=y": "",
		"":                         "",
		"/acme?tab=all":            "/acme?tab=all",
	} {
		require.Equal(t, want, SanitizeRedirectAfter(input), input)
	}
}

func sha256Challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func TestUserSSORequiredExemptsOwnersAndNonEnforcedWorkspaces(t *testing.T) {
	rig := newSSOTestRig(t)
	ctx := context.Background()
	userID := uuid.New()
	second := &domain.Workspace{ID: uuid.New(), Slug: "beta", Name: "Beta", OwnerID: uuid.New()}
	rig.repo.configs[second.ID] = &domain.SSOConfig{
		WorkspaceID: second.ID, IssuerURL: rig.idp.server.URL, ClientID: "test-client",
		DefaultRole: domain.RoleMember, Enabled: true, RequireSSO: true, UpdatedAt: time.Now(),
	}
	rig.repo.configs[rig.workspace.ID].RequireSSO = true

	rig.repo.roles = []domain.SSOWorkspaceRole{
		{WorkspaceID: rig.workspace.ID, Slug: "acme", Role: domain.RoleMember},
		{WorkspaceID: second.ID, Slug: "beta", Role: domain.RoleMember},
	}
	slug, required, err := rig.service.UserSSORequired(ctx, userID)
	require.NoError(t, err)
	require.True(t, required)
	require.Equal(t, "acme", slug)

	// One workspace without require-SSO is enough to keep password sign-in.
	rig.repo.roles[1].Role = domain.RoleAdmin
	rig.repo.configs[second.ID].RequireSSO = false
	_, required, err = rig.service.UserSSORequired(ctx, userID)
	require.NoError(t, err)
	require.False(t, required)

	// An owner is the break-glass path even when every workspace enforces SSO.
	rig.repo.configs[second.ID].RequireSSO = true
	rig.repo.roles[1].Role = domain.RoleOwner
	_, required, err = rig.service.UserSSORequired(ctx, userID)
	require.NoError(t, err)
	require.False(t, required)

	// A user without memberships is not blocked.
	rig.repo.roles = nil
	_, required, err = rig.service.UserSSORequired(ctx, userID)
	require.NoError(t, err)
	require.False(t, required)
}
