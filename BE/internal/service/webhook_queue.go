package service

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	log "github.com/sirupsen/logrus"
)

// webhookLimits bounds the jobs waiting for a worker, in all workspaces and in
// one workspace.
type webhookLimits struct {
	jobs, bytes                   int // waiting in all
	workspaceJobs, workspaceBytes int // waiting per workspace
}

var defaultWebhookLimits = webhookLimits{
	jobs:           webhookQueueJobs,
	bytes:          webhookQueueBytes,
	workspaceJobs:  webhookWorkspaceJobs,
	workspaceBytes: webhookWorkspaceBytes,
}

// webhookJob is one payload for every webhook subscribed to its event. The
// body is held once and sent to hooks in order.
type webhookJob struct {
	ctx         context.Context // the emitter's values, no cancellation, no caller access
	workspaceID uuid.UUID
	event       string
	hooks       []domain.Webhook
	body        []byte
	queued      time.Time // set by enqueue from q.now()
}

// webhookBacklog is one workspace's FIFO of waiting jobs.
type webhookBacklog struct {
	jobs  []webhookJob
	bytes int
}

// webhookQueue holds deliveries waiting for a worker. Each workspace has its
// own FIFO backlog and is offered to workers once at a time, so a slow or hung
// receiver delays only its own workspace's deliveries. Per-workspace bounds
// keep one workspace from crowding out the others, and the global bounds keep
// memory bounded by the bodies waiting plus the small per-job overhead. A
// workspace that waited too long has all of its stale jobs purged at once.
type webhookQueue struct {
	mu        sync.Mutex
	cond      *sync.Cond // on mu; wakes workers when a workspace becomes ready
	backlog   map[uuid.UUID]*webhookBacklog
	scheduled map[uuid.UUID]bool // in ready, or a delivery is in flight
	ready     []uuid.UUID        // scheduled workspaces with no delivery in flight, FIFO
	jobs      int
	bytes     int
	limits    webhookLimits
	maxWait   time.Duration
	now       func() time.Time // tests replace it
	closed    bool
	// dropped counts jobs the queue discarded: rejected at enqueue, purged as
	// stale, or abandoned at close.
	dropped atomic.Int64
}

func newWebhookQueue(limits webhookLimits) *webhookQueue {
	q := &webhookQueue{
		backlog:   map[uuid.UUID]*webhookBacklog{},
		scheduled: map[uuid.UUID]bool{},
		limits:    limits,
		maxWait:   webhookMaxQueueWait,
		now:       time.Now,
	}
	q.cond = sync.NewCond(&q.mu)
	return q
}

// enqueue adds job to its workspace's backlog. It reports whether the job was
// accepted: a job beyond any bound is dropped and counted, never waited for.
// The body is counted once per job, however many webhooks it is sent to.
func (q *webhookQueue) enqueue(job webhookJob) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		q.dropped.Add(1)
		return false
	}
	size := len(job.body)
	backlog := q.backlog[job.workspaceID]
	workspaceJobs, workspaceBytes := 0, 0
	if backlog != nil {
		workspaceJobs, workspaceBytes = len(backlog.jobs), backlog.bytes
	}
	if q.jobs+1 > q.limits.jobs || q.bytes+size > q.limits.bytes ||
		workspaceJobs+1 > q.limits.workspaceJobs || workspaceBytes+size > q.limits.workspaceBytes {
		q.dropped.Add(1)
		return false
	}
	job.queued = q.now()
	if backlog == nil {
		backlog = &webhookBacklog{}
		q.backlog[job.workspaceID] = backlog
	}
	backlog.jobs = append(backlog.jobs, job)
	backlog.bytes += size
	q.jobs++
	q.bytes += size
	if !q.scheduled[job.workspaceID] {
		q.scheduled[job.workspaceID] = true
		q.ready = append(q.ready, job.workspaceID)
		q.cond.Signal()
	}
	return true
}

// take waits for a workspace with a backlog and returns its first fresh job.
// The workspace stays scheduled while the returned job is in flight. Jobs of
// the workspace that waited longer than maxWait are purged together, and
// reported with one aggregated log line.
func (q *webhookQueue) take() (webhookJob, bool) {
	for {
		job, found, purged, workspaceID, open := q.next()
		if !open {
			return webhookJob{}, false
		}
		if purged > 0 {
			log.WithField("workspace_id", workspaceID).WithField("dropped", purged).
				WithField("dropped_total", q.dropped.Load()).
				Warn("webhook deliveries dropped: they waited too long for a worker")
		}
		if found {
			return job, true
		}
	}
}

