package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	log "github.com/sirupsen/logrus"
)

func (s *ConversationService) prepareStoppedPublication(ctx context.Context, chat domain.Conversation, arguments string) (uuid.UUID, domain.PublishTaskChangesPayload, error) {
	var payload domain.PublishTaskChangesPayload
	var input struct {
		Identifier string `json:"identifier"`
	}
	if err := decodeToolInput(arguments, &input); err != nil {
		return uuid.Nil, payload, err
	}
	// Check that the caller still belongs to the workspace before looking up
	// the issue, even if the task used to belong to them.
	member, err := s.tools.members.GetMember(ctx, chat.WorkspaceID, chat.CreatorID)
	if err != nil {
		return uuid.Nil, payload, err
	}
	if member == nil || member.Role == domain.RoleGuest {
		return uuid.Nil, payload, toolFailure("%s", repository.ErrStoppedPublicationForbidden)
	}
	ctx = domain.WithAccess(ctx, domain.Access{WorkspaceID: chat.WorkspaceID, UserID: chat.CreatorID})
	issue, err := s.tools.issues.GetByIdentifier(ctx, chat.WorkspaceID, strings.ToUpper(strings.TrimSpace(input.Identifier)))
	if err != nil || issue == nil {
		return uuid.Nil, payload, toolFailure("issue not found")
	}
	target, err := s.tools.work.FollowUpTarget(ctx, chat.WorkspaceID, issue.ID)
	if errors.Is(err, repository.ErrNoMergeTarget) {
		return uuid.Nil, payload, toolFailure("this issue has no coding task")
	}
	if err != nil {
		return uuid.Nil, payload, err
	}
	task, err := s.Repo.StoppedPublicationTarget(ctx, chat.WorkspaceID, chat.ID, issue.ID, target.TaskID)
	if err != nil {
		if message := stoppedPublicationRefusal(err); message != "" {
			err = toolFailure("%s", message)
		}
		return uuid.Nil, payload, err
	}
	return issue.ID, domain.PublishTaskChangesPayload{Identifier: issue.Identifier, Title: issue.Title, TaskID: task.ID, Generation: task.Generation}, nil
}

func stoppedPublicationRefusal(err error) string {
	switch {
	case errors.Is(err, repository.ErrStoppedPublicationForbidden), errors.Is(err, repository.ErrStoppedPublicationState),
		errors.Is(err, repository.ErrStoppedPublicationUnavailable), errors.Is(err, repository.ErrStoppedPublicationBusy):
		return err.Error()
	case errors.Is(err, repository.ErrAgentTaskConflict):
		return "The task has changed. Ask for a new publication approval for the current attempt."
	}
	return ""
}

func (s *AssistantApprovals) publishTaskChanges(ctx context.Context, ws, conversation, approval uuid.UUID) (*domain.ConversationApproval, error) {
	queued, err := s.conversations.QueueStoppedPublication(ctx, ws, conversation, approval)
	if message := stoppedPublicationRefusal(err); message != "" {
		return s.conversations.FinishApproval(ctx, ws, conversation, approval, nil, message)
	}
	return queued, err
}

// RunStoppedPublicationDecisions reports each finished publish_agent operation
// back to the approval card, including an empty checkout or a lost machine.
func (s *AssistantApprovals) RunStoppedPublicationDecisions(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			decisions, err := s.conversations.StoppedPublicationDecisions(ctx)
			if err != nil {
				log.WithError(err).Warn("could not read stopped task publication outcomes")
				continue
			}
			for _, decision := range decisions {
				finished, err := s.conversations.FinishStoppedPublication(ctx, decision)
				if err != nil {
					log.WithError(err).Warn("could not record a stopped task publication outcome")
				} else if finished != nil {
					auditApprovalOutcome(decision.WorkspaceID, finished, finished, true, nil)
				}
			}
		}
	}
}
