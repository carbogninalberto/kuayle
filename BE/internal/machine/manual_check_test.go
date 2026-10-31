package machine

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	cryptoutil "github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

type manualCheckStoreFake struct {
	*validationStoreFake
	services []domain.DevMachineService
	failures []string
}

func (f *manualCheckStoreFake) CreateRuntimeService(_ context.Context, service *domain.DevMachineService) error {
	f.services = append(f.services, *service)
	return nil
}
func (f *manualCheckStoreFake) FailManualCheck(_ context.Context, _ domain.DevMachineOperation, summary string) error {
	f.failures = append(f.failures, summary)
	return nil
}

func manualCheckManager(t *testing.T, exit int) (*Manager, *manualCheckStoreFake, *validationRuntimeFake, *domain.DevMachine, *domain.DevMachineOperation) {
	t.Helper()
	taskID, checkoutID, runID := uuid.New(), uuid.New(), uuid.New()
	machine := &domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New(), RoutingKey: "fixture"}
	key := make([]byte, 32)
	encrypted, err := cryptoutil.Encrypt(`{"FIXTURE_CREDENTIAL":"fixture-private-value"}`, key)
	require.NoError(t, err)
	base := &validationStoreFake{managerStoreFake: &managerStoreFake{machine: machine, agentRun: &domain.DevMachineAgentRun{ID: runID, TaskID: &taskID, CheckoutID: &checkoutID, EncryptedOutputSecrets: &encrypted, MaxRuntimeSeconds: 60}, checkout: &domain.DevMachineCheckout{ID: checkoutID, MachineID: machine.ID, WorkspaceID: machine.WorkspaceID, ScopeKey: taskID.String(), Status: "ready"}, services: []domain.DevMachineService{{ServiceType: "terminal", ImageRef: "developer:fixture"}}}, step: domain.DevMachineAgentRunStep{ID: uuid.New(), AgentRunID: runID, StepType: "manual_check", Status: "queued", CreatedAt: time.Now(), CommandArgv: []byte(`["sh","-c","npm test -- auth"]`)}}
	store := &manualCheckStoreFake{validationStoreFake: base}
	runtime := &validationRuntimeFake{runtimeFake: &runtimeFake{}, exit: exit}
	manager := &Manager{store: store, runtime: runtime, encryptionKey: key}
	return manager, store, runtime, machine, &domain.DevMachineOperation{Action: domain.DevMachineOpRunCheck, AgentRunID: &runID, AgentTaskID: &taskID}
}

func TestManualCheckReusesTheCheckContainerWithoutAnAttempt(t *testing.T) {
	manager, store, runtime, machine, op := manualCheckManager(t, 1)
	require.NoError(t, manager.processOperation(context.Background(), op))
	require.Equal(t, 1, runtime.calls, "the check container runs the command once")
	require.Equal(t, "developer:fixture", runtime.image)
	require.Zero(t, runtime.agentRuns, "no agent attempt starts")
	require.Equal(t, 1, *store.exit, "the process exit decides the result")
	require.Len(t, store.services, 1)
	require.Equal(t, "check-"+store.step.ID.String(), store.services[0].ServiceKey, "each manual check has its own container")
	require.Equal(t, "kuayle-"+machine.RoutingKey+"-check-"+store.step.ID.String(), store.services[0].ContainerName)
	require.Equal(t, []uuid.UUID{store.step.ID}, runtime.removedChecks, "a recorded check is not kept for replay")
	require.NotEmpty(t, store.validationChunks)
	for _, chunk := range store.validationChunks {
		require.NotContains(t, chunk.Content, "fixture-private-value")
		require.Equal(t, store.step.ID, *chunk.ServiceID)
	}
}

func TestManualCheckRecordsAStaleOperationAsFailed(t *testing.T) {
	manager, store, runtime, machine, op := manualCheckManager(t, 0)
	machine.Generation = 2
	op.Generation = 1
	err := manager.processOperation(context.Background(), op)
	var terminal *terminalOperationError
	require.True(t, errors.As(err, &terminal), "a stale check is not retried")
	require.Zero(t, runtime.calls)
	require.Equal(t, "The machine changed before the check ran", manualCheckFailure(err))
	require.Nil(t, store.exit)
}

