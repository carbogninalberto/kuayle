package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/realtime"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/storage"
	log "github.com/sirupsen/logrus"
)

var (
	ErrPrivacyConfirmation = errors.New("confirm the visibility change and privacy limitations")
	ErrTeamNotFound        = errors.New("team not found")
	ErrTeamMemberNotFound  = errors.New("team member not found")
	ErrInvalidTeamIcon     = errors.New("image icons are set by uploading a file")
	ErrTeamIconUnavailable = errors.New("team icon uploads are not available")
)

type TeamService struct {
	hub            *realtime.Hub
	teamRepo       repository.TeamRepo
	teamStatusRepo repository.TeamStatusRepo
	iconStore      storage.Backend
}

func NewTeamService(teamRepo repository.TeamRepo, teamStatusRepo repository.TeamStatusRepo, hubs ...*realtime.Hub) *TeamService {
	var hub *realtime.Hub
	if len(hubs) > 0 {
		hub = hubs[0]
	}
	return &TeamService{teamRepo: teamRepo, teamStatusRepo: teamStatusRepo, hub: hub}
}

// SetIconStore enables team icon image uploads.
func (s *TeamService) SetIconStore(store storage.Backend) { s.iconStore = store }

func teamIconKey(team uuid.UUID, file string) string {
	return "team-icon-" + team.String() + "-" + file
}

// deleteIconImage removes the stored image behind an icon value, if it is one.
func (s *TeamService) deleteIconImage(ctx context.Context, teamID uuid.UUID, icon *string) {
	file, ok := domain.TeamIconImageFile(icon)
	if !ok || s.iconStore == nil {
		return
	}
	if err := s.iconStore.Delete(ctx, teamIconKey(teamID, file)); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.WithError(err).Warn("failed to delete team icon image")
	}
}

// deleteReplacedIcon removes the image the last Update displaced. The
// repository reports it under the row lock, so concurrent icon changes each
// delete exactly the file they replaced and none is left behind.
func (s *TeamService) deleteReplacedIcon(ctx context.Context, team *domain.Team) {
	replaced := team.ReplacedIcon
	team.ReplacedIcon = nil
	if replaced == nil || (team.Icon != nil && *team.Icon == *replaced) {
		return
	}
	s.deleteIconImage(ctx, team.ID, replaced)
}

// UploadIcon stores an already validated image as the team's icon and removes
// the previous image. ext is one of the TeamIconImageTypes extensions.
func (s *TeamService) UploadIcon(ctx context.Context, workspaceID, id uuid.UUID, src io.Reader, contentType, ext string) (*domain.Team, error) {
	if s.iconStore == nil {
		return nil, ErrTeamIconUnavailable
	}
	team, err := s.teamRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if team == nil || team.WorkspaceID != workspaceID {
		return nil, ErrTeamNotFound
	}
	file := uuid.New().String() + ext
	if _, err := s.iconStore.Put(ctx, teamIconKey(id, file), src, contentType); err != nil {
		return nil, err
	}
	icon := domain.TeamIconImagePrefix + file
	// Only the icon is written, so fields changed while the file was stored stay.
	updated, err := s.teamRepo.SetIcon(ctx, id, icon)
	if err != nil || updated == nil {
		_ = s.iconStore.Delete(ctx, teamIconKey(id, file))
		if err == nil {
			err = ErrTeamNotFound
		}
		return nil, err
	}
	team = updated
	s.deleteReplacedIcon(ctx, team)
	s.refresh(team.WorkspaceID)
	return team, nil
}

// Icon opens a team's stored icon image. The team lookup applies the caller's
// team visibility, and only the file the team currently uses is served.
func (s *TeamService) Icon(ctx context.Context, workspaceID, id uuid.UUID, file string) (io.ReadCloser, error) {
	if s.iconStore == nil || !domain.TeamIconFilePattern.MatchString(file) {
		return nil, ErrTeamNotFound
	}
	team, err := s.teamRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if team == nil || team.WorkspaceID != workspaceID {
		return nil, ErrTeamNotFound
	}
	if current, ok := domain.TeamIconImageFile(team.Icon); !ok || current != file {
		return nil, ErrTeamNotFound
	}
	return s.iconStore.Get(ctx, teamIconKey(id, file))
}

func (s *TeamService) Create(ctx context.Context, workspaceID uuid.UUID, creatorID uuid.UUID, req dto.CreateTeamRequest) (*domain.Team, error) {
	if req.IsPrivate && !req.AcknowledgePrivacyLimitations {
		return nil, ErrPrivacyConfirmation
	}
	if req.Icon != nil && strings.HasPrefix(*req.Icon, domain.TeamIconImagePrefix) {
		return nil, ErrInvalidTeamIcon
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
	// Only an explicit icon change writes the icon column. An image icon only
	// comes from UploadIcon, so resending the current one changes nothing.
	team.IconSet = false
	if req.Icon != nil {
		if strings.HasPrefix(*req.Icon, domain.TeamIconImagePrefix) {
			if team.Icon == nil || *team.Icon != *req.Icon {
				return nil, ErrInvalidTeamIcon
			}
		} else {
			team.Icon = req.Icon
			team.IconSet = true
		}
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
	s.deleteReplacedIcon(ctx, team)
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

	// The icon comes from the delete itself, so an upload that changed it after
	// the read above does not leave its image behind.
	icon, err := s.teamRepo.Delete(ctx, id)
	if err == nil {
		s.deleteIconImage(ctx, id, icon)
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
