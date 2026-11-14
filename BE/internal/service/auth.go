package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	jwtpkg "github.com/kuayle/kuayle-backend/pkg/jwt"
	"github.com/kuayle/kuayle-backend/pkg/mail"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials   = errors.New("invalid credentials")
	ErrEmailTaken           = errors.New("email already taken")
	ErrInvalidToken         = errors.New("invalid or expired token")
	ErrWeakPassword         = errors.New("password must contain at least one uppercase letter, one lowercase letter, and one digit")
	ErrRegistrationDisabled = errors.New("public registration is disabled")
	// ErrSSORequired: every workspace the account belongs to requires SSO and
	// the account owns none of them, so password and passkey sign-in is refused.
	ErrSSORequired = errors.New("this account must sign in with SSO")
)

// SSORequiredError names a workspace to start the SSO flow from. It unwraps to
// ErrSSORequired so handlers can map it to 403 SSO_REQUIRED.
type SSORequiredError struct {
	WorkspaceSlug string
}

func (e *SSORequiredError) Error() string { return ErrSSORequired.Error() }

func (e *SSORequiredError) Unwrap() error { return ErrSSORequired }

// SSOEnforcer answers whether an account may sign in with a password or
// passkey. Implemented by SSOService and attached after construction so the
// auth service does not depend on the SSO repository.
type SSOEnforcer interface {
	UserSSORequired(ctx context.Context, userID uuid.UUID) (workspaceSlug string, required bool, err error)
}

// InviteRedeemer validates and consumes workspace invite links during
// registration. Implemented by InviteLinkService.
type InviteRedeemer interface {
	Validate(ctx context.Context, token string) (*domain.WorkspaceInviteLink, *domain.Workspace, error)
	Register(ctx context.Context, token string, user *domain.User) error
}

type AuthServiceOption func(*AuthService)

// WithRegistrationDisabled gates public registration behind a valid invite token.
func WithRegistrationDisabled(disabled bool) AuthServiceOption {
	return func(s *AuthService) { s.disableRegistration = disabled }
}

// WithInviteRedeemer enables invite-token redemption on registration.
func WithInviteRedeemer(redeemer InviteRedeemer) AuthServiceOption {
	return func(s *AuthService) { s.inviteRedeemer = redeemer }
}

type AuthService struct {
	userRepo            repository.UserRepo
	refreshRepo         repository.RefreshTokenRepo
	jwtSecret           string
	disableRegistration bool
	inviteRedeemer      InviteRedeemer
	resetRepo           repository.PasswordResetRepo
	mailer              mail.Sender
	frontendURL         string
	logResetLinks       bool
	async               func(func()) // test hook; nil runs in a goroutine
	sso                 SSOEnforcer
}

// SetSSOEnforcer enables the per-workspace require-SSO sign-in check.
func (s *AuthService) SetSSOEnforcer(enforcer SSOEnforcer) { s.sso = enforcer }

func NewAuthService(userRepo repository.UserRepo, refreshRepo repository.RefreshTokenRepo, jwtSecret string, opts ...AuthServiceOption) *AuthService {
	s := &AuthService{userRepo: userRepo, refreshRepo: refreshRepo, jwtSecret: jwtSecret}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *AuthService) Register(ctx context.Context, req dto.RegisterRequest) (*domain.User, string, string, error) {
	if err := validatePasswordComplexity(req.Password); err != nil {
		return nil, "", "", err
	}

	inviteToken := strings.TrimSpace(req.InviteToken)
	if s.disableRegistration && inviteToken == "" {
		return nil, "", "", ErrRegistrationDisabled
	}
	if inviteToken != "" {
		if s.inviteRedeemer == nil {
			return nil, "", "", ErrInviteLinkInvalid
		}
		// Validate before creating the account so a bad token never
		// leaves a dangling registration behind.
		if _, _, err := s.inviteRedeemer.Validate(ctx, inviteToken); err != nil {
			return nil, "", "", err
		}
	}

	existing, err := s.userRepo.GetByEmail(ctx, req.Email)
	if err != nil {
		return nil, "", "", err
	}
	if existing != nil {
		return nil, "", "", ErrEmailTaken
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, "", "", err
	}

	user := &domain.User{
		ID:           uuid.New(),
		Email:        req.Email,
		Name:         req.Name,
		DisplayName:  req.Name,
		PasswordHash: string(hash),
	}

	if inviteToken != "" {
		err = s.inviteRedeemer.Register(ctx, inviteToken, user)
	} else {
		err = s.userRepo.Create(ctx, user)
	}
	if err != nil {
		if errors.Is(err, repository.ErrDuplicateEmail) {
			return nil, "", "", ErrEmailTaken
		}
		return nil, "", "", err
	}

	accessToken, refreshToken, err := s.issueSession(ctx, user.ID, time.Now(), nil)
	if err != nil {
		return nil, "", "", err
	}

	return user, accessToken, refreshToken, nil
}

func (s *AuthService) Login(ctx context.Context, req dto.LoginRequest) (*domain.User, string, string, error) {
	user, err := s.userRepo.GetByEmail(ctx, req.Email)
	if err != nil {
		return nil, "", "", err
	}
	if user == nil {
		return nil, "", "", ErrInvalidCredentials
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		return nil, "", "", ErrInvalidCredentials
	}

	// Only after the password is verified, so the check cannot reveal which
	// accounts exist.
	if err := s.RejectPasswordSignIn(ctx, user.ID); err != nil {
		return nil, "", "", err
	}

	accessToken, refreshToken, err := s.issueSession(ctx, user.ID, time.Now(), nil)
	if err != nil {
		return nil, "", "", err
	}

	return user, accessToken, refreshToken, nil
}

