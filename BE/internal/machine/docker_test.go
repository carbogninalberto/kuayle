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

func TestHostDiskUsageReturnsUnknownWithoutRunningDeveloperContainer(t *testing.T) {
	dockerClient := &dockerClientFake{}
	runtime := &DockerRuntime{config: DockerConfig{DiskQuotaMode: "monitored"}, client: dockerClient}

	_, err := runtime.HostDiskUsage(context.Background())

	require.ErrorIs(t, err, ErrHostDiskUnknown)
	require.False(t, dockerClient.containerListOptions.All)
	require.Equal(t, map[string]bool{
		"com.kuayle.managed=true":     true,
		"com.kuayle.service=terminal": true,
	}, dockerClient.containerListOptions.Filters["label"])
}

// Without a configured host path, the free space of the Docker data root is
// read with df in a running developer (terminal) container.
func TestHostDiskUsageMeasuresInARunningDeveloperContainer(t *testing.T) {
	labels := func(service string) map[string]string {
		return map[string]string{"com.kuayle.managed": "true", "com.kuayle.service": service}
	}
	dockerClient := &readonlyInspectClient{logExecClient: &logExecClient{
		dockerClientFake: &dockerClientFake{containers: []container.Summary{
			{ID: "stopped-terminal", Labels: labels("terminal"), State: container.StateExited},
			{ID: "egress", Labels: labels("egress"), State: container.StateRunning},
			{ID: "sudo-terminal", Labels: labels("terminal"), State: container.StateRunning},
			{ID: "running-terminal", Labels: labels("terminal"), State: container.StateRunning},
		}},
		stdout: "Filesystem 1-blocks Used Available Capacity Mounted on\n/dev/sdb 1000000 400000 600000 40% /workspace\n",
	}, readonly: map[string]bool{"running-terminal": true}}
	runtime := &DockerRuntime{config: DockerConfig{DiskQuotaMode: "monitored"}, client: dockerClient}

	usage, err := runtime.HostDiskUsage(context.Background())

	require.NoError(t, err)
	require.Equal(t, HostDiskUsage{TotalBytes: 1000000, FreeBytes: 600000}, usage)
	require.Equal(t, "running-terminal", dockerClient.target, "a terminal whose root filesystem its user can write (sudo) is never trusted")
	require.Equal(t, []string{"/usr/bin/df", "-B1", "-P", "/workspace"}, dockerClient.exec.Cmd, "by absolute path, never the user's PATH")
	require.Equal(t, "0:0", dockerClient.exec.User)
}

// readonlyInspectClient answers ContainerInspect with a read-only root
// filesystem for the containers in readonly.
type readonlyInspectClient struct {
	*logExecClient
	readonly map[string]bool
}

func (f *readonlyInspectClient) ContainerInspect(_ context.Context, id string, _ client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	return client.ContainerInspectResult{Container: container.InspectResponse{HostConfig: &container.HostConfig{ReadonlyRootfs: f.readonly[id]}}}, nil
}

