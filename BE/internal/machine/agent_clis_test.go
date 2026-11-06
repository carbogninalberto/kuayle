package machine

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
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

func TestAgentCLIRefreshReportParsesTheHelperOutput(t *testing.T) {
	result, err := parseAgentCLIRefresh("npm notice\nKUAYLE_AGENT_CLIS changed @anthropic-ai/claude-code=2.2.0 @openai/codex=0.170.1\n")
	require.NoError(t, err)
	require.True(t, result.Changed)
	require.Equal(t, map[string]string{"@anthropic-ai/claude-code": "2.2.0", "@openai/codex": "0.170.1"}, result.Versions)
	result, err = parseAgentCLIRefresh("KUAYLE_AGENT_CLIS unchanged opencode-ai=1.19.5\n")
	require.NoError(t, err)
	require.False(t, result.Changed)
	for _, output := range []string{"", "KUAYLE_AGENT_CLIS changed", "KUAYLE_AGENT_CLIS maybe x=1", "KUAYLE_AGENT_CLIS changed claude"} {
		_, err := parseAgentCLIRefresh(output)
		require.Error(t, err, output)
	}
}

type agentCLIRuntimeFake struct {
	*runtimeFake
	image   string
	before  []time.Time
	refresh AgentCLIRefresh
}

func (f *agentCLIRuntimeFake) RefreshAgentCLIs(_ context.Context, image string, before time.Time) (AgentCLIRefresh, error) {
	f.image = image
	f.before = append(f.before, before)
	return f.refresh, nil
}

func TestManagerRefreshesAgentCLIsOnItsIntervalWithAMinimumAge(t *testing.T) {
	runtime := &agentCLIRuntimeFake{runtimeFake: &runtimeFake{}, refresh: AgentCLIRefresh{Changed: true, Versions: map[string]string{"@openai/codex": "0.170.1"}}}
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
