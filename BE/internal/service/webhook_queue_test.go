package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/lib/pq"
	"github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/require"
)

// hook registers a webhook for ws at an arbitrary URL, without a receiver.
func (f *webhookFixture) hook(ws uuid.UUID, url string, events ...string) uuid.UUID {
	f.t.Helper()
	encrypted, err := crypto.Encrypt("hook-secret", crypto.DeriveKey(webhookTestJWTSecret+":webhook"))
	require.NoError(f.t, err)
	id := uuid.New()
	_, err = f.db.Exec(`INSERT INTO webhooks(id,workspace_id,url,secret,events,is_active) VALUES($1,$2,$3,$4,$5,true)`,
		id, ws, url, encrypted, pq.StringArray(events))
	require.NoError(f.t, err)
	return id
}

// backdateHook makes a webhook older than the others, so ListDeliverable's
// ordering by created_at, id is deterministic regardless of insert timing.
func (f *webhookFixture) backdateHook(id uuid.UUID, ago time.Duration) {
	f.t.Helper()
	_, err := f.db.Exec(`UPDATE webhooks SET created_at = NOW() - $2::interval WHERE id = $1`, id, ago.String())
	require.NoError(f.t, err)
}

// hungReceiver is a receiver that answers only when the test releases it,
// recording how many requests were in flight at once and how many arrived.
type hungReceiver struct {
	server      *httptest.Server
	arrived     chan struct{}
	release     chan struct{}
	releaseOnce sync.Once
	hits        atomic.Int64
	inFlight    atomic.Int64
	maxInFlight atomic.Int64
}

func newHungReceiver(t *testing.T) *hungReceiver {
	t.Helper()
	h := &hungReceiver{arrived: make(chan struct{}, 64), release: make(chan struct{})}
	h.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.hits.Add(1)
		current := h.inFlight.Add(1)
		for {
			max := h.maxInFlight.Load()
			if current <= max || h.maxInFlight.CompareAndSwap(max, current) {
				break
			}
		}
		select {
		case h.arrived <- struct{}{}:
		default:
		}
		select {
		case <-h.release:
		case <-r.Context().Done():
		}
		h.inFlight.Add(-1)
		w.WriteHeader(http.StatusNoContent)
	}))
	// LIFO: release the handlers before Close waits for them.
	t.Cleanup(func() {
		h.close()
		h.server.Close()
	})
	return h
}

func (h *hungReceiver) close() {
	h.releaseOnce.Do(func() { close(h.release) })
}

func (h *hungReceiver) waitArrival(t *testing.T) {
	t.Helper()
	select {
	case <-h.arrived:
	case <-time.After(5 * time.Second):
		t.Fatal("webhook delivery did not reach the receiver")
	}
}

func (h *hungReceiver) noArrival(t *testing.T, window time.Duration) {
	t.Helper()
	select {
	case <-h.arrived:
		t.Fatal("unexpected webhook delivery reached the receiver")
	case <-time.After(window):
	}
}

func testQueueState(q *webhookQueue) (queued int, scheduled int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.jobs, len(q.scheduled)
}

// logEntriesWithMessage returns the captured log entries with this message.
func logEntriesWithMessage(hook *logtest.Hook, message string) []*logrus.Entry {
	var entries []*logrus.Entry
	for _, entry := range hook.AllEntries() {
		if entry.Message == message {
			entries = append(entries, entry)
		}
	}
	return entries
}

