package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type DevMachineStatus string

const (
	DevMachineStatusConfiguring DevMachineStatus = "configuring"
	DevMachineStatusQueued      DevMachineStatus = "queued"
	DevMachineStatusSpawning    DevMachineStatus = "spawning"
	DevMachineStatusRunning     DevMachineStatus = "running"
	DevMachineStatusPaused      DevMachineStatus = "paused"
	DevMachineStatusStopping    DevMachineStatus = "stopping"
	DevMachineStatusStopped     DevMachineStatus = "stopped"
	DevMachineStatusTearingDown DevMachineStatus = "tearing_down"
	DevMachineStatusDestroyed   DevMachineStatus = "destroyed"
	DevMachineStatusFailed      DevMachineStatus = "failed"
	DevMachineStatusExpired     DevMachineStatus = "expired"
)

type DevMachineOperationAction string

const (
	DevMachineOpSpawn               DevMachineOperationAction = "spawn"
	DevMachineOpStart               DevMachineOperationAction = "start"
	DevMachineOpStop                DevMachineOperationAction = "stop"
	DevMachineOpPause               DevMachineOperationAction = "pause"
	DevMachineOpTeardown            DevMachineOperationAction = "teardown"
	DevMachineOpReconcile           DevMachineOperationAction = "reconcile"
	DevMachineOpRunAgent            DevMachineOperationAction = "run_agent"
	DevMachineOpCancelAgent         DevMachineOperationAction = "cancel_agent"
	DevMachineOpCheckoutIssue       DevMachineOperationAction = "checkout_issue"
	DevMachineOpSnapshotEnvironment DevMachineOperationAction = "snapshot_environment"
	DevMachineOpTerminateTerminal   DevMachineOperationAction = "terminate_terminal"
)

type DevMachineOperationStatus string

const (
	DevMachineOpStatusPending   DevMachineOperationStatus = "pending"
	DevMachineOpStatusLeased    DevMachineOperationStatus = "leased"
	DevMachineOpStatusCompleted DevMachineOperationStatus = "completed"
	DevMachineOpStatusFailed    DevMachineOperationStatus = "failed"
	DevMachineOpStatusCancelled DevMachineOperationStatus = "cancelled"
)

type DevMachineAgentRunStatus string

const (
	DevMachineAgentRunStatusQueued       DevMachineAgentRunStatus = "queued"
	DevMachineAgentRunStatusStarting     DevMachineAgentRunStatus = "starting"
	DevMachineAgentRunStatusRunning      DevMachineAgentRunStatus = "running"
	DevMachineAgentRunStatusWaitingInput DevMachineAgentRunStatus = "waiting_input"
	DevMachineAgentRunStatusSucceeded    DevMachineAgentRunStatus = "succeeded"
	DevMachineAgentRunStatusFailed       DevMachineAgentRunStatus = "failed"
	DevMachineAgentRunStatusCancelled    DevMachineAgentRunStatus = "cancelled"
	DevMachineAgentRunStatusTimeout      DevMachineAgentRunStatus = "timeout"
)

type DevMachineAgentRunStepStatus string

const (
	DevMachineAgentRunStepStatusQueued    DevMachineAgentRunStepStatus = "queued"
	DevMachineAgentRunStepStatusRunning   DevMachineAgentRunStepStatus = "running"
	DevMachineAgentRunStepStatusSucceeded DevMachineAgentRunStepStatus = "succeeded"
	DevMachineAgentRunStepStatusFailed    DevMachineAgentRunStepStatus = "failed"
	DevMachineAgentRunStepStatusCancelled DevMachineAgentRunStepStatus = "cancelled"
	DevMachineAgentRunStepStatusSkipped   DevMachineAgentRunStepStatus = "skipped"
)

type DevMachineAccessTicketStatus string

const (
	DevMachineAccessTicketStatusActive  DevMachineAccessTicketStatus = "active"
	DevMachineAccessTicketStatusUsed    DevMachineAccessTicketStatus = "used"
	DevMachineAccessTicketStatusExpired DevMachineAccessTicketStatus = "expired"
	DevMachineAccessTicketStatusRevoked DevMachineAccessTicketStatus = "revoked"
)

