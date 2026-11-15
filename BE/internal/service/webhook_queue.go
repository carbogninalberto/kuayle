package service

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// webhookJob is one delivery: a body for one webhook.
type webhookJob struct {
	ctx         context.Context // the emitter's values, no cancellation, no caller access
	workspaceID uuid.UUID
	event       string
	hook        domain.Webhook
	body        []byte
	queued      time.Time
}

// webhookQueue holds deliveries waiting for a worker. Each workspace has its
// own FIFO backlog. A workspace with a backlog is on the ready channel once,
// and is not put back while one of its deliveries is in flight, so a
// workspace uses at most one worker at a time and workspaces take turns: a
// slow or hung receiver delays only its own workspace's deliveries.
type webhookQueue struct {
	mu                            sync.Mutex
	backlog                       map[uuid.UUID][]webhookJob // queued, per workspace
	scheduled                     map[uuid.UUID]bool         // on ready, or a delivery in flight
	ready                         chan uuid.UUID             // capacity limit: never blocks
	queued                        int
	bytes                         int
	limit, perWorkspace, maxBytes int
	dropped                       atomic.Int64
}

func newWebhookQueue(limit, perWorkspace, maxBytes int) *webhookQueue {
	return &webhookQueue{
		backlog:      map[uuid.UUID][]webhookJob{},
		scheduled:    map[uuid.UUID]bool{},
		ready:        make(chan uuid.UUID, limit),
		limit:        limit,
		perWorkspace: perWorkspace,
		maxBytes:     maxBytes,
	}
}

// enqueue adds job to its workspace's backlog. It reports whether the job was
// accepted: a job beyond any bound is dropped and counted, never waited for.
func (q *webhookQueue) enqueue(job webhookJob) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.queued >= q.limit || q.bytes+len(job.body) > q.maxBytes || len(q.backlog[job.workspaceID]) >= q.perWorkspace {
		q.dropped.Add(1)
		return false
	}
	q.backlog[job.workspaceID] = append(q.backlog[job.workspaceID], job)
	q.queued++
	q.bytes += len(job.body)
	if !q.scheduled[job.workspaceID] {
		q.scheduled[job.workspaceID] = true
		// Only workspaces with at least one queued job are on the channel, and
		// queued never exceeds the channel's capacity, so this never blocks.
		q.ready <- job.workspaceID
	}
	return true
}

// take waits for a workspace with a backlog and returns its first job. The
// workspace stays scheduled while the returned job is in flight.
func (q *webhookQueue) take() webhookJob {
	workspaceID := <-q.ready
	q.mu.Lock()
	defer q.mu.Unlock()
	backlog := q.backlog[workspaceID]
	job := backlog[0]
	backlog[0] = webhookJob{} // zero the popped slot
	backlog = backlog[1:]
	if len(backlog) == 0 {
		delete(q.backlog, workspaceID)
	} else {
		q.backlog[workspaceID] = backlog
	}
	q.queued--
	q.bytes -= len(job.body)
	return job
}

// done reports that the workspace's in-flight delivery finished. A remaining
// backlog goes to the back of the ready channel so workspaces take turns.
func (q *webhookQueue) done(workspaceID uuid.UUID) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.backlog[workspaceID]) > 0 {
		q.ready <- workspaceID
		return
	}
	delete(q.scheduled, workspaceID)
}
