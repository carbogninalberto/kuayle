package repository

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// An agent can be interrupted for a message whenever it is working, with or
// without a workflow, and never when there is no turn to interrupt.
func TestWhichTasksCanBeInterruptedForAMessage(t *testing.T) {
	version := uuid.New()
	running, waiting := domain.WorkflowStatusRunning, domain.WorkflowStatusWaiting
	afterStep := domain.WorkflowPauseAfterStep
	now := time.Now()
	plain := domain.AgentTask{Kind: domain.AgentTaskKindImplement, State: "running"}
	workflow := domain.AgentTask{Kind: domain.AgentTaskKindImplement, State: "running", WorkflowVersionID: &version, WorkflowStatus: &running}
	for name, task := range map[string]domain.AgentTask{"no workflow": plain, "a workflow": workflow} {
		for _, run := range []string{"queued", "starting", "running", "waiting_input"} {
			require.True(t, CanInterruptForLiveMessage(task, run), "%s, run %s", name, run)
		}
		for _, run := range []string{"", "succeeded", "failed", "timeout", "cancelled"} {
			require.False(t, CanInterruptForLiveMessage(task, run), "%s, run %q: the turn is over", name, run)
		}
		for _, state := range []string{"preparing", "waiting_input"} {
			working := task
			working.State = state
			require.True(t, CanInterruptForLiveMessage(working, "running"), "%s, %s", name, state)
		}
		for _, state := range []string{"queued", "validating", "publishing", "awaiting_review", "blocked", "failed", "cancelling", "cancelled", "completed"} {
			idle := task
			idle.State = state
			require.False(t, CanInterruptForLiveMessage(idle, "running"), "%s, %s", name, state)
		}
		failing := task
		failing.FailurePending = true
		require.False(t, CanInterruptForLiveMessage(failing, "running"), name)
		review := task
		review.Kind = domain.AgentTaskKindReview
		require.False(t, CanInterruptForLiveMessage(review, "running"), name)
	}
	// A pause that waits for the step does not keep the person from
	// interrupting; a workflow that is paused or waiting has no turn.
	pending := workflow
	pending.WorkflowPauseRequest = &afterStep
	require.True(t, CanInterruptForLiveMessage(pending, "running"))
	paused := workflow
	paused.WorkflowPausedAt = &now
	require.False(t, CanInterruptForLiveMessage(paused, "running"))
	idle := workflow
	idle.WorkflowStatus = &waiting
	require.False(t, CanInterruptForLiveMessage(idle, "running"))
}
