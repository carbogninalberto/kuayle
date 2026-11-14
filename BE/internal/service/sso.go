package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/kuayle/kuayle-backend/pkg/forge"
	"github.com/kuayle/kuayle-backend/pkg/validate"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/oauth2"
)

// SSOLoginStateTTL is how long a started OIDC flow may be completed.
const SSOLoginStateTTL = 10 * time.Minute

var (
	// ErrSSONotConfigured: the workspace has no enabled OIDC configuration.
	ErrSSONotConfigured = errors.New("SSO is not configured for this workspace")
	// ErrSSOStateInvalid: the callback state is unknown, expired, already used,
	// belongs to another workspace or browser.
	ErrSSOStateInvalid = errors.New("SSO state is missing, expired or does not belong to this workspace")
	// ErrSSOEmailUnverified: the IdP did not assert a verified email address.
	ErrSSOEmailUnverified = errors.New("the identity provider did not verify the email address")
	// ErrSSODomainNotAllowed: the verified email is outside allowed_domains.
	ErrSSODomainNotAllowed = errors.New("the email domain is not allowed for this workspace")
	// ErrSSOTokenInvalid: the token exchange or ID token verification failed.
	ErrSSOTokenInvalid = errors.New("the identity provider response could not be verified")
	// ErrSSOInstanceManaged: the configuration comes from instance environment
	// variables and cannot be changed from the workspace settings.
	ErrSSOInstanceManaged = errors.New("SSO for this workspace is managed by the instance configuration")
	// ErrSSOInvalidInput is the base of a safe validation failure.
	ErrSSOInvalidInput = errors.New("invalid SSO configuration")
)

// SSOConfigError is a validation failure whose message is safe to show to the
// workspace manager. It unwraps to ErrSSOInvalidInput.
type SSOConfigError struct{ message string }

func (e *SSOConfigError) Error() string { return e.message }

func (e *SSOConfigError) Is(target error) bool { return target == ErrSSOInvalidInput }

func ssoInvalidf(format string, args ...any) error {
	return &SSOConfigError{message: fmt.Sprintf(format, args...)}
}

// InstanceSSOConfig is the instance-level OIDC configuration. When its
// workspace slug matches, it wins over the workspace's database settings.
type InstanceSSOConfig struct {
	WorkspaceSlug  string
	IssuerURL      string
	ClientID       string
	ClientSecret   string
	AllowedDomains []string
	DefaultRole    string
	RequireSSO     bool
}

func (c InstanceSSOConfig) Configured() bool {
	return strings.TrimSpace(c.WorkspaceSlug) != "" && strings.TrimSpace(c.IssuerURL) != "" && strings.TrimSpace(c.ClientID) != ""
}

// SSOWorkspaceLookup resolves a workspace slug for the public sign-in routes.
type SSOWorkspaceLookup interface {
	GetBySlug(ctx context.Context, slug string) (*domain.Workspace, error)
}

type resolvedSSOConfig struct {
	WorkspaceID       uuid.UUID
	IssuerURL         string
	ClientID          string
	ClientSecret      string
	AllowedDomains    []string
	ProviderName      string
	DefaultRole       string
	RequireSSO        bool
	Enabled           bool
	ManagedByInstance bool
	UpdatedAt         time.Time
}

type cachedSSOProvider struct {
	issuer    string
	clientID  string
	updatedAt time.Time
	provider  *oidc.Provider
}

// SSOService runs the OIDC authorization-code flow and manages per-workspace
// configuration, identities and just-in-time provisioning.
type SSOService struct {
	repo          repository.SSORepo
	users         repository.UserRepo
	workspaces    SSOWorkspaceLookup
	auth          *AuthService
	encryptionKey []byte
	frontendURL   string
	allowPrivate  bool
	httpClient    *http.Client
	instance      InstanceSSOConfig
	now           func() time.Time
	mu            sync.Mutex
	providers     map[uuid.UUID]cachedSSOProvider
}

