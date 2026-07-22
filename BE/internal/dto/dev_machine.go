package dto

import (
	"time"

	"github.com/kuayle/kuayle-backend/internal/domain"
)

type DevMachineRepoInput struct {
	Provider string `json:"provider" validate:"required,oneof=github"`
	Owner    string `json:"owner" validate:"required,max=255"`
	Name     string `json:"name" validate:"required,max=255"`
	URL      string `json:"url" validate:"required,url"`
}

type DevMachineServicesInput struct {
	IDE     bool `json:"ide"`
	Browser bool `json:"browser"`
}

type DevMachineAgentInput struct {
	Provider string `json:"provider" validate:"required,oneof=claude-code opencode codex custom"`
	Mode     string `json:"mode" validate:"required,oneof=interactive autonomous"`
	Config   any    `json:"config,omitempty"`
}

type DevMachineEnvVarInput struct {
	Name          string  `json:"name" validate:"required,max=255"`
	Value         string  `json:"value" validate:"required,max=65536"`
	TargetService string  `json:"target_service" validate:"required,oneof=ide agent collector"`
	Provider      *string `json:"provider,omitempty"`
	Secret        *bool   `json:"secret,omitempty"`
	TTLMinutes    *int    `json:"ttl_minutes,omitempty" validate:"omitempty,min=1,max=1440"`
}

type CreateDevMachineRequest struct {
	Name               string                  `json:"name" validate:"omitempty,min=3,max=255"`
	IssueID            *string                 `json:"issue_id,omitempty" validate:"omitempty,uuid"`
	ProjectID          *string                 `json:"project_id,omitempty" validate:"omitempty,uuid"`
	Repo               *DevMachineRepoInput    `json:"repo,omitempty"`
	BaseBranch         string                  `json:"base_branch,omitempty" validate:"omitempty,max=255"`
	WorkingBranch      string                  `json:"working_branch,omitempty" validate:"omitempty,max=255"`
	Size               string                  `json:"size" validate:"required,oneof=small medium large"`
	Services           DevMachineServicesInput `json:"services"`
	Agents             []DevMachineAgentInput  `json:"agents" validate:"max=4,dive"`
	EnvVars            []DevMachineEnvVarInput `json:"env_vars" validate:"max=64,dive"`
	EnvironmentID      *string                 `json:"environment_id,omitempty" validate:"omitempty,uuid"`
	KeepRunning        bool                    `json:"keep_running"`
	EnvironmentBuilder bool                    `json:"environment_builder"`
}

type DevMachineNameAvailabilityResponse struct {
	Name      string `json:"name"`
	Available bool   `json:"available"`
}

type DevMachineScopeSettingRequest struct {
	ScopeType     string  `json:"scope_type" validate:"required,oneof=workspace team project issue"`
	ScopeID       *string `json:"scope_id,omitempty" validate:"omitempty,uuid"`
	GitHubRepoID  *string `json:"github_repo_id,omitempty" validate:"omitempty,uuid"`
	BaseBranch    *string `json:"base_branch,omitempty" validate:"omitempty,max=255"`
	EnvironmentID *string `json:"environment_id,omitempty" validate:"omitempty,uuid"`
}

type CheckoutIssueRequest struct {
	IssueID string `json:"issue_id" validate:"required,uuid"`
}

type UpdateDevMachineRequest struct {
	KeepRunning *bool `json:"keep_running,omitempty"`
}

type BulkDeleteDevMachinesRequest struct {
	MachineIDs []string `json:"machine_ids,omitempty" validate:"omitempty,max=500,dive,uuid"`
	OldOnly    bool     `json:"old_only"`
}

type PurgeDevMachinesRequest struct {
	MachineIDs     []string `json:"machine_ids,omitempty" validate:"omitempty,max=500,dive,uuid"`
	OlderThanDays  int      `json:"older_than_days,omitempty" validate:"omitempty,min=1,max=3650"`
	IncludeFailed  bool     `json:"include_failed"`
	IncludeExpired bool     `json:"include_expired"`
}

