package machine

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	log "github.com/sirupsen/logrus"
)

// Resource sampling runs beside reconcile, never inside it: a slow or hung
// reconcile pass (a recovering machine can take minutes) must not stop the
// usage history of every other machine. The sampler is the only caller of
// Runtime.Stats, because the Docker runtime derives CPU from the difference
// between two consecutive calls for a machine.
const (
	// resourceSampleTimeout bounds one machine's sample, its retry included.
	resourceSampleTimeout = 20 * time.Second
	// resourceSampleRetryDelay is the wait before the one retry of a failed
	// stats call or insert. With the 30 second interval the longest gap a
	// failing sample leaves is about a minute.
	resourceSampleRetryDelay = 5 * time.Second
	// resourceSampleConcurrency caps the machines sampled at the same moment.
	resourceSampleConcurrency = 8
	// resourceSamplePageSize is how many running machines one list call returns.
	resourceSamplePageSize = 100

	// resourceSamplePruneInterval is how often samples older than
	// DevMachineResourceRetention are removed, for every machine.
	resourceSamplePruneInterval = 5 * time.Minute
	// resourceSamplePruneBatch is how many rows one delete statement removes.
	resourceSamplePruneBatch = 5000
	// resourceSamplePruneTimeout bounds one whole prune pass.
	resourceSamplePruneTimeout = 2 * time.Minute
)

// scheduleResourceSampling lists and starts this interval's samples in the
// background, so a slow database never holds up the manager loop. A listing
// still running from the previous tick is not repeated.
func (m *Manager) scheduleResourceSampling(ctx context.Context) {
	if !m.sampleListing.CompareAndSwap(false, true) {
		return
	}
	m.operations.Add(1)
	go func() {
		defer m.operations.Done()
		defer m.sampleListing.Store(false)
		m.sampleResources(ctx)
	}()
}

// scheduleResourcePrune runs the retention prune in the background; a pass
// still running from the previous tick is not repeated.
func (m *Manager) scheduleResourcePrune(ctx context.Context) {
	if !m.pruningSamples.CompareAndSwap(false, true) {
		return
	}
	m.operations.Add(1)
	go func() {
		defer m.operations.Done()
		defer m.pruningSamples.Store(false)
		m.pruneResourceSamples(ctx)
	}()
}

// sampleResources starts a sample of every running machine and returns without
// waiting for them: each machine is sampled by its own goroutine, so a Docker
// call that hangs for one machine delays nobody else. A machine whose previous
// sample is still running is skipped, which keeps goroutines from piling up
// behind a stuck daemon call. Callers that need the samples to finish wait on
// m.operations.
func (m *Manager) sampleResources(ctx context.Context) {
	listCtx, cancel := context.WithTimeout(ctx, resourceSampleTimeout)
	defer cancel()
	for offset := 0; ; offset += resourceSamplePageSize {
		machines, err := m.store.ListSamplingMachines(listCtx, resourceSamplePageSize, offset)
		if err != nil {
			if ctx.Err() == nil {
				log.WithError(err).Warn("listing machines to sample failed; this interval's samples are missed")
			}
			return
		}
		for i := range machines {
			machine := machines[i]
			if !m.claimSampling(machine.ID) {
				continue
			}
			m.operations.Add(1)
			go func() {
				defer m.operations.Done()
				defer m.releaseSampling(machine.ID)
				// A machine waiting for a slot still counts as in flight.
				slots := m.sampleSlotChannel()
				select {
				case slots <- struct{}{}:
					defer func() { <-slots }()
				case <-ctx.Done():
					return
				}
				m.sampleMachine(ctx, &machine)
			}()
		}
		if len(machines) < resourceSamplePageSize {
			return
		}
	}
}

func (m *Manager) sampleSlotChannel() chan struct{} {
	m.samplingMu.Lock()
	defer m.samplingMu.Unlock()
	if m.sampleSlots == nil {
		m.sampleSlots = make(chan struct{}, resourceSampleConcurrency)
	}
	return m.sampleSlots
}

func (m *Manager) claimSampling(machineID uuid.UUID) bool {
	m.samplingMu.Lock()
	defer m.samplingMu.Unlock()
	if m.sampling == nil {
		m.sampling = make(map[uuid.UUID]struct{})
	}
	if _, running := m.sampling[machineID]; running {
		return false
	}
	m.sampling[machineID] = struct{}{}
	return true
}

func (m *Manager) releaseSampling(machineID uuid.UUID) {
	m.samplingMu.Lock()
	defer m.samplingMu.Unlock()
	delete(m.sampling, machineID)
}