func TestWebhookQueueTakesWorkspacesInTurnsAndBoundsBacklog(t *testing.T) {
	wsA, wsB, wsC := uuid.New(), uuid.New(), uuid.New()
	q := newWebhookQueue(webhookLimits{jobs: 4, bytes: 1 << 20, workspaceJobs: 2, workspaceBytes: 1 << 20})
	job := func(ws uuid.UUID, marker byte) webhookJob {
		return webhookJob{workspaceID: ws, body: []byte{marker}}
	}
	take := func() webhookJob {
		t.Helper()
		got, ok := q.take()
		require.True(t, ok)
		return got
	}

	require.True(t, q.enqueue(job(wsA, 'a')), "A1")
	require.True(t, q.enqueue(job(wsA, 'b')), "A2")
	require.False(t, q.enqueue(job(wsA, 'c')), "A3: the workspace backlog is full")
	require.True(t, q.enqueue(job(wsB, 'd')), "B1")
	require.True(t, q.enqueue(job(wsC, 'e')), "C1")
	require.False(t, q.enqueue(job(wsC, 'f')), "C2: the queue is full")

	require.Equal(t, job(wsA, 'a').body, take().body, "A first")
	require.Equal(t, job(wsB, 'd').body, take().body, "then B takes a turn")
	require.Equal(t, job(wsC, 'e').body, take().body, "then C takes a turn")
	q.mu.Lock()
	require.Zero(t, len(q.ready), "A is in flight, so its remaining job is not offered")
	q.mu.Unlock()

	q.done(wsA)
	require.Equal(t, job(wsA, 'b').body, take().body, "A continues once its delivery finished")
	q.done(wsB)
	q.done(wsC)
	q.done(wsA)

	queued, scheduled := testQueueState(q)
	require.Zero(t, queued)
	require.Zero(t, scheduled)
	require.Equal(t, int64(2), q.dropped.Load(), "A3 and C2")

	tiny := newWebhookQueue(webhookLimits{jobs: 4, bytes: 10, workspaceJobs: 2, workspaceBytes: 10})
	require.False(t, tiny.enqueue(webhookJob{workspaceID: wsA, body: []byte("0123456789x")}), "body bytes are bounded")
	require.Equal(t, int64(1), tiny.dropped.Load())
}

func TestWebhookQueueOneWorkspaceCannotCrowdOutAnother(t *testing.T) {
	small := []byte("{}") // 2 bytes

	fill := func(t *testing.T, q *webhookQueue, ws uuid.UUID, body []byte) (jobs int, bytes int) {
		t.Helper()
		for q.enqueue(webhookJob{workspaceID: ws, body: body}) {
			jobs++
			bytes += len(body)
			require.Less(t, jobs, 1<<16, "enqueue must refuse a full workspace")
		}
		return jobs, bytes
	}

	t.Run("job bound", func(t *testing.T) {
		q := newWebhookQueue(defaultWebhookLimits)
		for range 4 {
			jobs, _ := fill(t, q, uuid.New(), small)
			require.Equal(t, webhookWorkspaceJobs, jobs, "each workspace fills its own share")
		}
		require.True(t, q.enqueue(webhookJob{workspaceID: uuid.New(), body: small}),
			"four full workspaces leave a fifth with room")
	})

	t.Run("byte bound", func(t *testing.T) {
		q := newWebhookQueue(defaultWebhookLimits)
		large := make([]byte, 128<<10)
		jobs, bytes := fill(t, q, uuid.New(), large)
		require.Equal(t, webhookWorkspaceBytes/len(large), jobs)
		require.LessOrEqual(t, bytes, webhookWorkspaceBytes)
		require.True(t, q.enqueue(webhookJob{workspaceID: uuid.New(), body: small}),
			"a workspace at its byte bound leaves another with room")
	})

	t.Run("body counted once", func(t *testing.T) {
		q := newWebhookQueue(defaultWebhookLimits)
		ws := uuid.New()
		hooks := make([]domain.Webhook, 3)
		for i := range hooks {
			hooks[i] = domain.Webhook{ID: uuid.New()}
		}
		require.True(t, q.enqueue(webhookJob{workspaceID: ws, hooks: hooks, body: make([]byte, webhookWorkspaceBytes)}),
			"three hooks share one body")
		require.False(t, q.enqueue(webhookJob{workspaceID: ws, body: []byte("x")}),
			"one more byte exceeds the workspace byte bound")
	})
}

