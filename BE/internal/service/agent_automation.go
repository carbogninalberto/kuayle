package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/realtime"
	"github.com/kuayle/kuayle-backend/internal/repository"
	log "github.com/sirupsen/logrus"
)

var ErrInvalidAutomation = errors.New("invalid automation")

type automationNotifier interface {
	Create(ctx context.Context, userID, workspaceID uuid.UUID, issueID *uuid.UUID, notifType, title string, sourceTeams ...uuid.UUID) error
}

type automationHub interface {
	BroadcastToUser(workspaceID, userID uuid.UUID, event realtime.Event)
}

// AgentAutomationService runs event-driven and scheduled delegation rules.
// Every start goes through the same path as chat delegation and acts for one
// recorded user under that user's current permissions; the rule itself grants
// nothing.
type AgentAutomationService struct {
	repo          *repository.AgentAutomationRepository
	conversations *repository.ConversationRepository
	tasks         approvalTasks
	notifications automationNotifier
	hub           automationHub
}

func NewAgentAutomationService(repo *repository.AgentAutomationRepository, conversations *repository.ConversationRepository, tasks approvalTasks, notifications automationNotifier, hub automationHub) *AgentAutomationService {
	return &AgentAutomationService{repo: repo, conversations: conversations, tasks: tasks, notifications: notifications, hub: hub}
}

type AutomationInput struct {
	TeamID       uuid.UUID  `json:"team_id"`
	Name         string     `json:"name"`
	LabelID      *uuid.UUID `json:"label_id,omitempty"`
	StatusID     *uuid.UUID `json:"status_id,omitempty"`
	Instructions string     `json:"instructions"`
	Unattended   bool       `json:"unattended"`
	RunHour      *int       `json:"run_hour,omitempty"`
	MaxActive    int        `json:"max_active"`
}

func (s *AgentAutomationService) List(ctx context.Context, ws uuid.UUID) ([]domain.AgentAutomation, error) {
	return s.repo.List(ctx, ws)
}

func (s *AgentAutomationService) Runs(ctx context.Context, ws uuid.UUID) ([]domain.AgentAutomationRunView, error) {
	return s.repo.Runs(ctx, ws, 50)
}

func (s *AgentAutomationService) Create(ctx context.Context, ws, owner uuid.UUID, input AutomationInput) (*domain.AgentAutomation, error) {
	input.Name, input.Instructions = strings.TrimSpace(input.Name), strings.TrimSpace(input.Instructions)
	if input.MaxActive == 0 {
		input.MaxActive = 1
	}
	if input.Name == "" || utf8.RuneCountInString(input.Name) > 100 || input.Instructions == "" || utf8.RuneCountInString(input.Instructions) > 4000 ||
		strings.ContainsRune(input.Instructions, 0) || (input.LabelID == nil && input.StatusID == nil) ||
		(input.RunHour != nil && (*input.RunHour < 0 || *input.RunHour > 23)) || input.MaxActive < 1 || input.MaxActive > 5 {
		return nil, ErrInvalidAutomation
	}
	automation := &domain.AgentAutomation{WorkspaceID: ws, TeamID: input.TeamID, OwnerID: owner, Name: input.Name, LabelID: input.LabelID, StatusID: input.StatusID,
		Instructions: input.Instructions, Unattended: input.Unattended, RunHour: input.RunHour, MaxActive: input.MaxActive}
	if err := s.repo.Create(ctx, automation); err != nil {
		if errors.Is(err, repository.ErrAutomationNotFound) {
			return nil, ErrInvalidAutomation
		}
		return nil, err
	}
	return automation, nil
}

func (s *AgentAutomationService) SetEnabled(ctx context.Context, ws, id uuid.UUID, enabled bool) (*domain.AgentAutomation, error) {
	return s.repo.SetEnabled(ctx, ws, id, enabled)
}

func (s *AgentAutomationService) Delete(ctx context.Context, ws, id uuid.UUID) error {
	return s.repo.Delete(ctx, ws, id)
}

// Decide approves or dismisses a pending run. An approved run starts at once
// as the approver; a transient failure leaves it for the worker to resume.
func (s *AgentAutomationService) Decide(ctx context.Context, ws, id, actor uuid.UUID, admin, approve bool) (*domain.AgentAutomationRunView, error) {
	run, err := s.repo.Decide(ctx, ws, id, actor, admin, approve)
	if err != nil || !approve {
		return run, err
	}
	s.start(ctx, *run)
	return s.repo.Run(ctx, ws, run.ID)
}

// Run claims matching issues and starts approved or unattended runs.
func (s *AgentAutomationService) Run(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.tick(ctx); err != nil {
				log.WithError(err).Warn("Agent automation pass failed")
			}
		}
	}
}

func (s *AgentAutomationService) tick(ctx context.Context) error {
	claimed, err := s.repo.Claim(ctx, 50)
	if err != nil {
		return err
	}
	for _, run := range claimed {
		if run.Status == "pending_approval" {
			s.notifyApproval(ctx, run)
		}
	}
	starting, err := s.repo.Starting(ctx, 20)
	if err != nil {
		return err
	}
	for _, run := range starting {
		s.start(ctx, run)
	}
	return nil
}

func (s *AgentAutomationService) notifyApproval(ctx context.Context, run domain.AgentAutomationRunView) {
	title := fmt.Sprintf("%s wants to start a coding task on %s", run.AutomationName, run.IssueIdentifier)
	if err := s.notifications.Create(ctx, run.OwnerID, run.WorkspaceID, &run.IssueID, "agent_automation_approval", title, run.TeamID); err != nil {
		log.WithError(err).Warn("Failed to notify about an automation approval")
		return
	}
	s.hub.BroadcastToUser(run.WorkspaceID, run.OwnerID, realtime.Event{Type: "notification.created", Payload: map[string]string{"type": "agent_automation_approval"}})
}

// start delegates the issue for the run's actor. The run's request ID makes a
// repeated start (crash, concurrent worker) replay the same task.
func (s *AgentAutomationService) start(ctx context.Context, run domain.AgentAutomationRunView) {
	if run.ActorID == nil {
		return
	}
	access := domain.WithAccess(ctx, domain.Access{WorkspaceID: run.WorkspaceID, UserID: *run.ActorID})
	task, err := delegateIssue(access, s.conversations, s.tasks, run.WorkspaceID, run.IssueID, run.Instructions, run.RequestID)
	if err != nil {
		message, permanent := delegationFailure(err)
		if !permanent {
			log.WithError(err).WithField("automation_run_id", run.ID).Warn("Automation run will be retried")
			return
		}
		err = s.repo.Finish(ctx, run.ID, nil, message)
	} else {
		err = s.repo.Finish(ctx, run.ID, &task.ID, "")
	}
	if err != nil {
		log.WithError(err).WithField("automation_run_id", run.ID).Warn("Failed to record automation run")
	}
}
