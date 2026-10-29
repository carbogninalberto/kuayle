package machine

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/containerd/errdefs"
	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
	log "github.com/sirupsen/logrus"
)

// The live channel lets the task's owner (or the orchestrator acting for
// them) reach a running attempt. While the attempt runs, the manager writes
// each queued message as <seq>.json into a per-run Docker volume that the
// agent container mounts read-only at /run/kuayle/inbox, outside the
// worktree, so the agent cannot write or forge messages there. The manager
// fills the volume through a writer container that is created with the
// volume read-write and never started. In the same loop the manager collects
// the outbox, so agent messages (including those to the orchestrator) are
// recorded within seconds instead of only when the attempt ends.

// liveChannelInterval is how often a running attempt's inbox and outbox are
// synchronized.
const liveChannelInterval = 5 * time.Second

const liveInboxService = "agent-inbox"

// errLiveInboxUnavailable reports an attempt without an inbox (yet).
var errLiveInboxUnavailable = errors.New("live inbox is unavailable")

// liveInboxRuntime is implemented by runtimes that give attempts an inbox.
type liveInboxRuntime interface {
	WriteLiveInbox(ctx context.Context, machine *domain.DevMachine, run *domain.DevMachineAgentRun, files []domain.AgentLiveInboxFile) error
	CloseLiveInbox(ctx context.Context, machine *domain.DevMachine, run *domain.DevMachineAgentRun) error
}

type liveInboxStore interface {
	PendingLiveMessages(ctx context.Context, run uuid.UUID) ([]domain.AgentLiveMessage, error)
	MarkLiveMessagesDelivered(ctx context.Context, run uuid.UUID, ids []uuid.UUID) (int, error)
}

// startLiveChannel synchronizes the run's inbox and the machine's outboxes
// until ctx ends or the returned stop is called; stop waits for the loop.
func (m *Manager) startLiveChannel(ctx context.Context, machine *domain.DevMachine, run *domain.DevMachineAgentRun) func() {
	interval := m.liveInterval
	if interval <= 0 {
		interval = liveChannelInterval
	}
	loopCtx, cancel := context.WithCancel(ctx)
	var done sync.WaitGroup
	done.Add(1)
	go func() {
		defer done.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-loopCtx.Done():
				return
			case <-ticker.C:
			}
			tickCtx, cancelTick := context.WithTimeout(loopCtx, 15*time.Second)
			if services, err := m.store.ListServices(tickCtx, machine.WorkspaceID, machine.ID); err == nil {
				m.collectOutbox(tickCtx, machine, services)
			}
			if err := m.deliverLiveInbox(tickCtx, machine, run); err != nil && !errors.Is(err, errLiveInboxUnavailable) && tickCtx.Err() == nil {
				log.WithFields(log.Fields{"machine_id": machine.ID, "agent_run_id": run.ID, "event_type": "agent_live_messages.deliver"}).WithError(err).Warn("live messages could not be delivered")
			}
			cancelTick()
		}
	}()
	return func() {
		cancel()
		done.Wait()
	}
}

// deliverLiveInbox writes the run's queued messages to its inbox, then marks
// them delivered. A failed mark leaves them queued; rewriting the same
// <seq>.json is harmless because the reader tracks sequence numbers.
func (m *Manager) deliverLiveInbox(ctx context.Context, machine *domain.DevMachine, run *domain.DevMachineAgentRun) error {
	runtime, ok := m.runtime.(liveInboxRuntime)
	store, recording := m.store.(liveInboxStore)
	if !ok || !recording {
		return nil
	}
	messages, err := store.PendingLiveMessages(ctx, run.ID)
	if err != nil || len(messages) == 0 {
		return err
	}
	files := make([]domain.AgentLiveInboxFile, 0, len(messages))
	ids := make([]uuid.UUID, 0, len(messages))
	for _, message := range messages {
		file, err := message.InboxFile()
		if err != nil {
			return err
		}
		files = append(files, file)
		ids = append(ids, message.ID)
	}
	if err = runtime.WriteLiveInbox(ctx, machine, run, files); err != nil {
		return err
	}
	delivered, err := store.MarkLiveMessagesDelivered(ctx, run.ID, ids)
	if delivered > 0 {
		_ = m.createEvent(ctx, machine, &run.ID, "agent", "agent_live_messages.delivered", map[string]any{"count": delivered})
	}
	return err
}