// next is one step of take under the lock: it pops the first ready workspace,
// purges that workspace's stale prefix, and returns either its first remaining
// job or, when everything was stale, nothing. It holds the lock while waiting
// for a workspace and returns open=false once the queue is closed.
func (q *webhookQueue) next() (job webhookJob, found bool, purged int, workspaceID uuid.UUID, open bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.ready) == 0 {
		if q.closed {
			return webhookJob{}, false, 0, uuid.Nil, false
		}
		q.cond.Wait()
	}
	if q.closed {
		return webhookJob{}, false, 0, uuid.Nil, false
	}
	workspaceID = q.ready[0]
	q.ready = q.ready[1:]
	backlog := q.backlog[workspaceID]
	now := q.now()
	// Jobs are FIFO, so the stale ones are a prefix.
	for purged < len(backlog.jobs) && now.Sub(backlog.jobs[purged].queued) > q.maxWait {
		size := len(backlog.jobs[purged].body)
		backlog.jobs[purged] = webhookJob{} // zero the removed slot
		backlog.bytes -= size
		q.bytes -= size
		purged++
	}
	if purged > 0 {
		backlog.jobs = backlog.jobs[purged:]
		q.jobs -= purged
		q.dropped.Add(int64(purged))
	}
	if len(backlog.jobs) == 0 {
		delete(q.backlog, workspaceID)
		delete(q.scheduled, workspaceID)
		return webhookJob{}, false, purged, workspaceID, true
	}
	job = backlog.jobs[0]
	backlog.jobs[0] = webhookJob{} // zero the popped slot
	backlog.jobs = backlog.jobs[1:]
	backlog.bytes -= len(job.body)
	q.jobs--
	q.bytes -= len(job.body)
	return job, true, purged, workspaceID, true
}

// done reports that the workspace's in-flight delivery finished. A remaining
// backlog goes to the back of the ready workspaces so workspaces take turns.
func (q *webhookQueue) done(workspaceID uuid.UUID) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	if backlog := q.backlog[workspaceID]; backlog != nil && len(backlog.jobs) > 0 {
		q.ready = append(q.ready, workspaceID)
		q.cond.Signal()
		return
	}
	delete(q.scheduled, workspaceID)
}

// close discards every waiting job, counting them as dropped, and wakes the
// workers so they stop. It is idempotent.
func (q *webhookQueue) close() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	q.closed = true
	for workspaceID, backlog := range q.backlog {
		q.dropped.Add(int64(len(backlog.jobs)))
		delete(q.backlog, workspaceID)
	}
	q.jobs = 0
	q.bytes = 0
	q.ready = nil
	q.cond.Broadcast()
}

// webhookBreaker pauses a webhook after repeated failed deliveries, per webhook
// ID and process. While paused its deliveries are dropped instead of sent, so
// a dead receiver no longer delays the healthy ones; after the cooldown one
// delivery probes it again.
type webhookBreaker struct {
	mu        sync.Mutex
	now       func() time.Time // tests replace it
	threshold int              // consecutive failures that pause a webhook
	cooldown  time.Duration    // how long a paused webhook is skipped
	circuits  map[uuid.UUID]*webhookCircuit
	swept     time.Time // last sweep of failed circuits that were never paused
}

type webhookCircuit struct {
	failures    int       // consecutive failed deliveries
	openUntil   time.Time // zero while closed
	skipped     int       // deliveries dropped while paused, not yet logged
	lastFailure time.Time
}

func newWebhookBreaker() *webhookBreaker {
	return &webhookBreaker{
		now:       time.Now,
		threshold: webhookBreakerFailures,
		cooldown:  webhookBreakerCooldown,
		circuits:  map[uuid.UUID]*webhookCircuit{},
	}
}

// allow reports whether deliveries to the webhook may be attempted now. While
// paused (now before openUntil) it adds n to skipped and returns false. Once
// the cooldown has passed (half-open) it returns true and, once, the number
// skipped while paused (then resets skipped to 0) so the caller logs one line.
func (b *webhookBreaker) allow(id uuid.UUID, n int) (allowed bool, skipped int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	circuit := b.circuits[id]
	if circuit == nil || circuit.openUntil.IsZero() {
		return true, 0
	}
	if b.now().Before(circuit.openUntil) {
		circuit.skipped += n
		return false, 0
	}
	skipped = circuit.skipped
	circuit.skipped = 0
	return true, skipped
}

// record notes the outcome of an attempted delivery. Success closes the
// circuit; a failure that reaches the threshold pauses the webhook, so a
// failure while half-open pauses it again at once.
func (b *webhookBreaker) record(id uuid.UUID, ok bool) (opened bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	if ok {
		delete(b.circuits, id)
		b.sweep(now)
		return false
	}
	circuit := b.circuits[id]
	if circuit == nil {
		circuit = &webhookCircuit{}
		b.circuits[id] = circuit
	}
	circuit.failures++
	circuit.lastFailure = now
	if circuit.failures >= b.threshold {
		circuit.openUntil = now.Add(b.cooldown)
		opened = true
	}
	b.sweep(now)
	return opened
}

// sweep forgets circuits that failed long ago and are not paused, at most once
// per cooldown, so the breaker's memory stays bounded.
func (b *webhookBreaker) sweep(now time.Time) {
	if now.Sub(b.swept) <= b.cooldown {
		return
	}
	for id, circuit := range b.circuits {
		if circuit.openUntil.After(now) {
			continue // still paused
		}
		if now.Sub(circuit.lastFailure) > 2*b.cooldown {
			delete(b.circuits, id)
		}
	}
	b.swept = now
}
