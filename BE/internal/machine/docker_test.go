package machine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/containerd/errdefs"
	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/moby/moby/api/types/container"
	volumetypes "github.com/moby/moby/api/types/volume"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestWorkspaceVolumeOptionsApplyExactHardQuota(t *testing.T) {
	labels := map[string]string{"com.kuayle.managed": "true", "com.kuayle.machine-id": uuid.NewString()}

	options, err := workspaceVolumeCreateOptions("workspace-test", labels, 20)

	require.NoError(t, err)
	require.Equal(t, "local", options.Driver)
	require.Equal(t, "21474836480", options.DriverOpts["size"])
	require.Equal(t, "21474836480", options.Labels[workspaceQuotaLabel])
	require.NotContains(t, labels, workspaceQuotaLabel)
	require.NoError(t, validateWorkspaceVolume(volumetypes.Volume{
		Name: options.Name, Driver: options.Driver, Labels: options.Labels, Options: options.DriverOpts,
	}, options))
}

func TestWorkspaceVolumeValidationRejectsUnboundedOrMismatchedVolumes(t *testing.T) {
	options, err := workspaceVolumeCreateOptions("workspace-test", map[string]string{"com.kuayle.managed": "true"}, 50)
	require.NoError(t, err)

	tests := []struct {
		name   string
		volume volumetypes.Volume
		error  string
	}{
		{name: "wrong driver", volume: volumetypes.Volume{Driver: "custom", Labels: options.Labels, Options: options.DriverOpts}, error: "uses driver"},
		{name: "missing ownership", volume: volumetypes.Volume{Driver: options.Driver, Labels: map[string]string{}, Options: options.DriverOpts}, error: "incompatible label"},
		{name: "missing quota", volume: volumetypes.Volume{Driver: options.Driver, Labels: options.Labels, Options: map[string]string{}}, error: "hard quota"},
		{name: "wrong quota", volume: volumetypes.Volume{Driver: options.Driver, Labels: options.Labels, Options: map[string]string{"size": "21474836480"}}, error: "hard quota"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.ErrorContains(t, validateWorkspaceVolume(test.volume, options), test.error)
		})
	}

	_, err = workspaceVolumeCreateOptions("workspace-test", nil, 0)
	require.ErrorContains(t, err, "must be positive")
}

func TestWorkspaceVolumeOptionsApplyMonitoredLabelsWithoutSizeOption(t *testing.T) {
	labels := map[string]string{"com.kuayle.managed": "true", "com.kuayle.machine-id": uuid.NewString()}

	options, err := workspaceVolumeCreateOptions("workspace-test", labels, 20, "monitored")

	require.NoError(t, err)
	require.Equal(t, "local", options.Driver)
	require.NotContains(t, options.DriverOpts, "size")
	require.Equal(t, "21474836480", options.Labels[workspaceQuotaLabel])
	require.Equal(t, "monitored", options.Labels["com.kuayle.workspace-quota-mode"])
	require.Equal(t, labels["com.kuayle.managed"], options.Labels["com.kuayle.managed"])
	require.Equal(t, labels["com.kuayle.machine-id"], options.Labels["com.kuayle.machine-id"])
	require.NotContains(t, labels, workspaceQuotaLabel)
}

func TestWorkspaceVolumeValidationMatrix(t *testing.T) {
	ownership := map[string]string{"com.kuayle.managed": "true", "com.kuayle.machine-id": uuid.NewString()}
	hardOptions, err := workspaceVolumeCreateOptions("workspace-test", ownership, 50)
	require.NoError(t, err)
	monitoredOptions, err := workspaceVolumeCreateOptions("workspace-test", ownership, 50, "monitored")
	require.NoError(t, err)

	legacyHard := volumetypes.Volume{
		Driver: hardOptions.Driver, Labels: hardOptions.Labels, Options: hardOptions.DriverOpts,
	}
	monitored := volumetypes.Volume{
		Driver: monitoredOptions.Driver, Labels: monitoredOptions.Labels, Options: monitoredOptions.DriverOpts,
	}

	require.ErrorContains(t, validateWorkspaceVolume(volumetypes.Volume{
		Driver: hardOptions.Driver, Labels: hardOptions.Labels, Options: map[string]string{},
	}, hardOptions), "hard quota")
	hardWithExtraMetadata := volumetypes.Volume{
		Driver: hardOptions.Driver, Labels: map[string]string{}, Options: hardOptions.DriverOpts,
	}
	for key, value := range hardOptions.Labels {
		hardWithExtraMetadata.Labels[key] = value
	}
	hardWithExtraMetadata.Labels[workspaceQuotaModeLabel] = "monitored"
	require.NoError(t, validateWorkspaceVolume(hardWithExtraMetadata, hardOptions))
	require.NoError(t, validateWorkspaceVolume(monitored, monitoredOptions))
	require.NoError(t, validateWorkspaceVolume(legacyHard, monitoredOptions))

	wrongLabels := map[string]string{}
	for key, value := range monitored.Labels {
		wrongLabels[key] = value
	}
	wrongLabels["com.kuayle.managed"] = "false"
	wrongModeLabels := map[string]string{}
	for key, value := range monitored.Labels {
		wrongModeLabels[key] = value
	}
	wrongModeLabels["com.kuayle.workspace-quota-mode"] = "hard"

	tests := []struct {
		name   string
		volume volumetypes.Volume
		error  string
	}{
		{name: "wrong size", volume: volumetypes.Volume{Driver: "local", Labels: monitored.Labels, Options: map[string]string{"size": "1"}}, error: "quota"},
		{name: "wrong driver", volume: volumetypes.Volume{Driver: "custom", Labels: monitored.Labels}, error: "uses driver"},
		{name: "wrong ownership label", volume: volumetypes.Volume{Driver: "local", Labels: wrongLabels}, error: "incompatible label"},
		{name: "wrong quota mode label", volume: volumetypes.Volume{Driver: "local", Labels: wrongModeLabels}, error: "incompatible label"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.ErrorContains(t, validateWorkspaceVolume(test.volume, monitoredOptions), test.error)
		})
	}
}

