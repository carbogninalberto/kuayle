package domain

import "github.com/google/uuid"

const (
	NotificationAgentTaskFinished = "agent_task_finished"
	NotificationReviewNeeded      = "review_needed"
)

// AgentTaskNotificationType is the notification a task's creator gets when the
// task enters the state; empty for states that notify nobody.
func AgentTaskNotificationType(state string) string {
	switch state {
	case "completed", "failed":
		return NotificationAgentTaskFinished
	case "awaiting_review":
		return NotificationReviewNeeded
	default:
		return ""
	}
}

// AgentTaskNotice is one recorded transition whose creator is still to be
// notified. State is the state the transition entered, which the task may
// have left since.
type AgentTaskNotice struct {
	TaskID          uuid.UUID `db:"task_id"`
	WorkspaceID     uuid.UUID `db:"workspace_id"`
	CreatorID       uuid.UUID `db:"creator_id"`
	IssueID         uuid.UUID `db:"issue_id"`
	Kind            string    `db:"kind"`
	State           string    `db:"state"`
	IssueIdentifier string    `db:"issue_identifier"`
	IssueTitle      string    `db:"issue_title"`
}