func TestWebhookHungReceiverLeavesPublicationSlotsAndOtherWorkspaces(t *testing.T) {
	f := newWebhookFixture(t)
	hungWS, owner, team := f.workspace()
	hung := newHungReceiver(t)
	f.hook(hungWS, hung.server.URL, WebhookEventIssueUpdated)

	issues := make([]domain.Issue, 30)
	for i := range issues {
		issues[i] = domain.Issue{ID: uuid.New(), WorkspaceID: hungWS, TeamID: team, Identifier: "ENG-1"}
	}
	ctx := domain.WithAccess(context.Background(), domain.Access{UserID: owner, WorkspaceID: hungWS})
	start := time.Now()
	f.svc.IssueEvents(ctx, hungWS, WebhookEventIssueUpdated, issues, owner)
	require.Less(t, time.Since(start), time.Second, "emitting must not wait for deliveries")

	hung.waitArrival(t)

	slotCtx, cancelSlot := context.WithTimeout(context.Background(), time.Second)
	releaseSlot, err := acquirePublicationSlot(slotCtx)
	cancelSlot()
	require.NoError(t, err, "webhook deliveries do not hold publication slots")
	releaseSlot()

	fastWS, fastOwner, fastTeam := f.workspace()
	f.receiver(fastWS, "fast", "fast-secret", true, WebhookEventIssueUpdated)
	fastCtx := domain.WithAccess(context.Background(), domain.Access{UserID: fastOwner, WorkspaceID: fastWS})
	f.svc.IssueEvent(fastCtx, fastWS, WebhookEventIssueUpdated, &domain.Issue{ID: uuid.New(), WorkspaceID: fastWS, TeamID: fastTeam, Identifier: "ENG-2"}, fastOwner)
	require.Equal(t, "fast", f.next().receiver, "another workspace is not starved")

	hung.noArrival(t, 300*time.Millisecond)
	require.Equal(t, int64(1), hung.maxInFlight.Load(), "one delivery per workspace at a time")
}

func TestWebhookDeliveriesNeverExceedTheWorkerCount(t *testing.T) {
	f := newWebhookFixture(t)
	hung := newHungReceiver(t)

	for i := 0; i < webhookWorkers+3; i++ {
		ws, owner, team := f.workspace()
		f.hook(ws, hung.server.URL, WebhookEventIssueUpdated)
		ctx := domain.WithAccess(context.Background(), domain.Access{UserID: owner, WorkspaceID: ws})
		issue := &domain.Issue{ID: uuid.New(), WorkspaceID: ws, TeamID: team, Identifier: "ENG-1"}
		f.svc.IssueEvent(ctx, ws, WebhookEventIssueUpdated, issue, owner)
	}

	for i := 0; i < webhookWorkers; i++ {
		hung.waitArrival(t)
	}
	hung.noArrival(t, 300*time.Millisecond)
	require.Equal(t, int64(webhookWorkers), hung.maxInFlight.Load())

	hung.close()
	for i := 0; i < 3; i++ {
		hung.waitArrival(t)
	}
}

func TestWebhookFullQueueDropsWithoutBlocking(t *testing.T) {
	f := newWebhookFixture(t)
	f.svc.queue = newWebhookQueue(webhookLimits{jobs: webhookQueueJobs, bytes: webhookQueueBytes, workspaceJobs: 3, workspaceBytes: webhookQueueBytes})
	f.svc.workers = 1
	hung := newHungReceiver(t)
	ws, owner, team := f.workspace()
	f.hook(ws, hung.server.URL, WebhookEventIssueUpdated)

	logHook := logtest.NewGlobal()
	defer logrus.StandardLogger().ReplaceHooks(make(logrus.LevelHooks))

	issues := make([]domain.Issue, 20)
	for i := range issues {
		issues[i] = domain.Issue{ID: uuid.New(), WorkspaceID: ws, TeamID: team, Identifier: "ENG-1"}
	}
	ctx := domain.WithAccess(context.Background(), domain.Access{UserID: owner, WorkspaceID: ws})
	start := time.Now()
	f.svc.IssueEvents(ctx, ws, WebhookEventIssueUpdated, issues, owner)
	require.Less(t, time.Since(start), time.Second, "a full queue drops instead of waiting")

	hung.waitArrival(t)

	dropped := f.svc.queue.dropped.Load()
	require.GreaterOrEqual(t, dropped, int64(20-1-3), "the worker may already hold one job")
	require.LessOrEqual(t, dropped, int64(17))

	var full *logrus.Entry
	for _, entry := range logHook.AllEntries() {
		if entry.Message == "webhook deliveries dropped: the delivery queue is full" {
			full = entry
		}
	}
	require.NotNil(t, full, "dropping is logged")
	formatted, err := full.String()
	require.NoError(t, err)
	require.NotContains(t, formatted, hung.server.URL, "the log names no receiver URL")
}