type DevMachine struct {
	ID                   uuid.UUID        `json:"id" db:"id"`
	WorkspaceID          uuid.UUID        `json:"workspace_id" db:"workspace_id"`
	ProjectID            *uuid.UUID       `json:"project_id,omitempty" db:"project_id"`
	IssueID              *uuid.UUID       `json:"issue_id,omitempty" db:"issue_id"`
	CreatedByUserID      *uuid.UUID       `json:"created_by_user_id,omitempty" db:"created_by_user_id"`
	RoutingKey           string           `json:"routing_key" db:"routing_key"`
	Name                 string           `json:"name" db:"name"`
	Status               DevMachineStatus `json:"status" db:"status"`
	DesiredStatus        DevMachineStatus `json:"desired_status" db:"desired_status"`
	Generation           int64            `json:"generation" db:"generation"`
	RepoURL              string           `json:"repo_url" db:"repo_url"`
	RepoProvider         string           `json:"repo_provider" db:"repo_provider"`
	RepoOwner            string           `json:"repo_owner" db:"repo_owner"`
	RepoName             string           `json:"repo_name" db:"repo_name"`
	BaseBranch           string           `json:"base_branch" db:"base_branch"`
	WorkingBranch        string           `json:"working_branch" db:"working_branch"`
	MachineSize          string           `json:"machine_size" db:"machine_size"`
	CPUMillis            int              `json:"cpu_millis" db:"cpu_millis"`
	MemoryMB             int              `json:"memory_mb" db:"memory_mb"`
	DiskGB               int              `json:"disk_gb" db:"disk_gb"`
	PidsLimit            int              `json:"pids_limit" db:"pids_limit"`
	MaxRuntimeMinutes    int              `json:"max_runtime_minutes" db:"max_runtime_minutes"`
	EnvironmentID        *uuid.UUID       `json:"environment_id,omitempty" db:"environment_id"`
	RepositoryAffinityID *uuid.UUID       `json:"repository_affinity_id,omitempty" db:"repository_affinity_id"`
	KeepRunning          bool             `json:"keep_running" db:"keep_running"`
	EnvironmentBuilder   bool             `json:"environment_builder" db:"environment_builder"`
	DeleteRequestedAt    *time.Time       `json:"delete_requested_at,omitempty" db:"delete_requested_at"`
	DockerNetworkName    *string          `json:"docker_network_name,omitempty" db:"docker_network_name"`
	WorkspaceVolumeName  *string          `json:"workspace_volume_name,omitempty" db:"workspace_volume_name"`
	ServicesConfig       json.RawMessage  `json:"services_config" db:"services_config"`
	Labels               json.RawMessage  `json:"labels" db:"labels"`
	LastErrorCode        *string          `json:"last_error_code,omitempty" db:"last_error_code"`
	LastErrorMessage     *string          `json:"last_error_message,omitempty" db:"last_error_message"`
	LastActivityAt       *time.Time       `json:"last_activity_at,omitempty" db:"last_activity_at"`
	CreatedAt            time.Time        `json:"created_at" db:"created_at"`
	UpdatedAt            time.Time        `json:"updated_at" db:"updated_at"`
	StartedAt            *time.Time       `json:"started_at,omitempty" db:"started_at"`
	StoppedAt            *time.Time       `json:"stopped_at,omitempty" db:"stopped_at"`
	ExpiresAt            time.Time        `json:"expires_at" db:"expires_at"`
	TeardownAt           *time.Time       `json:"teardown_at,omitempty" db:"teardown_at"`
	DestroyedAt          *time.Time       `json:"destroyed_at,omitempty" db:"destroyed_at"`
}

type DevMachineAgentProvider struct {
	ID              uuid.UUID       `json:"id" db:"id"`
	MachineID       uuid.UUID       `json:"machine_id" db:"machine_id"`
	ProviderID      string          `json:"provider_id" db:"provider_id"`
	DisplayName     string          `json:"display_name" db:"display_name"`
	ImageRef        string          `json:"image_ref" db:"image_ref"`
	SupportedModes  json.RawMessage `json:"supported_modes" db:"supported_modes"`
	RequiredSecrets json.RawMessage `json:"required_secrets" db:"required_secrets"`
	Config          json.RawMessage `json:"config" db:"config"`
	Enabled         bool            `json:"enabled" db:"enabled"`
	IsCustom        bool            `json:"is_custom" db:"is_custom"`
	CreatedAt       time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at" db:"updated_at"`
}

