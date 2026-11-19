package machine

import (
	"context"
	"fmt"
	"strings"

	"github.com/containerd/errdefs"
	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
)

// PurgeTaskSession removes one task's harness transcripts with a short-lived
// helper on the workspace volume: no network, read-only root, no capabilities,
// and an argv-only removal of the single validated task directory.
func (r *DockerRuntime) PurgeTaskSession(ctx context.Context, machine *domain.DevMachine, imageRef string, taskID uuid.UUID) error {
	_, err := r.runTaskWorkspaceHelper(ctx, machine, imageRef, taskID, "session-purge",
		[]string{"rm", "-rf", "--", "/workspace/.kuayle/agent-sessions/" + taskID.String()})
	return err
}

func (r *DockerRuntime) runTaskWorkspaceHelper(ctx context.Context, machine *domain.DevMachine, imageRef string, taskID uuid.UUID, name string, command []string) (string, error) {
	if machine.WorkspaceVolumeName == nil {
		return "", nil
	}
	if _, err := r.client.VolumeInspect(ctx, *machine.WorkspaceVolumeName, client.VolumeInspectOptions{}); errdefs.IsNotFound(err) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	if err := r.ensureImage(ctx, imageRef); err != nil {
		return "", err
	}
	containerName := "kuayle-" + machine.RoutingKey + "-" + name + "-" + taskID.String()
	if inspection, err := r.client.ContainerInspect(ctx, containerName, client.ContainerInspectOptions{}); err == nil {
		if inspection.Container.Config.Labels["com.kuayle.machine-id"] != machine.ID.String() {
			return "", fmt.Errorf("existing task workspace helper is not managed by Kuayle")
		}
		if _, err := r.client.ContainerRemove(ctx, inspection.Container.ID, client.ContainerRemoveOptions{Force: true}); err != nil {
			return "", err
		}
	} else if !errdefs.IsNotFound(err) {
		return "", err
	}
	labels := machineLabels(machine)
	labels["com.kuayle.service"] = name
	pidsLimit := int64(16)
	response, err := r.client.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{
			Image: imageRef, User: "1000:1000", Labels: labels,
			Entrypoint: command[:1], Cmd: command[1:],
		},
		HostConfig: &container.HostConfig{
			NetworkMode: "none", ReadonlyRootfs: true, CapDrop: []string{"ALL"},
			SecurityOpt: []string{"no-new-privileges=true"},
			LogConfig:   boundedLogConfig(),
			Mounts: []mount.Mount{{
				Type: mount.TypeVolume, Source: *machine.WorkspaceVolumeName, Target: "/workspace",
				VolumeOptions: &mount.VolumeOptions{NoCopy: true},
			}},
			Resources: container.Resources{Memory: 64 * 1024 * 1024, MemorySwap: 64 * 1024 * 1024, NanoCPUs: 100_000_000, PidsLimit: &pidsLimit},
		},
		Name: containerName,
	})
	if err != nil {
		return "", err
	}
	defer func() {
		_, _ = r.client.ContainerRemove(context.Background(), response.ID, client.ContainerRemoveOptions{Force: true})
	}()
	if _, err := r.client.ContainerStart(ctx, response.ID, client.ContainerStartOptions{}); err != nil {
		return "", err
	}
	waitResult := r.client.ContainerWait(ctx, response.ID, client.ContainerWaitOptions{Condition: container.WaitConditionNotRunning})
	select {
	case result := <-waitResult.Result:
		stdout, stderr, _ := r.containerLogs(ctx, response.ID)
		if result.StatusCode != 0 {
			return stdout, fmt.Errorf("%s exited %d: %s", name, result.StatusCode, strings.TrimSpace(stderr))
		}
		return stdout, nil
	case err := <-waitResult.Error:
		return "", err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}
