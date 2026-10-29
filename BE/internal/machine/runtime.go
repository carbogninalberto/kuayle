package machine

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

var ErrHostDiskUnknown = errors.New("host disk usage is unknown")

// ErrRuntimeIncomplete reports that a stopped or paused machine cannot simply
// be resumed because its workspace volume or a service container was never
// created or no longer exists. The manager provisions the machine again, which
// reuses every part that still exists.
var ErrRuntimeIncomplete = errors.New("machine runtime is incomplete")

// AgentOutput consumes raw runtime bytes synchronously. The manager must redact
// them before persistence. Returning an error aborts execution.
type AgentOutput func(stream string, data []byte) error

// AgentObserver checks authority after preparation and before start/resume or
// credential handoff, and persists execution milestones. Started is called after
// container startup (or on recovery of an executed container), before Output.
// Any callback failing requires the runtime to stop the native process.
type AgentObserver struct {
	Authorize func() error
	Started   func(containerID string) error
	Output    AgentOutput
}

type AgentExecution struct {
	ContainerID string
	Stdout      string
	Stderr      string
	ExitCode    int
	// OOMKilled reports that the kernel killed a process of the container for
	// reaching MemoryLimitBytes, the container's memory limit.
	OOMKilled        bool
	MemoryLimitBytes int64
}

type ResourceUsage struct {
	CPUPercent     float64
	MemoryBytes    int64
	DiskBytes      int64
	Pids           int
	NetworkRxBytes int64
	NetworkTxBytes int64
}

type HostDiskUsage struct {
	TotalBytes int64
	FreeBytes  int64
}

type GitState struct {
	Branch       string
	Commits      []string
	ChangedFiles []string
	// Diff is the worktree (including new files) against the base branch.
	Diff          string
	DiffTruncated bool
}

type RuntimeInspection struct {
	NetworkName     string
	VolumeName      string
	NetworkExists   bool
	VolumeExists    bool
	GatewayAttached bool
	Services        map[string]RuntimeServiceInspection
}

type RuntimeServiceInspection struct {
	ContainerID  string
	Status       string
	HealthStatus string
	Exists       bool
	Running      bool
	Paused       bool
	OnNetwork    bool
}

type Runtime interface {
	RunValidation(context.Context, *domain.DevMachine, *domain.DevMachineAgentRun, *domain.DevMachineAgentRunStep, *domain.DevMachineCheckout, string, *AgentObserver) (*AgentExecution, error)
	HostResources(context.Context) (HostResources, error)
	CollectValidationArtifacts(context.Context, *domain.DevMachine, *domain.DevMachineAgentRun, *domain.DevMachineAgentRunStep, *domain.DevMachineCheckout) ([]domain.ValidationArtifact, error)
	RemoveCheckServices(context.Context, *domain.DevMachine, *domain.DevMachineAgentRun, *domain.DevMachineAgentRunStep) error
	// RemoveCheck deletes a finished manual check's container.
	RemoveCheck(context.Context, *domain.DevMachine, *domain.DevMachineAgentRun, *domain.DevMachineAgentRunStep) error
	// RunTool runs a custom tool in its own check container; RemoveTool
	// deletes the finished run's container.
	RunTool(context.Context, *domain.DevMachine, *domain.AssistantToolRunExecution, string, *AgentObserver) (*AgentExecution, error)
	RemoveTool(context.Context, *domain.DevMachine, uuid.UUID) error
	// StartPreview (re)starts one task's preview process; StopPreview stops it.
	// Neither affects another task's preview.
	StartPreview(ctx context.Context, machine *domain.DevMachine, services []domain.DevMachineService, checkout *domain.DevMachineCheckout, taskID uuid.UUID, argv []string, port int) error
	StopPreview(ctx context.Context, services []domain.DevMachineService, taskID uuid.UUID) error
	// ScanPorts lists the developer container's listening TCP ports and takes
	// the agents' port requests; WritePortState publishes the result to them.
	ScanPorts(ctx context.Context, services []domain.DevMachineService) (PortScan, error)
	WritePortState(ctx context.Context, services []domain.DevMachineService, state []byte) error
	ReadSignIn(ctx context.Context, services []domain.DevMachineService, provider string) (string, error)
	TakeSignInReturn(ctx context.Context, services []domain.DevMachineService, runID uuid.UUID) (string, error)
	PurgeTaskSession(context.Context, *domain.DevMachine, string, uuid.UUID) error
	PublishCheckout(context.Context, *domain.DevMachine, []domain.DevMachineService, *domain.DevMachineCheckout, PublishRequest) (string, error)
	Spawn(context.Context, *domain.DevMachine, []domain.DevMachineService, map[string]map[string]string) (string, string, map[string]string, error)
	Start(context.Context, *domain.DevMachine, []domain.DevMachineService, map[string]map[string]string) error
	Pause(context.Context, *domain.DevMachine, []domain.DevMachineService) error
	Stop(context.Context, *domain.DevMachine, []domain.DevMachineService) error
	Teardown(context.Context, *domain.DevMachine, []domain.DevMachineService) error
	RunAgent(context.Context, *domain.DevMachine, *domain.DevMachineAgentRun, *domain.DevMachineAgentProvider, *domain.DevMachineCheckout, map[string]string, *AgentObserver) (*AgentExecution, error)
	CancelAgent(context.Context, *domain.DevMachineAgentRun) error
	TerminateTerminal(context.Context, *domain.DevMachine, []domain.DevMachineService, *domain.DevMachineTerminalSession) error
	Inspect(context.Context, *domain.DevMachine, []domain.DevMachineService) (RuntimeInspection, error)
	Stats(context.Context, *domain.DevMachine, []domain.DevMachineService) (ResourceUsage, error)
	HostDiskUsage(context.Context) (HostDiskUsage, error)
	GitState(context.Context, *domain.DevMachine, []domain.DevMachineService, *domain.DevMachineCheckout) (GitState, error)
	PrepareCheckout(context.Context, *domain.DevMachine, []domain.DevMachineService, *domain.DevMachineCheckout, string) error
	SnapshotEnvironment(context.Context, *domain.DevMachine, []domain.DevMachineService, *domain.DevMachineEnvironment) (string, error)
	DeleteEnvironmentImage(context.Context, *domain.DevMachineEnvironment) error
	Ping(context.Context) error
	// UpdateResources sets the CPU, memory and process limits of every
	// container of the machine (running or not) from its size.
	UpdateResources(context.Context, *domain.DevMachine) error
}
