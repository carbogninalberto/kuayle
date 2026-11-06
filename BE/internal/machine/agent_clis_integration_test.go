package machine

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

// TestAgentCLIRefreshInstallsStartableCLIs runs the real refresh against the
// npm registry (KUAYLE_AGENT_CLI_INTEGRATION=1, KUAYLE_TEST_HEADLESS_IMAGE and
// network access). It uses and removes the shared volume, so do not run it on
// a host whose machines use it.
func TestAgentCLIRefreshInstallsStartableCLIs(t *testing.T) {
	image := os.Getenv("KUAYLE_TEST_HEADLESS_IMAGE")
	if os.Getenv("KUAYLE_AGENT_CLI_INTEGRATION") != "1" || image == "" {
		t.Skip("set KUAYLE_AGENT_CLI_INTEGRATION=1 and KUAYLE_TEST_HEADLESS_IMAGE")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer cancel()
	runtime, err := NewDockerRuntime(DockerConfig{})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = runtime.client.VolumeRemove(context.Background(), agentCLIVolume, client.VolumeRemoveOptions{Force: true})
	})
	first, err := runtime.RefreshAgentCLIs(ctx, image, time.Now().Add(-48*time.Hour))
	require.NoError(t, err)
	require.True(t, first.Changed)
	require.Len(t, first.Versions, len(agentCLIPackages))
	again, err := runtime.RefreshAgentCLIs(ctx, image, time.Now().Add(-48*time.Hour))
	require.NoError(t, err)
	require.False(t, again.Changed, "the same versions do not replace the current ones")
	require.Equal(t, first.Versions, again.Versions)

	// A developer container sees the refreshed CLIs first and cannot change them.
	runtime.EnableAgentCLIs()
	hostConfig := &container.HostConfig{}
	environment := runtime.useAgentCLIs(hostConfig, []string{"PATH=/usr/local/bin:/usr/bin:/bin"})
	response, err := runtime.client.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{Image: image, User: "1000:1000", Env: environment, Entrypoint: []string{"/bin/sh", "-c"},
			Cmd: []string{`command -v claude; claude --version; touch ` + agentCLIRoot + `/x 2>/dev/null && echo writable || echo read-only`}},
		HostConfig: hostConfig,
	})
	require.NoError(t, err)
	defer func() {
		_, _ = runtime.client.ContainerRemove(context.Background(), response.ID, client.ContainerRemoveOptions{Force: true})
	}()
	require.Equal(t, []mount.Mount{{Type: mount.TypeVolume, Source: agentCLIVolume, Target: agentCLIRoot, ReadOnly: true}}, hostConfig.Mounts)
	_, err = runtime.client.ContainerStart(ctx, response.ID, client.ContainerStartOptions{})
	require.NoError(t, err)
	wait := runtime.client.ContainerWait(ctx, response.ID, client.ContainerWaitOptions{Condition: container.WaitConditionNotRunning})
	select {
	case <-wait.Result:
	case err := <-wait.Error:
		require.NoError(t, err)
	}
	stdout, stderr, err := runtime.containerLogs(ctx, response.ID)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	require.Len(t, lines, 3, stderr)
	require.Equal(t, agentCLIBin+"/claude", lines[0])
	require.Contains(t, lines[1], first.Versions["@anthropic-ai/claude-code"])
	require.Equal(t, "read-only", lines[2])
}