type DevMachineService struct {
	ID            uuid.UUID  `json:"id" db:"id"`
	WorkspaceID   uuid.UUID  `json:"workspace_id" db:"workspace_id"`
	MachineID     uuid.UUID  `json:"machine_id" db:"machine_id"`
	AgentRunID    *uuid.UUID `json:"agent_run_id,omitempty" db:"agent_run_id"`
	ServiceType   string     `json:"service_type" db:"service_type"`
	ServiceKey    string     `json:"service_key" db:"service_key"`
	ContainerID   *string    `json:"container_id,omitempty" db:"container_id"`
	ContainerName string     `json:"container_name" db:"container_name"`
	ImageRef      string     `json:"image_ref" db:"image_ref"`
	InternalHost  string     `json:"internal_host" db:"internal_host"`
	InternalPort  int        `json:"internal_port" db:"internal_port"`
	Status        string     `json:"status" db:"status"`
	HealthStatus  string     `json:"health_status" db:"health_status"`
	HealthMessage *string    `json:"health_message,omitempty" db:"health_message"`
	StartedAt     *time.Time `json:"started_at,omitempty" db:"started_at"`
	StoppedAt     *time.Time `json:"stopped_at,omitempty" db:"stopped_at"`
	CreatedAt     time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at" db:"updated_at"`
}

type DevMachineVolume struct {
	ID               uuid.UUID  `json:"id" db:"id"`
	MachineID        uuid.UUID  `json:"machine_id" db:"machine_id"`
	VolumeType       string     `json:"volume_type" db:"volume_type"`
	RuntimeName      string     `json:"runtime_name" db:"runtime_name"`
	MountPath        string     `json:"mount_path" db:"mount_path"`
	SizeLimitBytes   int64      `json:"size_limit_bytes" db:"size_limit_bytes"`
	CurrentSizeBytes int64      `json:"current_size_bytes" db:"current_size_bytes"`
	CreatedAt        time.Time  `json:"created_at" db:"created_at"`
	DeletedAt        *time.Time `json:"deleted_at,omitempty" db:"deleted_at"`
}

type DevMachineEnvVar struct {
	ID                   uuid.UUID  `json:"id" db:"id"`
	MachineID            uuid.UUID  `json:"machine_id" db:"machine_id"`
	AgentRunID           *uuid.UUID `json:"agent_run_id,omitempty" db:"agent_run_id"`
	ProviderID           *string    `json:"provider_id,omitempty" db:"provider_id"`
	TargetService        string     `json:"target_service" db:"target_service"`
	Name                 string     `json:"name" db:"name"`
	EncryptedValue       string     `json:"-" db:"encrypted_value"`
	EncryptionKeyVersion int        `json:"encryption_key_version" db:"encryption_key_version"`
	IsSecret             bool       `json:"is_secret" db:"is_secret"`
	ExpiresAt            *time.Time `json:"expires_at,omitempty" db:"expires_at"`
	RevokedAt            *time.Time `json:"revoked_at,omitempty" db:"revoked_at"`
	CreatedAt            time.Time  `json:"created_at" db:"created_at"`
}

const (
	DevMachineRuntimeCredentialScopeMachine    = "machine"
	DevMachineRuntimeCredentialTypeGitHubToken = "github_installation_token"
)

type DevMachineRuntimeCredential struct {
	ID                   uuid.UUID `json:"id" db:"id"`
	MachineID            uuid.UUID `json:"machine_id" db:"machine_id"`
	Scope                string    `json:"scope" db:"scope"`
	CredentialType       string    `json:"credential_type" db:"credential_type"`
	FingerprintSHA256    string    `json:"-" db:"fingerprint_sha256"`
	EncryptedValue       string    `json:"-" db:"encrypted_value"`
	EncryptionKeyVersion int       `json:"encryption_key_version" db:"encryption_key_version"`
	ExpiresAt            time.Time `json:"expires_at" db:"expires_at"`
	CreatedAt            time.Time `json:"created_at" db:"created_at"`
	UpdatedAt            time.Time `json:"updated_at" db:"updated_at"`
}