// closeLiveInbox removes the run's inbox writer once the attempt ended.
func (m *Manager) closeLiveInbox(ctx context.Context, machine *domain.DevMachine, run *domain.DevMachineAgentRun) {
	runtime, ok := m.runtime.(liveInboxRuntime)
	if !ok {
		return
	}
	if err := runtime.CloseLiveInbox(ctx, machine, run); err != nil && ctx.Err() == nil {
		log.WithFields(log.Fields{"machine_id": machine.ID, "agent_run_id": run.ID}).WithError(err).Warn("live inbox could not be closed")
	}
}

func liveInboxName(machine *domain.DevMachine, run *domain.DevMachineAgentRun) string {
	return "kuayle-" + machine.RoutingKey + "-inbox-" + run.ID.String()
}

func liveInboxLabels(machine *domain.DevMachine, run *domain.DevMachineAgentRun) map[string]string {
	labels := machineLabels(machine)
	labels["com.kuayle.service"] = liveInboxService
	labels["com.kuayle.agent-run-id"] = run.ID.String()
	return labels
}

func ownedLiveInbox(labels map[string]string, machine *domain.DevMachine, run *domain.DevMachineAgentRun) bool {
	return ownedByMachine(labels, machine.ID) && labels["com.kuayle.service"] == liveInboxService && labels["com.kuayle.agent-run-id"] == run.ID.String()
}

// liveInboxMount is the agent container's read-only view of its inbox.
func liveInboxMount(machine *domain.DevMachine, run *domain.DevMachineAgentRun) mount.Mount {
	return mount.Mount{
		Type: mount.TypeVolume, Source: liveInboxName(machine, run), Target: domain.AgentLiveInboxPath, ReadOnly: true,
		VolumeOptions: &mount.VolumeOptions{NoCopy: true},
	}
}

// prepareLiveInbox creates the run's inbox volume and its writer container
// (created, never started: Docker copies into a stopped container's
// volumes). An existing writer must belong to the run.
func (r *DockerRuntime) prepareLiveInbox(ctx context.Context, machine *domain.DevMachine, run *domain.DevMachineAgentRun, imageRef string) error {
	name, labels := liveInboxName(machine, run), liveInboxLabels(machine, run)
	created, err := r.client.VolumeCreate(ctx, client.VolumeCreateOptions{Name: name, Driver: "local", Labels: labels})
	if err != nil {
		return fmt.Errorf("create live inbox volume: %w", err)
	}
	if !ownedLiveInbox(created.Volume.Labels, machine, run) {
		return fmt.Errorf("live inbox volume %s ownership does not match the run", name)
	}
	if inspection, err := r.client.ContainerInspect(ctx, name, client.ContainerInspectOptions{}); err == nil {
		if !ownedLiveInbox(inspection.Container.Config.Labels, machine, run) {
			return fmt.Errorf("live inbox writer %s ownership does not match the run", name)
		}
		return nil
	} else if !errdefs.IsNotFound(err) {
		return err
	}
	_, err = r.client.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{Image: imageRef, Entrypoint: []string{"/bin/true"}, Labels: labels, NetworkDisabled: true},
		HostConfig: &container.HostConfig{
			NetworkMode: "none", ReadonlyRootfs: true, CapDrop: []string{"ALL"}, SecurityOpt: []string{"no-new-privileges=true"},
			LogConfig: boundedLogConfig(),
			Mounts:    []mount.Mount{{Type: mount.TypeVolume, Source: name, Target: "/inbox", VolumeOptions: &mount.VolumeOptions{NoCopy: true}}},
		},
		Name: name,
	})
	if err != nil {
		_, _ = r.client.VolumeRemove(context.WithoutCancel(ctx), name, client.VolumeRemoveOptions{})
		return fmt.Errorf("create live inbox writer: %w", err)
	}
	return nil
}