func TestHostDiskUsageUsesConfiguredPathWithoutListingContainers(t *testing.T) {
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

func TestHostDiskUsageFallsBackToDeveloperContainerWhenPathEmpty(t *testing.T) {
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

	agentConfig := runtime.secureHostConfig(machine, "agent", "network", "volume", false)
	terminalConfig := runtime.secureHostConfig(machine, "terminal", "network", "volume", false)
	egressConfig := runtime.secureHostConfig(machine, "egress", "network", "volume", false)

	require.False(t, strings.Contains(agentConfig.Tmpfs["/tmp"], "noexec"))
	require.True(t, strings.Contains(agentConfig.Tmpfs["/tmp"], "exec"))
	require.False(t, strings.Contains(terminalConfig.Tmpfs["/tmp"], "noexec"))
	require.True(t, strings.Contains(terminalConfig.Tmpfs["/tmp"], "exec"))
	require.True(t, strings.Contains(egressConfig.Tmpfs["/tmp"], "noexec"))
	require.True(t, strings.Contains(agentConfig.Tmpfs["/run/kuayle-secrets"], "noexec"))
	// Docker mounts tmpfs noexec unless exec is explicit; tools such as Codex
	// install and run managed binaries from the home directory.
	require.Contains(t, strings.Split(agentConfig.Tmpfs["/home/kuayle"], ","), "exec")
	require.Contains(t, strings.Split(terminalConfig.Tmpfs["/home/kuayle"], ","), "exec")
	require.Contains(t, strings.Split(egressConfig.Tmpfs["/home/kuayle"], ","), "noexec")
	require.True(t, strings.Contains(terminalConfig.Tmpfs["/run/kuayle-secrets"], "noexec"))

	// KUA-175: an agent CLI is PID 1 and does not reap the orphaned git
	// children it leaves behind; they become zombies counted by PidsLimit
	// until the run crashes at the limit. Docker's bundled init reaps them.
	for name, config := range map[string]*container.HostConfig{
		"agent": agentConfig, "terminal": terminalConfig,
		"egress": egressConfig, "check": runtime.secureHostConfig(machine, "check", "network", "volume", false),
	} {
		require.NotNil(t, config.Init, "%s runs without an init process", name)
		require.True(t, *config.Init, "%s runs without an init process", name)
	}
}

func TestHeadlessTerminalUsesDeveloperContainerSecurityAndResources(t *testing.T) {
	machine := &domain.DevMachine{MemoryMB: 8192, CPUMillis: 4000, PidsLimit: 512, Profile: domain.DevMachineProfileHeadless}
	runtime := &DockerRuntime{}

	terminalConfig := runtime.secureHostConfig(machine, "terminal", "network", "volume", false)

	require.True(t, terminalConfig.ReadonlyRootfs)
	require.Equal(t, []string{"ALL"}, terminalConfig.CapDrop)
	require.Contains(t, strings.Split(terminalConfig.Tmpfs["/home/kuayle"], ","), "exec")
	require.Len(t, terminalConfig.Mounts, 1)
	require.Equal(t, "/workspace", terminalConfig.Mounts[0].Target)
	require.False(t, terminalConfig.Mounts[0].ReadOnly)
}

// Developer containers keep their floor so parallel build tools can fork even
// on machines with a small PID budget. Support containers keep their share.
func TestBuildContainersGetAProcessBudgetForParallelToolchains(t *testing.T) {
	runtime := &DockerRuntime{}
	machine := &domain.DevMachine{MemoryMB: 12288, CPUMillis: 4000, PidsLimit: 512, Profile: domain.DevMachineProfileHeadless}
	for _, docker := range []bool{false, true} {
		pids := *runtime.secureHostConfig(machine, "terminal", "network", "volume", docker).Resources.PidsLimit
		require.Equal(t, int64(1024), pids, "terminal (docker=%t)", docker)
		require.Equal(t, int64(32), *runtime.secureHostConfig(machine, "egress", "network", "volume", docker).Resources.PidsLimit)
	}
}

func TestAgentPIDLimitStaysWithinMachineBudget(t *testing.T) {
	runtime := &DockerRuntime{}
	for _, test := range []struct {
		name  string
		limit int
		want  int64
	}{
		{"below the build floor", 512, 512},
		{"between the floors", 2048, 2048},
		{"at the agent floor", 4096, 4096},
		{"share below the agent floor", 8192, 4096},
		{"share above the agent floor", 16384, 5734},
	} {
		t.Run(test.name, func(t *testing.T) {
			machine := &domain.DevMachine{MemoryMB: 16384, CPUMillis: 8000, PidsLimit: test.limit, Profile: domain.DevMachineProfileHeadless}
			config := runtime.secureHostConfig(machine, "agent", "network", "volume", true)
			require.Equal(t, test.want, *config.Resources.PidsLimit)
		})
	}
}

func TestDeveloperContainerIDSupportsHeadlessTerminalOnlyMachines(t *testing.T) {
	terminalContainer := "headless-developer"
	egressContainer := "egress"
	services := []domain.DevMachineService{
		{ServiceKey: "egress", ServiceType: "egress", ContainerID: &egressContainer},
		{ServiceKey: "terminal", ServiceType: "terminal", ContainerID: &terminalContainer},
	}

	require.Equal(t, terminalContainer, developerContainerID(services))
}

func TestAgentRuntimeEnvironmentAppliesNonSecretInvocationValues(t *testing.T) {
	environment, err := agentRuntimeEnvironment(map[string]string{
		"CLAUDE_CODE_EFFORT_LEVEL": "high",
	})

	require.NoError(t, err)
	require.Equal(t, []string{"CLAUDE_CODE_EFFORT_LEVEL=high"}, environment)

	for _, invalid := range []map[string]string{
		{"DEEPSEEK_API_KEY": "secret"},
		{"CLAUDE_CODE_EFFORT_LEVEL": "high\nINJECTED=1"},
		{"claude_effort": "high"},
	} {
		_, err = agentRuntimeEnvironment(invalid)
		require.Error(t, err)
	}
}

func TestSpawnCleanupPlanOnlyIncludesInvocationOwnedResources(t *testing.T) {
	containers := map[string]string{}
	createdContainers := map[string]string{}

	recordSpawnServiceContainer(containers, createdContainers, "egress", "egress-created", true)
	recordSpawnServiceContainer(containers, createdContainers, "terminal", "terminal-reused", false)

	cleanup := planSpawnFailureCleanup(createdContainers, map[string]string{"gateway": "gateway", "terminal": "terminal-reused"}, false, true)

	require.Equal(t, map[string]string{
		"egress": "egress-created",
	}, cleanup.Containers)
	require.Equal(t, map[string]string{
		"gateway":  "gateway",
		"terminal": "terminal-reused",
	}, cleanup.NetworkConnections)
	require.False(t, cleanup.RemoveNetwork)
	require.True(t, cleanup.RemoveVolume)
	require.Equal(t, map[string]string{
		"egress":   "egress-created",
		"terminal": "terminal-reused",
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

func TestMissingImageRefusesPullOnlyWhenPullingIsDisabled(t *testing.T) {
	require.NoError(t, missingImageError("registry.example/kuayle/app:latest", true))
	require.ErrorContains(t, missingImageError("registry.example/kuayle/app:latest", false), "pulling is disabled")
}

// A machine created while the workspace allows sudo gets developer containers
// whose root filesystem is writable and which may gain privileges through
// sudo, with Docker's default capabilities minus raw sockets: never
// privileged, never NET_ADMIN or SYS_ADMIN, still on the machine network
// behind the egress proxy and still under the configured seccomp and AppArmor
// profiles. The support containers stay locked.
func TestSudoMachinesRelaxOnlyTheDeveloperContainers(t *testing.T) {
	runtime := &DockerRuntime{config: DockerConfig{SeccompProfile: "/etc/kuayle/seccomp.json", AppArmorProfile: "kuayle-machine"}}
	sudo := &domain.DevMachine{MemoryMB: 4096, CPUMillis: 2000, PidsLimit: 512, SudoEnabled: true}
	locked := &domain.DevMachine{MemoryMB: 4096, CPUMillis: 2000, PidsLimit: 512}

	relaxed := runtime.secureHostConfig(sudo, "terminal", "machine-network", "workspace-volume", false)
	requireSudoRelaxation(t, relaxed, "terminal")
	same := runtime.secureHostConfig(locked, "terminal", "machine-network", "workspace-volume", false)
	require.Equal(t, same.Resources, relaxed.Resources, "sudo does not change the container's resources")
	require.True(t, same.ReadonlyRootfs)
	require.Equal(t, []string{"ALL"}, same.CapDrop)
	require.Contains(t, same.SecurityOpt, "no-new-privileges=true")
	hostConfig := runtime.secureHostConfig(sudo, "egress", "machine-network", "workspace-volume", false)
	require.True(t, hostConfig.ReadonlyRootfs)
	require.Equal(t, []string{"ALL"}, hostConfig.CapDrop)
	require.Contains(t, hostConfig.SecurityOpt, "no-new-privileges=true")
}

// Agent-class containers (attempts, checks, previews, tools and commands) may
// always gain root through the image's sudo, so an agent can install the
// system packages a project needs, on every machine: the same relaxation as a
// sudo developer container, never more.
func TestAgentContainersAlwaysAllowSudo(t *testing.T) {
	runtime := &DockerRuntime{config: DockerConfig{SeccompProfile: "/etc/kuayle/seccomp.json", AppArmorProfile: "kuayle-machine"}}
	for _, machine := range []*domain.DevMachine{
		{MemoryMB: 4096, CPUMillis: 2000, PidsLimit: 512},
		{MemoryMB: 4096, CPUMillis: 2000, PidsLimit: 512, SudoEnabled: true},
	} {
		requireSudoRelaxation(t, runtime.secureHostConfig(machine, "agent", "machine-network", "workspace-volume", false), "agent")
	}
}

func requireSudoRelaxation(t *testing.T, relaxed *container.HostConfig, serviceType string) {
	t.Helper()
	require.False(t, relaxed.ReadonlyRootfs, serviceType)
	require.False(t, relaxed.Privileged, serviceType)
	require.NotContains(t, relaxed.SecurityOpt, "no-new-privileges=true", serviceType)
	require.Contains(t, relaxed.SecurityOpt, "seccomp=/etc/kuayle/seccomp.json", serviceType)
	require.Contains(t, relaxed.SecurityOpt, "apparmor=kuayle-machine", serviceType)
	require.Empty(t, relaxed.CapAdd, serviceType)
	require.Equal(t, []string{"NET_RAW"}, relaxed.CapDrop, serviceType)
	require.Equal(t, container.NetworkMode("machine-network"), relaxed.NetworkMode, serviceType)
	require.Empty(t, relaxed.Binds, serviceType)
	for _, mounted := range relaxed.Mounts {
		require.NotContains(t, mounted.Source, "docker.sock", serviceType)
	}
}

// Agent attempts that run on a developer image (the headless providers) keep
// using the image's pinned CLIs: the workspace npm prefix, which people and
// agents can write, is removed from their PATH.
func TestAgentAttemptsDoNotRunCLIsFromTheWorkspaceNpmPrefix(t *testing.T) {
	environment := agentImageEnvironment([]string{
		"HOME=/home/kuayle",
		"PATH=/workspace/.kuayle/npm-global/bin:/usr/local/go/bin:/usr/local/bin:/usr/bin:/workspace/.kuayle/npm-global/bin",
		"NPM_CONFIG_GLOBALCONFIG=/etc/npmrc",
	})
	require.Equal(t, []string{"HOME=/home/kuayle", "PATH=/usr/local/go/bin:/usr/local/bin:/usr/bin", "NPM_CONFIG_GLOBALCONFIG=/etc/npmrc"}, environment)
	unchanged := []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"}
	require.Equal(t, unchanged, agentImageEnvironment(unchanged), "agent images are unaffected")
}

// Each container's CPU, memory and process limits are a share of the machine.
// The agent gets half of the machine and the developer terminal 40%; with a Docker daemon the daemon takes 40% and the
// agent and terminal keep 35% each, ceilings that may overlap.
func TestContainerSharesSplitTheMachine(t *testing.T) {
	runtime := &DockerRuntime{}
	shares := func(machine *domain.DevMachine, docker bool, serviceTypes ...string) map[string]int64 {
		result := map[string]int64{}
		for _, serviceType := range serviceTypes {
			config := runtime.secureHostConfig(machine, serviceType, "network", "volume", docker)
			require.Equal(t, int64(machine.MemoryMB)*1024*1024*containerSharePercent(machine, serviceType, docker)/100, config.Resources.Memory, serviceType)
			require.Equal(t, int64(machine.CPUMillis)*1_000_000*containerSharePercent(machine, serviceType, docker)/100, config.Resources.NanoCPUs, serviceType)
			result[serviceType] = containerSharePercent(machine, serviceType, docker)
		}
		if docker {
			result["docker"] = dockerShare
		}
		return result
	}
	total := func(values map[string]int64) (sum int64) {
		for _, value := range values {
			sum += value
		}
		return sum
	}
	headless := &domain.DevMachine{MemoryMB: 8192, CPUMillis: 4000, PidsLimit: 512, Profile: domain.DevMachineProfileHeadless}
	split := shares(headless, false, "agent", "terminal", "egress")
	require.Equal(t, map[string]int64{"agent": 50, "terminal": 40, "egress": 5}, split)
	require.Equal(t, int64(4096)*1024*1024, runtime.secureHostConfig(headless, "agent", "network", "volume", false).Resources.Memory, "an 8 GiB task machine gives its agent 4 GiB")
	require.LessOrEqual(t, total(split), int64(100))
	split = shares(headless, true, "agent", "terminal", "egress")
	require.Equal(t, map[string]int64{"agent": 35, "terminal": 35, "egress": 5, "docker": 40}, split)
	require.Equal(t, int64(115), total(split), "with a daemon the agent and terminal ceilings overlap")
}

// A container with a writable root filesystem (every agent-class container,
// and a sudo terminal) writes to the Docker data root, not the quota-bounded
// workspace volume. With hard quotas the data root supports project quotas,
// so that layer is bounded by the machine's disk size too; containers with a
// read-only root get no size, and monitored mode (no project quotas) cannot
// bound it, where the host free-space floor is the protection.
func TestWritableRootFilesystemsAreBoundedByTheMachineDiskWithHardQuotas(t *testing.T) {
	hard := &DockerRuntime{config: DockerConfig{DiskQuotaMode: "hard"}}
	monitored := &DockerRuntime{config: DockerConfig{DiskQuotaMode: "monitored"}}
	sudo := &domain.DevMachine{MemoryMB: 4096, CPUMillis: 2000, PidsLimit: 512, DiskGB: 20, SudoEnabled: true}
	locked := &domain.DevMachine{MemoryMB: 4096, CPUMillis: 2000, PidsLimit: 512, DiskGB: 20}

	require.Equal(t, map[string]string{"size": "20G"}, hard.secureHostConfig(locked, "agent", "network", "volume", false).StorageOpt)
	require.Equal(t, map[string]string{"size": "20G"}, hard.secureHostConfig(sudo, "terminal", "network", "volume", false).StorageOpt)
	require.Empty(t, hard.secureHostConfig(locked, "terminal", "network", "volume", false).StorageOpt, "a read-only root writes nothing there")
	require.Empty(t, hard.secureHostConfig(locked, "egress", "network", "volume", false).StorageOpt)
	require.Empty(t, monitored.secureHostConfig(locked, "agent", "network", "volume", false).StorageOpt)
}
