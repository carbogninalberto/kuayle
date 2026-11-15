package service

import (
	"context"
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

// hungReceiver is a receiver that answers only when the test releases it,
// recording how many requests were in flight at once.
type hungReceiver struct {
	server      *httptest.Server
	arrived     chan struct{}
	release     chan struct{}
	releaseOnce sync.Once
	inFlight    atomic.Int64
	maxInFlight atomic.Int64
}

func newHungReceiver(t *testing.T) *hungReceiver {
	t.Helper()
	h := &hungReceiver{arrived: make(chan struct{}, 64), release: make(chan struct{})}
	h.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	return q.queued, len(q.scheduled)
}

func TestWebhookQueueTakesWorkspacesInTurnsAndBoundsBacklog(t *testing.T) {
	wsA, wsB, wsC := uuid.New(), uuid.New(), uuid.New()
	q := newWebhookQueue(4, 2, 1<<20)
	job := func(ws uuid.UUID, marker byte) webhookJob {
		return webhookJob{workspaceID: ws, body: []byte{marker}}
	}

	require.True(t, q.enqueue(job(wsA, 'a')), "A1")
	require.True(t, q.enqueue(job(wsA, 'b')), "A2")
	require.False(t, q.enqueue(job(wsA, 'c')), "A3: the workspace backlog is full")
	require.True(t, q.enqueue(job(wsB, 'd')), "B1")
	require.True(t, q.enqueue(job(wsC, 'e')), "C1")
	require.False(t, q.enqueue(job(wsC, 'f')), "C2: the queue is full")

	require.Equal(t, job(wsA, 'a').body, q.take().body, "A first")
	require.Equal(t, job(wsB, 'd').body, q.take().body, "then B takes a turn")
	require.Equal(t, job(wsC, 'e').body, q.take().body, "then C takes a turn")
	require.Zero(t, len(q.ready), "A is in flight, so its remaining job is not offered")

	q.done(wsA)
	require.Equal(t, job(wsA, 'b').body, q.take().body, "A continues once its delivery finished")
	q.done(wsB)
	q.done(wsC)
	q.done(wsA)

	queued, scheduled := testQueueState(q)
	require.Zero(t, queued)
	require.Zero(t, scheduled)
	require.Equal(t, int64(2), q.dropped.Load(), "A3 and C2")

	tiny := newWebhookQueue(4, 2, 10)
	require.False(t, tiny.enqueue(webhookJob{workspaceID: wsA, body: []byte("0123456789x")}), "body bytes are bounded")
	require.Equal(t, int64(1), tiny.dropped.Load())
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
	f.svc.queue = newWebhookQueue(webhookQueueLimit, 3, webhookQueueBytes)
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
	f.svc.queue = newWebhookQueue(webhookQueueLimit, webhookWorkspaceBacklog, webhookQueueBytes)
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
