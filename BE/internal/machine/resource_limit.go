package machine

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/client"
	log "github.com/sirupsen/logrus"
)

// ResourceLimitHit is an out-of-memory kill in one machine container, read
// from the container's labels and limit.
type ResourceLimitHit struct {
	MachineID   uuid.UUID
	WorkspaceID uuid.UUID
	AgentRunID  *uuid.UUID
	ContainerID string
	// Service is the container's com.kuayle.service label, or "validation"
	// for an independent check container.
	Service    string
	LimitBytes int64
}

// resourceLimitWatcher is implemented by runtimes that report memory limit
// hits of long-running containers (terminal, editor, Docker daemon) as they
// happen; agent and check exits are inspected by the runs themselves.
type resourceLimitWatcher interface {
	WatchResourceLimits(context.Context, func(ResourceLimitHit))
}

// memoryLimitReportInterval reports one container's burst of kills once.
const memoryLimitReportInterval = time.Minute

const resourceLimitWatchRetry = 5 * time.Second

// WatchResourceLimits follows Docker's out-of-memory events for Kuayle
// containers until ctx ends, reconnecting after stream errors.
func (r *DockerRuntime) WatchResourceLimits(ctx context.Context, handle func(ResourceLimitHit)) {
	for ctx.Err() == nil {
		stream := r.client.Events(ctx, client.EventsListOptions{Filters: make(client.Filters).
			Add("type", string(events.ContainerEventType)).Add("event", string(events.ActionOOM)).
			Add("label", "com.kuayle.managed=true")})
		r.followResourceLimits(ctx, stream, handle)
		select {
		case <-ctx.Done():
			return
		case <-time.After(resourceLimitWatchRetry):
		}
	}
}

func (r *DockerRuntime) followResourceLimits(ctx context.Context, stream client.EventsResult, handle func(ResourceLimitHit)) {
	for {
		select {
		case <-ctx.Done():
			return
		case err := <-stream.Err:
			if err != nil && ctx.Err() == nil {
				log.WithError(err).Warn("container memory event stream ended")
			}
			return
		case message, ok := <-stream.Messages:
			if !ok {
				return
			}
			if hit, ok := r.resourceLimitHit(ctx, message); ok {
				handle(hit)
			}
		}
	}
}

func (r *DockerRuntime) resourceLimitHit(ctx context.Context, message events.Message) (ResourceLimitHit, bool) {
	labels := message.Actor.Attributes
	machineID, err := uuid.Parse(labels["com.kuayle.machine-id"])
	if err != nil || labels["com.kuayle.managed"] != "true" {
		return ResourceLimitHit{}, false
	}
	workspaceID, err := uuid.Parse(labels["com.kuayle.workspace-id"])
	if err != nil {
		return ResourceLimitHit{}, false
	}
	hit := ResourceLimitHit{MachineID: machineID, WorkspaceID: workspaceID, ContainerID: message.Actor.ID, Service: labels["com.kuayle.service"]}
	if labels["com.kuayle.validation-id"] != "" {
		hit.Service = "validation"
	}
	if runID, err := uuid.Parse(labels["com.kuayle.agent-run-id"]); err == nil {
		hit.AgentRunID = &runID
	}
	if inspection, err := r.client.ContainerInspect(ctx, message.Actor.ID, client.ContainerInspectOptions{}); err == nil && inspection.Container.HostConfig != nil {
		hit.LimitBytes = inspection.Container.HostConfig.Memory
	}
	return hit, true
}

// recordMemoryLimit keeps an out-of-memory kill on its machine and in the
// machine's activity, once per container burst. A container whose labels
// name another workspace than its machine is ignored.
func (m *Manager) recordMemoryLimit(ctx context.Context, hit ResourceLimitHit, now time.Time) {
	machine, err := m.store.GetMachineInternal(ctx, hit.MachineID)
	if err != nil || machine == nil || machine.WorkspaceID != hit.WorkspaceID {
		return
	}
	m.limitReportsMu.Lock()
	if m.limitReports == nil {
		m.limitReports = map[string]time.Time{}
	}
	if last, seen := m.limitReports[hit.ContainerID]; seen && now.Sub(last) < memoryLimitReportInterval {
		m.limitReportsMu.Unlock()
		return
	}
	for id, at := range m.limitReports {
		if now.Sub(at) >= memoryLimitReportInterval {
			delete(m.limitReports, id)
		}
	}
	m.limitReports[hit.ContainerID] = now
	m.limitReportsMu.Unlock()
	taskMachine := false
	if hit.AgentRunID != nil {
		if run, err := m.store.GetAgentRunInternal(ctx, *hit.AgentRunID); err == nil && run != nil && run.MachineID == machine.ID {
			taskMachine = run.TaskID != nil
		} else {
			hit.AgentRunID = nil
		}
	}
	message := domain.MemoryLimitMessage(hit.Service, hit.LimitBytes, taskMachine)
	if err := m.store.RecordResourceLimit(ctx, machine.ID, domain.DevMachineResourceLimitMemory, message); err != nil {
		log.WithField("machine_id", machine.ID).WithError(err).Warn("memory limit could not be recorded")
	}
	_ = m.createEvent(ctx, machine, hit.AgentRunID, "runtime", "resource.memory_limit", map[string]any{
		"service": hit.Service, "limit_bytes": hit.LimitBytes, "message": message,
	})
	log.WithFields(log.Fields{"machine_id": machine.ID, "workspace_id": machine.WorkspaceID, "service": hit.Service, "limit_bytes": hit.LimitBytes}).
		Warn("machine container reached its memory limit")
}

// recordRunMemoryLimit explains an agent or check exit after an
// out-of-memory kill and keeps it on the machine; empty when none happened.
func (m *Manager) recordRunMemoryLimit(ctx context.Context, machine *domain.DevMachine, run *domain.DevMachineAgentRun, service string, execution *AgentExecution) string {
	if execution == nil || !execution.OOMKilled {
		return ""
	}
	message := domain.MemoryLimitMessage(service, execution.MemoryLimitBytes, run.TaskID != nil)
	if err := m.store.RecordResourceLimit(ctx, machine.ID, domain.DevMachineResourceLimitMemory, message); err != nil {
		log.WithField("machine_id", machine.ID).WithError(err).Warn("memory limit could not be recorded")
	}
	return message
}

// writeKuayleNote appends Kuayle's explanation to a run's native stderr log,
// in the timestamped form of the container's own lines. The note is
// informational: failing to store it does not change the run's outcome.
func writeKuayleNote(output *agentOutputBuffer, runID uuid.UUID, message string) {
	line := time.Now().UTC().Format(time.RFC3339Nano) + " Kuayle: " + message + "\n"
	if err := output.WriteDocker("stderr", []byte(line)); err != nil {
		log.WithField("agent_run_id", runID).WithError(err).Warn("run log note could not be stored")
	}
}