type DevMachineToken struct {
	ID         uuid.UUID       `json:"id" db:"id"`
	MachineID  uuid.UUID       `json:"machine_id" db:"machine_id"`
	AgentRunID *uuid.UUID      `json:"agent_run_id,omitempty" db:"agent_run_id"`
	TokenHash  string          `json:"-" db:"token_hash"`
	Scopes     json.RawMessage `json:"scopes" db:"scopes"`
	ExpiresAt  time.Time       `json:"expires_at" db:"expires_at"`
	LastUsedAt *time.Time      `json:"last_used_at,omitempty" db:"last_used_at"`
	RevokedAt  *time.Time      `json:"revoked_at,omitempty" db:"revoked_at"`
	CreatedAt  time.Time       `json:"created_at" db:"created_at"`
}

type DevMachineOperation struct {
	ID                uuid.UUID                 `json:"id" db:"id"`
	MachineID         uuid.UUID                 `json:"machine_id" db:"machine_id"`
	AgentRunID        *uuid.UUID                `json:"agent_run_id,omitempty" db:"agent_run_id"`
	CheckoutID        *uuid.UUID                `json:"checkout_id,omitempty" db:"checkout_id"`
	EnvironmentID     *uuid.UUID                `json:"environment_id,omitempty" db:"environment_id"`
	TerminalSessionID *uuid.UUID                `json:"terminal_session_id,omitempty" db:"terminal_session_id"`
	WorkspaceID       uuid.UUID                 `json:"workspace_id" db:"workspace_id"`
	Action            DevMachineOperationAction `json:"action" db:"action"`
	Status            DevMachineOperationStatus `json:"status" db:"status"`
	Generation        int64                     `json:"generation" db:"generation"`
	IdempotencyKey    string                    `json:"idempotency_key" db:"idempotency_key"`
	RequestedByUserID *uuid.UUID                `json:"requested_by_user_id,omitempty" db:"requested_by_user_id"`
	LeaseOwner        *string                   `json:"lease_owner,omitempty" db:"lease_owner"`
	LeaseExpiresAt    *time.Time                `json:"lease_expires_at,omitempty" db:"lease_expires_at"`
	Attempts          int                       `json:"attempts" db:"attempts"`
	MaxAttempts       int                       `json:"max_attempts" db:"max_attempts"`
	ErrorCode         *string                   `json:"error_code,omitempty" db:"error_code"`
	ErrorMessage      *string                   `json:"error_message,omitempty" db:"error_message"`
	AvailableAt       time.Time                 `json:"available_at" db:"available_at"`
	CreatedAt         time.Time                 `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time                 `json:"updated_at" db:"updated_at"`
	CompletedAt       *time.Time                `json:"completed_at,omitempty" db:"completed_at"`
}

type DevMachineAgentRun struct {
	ID                 uuid.UUID                `json:"id" db:"id"`
	MachineID          uuid.UUID                `json:"machine_id" db:"machine_id"`
	WorkspaceID        uuid.UUID                `json:"workspace_id" db:"workspace_id"`
	IssueID            *uuid.UUID               `json:"issue_id,omitempty" db:"issue_id"`
	CheckoutID         *uuid.UUID               `json:"checkout_id,omitempty" db:"checkout_id"`
	RequestedByUserID  *uuid.UUID               `json:"requested_by_user_id,omitempty" db:"requested_by_user_id"`
	ProviderID         string                   `json:"provider_id" db:"provider_id"`
	Mode               string                   `json:"mode" db:"mode"`
	Status             DevMachineAgentRunStatus `json:"status" db:"status"`
	Prompt             string                   `json:"prompt" db:"prompt"`
	AcceptanceCriteria json.RawMessage          `json:"acceptance_criteria" db:"acceptance_criteria"`
	AllowedCommands    json.RawMessage          `json:"allowed_commands" db:"allowed_commands"`
	ForbiddenPaths     json.RawMessage          `json:"forbidden_paths" db:"forbidden_paths"`
	AllowedSecrets     json.RawMessage          `json:"allowed_secrets" db:"allowed_secrets"`
	TestCommand        *json.RawMessage         `json:"test_command,omitempty" db:"test_command"`
	CommandArgv        json.RawMessage          `json:"command_argv" db:"command_argv"`
	MaxRuntimeSeconds  int                      `json:"max_runtime_seconds" db:"max_runtime_seconds"`
	PushBranch         bool                     `json:"push_branch" db:"push_branch"`
	OpenPullRequest    bool                     `json:"open_pull_request" db:"open_pull_request"`
	Result             *json.RawMessage         `json:"result,omitempty" db:"result"`
	Summary            *string                  `json:"summary,omitempty" db:"summary"`
	ChangedFiles       json.RawMessage          `json:"changed_files" db:"changed_files"`
	Commits            json.RawMessage          `json:"commits" db:"commits"`
	Branch             *string                  `json:"branch,omitempty" db:"branch"`
	PullRequestURL     *string                  `json:"pull_request_url,omitempty" db:"pull_request_url"`
	TestsRun           json.RawMessage          `json:"tests_run" db:"tests_run"`
	TestStatus         string                   `json:"test_status" db:"test_status"`
	RiskNotes          json.RawMessage          `json:"risk_notes" db:"risk_notes"`
	ExitCode           *int                     `json:"exit_code,omitempty" db:"exit_code"`
	ErrorMessage       *string                  `json:"error_message,omitempty" db:"error_message"`
	CreatedAt          time.Time                `json:"created_at" db:"created_at"`
	StartedAt          *time.Time               `json:"started_at,omitempty" db:"started_at"`
	CompletedAt        *time.Time               `json:"completed_at,omitempty" db:"completed_at"`
	CancelledAt        *time.Time               `json:"cancelled_at,omitempty" db:"cancelled_at"`
}

type DevMachineAgentRunStep struct {
	ID          uuid.UUID                    `json:"id" db:"id"`
	AgentRunID  uuid.UUID                    `json:"agent_run_id" db:"agent_run_id"`
	Sequence    int                          `json:"sequence" db:"sequence"`
	StepType    string                       `json:"step_type" db:"step_type"`
	Name        string                       `json:"name" db:"name"`
	Status      DevMachineAgentRunStepStatus `json:"status" db:"status"`
	CommandArgv json.RawMessage              `json:"command_argv,omitempty" db:"command_argv"`
	Summary     *string                      `json:"summary,omitempty" db:"summary"`
	ExitCode    *int                         `json:"exit_code,omitempty" db:"exit_code"`
	StartedAt   *time.Time                   `json:"started_at,omitempty" db:"started_at"`
	CompletedAt *time.Time                   `json:"completed_at,omitempty" db:"completed_at"`
	CreatedAt   time.Time                    `json:"created_at" db:"created_at"`
}

type DevMachineEvent struct {
	ID          int64           `json:"id" db:"id"`
	WorkspaceID uuid.UUID       `json:"workspace_id" db:"workspace_id"`
	MachineID   uuid.UUID       `json:"machine_id" db:"machine_id"`
	AgentRunID  *uuid.UUID      `json:"agent_run_id,omitempty" db:"agent_run_id"`
	ActorUserID *uuid.UUID      `json:"actor_user_id,omitempty" db:"actor_user_id"`
	Source      string          `json:"source" db:"source"`
	EventType   string          `json:"event_type" db:"event_type"`
	Payload     json.RawMessage `json:"payload" db:"payload"`
	OccurredAt  time.Time       `json:"occurred_at" db:"occurred_at"`
	CreatedAt   time.Time       `json:"created_at" db:"created_at"`
}

type DevMachineLogChunk struct {
	ID          int64      `json:"id" db:"id"`
	WorkspaceID uuid.UUID  `json:"workspace_id" db:"workspace_id"`
	MachineID   uuid.UUID  `json:"machine_id" db:"machine_id"`
	AgentRunID  *uuid.UUID `json:"agent_run_id,omitempty" db:"agent_run_id"`
	ServiceID   *uuid.UUID `json:"service_id,omitempty" db:"service_id"`
	Stream      string     `json:"stream" db:"stream"`
	Sequence    int64      `json:"sequence" db:"sequence"`
	Content     string     `json:"content" db:"content"`
	Truncated   bool       `json:"truncated" db:"truncated"`
	CreatedAt   time.Time  `json:"created_at" db:"created_at"`
}

type DevMachineArtifact struct {
	ID             uuid.UUID       `json:"id" db:"id"`
	WorkspaceID    uuid.UUID       `json:"workspace_id" db:"workspace_id"`
	MachineID      uuid.UUID       `json:"machine_id" db:"machine_id"`
	AgentRunID     *uuid.UUID      `json:"agent_run_id,omitempty" db:"agent_run_id"`
	ArtifactType   string          `json:"artifact_type" db:"artifact_type"`
	Name           string          `json:"name" db:"name"`
	StorageKey     string          `json:"storage_key" db:"storage_key"`
	ContentType    *string         `json:"content_type,omitempty" db:"content_type"`
	SizeBytes      *int64          `json:"size_bytes,omitempty" db:"size_bytes"`
	ChecksumSHA256 *string         `json:"checksum_sha256,omitempty" db:"checksum_sha256"`
	Metadata       json.RawMessage `json:"metadata" db:"metadata"`
	CreatedAt      time.Time       `json:"created_at" db:"created_at"`
}

type DevMachineGitRef struct {
	ID                 uuid.UUID  `json:"id" db:"id"`
	WorkspaceID        uuid.UUID  `json:"workspace_id" db:"workspace_id"`
	MachineID          uuid.UUID  `json:"machine_id" db:"machine_id"`
	AgentRunID         *uuid.UUID `json:"agent_run_id,omitempty" db:"agent_run_id"`
	IssueID            *uuid.UUID `json:"issue_id,omitempty" db:"issue_id"`
	RefType            string     `json:"ref_type" db:"ref_type"`
	RepositoryFullName string     `json:"repository_full_name" db:"repository_full_name"`
	RefName            *string    `json:"ref_name,omitempty" db:"ref_name"`
	CommitSHA          *string    `json:"commit_sha,omitempty" db:"commit_sha"`
	PullRequestNumber  *int       `json:"pull_request_number,omitempty" db:"pull_request_number"`
	URL                *string    `json:"url,omitempty" db:"url"`
	CreatedAt          time.Time  `json:"created_at" db:"created_at"`
}

type DevMachineAccessTicket struct {
	ID                uuid.UUID                    `json:"id" db:"id"`
	WorkspaceID       uuid.UUID                    `json:"workspace_id" db:"workspace_id"`
	MachineID         uuid.UUID                    `json:"machine_id" db:"machine_id"`
	ServiceID         uuid.UUID                    `json:"service_id" db:"service_id"`
	TerminalSessionID *uuid.UUID                   `json:"terminal_session_id,omitempty" db:"terminal_session_id"`
	UserID            uuid.UUID                    `json:"user_id" db:"user_id"`
	TokenHash         string                       `json:"-" db:"token_hash"`
	Status            DevMachineAccessTicketStatus `json:"status" db:"status"`
	BoundHost         string                       `json:"bound_host" db:"bound_host"`
	ExpiresAt         time.Time                    `json:"expires_at" db:"expires_at"`
	UsedAt            *time.Time                   `json:"used_at,omitempty" db:"used_at"`
	CreatedAt         time.Time                    `json:"created_at" db:"created_at"`
	RevokedAt         *time.Time                   `json:"revoked_at,omitempty" db:"revoked_at"`
}

type DevMachineAccessSession struct {
	ID          uuid.UUID  `json:"id" db:"id"`
	WorkspaceID uuid.UUID  `json:"workspace_id" db:"workspace_id"`
	MachineID   uuid.UUID  `json:"machine_id" db:"machine_id"`
	ServiceID   uuid.UUID  `json:"service_id" db:"service_id"`
	UserID      uuid.UUID  `json:"user_id" db:"user_id"`
	TokenHash   string     `json:"-" db:"token_hash"`
	BoundHost   string     `json:"bound_host" db:"bound_host"`
	ExpiresAt   time.Time  `json:"expires_at" db:"expires_at"`
	LastSeenAt  time.Time  `json:"last_seen_at" db:"last_seen_at"`
	CreatedAt   time.Time  `json:"created_at" db:"created_at"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty" db:"revoked_at"`
}

