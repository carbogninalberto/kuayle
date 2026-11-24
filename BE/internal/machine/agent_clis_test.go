package machine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
	cryptoutil "github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/stretchr/testify/require"
)

func TestRefreshedAgentCLIsComeBeforeTheImageButAfterPersonalInstalls(t *testing.T) {
	headless := []string{"HOME=/home/kuayle", "PATH=/workspace/.kuayle/npm-global/bin:/usr/local/go/bin:/usr/local/bin:/usr/bin"}
	require.Contains(t, withAgentCLIPath(headless), "PATH=/workspace/.kuayle/npm-global/bin:"+agentCLIBin+":/usr/local/go/bin:/usr/local/bin:/usr/bin",
		"a person's own `npm i -g` stays first in the terminal")
	agentImage := []string{"PATH=/usr/local/go/bin:/usr/local/bin:/usr/bin"}
	require.Equal(t, []string{"PATH=" + agentCLIBin + ":/usr/local/go/bin:/usr/local/bin:/usr/bin"}, withAgentCLIPath(agentImage))
	require.Equal(t, []string{"HOME=/x", "PATH=" + agentCLIBin + ":" + defaultImagePath}, withAgentCLIPath([]string{"HOME=/x"}))
	once := withAgentCLIPath(agentImage)
	require.Equal(t, once, withAgentCLIPath(once), "applying twice changes nothing")
}

func TestAgentCLIVolumeIsMountedReadOnlyOnlyWhenEnabled(t *testing.T) {
	hostConfig := &container.HostConfig{}
	env := []string{"PATH=/usr/bin"}
	disabled := &DockerRuntime{}
	require.Equal(t, env, disabled.useAgentCLIs(hostConfig, env))
	require.Empty(t, hostConfig.Mounts)

	enabled := &DockerRuntime{agentCLIs: true}
	env = enabled.useAgentCLIs(hostConfig, env)
	require.Equal(t, []mount.Mount{{Type: mount.TypeVolume, Source: agentCLIVolume, Target: agentCLIRoot, ReadOnly: true}}, hostConfig.Mounts,
		"agents and people cannot change the shared CLIs")
	require.Equal(t, []string{"PATH=" + agentCLIBin + ":/usr/bin"}, env)
}

// The refresh replaces the images' npm CLIs on PATH, so it must install the
// same packages, run the same install scripts and report the versions the
// run trace reads: a CLI moving to a new package (OpenCode 2 is
// @opencode/cli) must move everywhere at once, or the stale package shadows
// the image's CLI.
func TestAgentCLIRefreshTracksThePackagesTheImagesInstall(t *testing.T) {
	installer, err := os.ReadFile(filepath.Join(repoRootFromCaller(t), "devmachine", "common", "install-agent-clis.sh"))
	require.NoError(t, err)
	for _, item := range agentCLIPackages {
		require.Contains(t, string(installer), `"`+item.name+`@${`, "the images install %s", item.name)
		if strings.Contains(string(installer), "--allow-scripts="+item.name+" ") {
			require.Contains(t, agentCLIScripts, "allow-scripts[]="+item.name+`\n`, "%s runs its install script in the refresh too", item.name)
		}
	}
	traced := map[string]bool{}
	for _, name := range agentCLIVersionPackages {
		traced[name] = true
	}
	for _, item := range agentCLIPackages {
		require.True(t, traced[item.name], "the run trace reads the refreshed version of %s", item.name)
	}
	// The helper runs in the headless image and checks the CLIs with that
	// image's flag contract, at the path its script defaults to.
	headless, err := os.ReadFile(filepath.Join(repoRootFromCaller(t), "devmachine", "headless", "Dockerfile"))
	require.NoError(t, err)
	require.Regexp(t, `(?m)^COPY common/\S.* common/check-agent-cli-flags\.sh( \S+)* /usr/local/lib/kuayle/$`, string(headless))
	require.Contains(t, agentCLIRefreshScript, "flag_check=${KUAYLE_AGENT_CLI_FLAG_CHECK:-/usr/local/lib/kuayle/check-agent-cli-flags.sh}")
}

