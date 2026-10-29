package machine

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/moby/moby/api/types/container"
	volumetypes "github.com/moby/moby/api/types/volume"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

type resizeDockerClientFake struct {
	*dockerClientFake
	updates map[string]container.Resources
}

func (f *resizeDockerClientFake) ContainerUpdate(_ context.Context, id string, options client.ContainerUpdateOptions) (client.ContainerUpdateResult, error) {
	f.updates[id] = *options.Resources
	return client.ContainerUpdateResult{}, nil
}

func TestUpdateResourcesAppliesEachContainerShareOfTheNewSize(t *testing.T) {
	machine := &domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New(), RoutingKey: "abc", CPUMillis: 8000, MemoryMB: 16384, PidsLimit: 512}
	owned := func(id, service string, extra ...string) container.Summary {
		labels := machineLabels(machine)
		labels["com.kuayle.service"] = service
		for index := 0; index+1 < len(extra); index += 2 {
			labels[extra[index]] = extra[index+1]
		}
		return container.Summary{ID: id, Labels: labels}
	}
	run := uuid.NewString()
	foreign := owned("foreign", "ide")
	foreign.Labels = map[string]string{"com.kuayle.managed": "true", "com.kuayle.machine-id": uuid.NewString(), "com.kuayle.service": "ide"}
	otherWorkspace := owned("other-workspace", "ide")
	otherWorkspace.Labels["com.kuayle.workspace-id"] = uuid.NewString()
	dockerClient := &resizeDockerClientFake{dockerClientFake: &dockerClientFake{containers: []container.Summary{
		owned("ide", "ide"), owned("collector", "collector"), owned("egress", "egress"), owned("browser", "browser"),
		owned("agent", "agent", "com.kuayle.agent-run-id", run),
		owned("validation", "agent", "com.kuayle.agent-run-id", run, "com.kuayle.validation-id", uuid.NewString()),
		owned("db", "compose", "com.kuayle.agent-run-id", run), owned("cache", "compose", "com.kuayle.agent-run-id", run),
		owned("docker", "docker"), owned("volume-init", "volume-init"), foreign, otherWorkspace,
	}}, updates: map[string]container.Resources{}}
	runtime := &DockerRuntime{client: dockerClient}

	require.NoError(t, runtime.UpdateResources(context.Background(), machine))

	require.Contains(t, dockerClient.containerListOptions.Filters["label"], "com.kuayle.machine-id="+machine.ID.String())
	require.True(t, dockerClient.containerListOptions.All, "stopped containers start with the new limits")
	expected := map[string]container.Resources{}
	for _, service := range []string{"ide", "collector", "egress", "browser", "agent"} {
		expected[service] = runtime.secureHostConfig(machine, service, "", "").Resources
	}
	expected["validation"] = expected["agent"]
	expected["db"] = runtime.composeHostConfig(machine, "", 2).Resources
	expected["cache"] = expected["db"]
	expected["docker"] = runtime.dockerHostConfig(machine, "", "", "").Resources
	require.Len(t, dockerClient.updates, len(expected), "helpers and other machines' containers are untouched")
	for id, resources := range expected {
		update := dockerClient.updates[id]
		require.Equal(t, resources.NanoCPUs, update.NanoCPUs, id)
		require.Equal(t, resources.Memory, update.Memory, id)
		require.Equal(t, resources.MemorySwap, update.MemorySwap, id)
		require.Equal(t, *resources.PidsLimit, *update.PidsLimit, id)
		require.Empty(t, update.Devices, "only limits are updated")
	}
	require.Equal(t, int64(16384)*1024*1024*35/100, dockerClient.updates["agent"].Memory)
}

func TestMonitoredVolumeAcceptsResizedDiskLimit(t *testing.T) {
	ownership := map[string]string{"com.kuayle.managed": "true", "com.kuayle.machine-id": uuid.NewString()}
	created, err := workspaceVolumeCreateOptions("workspace-test", ownership, 20, "monitored")
	require.NoError(t, err)
	existing := volumetypes.Volume{Driver: created.Driver, Labels: created.Labels, Options: created.DriverOpts}
	resized, err := workspaceVolumeCreateOptions("workspace-test", ownership, 60, "monitored")
	require.NoError(t, err)
	require.NoError(t, validateWorkspaceVolume(existing, resized), "monitoring reads the stored machine size")

	hardCreated, err := workspaceVolumeCreateOptions("workspace-test", ownership, 20)
	require.NoError(t, err)
	hardResized, err := workspaceVolumeCreateOptions("workspace-test", ownership, 60)
	require.NoError(t, err)
	require.ErrorContains(t, validateWorkspaceVolume(volumetypes.Volume{Driver: hardCreated.Driver, Labels: hardCreated.Labels, Options: hardCreated.DriverOpts}, hardResized), "incompatible label",
		"a hard quota cannot change after creation")
}
