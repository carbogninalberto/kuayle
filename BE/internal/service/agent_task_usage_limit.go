package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	log "github.com/sirupsen/logrus"
)

const (
	usageLimitResumedReason = "Resumed after the usage limit reset"
	usageLimitResumeNowText = "Resumed by request after the usage limit"
)

// pauseForUsageLimit pauses an attempt the provider refused at its usage
// limit. The task blocks with the provider's message until the reset; the
// creator's other queued tasks for this agent wait too.
func (s *AgentTaskService) pauseForUsageLimit(ctx context.Context, task domain.AgentTask, limit domain.UsageLimit) error {
	agentName := agentDisplayNames[task.Provider]
	if agentName == "" {
		agentName = "The agent"
	}
	reason := fmt.Sprintf("Paused: %s usage limit reached. %s", agentName, limit.Message)
	if task.UsageLimitResumes >= domain.MaxUsageLimitAutoResumes {
		reason += " Resume it once the limit resets."
	} else {
		reason += " It resumes automatically after the reset."
	}
	waiting := fmt.Sprintf("Waiting for the %s usage limit to reset", agentName)
	return s.store.PauseForUsageLimit(ctx, task, limit, truncateRunes(reason, 2000), waiting)
}

// resumeUsageLimited starts the next attempt of paused tasks whose reset has
// come. Errors are logged; the next sweep tries again.
func (s *AgentTaskService) resumeUsageLimited(ctx context.Context) {
	sweepCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	resumed, err := s.store.ResumeDueUsageLimits(sweepCtx, usageLimitResumedReason)
	if err != nil && ctx.Err() == nil {
		log.WithError(err).Warn("Could not resume agent tasks paused by a usage limit")
	}
	if resumed > 0 {
		log.WithField("tasks", resumed).Info("Resumed agent tasks after their usage limit reset")
	}
}

// ResumeUsageLimitNow is the creator's "Resume now" on a paused task.
func (s *AgentTaskService) ResumeUsageLimitNow(ctx context.Context, ws, id uuid.UUID) (*domain.AgentTask, error) {
	return s.Repo.ResumeUsageLimitNow(ctx, ws, id, usageLimitResumeNowText)
}

// CancelUsageLimitResume is the creator's "Don't resume": the task stays
// paused until they retry it.
func (s *AgentTaskService) CancelUsageLimitResume(ctx context.Context, ws, id uuid.UUID) (*domain.AgentTask, error) {
	return s.Repo.CancelUsageLimitResume(ctx, ws, id)
}
