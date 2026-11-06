package machine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/containerd/errdefs"
	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

type machineSlicesFake struct {
	applied  []int
	removed  int
	applyErr error
}

func (f *machineSlicesFake) Apply(_ context.Context, machine *domain.DevMachine) error {
	if f.applyErr != nil {
		return f.applyErr
	}
	f.applied = append(f.applied, machine.MemoryMB)
	return nil
}

func (f *machineSlicesFake) Remove(context.Context, *domain.DevMachine) error {
	f.removed++
	return nil
}

// sliceDockerClientFake serves the machine network: absent, or labelled as
// created (with or without shared memory).
type sliceDockerClientFake struct {
	*dockerClientFake
	network *network.Inspect
	updates map[string]container.Resources
}

func (f *sliceDockerClientFake) NetworkInspect(context.Context, string, client.NetworkInspectOptions) (client.NetworkInspectResult, error) {
	if f.network == nil {
		return client.NetworkInspectResult{}, errdefs.ErrNotFound
	}
	return client.NetworkInspectResult{Network: *f.network}, nil
}

func (f *sliceDockerClientFake) ContainerUpdate(_ context.Context, id string, options client.ContainerUpdateOptions) (client.ContainerUpdateResult, error) {
	f.updates[id] = *options.Resources
	return client.ContainerUpdateResult{}, nil
}

func sliceMachine() *domain.DevMachine {
	return &domain.DevMachine{
		ID: uuid.New(), WorkspaceID: uuid.New(), RoutingKey: "0123456789abcdef0123", Profile: domain.DevMachineProfileHeadless,
		CPUMillis: 16000, MemoryMB: 16384, PidsLimit: 512,
	}
}

func machineNetwork(machine *domain.DevMachine, shared bool) *network.Inspect {
	labels := machineLabels(machine)
	if shared {
		labels[resourceScopeLabel] = resourceScopeMachine
	}
	inspect := &network.Inspect{}
	inspect.Labels = labels
	return inspect
}

func TestNewMachineSharesItsMemoryOnlyOnceItsSliceIsLimited(t *testing.T) {
	machine := sliceMachine()
	slices := &machineSlicesFake{}
	runtime := &DockerRuntime{client: &sliceDockerClientFake{dockerClientFake: &dockerClientFake{}}, slices: slices}
	labels, err := runtime.prepareMachineSlice(context.Background(), machine, machineNetworkName(machine), machineLabels(machine))
	require.NoError(t, err)
	require.Equal(t, resourceScopeMachine, labels[resourceScopeLabel], "the network records the choice")
	require.Equal(t, []int{16384}, slices.applied, "limits are set before any container runs in the slice")

	failing := &DockerRuntime{client: &sliceDockerClientFake{dockerClientFake: &dockerClientFake{}}, slices: &machineSlicesFake{applyErr: errors.New("systemd unreachable")}}
	labels, err = failing.prepareMachineSlice(context.Background(), machine, machineNetworkName(machine), machineLabels(machine))
	require.NoError(t, err, "the machine still starts")
	require.NotContains(t, labels, resourceScopeLabel, "without a limited slice it keeps separate container limits")

	unsupported := &DockerRuntime{client: &sliceDockerClientFake{dockerClientFake: &dockerClientFake{}}}
	labels, err = unsupported.prepareMachineSlice(context.Background(), machine, machineNetworkName(machine), machineLabels(machine))
	require.NoError(t, err)
	require.NotContains(t, labels, resourceScopeLabel)
}

func TestExistingMachineKeepsItsMemoryModel(t *testing.T) {
	machine := sliceMachine()
	for _, shared := range []bool{false, true} {
		slices := &machineSlicesFake{}
		runtime := &DockerRuntime{client: &sliceDockerClientFake{dockerClientFake: &dockerClientFake{}, network: machineNetwork(machine, shared)}, slices: slices}
		labels, err := runtime.prepareMachineSlice(context.Background(), machine, machineNetworkName(machine), machineLabels(machine))
		require.NoError(t, err)
		require.Equal(t, machineLabels(machine), labels, "an existing network is matched with its creation labels")
		if shared {
			require.Equal(t, []int{16384}, slices.applied, "a host that lost the slice limits gets them again")
		} else {
			require.Empty(t, slices.applied, "machines created with container shares are not moved into a slice")
		}
	}
}