func TestWebhookDeliveryErrorsDoNotLogTheReceiverURL(t *testing.T) {
	f := newWebhookFixture(t)
	ws, owner, team := f.workspace()

	logHook := logtest.NewGlobal()
	defer logrus.StandardLogger().ReplaceHooks(make(logrus.LevelHooks))

	closed := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	closedURL := closed.URL
	closed.Close()
	f.hook(ws, closedURL+"/services/T000/B000/secret-token-one", WebhookEventIssueCreated)
	f.hook(ws, "http://bad host/hooks/secret-token-two", WebhookEventIssueCreated)

	ctx := domain.WithAccess(context.Background(), domain.Access{UserID: owner, WorkspaceID: ws})
	f.svc.IssueEvent(ctx, ws, WebhookEventIssueCreated, &domain.Issue{ID: uuid.New(), WorkspaceID: ws, TeamID: team, Identifier: "ENG-1"}, owner)

	var failed, build *logrus.Entry
	deadline := time.Now().Add(5 * time.Second)
	for {
		for _, entry := range logHook.AllEntries() {
			switch entry.Message {
			case "webhook delivery failed":
				failed = entry
			case "failed to create webhook request":
				build = entry
			}
		}
		if failed != nil && build != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected delivery errors to be logged; failed=%v build=%v", failed != nil, build != nil)
		}
		time.Sleep(20 * time.Millisecond)
	}

	require.Contains(t, failed.Data, "webhook_id")
	require.Contains(t, build.Data, "webhook_id")
	for _, entry := range logHook.AllEntries() {
		formatted, err := entry.String()
		require.NoError(t, err)
		require.NotContains(t, formatted, "secret-token", "logged errors must not carry the receiver URL")
	}
}

func TestWebhookProductionClientRefusesLoopbackReceiver(t *testing.T) {
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
	}))
	defer server.Close()

	req, err := http.NewRequest(http.MethodPost, server.URL, strings.NewReader("{}"))
	require.NoError(t, err)
	resp, err := newWebhookClient(false).Do(req)
	if resp != nil {
		resp.Body.Close()
	}
	require.Error(t, err)
	require.Zero(t, hits.Load())
}

func TestWebhookClientDoesNotFollowRedirects(t *testing.T) {
	var hits atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
	}))
	defer target.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirector.Close()

	req, err := http.NewRequest(http.MethodPost, redirector.URL, strings.NewReader("{}"))
	require.NoError(t, err)
	resp, err := newWebhookClient(true).Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusFound, resp.StatusCode)
	require.Zero(t, hits.Load())
}

func TestWebhookQueuedDeliveryRechecksPrivacyAtSend(t *testing.T) {
	f := newWebhookFixture(t)
	f.svc.queue = newWebhookQueue(defaultWebhookLimits)
	f.svc.workers = 1

	x, ownerX, teamX := f.workspace()
	y, ownerY, teamY := f.workspace()
	hung := newHungReceiver(t)
	f.hook(x, hung.server.URL, WebhookEventIssueUpdated)
	f.receiver(y, "y", "y-secret", true, WebhookEventIssueUpdated)

	ctxX := domain.WithAccess(context.Background(), domain.Access{UserID: ownerX, WorkspaceID: x})
	f.svc.IssueEvent(ctxX, x, WebhookEventIssueUpdated, &domain.Issue{ID: uuid.New(), WorkspaceID: x, TeamID: teamX, Identifier: "ENG-1"}, ownerX)
	hung.waitArrival(t) // the only worker is now busy with X

	ctxY := domain.WithAccess(context.Background(), domain.Access{UserID: ownerY, WorkspaceID: y})
	f.svc.IssueEvent(ctxY, y, WebhookEventIssueUpdated, &domain.Issue{ID: uuid.New(), WorkspaceID: y, TeamID: teamY, Identifier: "ENG-2"}, ownerY)

	_, err := f.db.Exec(`UPDATE teams SET is_private=TRUE WHERE id=$1`, teamX)
	require.ErrorContains(t, err, "Publication in progress", "the in-flight delivery holds X's publication lock")

	_, err = f.db.Exec(`UPDATE teams SET is_private=TRUE WHERE id=$1`, teamY)
	require.NoError(t, err, "nothing of Y is in flight")

	targets, err := f.svc.webhookRepo.ListDeliverable(context.Background(), y, WebhookEventIssueUpdated)
	require.NoError(t, err)
	require.Empty(t, targets, "a privacy-mode workspace has no deliverable webhooks")

	hung.close()

	deadline := time.Now().Add(5 * time.Second)
	for {
		queued, scheduled := testQueueState(f.svc.queue)
		if queued == 0 && scheduled == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("deliveries did not drain: queued=%d scheduled=%d", queued, scheduled)
		}
		time.Sleep(20 * time.Millisecond)
	}
	f.none() // Y's queued delivery was skipped when its worker took it
}

