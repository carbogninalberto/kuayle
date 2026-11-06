package machine

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

type statsDockerClientFake struct {
	*dockerClientFake
	memory  map[string]uint64
	queried []string
}

func (f *statsDockerClientFake) ContainerStats(_ context.Context, id string, _ client.ContainerStatsOptions) (client.ContainerStatsResult, error) {
	f.queried = append(f.queried, id)
	var stats container.StatsResponse
	stats.MemoryStats.Usage = f.memory[id]
	stats.CPUStats.CPUUsage.TotalUsage, stats.CPUStats.SystemUsage, stats.CPUStats.OnlineCPUs = 200, 1000, 4
	body, _ := json.Marshal(stats)
	return client.ContainerStatsResult{Body: io.NopCloser(bytes.NewReader(body))}, nil
}

// Agent runs, checks and check services run in their own containers; the
// machine's usage history counts them with its services.
func TestStatsCountsEveryRunningContainerOfTheMachine(t *testing.T) {
	machine := &domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New(), RoutingKey: "abc"}
	owned := func(id, service string) container.Summary {
		labels := machineLabels(machine)
		labels["com.kuayle.service"] = service
		return container.Summary{ID: id, Labels: labels, State: container.StateRunning}
	}
	foreign := owned("foreign", "agent")
	foreign.Labels["com.kuayle.machine-id"] = uuid.NewString()
	ptr := func(value string) *string { return &value }
	dockerClient := &statsDockerClientFake{dockerClientFake: &dockerClientFake{containers: []container.Summary{
		owned("terminal", "terminal"), owned("agent-1", "agent"), owned("db", "compose"), foreign,
	}}, memory: map[string]uint64{"terminal": 1 << 30, "agent-1": 3 << 30, "db": 1 << 29, "foreign": 8 << 30}}
	runtime := &DockerRuntime{client: dockerClient}
	usage, err := runtime.Stats(context.Background(), machine, []domain.DevMachineService{
		{ServiceType: "terminal", Status: "running", ContainerID: ptr("terminal")},
	})
	require.NoError(t, err)
	require.Equal(t, int64(1<<30+3<<30+1<<29), usage.MemoryBytes)
	require.InDelta(t, 3*80.0, usage.CPUPercent, 0.001)
	require.ElementsMatch(t, []string{"terminal", "agent-1", "db"}, dockerClient.queried, "each container once, never another machine's")
	require.True(t, dockerClient.containerListOptions.Filters["status"]["running"], "only running containers are sampled")
}
