package machine

import (
	"context"

	"github.com/kuayle/kuayle-backend/internal/domain"
)

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

type GitState struct {
	Branch       string
	Commits      []string
	ChangedFiles []string
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
	Spawn(context.Context, *domain.DevMachine, []domain.DevMachineService, map[string]map[string]string) (string, string, map[string]string, error)
	Start(context.Context, *domain.DevMachine, []domain.DevMachineService, map[string]map[string]string) error
	Pause(context.Context, *domain.DevMachine, []domain.DevMachineService) error
	Stop(context.Context, *domain.DevMachine, []domain.DevMachineService) error
	Teardown(context.Context, *domain.DevMachine, []domain.DevMachineService) error
	RunAgent(context.Context, *domain.DevMachine, *domain.DevMachineAgentRun, *domain.DevMachineAgentProvider, *domain.DevMachineCheckout, map[string]string) (*AgentExecution, error)
	CancelAgent(context.Context, *domain.DevMachineAgentRun) error
	TerminateTerminal(context.Context, *domain.DevMachine, []domain.DevMachineService, *domain.DevMachineTerminalSession) error
	Inspect(context.Context, *domain.DevMachine, []domain.DevMachineService) (RuntimeInspection, error)
	Stats(context.Context, *domain.DevMachine, []domain.DevMachineService) (ResourceUsage, error)
	GitState(context.Context, *domain.DevMachine, []domain.DevMachineService, *domain.DevMachineCheckout) (GitState, error)
	PrepareCheckout(context.Context, *domain.DevMachine, []domain.DevMachineService, *domain.DevMachineCheckout, string) error
	SnapshotEnvironment(context.Context, *domain.DevMachine, []domain.DevMachineService, *domain.DevMachineEnvironment) (string, error)
	DeleteEnvironmentImage(context.Context, *domain.DevMachineEnvironment) error
	Ping(context.Context) error
}