func TestWebhookFullWorkspaceBacklogDoesNotDropAnotherWorkspace(t *testing.T) {
	f := newWebhookFixture(t)

	hungA, hungB := newHungReceiver(t), newHungReceiver(t)
	wsA, ownerA, teamA := f.workspace()
	f.hook(wsA, hungA.server.URL, WebhookEventIssueUpdated)
	wsB, ownerB, teamB := f.workspace()
	f.hook(wsB, hungB.server.URL, WebhookEventIssueUpdated)

	ctxA := domain.WithAccess(context.Background(), domain.Access{UserID: ownerA, WorkspaceID: wsA})
	issuesA := make([]domain.Issue, 2*webhookWorkspaceJobs)
	for i := range issuesA {
		issuesA[i] = domain.Issue{ID: uuid.New(), WorkspaceID: wsA, TeamID: teamA, Identifier: "ENG-A"}
	}
	f.svc.IssueEvents(ctxA, wsA, WebhookEventIssueUpdated, issuesA, ownerA)
	afterA := f.svc.queue.dropped.Load()
	require.GreaterOrEqual(t, afterA, int64(webhookWorkspaceJobs-1), "A hit its job bound")

	// 64 KiB bodies fill B's byte share long before its job share.
	ctxB := domain.WithAccess(context.Background(), domain.Access{UserID: ownerB, WorkspaceID: wsB})
	description := strings.Repeat("b", 64<<10)
	issuesB := make([]domain.Issue, 64)
	for i := range issuesB {
		issuesB[i] = domain.Issue{ID: uuid.New(), WorkspaceID: wsB, TeamID: teamB, Identifier: "ENG-B", Description: &description}
	}
	f.svc.IssueEvents(ctxB, wsB, WebhookEventIssueUpdated, issuesB, ownerB)
	afterB := f.svc.queue.dropped.Load()
	require.Greater(t, afterB, afterA, "B hit its byte bound")
	f.svc.queue.mu.Lock()
	var bytesB int
	if backlog := f.svc.queue.backlog[wsB]; backlog != nil {
		bytesB = backlog.bytes
	}
	f.svc.queue.mu.Unlock()
	require.Greater(t, bytesB, 3*webhookWorkspaceBytes/4, "B's backlog sits near its byte bound")

	hungA.waitArrival(t)
	hungB.waitArrival(t)

	wsC, ownerC, teamC := f.workspace()
	f.receiver(wsC, "c", "c-secret", true, WebhookEventIssueUpdated)
	before := f.svc.queue.dropped.Load()
	ctxC := domain.WithAccess(context.Background(), domain.Access{UserID: ownerC, WorkspaceID: wsC})
	f.svc.IssueEvent(ctxC, wsC, WebhookEventIssueUpdated, &domain.Issue{ID: uuid.New(), WorkspaceID: wsC, TeamID: teamC, Identifier: "ENG-C"}, ownerC)
	require.Equal(t, "c", f.next().receiver, "a full backlog elsewhere does not starve C")
	require.Equal(t, before, f.svc.queue.dropped.Load(), "C's delivery is not dropped")
}

