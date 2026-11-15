package machine

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/containerd/errdefs"
	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	log "github.com/sirupsen/logrus"
)

// An assistant command runs in a fresh check container, like a custom tool:
// the developer image, the machine's workspace volume, no credentials and
// egress only through the proxy. The command reaches the container only as
// the KUAYLE_COMMAND variable, never in its argv. A read-only run mounts
// /workspace read-only, so only /tmp and $HOME (tmpfs) are writable; a write
// run mounts it read-write and starts in the conversation's folder.

// commandBootstrap is the fixed command line of every command container. It
// enters the working directory (a write run creates it), then runs the
// command with sh under a hard kill timeout ($1 seconds).
const commandBootstrap = `set -u
if [ "${KUAYLE_COMMAND_WRITE:-0}" = 1 ]; then mkdir -p "$KUAYLE_COMMAND_CWD" 2>/dev/null; fi
cd "$KUAYLE_COMMAND_CWD" 2>/dev/null || { echo "kuayle: the working directory $KUAYLE_COMMAND_CWD does not exist" >&2; exit 2; }
kuayle_command=$KUAYLE_COMMAND
unset KUAYLE_COMMAND KUAYLE_COMMAND_CWD KUAYLE_COMMAND_WRITE
if command -v timeout >/dev/null 2>&1; then exec timeout -s KILL "$1" sh -c "$kuayle_command"; fi
exec sh -c "$kuayle_command"`

// commandArgv is the container's argv after the sh entrypoint.
func commandArgv() []string {
	return []string{"-c", commandBootstrap, "kuayle-cmd", strconv.Itoa(int(domain.CommandRunTimeout / time.Second))}
}

// commandEnvironment is a command container's whole environment.
func commandEnvironment(machine *domain.DevMachine, run *domain.CommandRun) []string {
	write := "0"
	if run.Write {
		write = "1"
	}
	env := []string{"HOME=/home/kuayle", "CI=true", "KUAYLE_COMMAND=" + run.Command, "KUAYLE_COMMAND_CWD=" + run.WorkingDirectory, "KUAYLE_COMMAND_WRITE=" + write}
	return append(env, proxyVariables(egressProxyURL(machine), "localhost,127.0.0.1")...)
}

func commandContainerName(machine *domain.DevMachine, run uuid.UUID) string {
	return "kuayle-" + machine.RoutingKey + "-cmd-" + run.String()
}

// commandHostConfig is a check container's host config with /workspace
// read-only unless the run may write.
func commandHostConfig(hostConfig *container.HostConfig, write bool) *container.HostConfig {
	for i := range hostConfig.Mounts {
		if hostConfig.Mounts[i].Target == "/workspace" {
			hostConfig.Mounts[i].ReadOnly = !write
		}
	}
	return hostConfig
}

// RunCommand runs an approved command in its own check container, or resumes
// the container this lease already started.
func (r *DockerRuntime) RunCommand(ctx context.Context, machine *domain.DevMachine, run *domain.CommandRun, image string, observer *AgentObserver) (*AgentExecution, error) {
	if machine.DockerNetworkName == nil || machine.WorkspaceVolumeName == nil || run == nil {
		return nil, fmt.Errorf("command runtime resources are unavailable")
	}
	if clean, ok := domain.CommandWorkingDirectory(run.WorkingDirectory); !ok || clean != run.WorkingDirectory || strings.ContainsRune(run.Command, 0) {
		return nil, fmt.Errorf("invalid command run")
	}
	if machine.EnvironmentID != nil {
		if err := r.ensureEnvironmentImage(ctx, image, machine.WorkspaceID, *machine.EnvironmentID); err != nil {
			return nil, err
		}
	} else if err := r.ensureImage(ctx, image); err != nil {
		return nil, err
	}
	cmd := commandArgv()
	name := commandContainerName(machine, run.ID)
	labels := machineLabels(machine)
	labels["com.kuayle.service"] = "agent"
	labels["com.kuayle.command-run-id"] = run.ID.String()
	if result, err := r.client.ContainerInspect(ctx, name, client.ContainerInspectOptions{}); err == nil {
		config := result.Container.Config
		if result.Container.State == nil || config == nil || config.Image != image || !slices.Equal(config.Entrypoint, []string{"sh"}) || !slices.Equal(config.Cmd, cmd) {
			return nil, fmt.Errorf("command container does not match the accepted run")
		}
		for key, value := range labels {
			if config.Labels[key] != value {
				return nil, fmt.Errorf("command container ownership does not match")
			}
		}
		return r.resumeExecution(ctx, result.Container, false, nil, observer, false)
	} else if !errdefs.IsNotFound(err) {
		return nil, err
	}
	if observer != nil && observer.Authorize != nil {
		if err := observer.Authorize(); err != nil {
			return nil, err
		}
	}
	hostConfig := commandHostConfig(r.secureHostConfig(machine, "agent", *machine.DockerNetworkName, *machine.WorkspaceVolumeName, true), run.Write)
	if err := r.scopeMachineResources(ctx, machine, "agent", labels, hostConfig); err != nil {
		return nil, err
	}
	result, err := r.client.ContainerCreate(ctx, client.ContainerCreateOptions{
		Name: name,
		Config: &container.Config{Image: image, User: "1000:1000", WorkingDir: "/workspace", Entrypoint: []string{"sh"}, Cmd: cmd, Labels: labels,
			AttachStdout: true, AttachStderr: true, Env: commandEnvironment(machine, run)},
		HostConfig:       hostConfig,
		NetworkingConfig: &network.NetworkingConfig{EndpointsConfig: map[string]*network.EndpointSettings{*machine.DockerNetworkName: {}}},
	})
	if err != nil {
		return nil, err
	}
	return r.resumeExecution(ctx, container.InspectResponse{ID: result.ID, State: &container.State{Status: "created"}}, false, nil, observer, false)
}