func TestAgentCLIRefreshReportParsesTheHelperOutput(t *testing.T) {
	result, err := parseAgentCLIRefresh("npm notice\nKUAYLE_AGENT_CLIS changed @anthropic-ai/claude-code=2.2.0 @openai/codex=0.170.1\n")
	require.NoError(t, err)
	require.True(t, result.Changed())
	require.True(t, result.Usable())
	require.Equal(t, map[string]string{"@anthropic-ai/claude-code": "2.2.0", "@openai/codex": "0.170.1"}, result.Versions)
	result, err = parseAgentCLIRefresh("KUAYLE_AGENT_CLIS unchanged @opencode/cli=2.3.2\n")
	require.NoError(t, err)
	require.False(t, result.Changed())
	require.True(t, result.Usable())
	// A refresh reports the current release first, then the outcome; when
	// the install fails, the first line is what machines keep using.
	result, err = parseAgentCLIRefresh("KUAYLE_AGENT_CLIS kept @opencode/cli=2.3.2\nKUAYLE_AGENT_CLIS changed @opencode/cli=2.3.5\n")
	require.NoError(t, err)
	require.Equal(t, "changed", result.State)
	result, err = parseAgentCLIRefresh("KUAYLE_AGENT_CLIS kept @opencode/cli=2.3.2\n")
	require.NoError(t, err)
	require.True(t, result.Usable())
	for _, output := range []string{"KUAYLE_AGENT_CLIS rejected\n", "KUAYLE_AGENT_CLIS none\n"} {
		result, err = parseAgentCLIRefresh(output)
		require.NoError(t, err)
		require.False(t, result.Usable(), output)
		require.Empty(t, result.Versions)
	}
	for _, output := range []string{"", "KUAYLE_AGENT_CLIS changed", "KUAYLE_AGENT_CLIS kept", "KUAYLE_AGENT_CLIS maybe x=1",
		"KUAYLE_AGENT_CLIS changed claude", "KUAYLE_AGENT_CLIS rejected x=1"} {
		_, err := parseAgentCLIRefresh(output)
		require.Error(t, err, output)
	}
}

// Agent runs pass the argv of this release, so they put the shared CLIs on
// PATH only once a check or refresh of this manager found them to fit the
// release's image; until then, and after they were set aside, runs use the
// image's CLIs. Developer containers always mount the volume, so a later
// refresh reaches them without a restart.
func TestAgentRunsUseTheSharedCLIsOnlyOnceTheyFitTheRelease(t *testing.T) {
	runtime := &DockerRuntime{agentCLIs: true}
	run := func() ([]mount.Mount, []string) {
		hostConfig := &container.HostConfig{}
		return hostConfig.Mounts, runtime.useAgentCLIsForRun(hostConfig, []string{"PATH=/usr/bin"})
	}
	mounts, env := run()
	require.Empty(t, mounts, "not checked yet")
	require.Equal(t, []string{"PATH=/usr/bin"}, env)

	runtime.recordAgentCLIs(AgentCLIRefresh{State: "kept", Versions: map[string]string{"@opencode/cli": "2.3.2"}})
	hostConfig := &container.HostConfig{}
	env = runtime.useAgentCLIsForRun(hostConfig, []string{"PATH=/usr/bin"})
	require.Equal(t, []mount.Mount{{Type: mount.TypeVolume, Source: agentCLIVolume, Target: agentCLIRoot, ReadOnly: true}}, hostConfig.Mounts)
	require.Equal(t, []string{"PATH=" + agentCLIBin + ":/usr/bin"}, env)

	runtime.recordAgentCLIs(AgentCLIRefresh{State: "rejected"})
	mounts, env = run()
	require.Empty(t, mounts, "set aside")
	require.Equal(t, []string{"PATH=/usr/bin"}, env)

	developer := &container.HostConfig{}
	require.Equal(t, []string{"PATH=" + agentCLIBin + ":/usr/bin"}, runtime.useAgentCLIs(developer, []string{"PATH=/usr/bin"}))
	require.Len(t, developer.Mounts, 1)

	disabled := &DockerRuntime{}
	disabled.recordAgentCLIs(AgentCLIRefresh{State: "kept", Versions: map[string]string{"@opencode/cli": "2.3.2"}})
	hostConfig = &container.HostConfig{}
	require.Equal(t, []string{"PATH=/usr/bin"}, disabled.useAgentCLIsForRun(hostConfig, []string{"PATH=/usr/bin"}))
	require.Empty(t, hostConfig.Mounts, "updates are off")
}

