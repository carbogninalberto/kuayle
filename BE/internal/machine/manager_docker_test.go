package machine

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
	cryptoutil "github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/stretchr/testify/require"
)

// dockerStoreFake scopes env vars by target like the repository and reports
// the workspace's Docker switch.
type dockerStoreFake struct {
	*managerStoreFake
	dockerAllowed bool
}

func (f *dockerStoreFake) ListEnvVarsInternal(_ context.Context, _ uuid.UUID, _ *string, target string) ([]domain.DevMachineEnvVar, error) {
	matched := []domain.DevMachineEnvVar{}
	for _, envVar := range f.envVars {
		if target == "" || envVar.TargetService == target {
			matched = append(matched, envVar)
		}
	}
	return matched, nil
}

func (f *dockerStoreFake) DockerAllowed(context.Context, uuid.UUID) (bool, error) {
	return f.dockerAllowed, nil
}

func dockerSecretFixture(t *testing.T, key []byte, machineID uuid.UUID) []domain.DevMachineEnvVar {
	t.Helper()
	values := []struct{ target, name, value string }{
		{"docker", "KUAYLE_DOCKER_TLS_CA", "ca"}, {"docker", "KUAYLE_DOCKER_TLS_CERT", "server-cert"}, {"docker", "KUAYLE_DOCKER_TLS_KEY", "server-key"},
		{"docker-client", "KUAYLE_DOCKER_CA", "ca"}, {"docker-client", "KUAYLE_DOCKER_CERT", "client-cert"}, {"docker-client", "KUAYLE_DOCKER_KEY", "client-key"},
		{"collector", "KUAYLE_MACHINE_TOKEN", "collector-token"},
	}
	envVars := []domain.DevMachineEnvVar{}
	for _, value := range values {
		encrypted, err := cryptoutil.Encrypt(value.value, key)
		require.NoError(t, err)
		envVars = append(envVars, domain.DevMachineEnvVar{ID: uuid.New(), MachineID: machineID, TargetService: value.target, Name: value.name, EncryptedValue: encrypted, IsSecret: true})
	}
	return envVars
}

// The daemon receives only its server TLS material; developer containers the
// client material; collector, browser and egress neither.
func TestManagerDeliversDockerTLSOnlyToTheDaemonAndDeveloperContainers(t *testing.T) {
	key := cryptoutil.DeriveKey("docker-secrets-test")
	machine := &domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New(), RoutingKey: "0123456789abcdef0123"}
	store := &dockerStoreFake{managerStoreFake: &managerStoreFake{machine: machine, envVars: dockerSecretFixture(t, key, machine.ID)}}
	manager := NewManager(store, &runtimeFake{}, agent.NewRegistry(), nil, key, nil, "test")
	services := []domain.DevMachineService{
		{ServiceKey: "ide", ServiceType: "ide"}, {ServiceKey: "terminal", ServiceType: "terminal"},
		{ServiceKey: "collector", ServiceType: "collector"}, {ServiceKey: "browser", ServiceType: "browser"},
		{ServiceKey: "egress", ServiceType: "egress"}, {ServiceKey: "docker", ServiceType: "docker"},
	}
	secrets, err := manager.serviceSecrets(context.Background(), machine, services)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"KUAYLE_DOCKER_TLS_CA": "ca", "KUAYLE_DOCKER_TLS_CERT": "server-cert", "KUAYLE_DOCKER_TLS_KEY": "server-key"}, secrets["docker"])
	for _, developer := range []string{"ide", "terminal"} {
		require.Equal(t, "client-key", secrets[developer]["KUAYLE_DOCKER_KEY"], developer)
		require.Equal(t, "client-cert", secrets[developer]["KUAYLE_DOCKER_CERT"], developer)
		require.Equal(t, "ca", secrets[developer]["KUAYLE_DOCKER_CA"], developer)
		require.NotContains(t, secrets[developer], "KUAYLE_DOCKER_TLS_KEY", developer)
	}
	for _, other := range []string{"collector", "browser", "egress"} {
		for name := range secrets[other] {
			require.NotContains(t, name, "DOCKER", other)
		}
	}

	withoutDaemon, err := manager.serviceSecrets(context.Background(), machine, services[:5])
	require.NoError(t, err)
	require.NotContains(t, withoutDaemon["ide"], "KUAYLE_DOCKER_KEY", "no client material without a daemon")
}

