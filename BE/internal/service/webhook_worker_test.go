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
	f.svc.limits = webhookLimits{jobs: webhookQueueJobs, bytes: webhookQueueBytes, workspaceJobs: webhookWorkspaceJobs, workspaceBytes: webhookWorkspaceBytes, webhookJobs: 3}
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
	require.Less(t, time.Since(start), time.Second, "a full backlog drops instead of waiting")

	hung.waitArrival(t)
	require.Len(t, f.stored(ws), 3, "the webhook's share is stored")

	full := logEntriesWithMessage(logHook, "webhook deliveries dropped: the delivery queue is full")
	require.Len(t, full, 1, "dropping is logged once per call")
	require.Equal(t, 17, full[0].Data["dropped"])
	formatted, err := full[0].String()
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
	f.eventually(func() bool {
		for _, row := range f.stored(ws) {
			if row.LastError == nil {
				return false
			}
		}
		return true
	}, "the attempts were not recorded")
	codes := map[string]string{}
	for _, row := range f.stored(ws) {
		codes[*row.LastError] = row.Status
	}
	require.Equal(t, map[string]string{"connection_failed": "pending", "invalid_url": "failed"}, codes,
		"a refused connection is retried, an unusable URL is not; the stored error is a code without the URL")
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

	f.eventually(func() bool {
		rows := f.stored(y)
		return len(rows) == 1 && rows[0].Status == "failed"
	}, "Y's stored delivery was not given up")
	require.Equal(t, "workspace_private", *f.stored(y)[0].LastError)
	require.Equal(t, "delivered", f.stored(x)[0].Status)
	f.none() // Y's stored delivery was skipped when its worker took it
}

func TestWebhookFullWorkspaceBacklogDoesNotDropAnotherWorkspace(t *testing.T) {
	f := newWebhookFixture(t)
	f.svc.limits = webhookLimits{jobs: webhookQueueJobs, bytes: webhookQueueBytes, workspaceJobs: 5, workspaceBytes: webhookWorkspaceBytes, webhookJobs: 5}

	hungA := newHungReceiver(t)
	wsA, ownerA, teamA := f.workspace()
	f.hook(wsA, hungA.server.URL, WebhookEventIssueUpdated)
	ctxA := domain.WithAccess(context.Background(), domain.Access{UserID: ownerA, WorkspaceID: wsA})
	issuesA := make([]domain.Issue, 20)
	for i := range issuesA {
		issuesA[i] = domain.Issue{ID: uuid.New(), WorkspaceID: wsA, TeamID: teamA, Identifier: "ENG-A"}
	}
	f.svc.IssueEvents(ctxA, wsA, WebhookEventIssueUpdated, issuesA, ownerA)
	require.Len(t, f.stored(wsA), 5, "A hit its bound")
	hungA.waitArrival(t)

	wsC, ownerC, teamC := f.workspace()
	f.receiver(wsC, "c", "c-secret", true, WebhookEventIssueUpdated)
	ctxC := domain.WithAccess(context.Background(), domain.Access{UserID: ownerC, WorkspaceID: wsC})
	f.svc.IssueEvent(ctxC, wsC, WebhookEventIssueUpdated, &domain.Issue{ID: uuid.New(), WorkspaceID: wsC, TeamID: teamC, Identifier: "ENG-C"}, ownerC)
	require.Equal(t, "c", f.next().receiver, "a full backlog elsewhere does not starve C")
	require.Len(t, f.stored(wsC), 1, "C's delivery is not dropped")
}

