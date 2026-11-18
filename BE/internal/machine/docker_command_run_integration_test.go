package machine

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/containerd/errdefs"
	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

// commandIntegrationImage stands in for the developer image: it has sh,
// busybox timeout and python3. Override it with KUAYLE_COMMAND_TEST_IMAGE.
const commandIntegrationImage = "python:3.12-alpine"

// tailCommandStore keeps only the stored tail of the output, as the
// repository's AppendCommandRunOutput does (tested against Postgres in
// repository.TestCommandRunsAreQueuedOnlyOnTheRequestersMachine).
type tailCommandStore struct {
	*commandStoreFake
	truncated bool
}

func (s *tailCommandStore) AppendCommandRunOutput(_ context.Context, _ domain.DevMachineOperation, content string, truncated bool) error {
	s.output += content
	if len(s.output) > domain.CommandOutputBytes {
		s.output, truncated = s.output[len(s.output)-domain.CommandOutputBytes:], true
	}
	s.truncated = s.truncated || truncated
	return nil
}

type dockerCommandFixture struct {
	runtime *DockerRuntime
	machine *domain.DevMachine
	image   string
	// createLatency is how long the daemon took to run one helper
	// container; a shared CI daemon can take over a minute.
	createLatency time.Duration
}

func newDockerCommandFixture(t *testing.T) dockerCommandFixture {
	t.Helper()
	if os.Getenv("KUAYLE_DOCKER_INTEGRATION") != "1" {
		t.Skip("set KUAYLE_DOCKER_INTEGRATION=1 to run Docker runtime tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	runtime, err := NewDockerRuntime(DockerConfig{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = runtime.client.Close() })
	ping, err := runtime.client.Ping(ctx, client.PingOptions{})
	require.NoError(t, err, "Docker must be reachable")
	if !apiAtLeast(ping.APIVersion, volumeSubpathAPI) {
		t.Skipf("Docker API %s cannot mount volume subpaths", ping.APIVersion)
	}
	image := commandIntegrationImage
	if override := os.Getenv("KUAYLE_COMMAND_TEST_IMAGE"); override != "" {
		image = override
	}
	if _, err := runtime.client.ImageInspect(ctx, image); errdefs.IsNotFound(err) {
		reader, pullErr := runtime.client.ImagePull(ctx, image, client.ImagePullOptions{})
		require.NoError(t, pullErr)
		_, _ = reader.Read(make([]byte, 1))
		_ = reader.Close()
		require.Eventually(t, func() bool { _, err := runtime.client.ImageInspect(ctx, image); return err == nil }, 50*time.Second, time.Second)
	}

	key := strings.ReplaceAll(uuid.NewString(), "-", "")[:16]
	// The default bridge stands in for the machine network: creating one can
	// take minutes on a busy daemon, and the command needs no peers.
	network, volume := "bridge", "kuayle-cmdtest-"+key
	_, err = runtime.client.VolumeCreate(ctx, client.VolumeCreateOptions{Name: volume})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = runtime.client.VolumeRemove(context.Background(), volume, client.VolumeRemoveOptions{Force: true})
	})
	f := dockerCommandFixture{runtime: runtime, image: image, machine: &domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New(), RoutingKey: key,
		DockerNetworkName: &network, WorkspaceVolumeName: &volume, CPUMillis: 1000, MemoryMB: 2048, DiskGB: 20, PidsLimit: 256}}
	// The developer user owns /workspace, as on a machine.
	started := time.Now()
	_, code := f.inspectVolume(t, "chown 1000:1000 /workspace")
	f.createLatency = time.Since(started)
	require.Zero(t, code)
	return f
}

// inspectVolume runs a root shell script against the workspace volume and
// returns its output and exit code.
func (f dockerCommandFixture) inspectVolume(t *testing.T, script string) (string, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	created, err := f.runtime.client.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config:     &container.Config{Image: f.image, Entrypoint: []string{"sh", "-c", script}, NetworkDisabled: true},
		HostConfig: &container.HostConfig{NetworkMode: "none", Mounts: []mount.Mount{{Type: mount.TypeVolume, Source: *f.machine.WorkspaceVolumeName, Target: "/workspace"}}},
	})
	require.NoError(t, err)
	defer func() {
		_, _ = f.runtime.client.ContainerRemove(context.Background(), created.ID, client.ContainerRemoveOptions{Force: true})
	}()
	_, err = f.runtime.client.ContainerStart(ctx, created.ID, client.ContainerStartOptions{})
	require.NoError(t, err)
	wait := f.runtime.client.ContainerWait(ctx, created.ID, client.ContainerWaitOptions{Condition: container.WaitConditionNotRunning})
	var code int
	select {
	case result := <-wait.Result:
		code = int(result.StatusCode)
	case err := <-wait.Error:
		require.NoError(t, err)
	}
	stdout, stderr, err := f.runtime.containerLogs(ctx, created.ID)
	require.NoError(t, err)
	return stdout + stderr, code
}

// run executes one command through the manager, as an approved run does,
// and returns what the store recorded.
func (f dockerCommandFixture) run(t *testing.T, command string, write bool) (*tailCommandStore, error) {
	t.Helper()
	run := &domain.CommandRun{ID: uuid.New(), Command: command, Write: write, WorkingDirectory: "/workspace", Status: "queued", CreatedAt: time.Now()}
	if write {
		run.WorkingDirectory = domain.CommandFolder(uuid.New())
	}
	store := &tailCommandStore{commandStoreFake: &commandStoreFake{managerStoreFake: &managerStoreFake{machine: f.machine,
		services: []domain.DevMachineService{{ServiceType: "terminal", ImageRef: f.image}}}, run: run}}
	manager := &Manager{store: store, runtime: f.runtime}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	err := manager.processOperation(ctx, &domain.DevMachineOperation{Action: domain.DevMachineOpRunCommand, MachineID: f.machine.ID, IdempotencyKey: "command-run:" + run.ID.String()})
	for _, name := range []string{commandContainerName(f.machine, run.ID), commandPrepName(f.machine, run.ID)} {
		_, inspectErr := f.runtime.client.ContainerInspect(context.Background(), name, client.ContainerInspectOptions{})
		require.True(t, errdefs.IsNotFound(inspectErr), "%s is removed after the run", name)
	}
	return store, err
}

