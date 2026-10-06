package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/realtime"
	"github.com/kuayle/kuayle-backend/internal/repository"
)

var (
	ErrPrivacyConfirmation = errors.New("confirm the visibility change and privacy limitations")
	ErrTeamNotFound        = errors.New("team not found")
	ErrTeamMemberNotFound  = errors.New("team member not found")
)

type TeamService struct {
	hub            *realtime.Hub
	teamRepo       repository.TeamRepo
	teamStatusRepo repository.TeamStatusRepo
}

func NewTeamService(teamRepo repository.TeamRepo, teamStatusRepo repository.TeamStatusRepo, hubs ...*realtime.Hub) *TeamService {
	var hub *realtime.Hub
	if len(hubs) > 0 {
		hub = hubs[0]
	}
	return &TeamService{teamRepo: teamRepo, teamStatusRepo: teamStatusRepo, hub: hub}
}

func (s *TeamService) Create(ctx context.Context, workspaceID uuid.UUID, creatorID uuid.UUID, req dto.CreateTeamRequest) (*domain.Team, error) {
	if req.IsPrivate && !req.AcknowledgePrivacyLimitations {
		return nil, ErrPrivacyConfirmation
	}
	team := &domain.Team{
		IsPrivate:   req.IsPrivate,
		ID:          uuid.New(),
		WorkspaceID: workspaceID,
		Name:        req.Name,
		Key:         req.Key,
		Description: req.Description,
		Color:       req.Color,
		Icon:        req.Icon,
	}

	// Create default statuses for the new team
	defaultStatuses := []struct {
		Name     string
		Slug     string
		Category domain.StatusCategory
		Position int
	}{
		{"Backlog", "backlog", domain.StatusCategoryBacklog, 0},
		{"Todo", "todo", domain.StatusCategoryUnstarted, 1},
		{"In Progress", "in_progress", domain.StatusCategoryStarted, 2},
		{"In Review", "in_review", domain.StatusCategoryStarted, 3},
		{"Done", "done", domain.StatusCategoryCompleted, 4},
		{"Cancelled", "cancelled", domain.StatusCategoryCancelled, 5},
	}
	statuses := make([]domain.TeamStatus, 0, len(defaultStatuses))
	for _, ds := range defaultStatuses {
		ts := &domain.TeamStatus{
			ID:        uuid.New(),
			TeamID:    team.ID,
			Name:      ds.Name,
			Slug:      ds.Slug,
			Category:  ds.Category,
			Position:  ds.Position,
			IsDefault: true,
		}
		statuses = append(statuses, *ts)
	}
	if atomic, ok := s.teamRepo.(interface {
		CreateWithDefaults(context.Context, *domain.Team, uuid.UUID, []domain.TeamStatus) error
	}); ok {
		if err := atomic.CreateWithDefaults(ctx, team, creatorID, statuses); err != nil {
			return nil, err
		}
	} else {
		// Retained for repository adapters; production uses the atomic path.
		if team.IsPrivate {
			return nil, fmt.Errorf("private team creation requires an atomic repository")
		}
		if err := s.teamRepo.Create(ctx, team); err != nil {
			return nil, err
		}
		if err := s.teamRepo.AddMember(ctx, &domain.TeamMember{TeamID: team.ID, UserID: creatorID}); err != nil {
			return nil, err
		}
		for i := range statuses {
			if err := s.teamStatusRepo.Create(ctx, &statuses[i]); err != nil {
				return nil, err
			}
		}
	}
	s.refresh(team.WorkspaceID)
	return team, nil
}

func (s *TeamService) GetByID(ctx context.Context, id uuid.UUID) (*domain.Team, error) {
	return s.teamRepo.GetByID(ctx, id)
}

func (s *TeamService) ListByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]domain.Team, error) {
	return s.teamRepo.ListByWorkspace(ctx, workspaceID)
}

func (s *TeamService) Update(ctx context.Context, id uuid.UUID, req dto.UpdateTeamRequest) (*domain.Team, error) {
	team, err := s.teamRepo.GetByID(ctx, id)
	if err != nil || team == nil {
		return nil, fmt.Errorf("team not found")
	}

	if req.Name != nil {
		team.Name = *req.Name
	}
	if req.Description != nil {
		team.Description = req.Description
	}
	if req.Color != nil {
		team.Color = req.Color
	}
	if req.Icon != nil {
		team.Icon = req.Icon
	}
	if req.TriageEnabled != nil {
		team.TriageEnabled = *req.TriageEnabled
	}
	if req.ParentAutoCloseEnabled != nil {
		team.ParentAutoCloseEnabled = *req.ParentAutoCloseEnabled
	}
	if req.SubIssueAutoCloseEnabled != nil {
		team.SubIssueAutoCloseEnabled = *req.SubIssueAutoCloseEnabled
	}
	if req.IssueCopyPrompt != nil {
		prompt := strings.TrimSpace(*req.IssueCopyPrompt)
		if prompt == "" {
			team.IssueCopyPrompt = nil
		} else {
			team.IssueCopyPrompt = &prompt
		}
	}

	if err := s.teamRepo.Update(ctx, team); err != nil {
		return nil, err
	}
	s.refresh(team.WorkspaceID)
	return team, nil
}

