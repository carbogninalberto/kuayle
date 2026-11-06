package machine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/containerd/errdefs"
	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/systemdslice"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
	log "github.com/sirupsen/logrus"
)

// Shared machine memory
//
// On hosts whose Docker daemon uses the systemd cgroup driver with cgroup v2,
// every container of a machine runs in one systemd slice,
// kuayle-<routing key>.slice, whose MemoryMax is the machine's size and whose
// MemorySwapMax is DockerConfig.MachineSwapPercent of it. The terminal, agent,
// Docker daemon and the other workload containers may each use all of it, so
// a 16 GiB machine gives a build 16 GiB instead of one container's share.
// When the machine as a whole runs out, the kernel stops the largest process
// in the slice. The collector and egress proxy keep their own small limits
// inside the slice, so a runaway workload cannot starve them.
//
// The machine's network records the choice when it is created
// (memoryScopeLabel), so a machine never mixes both models: machines created
// before, or on hosts without systemd, keep each container's share
// (containerSharePercent).
const (
	memoryScopeLabel   = "com.kuayle.memory-scope"
	memoryScopeMachine = "machine"
	// memorySliceService labels the short-lived helper containers.
	memorySliceService = "memory-slice"
	// memorySliceEntrypoint is the manager binary in its own image.
	memorySliceEntrypoint = "/usr/local/bin/machine-manager"
	memorySliceTimeout    = time.Minute
)

var memorySliceRoutingKey = regexp.MustCompile(`^[a-z0-9]{1,64}$`)

// memorySlices sets and removes a machine's slice limits.
type memorySlices interface {
	Apply(ctx context.Context, machine *domain.DevMachine) error
	Remove(ctx context.Context, machine *domain.DevMachine) error
}

func memorySliceName(machine *domain.DevMachine) (string, bool) {
	if !memorySliceRoutingKey.MatchString(machine.RoutingKey) {
		return "", false
	}
	return "kuayle-" + machine.RoutingKey + ".slice", true
}

func machineNetworkName(machine *domain.DevMachine) string {
	if machine.DockerNetworkName != nil && *machine.DockerNetworkName != "" {
		return *machine.DockerNetworkName
	}
	return "kuayle-machine-" + machine.RoutingKey
}

func machineMemoryBytes(machine *domain.DevMachine) int64 {
	return int64(machine.MemoryMB) * 1024 * 1024
}

// machineMemoryShared reports whether the machine's containers share its
// memory, as recorded on its network when it was created.
func (r *DockerRuntime) machineMemoryShared(ctx context.Context, machine *domain.DevMachine) (bool, error) {
	r.memoryScopeMu.Lock()
	shared, known := r.memoryScopes[machine.ID]
	r.memoryScopeMu.Unlock()
	if known {
		return shared, nil
	}
	inspection, err := r.client.NetworkInspect(ctx, machineNetworkName(machine), client.NetworkInspectOptions{})
	if err != nil {
		return false, err
	}
	labels := inspection.Network.Labels
	shared = ownedByMachine(labels, machine.ID) && labels[memoryScopeLabel] == memoryScopeMachine
	r.memoryScopeMu.Lock()
	if r.memoryScopes == nil {
		r.memoryScopes = map[uuid.UUID]bool{}
	}
	r.memoryScopes[machine.ID] = shared
	r.memoryScopeMu.Unlock()
	return shared, nil
}

func (r *DockerRuntime) forgetMemoryScope(machine *domain.DevMachine) {
	r.memoryScopeMu.Lock()
	delete(r.memoryScopes, machine.ID)
	r.memoryScopeMu.Unlock()
}

// prepareMachineMemory decides, before a machine network is created, whether
// the new machine shares its memory: its slice limits are applied first, so
// no container ever runs in an unlimited slice. It returns the labels for the
// network; an existing network keeps its choice, and its slice limits are
// applied again in case the host lost them.
func (r *DockerRuntime) prepareMachineMemory(ctx context.Context, machine *domain.DevMachine, networkName string, labels map[string]string) (map[string]string, error) {
	if r.slices == nil {
		return labels, nil
	}
	if _, err := r.client.NetworkInspect(ctx, networkName, client.NetworkInspectOptions{}); err == nil {
		shared, err := r.machineMemoryShared(ctx, machine)
		if err != nil || !shared {
			return labels, err
		}
		return labels, r.slices.Apply(ctx, machine)
	} else if !errdefs.IsNotFound(err) {
		return nil, err
	}
	if err := r.slices.Apply(ctx, machine); err != nil {
		log.WithField("machine_id", machine.ID).WithError(err).Warn("machine memory slice could not be prepared; its containers keep separate memory limits")
		return labels, nil
	}
	networkLabels := make(map[string]string, len(labels)+1)
	for key, value := range labels {
		networkLabels[key] = value
	}
	networkLabels[memoryScopeLabel] = memoryScopeMachine
	r.forgetMemoryScope(machine)
	return networkLabels, nil
}

// scopeMachineMemory places a new container of a machine that shares its
// memory in the machine's slice. Workload containers may then use the whole
// machine (swap is bounded by the slice) and are labelled so a memory kill
// names the machine's limit; the collector and egress proxy keep theirs.
// Without slices (a manager started with them off or unavailable) a new
// container keeps its bounded share.
func (r *DockerRuntime) scopeMachineMemory(ctx context.Context, machine *domain.DevMachine, serviceType string, labels map[string]string, hostConfig *container.HostConfig) error {
	if r.slices == nil {
		return nil
	}
	shared, err := r.machineMemoryShared(ctx, machine)
	if err != nil {
		return fmt.Errorf("inspect machine memory scope: %w", err)
	}
	if !shared {
		return nil
	}
	name, ok := memorySliceName(machine)
	if !ok {
		return fmt.Errorf("machine routing key cannot name a memory slice")
	}
	hostConfig.CgroupParent = name
	if serviceType == "collector" || serviceType == "egress" {
		return nil
	}
	hostConfig.Resources.Memory = machineMemoryBytes(machine)
	hostConfig.Resources.MemorySwap = -1
	labels[memoryScopeLabel] = memoryScopeMachine
	return nil
}