func TestManualCheckContainerIsNamedPerStepAndGetsNoCredentials(t *testing.T) {
	machine := &domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New(), RoutingKey: "fixture", DockerNetworkName: ptr("network"), WorkspaceVolumeName: ptr("workspace"), CPUMillis: 1000, MemoryMB: 2048, PidsLimit: 256}
	run := &domain.DevMachineAgentRun{ID: uuid.New()}
	step := &domain.DevMachineAgentRunStep{ID: uuid.New(), StepType: "manual_check", CommandArgv: json.RawMessage(`["sh","-c","npm test -- auth"]`)}
	checkout := &domain.DevMachineCheckout{WorkspacePath: "/workspace/tasks/fixture"}
	var input struct {
		container.Config
		HostConfig       container.HostConfig
		NetworkingConfig struct {
			EndpointsConfig map[string]struct{ Aliases []string }
		}
	}
	var inspected, created []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/images/"):
			_, _ = w.Write([]byte(`{"Id":"image","Config":{}}`))
		case strings.HasSuffix(r.URL.Path, "/json"):
			inspected = append(inspected, r.URL.Path)
			http.NotFound(w, r)
		case strings.HasSuffix(r.URL.Path, "/containers/create"):
			created = append(created, r.URL.Query().Get("name"))
			require.NoError(t, json.NewDecoder(r.Body).Decode(&input))
			_, _ = w.Write([]byte(`{"Id":"check"}`))
		case strings.HasSuffix(r.URL.Path, "/start"):
			w.WriteHeader(204)
		case strings.HasSuffix(r.URL.Path, "/archive"):
			http.Error(w, "credentials must not be copied", 500)
		case strings.HasSuffix(r.URL.Path, "/wait"):
			_, _ = w.Write([]byte(`{"StatusCode":0}`))
		case strings.HasSuffix(r.URL.Path, "/logs"):
			w.WriteHeader(200)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	docker, err := client.New(client.WithHost(server.URL), client.WithAPIVersion("1.52"))
	require.NoError(t, err)
	defer docker.Close()
	result, err := (&DockerRuntime{client: docker}).RunValidation(context.Background(), machine, run, step, 0, checkout, "developer:fixture", &AgentObserver{})
	require.NoError(t, err)
	require.Zero(t, result.ExitCode)
	name := "kuayle-fixture-check-" + step.ID.String()
	require.Equal(t, []string{name}, created, "never the attempt's retained validation container")
	for _, path := range inspected {
		require.NotContains(t, path, "-validation-")
	}
	require.Equal(t, []string{"check-" + step.ID.String()}, input.NetworkingConfig.EndpointsConfig["network"].Aliases)
	require.Equal(t, step.ID.String(), input.Labels["com.kuayle.validation-id"])
	require.Equal(t, []string{"/bin/sh"}, input.Entrypoint)
	require.Equal(t, []string{"-c", checkRunner, "kuayle-check", "sh", "-c", "npm test -- auth"}, input.Cmd)
	require.True(t, input.HostConfig.ReadonlyRootfs)
	require.Contains(t, input.HostConfig.CapDrop, "ALL")
	require.Len(t, input.HostConfig.Mounts, 1, "only the workspace volume, no credential files")
	for _, env := range input.Env {
		require.Regexp(t, `^(HOME|CI|HTTP_PROXY|HTTPS_PROXY|NO_PROXY|KUAYLE_RUN_TMP|TMPDIR|TMP|TEMP|GOTMPDIR|GOCACHE|GOMODCACHE|XDG_CACHE_HOME|npm_config_cache|PIP_CACHE_DIR|YARN_CACHE_FOLDER)=`, env)
	}
	require.Contains(t, input.Env, "TMPDIR=/workspace/.kuayle/tmp/check-"+step.ID.String()+"/tmp", "temporary files go to the workspace volume, not the small /tmp")
}
