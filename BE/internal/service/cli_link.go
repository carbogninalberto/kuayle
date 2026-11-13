package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
)

var (
	ErrCLILinkNotFound         = errors.New("connection link not found")
	ErrCLILinkWorkspace        = errors.New("the link's workspace is no longer accessible")
	ErrCLILinkInvalidTokenTime = errors.New("token_expires_at must be in the future")
)

type CLILinkService struct {
	linkRepo    repository.CLILinkRepo
	tokenSvc    *TokenService
	frontendURL string
}

func NewCLILinkService(linkRepo repository.CLILinkRepo, tokenSvc *TokenService, frontendURL string) *CLILinkService {
	return &CLILinkService{linkRepo: linkRepo, tokenSvc: tokenSvc, frontendURL: frontendURL}
}

// Create validates the request, stores only the code hash and returns the
// link that carries the plaintext code in its fragment.
func (s *CLILinkService) Create(ctx context.Context, userID uuid.UUID, req dto.CreateCLILinkRequest) (*domain.CLIConnectionLink, string, error) {
	workspaceSlugs, err := s.tokenSvc.ValidateScopesAndWorkspaces(ctx, userID, req.Scopes, req.WorkspaceSlugs)
	if err != nil {
		return nil, "", err
	}
	if req.TokenExpiresAt != nil && !req.TokenExpiresAt.After(time.Now()) {
		return nil, "", ErrCLILinkInvalidTokenTime
	}

	ttl := req.TTLMinutes
	if ttl == 0 {
		ttl = dto.CLILinkDefaultTTLMinutes
	}
	maxUses := req.MaxUses
	if maxUses == 0 {
		maxUses = 1
	}

	plaintext, hash, prefix, err := domain.GenerateCLILinkCode()
	if err != nil {
		return nil, "", err
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "CLI connection"
	}

	link := &domain.CLIConnectionLink{
		ID:             uuid.New(),
		UserID:         userID,
		Name:           name,
		CodeHash:       hash,
		CodePrefix:     prefix,
		Scopes:         req.Scopes,
		WorkspaceSlugs: workspaceSlugs,
		TokenExpiresAt: req.TokenExpiresAt,
		ExpiresAt:      time.Now().Add(time.Duration(ttl) * time.Minute),
		MaxUses:        maxUses,
	}
	if err := s.linkRepo.Create(ctx, link); err != nil {
		return nil, "", err
	}

	linkURL, err := s.linkURL(plaintext, workspaceSlugs)
	if err != nil {
		return nil, "", err
	}
	return link, linkURL, nil
}

func (s *CLILinkService) linkURL(code string, workspaceSlugs []string) (string, error) {
	base, err := url.Parse(s.frontendURL)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return "", fmt.Errorf("FRONTEND_URL is not an absolute URL")
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/cli/connect"
	base.RawQuery = ""
	fragment := url.Values{}
	fragment.Set("code", code)
	if len(workspaceSlugs) > 0 {
		fragment.Set("workspace", workspaceSlugs[0])
	}
	base.Fragment = fragment.Encode()
	return base.String(), nil
}

func (s *CLILinkService) List(ctx context.Context, userID uuid.UUID) ([]domain.CLIConnectionLink, error) {
	return s.linkRepo.ListActiveByUser(ctx, userID)
}

func (s *CLILinkService) Revoke(ctx context.Context, id, userID uuid.UUID) error {
	if err := s.linkRepo.Revoke(ctx, id, userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrCLILinkNotFound
		}
		return err
	}
	return nil
}

// Redeem exchanges a connection-link code for a freshly minted personal
// access token. The link's use is consumed and the token is inserted in one
// transaction; a lost workspace membership mints nothing.
func (s *CLILinkService) Redeem(ctx context.Context, code, clientName string) (*domain.PersonalAccessToken, string, error) {
	if !strings.HasPrefix(code, domain.CLILinkPrefix) {
		return nil, "", repository.ErrCLILinkInvalid
	}
	link, err := s.linkRepo.GetByHash(ctx, domain.HashToken(code))
	if err != nil {
		return nil, "", err
	}
	if link == nil {
		return nil, "", repository.ErrCLILinkInvalid
	}
	switch {
	case link.RevokedAt != nil:
		return nil, "", repository.ErrCLILinkRevoked
	case !time.Now().Before(link.ExpiresAt):
		return nil, "", repository.ErrCLILinkExpired
	case link.Uses >= link.MaxUses:
		return nil, "", repository.ErrCLILinkUsed
	}

	if err := s.validateMembership(ctx, link); err != nil {
		return nil, "", err
	}

	plaintext, hash, prefix, err := domain.GenerateToken()
	if err != nil {
		return nil, "", err
	}
	name := fmt.Sprintf("CLI (link %s)", link.CodePrefix)
	if strings.TrimSpace(clientName) != "" {
		name = fmt.Sprintf("CLI %s (link %s)", strings.TrimSpace(clientName), link.CodePrefix)
	}

	token := &domain.PersonalAccessToken{
		ID:          uuid.New(),
		UserID:      link.UserID,
		Name:        name,
		TokenHash:   hash,
		TokenPrefix: prefix,
	}
	_, err = s.linkRepo.Redeem(ctx, link.CodeHash, func(current *domain.CLIConnectionLink) (*domain.PersonalAccessToken, error) {
		token.Scopes = current.Scopes
		token.WorkspaceSlugs = current.WorkspaceSlugs
		token.ExpiresAt = current.TokenExpiresAt
		token.ConnectionLinkID = &current.ID
		return token, nil
	})
	if err != nil {
		return nil, "", err
	}
	return token, plaintext, nil
}

func (s *CLILinkService) validateMembership(ctx context.Context, link *domain.CLIConnectionLink) error {
	if len(link.WorkspaceSlugs) == 0 {
		return nil
	}
	_, err := s.tokenSvc.ValidateScopesAndWorkspaces(ctx, link.UserID, nil, []string(link.WorkspaceSlugs))
	if err != nil {
		if errors.Is(err, ErrInvalidTokenWorkspace) {
			return ErrCLILinkWorkspace
		}
		return err
	}
	return nil
}
