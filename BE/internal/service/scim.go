package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/mail"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
)

const (
	SCIMUserSchema  = "urn:ietf:params:scim:schemas:core:2.0:User"
	SCIMGroupSchema = "urn:ietf:params:scim:schemas:core:2.0:Group"
	SCIMMessage     = "urn:ietf:params:scim:api:messages:2.0:"
	scimTokenPrefix = "kuayle_scim_"
)

type SCIMError struct {
	Type   string
	Detail string
}

func (e *SCIMError) Error() string { return e.Detail }

type SCIMService struct {
	Repo          *repository.SCIMRepository
	refreshAccess func(uuid.UUID)
}

func NewSCIMService(repo *repository.SCIMRepository, workspace *WorkspaceService) *SCIMService {
	return &SCIMService{Repo: repo, refreshAccess: workspace.refreshAccess}
}

func (s *SCIMService) RotateToken(ctx context.Context, workspaceID uuid.UUID) (string, error) {
	var entropy [32]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return "", err
	}
	token := scimTokenPrefix + base64.RawURLEncoding.EncodeToString(entropy[:])
	if err := s.Repo.RotateToken(ctx, workspaceID, domain.HashToken(token)); err != nil {
		return "", err
	}
	return token, nil
}

func (s *SCIMService) WithToken(ctx context.Context, slug, token string, write bool, fn func(*repository.SCIMTransaction) error) error {
	if !strings.HasPrefix(token, scimTokenPrefix) || len(token) != len(scimTokenPrefix)+43 {
		return repository.ErrSCIMUnauthorized
	}
	var workspaceID uuid.UUID
	err := s.Repo.WithToken(ctx, slug, domain.HashToken(token), write, func(tx *repository.SCIMTransaction) error {
		workspaceID = tx.WorkspaceID()
		return fn(tx)
	})
	if err == nil && write {
		s.refreshAccess(workspaceID)
	}
	return err
}

func (s *SCIMService) MapGroup(ctx context.Context, workspaceID, groupID uuid.UUID, teamID *uuid.UUID) error {
	err := s.Repo.WithWorkspace(ctx, workspaceID, func(tx *repository.SCIMTransaction) error {
		return tx.MapGroup(ctx, groupID, teamID)
	})
	if err == nil {
		s.refreshAccess(workspaceID)
	}
	return err
}

type scimUserInput struct {
	domain.SCIMUserProfile
	Active *scimBoolean `json:"active"`
}

type scimBoolean bool

func (b *scimBoolean) UnmarshalJSON(raw []byte) error {
	switch strings.ToLower(string(raw)) {
	case "true", `"true"`:
		*b = true
	case "false", `"false"`:
		*b = false
	default:
		return errors.New("invalid SCIM boolean")
	}
	return nil
}

func SaveSCIMUser(ctx context.Context, tx *repository.SCIMTransaction, current domain.SCIMUser, body json.RawMessage, patch bool) (domain.SCIMUser, error) {
	updated := current
	var input scimUserInput
	if patch {
		if err := patchSCIMUser(&updated, body); err != nil {
			return updated, err
		}
	} else {
		if err := decodeSCIMResource(body, SCIMUserSchema, &input); err != nil {
			return updated, err
		}
		updated.SCIMUserProfile = input.SCIMUserProfile
		if input.Active != nil {
			updated.Active = bool(*input.Active)
		} else if current.ID == uuid.Nil {
			updated.Active = true
		}
	}
	if err := normalizeSCIMUser(&updated.SCIMUserProfile); err != nil {
		return updated, err
	}
	if current.ID != uuid.Nil {
		accountName := ""
		if name := scimAccountName(updated); name != scimAccountName(current) {
			accountName = name
		}
		return tx.UpdateUser(ctx, updated, current.Active, accountName)
	}
	email := updated.UserName
	for i, address := range updated.Emails {
		if i == 0 || address.Primary {
			email = address.Value
		}
	}
	email = strings.ToLower(email)
	if !scimEmailValid(email) {
		return updated, scimInvalid("an email address is required for a new account")
	}
	passwordHash, err := unusablePasswordHash()
	if err != nil {
		return updated, err
	}
	return tx.CreateUser(ctx, updated, email, scimAccountName(updated), passwordHash)
}