type DevMachineAccessLog struct {
	ID             int64      `json:"id" db:"id"`
	WorkspaceID    *uuid.UUID `json:"workspace_id,omitempty" db:"workspace_id"`
	MachineID      *uuid.UUID `json:"machine_id,omitempty" db:"machine_id"`
	ServiceID      *uuid.UUID `json:"service_id,omitempty" db:"service_id"`
	UserID         *uuid.UUID `json:"user_id,omitempty" db:"user_id"`
	Decision       string     `json:"decision" db:"decision"`
	Reason         *string    `json:"reason,omitempty" db:"reason"`
	Method         string     `json:"method" db:"method"`
	Path           string     `json:"path" db:"path"`
	ResponseStatus *int       `json:"response_status,omitempty" db:"response_status"`
	RemoteIP       *string    `json:"remote_ip,omitempty" db:"remote_ip"`
	UserAgent      *string    `json:"user_agent,omitempty" db:"user_agent"`
	CreatedAt      time.Time  `json:"created_at" db:"created_at"`
}

type DevMachineResourceSample struct {
	ID             int64     `json:"id" db:"id"`
	WorkspaceID    uuid.UUID `json:"workspace_id" db:"workspace_id"`
	MachineID      uuid.UUID `json:"machine_id" db:"machine_id"`
	CPUPercent     float64   `json:"cpu_percent" db:"cpu_percent"`
	MemoryBytes    int64     `json:"memory_bytes" db:"memory_bytes"`
	DiskBytes      int64     `json:"disk_bytes" db:"disk_bytes"`
	Pids           int       `json:"pids" db:"pids"`
	NetworkRxBytes int64     `json:"network_rx_bytes" db:"network_rx_bytes"`
	NetworkTxBytes int64     `json:"network_tx_bytes" db:"network_tx_bytes"`
	CreatedAt      time.Time `json:"created_at" db:"created_at"`
}

