package machine

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

var ErrHostDiskUnknown = errors.New("host disk usage is unknown")

// ErrAgentDetached reports that the runtime stopped following a running agent
// because the manager is shutting down; the container was left running.
var ErrAgentDetached = errors.New("agent_detached: the machine manager stopped following the run, which keeps running")

// ErrAgentContainerGone reports that a run which had started has no container
// any more (removed while no manager followed it). It is never re-created.
var ErrAgentContainerGone = errors.New("agent_container_gone: the run's container no longer exists")

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
	// PrepareInbox writes images referenced by the prompt before container startup.
	PrepareInbox func(context.Context) error
	Authorize    func() error
	Started      func(containerID string) error
	Output       AgentOutput
	// Detach, when closed, makes the runtime stop following a running agent
	// attempt without stopping it: it returns ErrAgentDetached and the
	// container keeps running, for the next machine manager to re-adopt.
	Detach <-chan struct{}
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
	// CPUMillis and PidsLimit are the container's limits as created; PidsPeak
	// is the most processes and threads seen in it while it ran.
	CPUMillis int64
	PidsLimit int64
	PidsPeak  int64
	// MachineMemory reports that the container shares the machine's memory,
	// so MemoryLimitBytes is the machine's size (machine_slice.go).
	MachineMemory bool
	// ExitedUnobserved reports that the container had already exited when this
	// manager found it: it ended while no manager followed it.
	ExitedUnobserved bool
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
	// RunManualCheck runs a manual check in its own container.
	RunManualCheck(context.Context, *domain.DevMachine, *domain.DevMachineAgentRun, *domain.DevMachineAgentRunStep, *domain.DevMachineCheckout, string, *AgentObserver) (*AgentExecution, error)
	HostResources(context.Context) (HostResources, error)
	CollectValidationArtifacts(context.Context, *domain.DevMachine, *domain.DevMachineAgentRun, *domain.DevMachineAgentRunStep, *domain.DevMachineCheckout) ([]domain.ValidationArtifact, error)
	RemoveCheckServices(context.Context, *domain.DevMachine, *domain.DevMachineAgentRun, *domain.DevMachineAgentRunStep) error
	// RemoveCheck deletes a finished manual check's container.
	RemoveCheck(context.Context, *domain.DevMachine, *domain.DevMachineAgentRun, *domain.DevMachineAgentRunStep) error
	// RunTool runs a custom tool in its own check container; RemoveTool
	// deletes the finished run's container.
	RunTool(context.Context, *domain.DevMachine, *domain.AssistantToolRunExecution, string, *AgentObserver) (*AgentExecution, error)
	RemoveTool(context.Context, *domain.DevMachine, uuid.UUID) error
	// RunCommand runs an approved assistant command in its own check
	// container; migrationSecret is a migration run's action secret, empty
	// otherwise. RemoveCommand deletes the run's container.
	RunCommand(context.Context, *domain.DevMachine, *domain.CommandRun, string, string, *AgentObserver) (*AgentExecution, error)
	RemoveCommand(context.Context, *domain.DevMachine, uuid.UUID) error
	// ScanWorktree reads one task worktree with rg and git in the developer
	// container. It changes nothing and needs no credentials.
	ScanWorktree(context.Context, *domain.DevMachine, []domain.DevMachineService, WorktreeScanJob) (*domain.WorktreeScanResult, error)
	// StartPreview (re)starts one task's preview command in that task's own
	// preview container, never in the developer container; StopPreview
	// removes it. Neither affects another task's preview. RemovePreviews
	// removes every preview container of the machine.
	// docker is the Docker daemon client material of a machine with a daemon,
	// or empty; a preview receives nothing else.
	StartPreview(ctx context.Context, machine *domain.DevMachine, services []domain.DevMachineService, checkout *domain.DevMachineCheckout, taskID uuid.UUID, argv []string, port int, docker map[string]string) error
	StopPreview(ctx context.Context, machine *domain.DevMachine, services []domain.DevMachineService, taskID uuid.UUID) error
	RemovePreviews(ctx context.Context, machine *domain.DevMachine) error
	StartAssistantProcess(context.Context, *domain.DevMachine, []domain.DevMachineService, *domain.AssistantProcess) error
	AssistantProcessRunning(context.Context, *domain.AssistantProcess) (bool, error)
	RemoveAssistantProcess(context.Context, *domain.AssistantProcess) error
	// ScanPorts lists the developer container's listening TCP ports, the
	// listening preview ports, and takes the agents' port requests;
	// WritePortState publishes the result to them.
	ScanPorts(ctx context.Context, machine *domain.DevMachine, services []domain.DevMachineService) (PortScan, error)
	WritePortState(ctx context.Context, services []domain.DevMachineService, state []byte) error
	// ProbePackageIndex reads whether the running developer container has
	// apt package lists; RefreshPackageIndex runs "sudo apt-get update" in it
	// and returns the exit code and output (a non-zero exit is no error).
	ProbePackageIndex(ctx context.Context, services []domain.DevMachineService) (PackageIndexProbe, error)
	RefreshPackageIndex(ctx context.Context, services []domain.DevMachineService) (int, string, error)
	ReadSignIn(ctx context.Context, services []domain.DevMachineService, provider string) (string, error)
	TakeSignInReturn(ctx context.Context, services []domain.DevMachineService, runID uuid.UUID) (string, error)
	// SyncAgentSignIns replaces the owner's sign-in files in the developer
	// container's in-memory home with files (name to content).
	SyncAgentSignIns(ctx context.Context, services []domain.DevMachineService, files map[string]string) error
	PurgeTaskSession(context.Context, *domain.DevMachine, string, uuid.UUID) error
	CleanupTaskResources(context.Context, *domain.DevMachine, []domain.DevMachineService, *domain.DevMachineCheckout, uuid.UUID, []uuid.UUID) error
	// SyncCheckout brings commits others pushed to the task branch on GitHub
	// into the checkout, preserving pending work without rewriting either side.
	// When the checkout rewrote the commits Kuayle published (the agent rebased
	// its own branch) and GitHub's tip is still the recorded published commit,
	// nobody else pushed: it returns that tip as the lease for a guarded force
	// push and leaves the checkout alone.
	SyncCheckout(ctx context.Context, machine *domain.DevMachine, services []domain.DevMachineService, checkout *domain.DevMachineCheckout, request PublishRequest) (lease string, err error)
	// PublishCheckout commits and pushes the checkout's task branch. It returns
	// the pushed commit, or "" when the push step found nothing to publish
	// against the base as it is on GitHub. It returns ErrPublishedWorkRemains
	// instead when the branch has nothing new to publish but GitHub's task
	// branch still holds work an earlier publication put there, which the agent
	// has since removed: nothing is pushed then.
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
	// RemoveContainers force-removes the machine's containers with these IDs.
	RemoveContainers(context.Context, *domain.DevMachine, []string) error
	// FinishedRunContainers counts the machine's finished agent run, check
	// and live inbox writer containers.
	FinishedRunContainers(context.Context, *domain.DevMachine) (int, error)
	// RemoveFinishedRunContainers removes them and their unused inbox
	// volumes. Call it only when no run is unfinished.
	RemoveFinishedRunContainers(context.Context, *domain.DevMachine) error
}
