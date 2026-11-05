package machine

import (
	"context"
	"sort"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

type removeDockerClientFake struct {
	*dockerClientFake
	removed []string
}

func (f *removeDockerClientFake) ContainerRemove(_ context.Context, id string, options client.ContainerRemoveOptions) (client.ContainerRemoveResult, error) {
	if !options.Force {
		return client.ContainerRemoveResult{}, nil
	}
	f.removed = append(f.removed, id)
	return client.ContainerRemoveResult{}, nil
}

func imageRemovalFixture() (*domain.DevMachine, *removeDockerClientFake, *DockerRuntime) {
	machine := &domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New(), RoutingKey: "abc"}
	owned := func(id, service string, state container.ContainerState, extra ...string) container.Summary {
		labels := machineLabels(machine)
		labels["com.kuayle.service"] = service
		for index := 0; index+1 < len(extra); index += 2 {
			labels[extra[index]] = extra[index+1]
		}
		return container.Summary{ID: id, Labels: labels, State: state}
	}
	run := uuid.NewString()
	foreign := owned("foreign", "agent", container.StateExited, "com.kuayle.agent-run-id", run)
	foreign.Labels["com.kuayle.machine-id"] = uuid.NewString()
	otherWorkspace := owned("other-workspace", "terminal", container.StateRunning)
	otherWorkspace.Labels["com.kuayle.workspace-id"] = uuid.NewString()
	dockerClient := &removeDockerClientFake{dockerClientFake: &dockerClientFake{containers: []container.Summary{
		owned("terminal", "terminal", container.StateRunning), owned("collector", "collector", container.StateRunning),
		owned("finished-agent", "agent", container.StateExited, "com.kuayle.agent-run-id", run),
		owned("finished-check", "agent", container.StateExited, "com.kuayle.agent-run-id", run, "com.kuayle.validation-id", uuid.NewString()),
		owned("running-agent", "agent", container.StateRunning, "com.kuayle.agent-run-id", uuid.NewString()),
		owned("compose", "compose", container.StateExited, "com.kuayle.agent-run-id", run),
		foreign, otherWorkspace,
	}}}
	return machine, dockerClient, &DockerRuntime{client: dockerClient}
}

func TestRemoveContainersRemovesOnlyTheMachinesListedContainers(t *testing.T) {
	machine, dockerClient, runtime := imageRemovalFixture()

	require.NoError(t, runtime.RemoveContainers(context.Background(), machine, []string{"terminal", "collector", "foreign", "other-workspace", "gone"}))

	sort.Strings(dockerClient.removed)
	require.Equal(t, []string{"collector", "terminal"}, dockerClient.removed, "other machines' and workspaces' containers are never removed")
	require.Contains(t, dockerClient.containerListOptions.Filters["label"], "com.kuayle.machine-id="+machine.ID.String())
	require.True(t, dockerClient.containerListOptions.All)
}

func TestRemoveFinishedRunContainersKeepsServicesAndRunningRuns(t *testing.T) {
	machine, dockerClient, runtime := imageRemovalFixture()

	require.NoError(t, runtime.RemoveFinishedRunContainers(context.Background(), machine))

	sort.Strings(dockerClient.removed)
	require.Equal(t, []string{"finished-agent", "finished-check"}, dockerClient.removed)
}
