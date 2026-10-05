package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
)

var (
	ErrInvalidTokenScope     = errors.New("invalid token scope")
	ErrInvalidTokenWorkspace = errors.New("invalid or inaccessible workspace")
	ErrInvalidTokenExpiry    = errors.New("expires_at must be in the future")
	ErrInvalidTokenName      = errors.New("name must not be empty")
	ErrTokenNotFound         = errors.New("token not found")
)

type TokenService struct {
	tokenRepo     repository.PersonalAccessTokenRepo
	workspaceRepo repository.WorkspaceRepo
}

func NewTokenService(tokenRepo repository.PersonalAccessTokenRepo, workspaceRepo repository.WorkspaceRepo) *TokenService {
	return &TokenService{tokenRepo: tokenRepo, workspaceRepo: workspaceRepo}
}

// Create validates the request and returns the stored token plus the
// plaintext token, which is only available here.
func (s *TokenService) Create(ctx context.Context, userID uuid.UUID, req dto.CreateTokenRequest) (*domain.PersonalAccessToken, string, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, "", ErrInvalidTokenName
	}

	for _, scope := range req.Scopes {
		if !domain.IsValidScope(scope) {
			return nil, "", fmt.Errorf("%w: %s", ErrInvalidTokenScope, scope)
		}
	}

	// An empty list means "all workspaces" (NULL), same as omitting the field.
	if len(req.WorkspaceSlugs) == 0 {
		req.WorkspaceSlugs = nil
	}

	for _, slug := range req.WorkspaceSlugs {
		ws, err := s.workspaceRepo.GetBySlug(ctx, slug)
		if err != nil {
			return nil, "", err
		}
		if ws == nil {
			return nil, "", fmt.Errorf("%w: %s", ErrInvalidTokenWorkspace, slug)
		}
		member, err := s.workspaceRepo.GetMember(ctx, ws.ID, userID)
		if err != nil {
			return nil, "", err
		}
		if member == nil {
			return nil, "", fmt.Errorf("%w: %s", ErrInvalidTokenWorkspace, slug)
		}
	}

	if req.ExpiresAt != nil && !req.ExpiresAt.After(time.Now()) {
		return nil, "", ErrInvalidTokenExpiry
	}

	plaintext, hash, prefix, err := domain.GenerateToken()
	if err != nil {
		return nil, "", err
	}

	token := &domain.PersonalAccessToken{
		ID:             uuid.New(),
		UserID:         userID,
		Name:           name,
		TokenHash:      hash,
		TokenPrefix:    prefix,
		Scopes:         req.Scopes,
		WorkspaceSlugs: req.WorkspaceSlugs,
		ExpiresAt:      req.ExpiresAt,
	}
	if err := s.tokenRepo.Create(ctx, token); err != nil {
		return nil, "", err
	}
	return token, plaintext, nil
}

func (s *TokenService) List(ctx context.Context, userID uuid.UUID) ([]domain.PersonalAccessToken, error) {
	return s.tokenRepo.ListByUser(ctx, userID)
}

func (s *TokenService) Revoke(ctx context.Context, id, userID uuid.UUID) error {
	if err := s.tokenRepo.Revoke(ctx, id, userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Unknown id or another user's token: deliberately indistinguishable.
			return ErrTokenNotFound
		}
		return err
	}
	return nil
}