func TestPingSkipsWorkspaceQuotaProbeInMonitoredMode(t *testing.T) {
	dockerClient := &dockerClientFake{}
	runtime := &DockerRuntime{config: DockerConfig{DiskQuotaMode: "monitored"}, client: dockerClient}

	require.NoError(t, runtime.Ping(context.Background()))
	require.Equal(t, 1, dockerClient.pingCalls)
	require.Zero(t, dockerClient.volumeInspectCalls)
	require.Zero(t, dockerClient.volumeCreateCalls)
}

func TestPingStillCreatesWorkspaceQuotaProbeInHardMode(t *testing.T) {
	dockerClient := &dockerClientFake{}
	runtime := &DockerRuntime{config: DockerConfig{DiskQuotaMode: "hard"}, client: dockerClient}

	require.NoError(t, runtime.Ping(context.Background()))
	require.Equal(t, 1, dockerClient.pingCalls)
	require.Equal(t, 1, dockerClient.volumeInspectCalls)
	require.Equal(t, 1, dockerClient.volumeCreateCalls)
	require.Equal(t, "1073741824", dockerClient.createdVolume.DriverOpts["size"])
}

func TestParseHostDiskUsage(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   HostDiskUsage
		error  string
	}{
		{
			name:   "normal",
			output: "Filesystem     1-blocks       Used  Available Capacity Mounted on\noverlay        107374182400 50000000000 57374182400 47% /workspace\n",
			want:   HostDiskUsage{TotalBytes: 107374182400, FreeBytes: 57374182400},
		},
		{
			name:   "header only",
			output: "Filesystem 1-blocks Used Available Capacity Mounted on\n",
			error:  "unavailable",
		},
		{
			name:   "garbage",
			output: "df: /workspace: No such file or directory\n",
			error:  "invalid",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			usage, err := parseHostDiskUsage(test.output)
			if test.error != "" {
				require.ErrorContains(t, err, test.error)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.want, usage)
		})
	}
}

func TestHostDiskUsageReturnsUnknownWithoutRunningManagedCollector(t *testing.T) {
	dockerClient := &dockerClientFake{}
	runtime := &DockerRuntime{config: DockerConfig{DiskQuotaMode: "monitored"}, client: dockerClient}

	_, err := runtime.HostDiskUsage(context.Background())

	require.ErrorIs(t, err, ErrHostDiskUnknown)
	require.False(t, dockerClient.containerListOptions.All)
	require.Equal(t, map[string]bool{
		"com.kuayle.managed=true":      true,
		"com.kuayle.service=collector": true,
	}, dockerClient.containerListOptions.Filters["label"])
}

func TestHostDiskUsageUsesConfiguredPathWithoutListingCollectors(t *testing.T) {
	dockerClient := &dockerClientFake{}
	statfsCalls := 0
	runtime := &DockerRuntime{
		config: DockerConfig{DiskQuotaMode: "monitored", HostDiskPath: "/var/lib/docker"},
		client: dockerClient,
		statfs: func(path string, stat *unix.Statfs_t) error {
			statfsCalls++
			require.Equal(t, "/var/lib/docker", path)
			stat.Blocks = 100
			stat.Bavail = 40
			stat.Bsize = 4096
			return nil
		},
	}

	usage, err := runtime.HostDiskUsage(context.Background())

	require.NoError(t, err)
	require.Equal(t, HostDiskUsage{TotalBytes: 409600, FreeBytes: 163840}, usage)
	require.Equal(t, 1, statfsCalls)
	require.Zero(t, dockerClient.containerListCalls)
}

