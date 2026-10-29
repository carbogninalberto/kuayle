package machine

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/containerd/errdefs"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

// composeShare is the part of the machine's CPU and memory that a check's
// services share, so a stack cannot starve the developer environment.
const (
	composeShare         = 30
	composeHealthTimeout = 3 * time.Minute
)

func checkNetworkName(machine *domain.DevMachine, run *domain.DevMachineAgentRun) string {
	return "kuayle-" + machine.RoutingKey + "-check-" + run.ID.String()
}

func composeContainerName(machine *domain.DevMachine, run *domain.DevMachineAgentRun, service string) string {
	return "kuayle-" + machine.RoutingKey + "-check-" + run.ID.String() + "-" + service
}

// readComposeFile reads the file through the created (not yet started) check
// container, which mounts the task checkout. Only a regular file is accepted.
func (r *DockerRuntime) readComposeFile(ctx context.Context, containerID string, checkout *domain.DevMachineCheckout, file string) ([]byte, error) {
	if domain.ValidateComposePath(file) != nil {
		return nil, composeErr("invalid compose file path")
	}
	result, err := r.client.CopyFromContainer(ctx, containerID, client.CopyFromContainerOptions{SourcePath: checkout.WorkspacePath + "/" + file})
	if errdefs.IsNotFound(err) {
		return nil, composeErr("%s was not found in the checkout", file)
	}
	if err != nil {
		return nil, err
	}
	defer result.Content.Close()
	archive := tar.NewReader(result.Content)
	header, err := archive.Next()
	if err != nil {
		return nil, err
	}
	if header.Typeflag != tar.TypeReg {
		return nil, composeErr("%s must be a regular file", file)
	}
	if header.Size > maxComposeFileBytes {
		return nil, composeErr("file is larger than 64 KiB")
	}
	return io.ReadAll(io.LimitReader(archive, maxComposeFileBytes+1))
}

// startCheckServices creates (or reuses) the check's services on a per-check
// internal network, waits until they are running and healthy, and attaches the
// check container. Retries converge on the same deterministic containers.
func (r *DockerRuntime) startCheckServices(ctx context.Context, machine *domain.DevMachine, run *domain.DevMachineAgentRun, checkID string, services []ComposeService) error {
	for _, service := range services {
		if strings.HasPrefix(service.Name, machine.RoutingKey) {
			return composeErr("service name %q is reserved", service.Name)
		}
	}
	labels := machineLabels(machine)
	labels["com.kuayle.agent-run-id"] = run.ID.String()
	labels["com.kuayle.check-network"] = "true"
	networkName := checkNetworkName(machine, run)
	if _, err := r.ensureNetwork(ctx, networkName, true, labels); err != nil {
		return err
	}
	ids := make([]string, 0, len(services))
	for _, service := range services {
		id, err := r.ensureCheckService(ctx, machine, run, networkName, service, len(services))
		if err != nil {
			return err
		}
		ids = append(ids, id)
	}
	if err := r.waitForCheckServices(ctx, services, ids); err != nil {
		return err
	}
	if !r.containerOnNetwork(ctx, networkName, checkID) {
		if _, err := r.connectNetwork(ctx, networkName, checkID, &network.EndpointSettings{}); err != nil {
			return err
		}
	}
	return nil
}

func (r *DockerRuntime) ensureCheckService(ctx context.Context, machine *domain.DevMachine, run *domain.DevMachineAgentRun, networkName string, service ComposeService, count int) (string, error) {
	name := composeContainerName(machine, run, service.Name)
	labels := machineLabels(machine)
	labels["com.kuayle.service"] = "compose"
	labels["com.kuayle.agent-run-id"] = run.ID.String()
	labels["com.kuayle.compose-service"] = service.Name
	if result, err := r.client.ContainerInspect(ctx, name, client.ContainerInspectOptions{}); err == nil {
		for key, value := range labels {
			if result.Container.Config == nil || result.Container.Config.Labels[key] != value {
				return "", fmt.Errorf("compose container %s ownership does not match", name)
			}
		}
		if result.Container.State != nil && !result.Container.State.Running {
			if _, err := r.client.ContainerStart(ctx, result.Container.ID, client.ContainerStartOptions{}); err != nil {
				return "", err
			}
		}
		return result.Container.ID, nil
	} else if !errdefs.IsNotFound(err) {
		return "", err
	}
	if err := r.ensureImage(ctx, service.Image); err != nil {
		return "", err
	}
	config := &container.Config{Image: service.Image, Env: service.Env, User: service.User, WorkingDir: service.WorkingDir, Labels: labels}
	if len(service.Command) > 0 {
		config.Cmd = service.Command
	}
	if service.Health != nil {
		config.Healthcheck = &container.HealthConfig{Test: service.Health.Test, Interval: service.Health.Interval, Timeout: service.Health.Timeout, StartPeriod: service.Health.StartPeriod, Retries: service.Health.Retries}
	}
	result, err := r.client.ContainerCreate(ctx, client.ContainerCreateOptions{
		Name: name, Config: config, HostConfig: r.composeHostConfig(machine, networkName, count),
		NetworkingConfig: &network.NetworkingConfig{EndpointsConfig: map[string]*network.EndpointSettings{networkName: {Aliases: []string{service.Name}}}},
	})
	if err != nil {
		return "", err
	}
	if _, err := r.client.ContainerStart(ctx, result.ID, client.ContainerStartOptions{}); err != nil {
		return "", err
	}
	return result.ID, nil
}