func NewSSOService(
	repo repository.SSORepo,
	users repository.UserRepo,
	workspaces SSOWorkspaceLookup,
	auth *AuthService,
	encryptionKey []byte,
	frontendURL string,
	allowPrivateNetwork bool,
	instance InstanceSSOConfig,
) *SSOService {
	return &SSOService{
		repo:          repo,
		users:         users,
		workspaces:    workspaces,
		auth:          auth,
		encryptionKey: encryptionKey,
		frontendURL:   strings.TrimRight(frontendURL, "/"),
		allowPrivate:  allowPrivateNetwork,
		httpClient:    forge.NewHTTPClient(allowPrivateNetwork),
		instance:      instance,
		now:           time.Now,
		providers:     map[uuid.UUID]cachedSSOProvider{},
	}
}

// redirectURI is the callback an administrator registers at the IdP.
func (s *SSOService) redirectURI(slug string) string {
	return s.frontendURL + "/api/auth/sso/" + url.PathEscape(slug) + "/callback"
}

func (s *SSOService) workspace(ctx context.Context, slug string) (*domain.Workspace, error) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return nil, ErrSSONotConfigured
	}
	ws, err := s.workspaces.GetBySlug(ctx, slug)
	if err != nil {
		return nil, err
	}
	if ws == nil {
		return nil, ErrSSONotConfigured
	}
	return ws, nil
}