func TestWebhookBreakerPausesAfterThreeFailuresAndRetriesAfterCooldown(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	b := newWebhookBreaker()
	b.now = func() time.Time { return now }
	advance := func(d time.Duration) { now = now.Add(d) }
	id := uuid.New()

	require.False(t, b.record(id, false), "one failure does not pause")
	require.False(t, b.record(id, false), "two failures do not pause")
	allowed, skipped := b.allow(id, 1)
	require.True(t, allowed)
	require.Zero(t, skipped)

	require.True(t, b.record(id, false), "the third consecutive failure pauses")

	for range 5 {
		allowed, skipped = b.allow(id, 1)
		require.False(t, allowed)
		require.Zero(t, skipped, "nothing to log while the pause is fresh")
	}
	allowed, skipped = b.allow(id, 3)
	require.False(t, allowed)
	require.Zero(t, skipped)

	advance(webhookBreakerCooldown - time.Second)
	allowed, skipped = b.allow(id, 0)
	require.False(t, allowed, "still paused just before the cooldown ends")
	require.Zero(t, skipped)

	advance(time.Second)
	allowed, skipped = b.allow(id, 1)
	require.True(t, allowed, "half-open: one attempt is allowed")
	require.Equal(t, 8, skipped, "the deliveries skipped while paused are reported once")
	allowed, skipped = b.allow(id, 1)
	require.True(t, allowed)
	require.Zero(t, skipped)

	require.True(t, b.record(id, false), "a failure while half-open pauses it again at once")
	advance(webhookBreakerCooldown)
	allowed, skipped = b.allow(id, 1)
	require.True(t, allowed)
	require.Zero(t, skipped)
	require.False(t, b.record(id, true), "a success closes the circuit")
	b.mu.Lock()
	_, exists := b.circuits[id]
	b.mu.Unlock()
	require.False(t, exists, "a closed circuit is forgotten")

	require.False(t, b.record(id, false), "two failures after a success do not pause")
	require.False(t, b.record(id, false))
	allowed, _ = b.allow(id, 1)
	require.True(t, allowed)

	// A circuit that failed long ago and never paused is swept.
	stale := uuid.New()
	b.mu.Lock()
	b.circuits[stale] = &webhookCircuit{failures: 1, lastFailure: now.Add(-3 * webhookBreakerCooldown)}
	b.mu.Unlock()
	advance(3 * webhookBreakerCooldown)
	b.record(uuid.New(), false)
	b.mu.Lock()
	_, exists = b.circuits[stale]
	b.mu.Unlock()
	require.False(t, exists, "the sweep forgets old circuits that are not paused")
}

func TestWebhookDeadReceiverIsPausedAndHealthyHookKeepsReceiving(t *testing.T) {
	f := newWebhookFixture(t)
	ws, owner, team := f.workspace()

	dead := newHungReceiver(t)
	deadHook := f.hook(ws, dead.server.URL, WebhookEventIssueUpdated)
	f.backdateHook(deadHook, time.Minute) // first in ListDeliverable's order
	f.receiver(ws, "healthy", "healthy-secret", true, WebhookEventIssueUpdated)

	f.svc.client = newWebhookClient(true)
	f.svc.client.Timeout = 200 * time.Millisecond // a hung receiver is a timeout failure
	fixed := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	f.svc.breaker.now = func() time.Time { return fixed }

	logHook := logtest.NewGlobal()
	defer logrus.StandardLogger().ReplaceHooks(make(logrus.LevelHooks))

	ctx := domain.WithAccess(context.Background(), domain.Access{UserID: owner, WorkspaceID: ws})
	var afterThird time.Time
	for i := 0; i < 10; i++ {
		issue := &domain.Issue{ID: uuid.New(), WorkspaceID: ws, TeamID: team, Identifier: fmt.Sprintf("ENG-%d", i)}
		f.svc.IssueEvent(ctx, ws, WebhookEventIssueUpdated, issue, owner)
		require.Equal(t, "healthy", f.next().receiver)
		if i+1 == webhookBreakerFailures {
			afterThird = time.Now()
		}
	}
	require.Equal(t, int64(webhookBreakerFailures), dead.hits.Load(), "the dead receiver is tried until it is paused")
	// Had the dead hook still been tried, each remaining delivery would have
	// waited for its timeout.
	remaining := time.Duration(10-webhookBreakerFailures) * f.svc.client.Timeout
	require.Less(t, time.Since(afterThird), remaining, "the deliveries after the pause do not wait for the dead receiver's timeout")

	paused := logEntriesWithMessage(logHook, "webhook paused after repeated failed deliveries")
	require.Len(t, paused, 1, "the pause is logged once")
	require.Equal(t, deadHook, paused[0].Data["webhook_id"])
	require.Equal(t, webhookBreakerFailures, paused[0].Data["failures"])
	require.Equal(t, webhookBreakerCooldown.String(), paused[0].Data["paused_for"])
	formatted, err := paused[0].String()
	require.NoError(t, err)
	require.NotContains(t, formatted, dead.server.URL, "the log names no receiver URL")
}

