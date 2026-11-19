package service

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	log "github.com/sirupsen/logrus"
)

var ErrInvalidAutomation = errors.New("invalid automation")

// AgentAutomationService runs event-driven and scheduled delegation rules.
// Every start goes through the same path as chat delegation and acts for one
// recorded user under that user's current permissions; the rule itself grants
// nothing.
type AgentAutomationService struct {
	repo          *repository.AgentAutomationRepository
	conversations *repository.ConversationRepository
	tasks         approvalTasks
}

func NewAgentAutomationService(repo *repository.AgentAutomationRepository, conversations *repository.ConversationRepository, tasks approvalTasks) *AgentAutomationService {
	return &AgentAutomationService{repo: repo, conversations: conversations, tasks: tasks}
}

type AutomationInput struct {
	TeamID        uuid.UUID  `json:"team_id"`
	Name          string     `json:"name"`
	LabelID       *uuid.UUID `json:"label_id,omitempty"`
	StatusID      *uuid.UUID `json:"status_id,omitempty"`
	Instructions  string     `json:"instructions"`
	Unattended    bool       `json:"unattended"`
	RunHour       *int       `json:"run_hour,omitempty"`
	WorkflowID    *uuid.UUID `json:"workflow_id,omitempty"`
	VersionPolicy string     `json:"version_policy,omitempty"`
	Cron          string     `json:"cron,omitempty"`
	Timezone      string     `json:"timezone,omitempty"`
	MaxActive     int        `json:"max_active"`
}

func (s *AgentAutomationService) List(ctx context.Context, ws uuid.UUID) ([]domain.AgentAutomation, error) {
	return s.repo.List(ctx, ws)
}

func (s *AgentAutomationService) Runs(ctx context.Context, ws uuid.UUID, rule, before *uuid.UUID) ([]domain.AgentAutomationRunView, error) {
	return s.repo.Runs(ctx, ws, rule, before, 50)
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
	automation.WorkflowID = input.WorkflowID
	if input.VersionPolicy != "" && (input.WorkflowID == nil || (input.VersionPolicy != "pin" && input.VersionPolicy != "latest")) {
		return nil, ErrInvalidAutomation
	}
	automation.Cron, automation.Timezone = strings.TrimSpace(input.Cron), input.Timezone
	if automation.Timezone == "" {
		automation.Timezone = "UTC"
	}
	if automation.Cron != "" {
		next, err := domain.NextAutomationCron(automation.Cron, automation.Timezone, time.Now())
		if err != nil || input.RunHour != nil {
			return nil, ErrInvalidAutomation
		}
		automation.NextRunAt = &next
	} else if automation.Timezone != "UTC" {
		return nil, ErrInvalidAutomation
	}
	if err := s.repo.Create(ctx, automation, input.VersionPolicy == "pin"); err != nil {
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
	for ctx.Err() == nil {
		if err := s.tick(ctx); err != nil && ctx.Err() == nil {
			log.WithError(err).Warn("Agent automation pass failed")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *AgentAutomationService) tick(ctx context.Context) error {
	_, err := s.repo.Claim(ctx, 50)
	if err != nil {
		return err
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

// start delegates the issue for the run's actor. The run's request ID makes a
// repeated start (crash, concurrent worker) replay the same task.
func (s *AgentAutomationService) start(ctx context.Context, run domain.AgentAutomationRunView) {
	lease, err := s.repo.LeaseStart(ctx, run.WorkspaceID, run.ID)
	if err != nil {
		log.WithError(err).WithField("automation_run_id", run.ID).Warn("Failed to lease automation run")
		return
	}
	if lease == nil {
		return
	}
	run = *lease
	startCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	access := domain.WithAccess(startCtx, domain.Access{WorkspaceID: run.WorkspaceID, UserID: *run.ActorID})
	workflow := ""
	if run.WorkflowID != nil {
		workflow = run.WorkflowID.String()
	}
	task, err := delegateIssue(access, s.conversations, s.tasks, run.WorkspaceID, run.IssueID, issueDelegation{
		requestID: run.RequestID, instructions: run.Instructions, workflow: workflow, pinnedVersion: run.PinnedVersion, team: &run.TeamID,
	})
	if err != nil {
		message, permanent := delegationFailure(err)
		if !permanent {
			message = "Temporary failure contacting the coding task service"
			log.WithError(err).WithField("automation_run_id", run.ID).Warn(message)
		}
		err = s.repo.Finish(ctx, run, nil, message, !permanent)
	} else {
		err = s.repo.Finish(ctx, run, &task.ID, "", false)
	}
	if err != nil {
		log.WithError(err).WithField("automation_run_id", run.ID).Warn("Failed to record automation run")
	}
}

func (s *AgentAutomationService) Replay(ctx context.Context, ws, id, actor uuid.UUID, admin bool) (*domain.AgentAutomationRunView, error) {
	return s.repo.Replay(ctx, ws, id, actor, admin)
}
