package domain

import (
	"encoding/json"
	"github.com/google/uuid"
	"time"
)

type Conversation struct {
	ID          uuid.UUID  `json:"id" db:"id"`
	WorkspaceID uuid.UUID  `json:"workspace_id" db:"workspace_id"`
	CreatorID   uuid.UUID  `json:"creator_id" db:"creator_id"`
	IssueID     *uuid.UUID `json:"issue_id" db:"issue_id"`
	TeamID      *uuid.UUID `json:"team_id" db:"team_id"`
	Title       string     `json:"title" db:"title"`
	CreatedAt   time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at" db:"updated_at"`
	FolderID    *uuid.UUID `json:"folder_id,omitempty" db:"folder_id"`
	// ProjectID is the effective project: a free chat's own tag, or an issue
	// chat's current issue project. It is null unless the project is visible to
	// the owner; Project carries its summary.
	ProjectID *uuid.UUID           `json:"project_id" db:"project_id"`
	Project   *ConversationProject `json:"project" db:"-"`
}

// ConversationProject is the visible project a conversation is tagged with.
type ConversationProject struct {
	ID     uuid.UUID  `json:"id" db:"id"`
	Name   string     `json:"name" db:"name"`
	TeamID *uuid.UUID `json:"team_id" db:"team_id"`
}

// ConversationFilter narrows the owner's list. ProjectID selects chats of one
// visible project; NoProject selects chats without a visible project.
type ConversationFilter struct {
	ProjectID *uuid.UUID
	NoProject bool
}

// ConversationFolder groups its owner's conversations; it is never shared.
type ConversationFolder struct {
	ID          uuid.UUID `json:"id" db:"id"`
	WorkspaceID uuid.UUID `json:"workspace_id" db:"workspace_id"`
	OwnerID     uuid.UUID `json:"-" db:"owner_id"`
	Name        string    `json:"name" db:"name"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}
type ConversationMessage struct {
	ID             uuid.UUID  `json:"id" db:"id"`
	ConversationID uuid.UUID  `json:"conversation_id" db:"conversation_id"`
	Sequence       int64      `json:"sequence" db:"sequence"`
	RequestID      uuid.UUID  `json:"request_id" db:"request_id"`
	ParentID       *uuid.UUID `json:"parent_id" db:"parent_id"`
	Role           string     `json:"role" db:"role"`
	Content        string     `json:"content" db:"content"`
	Status         string     `json:"status" db:"status"`
	Provider       string     `json:"provider" db:"provider"`
	Model          string     `json:"model" db:"model"`
	LeaseToken     *uuid.UUID `json:"-" db:"lease_token"`
	LeaseExpiresAt *time.Time `json:"-" db:"lease_expires_at"`
	CreatedAt      time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at" db:"updated_at"`
	// Attachments are the images of a user message, in order.
	Attachments []ConversationAttachment `json:"attachments,omitempty" db:"-"`
}
type ConversationEvent struct {
	Cursor         int64           `json:"cursor" db:"cursor"`
	ID             uuid.UUID       `json:"id" db:"id"`
	ConversationID uuid.UUID       `json:"conversation_id" db:"conversation_id"`
	Type           string          `json:"type" db:"type"`
	Payload        json.RawMessage `json:"payload" db:"payload"`
	CreatedAt      time.Time       `json:"created_at" db:"created_at"`
}
type ConversationHistory struct {
	Conversation Conversation           `json:"conversation"`
	Issue        *ConversationIssue     `json:"issue,omitempty"`
	Messages     []ConversationMessage  `json:"messages"`
	Tools        []ConversationToolCall `json:"tools"`
	Approvals    []ConversationApproval `json:"approvals"`
	Cursor       int64                  `json:"cursor"`
	HasMore      bool                   `json:"has_more"`
}
type ConversationAttempt struct {
	Conversation Conversation
	Message      ConversationMessage
}

// ConversationIssue is a bounded, freshly authorized snapshot for the assistant.
type ConversationIssue struct {
	ID          uuid.UUID `json:"id" db:"id"`
	Identifier  string    `json:"identifier" db:"identifier_text"`
	Title       string    `json:"title" db:"title"`
	Description string    `json:"description" db:"description"`
	Status      string    `json:"status" db:"status"`
}

