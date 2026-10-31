package machine

import (
	"context"
	"fmt"
	"slices"
	"strconv"

	"github.com/containerd/errdefs"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

// RunValidation runs one command of a check step: each command of an
// agent-chosen check has its own deterministic container, so a retry replays
// the commands that already ran and Docker reports every exit code.
func (r *DockerRuntime) RunValidation(ctx context.Context, machine *domain.DevMachine, run *domain.DevMachineAgentRun, step *domain.DevMachineAgentRunStep, index int, checkout *domain.DevMachineCheckout, image string, observer *AgentObserver) (*AgentExecution, error) {
	if machine.DockerNetworkName == nil || machine.WorkspaceVolumeName == nil || checkout == nil {
		return nil, fmt.Errorf("validation runtime resources are unavailable")
	}
	commands := step.CheckCommands()
	if index < 0 || index >= len(commands) || domain.ValidateTestCommand(commands[index]) != nil {
		return nil, fmt.Errorf("invalid validation command")
	}
	argv := commands[index]
	if machine.EnvironmentID != nil {
		if err := r.ensureEnvironmentImage(ctx, image, machine.WorkspaceID, *machine.EnvironmentID); err != nil {
			return nil, err
		}
	} else if err := r.ensureImage(ctx, image); err != nil {
		return nil, err
	}
	key := checkCommandKey(run, step, index)
	name := "kuayle-" + machine.RoutingKey + "-" + key
	labels := machineLabels(machine)
	labels["com.kuayle.service"] = "agent"
	labels["com.kuayle.agent-run-id"] = run.ID.String()
	labels["com.kuayle.validation-id"] = step.ID.String()
	if result, err := r.client.ContainerInspect(ctx, name, client.ContainerInspectOptions{}); err == nil {
		config := result.Container.Config
		if result.Container.State == nil || config == nil || config.Image != image || !checkContainerRuns(config, argv) || config.WorkingDir != checkout.WorkspacePath {
			return nil, fmt.Errorf("validation container does not match the accepted command")
		}
		for key, value := range labels {
			if config.Labels[key] != value {
				return nil, fmt.Errorf("validation container ownership does not match")
			}
		}
		// A check that never started still needs its services; one that ran is
		// only replayed.
		if result.Container.State.Status == "created" {
			if err := r.prepareCheckServices(ctx, machine, run, step, checkout, result.Container.ID); err != nil {
				return nil, err
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
		Config: &container.Config{Image: image, User: "1000:1000", WorkingDir: checkout.WorkspacePath, Entrypoint: checkEntrypoint, Cmd: checkCommand(argv), Labels: labels, AttachStdout: true, AttachStderr: true,
			Env: append([]string{"HOME=/home/kuayle", "CI=true", "HTTP_PROXY=http://" + machine.RoutingKey + "-egress:3128", "HTTPS_PROXY=http://" + machine.RoutingKey + "-egress:3128", "NO_PROXY=localhost,127.0.0.1"}, runTmpEnvironment(runTmpDir(key))...)},
		// A check may start compose services beside it, so it keeps the agent
		// share of a machine with a daemon (35%).
		HostConfig:       r.secureHostConfig(machine, "agent", *machine.DockerNetworkName, *machine.WorkspaceVolumeName, true),
		NetworkingConfig: &network.NetworkingConfig{EndpointsConfig: map[string]*network.EndpointSettings{*machine.DockerNetworkName: {Aliases: []string{key}}}},
	})
	if err != nil {
		return nil, err
	}
	if err := r.prepareCheckServices(ctx, machine, run, step, checkout, result.ID); err != nil {
		return nil, err
	}
	// The same stream/recovery path executes a direct argv, without the native
	// harness bootstrap, model parsing or any provider/GitHub credential handoff.
	return r.resumeExecution(ctx, container.InspectResponse{ID: result.ID, State: &container.State{Status: "created"}}, false, nil, observer, false)
}

// checkContainerRuns reports whether a retained check container runs argv,
// through the check runner or, when created by an earlier version, directly.
func checkContainerRuns(config *container.Config, argv []string) bool {
	direct := slices.Equal(config.Entrypoint, argv[:1]) && slices.Equal(config.Cmd, argv[1:])
	return direct || slices.Equal(config.Entrypoint, checkEntrypoint) && slices.Equal(config.Cmd, checkCommand(argv))
}

// checkContainerKey names a check's container, service and network alias.
// The attempt's independent validation keeps one per attempt; each manual
// check gets its own, so it never replays an earlier check's result.
func checkContainerKey(run *domain.DevMachineAgentRun, step *domain.DevMachineAgentRunStep) string {
	if step != nil && step.StepType == manualCheckStepType {
		return "check-" + step.ID.String()
	}
	return "validation-" + run.ID.String()
}

// checkCommandKey names the container of a check's command index; the first
// keeps the check's own name.
func checkCommandKey(run *domain.DevMachineAgentRun, step *domain.DevMachineAgentRunStep, index int) string {
	if index == 0 {
		return checkContainerKey(run, step)
	}
	return checkContainerKey(run, step) + "-" + strconv.Itoa(index)
}

const manualCheckStepType = "manual_check"

// RemoveCheck deletes a finished manual check's container. Its recorded
// result and output stay; only replay needed the container.
func (r *DockerRuntime) RemoveCheck(ctx context.Context, machine *domain.DevMachine, run *domain.DevMachineAgentRun, step *domain.DevMachineAgentRunStep) error {
	if step == nil || step.StepType != manualCheckStepType {
		return nil
	}
	name := "kuayle-" + machine.RoutingKey + "-" + checkContainerKey(run, step)
	result, err := r.client.ContainerInspect(ctx, name, client.ContainerInspectOptions{})
	if errdefs.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if result.Container.Config == nil || !ownedByMachine(result.Container.Config.Labels, machine.ID) || result.Container.Config.Labels["com.kuayle.validation-id"] != step.ID.String() {
		return fmt.Errorf("check container ownership does not match")
	}
	if _, err = r.client.ContainerRemove(ctx, result.Container.ID, client.ContainerRemoveOptions{Force: true}); err != nil && !errdefs.IsNotFound(err) {
		return err
	}
	return nil
}

func (r *DockerRuntime) prepareCheckServices(ctx context.Context, machine *domain.DevMachine, run *domain.DevMachineAgentRun, step *domain.DevMachineAgentRunStep, checkout *domain.DevMachineCheckout, checkID string) error {
	if step.ComposeFile == "" {
		return nil
	}
	data, err := r.readComposeFile(ctx, checkID, checkout, step.ComposeFile)
	if err != nil {
		return err
	}
	services, err := ParseCompose(data, r.config.ComposeImages)
	if err != nil {
		return err
	}
	return r.startCheckServices(ctx, machine, run, checkID, services)
}
