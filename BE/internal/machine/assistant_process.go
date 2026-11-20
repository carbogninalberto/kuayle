package machine

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/containerd/errdefs"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	log "github.com/sirupsen/logrus"
)

const processLabel = "com.kuayle.assistant-process-id"

// The deadline travels in the fixed argv so delayed startup cannot extend
// the lifetime. Docker's init is PID 1 and reaps the command's descendants
// when the timeout exits. The command is carried only in the environment.
const assistantProcessBootstrap = `set -eu
deadline=$1
now=$(date +%s)
remaining=$((deadline - now))
[ "$remaining" -gt 0 ] || exit 1
cd "$KUAYLE_COMMAND_CWD"
cmd=$KUAYLE_COMMAND
unset KUAYLE_COMMAND KUAYLE_COMMAND_CWD
exec timeout -s KILL "$remaining" sh -c "$cmd"`

func (r *DockerRuntime) StartAssistantProcess(ctx context.Context, machine *domain.DevMachine, services []domain.DevMachineService, process *domain.AssistantProcess) error {
	if machine.DockerNetworkName == nil || machine.WorkspaceVolumeName == nil || !domain.ValidAppPort(process.Port) || !time.Now().Before(process.ExpiresAt) {
		return fmt.Errorf("assistant process runtime resources or deadline are unavailable")
	}
	image := ""
	for _, service := range services {
		if isDeveloperService(service.ServiceType) {
			image = service.ImageRef
			break
		}
	}
	if image == "" {
		return fmt.Errorf("developer environment is unavailable")
	}
	labels := machineLabels(machine)
	labels["com.kuayle.service"] = "agent"
	labels[processLabel] = process.ID.String()
	labels[previewPortLabel] = strconv.Itoa(process.Port)
	host, err := r.agentHostConfig(ctx, machine, process.ID.String(), hasDockerService(services))
	if err != nil {
		return err
	}
	for i := range host.Mounts {
		if host.Mounts[i].Target == "/workspace" {
			host.Mounts[i].ReadOnly = true
		}
	}
	if err := r.scopeMachineResources(ctx, machine, "agent", labels, host); err != nil {
		return err
	}
	env := []string{"HOME=/home/kuayle", "HOST=0.0.0.0", "PORT=" + strconv.Itoa(process.Port),
		"KUAYLE_COMMAND=" + process.Command, "KUAYLE_COMMAND_CWD=" + process.WorkingDirectory}
	env = append(env, proxyVariables(egressProxyURL(machine), "localhost,127.0.0.1")...)
	created, err := r.client.ContainerCreate(ctx, client.ContainerCreateOptions{
		Name: domain.AssistantProcessHost(process.ID),
		Config: &container.Config{
			Image: image, User: "1000:1000", WorkingDir: "/workspace", Env: env, Labels: labels,
			Entrypoint: []string{"sh"}, Cmd: []string{"-c", assistantProcessBootstrap, "kuayle-process", strconv.FormatInt(process.ExpiresAt.Unix(), 10)},
		},
		HostConfig:       host,
		NetworkingConfig: &network.NetworkingConfig{EndpointsConfig: map[string]*network.EndpointSettings{*machine.DockerNetworkName: {}}},
	})
	if err != nil {
		return err
	}
	if _, err := r.client.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(time.Second):
	}
	running, err := r.AssistantProcessRunning(ctx, process)
	if err == nil && !running {
		return fmt.Errorf("assistant process exited during startup")
	}
	return err
}

func (r *DockerRuntime) assistantProcessContainer(ctx context.Context, process *domain.AssistantProcess) (*container.InspectResponse, error) {
	found, err := r.client.ContainerInspect(ctx, domain.AssistantProcessHost(process.ID), client.ContainerInspectOptions{})
	if errdefs.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if found.Container.Config == nil || process.MachineID == nil || !ownedByMachine(found.Container.Config.Labels, *process.MachineID) ||
		found.Container.Config.Labels[processLabel] != process.ID.String() ||
		found.Container.Config.Labels["com.kuayle.workspace-id"] != process.WorkspaceID.String() {
		return nil, fmt.Errorf("assistant process container ownership does not match")
	}
	return &found.Container, nil
}

func (r *DockerRuntime) AssistantProcessRunning(ctx context.Context, process *domain.AssistantProcess) (bool, error) {
	found, err := r.assistantProcessContainer(ctx, process)
	return found != nil && found.State != nil && found.State.Running, err
}

func (r *DockerRuntime) RemoveAssistantProcess(ctx context.Context, process *domain.AssistantProcess) error {
	found, err := r.assistantProcessContainer(ctx, process)
	if err != nil || found == nil {
		return err
	}
	_, err = r.client.ContainerRemove(ctx, found.ID, client.ContainerRemoveOptions{Force: true, RemoveVolumes: true})
	if errdefs.IsNotFound(err) {
		return nil
	}
	return err
}

func (m *Manager) startAssistantProcess(ctx context.Context, machine *domain.DevMachine, services []domain.DevMachineService, op *domain.DevMachineOperation) error {
	if op.Attempts > 1 {
		// A manager may have been lost after starting this command. The
		// reconciler removes any uncommitted container instead of rerunning it.
		return &terminalOperationError{code: "process_interrupted", message: "The process start was interrupted"}
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	process, err := m.store.AssistantProcessToStart(ctx, *op)
	if err != nil || process == nil {
		return err
	}
	if err := m.runtime.StartAssistantProcess(ctx, machine, services, process); err != nil {
		return err
	}
	started, err := m.store.MarkAssistantProcessRunning(ctx, *op)
	if err != nil || started {
		return err
	}
	return m.runtime.RemoveAssistantProcess(ctx, process)
}

func (m *Manager) reconcileAssistantProcesses(ctx context.Context) error {
	processes, err := m.store.AssistantProcessesToCheck(ctx)
	if err != nil {
		return err
	}
	for i := range processes {
		process := &processes[i]
		checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := m.checkAssistantProcess(checkCtx, process)
		cancel()
		if err != nil {
			log.WithField("process_id", process.ID).WithError(err).Warn("assistant process reconciliation failed")
		}
	}
	return nil
}

func (m *Manager) checkAssistantProcess(ctx context.Context, process *domain.AssistantProcess) error {
	status := "stopped"
	if process.Status == "queued" {
		status = "failed"
	}
	if process.MachineID != nil {
		if process.Status == "running" {
			running, err := m.runtime.AssistantProcessRunning(ctx, process)
			if err != nil || running {
				return err
			}
		}
		if err := m.runtime.RemoveAssistantProcess(ctx, process); err != nil {
			return err
		}
	}
	return m.store.FinishAssistantProcess(ctx, process.ID, status)
}
