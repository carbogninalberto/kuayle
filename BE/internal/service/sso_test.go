package service

import (
	"context"
	stdcrypto "crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
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

func (r *ssoFakeRepo) HasEnabledConfig(context.Context) (bool, error) {
	for _, config := range r.configs {
		if config.Enabled {
			return true, nil
		}
	}
	return false, nil
}

func (r *ssoFakeRepo) RequiresSSO(ctx context.Context, workspaceID uuid.UUID) (bool, error) {
	config := r.configs[workspaceID]
	return config != nil && config.Enabled && config.RequireSSO, nil
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
	if identity := r.identities[key]; identity != nil {
		return r.users[identity.UserID], nil
	}
	for _, candidate := range r.users {
		if strings.EqualFold(candidate.Email, params.Email) {
			return nil, repository.ErrSSOLinkRequired
		}
	}
	user := &domain.User{ID: uuid.New(), Email: params.Email, Name: params.Name, DisplayName: params.Name, PasswordHash: params.PasswordHash}
	r.users[user.ID] = user
	r.identities[key] = &domain.UserIdentity{
		ID: uuid.New(), UserID: user.ID, WorkspaceID: params.WorkspaceID,
		Provider: params.Provider, Issuer: params.Issuer, Subject: params.Subject, Email: params.Email,
	}
	memberKey := params.WorkspaceID.String() + "|" + user.ID.String()
	if _, ok := r.memberships[memberKey]; !ok {
		r.memberships[memberKey] = params.DefaultRole
	}
	return user, nil
}

func (r *ssoFakeRepo) LinkIdentity(ctx context.Context, userID, workspaceID uuid.UUID, provider, issuer, subject, email string) (*domain.UserIdentity, error) {
	key := identityKey(workspaceID, issuer, subject)
	if identity := r.identities[key]; identity != nil {
		if identity.UserID != userID {
			return nil, repository.ErrSSOIdentityTaken
		}
		return identity, nil
	}
	identity := &domain.UserIdentity{
		ID: uuid.New(), UserID: userID, WorkspaceID: workspaceID,
		Provider: provider, Issuer: issuer, Subject: subject, Email: email,
	}
	r.identities[key] = identity
	return identity, nil
}

func (r *ssoFakeRepo) HasIdentity(ctx context.Context, userID, workspaceID uuid.UUID) (bool, error) {
	for _, identity := range r.identities {
		if identity.UserID == userID && identity.WorkspaceID == workspaceID {
			return true, nil
		}
	}
	return false, nil
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

type ssoFakeWorkspaces struct {
	workspaces  map[string]*domain.Workspace
	memberships map[string]string
	// lookups counts GetBySlug calls per slug.
	lookups map[string]int
}

func (w ssoFakeWorkspaces) GetBySlug(ctx context.Context, slug string) (*domain.Workspace, error) {
	if w.lookups != nil {
		w.lookups[slug]++
	}
	return w.workspaces[slug], nil
}

func (w ssoFakeWorkspaces) GetMember(ctx context.Context, workspaceID, userID uuid.UUID) (*domain.WorkspaceMember, error) {
	role, ok := w.memberships[workspaceID.String()+"|"+userID.String()]
	if !ok {
		return nil, nil
	}
	return &domain.WorkspaceMember{WorkspaceID: workspaceID, UserID: userID, Role: role}, nil
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
	server     *httptest.Server
	key        *rsa.PrivateKey
	otherKey   *rsa.PrivateKey
	issuerPath string
	mu         sync.Mutex
	codes      map[string]*fakeIDPCode
}

// issuer is the exact issuer the IdP advertises. Tests set issuerPath to "/"
// to cover IdPs such as Auth0 whose issuer ends in a slash.
func (f *fakeIDP) issuer() string { return f.server.URL + f.issuerPath }

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
			"issuer":                                idp.issuer(),
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
		"iss":   f.issuer(),
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
	lookups   map[string]int
	byslug    map[string]*domain.Workspace
}

func newSSOTestRig(t *testing.T) *ssoTestRig {
	t.Helper()
	repo := newSSOFakeRepo()
	users := &ssoFakeUsers{users: map[uuid.UUID]*domain.User{}}
	refresh := &ssoFakeRefresh{}
	workspace := &domain.Workspace{ID: uuid.New(), Slug: "acme", Name: "Acme", OwnerID: uuid.New()}
	workspaces := ssoFakeWorkspaces{workspaces: map[string]*domain.Workspace{"acme": workspace}, memberships: repo.memberships, lookups: map[string]int{}}
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
	return &ssoTestRig{service: service, repo: repo, users: users, refresh: refresh, idp: idp, workspace: workspace, key: key,
		lookups: workspaces.lookups, byslug: workspaces.workspaces}
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

func TestSSOExistingAccountIsNeverAutoLinked(t *testing.T) {
	rig := newSSOTestRig(t)
	existing := &domain.User{ID: uuid.New(), Email: "person@example.com", Name: "Existing", PasswordHash: "hash"}
	rig.users.users[existing.ID] = existing
	rig.repo.users[existing.ID] = existing

	_, _, _, _, err := rig.callback(t, nil)
	require.ErrorIs(t, err, ErrSSOLinkRequired)
	require.Empty(t, rig.repo.identities, "no identity may be created for an existing account")
	require.Empty(t, rig.repo.memberships, "an existing account is never added to the workspace")
	require.Empty(t, rig.refresh.created, "no session may be minted")
}

func TestSSOAttackerWorkspaceCannotTakeOverAccount(t *testing.T) {
	rig := newSSOTestRig(t)
	ctx := context.Background()
	attacker := &domain.Workspace{ID: uuid.New(), Slug: "attacker", Name: "Attacker", OwnerID: uuid.New()}
	rig.service.workspaces.(ssoFakeWorkspaces).workspaces[attacker.Slug] = attacker
	secret, err := kcrypto.Encrypt("client-secret", rig.key)
	require.NoError(t, err)
	rig.repo.configs[attacker.ID] = &domain.SSOConfig{
		WorkspaceID: attacker.ID, IssuerURL: rig.idp.server.URL, ClientID: "test-client",
		ClientSecretEncrypted: secret, DefaultRole: domain.RoleMember, Enabled: true, UpdatedAt: time.Now(),
	}

	victim := &domain.User{ID: uuid.New(), Email: "person@example.com", Name: "Victim", PasswordHash: "hash"}
	rig.users.users[victim.ID] = victim
	rig.repo.users[victim.ID] = victim
	// Being invited to the attacker's workspace is not consent to link: the
	// invite adds any existing account by email alone.
	rig.repo.memberships[attacker.ID.String()+"|"+victim.ID.String()] = domain.RoleMember

	authURL, bind, err := rig.service.Start(ctx, attacker.Slug, "")
	require.NoError(t, err)
	code := rig.idp.authorize(t, authURL, nil)
	_, _, _, _, err = rig.service.CallbackRedirect(ctx, attacker.Slug, stateFromAuthURL(t, authURL), code, bind)
	require.ErrorIs(t, err, ErrSSOLinkRequired)
	require.Empty(t, rig.refresh.created, "no session may be minted for the victim")
	for _, identity := range rig.repo.identities {
		require.NotEqual(t, victim.ID, identity.UserID)
	}
}

func TestSSOLinkFlow(t *testing.T) {
	ctx := context.Background()
	newLinkedRig := func(t *testing.T) (*ssoTestRig, *domain.User) {
		t.Helper()
		rig := newSSOTestRig(t)
		user := &domain.User{ID: uuid.New(), Email: "person@example.com", Name: "Existing", PasswordHash: "hash"}
		rig.users.users[user.ID] = user
		rig.repo.users[user.ID] = user
		rig.repo.memberships[rig.workspace.ID.String()+"|"+user.ID.String()] = domain.RoleMember
		return rig, user
	}

	t.Run("member links and signs in", func(t *testing.T) {
		rig, user := newLinkedRig(t)
		authURL, bind, err := rig.service.StartLink(ctx, "acme", user.ID, "/acme/inbox")
		require.NoError(t, err)
		code := rig.idp.authorize(t, authURL, nil)
		signedIn, access, refresh, redirect, err := rig.service.CallbackRedirect(ctx, "acme", stateFromAuthURL(t, authURL), code, bind)
		require.NoError(t, err)
		require.Equal(t, user.ID, signedIn.ID)
		require.NotEmpty(t, access)
		require.NotEmpty(t, refresh)
		require.Equal(t, "/acme/inbox", redirect)
		identity, err := rig.repo.GetIdentity(ctx, rig.workspace.ID, rig.idp.server.URL, "user-1")
		require.NoError(t, err)
		require.NotNil(t, identity)
		require.Equal(t, user.ID, identity.UserID)
	})

	t.Run("account that already linked this identity signs in again", func(t *testing.T) {
		rig, user := newLinkedRig(t)
		rig.repo.identities[identityKey(rig.workspace.ID, rig.idp.issuer(), "user-1")] = &domain.UserIdentity{
			ID: uuid.New(), UserID: user.ID, WorkspaceID: rig.workspace.ID,
			Provider: domain.ProviderOIDC, Issuer: rig.idp.issuer(), Subject: "user-1", Email: user.Email,
		}
		authURL, bind, err := rig.service.StartLink(ctx, "acme", user.ID, "/acme/inbox")
		require.NoError(t, err)
		code := rig.idp.authorize(t, authURL, nil)
		signedIn, access, refresh, redirect, err := rig.service.CallbackRedirect(ctx, "acme", stateFromAuthURL(t, authURL), code, bind)
		require.NoError(t, err)
		require.Equal(t, user.ID, signedIn.ID)
		require.NotEmpty(t, access)
		require.NotEmpty(t, refresh)
		require.Equal(t, "/acme/inbox", redirect)
		require.Len(t, rig.repo.identities, 1)
	})

	t.Run("non-member cannot start a link", func(t *testing.T) {
		rig, _ := newLinkedRig(t)
		_, _, err := rig.service.StartLink(ctx, "acme", uuid.New(), "")
		require.ErrorIs(t, err, ErrSSONotMember)
	})

	t.Run("subject linked to another account is refused", func(t *testing.T) {
		rig, user := newLinkedRig(t)
		other := &domain.User{ID: uuid.New(), Email: "other@example.com", Name: "Other", PasswordHash: "hash"}
		rig.users.users[other.ID] = other
		rig.repo.users[other.ID] = other
		rig.repo.identities[identityKey(rig.workspace.ID, rig.idp.server.URL, "user-1")] = &domain.UserIdentity{
			ID: uuid.New(), UserID: other.ID, WorkspaceID: rig.workspace.ID,
			Provider: domain.ProviderOIDC, Issuer: rig.idp.server.URL, Subject: "user-1", Email: other.Email,
		}
		authURL, bind, err := rig.service.StartLink(ctx, "acme", user.ID, "")
		require.NoError(t, err)
		code := rig.idp.authorize(t, authURL, nil)
		_, _, _, _, err = rig.service.CallbackRedirect(ctx, "acme", stateFromAuthURL(t, authURL), code, bind)
		require.ErrorIs(t, err, ErrSSOIdentityTaken)
	})

	t.Run("link state cannot be replayed on another workspace", func(t *testing.T) {
		rig, user := newLinkedRig(t)
		other := &domain.Workspace{ID: uuid.New(), Slug: "beta", Name: "Beta", OwnerID: uuid.New()}
		rig.service.workspaces.(ssoFakeWorkspaces).workspaces["beta"] = other
		secret, err := kcrypto.Encrypt("client-secret", rig.key)
		require.NoError(t, err)
		rig.repo.configs[other.ID] = &domain.SSOConfig{
			WorkspaceID: other.ID, IssuerURL: rig.idp.server.URL, ClientID: "test-client",
			ClientSecretEncrypted: secret, DefaultRole: domain.RoleMember, Enabled: true, UpdatedAt: time.Now(),
		}
		authURL, bind, err := rig.service.StartLink(ctx, "acme", user.ID, "")
		require.NoError(t, err)
		code := rig.idp.authorize(t, authURL, nil)
		_, _, _, _, err = rig.service.CallbackRedirect(ctx, "beta", stateFromAuthURL(t, authURL), code, bind)
		require.ErrorIs(t, err, ErrSSOStateInvalid)
	})

	t.Run("link flow still honours allowed domains", func(t *testing.T) {
		rig, user := newLinkedRig(t)
		rig.repo.configs[rig.workspace.ID].AllowedDomains = []string{"company.example"}
		authURL, bind, err := rig.service.StartLink(ctx, "acme", user.ID, "")
		require.NoError(t, err)
		code := rig.idp.authorize(t, authURL, nil)
		_, _, _, _, err = rig.service.CallbackRedirect(ctx, "acme", stateFromAuthURL(t, authURL), code, bind)
		require.ErrorIs(t, err, ErrSSODomainNotAllowed)
	})
}

func TestSSOReturningIdentityRechecksMembershipAndDomain(t *testing.T) {
	t.Run("removed member is refused", func(t *testing.T) {
		rig := newSSOTestRig(t)
		user, _, _, _, err := rig.callback(t, nil)
		require.NoError(t, err)
		delete(rig.repo.memberships, rig.workspace.ID.String()+"|"+user.ID.String())
		_, _, _, _, err = rig.callback(t, nil)
		require.ErrorIs(t, err, ErrSSONotMember)
	})

	t.Run("narrowed allowed domains cut off returning identities", func(t *testing.T) {
		rig := newSSOTestRig(t)
		_, _, _, _, err := rig.callback(t, nil)
		require.NoError(t, err)
		rig.repo.configs[rig.workspace.ID].AllowedDomains = []string{"company.example"}
		_, _, _, _, err = rig.callback(t, func(c *fakeIDPCode) { c.claims["email"] = "person@other.example" })
		require.ErrorIs(t, err, ErrSSODomainNotAllowed)
	})
}

func TestSSOIssuerWithTrailingSlash(t *testing.T) {
	rig := newSSOTestRig(t)
	rig.idp.issuerPath = "/"
	issuer := rig.idp.issuer()

	// Saving keeps the trailing slash that discovery advertises.
	_, err := rig.service.SaveConfig(context.Background(), "acme", dto.SSOConfigRequest{
		IssuerURL: issuer, ClientID: "test-client", DefaultRole: domain.RoleMember,
		Enabled: true, AllowedDomains: []string{"example.com"},
	})
	require.NoError(t, err)
	require.Equal(t, issuer, rig.repo.configs[rig.workspace.ID].IssuerURL)

	user, _, _, _, err := rig.callback(t, nil)
	require.NoError(t, err)
	require.Equal(t, "person@example.com", user.Email)
}

func TestSSOLinkRejectsWrongBrowserBinding(t *testing.T) {
	rig := newSSOTestRig(t)
	user := &domain.User{ID: uuid.New(), Email: "person@example.com", Name: "Existing", PasswordHash: "hash"}
	rig.users.users[user.ID] = user
	rig.repo.users[user.ID] = user
	rig.repo.memberships[rig.workspace.ID.String()+"|"+user.ID.String()] = domain.RoleMember

	authURL, _, err := rig.service.StartLink(context.Background(), "acme", user.ID, "")
	require.NoError(t, err)
	code := rig.idp.authorize(t, authURL, nil)
	_, _, _, _, err = rig.service.CallbackRedirect(context.Background(), "acme", stateFromAuthURL(t, authURL), code, "wrong-bind")
	require.ErrorIs(t, err, ErrSSOStateInvalid)
	require.Empty(t, rig.repo.identities)
}

func TestSSOIdentityInAnotherWorkspaceDoesNotAuthenticate(t *testing.T) {
	rig := newSSOTestRig(t)
	other := &domain.Workspace{ID: uuid.New(), Slug: "beta", Name: "Beta", OwnerID: uuid.New()}
	rig.service.workspaces.(ssoFakeWorkspaces).workspaces["beta"] = other
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

// RequiresSSO runs on every authenticated workspace request, so it must read
// only the stored flags and never decrypt the client secret.
func TestSSORequiresSSODoesNotDecryptClientSecret(t *testing.T) {
	rig := newSSOTestRig(t)
	ctx := context.Background()
	config := rig.repo.configs[rig.workspace.ID]
	config.Enabled = true
	config.RequireSSO = true
	config.ClientSecretEncrypted = "not-a-ciphertext"

	// Reading the full configuration still fails on the invalid ciphertext; the
	// middleware check must not depend on it.
	_, err := rig.service.resolved(ctx, rig.workspace.ID)
	require.Error(t, err)

	required, err := rig.service.RequiresSSO(ctx, rig.workspace.ID)
	require.NoError(t, err)
	require.True(t, required)

	config.Enabled = false
	required, err = rig.service.RequiresSSO(ctx, rig.workspace.ID)
	require.NoError(t, err)
	require.False(t, required)
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

func TestUserSSORequiredUsesLinkedIdentities(t *testing.T) {
	rig := newSSOTestRig(t)
	ctx := context.Background()
	userID := uuid.New()
	second := &domain.Workspace{ID: uuid.New(), Slug: "beta", Name: "Beta", OwnerID: uuid.New()}
	rig.repo.configs[second.ID] = &domain.SSOConfig{
		WorkspaceID: second.ID, IssuerURL: rig.idp.server.URL, ClientID: "test-client",
		DefaultRole: domain.RoleMember, Enabled: true, RequireSSO: true, UpdatedAt: time.Now(),
	}
	rig.repo.configs[rig.workspace.ID].RequireSSO = true
	link := func(workspaceID uuid.UUID) {
		rig.repo.identities[identityKey(workspaceID, rig.idp.server.URL, "user-"+workspaceID.String())] = &domain.UserIdentity{
			ID: uuid.New(), UserID: userID, WorkspaceID: workspaceID,
			Provider: domain.ProviderOIDC, Issuer: rig.idp.server.URL, Subject: "user-" + workspaceID.String(),
		}
	}

	rig.repo.roles = []domain.SSOWorkspaceRole{
		{WorkspaceID: rig.workspace.ID, Slug: "acme", Role: domain.RoleMember},
		{WorkspaceID: second.ID, Slug: "beta", Role: domain.RoleMember},
	}
	// No identity yet: the account must still be able to sign in with its
	// password and link, so password sign-in is not refused.
	_, required, err := rig.service.UserSSORequired(ctx, userID)
	require.NoError(t, err)
	require.False(t, required)

	// With a linked identity in every enforced workspace, password sign-in is
	// refused and the first workspace is named.
	link(rig.workspace.ID)
	link(second.ID)
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

func TestSSOInstanceSlugWithoutWorkspaceIsIgnored(t *testing.T) {
	rig := newSSOTestRig(t)
	ctx := context.Background()
	rig.repo.configs[rig.workspace.ID].RequireSSO = true
	rig.service.instance = InstanceSSOConfig{
		WorkspaceSlug: "typo", IssuerURL: rig.idp.server.URL, ClientID: "instance-client",
		ClientSecret: "instance-secret", RequireSSO: true,
	}

	required, err := rig.service.RequiresSSO(ctx, rig.workspace.ID)
	require.NoError(t, err)
	require.True(t, required, "the workspace's own config still applies")

	required, err = rig.service.RequiresSSO(ctx, uuid.New())
	require.NoError(t, err)
	require.False(t, required)

	rig.repo.roles = []domain.SSOWorkspaceRole{{WorkspaceID: rig.workspace.ID, Slug: "acme", Role: domain.RoleMember}}
	_, _, err = rig.service.UserSSORequired(ctx, uuid.New())
	require.NoError(t, err)

	info, err := rig.service.PublicInfo(ctx, "acme")
	require.NoError(t, err)
	require.True(t, info.Enabled)

	// The miss is cached: later requests do not look the slug up again.
	require.Equal(t, 1, rig.lookups["typo"])
}

func TestSSOSignInAvailability(t *testing.T) {
	rig := newSSOTestRig(t)
	ctx := context.Background()
	config := rig.repo.configs[rig.workspace.ID]

	enabled, err := rig.service.SignInEnabled(ctx)
	require.NoError(t, err)
	require.True(t, enabled)

	config.Enabled = false
	enabled, err = rig.service.SignInEnabled(ctx)
	require.NoError(t, err)
	require.False(t, enabled)

	rig.service.instance = InstanceSSOConfig{
		WorkspaceSlug: "missing", IssuerURL: rig.idp.server.URL, ClientID: "instance-client",
	}
	enabled, err = rig.service.SignInEnabled(ctx)
	require.NoError(t, err)
	require.False(t, enabled)

	rig.service.instance.WorkspaceSlug = rig.workspace.Slug
	enabled, err = rig.service.SignInEnabled(ctx)
	require.NoError(t, err)
	require.True(t, enabled)

	delete(rig.byslug, rig.workspace.Slug)
	enabled, err = rig.service.SignInEnabled(ctx)
	require.NoError(t, err)
	require.False(t, enabled)
}

func TestSSOInstanceWorkspaceIDIsCached(t *testing.T) {
	rig := newSSOTestRig(t)
	ctx := context.Background()
	rig.service.instance = InstanceSSOConfig{
		WorkspaceSlug: "acme", IssuerURL: rig.idp.server.URL, ClientID: "instance-client",
		ClientSecret: "instance-secret", RequireSSO: true,
	}
	for i := 0; i < 3; i++ {
		required, err := rig.service.RequiresSSO(ctx, rig.workspace.ID)
		require.NoError(t, err)
		require.True(t, required)
	}
	require.Equal(t, 1, rig.lookups["acme"])
}

func TestSSOInstanceWorkspaceCreatedLaterIsPickedUp(t *testing.T) {
	rig := newSSOTestRig(t)
	ctx := context.Background()
	now := time.Now()
	rig.service.now = func() time.Time { return now }
	rig.service.instance = InstanceSSOConfig{
		WorkspaceSlug: "later", IssuerURL: rig.idp.server.URL, ClientID: "instance-client",
		ClientSecret: "instance-secret", RequireSSO: true,
	}
	// Startup check: the workspace does not exist yet, and it must not fail.
	rig.service.CheckInstanceWorkspace(ctx)

	later := &domain.Workspace{ID: uuid.New(), Slug: "later", Name: "Later", OwnerID: uuid.New()}
	rig.byslug["later"] = later
	required, err := rig.service.RequiresSSO(ctx, later.ID)
	require.NoError(t, err)
	require.False(t, required, "the miss is cached for a short while")

	now = now.Add(31 * time.Second)
	required, err = rig.service.RequiresSSO(ctx, later.ID)
	require.NoError(t, err)
	require.True(t, required, "the instance config applies once the workspace exists")
}

type failingSSOWorkspaces struct{ ssoFakeWorkspaces }

func (failingSSOWorkspaces) GetBySlug(ctx context.Context, slug string) (*domain.Workspace, error) {
	return nil, errors.New("database unavailable")
}

func TestSSOInstanceLookupErrorIsReturnedNotCached(t *testing.T) {
	rig := newSSOTestRig(t)
	rig.service.workspaces = failingSSOWorkspaces{}
	rig.service.instance = InstanceSSOConfig{WorkspaceSlug: "acme", IssuerURL: rig.idp.server.URL, ClientID: "c"}
	// Startup only logs a lookup failure.
	rig.service.CheckInstanceWorkspace(context.Background())
	_, err := rig.service.RequiresSSO(context.Background(), rig.workspace.ID)
	require.Error(t, err)
}

func TestSSOSharedIssuerNeedsAllowedDomains(t *testing.T) {
	rig := newSSOTestRig(t)
	for _, issuer := range []string{
		"https://accounts.google.com",
		"https://login.microsoftonline.com/common/v2.0",
		"https://login.microsoftonline.com/organizations",
		"https://login.microsoftonline.com/consumers/v2.0/",
	} {
		_, err := rig.service.SaveConfig(context.Background(), "acme", dto.SSOConfigRequest{
			IssuerURL: issuer, ClientID: "test-client", DefaultRole: domain.RoleMember, Enabled: true,
		})
		require.ErrorIs(t, err, ErrSSOInvalidInput, issuer)
		require.Contains(t, err.Error(), "Allowed email domains", issuer)
	}

	// With a domain the rule passes; discovery is the next step, made to fail
	// here so the test never reaches the real provider.
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := rig.service.SaveConfig(cancelled, "acme", dto.SSOConfigRequest{
		IssuerURL: "https://accounts.google.com", ClientID: "test-client", DefaultRole: domain.RoleMember,
		Enabled: true, AllowedDomains: []string{"example.com"},
	})
	require.ErrorIs(t, err, ErrSSOInvalidInput)
	require.Contains(t, err.Error(), "discovery")

	// A disabled config may be saved without domains.
	_, err = rig.service.SaveConfig(cancelled, "acme", dto.SSOConfigRequest{
		IssuerURL: "https://accounts.google.com", ClientID: "test-client", DefaultRole: domain.RoleMember,
	})
	require.NotContains(t, err.Error(), "Allowed email domains")

	// A dedicated issuer (the fake IdP) needs no domain list.
	_, err = rig.service.SaveConfig(context.Background(), "acme", dto.SSOConfigRequest{
		IssuerURL: rig.idp.server.URL, ClientID: "test-client", DefaultRole: domain.RoleMember, Enabled: true,
	})
	require.NoError(t, err)
}