// The helper sees only the CLI volume: a machine's workspace (repositories,
// task sessions under /workspace/.kuayle, notebook files), its home with the
// sign-ins, and its secrets are never mounted, so an upgrade cannot change
// them. Only a refresh reaches the network.
func TestAgentCLIHelperSeesOnlyTheCLIVolume(t *testing.T) {
	for _, mode := range []string{"check", "refresh"} {
		options := agentCLIHelperContainer("kuayle/headless:release", mode, time.Unix(0, 0))
		require.Equal(t, []mount.Mount{{Type: mount.TypeVolume, Source: agentCLIVolume, Target: agentCLIRoot}}, options.HostConfig.Mounts, mode)
		require.Empty(t, options.HostConfig.Binds, mode)
		require.True(t, options.HostConfig.ReadonlyRootfs, mode)
		require.Equal(t, []string{"ALL"}, options.HostConfig.CapDrop, mode)
		require.Equal(t, mode, options.Config.Cmd[3])
		for _, variable := range options.Config.Env {
			require.Regexp(t, `^KUAYLE_(RESOLVER|VALIDATOR)=`, variable, "no secrets or machine settings")
		}
	}
	require.Equal(t, container.NetworkMode("none"), agentCLIHelperContainer("image", "check", time.Unix(0, 0)).HostConfig.NetworkMode)
	require.Equal(t, container.NetworkMode(egressNetworkName), agentCLIHelperContainer("image", "refresh", time.Unix(0, 0)).HostConfig.NetworkMode)
}

type agentCLIRuntimeFake struct {
	*runtimeFake
	image      string
	before     []time.Time
	refresh    AgentCLIRefresh
	refreshErr error
	check      AgentCLIRefresh
	checks     int
	onCheck    func()
}

func (f *agentCLIRuntimeFake) RefreshAgentCLIs(_ context.Context, image string, before time.Time) (AgentCLIRefresh, error) {
	f.image = image
	f.before = append(f.before, before)
	return f.refresh, f.refreshErr
}

func (f *agentCLIRuntimeFake) CheckAgentCLIs(_ context.Context, image string) (AgentCLIRefresh, error) {
	f.image = image
	f.checks++
	if f.onCheck != nil {
		f.onCheck()
	}
	return f.check, nil
}

func TestManagerRefreshesAgentCLIsOnItsIntervalWithAMinimumAge(t *testing.T) {
	runtime := &agentCLIRuntimeFake{runtimeFake: &runtimeFake{}, refresh: AgentCLIRefresh{State: "changed", Versions: map[string]string{"@openai/codex": "0.170.1"}}}
	manager := NewManager(&managerStoreFake{machine: &domain.DevMachine{ID: uuid.New()}}, runtime, agent.NewRegistry(), nil, nil, nil, "test")
	manager.SetImages(MachineImages{Headless: "kuayle/headless:release"})
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)

	manager.refreshAgentCLIs(context.Background(), now)
	require.Empty(t, runtime.before, "updates are off unless configured")

	manager.SetAgentCLIUpdates(AgentCLIUpdates{Interval: 6 * time.Hour, MinAge: 48 * time.Hour})
	manager.refreshAgentCLIs(context.Background(), now)
	require.Equal(t, []time.Time{now.Add(-48 * time.Hour)}, runtime.before, "only versions published two days ago or earlier")
	require.Equal(t, "kuayle/headless:release", runtime.image)
	manager.refreshAgentCLIs(context.Background(), now.Add(time.Hour))
	require.Len(t, runtime.before, 1, "not again before the interval")
	manager.refreshAgentCLIs(context.Background(), now.Add(6*time.Hour))
	require.Len(t, runtime.before, 2)
	require.Equal(t, map[string]string{"@openai/codex": "0.170.1"}, manager.AgentCLIVersions())
}

