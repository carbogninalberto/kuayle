package domain

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// AgentTask is the durable business workflow. Native execution attempts remain
// DevMachineAgentRuns; a task can survive replacement of a runtime or a worker.
type AgentTask struct {
	IssueStatusID     *uuid.UUID `json:"-" db:"issue_status_id"`
	IssueUpdatedAt    *time.Time `json:"-" db:"issue_updated_at"`
	FailureCount      int        `json:"failure_count" db:"failure_count"`
	FailurePending    bool       `json:"-" db:"failure_pending"`
	ID                uuid.UUID  `json:"id" db:"id"`
	WorkspaceID       uuid.UUID  `json:"workspace_id" db:"workspace_id"`
	ConversationID    *uuid.UUID `json:"conversation_id" db:"conversation_id"`
	CreatorID         uuid.UUID  `json:"creator_id" db:"creator_id"`
	IssueID           uuid.UUID  `json:"issue_id" db:"issue_id"`
	TeamID            uuid.UUID  `json:"team_id" db:"team_id"`
	RequestID         uuid.UUID  `json:"request_id" db:"request_id"`
	Prompt            string     `json:"prompt" db:"prompt"`
	Provider          string     `json:"provider" db:"provider"`
	Model             string     `json:"model" db:"model"`
	Thinking          string     `json:"thinking" db:"thinking"`
	State             string     `json:"state" db:"state"`
	Reason            string     `json:"reason" db:"reason"`
	Generation        int64      `json:"generation" db:"generation"`
	MachineID         *uuid.UUID `json:"machine_id" db:"machine_id"`
	ReservedMachineID uuid.UUID  `json:"-" db:"reserved_machine_id"`
	CheckoutID        uuid.UUID  `json:"checkout_id" db:"checkout_id"`
	RunID             uuid.UUID  `json:"run_id" db:"run_id"`
	RunCursor         int64      `json:"-" db:"run_cursor"`
	LeaseToken        *uuid.UUID `json:"-" db:"lease_token"`
	LeaseExpiresAt    *time.Time `json:"-" db:"lease_expires_at"`
	NextPollAt        time.Time  `json:"-" db:"next_poll_at"`
	CreatedAt         time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at" db:"updated_at"`

	TestCommand       *json.RawMessage `json:"test_command" db:"test_command"`
	ValidationCommand json.RawMessage  `json:"validation_command" db:"validation_command"`
	// AgentInstructions are the inherited project instructions accepted with
	// this attempt; trusted configuration set by workspace members.
	AgentInstructions string `json:"agent_instructions" db:"agent_instructions"`
}

type AgentTaskRequest struct {
	RequestID uuid.UUID `json:"request_id"`
	Prompt    string    `json:"prompt"`
	Provider  string    `json:"provider"`
	Model     string    `json:"model"`
	Thinking  string    `json:"thinking"`

	TestCommand []string `json:"test_command"`
}

type AgentTaskRetryRequest struct {
	RequestID  uuid.UUID `json:"request_id"`
	Generation int64     `json:"generation"`
	Prompt     string    `json:"prompt"`

	TestCommand []string `json:"test_command"`
}

type AgentTaskTransition struct {
	Sequence  int64      `json:"sequence" db:"sequence"`
	TaskID    uuid.UUID  `json:"task_id" db:"task_id"`
	FromState string     `json:"from_state" db:"from_state"`
	ToState   string     `json:"to_state" db:"to_state"`
	ActorID   *uuid.UUID `json:"actor_id" db:"actor_id"`
	Reason    string     `json:"reason" db:"reason"`
	CreatedAt time.Time  `json:"created_at" db:"created_at"`
}

func ValidAgentTaskTransition(from, to string) bool {
	if from == to {
		return false
	}
	if to == "cancelling" {
		return from == "queued" || from == "preparing" || from == "running" || from == "waiting_input" || from == "validating" || from == "publishing" || from == "awaiting_review" || from == "blocked" || from == "failed"
	}
	if to == "blocked" {
		return from == "queued" || from == "preparing" || from == "running" || from == "waiting_input" || from == "validating" || from == "publishing"
	}
	switch from {
	case "queued":
		return to == "preparing"
	case "preparing":
		return to == "running"
	case "running":
		return to == "waiting_input" || to == "validating"
	case "waiting_input":
		return to == "running"
	case "validating":
		return to == "running" || to == "publishing"
	case "publishing":
		return to == "awaiting_review"
	case "awaiting_review":
		return to == "completed" || to == "queued"
	case "blocked", "failed":
		return to == "queued"
	case "cancelling":
		return to == "cancelled" || to == "failed"
	}
	return false
}

// TaskExecution is internal dispatch authority, never decoded from HTTP. Runtime
// repositories fence it in the same transaction that queues an external effect.
type TaskExecution struct {
	TaskID, LeaseToken uuid.UUID
	Generation         int64
}
type taskExecutionKey struct{}