// composeHostConfig keeps the machine's hardening: no privileges, mounts,
// devices or published ports, an internal network, and bounded resources.
// The root filesystem stays writable and a few ownership capabilities remain
// because database images initialise data directories as root; the container
// is discarded after the check.
func (r *DockerRuntime) composeHostConfig(machine *domain.DevMachine, networkName string, count int) *container.HostConfig {
	memory := int64(machine.MemoryMB) * 1024 * 1024 * composeShare / 100 / int64(count)
	if memory < 128*1024*1024 {
		memory = 128 * 1024 * 1024
	}
	pids := int64(machine.PidsLimit) * composeShare / 100 / int64(count)
	if pids < 64 {
		pids = 64
	}
	securityOptions := []string{"no-new-privileges=true"}
	if r.config.SeccompProfile != "" && r.config.SeccompProfile != "default" {
		securityOptions = append(securityOptions, "seccomp="+r.config.SeccompProfile)
	}
	if r.config.AppArmorProfile != "" && r.config.AppArmorProfile != "default" {
		securityOptions = append(securityOptions, "apparmor="+r.config.AppArmorProfile)
	}
	return &container.HostConfig{
		NetworkMode: container.NetworkMode(networkName), Privileged: false,
		CapDrop: []string{"ALL"}, CapAdd: []string{"CHOWN", "DAC_OVERRIDE", "FOWNER", "SETGID", "SETUID"},
		SecurityOpt: securityOptions, LogConfig: boundedLogConfig(),
		Tmpfs:  map[string]string{"/tmp": "rw,nosuid,size=256m"},
		Mounts: []mount.Mount{},
		Resources: container.Resources{
			NanoCPUs: int64(machine.CPUMillis) * 1_000_000 * composeShare / 100 / int64(count),
			Memory:   memory, MemorySwap: memory, PidsLimit: &pids,
		},
		ShmSize: 64 * 1024 * 1024,
	}
}

func (r *DockerRuntime) waitForCheckServices(ctx context.Context, services []ComposeService, ids []string) error {
	deadline := time.Now().Add(composeHealthTimeout)
	for {
		ready := true
		for i, id := range ids {
			result, err := r.client.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
			if err != nil {
				return err
			}
			state := result.Container.State
			if state == nil || !state.Running {
				return composeErr("service %q exited before the check started", services[i].Name)
			}
			if services[i].Health == nil {
				continue
			}
			switch health := state.Health; {
			case health != nil && health.Status == container.Unhealthy:
				return composeErr("service %q is unhealthy", services[i].Name)
			case health == nil || health.Status != container.Healthy:
				ready = false
			}
		}
		if ready {
			return nil
		}
		if time.Now().After(deadline) {
			return composeErr("services did not become healthy within %s", composeHealthTimeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// RemoveCheckServices deletes a check's services and network. The check
// container itself is kept for artifact collection and result replay.
func (r *DockerRuntime) RemoveCheckServices(ctx context.Context, machine *domain.DevMachine, run *domain.DevMachineAgentRun, step *domain.DevMachineAgentRunStep) error {
	containers, err := r.client.ContainerList(ctx, client.ContainerListOptions{All: true, Filters: make(client.Filters).Add("label", "com.kuayle.agent-run-id="+run.ID.String()).Add("label", "com.kuayle.service=compose")})
	if err != nil {
		return err
	}
	var cleanup []error
	for _, item := range containers.Items {
		if !ownedByMachine(item.Labels, machine.ID) {
			cleanup = append(cleanup, fmt.Errorf("compose container %s is not owned by machine", item.ID))
			continue
		}
		if _, err := r.client.ContainerRemove(ctx, item.ID, client.ContainerRemoveOptions{Force: true, RemoveVolumes: true}); err != nil && !errdefs.IsNotFound(err) {
			cleanup = append(cleanup, err)
		}
	}
	networkName := checkNetworkName(machine, run)
	_, _ = r.client.NetworkDisconnect(ctx, networkName, client.NetworkDisconnectOptions{Container: "kuayle-" + machine.RoutingKey + "-" + checkContainerKey(run, step), Force: true})
	cleanup = append(cleanup, r.removeManagedNetwork(ctx, networkName, machine.ID))
	return errors.Join(cleanup...)
}

// removeCheckNetworks deletes check networks left by interrupted checks.
func (r *DockerRuntime) removeCheckNetworks(ctx context.Context, filter string, machineID string) error {
	networks, err := r.client.NetworkList(ctx, client.NetworkListOptions{Filters: make(client.Filters).Add("label", filter).Add("label", "com.kuayle.check-network=true")})
	if err != nil {
		return err
	}
	var cleanup []error
	for _, item := range networks.Items {
		if item.Labels["com.kuayle.managed"] != "true" || item.Labels["com.kuayle.machine-id"] != machineID {
			cleanup = append(cleanup, fmt.Errorf("network %s ownership does not match", item.Name))
			continue
		}
		if _, err := r.client.NetworkRemove(ctx, item.ID, client.NetworkRemoveOptions{}); err != nil && !errdefs.IsNotFound(err) {
			cleanup = append(cleanup, err)
		}
	}
	return errors.Join(cleanup...)
}