// An autonomous attempt on a machine with a daemon gets the client material
// and the daemon address in its sealed secrets; the key is redacted from its
// output.
func TestAgentRunsOnAMachineWithADaemonGetItsClientCertificates(t *testing.T) {
	key := cryptoutil.DeriveKey("docker-agent-test")
	machineID, workspaceID, runID := uuid.New(), uuid.New(), uuid.New()
	run := &domain.DevMachineAgentRun{ID: runID, MachineID: machineID, WorkspaceID: workspaceID, ProviderID: "opencode", Mode: "autonomous",
		Status: domain.DevMachineAgentRunStatusQueued, AllowedSecrets: json.RawMessage(`[]`), CommandArgv: json.RawMessage(`["opencode"]`), MaxRuntimeSeconds: 30}
	machine := &domain.DevMachine{ID: machineID, WorkspaceID: workspaceID, RoutingKey: "0123456789abcdef0123"}
	store := &dockerStoreFake{managerStoreFake: &managerStoreFake{machine: machine, agentRun: run, envVars: dockerSecretFixture(t, key, machineID),
		services: []domain.DevMachineService{{ServiceKey: "ide", ServiceType: "ide"}, {ServiceKey: "docker", ServiceType: "docker"}}}}
	manager := NewManager(store, &runtimeFake{}, agent.NewRegistry(), nil, key, nil, "test")
	snapshot, err := manager.agentRunSecrets(context.Background(), machine, run, nil)
	require.NoError(t, err)
	require.Equal(t, "client-key", snapshot.Environment["KUAYLE_DOCKER_KEY"])
	require.Equal(t, "client-cert", snapshot.Environment["KUAYLE_DOCKER_CERT"])
	require.Equal(t, "ca", snapshot.Environment["KUAYLE_DOCKER_CA"])
	require.Equal(t, "tcp://0123456789abcdef0123-docker:2376", snapshot.Environment["KUAYLE_DOCKER_HOST"])
	require.Equal(t, "client-key", snapshot.Redactions["KUAYLE_DOCKER_KEY"])
	require.NotContains(t, snapshot.Environment, "KUAYLE_DOCKER_TLS_KEY")

	run.EncryptedOutputSecrets = nil
	store.services = store.services[:1]
	snapshot, err = manager.agentRunSecrets(context.Background(), machine, run, nil)
	require.NoError(t, err)
	require.NotContains(t, snapshot.Environment, "KUAYLE_DOCKER_HOST", "no daemon, no client material")
}

// A machine created with a daemon does not start once the workspace turned
// Docker off; it is not silently started without it either.
func TestStartingAMachineWithADaemonIsRefusedWhenDockerIsNoLongerAllowed(t *testing.T) {
	for _, action := range []domain.DevMachineOperationAction{domain.DevMachineOpSpawn, domain.DevMachineOpStart} {
		machine := &domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New(), RoutingKey: "0123456789abcdef0123", Status: domain.DevMachineStatusStopped, DesiredStatus: domain.DevMachineStatusRunning, Generation: 1}
		store := &dockerStoreFake{managerStoreFake: &managerStoreFake{machine: machine, services: []domain.DevMachineService{{ServiceKey: "ide", ServiceType: "ide"}, {ServiceKey: "docker", ServiceType: "docker"}}}}
		runtime := &runtimeFake{}
		manager := NewManager(store, runtime, agent.NewRegistry(), nil, cryptoutil.DeriveKey("docker-policy-test"), nil, "test")
		err := manager.processOperation(context.Background(), &domain.DevMachineOperation{MachineID: machine.ID, WorkspaceID: machine.WorkspaceID, Action: action, Generation: 1})
		var terminal *terminalOperationError
		require.ErrorAs(t, err, &terminal, action)
		require.Equal(t, "docker_not_allowed", terminal.code)
		require.Zero(t, runtime.spawned+runtime.started, "nothing is started")
	}
}