func scimAccountName(user domain.SCIMUser) string {
	name := strings.TrimSpace(user.DisplayName)
	if name == "" {
		name = strings.TrimSpace(user.Name.GivenName + " " + user.Name.FamilyName)
	}
	if name == "" {
		name = user.UserName
	}
	if runes := []rune(name); len(runes) > 100 {
		name = string(runes[:100])
	}
	return name
}

func normalizeSCIMUser(profile *domain.SCIMUserProfile) error {
	profile.UserName = strings.TrimSpace(profile.UserName)
	if profile.UserName == "" || !scimTextValid(profile.UserName) {
		return scimInvalid("userName must contain 1 to 255 characters")
	}
	for _, value := range []string{profile.DisplayName, profile.ExternalID, profile.Name.Formatted,
		profile.Name.FamilyName, profile.Name.GivenName, profile.Name.MiddleName} {
		if !scimTextValid(value) {
			return scimInvalid("profile fields must contain at most 255 characters")
		}
	}
	var primary bool
	for i := range profile.Emails {
		email := &profile.Emails[i]
		email.Value = strings.TrimSpace(email.Value)
		email.Type = strings.ToLower(strings.TrimSpace(email.Type))
		if !scimEmailValid(email.Value) || !scimTextValid(email.Type) {
			return scimInvalid("invalid emails entry")
		}
		if email.Primary && primary {
			return scimInvalid("emails may contain only one primary value")
		}
		primary = primary || email.Primary
	}
	return nil
}

func scimEmailValid(email string) bool {
	address, err := mail.ParseAddress(email)
	return err == nil && address.Address == email && scimTextValid(email)
}

func scimTextValid(value string) bool {
	return !strings.ContainsRune(value, 0) && utf8.RuneCountInString(value) <= 255
}

func SaveSCIMGroup(ctx context.Context, tx *repository.SCIMTransaction, group domain.SCIMGroup, body json.RawMessage, patch bool) (domain.SCIMGroup, error) {
	if patch {
		if err := patchSCIMGroup(&group, body); err != nil {
			return group, err
		}
	} else {
		var input struct {
			DisplayName string                   `json:"displayName"`
			ExternalID  string                   `json:"externalId"`
			Members     []domain.SCIMGroupMember `json:"members"`
		}
		if err := decodeSCIMResource(body, SCIMGroupSchema, &input); err != nil {
			return group, err
		}
		group.DisplayName, group.ExternalID, group.Members = input.DisplayName, input.ExternalID, input.Members
	}
	group.DisplayName = strings.TrimSpace(group.DisplayName)
	if group.DisplayName == "" || !scimTextValid(group.DisplayName) || !scimTextValid(group.ExternalID) {
		return group, scimInvalid("displayName is required and group fields must contain at most 255 characters")
	}
	slices.SortFunc(group.Members, func(a, b domain.SCIMGroupMember) int {
		return bytes.Compare(a.Value[:], b.Value[:])
	})
	group.Members = slices.Compact(group.Members)
	for _, member := range group.Members {
		if member.Value == uuid.Nil {
			return group, scimInvalid("members entries require a user ID in value")
		}
	}
	return tx.SaveGroup(ctx, group)
}

func decodeSCIMResource(body json.RawMessage, schema string, target any) error {
	var envelope struct {
		Schemas []string `json:"schemas"`
	}
	if json.Unmarshal(body, &envelope) != nil || !slices.Contains(envelope.Schemas, schema) {
		return &SCIMError{Type: "invalidSyntax", Detail: "request must include the resource schema"}
	}
	if err := json.Unmarshal(body, target); err != nil {
		return scimInvalid("invalid resource attributes")
	}
	return nil
}

func scimInvalid(detail string) error {
	return &SCIMError{Type: "invalidValue", Detail: detail}
}
