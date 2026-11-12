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
	IssueStatusID  *uuid.UUID `json:"-" db:"issue_status_id"`
	IssueUpdatedAt *time.Time `json:"-" db:"issue_updated_at"`
	FailureCount   int        `json:"failure_count" db:"failure_count"`
	FailurePending bool       `json:"-" db:"failure_pending"`
	// FailureClass is the class (a FailureClass) of the failure the task is
	// failing or failed with; Reason is its cause. Nil otherwise.
	FailureClass      *string    `json:"failure_class,omitempty" db:"failure_class"`
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
	ModelServerID     *uuid.UUID `json:"model_server_id,omitempty" db:"model_server_id"`
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
	// ComposeFile is the inherited repository-relative Compose file whose
	// services run beside the independent check; empty means none.
	ComposeFile string `json:"compose_file" db:"compose_file"`
	// PreviewCommand and its preferred PreviewPort start a QA preview when
	// the task reaches review or on request; an empty command means none.
	PreviewCommand json.RawMessage `json:"preview_command" db:"preview_command"`
	PreviewPort    *int            `json:"preview_port,omitempty" db:"preview_port"`
	// PreviewState is nil until a preview was requested. PreviewPortID and
	// PreviewAppPort name the task's app port while it exists.
	PreviewState   *string    `json:"-" db:"preview_state"`
	PreviewError   *string    `json:"-" db:"preview_error"`
	PreviewPortID  *uuid.UUID `json:"-" db:"preview_port_id"`
	PreviewAppPort *int       `json:"-" db:"preview_app_port"`
	// SignInRequired means the attempt stopped because the agent needs the
	// user's own sign-in; capturing it from their terminal continues the task.
	SignInRequired bool `json:"sign_in_required" db:"sign_in_required"`
	// UsageLimitMessage is set while the task is paused (blocked) because its
	// agent hit the provider's usage limit: the provider's own text, with the
	// reset time and when the task resumes on its own (nil: it does not).
	UsageLimitMessage  *string    `json:"usage_limit_message,omitempty" db:"usage_limit_message"`
	UsageLimitResetsAt *time.Time `json:"usage_limit_resets_at,omitempty" db:"usage_limit_resets_at"`
	UsageLimitResumeAt *time.Time `json:"usage_limit_resume_at,omitempty" db:"usage_limit_resume_at"`
	// UsageLimitResumes counts automatic resumes in a row (MaxUsageLimitAutoResumes).
	UsageLimitResumes int `json:"usage_limit_resumes" db:"usage_limit_resumes"`
	// ResumeNativeSession makes the next attempt continue the previous
	// attempt's native session: set when a usage-limited task resumes, or a
	// workflow a person paused while its agent was working.
	ResumeNativeSession bool `json:"-" db:"resume_native_session"`
	// Kind is "implement" or "review". A review task reviews the published
	// pull request of ReviewOfTaskID and never validates or publishes.
	Kind           string     `json:"kind" db:"kind"`
	ReviewOfTaskID *uuid.UUID `json:"review_of_task_id,omitempty" db:"review_of_task_id"`
	// AgentProfileID is the agent identity the task runs as (an assigned or
	// mentioned agent); nil for delegations and workflow tasks.
	AgentProfileID *uuid.UUID `json:"agent_profile_id,omitempty" db:"agent_profile_id"`

	// WorkflowVersionID pins the configurable workflow the task runs; nil
	// runs the built-in Standard flow. The cursor fields are the durable
	// position of the workflow runner (see BE/docs/workflows.md).
	WorkflowVersionID  *uuid.UUID      `json:"workflow_version_id,omitempty" db:"workflow_version_id"`
	WorkflowNode       *string         `json:"workflow_node,omitempty" db:"workflow_node"`
	WorkflowVisits     json.RawMessage `json:"workflow_visits,omitempty" db:"workflow_visits"`
	WorkflowIterations int             `json:"workflow_iterations,omitempty" db:"workflow_iterations"`
	WorkflowStartedAt  *time.Time      `json:"workflow_started_at,omitempty" db:"workflow_started_at"`
	WorkflowStatus     *string         `json:"workflow_status,omitempty" db:"workflow_status"`
	WorkflowWakeAt     *time.Time      `json:"-" db:"workflow_wake_at"`
	WorkflowSequence   int             `json:"-" db:"workflow_sequence"`
	// WorkflowExtensions counts the budgets people granted by continuing.
	WorkflowExtensions int        `json:"workflow_budget_extensions,omitempty" db:"workflow_budget_extensions"`
	WorkflowBudgetFrom *time.Time `json:"workflow_budget_from,omitempty" db:"workflow_budget_from"`
	// WorkflowPausedAt is set while a person's pause holds the workflow: the
	// worker leaves the task alone until it is resumed. WorkflowPauseRequest
	// is a pause that has not taken effect yet (WorkflowPauseNow while Kuayle
	// finishes a check or a publication, WorkflowPauseAfterStep until the
	// current step has an outcome).
	WorkflowPauseRequest *string    `json:"workflow_pause_request,omitempty" db:"workflow_pause_request"`
	WorkflowPausedAt     *time.Time `json:"workflow_paused_at,omitempty" db:"workflow_paused_at"`
	// LiveResumeGeneration is the attempt a person started to deliver a
	// message (see LiveResumed); one more than the generation while the
	// attempt they interrupted is still being stopped (LiveInterrupting).
	LiveResumeGeneration *int64 `json:"-" db:"live_resume_generation"`
}