// resolved merges the instance-level configuration over the workspace one.
func (s *SSOService) resolved(ctx context.Context, workspaceID uuid.UUID) (*resolvedSSOConfig, error) {
	stored, err := s.repo.GetConfig(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if s.instance.Configured() {
		instance, err := s.workspace(ctx, s.instance.WorkspaceSlug)
		if err != nil {
			return nil, err
		}
		if instance != nil && instance.ID == workspaceID {
			return &resolvedSSOConfig{
				WorkspaceID:       workspaceID,
				IssuerURL:         strings.TrimRight(s.instance.IssuerURL, "/"),
				ClientID:          s.instance.ClientID,
				ClientSecret:      s.instance.ClientSecret,
				AllowedDomains:    normalizeDomains(s.instance.AllowedDomains),
				ProviderName:      strings.TrimSpace(s.instance.WorkspaceSlug),
				DefaultRole:       normalizedDefaultRole(s.instance.DefaultRole),
				RequireSSO:        s.instance.RequireSSO,
				Enabled:           true,
				ManagedByInstance: true,
			}, nil
		}
	}
	if stored == nil {
		return nil, nil
	}
	secret := ""
	if stored.ClientSecretEncrypted != "" {
		secret, err = crypto.Decrypt(stored.ClientSecretEncrypted, s.encryptionKey)
		if err != nil {
			return nil, fmt.Errorf("decrypting the SSO client secret: %w", err)
		}
	}
	return &resolvedSSOConfig{
		WorkspaceID:    stored.WorkspaceID,
		IssuerURL:      strings.TrimRight(stored.IssuerURL, "/"),
		ClientID:       stored.ClientID,
		ClientSecret:   secret,
		AllowedDomains: normalizeDomains(stored.AllowedDomains),
		ProviderName:   stored.ProviderName,
		DefaultRole:    normalizedDefaultRole(stored.DefaultRole),
		RequireSSO:     stored.RequireSSO,
		Enabled:        stored.Enabled,
		UpdatedAt:      stored.UpdatedAt,
	}, nil
}

// RequiresSSO reports whether a workspace blocks non-SSO sessions. It is used
// by the workspace membership middleware.
func (s *SSOService) RequiresSSO(ctx context.Context, workspaceID uuid.UUID) (bool, error) {
	config, err := s.resolved(ctx, workspaceID)
	if err != nil {
		return false, err
	}
	return config != nil && config.Enabled && config.RequireSSO, nil
}

// UserSSORequired reports whether every workspace the user belongs to requires
// SSO and none of them is owned by the user. It returns one workspace slug to
// start the flow from.
func (s *SSOService) UserSSORequired(ctx context.Context, userID uuid.UUID) (string, bool, error) {
	roles, err := s.repo.UserWorkspaceRoles(ctx, userID)
	if err != nil {
		return "", false, err
	}
	if len(roles) == 0 {
		return "", false, nil
	}
	slug := ""
	for _, membership := range roles {
		if membership.Role == domain.RoleOwner {
			return "", false, nil
		}
		required, err := s.RequiresSSO(ctx, membership.WorkspaceID)
		if err != nil {
			return "", false, err
		}
		if !required {
			return "", false, nil
		}
		if slug == "" {
			slug = membership.Slug
		}
	}
	return slug, true, nil
}

// GetConfig returns the workspace's configuration for the settings page. It
// never includes the client secret, only whether one is stored.
func (s *SSOService) GetConfig(ctx context.Context, slug string) (*dto.SSOConfigResponse, error) {
	ws, err := s.workspace(ctx, slug)
	if err != nil {
		return nil, err
	}
	config, err := s.resolved(ctx, ws.ID)
	if err != nil {
		return nil, err
	}
	response := &dto.SSOConfigResponse{
		DefaultRole:       domain.RoleMember,
		AllowedDomains:    []string{},
		RedirectURI:       s.redirectURI(ws.Slug),
		ManagedByInstance: s.instance.Configured() && s.instance.WorkspaceSlug == ws.Slug,
	}
	if config == nil {
		response.Enabled = false
		return response, nil
	}
	response.IssuerURL = config.IssuerURL
	response.ClientID = config.ClientID
	response.HasClientSecret = config.ClientSecret != ""
	response.AllowedDomains = config.AllowedDomains
	response.DefaultRole = config.DefaultRole
	response.RequireSSO = config.RequireSSO
	response.Enabled = config.Enabled
	response.ProviderName = config.ProviderName
	response.ManagedByInstance = config.ManagedByInstance
	return response, nil
}

// SaveConfig creates or updates the workspace OIDC configuration. A request
// without a client secret keeps the stored one.
func (s *SSOService) SaveConfig(ctx context.Context, slug string, req dto.SSOConfigRequest) (*dto.SSOConfigResponse, error) {
	ws, err := s.workspace(ctx, slug)
	if err != nil {
		return nil, err
	}
	if s.instance.Configured() && s.instance.WorkspaceSlug == ws.Slug {
		return nil, ErrSSOInstanceManaged
	}
	if req.RequireSSO && !req.Enabled {
		return nil, ssoInvalidf("Require SSO needs SSO to be enabled")
	}
	if !validSSODefaultRole(req.DefaultRole) {
		return nil, ssoInvalidf("Default role must be member or guest")
	}
	issuer, err := validate.OperatorBaseURL(req.IssuerURL)
	if err != nil {
		return nil, ssoInvalidf("Issuer URL %s", err.Error())
	}
	if issuer.Scheme != "https" && !s.allowPrivate {
		return nil, ssoInvalidf("Issuer URL must use https; set OIDC_ALLOW_PRIVATE_NETWORK for a private http identity provider")
	}
	clientID := strings.TrimSpace(req.ClientID)
	if clientID == "" {
		return nil, ssoInvalidf("Client ID is required")
	}
	domains, err := validDomains(req.AllowedDomains)
	if err != nil {
		return nil, err
	}

	// Discovery validates the issuer and must happen before the secret is
	// touched or stored. A failure never echoes the client secret.
	probe := &resolvedSSOConfig{IssuerURL: issuer.String(), ClientID: clientID}
	probeCtx := oidc.ClientContext(ctx, s.httpClient)
	if _, err := oidc.NewProvider(probeCtx, probe.IssuerURL); err != nil {
		return nil, ssoInvalidf("OIDC discovery failed for the issuer URL")
	}

	existing, err := s.repo.GetConfig(ctx, ws.ID)
	if err != nil {
		return nil, err
	}
	config := &domain.SSOConfig{
		WorkspaceID:    ws.ID,
		IssuerURL:      issuer.String(),
		ClientID:       clientID,
		AllowedDomains: domains,
		ProviderName:   strings.TrimSpace(req.ProviderName),
		DefaultRole:    req.DefaultRole,
		RequireSSO:     req.RequireSSO,
		Enabled:        req.Enabled,
	}
	if existing != nil {
		config.ClientSecretEncrypted = existing.ClientSecretEncrypted
	}
	if req.ClientSecret != nil && strings.TrimSpace(*req.ClientSecret) != "" {
		encrypted, err := crypto.Encrypt(*req.ClientSecret, s.encryptionKey)
		if err != nil {
			return nil, err
		}
		config.ClientSecretEncrypted = encrypted
	}
	if err := s.repo.SaveConfig(ctx, config); err != nil {
		return nil, err
	}
	s.invalidateProvider(ws.ID)
	return s.GetConfig(ctx, slug)
}

// DeleteConfig removes the workspace's OIDC configuration.
func (s *SSOService) DeleteConfig(ctx context.Context, slug string) error {
	ws, err := s.workspace(ctx, slug)
	if err != nil {
		return err
	}
	if s.instance.Configured() && s.instance.WorkspaceSlug == ws.Slug {
		return ErrSSOInstanceManaged
	}
	if err := s.repo.DeleteConfig(ctx, ws.ID); err != nil {
		return err
	}
	s.invalidateProvider(ws.ID)
	return nil
}

// PublicInfo tells the sign-in page whether a workspace offers SSO.
func (s *SSOService) PublicInfo(ctx context.Context, slug string) (*dto.SSOPublicInfo, error) {
	ws, err := s.workspace(ctx, slug)
	if err != nil {
		return nil, err
	}
	config, err := s.resolved(ctx, ws.ID)
	if err != nil {
		return nil, err
	}
	if config == nil || !config.Enabled {
		return &dto.SSOPublicInfo{Enabled: false}, nil
	}
	name := strings.TrimSpace(config.ProviderName)
	if name == "" {
		name = "SSO"
	}
	return &dto.SSOPublicInfo{Enabled: true, ProviderName: name}, nil
}

// Start begins the authorization-code flow and returns the IdP URL to send the
// browser to plus the binding value the callback cookie must carry.
func (s *SSOService) Start(ctx context.Context, slug, redirectAfter string) (string, string, error) {
	ws, err := s.workspace(ctx, slug)
	if err != nil {
		return "", "", err
	}
	config, err := s.resolved(ctx, ws.ID)
	if err != nil {
		return "", "", err
	}
	if config == nil || !config.Enabled {
		return "", "", ErrSSONotConfigured
	}
	provider, err := s.provider(ctx, config)
	if err != nil {
		return "", "", err
	}

	state, err := randomValue()
	if err != nil {
		return "", "", err
	}
	nonce, err := randomValue()
	if err != nil {
		return "", "", err
	}
	bind, err := randomValue()
	if err != nil {
		return "", "", err
	}
	verifier := oauth2.GenerateVerifier()
	encryptedVerifier, err := crypto.Encrypt(verifier, s.encryptionKey)
	if err != nil {
		return "", "", err
	}
	loginState := &domain.SSOLoginState{
		StateHash:             hashValue(state),
		WorkspaceID:           ws.ID,
		Nonce:                 nonce,
		CodeVerifierEncrypted: encryptedVerifier,
		BindHash:              hashValue(bind),
		RedirectAfter:         SanitizeRedirectAfter(redirectAfter),
		ExpiresAt:             s.now().Add(SSOLoginStateTTL),
	}
	if err := s.repo.CreateLoginState(ctx, loginState); err != nil {
		return "", "", err
	}
	// Opportunistic cleanup; a failure does not block a sign-in.
	_ = s.repo.DeleteExpiredLoginStates(ctx)

	oauthConfig := s.oauthConfig(ws.Slug, config, provider)
	authURL := oauthConfig.AuthCodeURL(state,
		oidc.Nonce(nonce),
		oauth2.S256ChallengeOption(verifier),
	)
	return authURL, bind, nil
}

// Callback is CallbackRedirect without the stored post-sign-in destination.
func (s *SSOService) Callback(ctx context.Context, slug, state, code, bindValue string) (*domain.User, string, string, error) {
	user, accessToken, refreshToken, _, err := s.CallbackRedirect(ctx, slug, state, code, bindValue)
	return user, accessToken, refreshToken, err
}

// CallbackRedirect finishes the flow: verify state, browser binding, PKCE and the ID
// token, then link or provision the user and mint an SSO session.
func (s *SSOService) CallbackRedirect(ctx context.Context, slug, state, code, bindValue string) (*domain.User, string, string, string, error) {
	ws, err := s.workspace(ctx, slug)
	if err != nil {
		return nil, "", "", "", err
	}
	config, err := s.resolved(ctx, ws.ID)
	if err != nil {
		return nil, "", "", "", err
	}
	if config == nil || !config.Enabled {
		return nil, "", "", "", ErrSSONotConfigured
	}
	if state == "" || bindValue == "" {
		return nil, "", "", "", ErrSSOStateInvalid
	}
	loginState, err := s.repo.ConsumeLoginState(ctx, hashValue(state))
	if err != nil {
		return nil, "", "", "", err
	}
	if loginState == nil || loginState.WorkspaceID != ws.ID ||
		!s.now().Before(loginState.ExpiresAt) ||
		subtle.ConstantTimeCompare([]byte(hashValue(bindValue)), []byte(loginState.BindHash)) != 1 {
		return nil, "", "", "", ErrSSOStateInvalid
	}
	verifier, err := crypto.Decrypt(loginState.CodeVerifierEncrypted, s.encryptionKey)
	if err != nil {
		return nil, "", "", "", ErrSSOStateInvalid
	}
	provider, err := s.provider(ctx, config)
	if err != nil {
		return nil, "", "", "", err
	}
	oauthConfig := s.oauthConfig(ws.Slug, config, provider)
	exchangeCtx := oidc.ClientContext(ctx, s.httpClient)
	token, err := oauthConfig.Exchange(exchangeCtx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return nil, "", "", "", ErrSSOTokenInvalid
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return nil, "", "", "", ErrSSOTokenInvalid
	}
	idToken, err := provider.Verifier(&oidc.Config{ClientID: config.ClientID}).Verify(exchangeCtx, rawIDToken)
	if err != nil {
		return nil, "", "", "", ErrSSOTokenInvalid
	}
	if subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(loginState.Nonce)) != 1 {
		return nil, "", "", "", ErrSSOTokenInvalid
	}

	var claims struct {
		Email             string `json:"email"`
		EmailVerified     any    `json:"email_verified"`
		Name              string `json:"name"`
		PreferredUsername string `json:"preferred_username"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return nil, "", "", "", ErrSSOTokenInvalid
	}
	email := strings.ToLower(strings.TrimSpace(claims.Email))

	identity, err := s.repo.GetIdentity(ctx, ws.ID, idToken.Issuer, idToken.Subject)
	if err != nil {
		return nil, "", "", "", err
	}
	if identity != nil {
		user, err := s.users.GetByID(ctx, identity.UserID)
		if err != nil {
			return nil, "", "", "", err
		}
		if user == nil {
			return nil, "", "", "", ErrSSOTokenInvalid
		}
		_ = s.repo.TouchIdentity(ctx, identity.ID)
		accessToken, refreshToken, err := s.auth.IssueSSOSession(ctx, user.ID, ws.ID)
		if err != nil {
			return nil, "", "", "", err
		}
		return user, accessToken, refreshToken, loginState.RedirectAfter, nil
	}

	if email == "" || !claimIsTrue(claims.EmailVerified) {
		return nil, "", "", "", ErrSSOEmailUnverified
	}
	if !domainAllowed(email, config.AllowedDomains) {
		return nil, "", "", "", ErrSSODomainNotAllowed
	}
	name := strings.TrimSpace(claims.Name)
	if name == "" {
		name = strings.TrimSpace(claims.PreferredUsername)
	}
	if name == "" {
		name = email
	}
	passwordHash, err := unusablePasswordHash()
	if err != nil {
		return nil, "", "", "", err
	}
	user, err := s.repo.ProvisionIdentity(ctx, repository.SSOProvisionParams{
		WorkspaceID:  ws.ID,
		Issuer:       idToken.Issuer,
		Subject:      idToken.Subject,
		Provider:     domain.ProviderOIDC,
		Email:        email,
		Name:         name,
		DefaultRole:  normalizedDefaultRole(config.DefaultRole),
		PasswordHash: passwordHash,
	})
	if err != nil {
		return nil, "", "", "", err
	}
	accessToken, refreshToken, err := s.auth.IssueSSOSession(ctx, user.ID, ws.ID)
	if err != nil {
		return nil, "", "", "", err
	}
	return user, accessToken, refreshToken, loginState.RedirectAfter, nil
}

func (s *SSOService) oauthConfig(slug string, config *resolvedSSOConfig, provider *oidc.Provider) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     config.ClientID,
		ClientSecret: config.ClientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  s.redirectURI(slug),
		Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
	}
}

func (s *SSOService) provider(ctx context.Context, config *resolvedSSOConfig) (*oidc.Provider, error) {
	s.mu.Lock()
	cached, ok := s.providers[config.WorkspaceID]
	s.mu.Unlock()
	if ok && cached.issuer == config.IssuerURL && cached.clientID == config.ClientID && cached.updatedAt.Equal(config.UpdatedAt) {
		return cached.provider, nil
	}
	provider, err := oidc.NewProvider(oidc.ClientContext(ctx, s.httpClient), config.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("OIDC discovery failed: %w", err)
	}
	s.mu.Lock()
	s.providers[config.WorkspaceID] = cachedSSOProvider{
		issuer: config.IssuerURL, clientID: config.ClientID, updatedAt: config.UpdatedAt, provider: provider,
	}
	s.mu.Unlock()
	return provider, nil
}

func (s *SSOService) invalidateProvider(workspaceID uuid.UUID) {
	s.mu.Lock()
	delete(s.providers, workspaceID)
	s.mu.Unlock()
}

// SanitizeRedirectAfter accepts only a same-origin relative path. Anything
// absolute, protocol-relative or containing control characters is dropped.
func SanitizeRedirectAfter(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") ||
		strings.Contains(raw, "\\") || strings.ContainsAny(raw, "\r\n\x00") {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.IsAbs() || parsed.Host != "" {
		return ""
	}
	return raw
}

func normalizeDomains(domains []string) []string {
	seen := make(map[string]struct{}, len(domains))
	out := make([]string, 0, len(domains))
	for _, domain := range domains {
		domain = strings.ToLower(strings.TrimSpace(domain))
		domain = strings.TrimPrefix(domain, "@")
		if domain == "" {
			continue
		}
		if _, ok := seen[domain]; ok {
			continue
		}
		seen[domain] = struct{}{}
		out = append(out, domain)
	}
	return out
}

func validDomains(domains []string) ([]string, error) {
	normalized := normalizeDomains(domains)
	for _, domain := range normalized {
		if !validDomainName(domain) {
			return nil, ssoInvalidf("%q is not a valid email domain", domain)
		}
	}
	if len(normalized) > 100 {
		return nil, ssoInvalidf("Too many allowed email domains")
	}
	return normalized, nil
}

func validDomainName(domain string) bool {
	if domain == "" || len(domain) > 253 || strings.ContainsAny(domain, " /@:?#\\") {
		return false
	}
	for _, label := range strings.Split(domain, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
				return false
			}
		}
	}
	return true
}

func domainAllowed(email string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	at := strings.LastIndex(email, "@")
	if at < 0 || at == len(email)-1 {
		return false
	}
	emailDomain := strings.ToLower(email[at+1:])
	for _, domain := range allowed {
		if domain == emailDomain {
			return true
		}
	}
	return false
}

func validSSODefaultRole(role string) bool {
	return role == domain.RoleMember || role == domain.RoleGuest
}

func normalizedDefaultRole(role string) string {
	if validSSODefaultRole(role) {
		return role
	}
	return domain.RoleMember
}

func claimIsTrue(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(typed, "true")
	default:
		return false
	}
}

func randomValue() (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func hashValue(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// unusablePasswordHash sets a random password nobody knows, so a JIT account
// cannot be signed into with a password.
func unusablePasswordHash() (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	hash, err := bcrypt.GenerateFromPassword(buffer, bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}