// Workspaces take turns: a workspace with a long backlog does not delay
// another workspace's delivery until its backlog is sent.
func TestWebhookWorkspacesTakeTurns(t *testing.T) {
	f := newWebhookFixture(t)
	f.svc.workers = 0
	busy, busyOwner, busyTeam := f.workspace()
	f.receiver(busy, "busy", "busy-secret", true, WebhookEventIssueUpdated)
	for i := range 5 {
		f.emit(busy, busyOwner, busyTeam, f.svc, "ENG-"+string(rune('1'+i)))
	}
	quiet, quietOwner, quietTeam := f.workspace()
	f.receiver(quiet, "quiet", "quiet-secret", true, WebhookEventIssueUpdated)
	f.emit(quiet, quietOwner, quietTeam, f.svc, "ENG-9")

	worker := f.instance()
	worker.workers = 1
	worker.Start()
	var order []string
	for range 6 {
		order = append(order, f.next().receiver)
	}
	require.Contains(t, order[:2], "quiet", "the quiet workspace takes its turn after at most one of the busy one's: %v", order)
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

	attempts := map[int]int{}
	for _, row := range f.stored(ws) {
		if row.WebhookID == deadHook {
			require.Equal(t, "pending", row.Status, "the paused webhook's deliveries are kept")
			attempts[row.Attempts]++
		}
	}
	require.Equal(t, map[int]int{1: webhookBreakerFailures, 0: 10 - webhookBreakerFailures}, attempts)
	var timeouts int
	require.NoError(t, f.db.Get(&timeouts, `SELECT count(*) FROM webhook_deliveries WHERE webhook_id=$1 AND last_error='timeout'`, deadHook))
	require.Equal(t, webhookBreakerFailures, timeouts)
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

	ctx := domain.WithAccess(context.Background(), domain.Access{UserID: owner, WorkspaceID: ws})
	emit := func() {
		t.Helper()
		issue := &domain.Issue{ID: uuid.New(), WorkspaceID: ws, TeamID: team, Identifier: "ENG-1"}
		f.svc.IssueEvent(ctx, ws, WebhookEventIssueUpdated, issue, owner)
		// The healthy hook comes after the flaky one, so its delivery means
		// the flaky hook's attempt for this change has finished.
		require.Equal(t, "healthy", f.next().receiver)
	}

	for range webhookBreakerFailures {
		emit()
	}
	require.Equal(t, int64(webhookBreakerFailures), hits.Load(), "three failures pause the flaky hook")

	emit()
	emit()
	require.Equal(t, int64(webhookBreakerFailures), hits.Load(), "a paused hook is not attempted")

	// The cooldown ends: the deliveries kept during the pause go first, in
	// order, then the new one.
	failing.Store(false)
	_, err := f.db.Exec(`UPDATE webhook_circuits SET paused_until=now()-interval '1 second' WHERE webhook_id=$1`, flakyHook)
	require.NoError(t, err)
	emit()
	require.Equal(t, int64(webhookBreakerFailures+3), hits.Load(), "the kept deliveries and the new one are sent")

	emit()
	require.Equal(t, int64(webhookBreakerFailures+4), hits.Load(), "the closed circuit sends every delivery again")
	var flakyRows []storedDelivery
	for _, row := range f.stored(ws) {
		if row.WebhookID == flakyHook {
			flakyRows = append(flakyRows, row)
		}
	}
	require.Len(t, flakyRows, 7)
	for _, row := range flakyRows[:webhookBreakerFailures] {
		require.Equal(t, "pending", row.Status, "a failed delivery waits for its retry")
	}
	for _, row := range flakyRows[webhookBreakerFailures:] {
		require.Equal(t, "delivered", row.Status)
	}
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
	require.Len(t, f.stored(ws), 300, "nothing was dropped")
	f.eventually(func() bool {
		for _, row := range f.stored(ws) {
			if row.Status != "delivered" {
				return false
			}
		}
		return true
	}, "not every delivery was recorded as delivered")
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

	// A dispatch after Close stores the change for the next instance.
	f.svc.IssueEvent(ctx, ws, WebhookEventIssueUpdated, &domain.Issue{ID: uuid.New(), WorkspaceID: ws, TeamID: team, Identifier: "ENG-2"}, owner)
	hung.noArrival(t, 300*time.Millisecond)
	rows := f.stored(ws)
	require.Len(t, rows, 2)
	for _, row := range rows {
		require.Equal(t, "pending", row.Status, "shutdown loses nothing")
		require.Zero(t, row.Attempts)
	}
}
