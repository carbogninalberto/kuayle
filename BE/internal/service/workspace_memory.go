package service

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/sanitize"
)

// ErrMemoryDenied: guests and non-members cannot read the workspace memory.
var ErrMemoryDenied = errors.New("workspace memory access denied")

// workspaceMemoryStore is the persistence the workspace memory needs.
type workspaceMemoryStore interface {
	MemberRole(ctx context.Context, ws, actor uuid.UUID) (string, error)
	List(ctx context.Context, ws, actor uuid.UUID, repository, state string) ([]domain.Lesson, error)
	Add(ctx context.Context, ws, actor uuid.UUID, repository, text string) (*domain.Lesson, error)
	Approve(ctx context.Context, ws, actor, id uuid.UUID, text *string) (*domain.Lesson, error)
	Edit(ctx context.Context, ws, actor, id uuid.UUID, text *string, share bool) (*domain.Lesson, error)
	Retire(ctx context.Context, ws, actor, id uuid.UUID) (*domain.Lesson, error)
	Drop(ctx context.Context, ws, actor, id uuid.UUID) (*domain.Lesson, error)
	Reorder(ctx context.Context, ws, actor uuid.UUID, repository string, ids []uuid.UUID) ([]domain.Lesson, error)
	Events(ctx context.Context, ws, actor, id uuid.UUID) ([]domain.LessonEvent, error)
	ApprovedTexts(ctx context.Context, ws, team uuid.UUID, repository string) ([]string, error)
	TaskRepository(ctx context.Context, task domain.AgentTask) (string, error)
}

// MachineSecrets returns the secret values a machine's runs may hold: the
// values run output is redacted with.
type MachineSecrets func(ctx context.Context, workspaceID, machineID uuid.UUID) ([]string, error)

// WorkspaceMemoryService is the workspace memory (KUA-134): short lessons per
// repository. Non-guest members read approved lessons, their own hand
// proposals and the proposals of their own tasks, and propose by hand; only
// owners and admins read everything and approve, edit, retire, drop and
// reorder, and every decision is audited. Agents receive approved lessons only,
// through ApprovedLessons.
type WorkspaceMemoryService struct {
	store   workspaceMemoryStore
	secrets MachineSecrets
}

// NewWorkspaceMemoryService takes the lessons' store and, optionally, the
// source of the secret values to redact from lessons placed in a prompt.
func NewWorkspaceMemoryService(store workspaceMemoryStore, secrets MachineSecrets) *WorkspaceMemoryService {
	return &WorkspaceMemoryService{store: store, secrets: secrets}
}

// MachineSecretValues exposes the values a machine's run output is redacted
// with, for the workspace memory.
func (s *DevMachineService) MachineSecretValues(ctx context.Context, workspaceID, machineID uuid.UUID) ([]string, error) {
	return s.machineSecretValues(ctx, workspaceID, machineID)
}

func (s *WorkspaceMemoryService) access(ctx context.Context, ws uuid.UUID) (uuid.UUID, bool, error) {
	access, ok := domain.AccessFromContext(ctx)
	if !ok || access.WorkspaceID != ws || access.UserID == uuid.Nil {
		return uuid.Nil, false, ErrMemoryDenied
	}
	role, err := s.store.MemberRole(ctx, ws, access.UserID)
	if err != nil {
		return uuid.Nil, false, err
	}
	if role == "" || role == domain.RoleGuest {
		return uuid.Nil, false, ErrMemoryDenied
	}
	return access.UserID, role == domain.RoleOwner || role == domain.RoleAdmin, nil
}

func (s *WorkspaceMemoryService) manager(ctx context.Context, ws uuid.UUID) (uuid.UUID, error) {
	actor, canManage, err := s.access(ctx, ws)
	if err != nil {
		return uuid.Nil, err
	}
	if !canManage {
		return uuid.Nil, repository.ErrLessonDenied
	}
	return actor, nil
}

// LessonLimits are the bounds of the workspace memory, for the form.
type LessonLimits struct {
	LessonCharacters      int `json:"lesson_characters"`
	ApprovedPerRepository int `json:"approved_per_repository"`
	PromptBytes           int `json:"prompt_bytes"`
}

// LessonList is the workspace memory as the reader may see it.
type LessonList struct {
	Lessons   []domain.Lesson `json:"lessons"`
	CanManage bool            `json:"can_manage"`
	Limits    LessonLimits    `json:"limits"`
}

func validLessonState(state string) bool {
	switch state {
	case "", domain.LessonProposed, domain.LessonApproved, domain.LessonRetired, domain.LessonDropped:
		return true
	}
	return false
}