func TestWebhookPausedReceiverRecoversAfterCooldown(t *testing.T) {
	f := newWebhookFixture(t)
	ws, owner, team := f.workspace()

	var failing atomic.Bool
	failing.Store(true)
	var hits atomic.Int64
	flaky := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if failing.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(flaky.Close)
	flakyHook := f.hook(ws, flaky.URL, WebhookEventIssueUpdated)
	f.backdateHook(flakyHook, time.Minute) // first in ListDeliverable's order
	f.receiver(ws, "healthy", "healthy-secret", true, WebhookEventIssueUpdated)

	var clockMu sync.Mutex
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	advance := func(d time.Duration) {
		clockMu.Lock()
		clock = clock.Add(d)
		clockMu.Unlock()
	}
	f.svc.breaker.now = func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		return clock
	}

	logHook := logtest.NewGlobal()
	defer logrus.StandardLogger().ReplaceHooks(make(logrus.LevelHooks))

	ctx := domain.WithAccess(context.Background(), domain.Access{UserID: owner, WorkspaceID: ws})
	emit := func() {
		t.Helper()
		issue := &domain.Issue{ID: uuid.New(), WorkspaceID: ws, TeamID: team, Identifier: "ENG-1"}
		f.svc.IssueEvent(ctx, ws, WebhookEventIssueUpdated, issue, owner)
		// The healthy hook comes after the flaky one, so its delivery means
		// the flaky hook's attempt for this job has finished.
		require.Equal(t, "healthy", f.next().receiver)
	}

	for range webhookBreakerFailures {
		emit()
	}
	require.Equal(t, int64(webhookBreakerFailures), hits.Load(), "three failures pause the flaky hook")

	emit()
	emit()
	require.Equal(t, int64(webhookBreakerFailures), hits.Load(), "a paused hook is not attempted")

	failing.Store(false)
	advance(webhookBreakerCooldown + time.Second)
	emit()
	require.Equal(t, int64(webhookBreakerFailures+1), hits.Load(), "the half-open probe is sent and succeeds")

	resumed := logEntriesWithMessage(logHook, "webhook deliveries dropped while the webhook was paused; trying it again")
	require.Len(t, resumed, 1, "skipped deliveries are logged once")
	require.Equal(t, 2, resumed[0].Data["dropped"])
	require.Equal(t, flakyHook, resumed[0].Data["webhook_id"])

	emit()
	require.Equal(t, int64(webhookBreakerFailures+2), hits.Load(), "the closed circuit sends every delivery again")
}

func TestWebhookBulkEditOfHundredIssuesWithThreeHooksIsNotDropped(t *testing.T) {
	f := newWebhookFixture(t)
	ws, owner, team := f.workspace()
	names := []string{"a", "b", "c"}
	for _, name := range names {
		f.receiver(ws, name, name+"-secret", true, WebhookEventIssueUpdated)
	}

	issues := make([]domain.Issue, 100)
	for i := range issues {
		issues[i] = domain.Issue{ID: uuid.New(), WorkspaceID: ws, TeamID: team, Identifier: fmt.Sprintf("ENG-%d", i)}
	}
	ctx := domain.WithAccess(context.Background(), domain.Access{UserID: owner, WorkspaceID: ws})
	f.svc.IssueEvents(ctx, ws, WebhookEventIssueUpdated, issues, owner)

	counts := map[string]int{}
	for range 100 * len(names) {
		counts[f.next().receiver]++
	}
	for _, name := range names {
		require.Equal(t, 100, counts[name], "each webhook got every issue")
	}
	require.Zero(t, f.svc.queue.dropped.Load(), "an idle queue drops nothing")
}

