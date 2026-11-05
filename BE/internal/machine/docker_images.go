package machine

import (
	"context"
	"errors"
	"fmt"

	"github.com/containerd/errdefs"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

// RemoveContainers force-removes the machine's containers with these IDs, so
// they are created again from their service's image. A container that is
// already gone, or that is not this machine's, is left alone.
func (r *DockerRuntime) RemoveContainers(ctx context.Context, machine *domain.DevMachine, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	wanted := make(map[string]bool, len(ids))
	for _, id := range ids {
		wanted[id] = true
	}
	return r.removeMachineContainers(ctx, machine, func(item container.Summary) bool { return wanted[item.ID] })
}

// finishedRunContainer is a run's agent, check or live inbox writer container
// that is not running. Inbox writers are created and never started.
func finishedRunContainer(item container.Summary) bool {
	service := item.Labels["com.kuayle.service"]
	return (service == "agent" || service == liveInboxService) && item.Labels["com.kuayle.agent-run-id"] != "" &&
		item.State != container.StateRunning && item.State != container.StatePaused
}

// FinishedRunContainers counts the machine's finished run containers.
func (r *DockerRuntime) FinishedRunContainers(ctx context.Context, machine *domain.DevMachine) (int, error) {
	containers, err := r.client.ContainerList(ctx, client.ContainerListOptions{
		All: true, Filters: make(client.Filters).Add("label", "com.kuayle.machine-id="+machine.ID.String()),
	})
	if err != nil {
		return 0, err
	}
	count := 0
	for _, item := range containers.Items {
		if ownedByMachine(item.Labels, machine.ID) && item.Labels["com.kuayle.workspace-id"] == machine.WorkspaceID.String() && finishedRunContainer(item) {
			count++
		}
	}
	return count, nil
}

// RemoveFinishedRunContainers removes the machine's finished agent, check and
// live inbox writer containers, then the inbox volumes no container uses.
// Their output is already recorded; left in place they keep old images in use,
// so the release updater cannot remove them.
func (r *DockerRuntime) RemoveFinishedRunContainers(ctx context.Context, machine *domain.DevMachine) error {
	if err := r.removeMachineContainers(ctx, machine, finishedRunContainer); err != nil {
		return err
	}
	volumes, err := r.client.VolumeList(ctx, client.VolumeListOptions{Filters: make(client.Filters).
		Add("label", "com.kuayle.machine-id="+machine.ID.String()).Add("label", "com.kuayle.service="+liveInboxService)})
	if err != nil {
		return err
	}
	var failures []error
	for _, item := range volumes.Items {
		if !ownedByMachine(item.Labels, machine.ID) || item.Labels["com.kuayle.service"] != liveInboxService {
			continue
		}
		// An inbox still mounted by a container is in use and stays.
		if _, err := r.client.VolumeRemove(ctx, item.Name, client.VolumeRemoveOptions{}); err != nil && !errdefs.IsNotFound(err) && !errdefs.IsConflict(err) {
			failures = append(failures, fmt.Errorf("remove live inbox volume: %w", err))
		}
	}
	return errors.Join(failures...)
}

func (r *DockerRuntime) removeMachineContainers(ctx context.Context, machine *domain.DevMachine, remove func(container.Summary) bool) error {
	containers, err := r.client.ContainerList(ctx, client.ContainerListOptions{
		All: true, Filters: make(client.Filters).Add("label", "com.kuayle.machine-id="+machine.ID.String()),
	})
	if err != nil {
		return err
	}
	var failures []error
	for _, item := range containers.Items {
		if !ownedByMachine(item.Labels, machine.ID) || item.Labels["com.kuayle.workspace-id"] != machine.WorkspaceID.String() || !remove(item) {
			continue
		}
		if _, err := r.client.ContainerRemove(ctx, item.ID, client.ContainerRemoveOptions{Force: true, RemoveVolumes: true}); err != nil && !errdefs.IsNotFound(err) {
			failures = append(failures, fmt.Errorf("remove %s container: %w", item.Labels["com.kuayle.service"], err))
		}
	}
	return errors.Join(failures...)
}