// RunsWorkflow reports whether the task follows a configurable workflow
// rather than the Standard fixed flow.
func (t AgentTask) RunsWorkflow() bool {
	return t.WorkflowVersionID != nil && t.Kind != AgentTaskKindReview
}

// Cursor returns the task's workflow position.
func (t AgentTask) Cursor() WorkflowCursor {
	cursor := WorkflowCursor{Visits: map[string]int{}, Iterations: t.WorkflowIterations, Extensions: t.WorkflowExtensions}
	if t.WorkflowNode != nil {
		cursor.Node = *t.WorkflowNode
	}
	if len(t.WorkflowVisits) > 0 {
		_ = json.Unmarshal(t.WorkflowVisits, &cursor.Visits)
		if cursor.Visits == nil {
			cursor.Visits = map[string]int{}
		}
	}
	if t.WorkflowStartedAt != nil {
		cursor.StartedAt = *t.WorkflowStartedAt
	}
	if t.WorkflowBudgetFrom != nil {
		cursor.BudgetFrom = *t.WorkflowBudgetFrom
	}
	return cursor
}

// Task preview states.
const (
	AgentTaskPreviewStarting = "starting"
	AgentTaskPreviewRunning  = "running"
	AgentTaskPreviewStopped  = "stopped"
	AgentTaskPreviewFailed   = "failed"
)

// AgentTaskPreview is the API view of a task's preview.
type AgentTaskPreview struct {
	State  string     `json:"state"`
	PortID *uuid.UUID `json:"port_id"`
	Port   *int       `json:"port"`
	Error  *string    `json:"error"`
}

// Preview returns the task's preview, or nil when none was requested.
func (t AgentTask) Preview() *AgentTaskPreview {
	if t.PreviewState == nil {
		return nil
	}
	return &AgentTaskPreview{State: *t.PreviewState, PortID: t.PreviewPortID, Port: t.PreviewAppPort, Error: t.PreviewError}
}

// MarshalJSON adds the derived preview to every task payload.
func (t AgentTask) MarshalJSON() ([]byte, error) {
	type plain AgentTask
	return json.Marshal(struct {
		plain
		Preview *AgentTaskPreview `json:"preview"`
		// Interrupting: the agent is being stopped to deliver a message and
		// continues by itself; the task is not being cancelled.
		Interrupting bool `json:"interrupting,omitempty"`
	}{plain(t), t.Preview(), t.LiveInterrupting()})
}

type AgentTaskRequest struct {
	RequestID uuid.UUID `json:"request_id"`
	Prompt    string    `json:"prompt"`
	Provider  string    `json:"provider"`
	Model     string    `json:"model"`
	Thinking  string    `json:"thinking"`
	// ModelServerID picks a workspace model server; nil uses the default.
	ModelServerID *uuid.UUID `json:"model_server_id,omitempty"`

	TestCommand []string `json:"test_command"`

	// WorkflowID chooses the task's workflow: a workflow's ID, "standard",
	// or omitted for the issue's project, team or workspace default.
	WorkflowID *string `json:"workflow_id,omitempty"`
	// Workflow is the resolved version and WorkflowInstructions the first
	// agent step's profile and step instructions; the service sets them.
	Workflow             *WorkflowVersion `json:"-"`
	WorkflowInstructions string           `json:"-"`
	// AgentProfile makes the task run as that agent identity: the built-in
	// flow with the profile's harness, model, instructions and skills. Only
	// services set it; it is never decoded from a request.
	AgentProfile *AgentProfile `json:"-"`
}

