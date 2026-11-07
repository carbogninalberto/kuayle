package machine

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

const mib = int64(1024 * 1024)

// Tasks sharing a machine split the agent share of its memory instead of each
// getting all of it: ten agents with half of a 16 GiB machine each could ask
// for 80 GiB and the kernel would kill them (exit code 137). Each keeps at
// least agentMinimumMemoryBytes, never more than the undivided share.
func TestAgentMemoryFollowsTheActiveTasksOnTheMachine(t *testing.T) {
	headless := &domain.DevMachine{Profile: domain.DevMachineProfileHeadless, MemoryMB: 16384}
	standard := &domain.DevMachine{Profile: domain.DevMachineProfileStandard, MemoryMB: 16384}
	small := &domain.DevMachine{Profile: domain.DevMachineProfileHeadless, MemoryMB: 2048}
	for _, test := range []struct {
		name     string
		machine  *domain.DevMachine
		docker   bool
		tasks    int
		expected int64
	}{
		{"one task keeps half of a headless machine", headless, false, 1, 8192 * mib},
		{"no count is one task", headless, false, 0, 8192 * mib},
		{"two tasks split it", headless, false, 2, 4096 * mib},
		{"four tasks get a quarter each", headless, false, 4, 2048 * mib},
		{"ten tasks keep the minimum", headless, false, 10, agentMinimumMemoryBytes},
		{"a daemon leaves 35 percent to split", headless, true, 2, 16384 * mib * 35 / 100 / 2},
		{"a standard machine splits its agent share", standard, false, 3, 16384 * mib * 35 / 100 / 3},
		{"the minimum never exceeds the undivided share", small, false, 4, 1024 * mib},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.expected, agentMemoryBytes(test.machine, test.docker, test.tasks))
		})
	}
	require.Equal(t, 1024*mib, int64(agentMinimumMemoryBytes))
}

// The total of the agent limits on a busy machine stays near the agent share
// instead of growing with every task.
func TestAgentMemoryLimitsDoNotAddUpToManyMachines(t *testing.T) {
	machine := &domain.DevMachine{Profile: domain.DevMachineProfileHeadless, MemoryMB: 16384}
	for tasks := 1; tasks <= domain.MaxTaskMachineMaxTasks; tasks++ {
		total := agentMemoryBytes(machine, false, tasks) * int64(tasks)
		require.LessOrEqual(t, total, machineMemoryBytes(machine)*10/16, "%d tasks", tasks)
	}
}