// Production: a 16 GiB headless machine with Docker gave its daemon container
// 6.4 GiB and its agent 5.6 GiB, and a Node build was killed at 6.3 GiB.
func TestSharedMachineWorkloadsMayUseTheWholeMachine(t *testing.T) {
	machine := sliceMachine()
	runtime := &DockerRuntime{client: &sliceDockerClientFake{dockerClientFake: &dockerClientFake{}, network: machineNetwork(machine, true)}, slices: &machineSlicesFake{}}
	hostConfigs := map[string]*container.HostConfig{
		"terminal": runtime.secureHostConfig(machine, "terminal", "", "", true), "agent": runtime.secureHostConfig(machine, "agent", "", "", true),
		"docker": runtime.dockerHostConfig(machine, "", "", ""), "compose": runtime.composeHostConfig(machine, "", 2),
		"collector": runtime.secureHostConfig(machine, "collector", "", "", true), "egress": runtime.secureHostConfig(machine, "egress", "", "", true),
	}
	for service, hostConfig := range hostConfigs {
		share := hostConfig.Resources
		labels := machineLabels(machine)
		require.NoError(t, runtime.scopeMachineResources(context.Background(), machine, service, labels, hostConfig))
		require.Equal(t, "kuayle-0123456789abcdef0123.slice", hostConfig.CgroupParent, service)
		require.Equal(t, *share.PidsLimit, *hostConfig.Resources.PidsLimit, "processes keep the share")
		if service == "collector" || service == "egress" {
			require.Equal(t, share.NanoCPUs, hostConfig.Resources.NanoCPUs, "infrastructure keeps its small limits inside the slice")
			require.Equal(t, share.Memory, hostConfig.Resources.Memory, "infrastructure keeps its small limit inside the slice")
			require.Equal(t, share.MemorySwap, hostConfig.Resources.MemorySwap, service)
			require.NotContains(t, labels, resourceScopeLabel, "its own limit names the container")
			continue
		}
		require.Equal(t, int64(16)<<30, hostConfig.Resources.Memory, service)
		require.Equal(t, int64(16_000_000_000), hostConfig.Resources.NanoCPUs, "%s may use all 16 CPUs; the slice bounds the machine", service)
		require.Equal(t, int64(-1), hostConfig.Resources.MemorySwap, "the slice bounds swap")
		require.Equal(t, resourceScopeMachine, labels[resourceScopeLabel], service)
	}

	split := &DockerRuntime{client: &sliceDockerClientFake{dockerClientFake: &dockerClientFake{}, network: machineNetwork(machine, false)}, slices: &machineSlicesFake{}}
	hostConfig := split.dockerHostConfig(machine, "", "", "")
	labels := machineLabels(machine)
	require.NoError(t, split.scopeMachineResources(context.Background(), machine, "docker", labels, hostConfig))
	require.Empty(t, hostConfig.CgroupParent)
	require.Equal(t, int64(16384)*1024*1024*dockerShare/100, hostConfig.Resources.Memory, "machines with container shares are unchanged")
	require.Equal(t, int64(16000)*1_000_000*dockerShare/100, hostConfig.Resources.NanoCPUs)
	require.NotContains(t, labels, resourceScopeLabel)
}