type CreateDevMachineEnvironmentRequest struct {
	Name            string `json:"name" validate:"required,min=3,max=255"`
	SourceMachineID string `json:"source_machine_id" validate:"required,uuid"`
}

type CreateTerminalSessionRequest struct {
	Name       string  `json:"name,omitempty" validate:"omitempty,max=128"`
	CheckoutID *string `json:"checkout_id,omitempty" validate:"omitempty,uuid"`
}

type TerminalSessionResponse struct {
	ID                 string     `json:"id"`
	MachineID          string     `json:"machine_id"`
	CheckoutID         *string    `json:"checkout_id,omitempty"`
	Name               string     `json:"name"`
	RuntimeSessionName string     `json:"runtime_session_name"`
	Status             string     `json:"status"`
	CreatedAt          time.Time  `json:"created_at"`
	LastActivityAt     time.Time  `json:"last_activity_at"`
	ClosedAt           *time.Time `json:"closed_at,omitempty"`
}

type TerminalSessionLaunchResponse struct {
	Status    string                   `json:"status"`
	Session   *TerminalSessionResponse `json:"session,omitempty"`
	LaunchURL string                   `json:"launch_url,omitempty"`
	// WebSocketURL is a one-use, frontend-origin-bound gateway URL for ttyd's
	// native /ws protocol. Native clients must speak ttyd framing directly: send
	// ttyd's initial terminal-size JSON message, then ttyd input/resize frames.
	WebSocketURL      string                       `json:"web_socket_url,omitempty"`
	Protocol          string                       `json:"protocol,omitempty"`
	ExpiresAt         time.Time                    `json:"expires_at,omitempty"`
	Operation         *DevMachineOperationResponse `json:"operation,omitempty"`
	RetryAfterSeconds int                          `json:"retry_after_seconds,omitempty"`
}

type DevMachineListParams struct {
	PaginationParams
	Status  string `query:"status"`
	IssueID string `query:"issue_id"`
}