// A new agent container counts the tasks whose attempt or check already runs
// on the machine (one each, whatever their containers) and itself.
func TestAgentContainerSplitsMemoryWithRunningTasks(t *testing.T) {
	machine := &domain.DevMachine{
		ID: uuid.New(), WorkspaceID: uuid.New(), RoutingKey: "fixture", Profile: domain.DevMachineProfileHeadless,
		CPUMillis: 16000, MemoryMB: 16384, PidsLimit: 4096, DockerNetworkName: ptr("network"), WorkspaceVolumeName: ptr("volume"),
	}
	run := &domain.DevMachineAgentRun{ID: uuid.New(), Mode: "autonomous", CommandArgv: []byte(`["codex"]`)}
	agentContainer := func(id, runID string, extra ...string) container.Summary {
		labels := machineLabels(machine)
		labels["com.kuayle.service"] = "agent"
		labels["com.kuayle.agent-run-id"] = runID
		for index := 0; index+1 < len(extra); index += 2 {
			labels[extra[index]] = extra[index+1]
		}
		return container.Summary{ID: id, Labels: labels, State: container.StateRunning}
	}
	first, second := uuid.NewString(), uuid.NewString()
	foreign := agentContainer("foreign", uuid.NewString())
	foreign.Labels["com.kuayle.machine-id"] = uuid.NewString()
	running := []container.Summary{
		agentContainer("first", first),
		agentContainer("first-check", first, "com.kuayle.validation-id", uuid.NewString()),
		agentContainer("second", second),
		foreign,
	}
	var listFilters []string
	var created struct {
		HostConfig container.HostConfig
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/images/"):
			_, _ = w.Write([]byte(`{"Id":"image","Config":{}}`))
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			listFilters = append(listFilters, r.URL.Query().Get("filters"))
			require.Empty(t, r.URL.Query().Get("all"), "only running containers count")
			require.NoError(t, json.NewEncoder(w).Encode(running))
		case strings.HasSuffix(r.URL.Path, "/containers/create"):
			require.NoError(t, json.NewDecoder(r.Body).Decode(&created))
			_, _ = w.Write([]byte(`{"Id":"native"}`))
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	docker, err := client.New(client.WithHost(server.URL), client.WithAPIVersion("1.52"))
	require.NoError(t, err)
	defer docker.Close()

	stop := errors.New("stop after create")
	_, err = (&DockerRuntime{client: docker}).RunAgent(context.Background(), machine, run, &domain.DevMachineAgentProvider{ImageRef: "fixture"}, nil, nil, &AgentObserver{Authorize: func() error { return stop }})
	require.ErrorIs(t, err, stop)

	require.Len(t, listFilters, 1)
	require.Contains(t, listFilters[0], "com.kuayle.machine-id="+machine.ID.String())
	require.Contains(t, listFilters[0], "com.kuayle.service=agent")
	expected := agentMemoryBytes(machine, false, 3)
	require.Equal(t, 16384*mib*50/100/3, expected)
	require.Equal(t, expected, created.HostConfig.Memory, "two running tasks and this one share the agent memory")
	require.Equal(t, expected, created.HostConfig.MemorySwap)
	undivided := (&DockerRuntime{}).secureHostConfig(machine, "agent", "network", "volume", false).Resources
	require.Equal(t, undivided.NanoCPUs, created.HostConfig.NanoCPUs, "CPU keeps the agent share")
	require.Equal(t, *undivided.PidsLimit, *created.HostConfig.PidsLimit, "the process limit keeps its share")
}

// Resizing a machine applies the same split to the agent and check
// containers of the tasks running there.
func TestUpdateResourcesSplitsAgentMemoryBetweenRunningTasks(t *testing.T) {
	machine := &domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New(), Profile: domain.DevMachineProfileHeadless, CPUMillis: 8000, MemoryMB: 16384, PidsLimit: 4096}
	agentContainer := func(id, runID string, state container.ContainerState, extra ...string) container.Summary {
		labels := machineLabels(machine)
		labels["com.kuayle.service"] = "agent"
		labels["com.kuayle.agent-run-id"] = runID
		for index := 0; index+1 < len(extra); index += 2 {
			labels[extra[index]] = extra[index+1]
		}
		return container.Summary{ID: id, Labels: labels, State: state}
	}
	first, second, finished := uuid.NewString(), uuid.NewString(), uuid.NewString()
	dockerClient := &resizeDockerClientFake{dockerClientFake: &dockerClientFake{containers: []container.Summary{
		agentContainer("first", first, container.StateRunning),
		agentContainer("second", second, container.StateRunning),
		agentContainer("second-check", second, container.StateRunning, "com.kuayle.validation-id", uuid.NewString()),
		agentContainer("finished", finished, container.StateExited),
	}}, updates: map[string]container.Resources{}}
	require.NoError(t, (&DockerRuntime{client: dockerClient}).UpdateResources(context.Background(), machine))
	half := agentMemoryBytes(machine, false, 2)
	require.Equal(t, 16384*mib*50/100/2, half)
	require.Equal(t, half, dockerClient.updates["first"].Memory)
	require.Equal(t, half, dockerClient.updates["second"].Memory)
	require.Equal(t, agentMemoryBytes(machine, true, 2), dockerClient.updates["second-check"].Memory, "a check keeps the share of a machine with a daemon")
	require.Equal(t, half, dockerClient.updates["finished"].Memory, "a stopped container restarts within the split")
}