type DevMachineWorkspacePolicy struct {
	WorkspaceID           uuid.UUID       `json:"workspace_id" db:"workspace_id"`
	Enabled               bool            `json:"enabled" db:"enabled"`
	MaxConcurrentMachines int             `json:"max_concurrent_machines" db:"max_concurrent_machines"`
	MaxMachinesPerUser    int             `json:"max_machines_per_user" db:"max_machines_per_user"`
	MaxDailyAgentRuns     int             `json:"max_daily_agent_runs" db:"max_daily_agent_runs"`
	MaxRuntimeMinutes     int             `json:"max_runtime_minutes" db:"max_runtime_minutes"`
	MaxDiskGB             int             `json:"max_disk_gb" db:"max_disk_gb"`
	AllowedProviders      json.RawMessage `json:"allowed_providers" db:"allowed_providers"`
	AllowedRepositories   json.RawMessage `json:"allowed_repositories" db:"allowed_repositories"`
	AllowCustomProviders  bool            `json:"allow_custom_providers" db:"allow_custom_providers"`
	IdlePauseMinutes      int             `json:"idle_pause_minutes" db:"idle_pause_minutes"`
	CreatedAt             time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt             time.Time       `json:"updated_at" db:"updated_at"`
}

type DevMachineEnvironment struct {
	ID                uuid.UUID  `json:"id" db:"id"`
	WorkspaceID       uuid.UUID  `json:"workspace_id" db:"workspace_id"`
	Name              string     `json:"name" db:"name"`
	ImageRef          string     `json:"image_ref" db:"image_ref"`
	ImageDigest       *string    `json:"image_digest,omitempty" db:"image_digest"`
	Status            string     `json:"status" db:"status"`
	SourceMachineID   *uuid.UUID `json:"source_machine_id,omitempty" db:"source_machine_id"`
	CreatedByUserID   *uuid.UUID `json:"created_by_user_id,omitempty" db:"created_by_user_id"`
	DeleteRequestedAt *time.Time `json:"delete_requested_at,omitempty" db:"delete_requested_at"`
	CreatedAt         time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at" db:"updated_at"`
}