// List returns the lessons the caller can see, of one repository or all, in
// one state or all.
func (s *WorkspaceMemoryService) List(ctx context.Context, ws uuid.UUID, repo, state string) (*LessonList, error) {
	actor, canManage, err := s.access(ctx, ws)
	if err != nil {
		return nil, err
	}
	if !validLessonState(state) {
		return nil, repository.ErrLessonState
	}
	lessons, err := s.store.List(ctx, ws, actor, repo, state)
	if err != nil {
		return nil, err
	}
	return &LessonList{Lessons: lessons, CanManage: canManage, Limits: LessonLimits{LessonCharacters: domain.LessonMaxRunes, ApprovedPerRepository: domain.LessonsApprovedPerRepository, PromptBytes: domain.LessonsPromptMaxBytes}}, nil
}

// Add records a lesson the caller wrote: approved at once for an owner or
// admin, a proposal for any other member.
func (s *WorkspaceMemoryService) Add(ctx context.Context, ws uuid.UUID, repo, text string) (*domain.Lesson, error) {
	actor, _, err := s.access(ctx, ws)
	if err != nil {
		return nil, err
	}
	return s.store.Add(ctx, ws, actor, repo, text)
}

func (s *WorkspaceMemoryService) Approve(ctx context.Context, ws, id uuid.UUID, text *string) (*domain.Lesson, error) {
	actor, err := s.manager(ctx, ws)
	if err != nil {
		return nil, err
	}
	return s.store.Approve(ctx, ws, actor, id, text)
}

func (s *WorkspaceMemoryService) Edit(ctx context.Context, ws, id uuid.UUID, text *string, share bool) (*domain.Lesson, error) {
	actor, err := s.manager(ctx, ws)
	if err != nil {
		return nil, err
	}
	return s.store.Edit(ctx, ws, actor, id, text, share)
}

func (s *WorkspaceMemoryService) Retire(ctx context.Context, ws, id uuid.UUID) (*domain.Lesson, error) {
	actor, err := s.manager(ctx, ws)
	if err != nil {
		return nil, err
	}
	return s.store.Retire(ctx, ws, actor, id)
}

func (s *WorkspaceMemoryService) Drop(ctx context.Context, ws, id uuid.UUID) (*domain.Lesson, error) {
	actor, err := s.manager(ctx, ws)
	if err != nil {
		return nil, err
	}
	return s.store.Drop(ctx, ws, actor, id)
}

func (s *WorkspaceMemoryService) Reorder(ctx context.Context, ws uuid.UUID, repo string, ids []uuid.UUID) ([]domain.Lesson, error) {
	actor, err := s.manager(ctx, ws)
	if err != nil {
		return nil, err
	}
	return s.store.Reorder(ctx, ws, actor, repo, ids)
}

func (s *WorkspaceMemoryService) Audit(ctx context.Context, ws, id uuid.UUID) ([]domain.LessonEvent, error) {
	actor, err := s.manager(ctx, ws)
	if err != nil {
		return nil, err
	}
	return s.store.Events(ctx, ws, actor, id)
}

// ApprovedLessons returns the approved lessons of the repository the task
// works on, ready to be placed in its prompt: a short heading and one line
// per lesson, in the order people gave them, at most
// domain.LessonsPromptMaxBytes bytes, with secrets redacted. It returns ""
// when the task has no checkout yet or the repository has no approved lesson.
// A proposed, dropped or retired lesson is never part of it, and a private
// team's lesson reaches only that team's tasks. The text is what a workspace
// owner or admin approved, so it is the workspace's own configuration, like
// the project's agent instructions.
func (s *WorkspaceMemoryService) ApprovedLessons(ctx context.Context, task domain.AgentTask) (string, error) {
	repo, err := s.store.TaskRepository(ctx, task)
	if err != nil || repo == "" {
		return "", err
	}
	machine := task.ReservedMachineID
	if task.MachineID != nil {
		machine = *task.MachineID
	}
	return s.ApprovedLessonsForRepository(ctx, task.WorkspaceID, task.TeamID, repo, machine)
}

// ApprovedLessonsForRepository is ApprovedLessons for a caller that already
// knows the repository ("owner/name") and the team of the task; machineID
// names the machine whose secret values are redacted (uuid.Nil: none known).
func (s *WorkspaceMemoryService) ApprovedLessonsForRepository(ctx context.Context, workspaceID, teamID uuid.UUID, repo string, machineID uuid.UUID) (string, error) {
	texts, err := s.store.ApprovedTexts(ctx, workspaceID, teamID, repo)
	if err != nil || len(texts) == 0 {
		return "", err
	}
	var secrets []string
	if s.secrets != nil && machineID != uuid.Nil {
		if secrets, err = s.secrets(ctx, workspaceID, machineID); err != nil {
			return "", err
		}
	}
	replacer := sanitize.SecretReplacer(secrets)
	for index, text := range texts {
		texts[index] = sanitize.RedactCredentials(replacer.Replace(text))
	}
	return domain.FormatLessons(texts), nil
}
