package dto

import (
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/agent"
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
	Provider string `json:"provider" validate:"required,oneof=claude-code opencode codex qwen-code crush kimi goose custom"`
	Mode     string `json:"mode" validate:"required,oneof=interactive autonomous"`
	Config   any    `json:"config,omitempty"`
}

type DevMachineEnvVarInput struct {
	Name          string  `json:"name" validate:"required,max=255"`
	Value         string  `json:"value" validate:"required,max=65536"`
	TargetService string  `json:"target_service" validate:"required,oneof=ide terminal agent collector"`
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
	Size               string                  `json:"size" validate:"required,oneof=small medium large custom"`
	CPUMillis          int                     `json:"cpu_millis"`
	MemoryMB           int                     `json:"memory_mb"`
	DiskGB             int                     `json:"disk_gb"`
	Services           DevMachineServicesInput `json:"services"`
	Agents             []DevMachineAgentInput  `json:"agents" validate:"max=4,dive"`
	EnvVars            []DevMachineEnvVarInput `json:"env_vars" validate:"max=64,dive"`
	EnvironmentID      *string                 `json:"environment_id,omitempty" validate:"omitempty,uuid"`
	EnvironmentBuilder bool                    `json:"environment_builder"`
	Profile            string                  `json:"profile,omitempty" validate:"omitempty,oneof=standard headless"`
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

	TestCommand []string `json:"test_command" validate:"omitempty,max=64,dive,max=8192"`

	AgentProvider     *string `json:"agent_provider,omitempty" validate:"omitempty,max=64"`
	AgentInstructions *string `json:"agent_instructions,omitempty"`
	ComposeFile       *string `json:"compose_file,omitempty"`

	PreviewCommand []string `json:"preview_command" validate:"omitempty,max=64,dive,max=8192"`
	PreviewPort    *int     `json:"preview_port,omitempty"`
}

type CheckoutIssueRequest struct {
	IssueID string `json:"issue_id" validate:"required,uuid"`
}

type UpdateDevMachineRequest struct {
	// CPUMillis, MemoryMB and DiskGB resize the machine; an omitted value
	// keeps the current one. The manager applies the new size live.
	CPUMillis *int `json:"cpu_millis,omitempty"`
	MemoryMB  *int `json:"memory_mb,omitempty"`
	DiskGB    *int `json:"disk_gb,omitempty"`
}

// Resizes reports whether the request changes the machine size.
func (r UpdateDevMachineRequest) Resizes() bool {
	return r.CPUMillis != nil || r.MemoryMB != nil || r.DiskGB != nil
}

type BulkDeleteDevMachinesRequest struct {
	MachineIDs []string `json:"machine_ids,omitempty" validate:"omitempty,max=500,dive,uuid"`
}

type BulkDeleteDevMachineResult struct {
	MachineID string `json:"machine_id"`
	Status    string `json:"status"`
	ErrorCode string `json:"error_code,omitempty"`
}

type BulkDeleteDevMachinesResponse struct {
	Count     int                          `json:"count"`
	Requested int                          `json:"requested"`
	Results   []BulkDeleteDevMachineResult `json:"results"`
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
	Provider           string   `json:"provider" validate:"required,oneof=claude-code opencode codex qwen-code crush kimi goose custom"`
	Mode               string   `json:"mode" validate:"required,oneof=interactive autonomous"`
	Prompt             string   `json:"prompt" validate:"required,min=1,max=100000"`
	AcceptanceCriteria []string `json:"acceptance_criteria"`
	AllowedCommands    []string `json:"allowed_commands"`
	ForbiddenPaths     []string `json:"forbidden_paths"`
	TestCommand        []string `json:"test_command"`
	// Session is set only by Kuayle's task runtime; HTTP input cannot carry it.
	Session           *agent.SessionInput `json:"-"`
	MaxRuntimeSeconds int                 `json:"max_runtime_seconds" validate:"omitempty,min=30,max=86400"`
	ExtraArgs         []string            `json:"extra_args"`
	Config            any                 `json:"config,omitempty"`
	AllowedSecrets    []string            `json:"allowed_secrets"`
	PushBranch        *bool               `json:"push_branch,omitempty"`
	// ModelServerID picks a workspace model server; nil uses the default one.
	ModelServerID *uuid.UUID `json:"model_server_id,omitempty"`
	Model         string     `json:"model,omitempty" validate:"omitempty,max=128"`
	Thinking      string     `json:"thinking,omitempty" validate:"omitempty,oneof=off low high max"`
}