// A failed refresh reports what machines keep using; when the current
// release was set aside, the trace no longer names its versions.
func TestManagerRecordsWhatAFailedRefreshLeftInPlace(t *testing.T) {
	runtime := &agentCLIRuntimeFake{runtimeFake: &runtimeFake{}, refresh: AgentCLIRefresh{State: "changed", Versions: map[string]string{"@opencode/cli": "2.3.5"}}}
	manager := NewManager(&managerStoreFake{machine: &domain.DevMachine{ID: uuid.New()}}, runtime, agent.NewRegistry(), nil, nil, nil, "test")
	manager.SetImages(MachineImages{Headless: "kuayle/headless:release"})
	manager.SetAgentCLIUpdates(AgentCLIUpdates{Interval: 6 * time.Hour, MinAge: 48 * time.Hour})
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	manager.refreshAgentCLIs(context.Background(), now)
	require.Equal(t, map[string]string{"@opencode/cli": "2.3.5"}, manager.AgentCLIVersions())

	runtime.refresh, runtime.refreshErr = AgentCLIRefresh{State: "kept", Versions: map[string]string{"@opencode/cli": "2.3.5"}}, errors.New("npm failed")
	manager.refreshAgentCLIs(context.Background(), now.Add(6*time.Hour))
	require.Equal(t, map[string]string{"@opencode/cli": "2.3.5"}, manager.AgentCLIVersions())

	runtime.refresh = AgentCLIRefresh{State: "rejected"}
	manager.refreshAgentCLIs(context.Background(), now.Add(12*time.Hour))
	require.Empty(t, manager.AgentCLIVersions())

	runtime.refresh = AgentCLIRefresh{}
	manager.refreshAgentCLIs(context.Background(), now.Add(18*time.Hour))
	require.Empty(t, manager.AgentCLIVersions(), "a helper that did not report leaves the record")
}

// A deploy starts a new manager with new images while the volume still holds
// the previous release's CLIs. The manager checks them before it leases any
// work, so no run or developer container it starts finds a CLI on PATH that
// does not fit this release.
func TestManagerChecksTheSharedCLIsBeforeItStartsWork(t *testing.T) {
	store, inner, _, runID := restartRunFixture(t)
	runtime := &agentCLIRuntimeFake{runtimeFake: inner, check: AgentCLIRefresh{State: "kept", Versions: map[string]string{"@openai/codex": "0.172.0"}}}
	manager := NewManager(store, runtime, agent.NewRegistry(agent.NewCodexProvider("test")), nil, cryptoutil.DeriveKey("manager-restart-test"), nil, "test")
	manager.SetImages(MachineImages{Headless: "kuayle/headless:release"})
	manager.SetAgentCLIUpdates(AgentCLIUpdates{Interval: 6 * time.Hour, MinAge: 48 * time.Hour})
	manager.pollInterval = 10 * time.Millisecond
	store.leased = []domain.DevMachineOperation{*restartOperation(store, runID)}
	var mu sync.Mutex
	var events []string
	record := func(event string) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, event)
	}
	runtime.onCheck = func() { record("check") }
	entered := make(chan struct{})
	var once sync.Once
	inner.onRunAgent = func(context.Context, *AgentObserver) (*AgentExecution, error) {
		record("run")
		once.Do(func() { close(entered) })
		return nil, ErrAgentDetached
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- manager.Run(ctx) }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("the leased agent operation never started")
	}
	cancel()
	require.NoError(t, <-done)
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{"check", "run"}, events[:2])
	require.Equal(t, 1, runtime.checks)
	require.Equal(t, "kuayle/headless:release", runtime.image, "the check runs in this release's image")
	require.Equal(t, map[string]string{"@openai/codex": "0.172.0"}, manager.AgentCLIVersions())
}
