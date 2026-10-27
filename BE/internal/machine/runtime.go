package machine

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

var ErrHostDiskUnknown = errors.New("host disk usage is unknown")

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
	CollectValidationArtifacts(context.Context, *domain.DevMachine, *domain.DevMachineAgentRun, *domain.DevMachineCheckout) ([]domain.ValidationArtifact, error)
	RemoveCheckServices(context.Context, *domain.DevMachine, *domain.DevMachineAgentRun) error
	StartPreview(ctx context.Context, machine *domain.DevMachine, services []domain.DevMachineService, checkout *domain.DevMachineCheckout, argv []string, port int) error
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
}
