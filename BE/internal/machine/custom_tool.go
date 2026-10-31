package machine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
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

// A custom tool runs its script in a check container: the developer image,
// the machine's workspace volume, no credentials and egress only through the
// proxy. Arguments reach the script only as data — KUAYLE_ARG_<NAME>
// variables and a JSON file — never in a command line.

// toolBootstrap is the fixed command line of every tool container. It writes
// the script and the inputs from the environment to private files and runs
// the script with the interpreter named by $1. Nothing from the request is
// part of it, so arguments cannot be interpreted as shell syntax. Temporary
// files and caches use the run's directory on the workspace volume, which is
// removed when the script ends.
const toolBootstrap = runTmpPrepare + `set -eu
umask 077
dir=$(mktemp -d "${TMPDIR:-/tmp}/kuayle-tool.XXXXXX")
printf '%s' "$KUAYLE_TOOL_SCRIPT" > "$dir/script"
printf '%s' "$KUAYLE_TOOL_INPUT" > "$dir/input.json"
unset KUAYLE_TOOL_SCRIPT KUAYLE_TOOL_INPUT
export KUAYLE_TOOL_INPUT_FILE="$dir/input.json"
set +e
"$1" "$dir/script"
kuayle_status=$?
[ -z "$kuayle_tmp" ] || kuayle_tmp_clean "$kuayle_tmp"
exit "$kuayle_status"`

// toolOutputFlush bounds how long output waits before it is stored.
const toolOutputFlush = time.Second

// toolCommand is the container's argv after the sh entrypoint.
func toolCommand(interpreter string) ([]string, error) {
	if !slices.Contains(domain.CustomToolInterpreters, interpreter) {
		return nil, fmt.Errorf("unsupported tool interpreter")
	}
	return []string{"-c", toolBootstrap, "kuayle-tool", interpreter}, nil
}

// toolEnvironment is a tool container's whole environment: the check
// container's, the script, the canonical inputs and one variable per input.
func toolEnvironment(machine *domain.DevMachine, execution *domain.AssistantToolRunExecution) ([]string, error) {
	values, err := domain.DecodeToolInputs(execution.Run.Inputs)
	if err != nil {
		return nil, err
	}
	args, err := domain.ToolArgumentEnvironment(values)
	if err != nil {
		return nil, err
	}
	if strings.ContainsRune(execution.Script, 0) {
		return nil, fmt.Errorf("invalid tool script")
	}
	env := []string{"HOME=/home/kuayle", "CI=true", "HTTP_PROXY=http://" + machine.RoutingKey + "-egress:3128", "HTTPS_PROXY=http://" + machine.RoutingKey + "-egress:3128", "NO_PROXY=localhost,127.0.0.1",
		"KUAYLE_TOOL_NAME=" + execution.Run.ToolName, "KUAYLE_TOOL_SCRIPT=" + execution.Script, "KUAYLE_TOOL_INPUT=" + string(execution.Run.Inputs)}
	env = append(env, runTmpEnvironment(runTmpDir("tool-"+execution.Run.ID.String()))...)
	return append(env, args...), nil
}

func toolContainerName(machine *domain.DevMachine, run uuid.UUID) string {
	return "kuayle-" + machine.RoutingKey + "-tool-" + run.String()
}

