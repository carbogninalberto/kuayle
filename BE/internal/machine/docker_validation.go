package machine

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/containerd/errdefs"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

const manualCheckStepType = "manual_check"

// RunManualCheck runs the manual check in a deterministic container so a retry can recover its exit code.
func (r *DockerRuntime) RunManualCheck(ctx context.Context, machine *domain.DevMachine, run *domain.DevMachineAgentRun, step *domain.DevMachineAgentRunStep, checkout *domain.DevMachineCheckout, image string, observer *AgentObserver) (*AgentExecution, error) {
	if machine.DockerNetworkName == nil || machine.WorkspaceVolumeName == nil || checkout == nil {
		return nil, fmt.Errorf("manual check runtime resources are unavailable")
	}
	var argv []string
	if json.Unmarshal(step.CommandArgv, &argv) != nil || len(argv) == 0 || domain.ValidateTestCommand(argv) != nil {
		return nil, fmt.Errorf("invalid check command")
	}
	if err := r.ensureImage(ctx, image); err != nil {
		return nil, err
	}
	key := checkContainerKey(run, step)
	name := "kuayle-" + machine.RoutingKey + "-" + key
	labels := machineLabels(machine)
	labels["com.kuayle.service"] = "agent"
	labels["com.kuayle.agent-run-id"] = run.ID.String()
	labels["com.kuayle.validation-id"] = step.ID.String()
	if result, err := r.client.ContainerInspect(ctx, name, client.ContainerInspectOptions{}); err == nil {
		config := result.Container.Config
		if result.Container.State == nil || config == nil || config.Image != image || !checkContainerRuns(config, argv) || config.WorkingDir != checkout.WorkspacePath {
			return nil, fmt.Errorf("manual check container does not match the accepted command")
		}
		for key, value := range labels {
			if config.Labels[key] != value {
				return nil, fmt.Errorf("manual check container ownership does not match")
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
	// A check may start compose services beside it, so it keeps the agent
	// share of a machine with a daemon (35%), split with the other tasks on
	// the machine, unless the machine shares its memory.
	hostConfig, err := r.agentHostConfig(ctx, machine, run.ID.String(), true)
	if err != nil {
		return nil, err
	}
	if err := r.scopeMachineResources(ctx, machine, "agent", labels, hostConfig); err != nil {
		return nil, err
	}
	result, err := r.client.ContainerCreate(ctx, client.ContainerCreateOptions{
		Name: name,
		Config: &container.Config{Image: image, User: "1000:1000", WorkingDir: checkout.WorkspacePath, Entrypoint: checkEntrypoint, Cmd: checkCommand(argv), Labels: labels, AttachStdout: true, AttachStderr: true,
			Env: append(append([]string{"HOME=/home/kuayle", "CI=true"}, proxyVariables(egressProxyURL(machine), "localhost,127.0.0.1")...), runTmpEnvironment(runTmpDir(key))...)},
		HostConfig:       hostConfig,
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
// Retain the original per-run name when cleaning up an older check.
func checkContainerKey(run *domain.DevMachineAgentRun, step *domain.DevMachineAgentRunStep) string {
	if step != nil && step.StepType == manualCheckStepType {
		return "check-" + step.ID.String()
	}
	return "validation-" + run.ID.String()
}

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