// sharedMachineResources is a workload container's update on a machine that
// shares its memory: the CPU and process limits of its share, the machine's
// memory.
func sharedMachineResources(machine *domain.DevMachine, resources container.Resources) container.Resources {
	resources.Memory = machineMemoryBytes(machine)
	resources.MemorySwap = -1
	return resources
}

// dockerMemorySlices changes slices through a helper container: the manager
// runs unprivileged, while systemd accepts unit changes from root only. The
// helper runs the manager's own image as root with no capabilities, network
// or writable root filesystem, and receives only systemd's private socket.
// systemdslice accepts nothing but Kuayle machine slices.
type dockerMemorySlices struct {
	client      client.APIClient
	image       string
	swapPercent int
}

func (s *dockerMemorySlices) Apply(ctx context.Context, machine *domain.DevMachine) error {
	name, ok := memorySliceName(machine)
	if !ok {
		return fmt.Errorf("machine routing key cannot name a memory slice")
	}
	memory := machineMemoryBytes(machine)
	swap := memory * int64(s.swapPercent) / 100
	return s.run(ctx, machine, "apply", name, strconv.FormatInt(memory, 10), strconv.FormatInt(swap, 10))
}

func (s *dockerMemorySlices) Remove(ctx context.Context, machine *domain.DevMachine) error {
	name, ok := memorySliceName(machine)
	if !ok {
		return nil
	}
	return s.run(ctx, machine, "remove", name)
}

func (s *dockerMemorySlices) run(ctx context.Context, machine *domain.DevMachine, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, memorySliceTimeout)
	defer cancel()
	labels := map[string]string{"com.kuayle.managed": "true"}
	if machine != nil {
		labels = machineLabels(machine)
	}
	labels["com.kuayle.service"] = memorySliceService
	pids := int64(32)
	response, err := s.client.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{
			Image: s.image, User: "0:0", Labels: labels,
			Entrypoint: []string{memorySliceEntrypoint}, Cmd: append([]string{systemdslice.Command}, args...),
		},
		HostConfig: &container.HostConfig{
			NetworkMode: "none", ReadonlyRootfs: true, CapDrop: []string{"ALL"},
			SecurityOpt: []string{"no-new-privileges=true"}, LogConfig: boundedLogConfig(),
			Mounts: []mount.Mount{{Type: mount.TypeBind, Source: systemdslice.PrivateSocket, Target: systemdslice.PrivateSocket}},
			Resources: container.Resources{
				Memory: 64 * 1024 * 1024, MemorySwap: 64 * 1024 * 1024, NanoCPUs: 200_000_000, PidsLimit: &pids,
			},
		},
	})
	if err != nil {
		return err
	}
	defer func() {
		_, _ = s.client.ContainerRemove(context.Background(), response.ID, client.ContainerRemoveOptions{Force: true})
	}()
	if _, err := s.client.ContainerStart(ctx, response.ID, client.ContainerStartOptions{}); err != nil {
		return err
	}
	wait := s.client.ContainerWait(ctx, response.ID, client.ContainerWaitOptions{Condition: container.WaitConditionNotRunning})
	select {
	case result := <-wait.Result:
		if result.StatusCode == 0 {
			return nil
		}
		return fmt.Errorf("memory slice %s exited %d: %s", args[0], result.StatusCode, helperOutput(ctx, s.client, response.ID))
	case err := <-wait.Error:
		return err
	}
}

func helperOutput(ctx context.Context, dockerClient client.APIClient, containerID string) string {
	stdout, stderr, _ := (&DockerRuntime{client: dockerClient}).containerLogs(ctx, containerID)
	return strings.TrimSpace(stderr + stdout)
}

// EnableMemorySlices lets machines created from now on share their memory
// when the host supports it: Docker's systemd cgroup driver with cgroup v2,
// the manager running in a container (its image runs the helper), and a
// helper that reaches systemd. Otherwise machines keep separate container
// limits and the reason is returned.
func (r *DockerRuntime) EnableMemorySlices(ctx context.Context, swapPercent int) error {
	if swapPercent < 0 || swapPercent > 100 {
		return fmt.Errorf("swap percent %d is outside 0-100", swapPercent)
	}
	info, err := r.client.Info(ctx, client.InfoOptions{})
	if err != nil {
		return err
	}
	if info.Info.CgroupDriver != "systemd" || info.Info.CgroupVersion != "2" {
		return fmt.Errorf("docker uses the %s cgroup driver with cgroup v%s, not systemd with v2", info.Info.CgroupDriver, info.Info.CgroupVersion)
	}
	hostname, err := os.Hostname()
	if err != nil {
		return err
	}
	self, err := r.client.ContainerInspect(ctx, hostname, client.ContainerInspectOptions{})
	if err != nil {
		return fmt.Errorf("the manager does not run in a container: %w", err)
	}
	if self.Container.Image == "" {
		return errors.New("the manager container has no image")
	}
	slices := &dockerMemorySlices{client: r.client, image: self.Container.Image, swapPercent: swapPercent}
	if err := slices.run(ctx, nil, "probe"); err != nil {
		return err
	}
	r.slices = slices
	return nil
}
