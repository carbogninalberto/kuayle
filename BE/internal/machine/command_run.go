package machine

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/containerd/errdefs"
	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	cryptoutil "github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/kuayle/kuayle-backend/pkg/sanitize"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	log "github.com/sirupsen/logrus"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

// An assistant command runs in a fresh check container, like a custom tool:
// the developer image, the machine's workspace volume, no credentials and
// egress only through the proxy. The command reaches the container only as
// the KUAYLE_COMMAND variable, never in its argv. /workspace is always
// mounted read-only, so a read-only run can write only /tmp and $HOME
// (tmpfs); a write run also mounts its conversation's folder read-write (a
// volume subpath, Docker API 1.45+) and starts there, so nothing else in the
// workspace can change.

// commandBootstrap is the fixed command line of every command container. It
// enters the working directory, then runs the command with sh under a hard
// kill timeout ($1 seconds). timeout is not exec'd: busybox timeout execs the
// command in its own process and kills it from a child, and a command that is
// the container's PID 1 ignores that SIGKILL. The shell stays PID 1 instead.
const commandBootstrap = `set -u
cd "$KUAYLE_COMMAND_CWD" 2>/dev/null || { echo "kuayle: the working directory $KUAYLE_COMMAND_CWD does not exist" >&2; exit 2; }
kuayle_command=$KUAYLE_COMMAND
unset KUAYLE_COMMAND KUAYLE_COMMAND_CWD KUAYLE_COMMAND_WRITE
timeout -s KILL "$1" sh -c "$kuayle_command"
exit $?`

// commandArgv is the container's argv after the sh entrypoint.
func commandArgv(timeoutSeconds int) []string {
	return []string{"-c", commandBootstrap, "kuayle-cmd", strconv.Itoa(timeoutSeconds)}
}

// commandFolderPrep is the fixed script of a write run's prep container: it
// creates the conversation's folder ($1) and refuses one that is, or sits in,
// a symbolic link, so the writable mount cannot reach elsewhere.
const commandFolderPrep = `set -eu
mkdir -p -- "$1"
parent=$(dirname -- "$1")
if [ -L "$1" ] || [ -L "$parent" ] || [ ! -d "$1" ]; then echo "kuayle: $1 is not a plain folder" >&2; exit 3; fi`

// commandPrepArgv is the prep container's argv after the sh entrypoint; it
// never holds the command.
func commandPrepArgv(folder string) []string {
	return []string{"-c", commandFolderPrep, "kuayle-cmd-prep", folder}
}

// errCommandWritesUnconfined refuses a write run on a daemon that cannot
// mount a volume's subpath: writes are never widened to all of /workspace.
var errCommandWritesUnconfined = &terminalOperationError{code: "command_write_unconfined", message: "This machine's Docker is too old to confine writes to the conversation folder"}

// commandEnvironment is a command container's whole environment. A migration
// run also carries its action's secret, decrypted moments before and never
// stored anywhere else.
func commandEnvironment(machine *domain.DevMachine, run *domain.CommandRun, migrationSecret string) []string {
	write := "0"
	if run.Write {
		write = "1"
	}
	env := []string{"HOME=/home/kuayle", "CI=true", "KUAYLE_COMMAND=" + run.Command, "KUAYLE_COMMAND_CWD=" + run.WorkingDirectory, "KUAYLE_COMMAND_WRITE=" + write}
	if run.Migration {
		env = append(env, domain.MigrationSecretEnv+"="+migrationSecret)
	}
	return append(env, proxyVariables(egressProxyURL(machine), "localhost,127.0.0.1")...)
}

func commandContainerName(machine *domain.DevMachine, run uuid.UUID) string {
	return "kuayle-" + machine.RoutingKey + "-cmd-" + run.String()
}

// commandPrepName names a write run's prep container; it never matches the
// command container's name.
func commandPrepName(machine *domain.DevMachine, run uuid.UUID) string {
	return commandContainerName(machine, run) + "-prep"
}

