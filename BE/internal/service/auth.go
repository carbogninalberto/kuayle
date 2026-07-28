package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	jwtpkg "github.com/kuayle/kuayle-backend/pkg/jwt"
	log "github.com/sirupsen/logrus"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials   = errors.New("invalid credentials")
	ErrEmailTaken           = errors.New("email already taken")
	ErrInvalidToken         = errors.New("invalid or expired token")
	ErrWeakPassword         = errors.New("password must contain at least one uppercase letter, one lowercase letter, and one digit")
	ErrRegistrationDisabled = errors.New("public registration is disabled")
)

// InviteRedeemer validates and consumes workspace invite links during
// registration. Implemented by InviteLinkService.
type InviteRedeemer interface {
	Validate(ctx context.Context, token string) (*domain.WorkspaceInviteLink, *domain.Workspace, error)
	Accept(ctx context.Context, token string, userID uuid.UUID) (*domain.Workspace, string, error)
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
}

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

	existing, _ := s.userRepo.GetByEmail(ctx, req.Email)
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

	if err := s.userRepo.Create(ctx, user); err != nil {
		if errors.Is(err, repository.ErrDuplicateEmail) {
			return nil, "", "", ErrEmailTaken
		}
		return nil, "", "", err
	}

	accessToken, err := jwtpkg.GenerateAccessToken(user.ID, s.jwtSecret)
	if err != nil {
		return nil, "", "", err
	}

	refreshToken, expiresAt, err := jwtpkg.GenerateRefreshToken(user.ID, s.jwtSecret)
	if err != nil {
		return nil, "", "", err
	}

	tokenHash := hashToken(refreshToken)
	rt := &repository.RefreshToken{
		ID:        uuid.New(),
		UserID:    user.ID,
		TokenHash: tokenHash,
		ExpiresAt: expiresAt,
	}
	if err := s.refreshRepo.Create(ctx, rt); err != nil {
		return nil, "", "", err
	}

	if inviteToken != "" {
		// The account exists now; a consume failure here (e.g. a use-count
		// race) must not fail the registration itself.
		if _, _, err := s.inviteRedeemer.Accept(ctx, inviteToken, user.ID); err != nil {
			log.WithError(err).WithField("user_id", user.ID).Warn("failed to redeem invite token after registration")
		}
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

	accessToken, err := jwtpkg.GenerateAccessToken(user.ID, s.jwtSecret)
	if err != nil {
		return nil, "", "", err
	}

	refreshToken, expiresAt, err := jwtpkg.GenerateRefreshToken(user.ID, s.jwtSecret)
	if err != nil {
		return nil, "", "", err
	}

	tokenHash := hashToken(refreshToken)
	rt := &repository.RefreshToken{
		ID:        uuid.New(),
		UserID:    user.ID,
		TokenHash: tokenHash,
		ExpiresAt: expiresAt,
	}
	if err := s.refreshRepo.Create(ctx, rt); err != nil {
		return nil, "", "", err
	}

	return user, accessToken, refreshToken, nil
}

func (s *AuthService) RefreshTokens(ctx context.Context, refreshToken string) (string, string, error) {
	claims, err := jwtpkg.ValidateToken(refreshToken, s.jwtSecret)
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

	newAccessToken, err := jwtpkg.GenerateAccessToken(claims.UserID, s.jwtSecret)
	if err != nil {
		return "", "", err
	}

	newRefreshToken, expiresAt, err := jwtpkg.GenerateRefreshToken(claims.UserID, s.jwtSecret)
	if err != nil {
		return "", "", err
	}

	newRT := &repository.RefreshToken{
		ID:        uuid.New(),
		UserID:    rt.UserID,
		TokenHash: hashToken(newRefreshToken),
		ExpiresAt: expiresAt,
	}
	if err := s.refreshRepo.Create(ctx, newRT); err != nil {
		return "", "", err
	}

	return newAccessToken, newRefreshToken, nil
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