func TestHostDiskUsageFallsBackToCollectorWhenPathEmpty(t *testing.T) {
	dockerClient := &dockerClientFake{}
	statfsCalls := 0
	runtime := &DockerRuntime{
		config: DockerConfig{DiskQuotaMode: "monitored"},
		client: dockerClient,
		statfs: func(string, *unix.Statfs_t) error {
			statfsCalls++
			return nil
		},
	}

	_, err := runtime.HostDiskUsage(context.Background())

	require.ErrorIs(t, err, ErrHostDiskUnknown)
	require.Equal(t, 1, dockerClient.containerListCalls)
	require.Zero(t, statfsCalls)
}

func TestHostDiskUsageWrapsStatfsErrors(t *testing.T) {
	dockerClient := &dockerClientFake{}
	statfsErr := errors.New("statfs failed")
	runtime := &DockerRuntime{
		config: DockerConfig{DiskQuotaMode: "monitored", HostDiskPath: "/var/lib/docker"},
		client: dockerClient,
		statfs: func(string, *unix.Statfs_t) error {
			return statfsErr
		},
	}

	_, err := runtime.HostDiskUsage(context.Background())

	require.ErrorIs(t, err, statfsErr)
	require.NotErrorIs(t, err, ErrHostDiskUnknown)
	require.ErrorContains(t, err, `statfs host disk path "/var/lib/docker"`)
	require.Zero(t, dockerClient.containerListCalls)
}

type dockerClientFake struct {
	client.APIClient
	pingCalls            int
	volumeInspectCalls   int
	volumeCreateCalls    int
	containerListCalls   int
	createdVolume        client.VolumeCreateOptions
	containers           []container.Summary
	containerListOptions client.ContainerListOptions
}

func (f *dockerClientFake) Ping(context.Context, client.PingOptions) (client.PingResult, error) {
	f.pingCalls++
	return client.PingResult{}, nil
}

func (f *dockerClientFake) VolumeInspect(context.Context, string, client.VolumeInspectOptions) (client.VolumeInspectResult, error) {
	f.volumeInspectCalls++
	return client.VolumeInspectResult{}, errdefs.ErrNotFound
}

func (f *dockerClientFake) ContainerList(_ context.Context, options client.ContainerListOptions) (client.ContainerListResult, error) {
	f.containerListCalls++
	f.containerListOptions = options
	return client.ContainerListResult{Items: f.containers}, nil
}

func (f *dockerClientFake) VolumeCreate(_ context.Context, options client.VolumeCreateOptions) (client.VolumeCreateResult, error) {
	f.volumeCreateCalls++
	f.createdVolume = options
	return client.VolumeCreateResult{Volume: volumetypes.Volume{
		Name: options.Name, Driver: options.Driver, Labels: options.Labels, Options: options.DriverOpts,
	}}, nil
}

func (f *dockerClientFake) VolumeRemove(context.Context, string, client.VolumeRemoveOptions) (client.VolumeRemoveResult, error) {
	return client.VolumeRemoveResult{}, nil
}

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

func TestTerminateTerminalTreatsAbsentStoppedRuntimeAsClosed(t *testing.T) {
	runtime := &DockerRuntime{}
	session := &domain.DevMachineTerminalSession{RuntimeSessionName: "term-test"}

	require.NoError(t, runtime.TerminateTerminal(context.Background(), &domain.DevMachine{Status: domain.DevMachineStatusStopped}, nil, session))
	require.NoError(t, runtime.TerminateTerminal(context.Background(), &domain.DevMachine{Status: domain.DevMachineStatusDestroyed}, nil, session))
	require.ErrorContains(t, runtime.TerminateTerminal(context.Background(), &domain.DevMachine{Status: domain.DevMachineStatusRunning}, nil, session), "developer container is unavailable")
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

func TestEnvironmentImageReferencesPreferDigestAndCleanupFallsBackToTag(t *testing.T) {
	digest := "sha256:immutable"
	environment := &domain.DevMachineEnvironment{ImageRef: "kuayle/dev-environment-test:snapshot", ImageDigest: &digest}

	require.Equal(t, digest, environmentImmutableImageID(environment))
	require.Equal(t, digest, environmentImmutableImageID(&domain.DevMachineEnvironment{ImageRef: digest}))
	require.Empty(t, environmentImmutableImageID(&domain.DevMachineEnvironment{ImageRef: "kuayle/dev-environment-test:snapshot"}))
	require.Equal(t, digest, environmentImageCleanupReference(environment))
	require.Equal(t, "kuayle/dev-environment-test:snapshot", environmentImageCleanupReference(&domain.DevMachineEnvironment{ImageRef: "kuayle/dev-environment-test:snapshot"}))
}