type AgentTaskRetryRequest struct {
	RequestID  uuid.UUID `json:"request_id"`
	Generation int64     `json:"generation"`
	Prompt     string    `json:"prompt"`
	// Note is what the retry adds to the task's notebook: the instruction a
	// person gave with it, or the review or check finding it answers. Only
	// handlers and services set it; it is never decoded from a request.
	Note *NotebookNote `json:"-"`
	// Model, when set, replaces the task's model for the new attempt; Thinking and
	// ModelServerID then replace its thinking level and model server with it.
	Model         string     `json:"model,omitempty"`
	Thinking      string     `json:"-"`
	ModelServerID *uuid.UUID `json:"-"`
	// RequestedModelServerID is the model server the caller picked for Model
	// (an approval card); nil lets the service choose. The retry receipt records
	// it with Model, so a replay with another choice conflicts.
	RequestedModelServerID *uuid.UUID `json:"-"`

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

// ValidAgentTaskKindTransition applies ValidAgentTaskTransition to a task of
// the given kind. A review task finishes when its harness succeeds: it never
// validates, publishes or awaits review, so it can never push.
func ValidAgentTaskKindTransition(kind, from, to string) bool {
	if kind == AgentTaskKindReview {
		switch to {
		case "validating", "publishing", "awaiting_review":
			return false
		case "completed":
			return from == "running"
		}
		if from == "awaiting_review" {
			return false
		}
	}
	return ValidAgentTaskTransition(from, to)
}

const (
	AgentTaskKindImplement = "implement"
	AgentTaskKindReview    = "review"
)

func ValidAgentTaskTransition(from, to string) bool {
	if from == to {
		return false
	}
	if to == "cancelling" {
		return from == "queued" || from == "preparing" || from == "running" || from == "waiting_input" || from == "validating" || from == "publishing" || from == "awaiting_review" || from == "blocked" || from == "failed"
	}
	if to == "blocked" {
		// From cancelling: the runtime of an attempt paused by a usage limit
		// was cleaned up, and the task waits for the reset.
		return from == "queued" || from == "preparing" || from == "running" || from == "waiting_input" || from == "validating" || from == "publishing" || from == "cancelling"
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
		// To queued: an attempt stopped to deliver a person's message
		// continues as a new attempt (AgentTask.LiveInterrupting).
		return to == "cancelled" || to == "failed" || to == "queued"
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
	// The independent check that passed for this attempt, for the PR body.
	CheckArgv     []string
	CheckExitCode *int
	// CheckSource is configured or agent; CheckCommands are every command it
	// ran with its exit code, and AgentSummary is the agent's own, untrusted
	// account of what it chose to test.
	CheckSource   string
	CheckCommands []CheckCommandRun
	AgentSummary  string
	// The workspace's configured commit author, when set.
	AuthorName, AuthorEmail string
	// The last commit Kuayle published on the task branch, if any.
	PublishedCommit string
	// The workspace's GitHub bot identity, for the description's signature.
	Bot *GitHubBotSettings
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
	ID         uuid.UUID `json:"id" db:"id"`
	TaskID     uuid.UUID `json:"task_id" db:"task_id"`
	RunID      uuid.UUID `json:"run_id" db:"run_id"`
	Kind       string    `json:"kind" db:"kind"`
	ExternalID string    `json:"external_id" db:"external_id"`
	Outcome    string    `json:"outcome" db:"outcome"`
	Name       string    `json:"name" db:"name"`
	Summary    string    `json:"summary" db:"summary"`
	URL        string    `json:"url" db:"url"`
	Action     string    `json:"action" db:"action"`
	// FailureClass classifies a failed CI signal (a CIFailure class) and
	// FailureEvidence is what decided it; a runner or network failure has
	// the action "infrastructure" and starts no follow-up.
	FailureClass       *string   `json:"failure_class,omitempty" db:"failure_class"`
	FailureEvidence    string    `json:"failure_evidence,omitempty" db:"failure_evidence"`
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
	// Trusted is true for a changes_requested review whose author GitHub
	// confirmed can push to the repository.
	Trusted bool
	// Failure classifies a failed CI signal; nil for other signals.
	Failure *CIFailure
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
	// ValidationSource says whether the check was configured or the agent's
	// own choice; FailedCommand is its first failing command, if any.
	ValidationSource      string
	FailedCommand         []string
	FailedCommandExitCode *int
	PullRequestURL        string
	// ContinuedInteractively means the user opened this attempt's native
	// session in Claude Code through the task terminal.
	ContinuedInteractively bool
	// OtherProfile means the previous attempt belonged to a workflow step
	// run by another agent profile than the current step's (a planner before
	// a builder): its native session is that agent's conversation, so the
	// current step does not continue it.
	OtherProfile bool
	// NoFinalMessage means the previous attempt's harness exited normally
	// but the agent never concluded, so there is no summary of its work.
	NoFinalMessage bool
	// NotebookProblem is the note that the previous attempt did not update
	// the task notebook (domain.NotebookNotUpdated); empty when it did.
	NotebookProblem string
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