type DevMachineScopeSetting struct {
	ID            uuid.UUID  `json:"id" db:"id"`
	WorkspaceID   uuid.UUID  `json:"workspace_id" db:"workspace_id"`
	TeamID        *uuid.UUID `json:"team_id,omitempty" db:"team_id"`
	ProjectID     *uuid.UUID `json:"project_id,omitempty" db:"project_id"`
	IssueID       *uuid.UUID `json:"issue_id,omitempty" db:"issue_id"`
	GitHubRepoID  *uuid.UUID `json:"github_repo_id,omitempty" db:"github_repo_id"`
	BaseBranch    *string    `json:"base_branch,omitempty" db:"base_branch"`
	EnvironmentID *uuid.UUID `json:"environment_id,omitempty" db:"environment_id"`
	CreatedAt     time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at" db:"updated_at"`
}

type DevMachineCheckout struct {
	ID                 uuid.UUID  `json:"id" db:"id"`
	WorkspaceID        uuid.UUID  `json:"workspace_id" db:"workspace_id"`
	MachineID          uuid.UUID  `json:"machine_id" db:"machine_id"`
	IssueID            uuid.UUID  `json:"issue_id" db:"issue_id"`
	GitHubRepoID       uuid.UUID  `json:"github_repo_id" db:"github_repo_id"`
	RepositoryFullName string     `json:"repository_full_name" db:"repository_full_name"`
	BaseBranch         string     `json:"base_branch" db:"base_branch"`
	WorkingBranch      string     `json:"working_branch" db:"working_branch"`
	WorkspacePath      string     `json:"workspace_path" db:"workspace_path"`
	Status             string     `json:"status" db:"status"`
	LastError          *string    `json:"last_error,omitempty" db:"last_error"`
	CreatedAt          time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at" db:"updated_at"`
	LastActivityAt     *time.Time `json:"last_activity_at,omitempty" db:"last_activity_at"`
}

