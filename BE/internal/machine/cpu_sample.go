package machine

import (
	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// cpuSample holds a container's cumulative CPU counters, in nanoseconds.
type cpuSample struct {
	total, system uint64
	onlineCPUs    uint32
}

// cpuPercent is the container's CPU use between two samples, where 100 is one
// full core. A missing or reset counter (a new or restarted container) has no
// usable delta yet.
func cpuPercent(previous, current cpuSample) float64 {
	if previous.system == 0 || current.total < previous.total || current.system <= previous.system || current.onlineCPUs == 0 {
		return 0
	}
	return float64(current.total-previous.total) / float64(current.system-previous.system) * float64(current.onlineCPUs) * 100
}

func (r *DockerRuntime) previousCPUSamples(machine *domain.DevMachine) map[string]cpuSample {
	if machine == nil {
		return nil
	}
	r.cpuSamplesMu.Lock()
	defer r.cpuSamplesMu.Unlock()
	return r.cpuSamples[machine.ID]
}

// Only containers seen in this sample are kept, so finished agents drop out.
func (r *DockerRuntime) storeCPUSamples(machine *domain.DevMachine, samples map[string]cpuSample) {
	if machine == nil {
		return
	}
	r.cpuSamplesMu.Lock()
	defer r.cpuSamplesMu.Unlock()
	if r.cpuSamples == nil {
		r.cpuSamples = make(map[uuid.UUID]map[string]cpuSample)
	}
	r.cpuSamples[machine.ID] = samples
}