func TestWebhookQueuePurgesStaleJobsOfAWorkspaceAtOnce(t *testing.T) {
	const tooLong = "webhook deliveries dropped: they waited too long for a worker"
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	q := newWebhookQueue(defaultWebhookLimits)
	q.now = func() time.Time { return now }
	wsA, wsB, wsC, wsD := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	job := func(ws uuid.UUID, marker byte) webhookJob {
		return webhookJob{workspaceID: ws, event: string(marker), body: []byte{marker}}
	}

	logHook := logtest.NewGlobal()
	defer logrus.StandardLogger().ReplaceHooks(make(logrus.LevelHooks))

	for i := 0; i < 5; i++ {
		require.True(t, q.enqueue(job(wsA, byte('a'+i))))
	}
	now = now.Add(6 * time.Minute)
	require.True(t, q.enqueue(job(wsA, 'f')), "A's fresh job fits behind its stale ones")

	fresh, ok := q.take()
	require.True(t, ok)
	require.Equal(t, []byte{'f'}, fresh.body, "A's stale prefix is purged and its fresh job delivered")
	require.Equal(t, int64(5), q.dropped.Load())
	purging := logEntriesWithMessage(logHook, tooLong)
	require.Len(t, purging, 1, "one aggregated line per purge")
	require.Equal(t, 5, purging[0].Data["dropped"])
	queued, scheduled := testQueueState(q)
	require.Zero(t, queued)
	require.Equal(t, 1, scheduled, "A's delivery is in flight")
	q.done(wsA)

	// C's whole backlog goes stale while B's and D's jobs are fresh: the next
	// take skips C in one go instead of offering its jobs one by one.
	for i := 0; i < 3; i++ {
		require.True(t, q.enqueue(job(wsC, byte('h'+i))))
	}
	now = now.Add(6 * time.Minute)
	require.True(t, q.enqueue(job(wsB, 'g')))
	require.True(t, q.enqueue(job(wsD, 'k')))

	next, ok := q.take()
	require.True(t, ok)
	require.Equal(t, []byte{'g'}, next.body, "C is skipped and B's fresh job comes first")
	require.Equal(t, int64(8), q.dropped.Load(), "C's three stale jobs are dropped")
	purging = logEntriesWithMessage(logHook, tooLong)
	require.Len(t, purging, 2)
	require.Equal(t, 3, purging[1].Data["dropped"])
	q.done(wsB)

	next, ok = q.take()
	require.True(t, ok)
	require.Equal(t, []byte{'k'}, next.body, "D's job follows")
	q.done(wsD)

	queued, scheduled = testQueueState(q)
	require.Zero(t, queued)
	require.Zero(t, scheduled)
	q.mu.Lock()
	_, scheduledC := q.scheduled[wsC]
	q.mu.Unlock()
	require.False(t, scheduledC, "a purged workspace is no longer scheduled")
}

func TestWebhookServiceCloseStopsWorkers(t *testing.T) {
	f := newWebhookFixture(t)
	ws, owner, team := f.workspace()
	hung := newHungReceiver(t)
	f.hook(ws, hung.server.URL, WebhookEventIssueUpdated)

	ctx := domain.WithAccess(context.Background(), domain.Access{UserID: owner, WorkspaceID: ws})
	f.svc.IssueEvent(ctx, ws, WebhookEventIssueUpdated, &domain.Issue{ID: uuid.New(), WorkspaceID: ws, TeamID: team, Identifier: "ENG-1"}, owner)
	hung.waitArrival(t)

	closed := make(chan struct{})
	go func() {
		f.svc.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not abort the in-flight delivery")
	}

	// A dispatch after Close is harmless.
	f.svc.IssueEvent(ctx, ws, WebhookEventIssueUpdated, &domain.Issue{ID: uuid.New(), WorkspaceID: ws, TeamID: team, Identifier: "ENG-2"}, owner)
	hung.noArrival(t, 300*time.Millisecond)
}
