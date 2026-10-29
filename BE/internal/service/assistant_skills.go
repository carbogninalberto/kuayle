package service

import (
	"context"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

const (
	assistantSkillNameRunes        = 100
	assistantSkillDescriptionRunes = 500
	assistantSkillBodyBytes        = 64 << 10
)

var (
	// ErrAssistantSkillsDenied: guests and non-members cannot read skills.
	ErrAssistantSkillsDenied = errors.New("assistant skills access denied")
)

// ErrInvalidAssistantSkill carries a user-safe validation message.
type ErrInvalidAssistantSkill struct{ Message string }

func (e *ErrInvalidAssistantSkill) Error() string { return e.Message }

type assistantSkillStore interface {
	List(ctx context.Context, ws uuid.UUID) ([]domain.AssistantSkill, error)
	Get(ctx context.Context, ws, id uuid.UUID) (*domain.AssistantSkill, error)
	Versions(ctx context.Context, ws, id uuid.UUID) ([]domain.AssistantSkillVersion, error)
	Create(ctx context.Context, ws, actor uuid.UUID, in domain.AssistantSkillInput) (*domain.AssistantSkill, error)
	Update(ctx context.Context, ws, id, actor uuid.UUID, in domain.AssistantSkillInput) (*domain.AssistantSkill, error)
	Restore(ctx context.Context, ws, id, actor uuid.UUID, version int) (*domain.AssistantSkill, error)
	Activate(ctx context.Context, ws, id, actor uuid.UUID, version *int) (*domain.AssistantSkill, error)
	SetEnabled(ctx context.Context, ws, id, actor uuid.UUID, enabled bool) (*domain.AssistantSkill, error)
	Delete(ctx context.Context, ws, id, actor uuid.UUID) error
}

// AssistantSkillService serves Settings → Assistant → Skills. Non-guest
// members read the skills they can see; the repository allows writes only for
// current owners and admins.
type AssistantSkillService struct {
	skills  assistantSkillStore
	members assistantMemberRepo
}

func NewAssistantSkillService(skills assistantSkillStore, members assistantMemberRepo) *AssistantSkillService {
	return &AssistantSkillService{skills: skills, members: members}
}

// AssistantSkillRequest is a skill's content as the API receives it. TeamID is
// read only on create; Status is "draft" or "active".
type AssistantSkillRequest struct {
	TeamID      *uuid.UUID `json:"team_id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Body        string     `json:"body"`
	Status      string     `json:"status"`
}

type AssistantSkillList struct {
	Skills []domain.AssistantSkill `json:"skills"`
	// CanManage: the caller is an owner or admin; the server enforces writes.
	CanManage bool `json:"can_manage"`
}

// validateSkill trims and bounds a skill's content. Names and descriptions are
// single lines because the assistant's system prompt lists them.
func validateSkill(name, description, body, status string) (domain.AssistantSkillInput, error) {
	name, description = strings.TrimSpace(name), strings.TrimSpace(description)
	singleLine := func(value string) bool {
		return !strings.ContainsFunc(value, unicode.IsControl)
	}
	switch {
	case name == "" || utf8.RuneCountInString(name) > assistantSkillNameRunes || !singleLine(name):
		return domain.AssistantSkillInput{}, &ErrInvalidAssistantSkill{"name must be 1 to 100 characters on one line"}
	case description == "" || utf8.RuneCountInString(description) > assistantSkillDescriptionRunes || !singleLine(description):
		return domain.AssistantSkillInput{}, &ErrInvalidAssistantSkill{"description must be 1 to 500 characters on one line"}
	case strings.TrimSpace(body) == "" || len(body) > assistantSkillBodyBytes || !utf8.ValidString(body) || strings.ContainsRune(body, 0):
		return domain.AssistantSkillInput{}, &ErrInvalidAssistantSkill{"body must be 1 byte to 64 KiB of text"}
	case status != "" && status != domain.AssistantSkillDraft && status != domain.AssistantSkillActive:
		return domain.AssistantSkillInput{}, &ErrInvalidAssistantSkill{"status must be draft or active"}
	}
	return domain.AssistantSkillInput{Name: name, Description: description, Body: body, Status: status, AuthorKind: "user"}, nil
}

// access allows current non-guest members and reports whether the caller is
// an owner or admin. Writes re-check the role in the writing transaction.
func (s *AssistantSkillService) access(ctx context.Context, ws uuid.UUID) (uuid.UUID, bool, error) {
	access, ok := domain.AccessFromContext(ctx)
	if !ok || access.WorkspaceID != ws {
		return uuid.Nil, false, ErrAssistantSkillsDenied
	}
	member, err := s.members.GetMember(ctx, ws, access.UserID)
	if err != nil {
		return uuid.Nil, false, err
	}
	if member == nil || member.Role == domain.RoleGuest {
		return uuid.Nil, false, ErrAssistantSkillsDenied
	}
	return access.UserID, member.Role == domain.RoleOwner || member.Role == domain.RoleAdmin, nil
}

func (s *AssistantSkillService) List(ctx context.Context, ws uuid.UUID) (*AssistantSkillList, error) {
	_, manage, err := s.access(ctx, ws)
	if err != nil {
		return nil, err
	}
	skills, err := s.skills.List(ctx, ws)
	if err != nil {
		return nil, err
	}
	return &AssistantSkillList{Skills: skills, CanManage: manage}, nil
}

func (s *AssistantSkillService) Get(ctx context.Context, ws, id uuid.UUID) (*domain.AssistantSkill, error) {
	if _, _, err := s.access(ctx, ws); err != nil {
		return nil, err
	}
	return s.skills.Get(ctx, ws, id)
}

func (s *AssistantSkillService) Versions(ctx context.Context, ws, id uuid.UUID) ([]domain.AssistantSkillVersion, error) {
	if _, _, err := s.access(ctx, ws); err != nil {
		return nil, err
	}
	return s.skills.Versions(ctx, ws, id)
}

// Create adds a skill; it is active unless the request asks for a draft.
func (s *AssistantSkillService) Create(ctx context.Context, ws uuid.UUID, req AssistantSkillRequest) (*domain.AssistantSkill, error) {
	user, _, err := s.access(ctx, ws)
	if err != nil {
		return nil, err
	}
	if req.Status == "" {
		req.Status = domain.AssistantSkillActive
	}
	in, err := validateSkill(req.Name, req.Description, req.Body, req.Status)
	if err != nil {
		return nil, err
	}
	in.TeamID = req.TeamID
	return s.skills.Create(ctx, ws, user, in)
}

// Update saves a new version. Without a status it keeps the skill's own; a
// draft edit of an active skill waits for activation.
func (s *AssistantSkillService) Update(ctx context.Context, ws, id uuid.UUID, req AssistantSkillRequest) (*domain.AssistantSkill, error) {
	user, _, err := s.access(ctx, ws)
	if err != nil {
		return nil, err
	}
	in, err := validateSkill(req.Name, req.Description, req.Body, req.Status)
	if err != nil {
		return nil, err
	}
	return s.skills.Update(ctx, ws, id, user, in)
}

func (s *AssistantSkillService) SetEnabled(ctx context.Context, ws, id uuid.UUID, enabled bool) (*domain.AssistantSkill, error) {
	user, _, err := s.access(ctx, ws)
	if err != nil {
		return nil, err
	}
	return s.skills.SetEnabled(ctx, ws, id, user, enabled)
}

// Activate puts a draft into effect; without a version, the newest draft.
func (s *AssistantSkillService) Activate(ctx context.Context, ws, id uuid.UUID, version *int) (*domain.AssistantSkill, error) {
	user, _, err := s.access(ctx, ws)
	if err != nil {
		return nil, err
	}
	return s.skills.Activate(ctx, ws, id, user, version)
}

// Restore saves an earlier version's content as a new version.
func (s *AssistantSkillService) Restore(ctx context.Context, ws, id uuid.UUID, version int) (*domain.AssistantSkill, error) {
	user, _, err := s.access(ctx, ws)
	if err != nil {
		return nil, err
	}
	return s.skills.Restore(ctx, ws, id, user, version)
}

func (s *AssistantSkillService) Delete(ctx context.Context, ws, id uuid.UUID) error {
	user, _, err := s.access(ctx, ws)
	if err != nil {
		return err
	}
	return s.skills.Delete(ctx, ws, id, user)
}
