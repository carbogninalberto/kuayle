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

// IsAgentTaskEndState reports whether a task stops in the state: it finished,
// failed or was cancelled. A watch registered without a chosen state
// (watch_agent_work) fires on these and only these.
func IsAgentTaskEndState(state string) bool {
	return state == "completed" || state == "failed" || state == "cancelled"
}

// AgentTaskWatch is one claimed one-shot watch (KUA-165): the transition that
// satisfied it, the user it pings and the conversation it was registered in.
// Until is the watched state, empty for any end state.
type AgentTaskWatch struct {
	TaskID          uuid.UUID `db:"task_id"`
	WorkspaceID     uuid.UUID `db:"workspace_id"`
	UserID          uuid.UUID `db:"user_id"`
	ConversationID  uuid.UUID `db:"conversation_id"`
	ParentMessageID uuid.UUID `db:"parent_message_id"`
	Until           string    `db:"until_state"`
	State           string    `db:"state"`
	IssueID         uuid.UUID `db:"issue_id"`
	IssueIdentifier string    `db:"issue_identifier"`
	IssueTitle      string    `db:"issue_title"`
}
