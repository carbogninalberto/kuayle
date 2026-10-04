package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/audit"
)

const defaultInviteLinkExpiryDays = 7

var (
	ErrInviteLinkInvalid   = repository.ErrInviteLinkInvalid
	ErrInviteLinkExpired   = repository.ErrInviteLinkExpired
	ErrInviteLinkRevoked   = repository.ErrInviteLinkRevoked
	ErrInviteLinkExhausted = repository.ErrInviteLinkExhausted
)

type InviteLinkService struct {
	inviteLinkRepo repository.WorkspaceInviteLinkRepo
	workspaceRepo  repository.WorkspaceRepo
}

func NewInviteLinkService(inviteLinkRepo repository.WorkspaceInviteLinkRepo, workspaceRepo repository.WorkspaceRepo) *InviteLinkService {
	return &InviteLinkService{inviteLinkRepo: inviteLinkRepo, workspaceRepo: workspaceRepo}
}

// Create mints a new invite link and returns it together with the raw token.
// The raw token is never persisted — only its SHA-256 hash is stored.
func (s *InviteLinkService) Create(ctx context.Context, workspaceID, creatorID uuid.UUID, req dto.CreateInviteLinkRequest) (*domain.WorkspaceInviteLink, string, error) {
	if req.Role != domain.RoleMember && req.Role != domain.RoleGuest {
		return nil, "", fmt.Errorf("invite link role must be %q or %q", domain.RoleMember, domain.RoleGuest)
	}

	expiresInDays := defaultInviteLinkExpiryDays
	if req.ExpiresInDays != nil {
		expiresInDays = *req.ExpiresInDays
	}

	rawToken, err := generateInviteToken()
	if err != nil {
		return nil, "", err
	}

	link := &domain.WorkspaceInviteLink{
		ID:          uuid.New(),
		WorkspaceID: workspaceID,
		TokenHash:   hashInviteToken(rawToken),
		Role:        req.Role,
		CreatedBy:   creatorID,
		ExpiresAt:   time.Now().Add(time.Duration(expiresInDays) * 24 * time.Hour),
		MaxUses:     req.MaxUses,
	}
	if err := s.inviteLinkRepo.Create(ctx, link); err != nil {
		return nil, "", err
	}

	audit.Log("invite_link.created", creatorID, map[string]interface{}{
		"workspace_id": workspaceID, "invite_link_id": link.ID, "role": link.Role,
		"expires_at": link.ExpiresAt, "max_uses": req.MaxUses,
	})

	return link, rawToken, nil
}

func (s *InviteLinkService) List(ctx context.Context, workspaceID uuid.UUID) ([]domain.WorkspaceInviteLink, error) {
	return s.inviteLinkRepo.ListByWorkspace(ctx, workspaceID)
}

func (s *InviteLinkService) Revoke(ctx context.Context, workspaceID, linkID, actorID uuid.UUID) error {
	link, err := s.inviteLinkRepo.GetByID(ctx, linkID)
	if err != nil {
		return err
	}
	if link == nil || link.WorkspaceID != workspaceID {
		return ErrInviteLinkInvalid
	}

	if err := s.inviteLinkRepo.Revoke(ctx, link.ID); err != nil {
		return err
	}

	audit.Log("invite_link.revoked", actorID, map[string]interface{}{
		"workspace_id": workspaceID, "invite_link_id": link.ID,
	})
	return nil
}

// Validate resolves a raw invite token and checks it is still usable,
// returning the link and its workspace.
func (s *InviteLinkService) Validate(ctx context.Context, token string) (*domain.WorkspaceInviteLink, *domain.Workspace, error) {
	link, err := s.inviteLinkRepo.GetByTokenHash(ctx, hashInviteToken(token))
	if err != nil {
		return nil, nil, err
	}
	if link == nil {
		return nil, nil, ErrInviteLinkInvalid
	}
	if err := checkInviteLinkUsable(link); err != nil {
		return nil, nil, err
	}

	ws, err := s.workspaceRepo.GetByID(ctx, link.WorkspaceID)
	if err != nil {
		return nil, nil, err
	}
	if ws == nil {
		return nil, nil, ErrInviteLinkInvalid
	}
	return link, ws, nil
}

// Preview returns public metadata about an invite token. Unknown tokens yield
// ErrInviteLinkInvalid; unusable links are reported via the Status field.
func (s *InviteLinkService) Preview(ctx context.Context, token string) (*dto.InvitePreviewResponse, error) {
	link, err := s.inviteLinkRepo.GetByTokenHash(ctx, hashInviteToken(token))
	if err != nil {
		return nil, err
	}
	if link == nil {
		return nil, ErrInviteLinkInvalid
	}

	ws, err := s.workspaceRepo.GetByID(ctx, link.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if ws == nil {
		return nil, ErrInviteLinkInvalid
	}

	status := dto.InviteStatusValid
	switch {
	case link.RevokedAt != nil:
		status = dto.InviteStatusRevoked
	case !time.Now().Before(link.ExpiresAt):
		status = dto.InviteStatusExpired
	case link.MaxUses != nil && link.UseCount >= *link.MaxUses:
		status = dto.InviteStatusExhausted
	}

	return &dto.InvitePreviewResponse{
		WorkspaceName:    ws.Name,
		WorkspaceSlug:    ws.Slug,
		WorkspaceLogoURL: ws.LogoURL,
		Role:             link.Role,
		Status:           status,
	}, nil
}

// Accept adds the user to the link's workspace. Accepting while already a
// member is idempotent and does not consume a use.
func (s *InviteLinkService) Accept(ctx context.Context, token string, userID uuid.UUID) (*domain.Workspace, string, error) {
	link, err := s.inviteLinkRepo.GetByTokenHash(ctx, hashInviteToken(token))
	if err != nil {
		return nil, "", err
	}
	if link == nil {
		return nil, "", ErrInviteLinkInvalid
	}
	ws, err := s.workspaceRepo.GetByID(ctx, link.WorkspaceID)
	if err != nil {
		return nil, "", err
	}
	if ws == nil {
		return nil, "", ErrInviteLinkInvalid
	}
	role, err := s.inviteLinkRepo.Join(ctx, link.ID, userID, nil)
	if err != nil {
		return nil, "", err
	}

	audit.Log("invite_link.accepted", userID, map[string]interface{}{
		"workspace_id": ws.ID, "invite_link_id": link.ID, "role": role,
	})

	return ws, role, nil
}

// Register creates an invited user and membership atomically with invite use.
func (s *InviteLinkService) Register(ctx context.Context, token string, user *domain.User) error {
	link, err := s.inviteLinkRepo.GetByTokenHash(ctx, hashInviteToken(token))
	if err != nil {
		return err
	}
	if link == nil {
		return ErrInviteLinkInvalid
	}
	role, err := s.inviteLinkRepo.Join(ctx, link.ID, user.ID, user)
	if err != nil {
		return err
	}
	audit.Log("invite_link.accepted", user.ID, map[string]interface{}{
		"workspace_id": link.WorkspaceID, "invite_link_id": link.ID, "role": role,
	})
	return nil
}

func checkInviteLinkUsable(link *domain.WorkspaceInviteLink) error {
	switch {
	case link.RevokedAt != nil:
		return ErrInviteLinkRevoked
	case !time.Now().Before(link.ExpiresAt):
		return ErrInviteLinkExpired
	case link.MaxUses != nil && link.UseCount >= *link.MaxUses:
		return ErrInviteLinkExhausted
	}
	return nil
}

func generateInviteToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashInviteToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}