type DevMachineOperationResponse struct {
	ID             string     `json:"id"`
	Action         string     `json:"action"`
	Status         string     `json:"status"`
	Generation     int64      `json:"generation"`
	IdempotencyKey string     `json:"idempotency_key"`
	Attempts       int        `json:"attempts"`
	ErrorCode      *string    `json:"error_code,omitempty"`
	ErrorMessage   *string    `json:"error_message,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
}

type CreateAgentRunRequest struct {
	CheckoutID         *string  `json:"checkout_id,omitempty" validate:"omitempty,uuid"`
	UseRootWorkspace   bool     `json:"use_root_workspace"`
	Provider           string   `json:"provider" validate:"required,oneof=claude-code opencode codex custom"`
	Mode               string   `json:"mode" validate:"required,oneof=interactive autonomous"`
	Prompt             string   `json:"prompt" validate:"required,min=1,max=100000"`
	AcceptanceCriteria []string `json:"acceptance_criteria"`
	AllowedCommands    []string `json:"allowed_commands"`
	ForbiddenPaths     []string `json:"forbidden_paths"`
	TestCommand        []string `json:"test_command"`
	MaxRuntimeSeconds  int      `json:"max_runtime_seconds" validate:"omitempty,min=30,max=86400"`
	ExtraArgs          []string `json:"extra_args"`
	Config             any      `json:"config,omitempty"`
	AllowedSecrets     []string `json:"allowed_secrets"`
	PushBranch         *bool    `json:"push_branch,omitempty"`
	OpenPullRequest    bool     `json:"open_pull_request"`
}

type AgentProviderResponse struct {
	ID              string   `json:"id"`
	DisplayName     string   `json:"display_name"`
	DefaultImage    string   `json:"default_image"`
	RequiredSecrets []string `json:"required_secrets"`
	SupportedModes  []string `json:"supported_modes"`
	Custom          bool     `json:"custom"`
}

type LaunchServiceResponse struct {
	Status            string                       `json:"status"`
	LaunchURL         string                       `json:"launch_url,omitempty"`
	ExpiresAt         time.Time                    `json:"expires_at,omitempty"`
	Operation         *DevMachineOperationResponse `json:"operation,omitempty"`
	RetryAfterSeconds int                          `json:"retry_after_seconds,omitempty"`
}

type EventListParams struct {
	AfterID int64 `query:"after_id"`
	Limit   int   `query:"limit"`
}

func (p *EventListParams) Defaults() {
	if p.Limit <= 0 || p.Limit > 200 {
		p.Limit = 100
	}
}

type LogListParams struct {
	AfterID int64   `query:"after_id"`
	Limit   int     `query:"limit"`
	RunID   *string `query:"agent_run_id"`
}

func (p *LogListParams) Defaults() {
	if p.Limit <= 0 || p.Limit > 1000 {
		p.Limit = 250
	}
}

type DevMachinePolicyRequest struct {
	Enabled               bool     `json:"enabled"`
	MaxConcurrentMachines int      `json:"max_concurrent_machines" validate:"min=0,max=100"`
	MaxMachinesPerUser    int      `json:"max_machines_per_user" validate:"min=0,max=50"`
	MaxDailyAgentRuns     int      `json:"max_daily_agent_runs" validate:"min=0,max=10000"`
	MaxRuntimeMinutes     int      `json:"max_runtime_minutes" validate:"min=5,max=1440"`
	MaxDiskGB             int      `json:"max_disk_gb" validate:"min=20,max=2048"`
	AllowedProviders      []string `json:"allowed_providers"`
	AllowedRepositories   []string `json:"allowed_repositories"`
	AllowCustomProviders  bool     `json:"allow_custom_providers"`
	IdlePauseMinutes      int      `json:"idle_pause_minutes" validate:"omitempty,min=5,max=10080"`
}

type CollectorEventInput struct {
	AgentRunID *string    `json:"agent_run_id,omitempty" validate:"omitempty,uuid"`
	Source     string     `json:"source" validate:"required,oneof=agent browser collector filesystem git process shell"`
	EventType  string     `json:"event_type" validate:"required,max=128"`
	Payload    any        `json:"payload"`
	OccurredAt *time.Time `json:"occurred_at,omitempty"`
}

type CollectorLogInput struct {
	AgentRunID *string `json:"agent_run_id,omitempty" validate:"omitempty,uuid"`
	ServiceID  *string `json:"service_id,omitempty" validate:"omitempty,uuid"`
	Stream     string  `json:"stream" validate:"required,oneof=stdout stderr pty system"`
	Sequence   int64   `json:"sequence" validate:"min=0"`
	Content    string  `json:"content" validate:"required,max=65536"`
	Truncated  bool    `json:"truncated"`
}

type TraceListParams struct {
	EventsAfterID int64 `query:"events_after_id"`
	EventsLimit   int   `query:"events_limit"`
	LogsAfterID   int64 `query:"logs_after_id"`
	LogsLimit     int   `query:"logs_limit"`
}

func (p *TraceListParams) Defaults() {
	if p.EventsLimit <= 0 || p.EventsLimit > 500 {
		p.EventsLimit = 200
	}
	if p.LogsLimit <= 0 || p.LogsLimit > 2000 {
		p.LogsLimit = 500
	}
}

type AgentRunTraceResponse struct {
	Run           *domain.DevMachineAgentRun      `json:"run"`
	Steps         []domain.DevMachineAgentRunStep `json:"steps"`
	Events        []domain.DevMachineEvent        `json:"events"`
	Logs          []domain.DevMachineLogChunk     `json:"logs"`
	NextEventID   int64                           `json:"next_event_id"`
	NextLogID     int64                           `json:"next_log_id"`
	HasMoreEvents bool                            `json:"has_more_events"`
	HasMoreLogs   bool                            `json:"has_more_logs"`
}