// commandWriteFolder is a write run's folder relative to the workspace volume:
// one conversation folder directly under domain.CommandFolderRoot.
func commandWriteFolder(directory string) (string, bool) {
	name, ok := strings.CutPrefix(directory, domain.CommandFolderRoot+"/")
	if clean, valid := domain.CommandWorkingDirectory(directory); !ok || !valid || clean != directory || name == "" || strings.ContainsAny(name, "/\x00") || strings.HasPrefix(name, ".") {
		return "", false
	}
	return strings.TrimPrefix(directory, "/workspace/"), true
}

// commandHostConfig is a check container's host config with /workspace
// read-only and, for a write run, its folder (a subpath of the workspace
// volume) read-write.
func commandHostConfig(hostConfig *container.HostConfig, volume string, run *domain.CommandRun) (*container.HostConfig, error) {
	for i := range hostConfig.Mounts {
		if hostConfig.Mounts[i].Target == "/workspace" {
			hostConfig.Mounts[i].ReadOnly = true
		}
	}
	if !run.Write {
		return hostConfig, nil
	}
	subpath, ok := commandWriteFolder(run.WorkingDirectory)
	if !ok {
		return nil, fmt.Errorf("invalid command run")
	}
	hostConfig.Mounts = append(hostConfig.Mounts, mount.Mount{Type: mount.TypeVolume, Source: volume, Target: run.WorkingDirectory, ReadOnly: false,
		VolumeOptions: &mount.VolumeOptions{NoCopy: true, Subpath: subpath}})
	return hostConfig, nil
}

// commandOwned reports whether a container carries the run's labels.
func commandOwned(config *container.Config, labels map[string]string) bool {
	if config == nil {
		return false
	}
	for key, value := range labels {
		if config.Labels[key] != value {
			return false
		}
	}
	return true
}

