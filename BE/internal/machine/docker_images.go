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

// RemoveFinishedRunContainers removes the machine's stopped agent run and
// check containers. Their output is already recorded; left in place they keep
// old images in use, so the release updater cannot remove them.
func (r *DockerRuntime) RemoveFinishedRunContainers(ctx context.Context, machine *domain.DevMachine) error {
	return r.removeMachineContainers(ctx, machine, func(item container.Summary) bool {
		return item.Labels["com.kuayle.service"] == "agent" && item.Labels["com.kuayle.agent-run-id"] != "" &&
			item.State != container.StateRunning && item.State != container.StatePaused
	})
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