// sampleMachine takes and stores one sample. A failed stats call or insert is
// logged and counted, and tried once more after a short wait, so a transient
// error costs a sample at most rather than a whole interval.
func (m *Manager) sampleMachine(ctx context.Context, machine *domain.DevMachine) {
	// parent ends when the manager stops; that is not a failed sample.
	parent := ctx
	ctx, cancel := context.WithTimeout(ctx, resourceSampleTimeout)
	defer cancel()
	logger := log.WithFields(log.Fields{"machine_id": machine.ID, "workspace_id": machine.WorkspaceID})
	retryDelay := m.sampleRetryDelay
	if retryDelay <= 0 {
		retryDelay = resourceSampleRetryDelay
	}

	var usage ResourceUsage
	gathered := false
	for attempt := 0; attempt < 2 && !gathered; attempt++ {
		if attempt > 0 && !sleepContext(ctx, retryDelay) {
			break
		}
		services, err := m.store.ListServices(ctx, machine.WorkspaceID, machine.ID)
		if err == nil {
			usage, err = m.runtime.Stats(ctx, machine, services)
		}
		if err == nil {
			gathered = true
			break
		}
		if parent.Err() != nil {
			return
		}
		m.resourceSampleStatsFailed.Add(1)
		logger.WithError(err).WithField("attempt", attempt+1).Warn("resource sampling failed")
	}
	if !gathered {
		m.resourceSamplesMissed.Add(1)
		logger.Warn("resource sample missed")
		return
	}

	sample := domain.DevMachineResourceSample{
		WorkspaceID: machine.WorkspaceID, MachineID: machine.ID, CPUPercent: usage.CPUPercent, MemoryBytes: usage.MemoryBytes,
		DiskBytes: usage.DiskBytes, Pids: usage.Pids, NetworkRxBytes: usage.NetworkRxBytes,
		NetworkTxBytes: usage.NetworkTxBytes,
	}
	stored := false
	for attempt := 0; attempt < 2 && !stored; attempt++ {
		if attempt > 0 && !sleepContext(ctx, retryDelay) {
			break
		}
		err := m.store.CreateResourceSample(ctx, &sample)
		if err == nil {
			stored = true
			break
		}
		if parent.Err() != nil {
			break
		}
		m.resourceSampleInsertFailed.Add(1)
		logger.WithError(err).WithField("attempt", attempt+1).Warn("resource sample insert failed")
	}
	if stored {
		m.resourceSamplesRecorded.Add(1)
	} else {
		m.resourceSamplesMissed.Add(1)
		logger.Warn("resource sample missed")
	}
	// The disk quota follows the measured usage whether or not the row was kept.
	m.enforceDiskUsage(ctx, machine, usage)
}

// sleepContext waits d and reports false if ctx ended first.
func sleepContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// enforceDiskUsage records the workspace volume size and applies the disk
// quota: a warning at 90% (once per machine generation) and a stop at the limit.
func (m *Manager) enforceDiskUsage(ctx context.Context, machine *domain.DevMachine, usage ResourceUsage) {
	if err := m.store.UpdateVolumeUsage(ctx, machine.ID, usage.DiskBytes); err != nil {
		log.WithField("machine_id", machine.ID).WithError(err).Warn("volume usage update failed")
	}
	// A machine already on its way to stopping (this quota's own stop included)
	// is not warned or stopped again on every sample.
	if machine.DesiredStatus != domain.DevMachineStatusRunning {
		return
	}
	limit := int64(machine.DiskGB) * 1024 * 1024 * 1024
	if usage.DiskBytes >= limit*9/10 && usage.DiskBytes < limit && m.claimDiskQuotaWarning(machine.ID, machine.Generation) {
		_ = m.createEvent(ctx, machine, nil, "collector", "resource.disk_quota_warning", map[string]any{
			"used_bytes": usage.DiskBytes, "limit_bytes": limit, "percent": usage.DiskBytes * 100 / limit,
		})
	}
	if usage.DiskBytes >= limit {
		operation := &domain.DevMachineOperation{
			ID: uuid.New(), MachineID: machine.ID, WorkspaceID: machine.WorkspaceID,
			Action: domain.DevMachineOpStop, Status: domain.DevMachineOpStatusPending,
			Generation: machine.Generation + 1, IdempotencyKey: fmt.Sprintf("disk-quota:%d", machine.Generation), MaxAttempts: 3,
		}
		_ = m.store.SetDesiredAndEnqueue(ctx, machine.WorkspaceID, machine.ID, domain.DevMachineStatusStopped, operation)
		_ = m.store.RecordResourceLimit(ctx, machine.ID, domain.DevMachineResourceLimitDisk, domain.DiskLimitMessage(machine.DiskGB))
		_ = m.createEvent(ctx, machine, nil, "collector", "resource.disk_quota_exceeded", map[string]any{"used_bytes": usage.DiskBytes, "limit_bytes": limit})
	}
}

// pruneResourceSamples enforces the rolling retention window: it deletes the
// samples of every machine, stopped and torn-down ones included, that are
// older than DevMachineResourceRetention, in batches so one pass never holds a
// long lock.
func (m *Manager) pruneResourceSamples(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, resourceSamplePruneTimeout)
	defer cancel()
	before := time.Now().UTC().Add(-domain.DevMachineResourceRetention)
	total := 0
	for {
		count, err := m.store.PruneExpiredResourceSamples(ctx, before, resourceSamplePruneBatch)
		total += count
		if err != nil {
			if ctx.Err() == nil {
				log.WithError(err).Warn("resource sample pruning failed")
			}
			break
		}
		if count < resourceSamplePruneBatch {
			break
		}
	}
	if total > 0 {
		log.WithField("pruned", total).Debug("expired resource samples pruned")
	}
}