func WithTaskExecution(ctx context.Context, task AgentTask) context.Context {
	if task.LeaseToken == nil {
		return ctx
	}
	return context.WithValue(ctx, taskExecutionKey{}, TaskExecution{task.ID, *task.LeaseToken, task.Generation})
}
func TaskExecutionFromContext(ctx context.Context) (TaskExecution, bool) {
	v, ok := ctx.Value(taskExecutionKey{}).(TaskExecution)
	return v, ok
}

// AgentTaskActivity references a bounded native log record. High-volume terminal
// text stays in the paginated log store; only actual structured activities are
// included in the conversation replay stream.
type AgentTaskActivity struct {
	TaskID    uuid.UUID                `json:"task_id"`
	RunID     uuid.UUID                `json:"run_id"`
	LogID     int64                    `json:"log_id"`
	Stream    string                   `json:"stream"`
	Truncated bool                     `json:"truncated"`
	Events    []AgentTaskActivityEvent `json:"events"`
}
type AgentTaskActivityEvent struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type AgentTaskPage struct {
	Tasks      []AgentTask `json:"tasks"`
	NextBefore *uuid.UUID  `json:"next_before,omitempty"`
}
type AgentTaskHistory struct {
	Transitions []AgentTaskTransition `json:"transitions"`
	NextAfter   *int64                `json:"next_after,omitempty"`
}
type AgentTaskRuns struct {
	Runs       []DevMachineAgentRun `json:"runs"`
	NextBefore *uuid.UUID           `json:"next_before,omitempty"`
}
type AgentTaskLogs struct {
	Logs      []DevMachineLogChunk `json:"logs"`
	NextAfter *int64               `json:"next_after,omitempty"`
}

// AgentPublication is the manager's view of a queued publication: the step it
// fences and the issue reference that links the pull request to the issue.
type AgentPublication struct {
	Step            DevMachineAgentRunStep
	IssueIdentifier string
	IssueTitle      string
}

// AgentPublicationResult is recorded only after the branch was pushed and the
// pull request exists on GitHub. An empty Commit means nothing to publish.
type AgentPublicationResult struct {
	Branch            string
	Commit            string
	PullRequestNumber int
	PullRequestURL    string
}

// AgentTaskSignal is a GitHub CI or review event for a task attempt's recorded
// pull request. Summary text comes from GitHub and is untrusted.
type AgentTaskSignal struct {
	ID                 uuid.UUID `json:"id" db:"id"`
	TaskID             uuid.UUID `json:"task_id" db:"task_id"`
	RunID              uuid.UUID `json:"run_id" db:"run_id"`
	Kind               string    `json:"kind" db:"kind"`
	ExternalID         string    `json:"external_id" db:"external_id"`
	Outcome            string    `json:"outcome" db:"outcome"`
	Name               string    `json:"name" db:"name"`
	Summary            string    `json:"summary" db:"summary"`
	URL                string    `json:"url" db:"url"`
	Action             string    `json:"action" db:"action"`
	RequestID          uuid.UUID `json:"-" db:"request_id"`
	FollowUpGeneration *int64    `json:"follow_up_generation,omitempty" db:"follow_up_generation"`
	CreatedAt          time.Time `json:"created_at" db:"created_at"`
	UpdatedAt          time.Time `json:"updated_at" db:"updated_at"`
}

// AgentTaskSignalInput is one normalized GitHub event for a pull request.
type AgentTaskSignalInput struct {
	Kind, ExternalID, Outcome, Name, Summary, URL string
	// HeadSHA must match the published commit for CI; reviews leave it empty.
	HeadSHA string
	// Trusted is false for reviews by authors without write association.
	Trusted bool
}

// AgentTaskFollowUp is a follow-up attempt the caller must request.
type AgentTaskFollowUp struct {
	Signal AgentTaskSignal
	Task   AgentTask
}

// AgentHandoff summarizes the previous attempt for its successor. Native
// harness sessions are not restored across attempts, so a successor receives
// this explicit handoff instead of hidden context.
type AgentHandoff struct {
	AttemptID          uuid.UUID
	MachineID          uuid.UUID
	NativeSessionID    string
	Status             string
	Summary            string
	ChangedFiles       []string
	Validation         string
	ValidationExitCode *int
	PullRequestURL     string
}

// ValidationArtifact is a file the independent check left in its report
// directories. Content is untrusted and only ever served as a download or
// an inert image.
type ValidationArtifact struct {
	ID          uuid.UUID `json:"id" db:"id"`
	StepID      uuid.UUID `json:"step_id" db:"step_id"`
	Path        string    `json:"path" db:"path"`
	ContentType string    `json:"content_type" db:"content_type"`
	Size        int       `json:"size" db:"size"`
	Data        []byte    `json:"-" db:"data"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
}