func (s *AuthService) RefreshTokens(ctx context.Context, refreshToken string) (string, string, error) {
	claims, err := jwtpkg.ValidateRefreshToken(refreshToken, s.jwtSecret)
	if err != nil {
		return "", "", ErrInvalidToken
	}

	tokenHash := hashToken(refreshToken)
	rt, err := s.refreshRepo.GetByHash(ctx, tokenHash)
	if err != nil {
		return "", "", ErrInvalidToken
	}

	// Rotate refresh token
	_ = s.refreshRepo.DeleteByHash(ctx, tokenHash)

	// The new session keeps when and how the user signed in: refreshing is not
	// a new authentication.
	var authenticatedAt time.Time
	if rt.AuthenticatedAt != nil {
		authenticatedAt = *rt.AuthenticatedAt
	}
	return s.issueSessionFor(ctx, claims.UserID, authenticatedAt, rt.PasskeyCredentialID, rt.SSOWorkspaceID)
}

// RejectPasswordSignIn refuses a verified password or passkey sign-in when
// every workspace the account belongs to requires SSO and the account owns
// none of them. Owners keep the break-glass password path.
func (s *AuthService) RejectPasswordSignIn(ctx context.Context, userID uuid.UUID) error {
	if s.sso == nil {
		return nil
	}
	slug, required, err := s.sso.UserSSORequired(ctx, userID)
	if err != nil {
		return err
	}
	if required {
		return &SSORequiredError{WorkspaceSlug: slug}
	}
	return nil
}

// IssueSSOSession mints a session that records the workspace whose SSO the
// user signed in through.
func (s *AuthService) IssueSSOSession(ctx context.Context, userID, workspaceID uuid.UUID) (string, string, error) {
	return s.issueSessionFor(ctx, userID, time.Now(), nil, &workspaceID)
}

// issueSession mints the access and refresh tokens of a session and stores the
// refresh token. Every sign-in path and refresh rotation goes through it.
// authenticatedAt is when the user proved who they are (zero when unknown),
// passkeyID the passkey that did so, if any, and ssoWorkspaceID the workspace
// whose SSO minted the session, if any.
func (s *AuthService) issueSession(ctx context.Context, userID uuid.UUID, authenticatedAt time.Time, passkeyID *uuid.UUID) (string, string, error) {
	return s.issueSessionFor(ctx, userID, authenticatedAt, passkeyID, nil)
}

func (s *AuthService) issueSessionFor(ctx context.Context, userID uuid.UUID, authenticatedAt time.Time, passkeyID, ssoWorkspaceID *uuid.UUID) (string, string, error) {
	accessToken, err := jwtpkg.GenerateSessionAccessToken(userID, s.jwtSecret, jwtpkg.Session{
		AuthenticatedAt: authenticatedAt,
		PasskeyID:       passkeyID,
		SSOWorkspaceID:  ssoWorkspaceID,
	})
	if err != nil {
		return "", "", err
	}

	refreshToken, expiresAt, err := jwtpkg.GenerateRefreshToken(userID, s.jwtSecret)
	if err != nil {
		return "", "", err
	}

	rt := &repository.RefreshToken{
		ID:                  uuid.New(),
		UserID:              userID,
		TokenHash:           hashToken(refreshToken),
		ExpiresAt:           expiresAt,
		PasskeyCredentialID: passkeyID,
		SSOWorkspaceID:      ssoWorkspaceID,
	}
	if !authenticatedAt.IsZero() {
		rt.AuthenticatedAt = &authenticatedAt
	}
	if err := s.refreshRepo.Create(ctx, rt); err != nil {
		return "", "", err
	}

	return accessToken, refreshToken, nil
}

func (s *AuthService) Logout(ctx context.Context, refreshToken string) error {
	tokenHash := hashToken(refreshToken)
	return s.refreshRepo.DeleteByHash(ctx, tokenHash)
}

func (s *AuthService) GetUserByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	return s.userRepo.GetByID(ctx, id)
}

func (s *AuthService) UpdateProfile(ctx context.Context, id uuid.UUID, req dto.UpdateProfileRequest) (*domain.User, error) {
	user, err := s.userRepo.GetByID(ctx, id)
	if err != nil || user == nil {
		return nil, err
	}
	if req.Name != nil {
		user.Name = strings.TrimSpace(*req.Name)
	}
	if req.DisplayName != nil {
		user.DisplayName = strings.TrimSpace(*req.DisplayName)
	}
	if req.AvatarURL.Set {
		if req.AvatarURL.Value == nil || strings.TrimSpace(*req.AvatarURL.Value) == "" {
			user.AvatarURL = nil
		} else {
			trimmed := strings.TrimSpace(*req.AvatarURL.Value)
			user.AvatarURL = &trimmed
		}
	}
	if user.DisplayName == "" {
		user.DisplayName = user.Name
	}
	if err := s.userRepo.Update(ctx, user); err != nil {
		return nil, err
	}
	return user, nil
}

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func validatePasswordComplexity(password string) error {
	var hasUpper, hasLower, hasDigit bool
	for _, ch := range password {
		switch {
		case unicode.IsUpper(ch):
			hasUpper = true
		case unicode.IsLower(ch):
			hasLower = true
		case unicode.IsDigit(ch):
			hasDigit = true
		}
	}
	if !hasUpper || !hasLower || !hasDigit {
		return fmt.Errorf("%w", ErrWeakPassword)
	}
	return nil
}