type DevMachineTerminalSession struct {
	ID                 uuid.UUID  `json:"id" db:"id"`
	WorkspaceID        uuid.UUID  `json:"workspace_id" db:"workspace_id"`
	MachineID          uuid.UUID  `json:"machine_id" db:"machine_id"`
	CheckoutID         *uuid.UUID `json:"checkout_id,omitempty" db:"checkout_id"`
	UserID             uuid.UUID  `json:"user_id" db:"user_id"`
	Name               string     `json:"name" db:"name"`
	RuntimeSessionName string     `json:"runtime_session_name" db:"runtime_session_name"`
	Status             string     `json:"status" db:"status"`
	CreatedAt          time.Time  `json:"created_at" db:"created_at"`
	LastActivityAt     time.Time  `json:"last_activity_at" db:"last_activity_at"`
	ClosedAt           *time.Time `json:"closed_at,omitempty" db:"closed_at"`
}

func ValidOperationForStatus(action DevMachineOperationAction, status DevMachineStatus) bool {
	switch action {
	case DevMachineOpSpawn:
		return status == DevMachineStatusConfiguring || status == DevMachineStatusQueued || status == DevMachineStatusFailed
	case DevMachineOpStart:
		return status == DevMachineStatusStopped || status == DevMachineStatusPaused || status == DevMachineStatusFailed
	case DevMachineOpStop:
		return status == DevMachineStatusRunning || status == DevMachineStatusPaused
	case DevMachineOpPause:
		return status == DevMachineStatusRunning
	case DevMachineOpTeardown:
		return status != DevMachineStatusDestroyed && status != DevMachineStatusTearingDown
	case DevMachineOpReconcile:
		return status != DevMachineStatusDestroyed
	case DevMachineOpRunAgent, DevMachineOpCancelAgent, DevMachineOpCheckoutIssue:
		return status == DevMachineStatusRunning
	case DevMachineOpSnapshotEnvironment:
		return status == DevMachineStatusPaused || status == DevMachineStatusStopped
	default:
		return false
	}
}

func DevMachineSafelyPurgeable(machine *DevMachine) bool {
	if machine == nil {
		return false
	}
	if machine.Status == DevMachineStatusDestroyed {
		return true
	}
	if machine.Status == DevMachineStatusFailed || machine.Status == DevMachineStatusExpired {
		return machine.DockerNetworkName == nil && machine.WorkspaceVolumeName == nil
	}
	return false
}

func DevMachineSize(size string) (cpuMillis, memoryMB, diskGB, maxRuntimeMinutes int, ok bool) {
	switch size {
	case "small":
		return 2000, 4096, 20, 120, true
	case "medium":
		return 4000, 8192, 50, 240, true
	case "large":
		return 8000, 16384, 100, 480, true
	default:
		return 0, 0, 0, 0, false
	}
}