func TestSharedMachineResizeLimitsTheSliceFirst(t *testing.T) {
	machine := sliceMachine()
	owned := func(id, service string, shared bool) container.Summary {
		labels := machineLabels(machine)
		labels["com.kuayle.service"] = service
		if shared {
			labels[resourceScopeLabel] = resourceScopeMachine
		}
		return container.Summary{ID: id, Labels: labels}
	}
	slices := &machineSlicesFake{}
	dockerClient := &sliceDockerClientFake{dockerClientFake: &dockerClientFake{containers: []container.Summary{
		owned("terminal", "terminal", true), owned("docker", "docker", true), owned("collector", "collector", false),
	}}, updates: map[string]container.Resources{}}
	runtime := &DockerRuntime{client: dockerClient, slices: slices}
	machine.MemoryMB, machine.CPUMillis = 32768, 8000
	require.NoError(t, runtime.UpdateResources(context.Background(), machine))
	require.Equal(t, []int{32768}, slices.applied)
	require.Equal(t, int64(32)<<30, dockerClient.updates["terminal"].Memory)
	require.Equal(t, int64(32)<<30, dockerClient.updates["docker"].Memory)
	require.Equal(t, int64(-1), dockerClient.updates["docker"].MemorySwap)
	require.Equal(t, int64(8_000_000_000), dockerClient.updates["docker"].NanoCPUs)
	require.Equal(t, int64(8_000_000_000), dockerClient.updates["terminal"].NanoCPUs)
	require.Equal(t, int64(32768)*1024*1024*5/100, dockerClient.updates["collector"].Memory)
	require.Equal(t, int64(8000)*1_000_000*5/100, dockerClient.updates["collector"].NanoCPUs)

	runtime.slices = nil
	dockerClient.updates = map[string]container.Resources{}
	require.Error(t, runtime.UpdateResources(context.Background(), machine), "a shared machine is not resized without its slice")
	require.Empty(t, dockerClient.updates)
}

func TestSharedMachineMemoryKillNamesTheMachineLimit(t *testing.T) {
	machine := &domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New(), Status: domain.DevMachineStatusRunning}
	labels := machineLabels(machine)
	labels["com.kuayle.service"] = "docker"
	labels[resourceScopeLabel] = resourceScopeMachine
	runtime := &DockerRuntime{client: &inspectLimitClientFake{dockerClientFake: &dockerClientFake{}, memory: 16 << 30}}
	hit, ok := runtime.resourceLimitHit(context.Background(), events.Message{Actor: events.Actor{ID: "docker-1", Attributes: labels}})
	require.True(t, ok)
	require.True(t, hit.MachineMemory)

	store := &managerStoreFake{machine: machine}
	(&Manager{store: store}).recordMemoryLimit(context.Background(), hit, time.Now())
	require.Equal(t, []string{"memory: Out of memory: the machine reached its 16 GiB memory limit and a process in the Docker daemon container was stopped. Resize the machine."}, store.resourceLimits)

	agentStore := runOutOfMemoryAgent(t, &AgentExecution{ContainerID: "agent", ExitCode: 137, OOMKilled: true, MemoryLimitBytes: 4 << 30, MachineMemory: true}, true)
	require.Equal(t, "Out of memory: the machine reached its 4 GiB memory limit and a process in the agent container was stopped. Resize the machine or raise the task machine memory.", *agentStore.completedRun.ErrorMessage)
}

type inspectLimitClientFake struct {
	*dockerClientFake
	memory int64
}

func (f *inspectLimitClientFake) ContainerInspect(context.Context, string, client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	return client.ContainerInspectResult{Container: container.InspectResponse{HostConfig: &container.HostConfig{Resources: container.Resources{Memory: f.memory}}}}, nil
}

func TestMachineSliceNamesOnlyPlainRoutingKeys(t *testing.T) {
	name, ok := machineSliceName(&domain.DevMachine{RoutingKey: "0123456789abcdef0123"})
	require.True(t, ok)
	require.Equal(t, "kuayle-0123456789abcdef0123.slice", name)
	for _, key := range []string{"", "a-b", "A6", "../x"} {
		_, ok := machineSliceName(&domain.DevMachine{RoutingKey: key})
		require.False(t, ok, key)
	}
}