// WriteLiveInbox copies inbox files into the run's inbox as root-owned,
// world-readable regular files.
func (r *DockerRuntime) WriteLiveInbox(ctx context.Context, machine *domain.DevMachine, run *domain.DevMachineAgentRun, files []domain.AgentLiveInboxFile) error {
	if len(files) == 0 {
		return nil
	}
	name := liveInboxName(machine, run)
	inspection, err := r.client.ContainerInspect(ctx, name, client.ContainerInspectOptions{})
	if errdefs.IsNotFound(err) {
		return errLiveInboxUnavailable
	}
	if err != nil {
		return err
	}
	if !ownedLiveInbox(inspection.Container.Config.Labels, machine, run) {
		return fmt.Errorf("live inbox writer %s ownership does not match the run", name)
	}
	archive, err := liveInboxArchive(files, time.Now())
	if err != nil {
		return err
	}
	_, err = r.client.CopyToContainer(ctx, inspection.Container.ID, client.CopyToContainerOptions{DestinationPath: "/inbox", Content: bytes.NewReader(archive)})
	return err
}

func liveInboxArchive(files []domain.AgentLiveInboxFile, now time.Time) ([]byte, error) {
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	for _, file := range files {
		if err := writer.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: file.Name, Mode: 0o644, Size: int64(len(file.Data)), ModTime: now, Format: tar.FormatPAX}); err != nil {
			return nil, err
		}
		if _, err := writer.Write(file.Data); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// CloseLiveInbox removes the run's writer and, once no container uses it,
// the inbox volume; an ended agent container keeps it until it is removed
// with the run (CancelAgent) or the machine (Teardown).
func (r *DockerRuntime) CloseLiveInbox(ctx context.Context, machine *domain.DevMachine, run *domain.DevMachineAgentRun) error {
	name := liveInboxName(machine, run)
	inspection, err := r.client.ContainerInspect(ctx, name, client.ContainerInspectOptions{})
	if err == nil {
		if !ownedLiveInbox(inspection.Container.Config.Labels, machine, run) {
			return fmt.Errorf("live inbox writer %s ownership does not match the run", name)
		}
		if _, err = r.client.ContainerRemove(ctx, inspection.Container.ID, client.ContainerRemoveOptions{Force: true}); err != nil && !errdefs.IsNotFound(err) {
			return err
		}
	} else if !errdefs.IsNotFound(err) {
		return err
	}
	if _, err = r.client.VolumeRemove(ctx, name, client.VolumeRemoveOptions{}); err != nil && !errdefs.IsNotFound(err) && !errdefs.IsConflict(err) {
		return err
	}
	return nil
}

// removeLiveInboxVolumes removes the inbox volumes matching filter, once the
// containers using them are gone. Only volumes of the machine are removed.
func (r *DockerRuntime) removeLiveInboxVolumes(ctx context.Context, filter string, machineID uuid.UUID) error {
	volumes, err := r.client.VolumeList(ctx, client.VolumeListOptions{Filters: make(client.Filters).Add("label", filter).Add("label", "com.kuayle.service="+liveInboxService)})
	if err != nil {
		return err
	}
	var failures []error
	for _, item := range volumes.Items {
		if !ownedByMachine(item.Labels, machineID) || item.Labels["com.kuayle.service"] != liveInboxService {
			failures = append(failures, fmt.Errorf("volume %s ownership does not match", item.Name))
			continue
		}
		if _, err := r.client.VolumeRemove(ctx, item.Name, client.VolumeRemoveOptions{Force: true}); err != nil && !errdefs.IsNotFound(err) {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

var (
	_ liveInboxRuntime = (*DockerRuntime)(nil)
	_ liveInboxStore   = (*repository.DevMachineRepository)(nil)
)
