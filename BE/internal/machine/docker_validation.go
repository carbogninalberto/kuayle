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

func (r *DockerRuntime) RunValidation(ctx context.Context, machine *domain.DevMachine, run *domain.DevMachineAgentRun, step *domain.DevMachineAgentRunStep, checkout *domain.DevMachineCheckout, image string, observer *AgentObserver) (*AgentExecution, error) {
	if machine.DockerNetworkName == nil || machine.WorkspaceVolumeName == nil || checkout == nil {
		return nil, fmt.Errorf("validation runtime resources are unavailable")
	}
	var argv []string
	if json.Unmarshal(step.CommandArgv, &argv) != nil || len(argv) == 0 || domain.ValidateTestCommand(argv) != nil {
		return nil, fmt.Errorf("invalid validation command")
	}
	if machine.EnvironmentID != nil {
		if err := r.ensureEnvironmentImage(ctx, image, machine.WorkspaceID, *machine.EnvironmentID); err != nil {
			return nil, err
		}
	} else if err := r.ensureImage(ctx, image); err != nil {
		return nil, err
	}
	name := "kuayle-" + machine.RoutingKey + "-validation-" + run.ID.String()
	labels := machineLabels(machine)
	labels["com.kuayle.service"] = "agent"
	labels["com.kuayle.agent-run-id"] = run.ID.String()
	labels["com.kuayle.validation-id"] = step.ID.String()
	if result, err := r.client.ContainerInspect(ctx, name, client.ContainerInspectOptions{}); err == nil {
		config := result.Container.Config
		if result.Container.State == nil || config == nil || config.Image != image || !slices.Equal(config.Entrypoint, argv[:1]) || !slices.Equal(config.Cmd, argv[1:]) || config.WorkingDir != checkout.WorkspacePath {
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
		Config: &container.Config{Image: image, User: "1000:1000", WorkingDir: checkout.WorkspacePath, Entrypoint: argv[:1], Cmd: argv[1:], Labels: labels, AttachStdout: true, AttachStderr: true,
			Env: []string{"HOME=/home/kuayle", "CI=true", "HTTP_PROXY=http://" + machine.RoutingKey + "-egress:3128", "HTTPS_PROXY=http://" + machine.RoutingKey + "-egress:3128", "NO_PROXY=localhost,127.0.0.1"}},
		// A check may start compose services beside it, so it keeps the agent
		// share of a machine with a daemon (35%).
		HostConfig:       r.secureHostConfig(machine, "agent", *machine.DockerNetworkName, *machine.WorkspaceVolumeName, true),
		NetworkingConfig: &network.NetworkingConfig{EndpointsConfig: map[string]*network.EndpointSettings{*machine.DockerNetworkName: {Aliases: []string{"validation-" + run.ID.String()}}}},
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