// RemoveCommand deletes a run's container. The recorded result stays.
func (r *DockerRuntime) RemoveCommand(ctx context.Context, machine *domain.DevMachine, run uuid.UUID) error {
	result, err := r.client.ContainerInspect(ctx, commandContainerName(machine, run), client.ContainerInspectOptions{})
	if errdefs.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if result.Container.Config == nil || !ownedByMachine(result.Container.Config.Labels, machine.ID) || result.Container.Config.Labels["com.kuayle.command-run-id"] != run.String() {
		return fmt.Errorf("command container ownership does not match")
	}
	if _, err = r.client.ContainerRemove(ctx, result.Container.ID, client.ContainerRemoveOptions{Force: true}); err != nil && !errdefs.IsNotFound(err) {
		return err
	}
	return nil
}

// commandRunFailure is the reason shown for a run that could not finish: a
// terminal reason is a limit or the machine's state and is safe to show;
// other infrastructure errors stay generic.
func commandRunFailure(err error) string {
	var terminal *terminalOperationError
	if errors.As(err, &terminal) {
		return terminal.message
	}
	return "The command could not finish"
}

// commandRunDeadline is the manager's hard stop for a run: the command's
// timeout plus a grace period that lets the container's own kill fire first.
var commandRunDeadline = domain.CommandRunTimeout + 15*time.Second

// runCommand executes an approved command run once. The store re-checks the
// run's authority (the requester still owns the running machine and may use
// it) for every write; the container gets no credentials.
func (m *Manager) runCommand(ctx context.Context, machine *domain.DevMachine, op *domain.DevMachineOperation) error {
	run, err := m.store.GetCommandRun(ctx, *op)
	switch {
	case errors.Is(err, repository.ErrCommandMachineUnavailable), errors.Is(err, repository.ErrCommandRunForbidden):
		return &terminalOperationError{code: "command_unauthorized", message: "The run is no longer allowed on this machine"}
	case err != nil:
		return err
	}
	if run.Status != "queued" && run.Status != "running" {
		return nil
	}
	image := ""
	services, err := m.store.ListServices(ctx, machine.WorkspaceID, machine.ID)
	if err != nil {
		return err
	}
	for _, service := range services {
		if service.ServiceType == "ide" || service.ServiceType == "terminal" {
			image = service.ImageRef
			break
		}
	}
	if image == "" {
		return &terminalOperationError{code: "command_unavailable", message: "Developer environment is unavailable"}
	}
	runCtx, cancel := context.WithTimeout(ctx, commandRunDeadline)
	defer cancel()
	writer := &toolOutputWriter{last: time.Now(), write: func(content string, truncated bool) error {
		writeCtx, done := context.WithTimeout(runCtx, 5*time.Second)
		defer done()
		return m.store.AppendCommandRunOutput(writeCtx, *op, content, truncated)
	}}
	// Nothing on the machine holds Kuayle credentials; the buffer only
	// bounds and stores the output.
	output := newAgentOutputBuffer(map[string]string{}, writer.persist)
	observer := &AgentObserver{
		Authorize: func() error { _, err := m.store.GetCommandRun(runCtx, *op); return err },
		Started:   func(string) error { return m.store.StartCommandRun(runCtx, *op) },
		Output:    output.WriteDocker,
	}
	result, execErr := m.runtime.RunCommand(runCtx, machine, run, image, observer)
	if execErr == nil {
		execErr = output.Finish()
	}
	if execErr == nil {
		execErr = writer.flush()
	}
	cleanup := func() {
		cleanupCtx, done := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer done()
		if err := m.runtime.RemoveCommand(cleanupCtx, machine, run.ID); err != nil {
			log.WithField("command_run_id", run.ID).WithError(err).Warn("command container cleanup failed")
		}
	}
	if execErr != nil {
		// A command runs at most once: its container is removed whatever happened.
		cleanup()
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return &terminalOperationError{code: "command_timeout", message: fmt.Sprintf("The command took too long and was stopped after %d seconds", int(domain.CommandRunTimeout/time.Second))}
		}
		if errors.Is(execErr, repository.ErrCommandMachineUnavailable) || errors.Is(execErr, repository.ErrCommandRunForbidden) {
			return &terminalOperationError{code: "command_unauthorized", message: "The run is no longer allowed on this machine"}
		}
		return execErr
	}
	if result == nil {
		cleanup()
		return errors.New("command execution returned no result")
	}
	if err = m.store.CompleteCommandRun(ctx, *op, result.ExitCode); err != nil {
		cleanup()
		return err
	}
	cleanup()
	return nil
}

// expireTemporaryMachines asks for the permanent deletion of every temporary
// machine past its expiry; the teardown and purge follow as for a user's
// permanent delete.
func (m *Manager) expireTemporaryMachines(ctx context.Context, now time.Time) {
	expired, err := m.store.ListExpiredTemporaryMachines(ctx, now, 100)
	if err != nil {
		log.WithError(err).Warn("expired temporary machine listing failed")
		return
	}
	for i := range expired {
		machine := &expired[i]
		if _, err := m.store.RequestPermanentDelete(ctx, machine.WorkspaceID, machine.ID, nil); err != nil && !errors.Is(err, sql.ErrNoRows) {
			log.WithField("machine_id", machine.ID).WithError(err).Warn("temporary machine deletion failed")
			continue
		}
		_ = m.createEvent(ctx, machine, nil, "lifecycle", "machine.temporary_expired", map[string]any{"expired_at": machine.TemporaryExpiresAt})
	}
}
