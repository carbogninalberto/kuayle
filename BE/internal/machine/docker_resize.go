package machine

import (
	"context"
	"errors"
	"fmt"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

// UpdateResources applies the machine's size to each of its containers,
// running or stopped, with the same share each one gets when it is created:
// service and agent containers through secureHostConfig (containerSharePercent), the Docker daemon
// through dockerHostConfig and check services through composeHostConfig.
// Short-lived helpers keep their fixed limits.
func (r *DockerRuntime) UpdateResources(ctx context.Context, machine *domain.DevMachine) error {
	containers, err := r.client.ContainerList(ctx, client.ContainerListOptions{
		All: true, Filters: make(client.Filters).Add("label", "com.kuayle.machine-id="+machine.ID.String()),
	})
	if err != nil {
		return err
	}
	composeCounts := map[string]int{}
	docker := false
	for _, item := range containers.Items {
		if !r.ownedForResize(item.Labels, machine) {
			continue
		}
		switch item.Labels["com.kuayle.service"] {
		case "compose":
			composeCounts[item.Labels["com.kuayle.agent-run-id"]]++
		case "docker":
			docker = true
		}
	}
	var failures []error
	for _, item := range containers.Items {
		if !r.ownedForResize(item.Labels, machine) {
			continue
		}
		var resources container.Resources
		switch service := item.Labels["com.kuayle.service"]; service {
		case "ide", "terminal", "browser", "collector", "egress", "agent":
			// Shares follow containerSharePercent; a check keeps the share of
			// a machine with a daemon, as when it is created.
			resources = r.secureHostConfig(machine, service, "", "", docker || item.Labels["com.kuayle.validation-id"] != "").Resources
		case "docker":
			resources = r.dockerHostConfig(machine, "", "", "").Resources
		case "compose":
			resources = r.composeHostConfig(machine, "", composeCounts[item.Labels["com.kuayle.agent-run-id"]]).Resources
		default:
			continue
		}
		pids := *resources.PidsLimit
		if _, err := r.client.ContainerUpdate(ctx, item.ID, client.ContainerUpdateOptions{Resources: &container.Resources{
			NanoCPUs: resources.NanoCPUs, Memory: resources.Memory, MemorySwap: resources.MemorySwap, PidsLimit: &pids,
		}}); err != nil {
			failures = append(failures, fmt.Errorf("update %s container: %w", item.Labels["com.kuayle.service"], err))
		}
	}
	return errors.Join(failures...)
}

func (r *DockerRuntime) ownedForResize(labels map[string]string, machine *domain.DevMachine) bool {
	return ownedByMachine(labels, machine.ID) && labels["com.kuayle.workspace-id"] == machine.WorkspaceID.String()
}