func TestDockerCommandRunsOnARealContainer(t *testing.T) {
	f := newDockerCommandFixture(t)
	// The manager's deadline also covers creating the container, which a
	// shared daemon can take minutes for; one subtest shortens it on purpose.
	deadline := commandRunDeadline
	commandRunDeadline = 10 * time.Minute
	t.Cleanup(func() { commandRunDeadline = deadline })

	t.Run("returns the output and exit code", func(t *testing.T) {
		store, err := f.run(t, `python3 -c 'print(2**100)'`, false)
		require.NoError(t, err)
		require.Equal(t, 0, *store.exit)
		require.Equal(t, "1267650600228229401496703205376\n", store.output)
		require.False(t, store.truncated)
	})

	t.Run("a read-only run writes only its tmpfs", func(t *testing.T) {
		store, err := f.run(t, `touch /workspace/x 2>/dev/null && echo workspace-written; echo scratch > /tmp/t && cat /tmp/t; echo home > "$HOME/h" && cat "$HOME/h"`, false)
		require.NoError(t, err)
		require.Equal(t, 0, *store.exit)
		require.Equal(t, "scratch\nhome\n", store.output)
		out, _ := f.inspectVolume(t, "ls -A /workspace")
		require.NotContains(t, out, "x")
	})

	t.Run("a write run changes only its conversation folder", func(t *testing.T) {
		store, err := f.run(t, `pwd; echo hi > inside.txt && echo inside-written; touch /workspace/outside 2>/dev/null || echo outside-refused; touch ../sibling 2>/dev/null || echo sibling-refused`, true)
		require.NoError(t, err)
		require.Equal(t, 0, *store.exit)
		folder := store.run.WorkingDirectory
		require.Equal(t, folder+"\ninside-written\noutside-refused\nsibling-refused\n", store.output)
		out, code := f.inspectVolume(t, "cat "+folder+"/inside.txt; ls -A /workspace /workspace/kuayle-assistant")
		require.Zero(t, code, out)
		require.Contains(t, out, "hi\n")
		require.NotContains(t, out, "outside")
		require.NotContains(t, out, "sibling")
	})

	t.Run("a write run refuses a folder that is a link", func(t *testing.T) {
		run := &domain.CommandRun{ID: uuid.New(), Command: "touch pwned", Write: true, WorkingDirectory: domain.CommandFolder(uuid.New()), Status: "queued"}
		_, code := f.inspectVolume(t, "mkdir -p /workspace/kuayle-assistant && ln -s /workspace "+run.WorkingDirectory)
		require.Zero(t, code)
		_, err := f.runtime.RunCommand(context.Background(), f.machine, run, f.image, "", &AgentObserver{})
		require.Equal(t, "The conversation folder could not be prepared on this machine", commandRunFailure(err))
		require.NoError(t, f.runtime.RemoveCommand(context.Background(), f.machine, run.ID))
		out, _ := f.inspectVolume(t, "ls -A /workspace")
		require.NotContains(t, out, "pwned")
	})

	t.Run("the in-container timeout kills the command", func(t *testing.T) {
		timeout := commandTimeout
		commandTimeout = 2 * time.Second
		t.Cleanup(func() { commandTimeout = timeout })
		store, err := f.run(t, "sleep 120; echo slept", false)
		require.NoError(t, err)
		require.NotNil(t, store.exit)
		require.Equal(t, 137, *store.exit, "killed by SIGKILL")
		require.NotContains(t, store.output, "slept", "the command never finished")
	})

	t.Run("the manager's deadline stops a command that outlives it", func(t *testing.T) {
		// Room to create and start the container, far less than the sleep;
		// the in-container timeout is pushed past it, so only the deadline fires.
		timeout := commandTimeout
		commandTimeout = time.Hour
		t.Cleanup(func() { commandTimeout = timeout })
		commandRunDeadline = 2*f.createLatency + 20*time.Second
		started := time.Now()
		store, err := f.run(t, "sleep 900", false)
		var terminal *terminalOperationError
		require.True(t, errors.As(err, &terminal), "%v", err)
		require.Equal(t, "The command took too long and was stopped after 60 seconds", commandRunFailure(err))
		require.Less(t, time.Since(started), 10*time.Minute)
		require.Nil(t, store.exit)
		commandRunDeadline = 10 * time.Minute
	})

	t.Run("long output keeps its end", func(t *testing.T) {
		// 1500 lines of 100 bytes: past the 64 KiB stored tail.
		store, err := f.run(t, `i=0; while [ $i -lt 1500 ]; do printf '%099d\n' $i; i=$((i+1)); done; echo the-end`, false)
		require.NoError(t, err)
		require.Equal(t, 0, *store.exit)
		require.True(t, store.truncated)
		require.Len(t, store.output, domain.CommandOutputBytes)
		require.True(t, strings.HasSuffix(store.output, "\n"+strings.Repeat("0", 95)+"1499\nthe-end\n"), store.output[len(store.output)-200:])
	})
}