func (s *TeamService) Delete(ctx context.Context, workspaceID, id uuid.UUID) error {
	team, err := s.teamRepo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if team == nil || team.WorkspaceID != workspaceID {
		return ErrTeamNotFound
	}

	err = s.teamRepo.Delete(ctx, id)
	if err == nil {
		s.refresh(team.WorkspaceID)
	}
	return err
}

func (s *TeamService) Leave(ctx context.Context, workspaceID, teamID, userID uuid.UUID, workspaceRole string) (bool, error) {
	team, err := s.teamRepo.GetByID(ctx, teamID)
	if err != nil {
		return false, err
	}
	if team == nil || team.WorkspaceID != workspaceID {
		return false, ErrTeamNotFound
	}

	if workspaceRole == domain.RoleOwner && !team.IsPrivate {
		return true, s.Delete(ctx, workspaceID, teamID)
	}

	member, err := s.teamRepo.GetMember(ctx, teamID, userID)
	if err != nil {
		return false, err
	}
	if member == nil {
		return false, ErrTeamMemberNotFound
	}
	// Leaving a private team only revokes explicit membership. Administrative
	// visibility remains governed by the workspace role; content is never deleted.
	if team.IsPrivate {
		err = s.teamRepo.RemoveMember(ctx, teamID, userID)
		if err == nil {
			s.refresh(team.WorkspaceID)
		}
		return false, err
	}

	members, err := s.teamRepo.ListMembers(ctx, teamID)
	if err != nil {
		return false, err
	}
	if len(members) <= 1 {
		return true, s.Delete(ctx, workspaceID, teamID)
	}

	err = s.teamRepo.RemoveMember(ctx, teamID, userID)
	if err == nil {
		s.refresh(team.WorkspaceID)
	}
	return false, err
}

func (s *TeamService) SetVisibility(ctx context.Context, teamID uuid.UUID, req dto.SetTeamVisibilityRequest) (*domain.Team, error) {
	team, err := s.teamRepo.GetByID(ctx, teamID)
	if err != nil || team == nil {
		return nil, ErrTeamNotFound
	}
	if team.IsPrivate && !req.IsPrivate && !req.ConfirmPublic {
		return nil, ErrPrivacyConfirmation
	}
	if !team.IsPrivate && req.IsPrivate && !req.AcknowledgePrivacyLimitations {
		return nil, ErrPrivacyConfirmation
	}
	manager, ok := s.teamRepo.(interface {
		SetVisibility(context.Context, uuid.UUID, bool, bool, bool) (*domain.Team, error)
	})
	if !ok {
		return nil, fmt.Errorf("team visibility management is unavailable")
	}
	updated, err := manager.SetVisibility(ctx, teamID, req.IsPrivate, req.ConfirmPublic, req.AcknowledgePrivacyLimitations)
	if err == nil {
		s.refresh(team.WorkspaceID)
	}
	return updated, err
}

func (s *TeamService) Members(ctx context.Context, teamID uuid.UUID) ([]domain.TeamMember, error) {
	team, err := s.teamRepo.GetByID(ctx, teamID)
	if err != nil || team == nil {
		return nil, ErrTeamNotFound
	}
	return s.teamRepo.ListMembers(ctx, teamID)
}

func (s *TeamService) AddMember(ctx context.Context, teamID, userID uuid.UUID) error {
	team, err := s.teamRepo.GetByID(ctx, teamID)
	if err != nil || team == nil {
		return ErrTeamNotFound
	}
	err = s.teamRepo.AddMember(ctx, &domain.TeamMember{TeamID: teamID, UserID: userID})
	if err == nil {
		s.refresh(team.WorkspaceID)
	}
	return err
}

func (s *TeamService) RemoveMember(ctx context.Context, teamID, userID uuid.UUID) error {
	team, err := s.teamRepo.GetByID(ctx, teamID)
	if err != nil || team == nil {
		return ErrTeamNotFound
	}
	err = s.teamRepo.RemoveMember(ctx, teamID, userID)
	if err == nil {
		s.refresh(team.WorkspaceID)
	}
	return err
}

func (s *TeamService) refresh(workspaceID uuid.UUID) {
	if s.hub != nil {
		s.hub.Broadcast(workspaceID, realtime.Event{Type: "app.refresh", Payload: map[string]any{"resources": []string{"workspace", "teams", "issues", "projects", "members", "cycles", "views", "favorites", "notifications"}}})
	}
}
