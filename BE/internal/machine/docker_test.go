package machine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestInteractiveToolingTmpfsAllowsExecutables(t *testing.T) {
	machine := &domain.DevMachine{MemoryMB: 4096, PidsLimit: 512}
	runtime := &DockerRuntime{}

	agentConfig := runtime.secureHostConfig(machine, "agent", "network", "volume")
	ideConfig := runtime.secureHostConfig(machine, "ide", "network", "volume")
	collectorConfig := runtime.secureHostConfig(machine, "collector", "network", "volume")

	require.False(t, strings.Contains(agentConfig.Tmpfs["/tmp"], "noexec"))
	require.True(t, strings.Contains(agentConfig.Tmpfs["/tmp"], "exec"))
	require.False(t, strings.Contains(ideConfig.Tmpfs["/tmp"], "noexec"))
	require.True(t, strings.Contains(ideConfig.Tmpfs["/tmp"], "exec"))
	require.True(t, strings.Contains(collectorConfig.Tmpfs["/tmp"], "noexec"))
	require.True(t, strings.Contains(agentConfig.Tmpfs["/run/kuayle-secrets"], "noexec"))
	require.True(t, strings.Contains(ideConfig.Tmpfs["/run/kuayle-secrets"], "noexec"))
}

func TestSpawnCleanupPlanOnlyIncludesInvocationOwnedResources(t *testing.T) {
	containers := map[string]string{}
	createdContainers := map[string]string{}

	recordSpawnServiceContainer(containers, createdContainers, "collector", "collector-created", true)
	recordSpawnServiceContainer(containers, createdContainers, "ide", "ide-reused", false)
	recordSpawnServiceContainer(containers, createdContainers, "terminal", "ide-reused", false)

	cleanup := planSpawnFailureCleanup(createdContainers, map[string]string{"gateway": "gateway", "ide": "ide-reused"}, false, true)

	require.Equal(t, map[string]string{
		"collector": "collector-created",
	}, cleanup.Containers)
	require.Equal(t, map[string]string{
		"gateway": "gateway",
		"ide":     "ide-reused",
	}, cleanup.NetworkConnections)
	require.False(t, cleanup.RemoveNetwork)
	require.True(t, cleanup.RemoveVolume)
	require.Equal(t, map[string]string{
		"collector": "collector-created",
		"ide":       "ide-reused",
		"terminal":  "ide-reused",
	}, containers)
}

func TestSpawnCleanupPlanRemovesOnlyNewNetworkAndVolume(t *testing.T) {
	cleanup := planSpawnFailureCleanup(map[string]string{}, map[string]string{}, true, false)

	require.True(t, cleanup.RemoveNetwork)
	require.False(t, cleanup.RemoveVolume)
	require.Empty(t, cleanup.Containers)
	require.Empty(t, cleanup.NetworkConnections)
}

func TestPausedStartActionRestartsInsteadOfPureUnpause(t *testing.T) {
	require.Equal(t, containerStartRestart, serviceStartAction(domain.DevMachineStatusPaused, true, true))
	require.Equal(t, containerStartRestart, serviceStartAction(domain.DevMachineStatusPaused, true, false))
	require.Equal(t, containerStartUnpause, serviceStartAction(domain.DevMachineStatusRunning, true, true))
}

func TestStartPlanDeduplicatesSharedIDEAndTerminalContainer(t *testing.T) {
	sharedContainer := "developer-container"
	browserContainer := "browser-container"
	services := []domain.DevMachineService{
		{ID: uuid.New(), ServiceKey: "terminal", ServiceType: "terminal", ContainerID: &sharedContainer},
		{ID: uuid.New(), ServiceKey: "ide", ServiceType: "ide", ContainerID: &sharedContainer},
		{ID: uuid.New(), ServiceKey: "browser", ServiceType: "browser", ContainerID: &browserContainer},
	}

	planned := plannedStartServices(domain.DevMachineStatusPaused, services)

	require.Len(t, planned, 2)
	require.Equal(t, "ide", planned[0].ServiceKey)
	require.Equal(t, "browser", planned[1].ServiceKey)
}

func TestInstallSecretsClearsStaleFilesBeforeActiveValuesAndReady(t *testing.T) {
	var calls []string
	clear := func(context.Context, string) error {
		calls = append(calls, "clear")
		return nil
	}
	write := func(_ context.Context, _ string, name, value string) error {
		calls = append(calls, "write:"+name+"="+value)
		return nil
	}

	err := installSecrets(context.Background(), "container-id", map[string]string{"ACTIVE_SECRET": "active-value"}, clear, write)

	require.NoError(t, err)
	require.Equal(t, []string{"clear", "write:ACTIVE_SECRET=active-value", "write:.ready="}, calls)
}

func TestMissingImmutableLocalImageRefusesPull(t *testing.T) {
	require.NoError(t, missingImageError("registry.example/kuayle/app:latest", true))
	require.ErrorContains(t, missingImageError("registry.example/kuayle/app:latest", false), "pulling is disabled")

	err := missingImageError("sha256:local-environment", true)
	require.True(t, errors.Is(err, ErrLocalEnvironmentMissing))
	require.ErrorContains(t, err, "sha256:local-environment")
}

func TestValidateEnvironmentImageLabelsRequiresExpectedWorkspaceAndEnvironment(t *testing.T) {
	workspaceID, environmentID := uuid.New(), uuid.New()
	labels := map[string]string{
		"com.kuayle.managed":        "true",
		"com.kuayle.kind":           "dev-machine-environment",
		"com.kuayle.workspace-id":   workspaceID.String(),
		"com.kuayle.environment-id": environmentID.String(),
	}

	require.NoError(t, validateEnvironmentImageLabels(labels, workspaceID, environmentID))
	require.Error(t, validateEnvironmentImageLabels(labels, uuid.New(), environmentID))
	require.Error(t, validateEnvironmentImageLabels(labels, workspaceID, uuid.New()))
	require.Error(t, validateEnvironmentImageLabels(map[string]string{"com.kuayle.managed": "true"}, workspaceID, environmentID))
}

func TestEnvironmentImmutableImageIDPrefersDigest(t *testing.T) {
	digest := "sha256:immutable"
	environment := &domain.DevMachineEnvironment{ImageRef: "kuayle/dev-environment-test:snapshot", ImageDigest: &digest}

	require.Equal(t, digest, environmentImmutableImageID(environment))
	require.Equal(t, digest, environmentImmutableImageID(&domain.DevMachineEnvironment{ImageRef: digest}))
	require.Empty(t, environmentImmutableImageID(&domain.DevMachineEnvironment{ImageRef: "kuayle/dev-environment-test:snapshot"}))
}
