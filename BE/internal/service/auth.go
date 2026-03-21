package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"unicode"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	jwtpkg "github.com/kuayle/kuayle-backend/pkg/jwt"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrEmailTaken         = errors.New("email already taken")
	ErrInvalidToken       = errors.New("invalid or expired token")
	ErrWeakPassword       = errors.New("password must contain at least one uppercase letter, one lowercase letter, and one digit")
)

type AuthService struct {
	userRepo    repository.UserRepo
	refreshRepo repository.RefreshTokenRepo
	jwtSecret   string
}

func NewAuthService(userRepo repository.UserRepo, refreshRepo repository.RefreshTokenRepo, jwtSecret string) *AuthService {
	return &AuthService{userRepo: userRepo, refreshRepo: refreshRepo, jwtSecret: jwtSecret}
}

func (s *AuthService) Register(ctx context.Context, req dto.RegisterRequest) (*domain.User, string, string, error) {
	if err := validatePasswordComplexity(req.Password); err != nil {
		return nil, "", "", err
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
