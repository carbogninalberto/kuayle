package machine

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/moby/moby/client"
)

// HostResources is the Docker host's capacity. Zero means unknown.
type HostResources struct {
	CPUMillis int
	MemoryMB  int
}

// CapacityConfig reserves host resources for the operating system and the
// Kuayle control plane, and bounds CPU overcommit. Machine memory is never
// overcommitted because exceeding it risks the host's OOM killer.
type CapacityConfig struct {
	ReservedCPUMillis int
	ReservedMemoryMB  int
	CPUOvercommit     float64
}

// capacityWaitError defers an operation until resources free up. It is not a
// failure and does not consume one of the operation's attempts.
type capacityWaitError struct{ message string }

func (e *capacityWaitError) Error() string { return "host_capacity_wait: " + e.message }

const capacityRetryDelay = 30 * time.Second

func (r *DockerRuntime) HostResources(ctx context.Context) (HostResources, error) {
	info, err := r.client.Info(ctx, client.InfoOptions{})
	if err != nil {
		return HostResources{}, err
	}
	return HostResources{CPUMillis: info.Info.NCPU * 1000, MemoryMB: int(info.Info.MemTotal / (1 << 20))}, nil
}

// SetCapacity enables host CPU/memory admission for spawning and starting.
func (m *Manager) SetCapacity(config CapacityConfig) {
	if config.CPUOvercommit < 1 {
		config.CPUOvercommit = 1
	}
	m.capacity = &config
}

// admit reserves the machine's resources or returns a capacity wait. Admission
// is serialized within this manager; machines admitted but not yet recorded as
// running are counted from memory, so concurrent spawns cannot overcommit.
// The returned release must be called when the operation finishes.
func (m *Manager) admit(ctx context.Context, machine *domain.DevMachine) (func(), error) {
	release := func() {}
	if m.capacity == nil {
		return release, nil
	}
	host, err := m.runtime.HostResources(ctx)
	if err != nil {
		return release, err
	}
	if host.CPUMillis <= 0 || host.MemoryMB <= 0 {
		return release, nil
	}
	m.admissionMu.Lock()
	defer m.admissionMu.Unlock()
	allocations, err := m.store.ListResourceAllocations(ctx)
	if err != nil {
		return release, err
	}
	cpu, memory := machine.CPUMillis, machine.MemoryMB
	for _, allocation := range allocations {
		if allocation.MachineID == machine.ID {
			continue
		}
		if _, pending := m.admitted[allocation.MachineID]; pending {
			continue
		}
		cpu += allocation.CPUMillis
		memory += allocation.MemoryMB
	}
	for id, pending := range m.admitted {
		if id != machine.ID {
			cpu += pending.CPUMillis
			memory += pending.MemoryMB
		}
	}
	memoryLimit := host.MemoryMB - m.capacity.ReservedMemoryMB
	cpuLimit := int(float64(host.CPUMillis-m.capacity.ReservedCPUMillis) * m.capacity.CPUOvercommit)
	if memory > memoryLimit {
		return release, &capacityWaitError{message: fmt.Sprintf("waiting for host memory: %d MB requested of %d MB available to machines", memory, max(memoryLimit, 0))}
	}
	if cpu > cpuLimit {
		return release, &capacityWaitError{message: fmt.Sprintf("waiting for host CPU: %d millicores requested of %d available to machines", cpu, max(cpuLimit, 0))}
	}
	if m.admitted == nil {
		m.admitted = map[uuid.UUID]HostResources{}
	}
	m.admitted[machine.ID] = HostResources{CPUMillis: machine.CPUMillis, MemoryMB: machine.MemoryMB}
	return func() {
		m.admissionMu.Lock()
		delete(m.admitted, machine.ID)
		m.admissionMu.Unlock()
	}, nil
}
