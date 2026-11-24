package machine

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

// TestAgentCLIRefreshInstallsStartableCLIs runs the real check and refresh
// against the npm registry (KUAYLE_AGENT_CLI_INTEGRATION=1,
// KUAYLE_TEST_HEADLESS_IMAGE and network access), starting from a volume an
// earlier release left: OpenCode 1.x under its old package name. It uses and
// removes the shared volume, so do not run it on a host whose machines use it.
func TestAgentCLIRefreshInstallsStartableCLIs(t *testing.T) {
	image := os.Getenv("KUAYLE_TEST_HEADLESS_IMAGE")
	if os.Getenv("KUAYLE_AGENT_CLI_INTEGRATION") != "1" || image == "" {
		t.Skip("set KUAYLE_AGENT_CLI_INTEGRATION=1 and KUAYLE_TEST_HEADLESS_IMAGE")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer cancel()
	runtime, err := NewDockerRuntime(DockerConfig{})
	require.NoError(t, err)
	_, _ = runtime.client.VolumeRemove(ctx, agentCLIVolume, client.VolumeRemoveOptions{Force: true})
	t.Cleanup(func() {
		_, _ = runtime.client.VolumeRemove(context.Background(), agentCLIVolume, client.VolumeRemoveOptions{Force: true})
	})
	runtime.EnableAgentCLIs()
	_, err = runtime.ensureNetwork(ctx, egressNetworkName, false, map[string]string{"com.kuayle.kind": "machine-egress"})
	require.NoError(t, err)
	imageOpenCode := strings.TrimSpace(runInAgentCLIImage(t, ctx, runtime, image, "1000:1000", false, `opencode --version`))

	// What an earlier release's refresh left: opencode-ai 1.19.6 is first on
	// a developer container's PATH, ahead of the image's OpenCode 2.
	runInAgentCLIImage(t, ctx, runtime, image, "0:0", true, `set -eu
root=`+agentCLIRoot+`
mkdir -p "$root/releases/20260101000000"
HOME=/tmp npm install --prefix "$root/releases/20260101000000" --no-audit --no-fund --no-save --allow-scripts=opencode-ai opencode-ai@1.19.6 >&2
ln -s releases/20260101000000 "$root/current"`)
	require.Equal(t, "1.19.6", developerRuns(t, ctx, runtime, image, "opencode"))

	// A previous manager's refresh that is still running could switch the
	// link after the check; the check removes it first.
	leftover, err := runtime.client.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{Image: image, Entrypoint: []string{"sleep", "600"},
			Labels: map[string]string{"com.kuayle.managed": "true", "com.kuayle.service": agentCLIService}},
		HostConfig: &container.HostConfig{NetworkMode: "none"},
	})
	require.NoError(t, err)
	_, err = runtime.client.ContainerStart(ctx, leftover.ID, client.ContainerStartOptions{})
	require.NoError(t, err)
	checked, err := runtime.CheckAgentCLIs(ctx, image)
	require.NoError(t, err)
	_, err = runtime.client.ContainerInspect(ctx, leftover.ID, client.ContainerInspectOptions{})
	require.True(t, errdefs.IsNotFound(err), "the previous manager's helper is gone")
	require.Equal(t, "rejected", checked.State)
	require.Contains(t, checked.Detail, "do not fit this release")
	require.False(t, runtime.agentCLIsFit.Load(), "agent runs use their image's CLIs")
	require.Equal(t, imageOpenCode, developerRuns(t, ctx, runtime, image, "opencode"), "set aside at once, also for running containers")

	first, err := runtime.RefreshAgentCLIs(ctx, image, time.Now().Add(-48*time.Hour))
	require.NoError(t, err)
	require.True(t, first.Changed())
	require.Len(t, first.Versions, len(agentCLIPackages))
	require.True(t, runtime.agentCLIsFit.Load())
	require.Equal(t, first.Versions["@opencode/cli"], developerRuns(t, ctx, runtime, image, "opencode"))
	again, err := runtime.RefreshAgentCLIs(ctx, image, time.Now().Add(-48*time.Hour))
	require.NoError(t, err)
	require.False(t, again.Changed(), "the same versions do not replace the current ones")
	require.Equal(t, first.Versions, again.Versions)
	checked, err = runtime.CheckAgentCLIs(ctx, image)
	require.NoError(t, err)
	require.Equal(t, "kept", checked.State, "what a refresh installs fits the image it ran in")
	require.Equal(t, first.Versions, checked.Versions)

	// A developer container sees the refreshed CLIs first and cannot change them.
	output := runInAgentCLIImage(t, ctx, runtime, image, "1000:1000", false,
		`command -v claude; claude --version; touch `+agentCLIRoot+`/x 2>/dev/null && echo writable || echo read-only`)
	lines := strings.Split(strings.TrimSpace(output), "\n")
	require.Len(t, lines, 3)
	require.Equal(t, agentCLIBin+"/claude", lines[0])
	require.Contains(t, lines[1], first.Versions["@anthropic-ai/claude-code"])
	require.Equal(t, "read-only", lines[2])
}

// developerRuns is the version of command a developer container starts.
func developerRuns(t *testing.T, ctx context.Context, runtime *DockerRuntime, image, command string) string {
	t.Helper()
	output := runInAgentCLIImage(t, ctx, runtime, image, "1000:1000", false, command+` --version`)
	fields := strings.Fields(output)
	require.NotEmpty(t, fields)
	return fields[len(fields)-1]
}

// runInAgentCLIImage runs script in image: as root with the volume writable
// and the egress network to seed it, otherwise as a developer container
// (read-only volume on PATH) without a network.
func runInAgentCLIImage(t *testing.T, ctx context.Context, runtime *DockerRuntime, image, user string, seed bool, script string) string {
	t.Helper()
	hostConfig := &container.HostConfig{NetworkMode: "none"}
	environment := []string{"PATH=/usr/local/bin:/usr/bin:/bin"}
	var networking *network.NetworkingConfig
	if seed {
		hostConfig.NetworkMode = container.NetworkMode(egressNetworkName)
		hostConfig.Mounts = []mount.Mount{{Type: mount.TypeVolume, Source: agentCLIVolume, Target: agentCLIRoot}}
		networking = &network.NetworkingConfig{EndpointsConfig: map[string]*network.EndpointSettings{egressNetworkName: {}}}
	} else {
		environment = runtime.useAgentCLIs(hostConfig, environment)
		require.Equal(t, []mount.Mount{{Type: mount.TypeVolume, Source: agentCLIVolume, Target: agentCLIRoot, ReadOnly: true}}, hostConfig.Mounts)
	}
	response, err := runtime.client.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config:     &container.Config{Image: image, User: user, Env: append(environment, "HOME=/tmp"), Entrypoint: []string{"/bin/sh", "-c"}, Cmd: []string{script}},
		HostConfig: hostConfig, NetworkingConfig: networking,
	})
	require.NoError(t, err)
	defer func() {
		_, _ = runtime.client.ContainerRemove(context.Background(), response.ID, client.ContainerRemoveOptions{Force: true})
	}()
	_, err = runtime.client.ContainerStart(ctx, response.ID, client.ContainerStartOptions{})
	require.NoError(t, err)
	wait := runtime.client.ContainerWait(ctx, response.ID, client.ContainerWaitOptions{Condition: container.WaitConditionNotRunning})
	select {
	case result := <-wait.Result:
		stdout, stderr, err := runtime.containerLogs(ctx, response.ID)
		require.NoError(t, err)
		require.Zero(t, result.StatusCode, stderr)
		return stdout
	case err := <-wait.Error:
		require.NoError(t, err)
	}
	return ""
}