// SaveModelServerRequest creates or updates a model server. The API key is
// required when creating and kept when omitted on update.
type SaveModelServerRequest struct {
	Name             string   `json:"name" validate:"required,max=64"`
	OpenAIBaseURL    *string  `json:"openai_base_url,omitempty"`
	AnthropicBaseURL *string  `json:"anthropic_base_url,omitempty"`
	APIKey           *string  `json:"api_key,omitempty"`
	Models           []string `json:"models" validate:"max=500"`
	DefaultModel     string   `json:"default_model" validate:"required,max=128"`
	FastModel        string   `json:"fast_model" validate:"max=128"`
	Thinking         string   `json:"thinking" validate:"required,oneof=off low high max"`
	IsDefault        bool     `json:"is_default"`
}

// DiscoverModelsRequest lists a server's models before or after saving it. A
// saved server's stored key is used when api_key is omitted.
type DiscoverModelsRequest struct {
	ServerID         *uuid.UUID `json:"server_id,omitempty"`
	OpenAIBaseURL    *string    `json:"openai_base_url,omitempty"`
	AnthropicBaseURL *string    `json:"anthropic_base_url,omitempty"`
	APIKey           *string    `json:"api_key,omitempty"`
}

type DiscoverModelsResponse struct {
	Models []string `json:"models"`
}

// ModelServerResponse never includes the API key.
type ModelServerResponse struct {
	ID               uuid.UUID `json:"id"`
	Name             string    `json:"name"`
	OpenAIBaseURL    *string   `json:"openai_base_url,omitempty"`
	AnthropicBaseURL *string   `json:"anthropic_base_url,omitempty"`
	Models           []string  `json:"models"`
	DefaultModel     string    `json:"default_model"`
	FastModel        string    `json:"fast_model"`
	Thinking         string    `json:"thinking"`
	IsDefault        bool      `json:"is_default"`
	// Harnesses that can use this server, from the protocols it speaks.
	Harnesses      []string  `json:"harnesses"`
	ThinkingLevels []string  `json:"thinking_levels"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type AgentProviderResponse struct {
	ID              string   `json:"id"`
	DisplayName     string   `json:"display_name"`
	DefaultImage    string   `json:"default_image"`
	RequiredSecrets []string `json:"required_secrets"`
	OptionalSecrets []string `json:"optional_secrets"`
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
	Enabled                    bool     `json:"enabled"`
	MaxConcurrentMachines      int      `json:"max_concurrent_machines" validate:"min=0,max=100"`
	MaxMachinesPerUser         int      `json:"max_machines_per_user" validate:"min=0,max=50"`
	MaxDailyAgentRuns          int      `json:"max_daily_agent_runs" validate:"min=0,max=10000"`
	MaxDiskGB                  int      `json:"max_disk_gb" validate:"min=20,max=2048"`
	AllowedProviders           []string `json:"allowed_providers" validate:"omitempty,max=7,dive,oneof=claude-code opencode codex qwen-code crush kimi goose"`
	AllowedRepositories        []string `json:"allowed_repositories"`
	AllowCustomProviders       bool     `json:"allow_custom_providers"`
	AllowUnattendedAutomations bool     `json:"allow_unattended_automations"`
	// AllowDocker is optional: omitted, the current choice is kept.
	AllowDocker *bool `json:"allow_docker"`
	// AllowSudo is optional: omitted, the current choice is kept.
	AllowSudo     *bool  `json:"allow_sudo"`
	AgentGitName  string `json:"agent_git_name" validate:"omitempty,max=100"`
	AgentGitEmail string `json:"agent_git_email" validate:"omitempty,max=254"`
	// Task machine resources are optional: omitted, the current values are
	// kept. The service applies the custom machine size rules to them.
	TaskMachineCPUMillis *int `json:"task_machine_cpu_millis,omitempty"`
	TaskMachineMemoryMB  *int `json:"task_machine_memory_mb,omitempty"`
	TaskMachineDiskGB    *int `json:"task_machine_disk_gb,omitempty"`
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

// AppPort is one exposed port of a machine's developer container.
type AppPort struct {
	ID             uuid.UUID  `json:"id"`
	Port           int        `json:"port"`
	Label          *string    `json:"label"`
	Source         string     `json:"source"`
	AgentTaskID    *uuid.UUID `json:"agent_task_id"`
	TaskIdentifier *string    `json:"task_identifier"`
	Host           string     `json:"host"`
	Listening      bool       `json:"listening"`
	// Target is the container serving it: "developer", or "docker" for a
	// port published by a container of the machine's Docker daemon.
	Target    string    `json:"target"`
	CreatedAt time.Time `json:"created_at"`
}

// AppPortList is a machine's app ports and its latest port scan.
type AppPortList struct {
	Ports     []AppPort  `json:"ports"`
	Listening []int      `json:"listening"`
	ScannedAt *time.Time `json:"scanned_at"`
}

// AppPortRequest exposes a port; Label is optional.
type AppPortRequest struct {
	Port  int     `json:"port"`
	Label *string `json:"label"`
}

// AppPortUpdate renames a port; null or "" clears the label.
type AppPortUpdate struct {
	Label *string `json:"label"`
}