// prepareCommandFolder creates a write run's folder with a short container
// that sees /workspace read-write, has no network and never sees the
// command; a subpath must exist before Docker mounts it. A prep container
// left by an interrupted attempt is replaced.
func (r *DockerRuntime) prepareCommandFolder(ctx context.Context, machine *domain.DevMachine, run *domain.CommandRun, image string, labels map[string]string) error {
	name := commandPrepName(machine, run.ID)
	prepLabels := maps.Clone(labels)
	prepLabels["com.kuayle.command-prep"] = "true"
	if existing, err := r.client.ContainerInspect(ctx, name, client.ContainerInspectOptions{}); err == nil {
		if !commandOwned(existing.Container.Config, prepLabels) {
			return fmt.Errorf("command prep container ownership does not match")
		}
		if _, err = r.client.ContainerRemove(ctx, existing.Container.ID, client.ContainerRemoveOptions{Force: true}); err != nil && !errdefs.IsNotFound(err) {
			return err
		}
	} else if !errdefs.IsNotFound(err) {
		return err
	}
	hostConfig := r.secureHostConfig(machine, "agent", *machine.DockerNetworkName, *machine.WorkspaceVolumeName, true)
	hostConfig.NetworkMode = "none"
	for i := range hostConfig.Mounts {
		if hostConfig.Mounts[i].Target == "/workspace" {
			hostConfig.Mounts[i].ReadOnly = false
		}
	}
	if err := r.scopeMachineResources(ctx, machine, "agent", prepLabels, hostConfig); err != nil {
		return err
	}
	created, err := r.client.ContainerCreate(ctx, client.ContainerCreateOptions{
		Name: name,
		Config: &container.Config{Image: image, User: "1000:1000", WorkingDir: "/workspace", Entrypoint: []string{"sh"}, Cmd: commandPrepArgv(run.WorkingDirectory),
			Labels: prepLabels, NetworkDisabled: true, Env: []string{"HOME=/home/kuayle"}},
		HostConfig: hostConfig,
	})
	if err != nil {
		return err
	}
	defer func() {
		_, _ = r.client.ContainerRemove(context.WithoutCancel(ctx), created.ID, client.ContainerRemoveOptions{Force: true})
	}()
	if _, err = r.client.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); err != nil {
		return err
	}
	wait := r.client.ContainerWait(ctx, created.ID, client.ContainerWaitOptions{Condition: container.WaitConditionNotRunning})
	select {
	case result := <-wait.Result:
		if result.StatusCode != 0 {
			return &terminalOperationError{code: "command_folder_unavailable", message: "The conversation folder could not be prepared on this machine"}
		}
		return nil
	case err = <-wait.Error:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// redactMigrationOutput redacts a stored chunk of a migration run: the
// literal secret (with its escaped forms) first, then any DSN or password a
// migration tool might echo. Applied to every chunk before it is stored.
func redactMigrationOutput(secret, content string) string {
	if secret != "" {
		content = sanitize.SecretReplacer([]string{secret}).Replace(content)
	}
	return sanitize.RedactDSNs(content)
}

// RunCommand runs an approved command in its own check container, or resumes
// the container this lease already started. A migration run's secret reaches
// only the container's environment.
func (r *DockerRuntime) RunCommand(ctx context.Context, machine *domain.DevMachine, run *domain.CommandRun, image, migrationSecret string, observer *AgentObserver) (*AgentExecution, error) {
	if machine.DockerNetworkName == nil || machine.WorkspaceVolumeName == nil || run == nil {
		return nil, fmt.Errorf("command runtime resources are unavailable")
	}
	if clean, ok := domain.CommandWorkingDirectory(run.WorkingDirectory); !domain.ValidCommandTimeout(run.TimeoutSeconds) || !ok || clean != run.WorkingDirectory || strings.ContainsRune(run.Command, 0) {
		return nil, fmt.Errorf("invalid command run")
	}
	if _, ok := commandWriteFolder(run.WorkingDirectory); run.Write && !ok {
		return nil, fmt.Errorf("invalid command run")
	}
	if machine.EnvironmentID != nil {
		if err := r.ensureEnvironmentImage(ctx, image, machine.WorkspaceID, *machine.EnvironmentID); err != nil {
			return nil, err
		}
	} else if err := r.ensureImage(ctx, image); err != nil {
		return nil, err
	}
	cmd := commandArgv(run.TimeoutSeconds)
	name := commandContainerName(machine, run.ID)
	labels := machineLabels(machine)
	labels["com.kuayle.service"] = "agent"
	labels["com.kuayle.command-run-id"] = run.ID.String()
	if result, err := r.client.ContainerInspect(ctx, name, client.ContainerInspectOptions{}); err == nil {
		config := result.Container.Config
		if result.Container.State == nil || config == nil || config.Image != image || !slices.Equal(config.Entrypoint, []string{"sh"}) || !slices.Equal(config.Cmd, cmd) {
			return nil, fmt.Errorf("command container does not match the accepted run")
		}
		if !commandOwned(config, labels) || config.Labels["com.kuayle.command-prep"] != "" {
			return nil, fmt.Errorf("command container ownership does not match")
		}
		return r.resumeExecution(ctx, result.Container, false, nil, observer, false)
	} else if !errdefs.IsNotFound(err) {
		return nil, err
	}
	if run.Write {
		// Fail closed: without subpath mounts the folder cannot be the only
		// writable place.
		ping, err := r.client.Ping(ctx, client.PingOptions{})
		if err != nil {
			return nil, err
		}
		if !apiAtLeast(ping.APIVersion, volumeSubpathAPI) {
			return nil, errCommandWritesUnconfined
		}
	}
	if observer != nil && observer.Authorize != nil {
		if err := observer.Authorize(); err != nil {
			return nil, err
		}
	}
	if run.Write {
		if err := r.prepareCommandFolder(ctx, machine, run, image, labels); err != nil {
			return nil, err
		}
	}
	hostConfig, err := commandHostConfig(r.secureHostConfig(machine, "agent", *machine.DockerNetworkName, *machine.WorkspaceVolumeName, true), *machine.WorkspaceVolumeName, run)
	if err != nil {
		return nil, err
	}
	if err := r.scopeMachineResources(ctx, machine, "agent", labels, hostConfig); err != nil {
		return nil, err
	}
	result, err := r.client.ContainerCreate(ctx, client.ContainerCreateOptions{
		Name: name,
		Config: &container.Config{Image: image, User: "1000:1000", WorkingDir: "/workspace", Entrypoint: []string{"sh"}, Cmd: cmd, Labels: labels,
			AttachStdout: true, AttachStderr: true, Env: commandEnvironment(machine, run, migrationSecret)},
		HostConfig:       hostConfig,
		NetworkingConfig: &network.NetworkingConfig{EndpointsConfig: map[string]*network.EndpointSettings{*machine.DockerNetworkName: {}}},
	})
	if err != nil {
		return nil, err
	}
	return r.resumeExecution(ctx, container.InspectResponse{ID: result.ID, State: &container.State{Status: "created"}}, false, nil, observer, false)
}

// RemoveCommand deletes a run's container and a write run's prep container.
// The recorded result stays.
func (r *DockerRuntime) RemoveCommand(ctx context.Context, machine *domain.DevMachine, run uuid.UUID) error {
	for _, name := range []string{commandContainerName(machine, run), commandPrepName(machine, run)} {
		result, err := r.client.ContainerInspect(ctx, name, client.ContainerInspectOptions{})
		if errdefs.IsNotFound(err) {
			continue
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
	// A migration run's secret is decrypted here only: it reaches the
	// container's environment and the redactions, never the run, the stored
	// output or a log.
	secret := ""
	if run.Migration {
		encrypted, err := m.store.MigrationActionSecret(ctx, op.WorkspaceID, machine.ID, run.ActionName)
		switch {
		case errors.Is(err, repository.ErrMigrationActionNotFound):
			return &terminalOperationError{code: "migration_action_missing", message: "The migration action is no longer configured on this machine"}
		case err != nil:
			return err
		}
		if secret, err = cryptoutil.Decrypt(encrypted, m.encryptionKey); err != nil {
			return fmt.Errorf("migration_secret_decryption_failed: %w", err)
		}
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
	// Allow the container to report its exit and final output after its hard kill.
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(run.TimeoutSeconds)*time.Second+15*time.Second)
	defer cancel()
	writer := &toolOutputWriter{write: func(content string, truncated bool) error {
		// Belt-and-braces: the buffer already redacts each line, and every
		// stored chunk of a migration run passes the same redaction again.
		if run.Migration {
			content = redactMigrationOutput(secret, content)
		}
		writeCtx, done := context.WithTimeout(ctx, 5*time.Second)
		defer done()
		return m.store.AppendCommandRunOutput(writeCtx, *op, content, truncated)
	}}
	observer := &AgentObserver{
		Authorize: func() error { _, err := m.store.GetCommandRun(runCtx, *op); return err },
		Started:   func(string) error { return m.store.StartCommandRun(runCtx, *op) },
	}
	var finishOutput func() error
	if run.Migration {
		output := newAgentOutputBuffer(map[string]string{domain.MigrationSecretEnv: secret}, writer.persist)
		observer.Output, finishOutput = output.WriteDocker, output.Finish
	} else {
		// The store caps the tail. Keep feeding it even after a verbose suite
		// exceeds an agent transcript's record limit. Decode split UTF-8 per stream.
		stdout := transform.NewWriter(writer, unicode.UTF8.NewDecoder())
		stderr := transform.NewWriter(writer, unicode.UTF8.NewDecoder())
		observer.PlainOutput = true
		observer.Output = func(stream string, data []byte) error {
			out := stdout
			if stream == "stderr" {
				out = stderr
			}
			_, err := out.Write(data)
			return err
		}
		finishOutput = func() error { return errors.Join(stdout.Close(), stderr.Close()) }
	}
	flushDone := make(chan error, 1)
	go func() {
		err := writer.flushWhileRunning(runCtx)
		if err != nil {
			cancel()
		}
		flushDone <- err
	}()
	result, execErr := m.runtime.RunCommand(runCtx, machine, run, image, secret, observer)
	cancel()
	execErr = errors.Join(execErr, <-flushDone)
	// Persist the last batch even when the manager deadline stopped the runtime.
	// A migration's partial last line may contain an incomplete secret.
	if execErr == nil || !run.Migration {
		execErr = errors.Join(execErr, finishOutput())
	}
	execErr = errors.Join(execErr, writer.flush())
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
		if ctx.Err() == nil && errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return &terminalOperationError{code: "command_timeout", message: fmt.Sprintf("The command took too long and was stopped after %d seconds", run.TimeoutSeconds)}
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