// RunTool runs a custom tool's script in its own check container, or resumes
// the container a previous lease started.
func (r *DockerRuntime) RunTool(ctx context.Context, machine *domain.DevMachine, execution *domain.AssistantToolRunExecution, image string, observer *AgentObserver) (*AgentExecution, error) {
	if machine.DockerNetworkName == nil || machine.WorkspaceVolumeName == nil || execution == nil {
		return nil, fmt.Errorf("tool runtime resources are unavailable")
	}
	run := execution.Run
	if !strings.HasPrefix(run.WorkingDirectory, "/workspace") || strings.Contains(run.WorkingDirectory, "..") {
		return nil, fmt.Errorf("invalid tool working directory")
	}
	cmd, err := toolCommand(execution.Interpreter)
	if err != nil {
		return nil, err
	}
	env, err := toolEnvironment(machine, execution)
	if err != nil {
		return nil, err
	}
	if machine.EnvironmentID != nil {
		if err := r.ensureEnvironmentImage(ctx, image, machine.WorkspaceID, *machine.EnvironmentID); err != nil {
			return nil, err
		}
	} else if err := r.ensureImage(ctx, image); err != nil {
		return nil, err
	}
	name := toolContainerName(machine, run.ID)
	labels := machineLabels(machine)
	labels["com.kuayle.service"] = "agent"
	labels["com.kuayle.tool-run-id"] = run.ID.String()
	if result, err := r.client.ContainerInspect(ctx, name, client.ContainerInspectOptions{}); err == nil {
		config := result.Container.Config
		if result.Container.State == nil || config == nil || config.Image != image || !slices.Equal(config.Entrypoint, []string{"sh"}) || !slices.Equal(config.Cmd, cmd) || config.WorkingDir != run.WorkingDirectory {
			return nil, fmt.Errorf("tool container does not match the accepted run")
		}
		for key, value := range labels {
			if config.Labels[key] != value {
				return nil, fmt.Errorf("tool container ownership does not match")
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
	result, err := r.client.ContainerCreate(ctx, client.ContainerCreateOptions{
		Name: name,
		Config: &container.Config{Image: image, User: "1000:1000", WorkingDir: run.WorkingDirectory, Entrypoint: []string{"sh"}, Cmd: cmd, Labels: labels,
			AttachStdout: true, AttachStderr: true, Env: env},
		// The same share and isolation as a check container.
		HostConfig:       r.secureHostConfig(machine, "agent", *machine.DockerNetworkName, *machine.WorkspaceVolumeName, true),
		NetworkingConfig: &network.NetworkingConfig{EndpointsConfig: map[string]*network.EndpointSettings{*machine.DockerNetworkName: {}}},
	})
	if err != nil {
		return nil, err
	}
	return r.resumeExecution(ctx, container.InspectResponse{ID: result.ID, State: &container.State{Status: "created"}}, false, nil, observer, false)
}

// RemoveTool deletes a finished run's container. The recorded result and
// output stay.
func (r *DockerRuntime) RemoveTool(ctx context.Context, machine *domain.DevMachine, run uuid.UUID) error {
	result, err := r.client.ContainerInspect(ctx, toolContainerName(machine, run), client.ContainerInspectOptions{})
	if errdefs.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if result.Container.Config == nil || !ownedByMachine(result.Container.Config.Labels, machine.ID) || result.Container.Config.Labels["com.kuayle.tool-run-id"] != run.String() {
		return fmt.Errorf("tool container ownership does not match")
	}
	if _, err = r.client.ContainerRemove(ctx, result.Container.ID, client.ContainerRemoveOptions{Force: true}); err != nil && !errdefs.IsNotFound(err) {
		return err
	}
	return nil
}

// toolOutputWriter batches a run's redacted output lines into the store, at
// most every toolOutputFlush or 8 KiB.
type toolOutputWriter struct {
	mu        sync.Mutex
	pending   strings.Builder
	truncated bool
	last      time.Time
	write     func(content string, truncated bool) error
}

func (w *toolOutputWriter) persist(_ string, _ int64, _ string, content string, truncated bool) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pending.WriteString(content)
	w.truncated = w.truncated || truncated
	if w.pending.Len() < 8<<10 && time.Since(w.last) < toolOutputFlush {
		return nil
	}
	return w.flushLocked()
}

func (w *toolOutputWriter) flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.flushLocked()
}

func (w *toolOutputWriter) flushLocked() error {
	w.last = time.Now()
	if w.pending.Len() == 0 && !w.truncated {
		return nil
	}
	if err := w.write(w.pending.String(), w.truncated); err != nil {
		return err
	}
	w.pending.Reset()
	w.truncated = false
	return nil
}

// toolRunFailure is the reason shown for a run that could not finish: a
// terminal reason is a limit or the tool's state and is safe to show; other
// infrastructure errors stay generic.
func toolRunFailure(err error) string {
	var terminal *terminalOperationError
	if errors.As(err, &terminal) {
		return terminal.message
	}
	return "The tool could not finish"
}

// runTool executes a custom tool run. The store re-checks the run's authority
// for every write; the container gets no credentials, and the attempt's
// redaction snapshot (for a run on a task's checkout) only hides them.
func (m *Manager) runTool(ctx context.Context, machine *domain.DevMachine, op *domain.DevMachineOperation) error {
	execution, err := m.store.GetToolRun(ctx, *op)
	switch {
	case errors.Is(err, repository.ErrToolUnavailable):
		return &terminalOperationError{code: "tool_unavailable", message: "The tool changed or was turned off before it ran"}
	case errors.Is(err, repository.ErrToolMachineUnavailable), errors.Is(err, repository.ErrToolRunForbidden):
		return &terminalOperationError{code: "tool_unauthorized", message: "The run is no longer allowed on this machine"}
	case err != nil:
		return err
	}
	run := execution.Run
	if run.Status != "queued" && run.Status != "running" {
		return nil
	}
	redactions := map[string]string{}
	if execution.EncryptedOutputSecrets != nil {
		secrets, err := m.decodeAgentRunSecrets(*execution.EncryptedOutputSecrets)
		if err != nil {
			return err
		}
		redactions = secrets.Redactions
	}
	services, err := m.store.ListServices(ctx, machine.WorkspaceID, machine.ID)
	if err != nil {
		return err
	}
	image := ""
	for _, service := range services {
		if service.ServiceType == "ide" || service.ServiceType == "terminal" {
			image = service.ImageRef
			break
		}
	}
	if image == "" {
		return &terminalOperationError{code: "tool_unavailable", message: "Developer environment is unavailable"}
	}
	// A restart cannot reset the run's timeout indefinitely.
	started := run.CreatedAt
	if run.StartedAt != nil {
		started = *run.StartedAt
	}
	deadline := started.Add(time.Duration(run.TimeoutSeconds) * time.Second)
	runCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	writer := &toolOutputWriter{last: time.Now(), write: func(content string, truncated bool) error {
		writeCtx, done := context.WithTimeout(runCtx, 5*time.Second)
		defer done()
		return m.store.AppendToolRunOutput(writeCtx, *op, content, truncated)
	}}
	output := newAgentOutputBuffer(redactions, writer.persist)
	observer := &AgentObserver{
		Authorize: func() error { _, err := m.store.GetToolRun(runCtx, *op); return err },
		Started:   func(id string) error { return m.store.StartToolRun(runCtx, *op, id) },
		Output:    output.WriteDocker,
	}
	result, execErr := m.runtime.RunTool(runCtx, machine, execution, image, observer)
	if execErr == nil {
		execErr = output.Finish()
	}
	if execErr == nil {
		execErr = writer.flush()
	}
	if execErr != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			cleanupCtx, done := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer done()
			if err := m.runtime.RemoveTool(cleanupCtx, machine, run.ID); err != nil {
				return fmt.Errorf("tool_cleanup_failed: %w", err)
			}
			return &terminalOperationError{code: "tool_timeout", message: "The tool exceeded its time limit"}
		}
		// Keep the container for replay; a retry resumes it instead of
		// running the script twice. Teardown removes it otherwise.
		return execErr
	}
	if result == nil {
		return errors.New("tool execution returned no result")
	}
	if err = m.store.CompleteToolRun(ctx, *op, result.ExitCode); err != nil {
		return err
	}
	if err = m.runtime.RemoveTool(ctx, machine, run.ID); err != nil {
		log.WithField("tool_run_id", run.ID).WithError(err).Warn("tool container cleanup failed")
	}
	return nil
}