// ConversationToolCall is one assistant tool invocation. Input and output are
// bounded, sanitized JSON; output is untrusted data, never instructions.
type ConversationToolCall struct {
	ID             uuid.UUID        `json:"id" db:"id"`
	ConversationID uuid.UUID        `json:"conversation_id" db:"conversation_id"`
	MessageID      uuid.UUID        `json:"message_id" db:"message_id"`
	Sequence       int              `json:"sequence" db:"sequence"`
	CallID         string           `json:"call_id" db:"call_id"`
	Name           string           `json:"name" db:"name"`
	Input          json.RawMessage  `json:"input" db:"input"`
	Status         string           `json:"status" db:"status"`
	Output         *json.RawMessage `json:"output,omitempty" db:"output"`
	Error          *string          `json:"error,omitempty" db:"error"`
	IssueID        *uuid.UUID       `json:"issue_id,omitempty" db:"issue_id"`
	CreatedAt      time.Time        `json:"created_at" db:"created_at"`
	CompletedAt    *time.Time       `json:"completed_at,omitempty" db:"completed_at"`
}

// ConversationApproval is a human decision requested by an assistant tool. The
// backend binds it to one actor, one action and one target; UI state is never
// the authority.
type ConversationApproval struct {
	ID             uuid.UUID       `json:"id" db:"id"`
	ConversationID uuid.UUID       `json:"conversation_id" db:"conversation_id"`
	ToolCallID     uuid.UUID       `json:"tool_call_id" db:"tool_call_id"`
	Action         string          `json:"action" db:"action"`
	ActorID        uuid.UUID       `json:"actor_id" db:"actor_id"`
	IssueID        uuid.UUID       `json:"issue_id" db:"issue_id"`
	Payload        json.RawMessage `json:"payload" db:"payload"`
	Status         string          `json:"status" db:"status"`
	RequestID      uuid.UUID       `json:"-" db:"request_id"`
	TaskID         *uuid.UUID      `json:"task_id,omitempty" db:"task_id"`
	Error          *string         `json:"error,omitempty" db:"error"`
	ExpiresAt      time.Time       `json:"expires_at" db:"expires_at"`
	ResolvedAt     *time.Time      `json:"resolved_at,omitempty" db:"resolved_at"`
	CreatedAt      time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at" db:"updated_at"`
}

// DelegationPayload is the immutable request an approval authorizes.
type DelegationPayload struct {
	Identifier   string `json:"identifier"`
	Title        string `json:"title"`
	Instructions string `json:"instructions"`
}

// MergePayload binds a merge approval to the pull request and the reviewed
// commit shown to the user; a later push invalidates the approval.
type MergePayload struct {
	Identifier     string    `json:"identifier"`
	Title          string    `json:"title"`
	TaskID         uuid.UUID `json:"task_id"`
	Repository     string    `json:"repository"`
	PullRequest    int       `json:"pull_request"`
	PullRequestURL string    `json:"pull_request_url"`
	Commit         string    `json:"commit"`
}

// AcceptPayload binds an acceptance approval to the task attempt and the pull
// request shown to the user. Accepting completes the task in Kuayle and never
// merges; a newer attempt invalidates the approval.
type AcceptPayload struct {
	Identifier     string    `json:"identifier"`
	Title          string    `json:"title"`
	TaskID         uuid.UUID `json:"task_id"`
	Generation     int64     `json:"generation"`
	PullRequest    int       `json:"pull_request"`
	PullRequestURL string    `json:"pull_request_url"`
	Commit         string    `json:"commit"`
}

// ReviewPayload binds a review approval to the task, its published pull
// request and commit, the harness and whether to comment on the pull request.
type ReviewPayload struct {
	Identifier        string    `json:"identifier"`
	Title             string    `json:"title"`
	TaskID            uuid.UUID `json:"task_id"`
	Harness           string    `json:"harness"`
	Focus             string    `json:"focus,omitempty"`
	PostToPullRequest bool      `json:"post_to_pull_request"`
	PullRequest       int       `json:"pull_request"`
	PullRequestURL    string    `json:"pull_request_url"`
	Commit            string    `json:"commit"`
}

// FollowUpPayload binds a follow-up approval to the task attempt the user saw
// and the exact instructions shown to them.
type FollowUpPayload struct {
	Identifier   string    `json:"identifier"`
	Title        string    `json:"title"`
	TaskID       uuid.UUID `json:"task_id"`
	Generation   int64     `json:"generation"`
	Instructions string    `json:"instructions"`
}
