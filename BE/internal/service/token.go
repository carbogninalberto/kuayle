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
	// ErrTokenSSOSessionScoped: an SSO-scoped session may only mint a token
	// or connection link for exactly the workspace it signed in through.
	ErrTokenSSOSessionScoped = errors.New("an SSO session may only create credentials for its own workspace")
	// ErrTokenWorkspaceRequiresSSO matches *TokenWorkspaceRequiresSSOError.
	ErrTokenWorkspaceRequiresSSO = errors.New("workspace requires single sign-on")
)

type TokenService struct {
	tokenRepo     repository.PersonalAccessTokenRepo
	workspaceRepo repository.WorkspaceRepo
	sso           SSORequirement
}

func NewTokenService(tokenRepo repository.PersonalAccessTokenRepo, workspaceRepo repository.WorkspaceRepo) *TokenService {
	return &TokenService{tokenRepo: tokenRepo, workspaceRepo: workspaceRepo}
}

// Create validates the request and returns the stored token plus the
// plaintext token, which is only available here. ssoWorkspaceID is the
// workspace whose SSO sign-in minted the caller session (uuid.Nil for none);
// it is recorded on the token.
func (s *TokenService) Create(ctx context.Context, userID, ssoWorkspaceID uuid.UUID, req dto.CreateTokenRequest) (*domain.PersonalAccessToken, string, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, "", ErrInvalidTokenName
	}

	workspaceSlugs, err := s.ValidateScopesAndWorkspaces(ctx, userID, ssoWorkspaceID, req.Scopes, req.WorkspaceSlugs)
	if err != nil {
		return nil, "", err
	}
	req.WorkspaceSlugs = workspaceSlugs

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
	if ssoWorkspaceID != uuid.Nil {
		token.SSOWorkspaceID = &ssoWorkspaceID
	}
	if err := s.tokenRepo.Create(ctx, token); err != nil {
		return nil, "", err
	}
	return token, plaintext, nil
}

// SSORequirement reports whether a workspace has require-SSO turned on. It is
// implemented by SSOService (and mirrors middleware.SSORequirementSource).
type SSORequirement interface {
	RequiresSSO(ctx context.Context, workspaceID uuid.UUID) (bool, error)
}

// TokenWorkspaceRequiresSSOError refuses a token or connection link that would
// reach a workspace requiring SSO without being minted through that
// workspace's SSO sign-in.
type TokenWorkspaceRequiresSSOError struct {
	Slug string
}

func (e *TokenWorkspaceRequiresSSOError) Error() string {
	return fmt.Sprintf("%s: %s", ErrTokenWorkspaceRequiresSSO, e.Slug)
}

func (e *TokenWorkspaceRequiresSSOError) Is(target error) bool {
	return target == ErrTokenWorkspaceRequiresSSO
}

// SetSSORequirement wires the require-SSO source. Without one no workspace is
// treated as requiring SSO at mint time; the request-time check in
// middleware.WorkspaceMembership still applies.
func (s *TokenService) SetSSORequirement(r SSORequirement) {
	s.sso = r
}

func (s *TokenService) requiresSSO(ctx context.Context, workspaceID uuid.UUID) (bool, error) {
	if s.sso == nil {
		return false, nil
	}
	return s.sso.RequiresSSO(ctx, workspaceID)
}

// ValidateScopesAndWorkspaces is the shared contract for token and
// connection-link creation and redemption: every scope must be a valid token
// scope and every workspace must exist and still contain the user. An empty
// list means "all workspaces" (nil).
//
// ssoWorkspaceID is the workspace whose SSO sign-in minted the caller's
// session (uuid.Nil for a password session). An SSO-scoped session may only
// mint for exactly its own workspace, and a workspace that requires SSO is
// only reachable from a session SSO-scoped to it — owners included, since
// break-glass covers interactive password sign-in only. An all-workspaces
// request is refused when any of the user's workspaces requires SSO.
func (s *TokenService) ValidateScopesAndWorkspaces(ctx context.Context, userID, ssoWorkspaceID uuid.UUID, scopes, workspaceSlugs []string) ([]string, error) {
	for _, scope := range scopes {
		if !domain.IsValidScope(scope) {
			return nil, fmt.Errorf("%w: %s", ErrInvalidTokenScope, scope)
		}
	}

	if ssoWorkspaceID != uuid.Nil && len(workspaceSlugs) != 1 {
		return nil, ErrTokenSSOSessionScoped
	}

	if len(workspaceSlugs) == 0 {
		if s.sso == nil {
			return nil, nil
		}
		workspaces, err := s.workspaceRepo.ListByUser(ctx, userID)
		if err != nil {
			return nil, err
		}
		for _, ws := range workspaces {
			required, err := s.requiresSSO(ctx, ws.ID)
			if err != nil {
				return nil, err
			}
			if required {
				return nil, &TokenWorkspaceRequiresSSOError{Slug: ws.Slug}
			}
		}
		return nil, nil
	}
	for _, slug := range workspaceSlugs {
		ws, err := s.workspaceRepo.GetBySlug(ctx, slug)
		if err != nil {
			return nil, err
		}
		if ssoWorkspaceID != uuid.Nil && (ws == nil || ws.ID != ssoWorkspaceID) {
			return nil, ErrTokenSSOSessionScoped
		}
		if ws == nil {
			return nil, fmt.Errorf("%w: %s", ErrInvalidTokenWorkspace, slug)
		}
		member, err := s.workspaceRepo.GetMember(ctx, ws.ID, userID)
		if err != nil {
			return nil, err
		}
		if member == nil {
			return nil, fmt.Errorf("%w: %s", ErrInvalidTokenWorkspace, slug)
		}
		if ws.ID != ssoWorkspaceID {
			required, err := s.requiresSSO(ctx, ws.ID)
			if err != nil {
				return nil, err
			}
			if required {
				return nil, &TokenWorkspaceRequiresSSOError{Slug: ws.Slug}
			}
		}
	}
	return workspaceSlugs, nil
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
