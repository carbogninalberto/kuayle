package service

import (
	"context"
	"io"
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
	"github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/require"
)

// storedDelivery is a webhook_deliveries row as the tests read it.
type storedDelivery struct {
	ID             uuid.UUID  `db:"id"`
	WebhookID      uuid.UUID  `db:"webhook_id"`
	Event          string     `db:"event"`
	Body           string     `db:"body"`
	Status         string     `db:"status"`
	Attempts       int        `db:"attempts"`
	NextAttemptAt  *time.Time `db:"next_attempt_at"`
	LastStatusCode *int       `db:"last_status_code"`
	LastError      *string    `db:"last_error"`
	UpdatedAt      time.Time  `db:"updated_at"`
	CompletedAt    *time.Time `db:"completed_at"`
}

// stored returns the workspace's deliveries in the order they were queued.
func (f *webhookFixture) stored(ws uuid.UUID) []storedDelivery {
	f.t.Helper()
	var rows []storedDelivery
	require.NoError(f.t, f.db.Select(&rows, `SELECT id,webhook_id,event,body,status,attempts,next_attempt_at,last_status_code,last_error,updated_at,completed_at
		FROM webhook_deliveries WHERE workspace_id=$1 ORDER BY seq`, ws))
	return rows
}

// eventually polls cond until it holds or five seconds pass.
func (f *webhookFixture) eventually(cond func() bool, msg string) {
	f.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			f.t.Fatal(msg)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (f *webhookFixture) emit(ws, owner, team uuid.UUID, svc *WebhookService, identifier string) {
	f.t.Helper()
	ctx := domain.WithAccess(context.Background(), domain.Access{UserID: owner, WorkspaceID: ws})
	svc.IssueEvent(ctx, ws, WebhookEventIssueUpdated, &domain.Issue{ID: uuid.New(), WorkspaceID: ws, TeamID: team, Identifier: identifier}, owner)
}

// scriptedReceiver answers each request with the next status of its script,
// then with the last one, recording what it received.
type scriptedReceiver struct {
	server   *httptest.Server
	mu       sync.Mutex
	script   []int
	requests []receivedDelivery
}

func newScriptedReceiver(t *testing.T, script ...int) *scriptedReceiver {
	t.Helper()
	r := &scriptedReceiver{script: script}
	r.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		r.mu.Lock()
		status := r.script[min(len(r.requests), len(r.script)-1)]
		r.requests = append(r.requests, receivedDelivery{event: req.Header.Get("X-Webhook-Event"), signature: req.Header.Get("X-Webhook-Signature"),
			delivery: req.Header.Get("X-Webhook-Delivery"), body: body})
		r.mu.Unlock()
		w.WriteHeader(status)
	}))
	t.Cleanup(r.server.Close)
	return r
}

func (r *scriptedReceiver) received() []receivedDelivery {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]receivedDelivery(nil), r.requests...)
}

func (r *scriptedReceiver) answer(status int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.script = []int{status}
	r.requests = r.requests[:0:0]
}

func TestWebhookRetryDelaysBackOffThenGiveUp(t *testing.T) {
	want := []time.Duration{30 * time.Second, 2 * time.Minute, 10 * time.Minute, 30 * time.Minute, 2 * time.Hour}
	for i, delay := range want {
		got, retry := webhookRetryDelay(defaultWebhookRetryDelays, i+1)
		require.True(t, retry, "attempt %d is retried", i+1)
		require.Equal(t, delay, got, "after attempt %d", i+1)
	}
	_, retry := webhookRetryDelay(defaultWebhookRetryDelays, len(want)+1)
	require.False(t, retry, "the sixth failed attempt is the last")
}

// KUA-197: a failure is retried with the same body, signature and delivery
// ID until the receiver accepts it, and the finished row keeps no payload.
func TestWebhookFailedDeliveryIsRetriedUntilItSucceeds(t *testing.T) {
	f := newWebhookFixture(t)
	f.svc.retryDelays = []time.Duration{10 * time.Millisecond, 10 * time.Millisecond, 10 * time.Millisecond}
	ws, owner, team := f.workspace()
	receiver := newScriptedReceiver(t, http.StatusInternalServerError, http.StatusBadGateway, http.StatusNoContent)
	hook := f.hook(ws, receiver.server.URL, WebhookEventIssueUpdated)

	f.emit(ws, owner, team, f.svc, "ENG-1")
	f.eventually(func() bool {
		rows := f.stored(ws)
		return len(rows) == 1 && rows[0].Status == "delivered"
	}, "the delivery was not retried until it succeeded")

	got := receiver.received()
	require.Len(t, got, 3)
	row := f.stored(ws)[0]
	for _, request := range got[1:] {
		require.Equal(t, got[0].body, request.body, "a retry sends the stored body")
		require.Equal(t, got[0].signature, request.signature)
		require.Equal(t, got[0].delivery, request.delivery)
	}
	require.Equal(t, "sha256="+crypto.HMACSha256(got[0].body, "hook-secret"), got[0].signature)
	require.Equal(t, row.ID.String(), got[0].delivery, "X-Webhook-Delivery identifies the stored delivery")
	require.Equal(t, hook, row.WebhookID)
	require.Equal(t, 3, row.Attempts)
	require.NotNil(t, row.LastStatusCode)
	require.Equal(t, http.StatusNoContent, *row.LastStatusCode)
	require.Nil(t, row.LastError)
	require.Nil(t, row.NextAttemptAt)
	require.NotNil(t, row.CompletedAt)
	require.Empty(t, row.Body, "a finished delivery keeps no payload")
}

func TestWebhookFailureSchedulesTheNextAttemptWithBackoff(t *testing.T) {
	f := newWebhookFixture(t)
	ws, owner, team := f.workspace()
	receiver := newScriptedReceiver(t, http.StatusServiceUnavailable)
	f.hook(ws, receiver.server.URL, WebhookEventIssueUpdated)

	f.emit(ws, owner, team, f.svc, "ENG-1")
	f.eventually(func() bool {
		rows := f.stored(ws)
		return len(rows) == 1 && rows[0].Attempts == 1
	}, "the first attempt was not recorded")

	row := f.stored(ws)[0]
	require.Equal(t, "pending", row.Status)
	require.Equal(t, http.StatusServiceUnavailable, *row.LastStatusCode)
	require.Equal(t, "bad_status", *row.LastError)
	require.NotNil(t, row.NextAttemptAt)
	require.InDelta(t, (30 * time.Second).Seconds(), row.NextAttemptAt.Sub(row.UpdatedAt).Seconds(), 1)
	require.NotEmpty(t, row.Body, "a pending delivery keeps its payload for the retry")
}

func TestWebhookDeliveryIsMarkedFailedAfterTheLastAttempt(t *testing.T) {
	f := newWebhookFixture(t)
	f.svc.retryDelays = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond, time.Millisecond, time.Millisecond}
	f.svc.breakerThreshold = 100 // isolate retries from the breaker
	ws, owner, team := f.workspace()
	receiver := newScriptedReceiver(t, http.StatusInternalServerError)
	hook := f.hook(ws, receiver.server.URL, WebhookEventIssueUpdated)

	f.emit(ws, owner, team, f.svc, "ENG-1")
	f.eventually(func() bool {
		rows := f.stored(ws)
		return len(rows) == 1 && rows[0].Status == "failed"
	}, "the delivery was not marked failed")

	require.Len(t, receiver.received(), webhookMaxAttempts)
	inA := domain.WithAccess(context.Background(), domain.Access{UserID: owner, WorkspaceID: ws})
	log, err := f.svc.ListDeliveries(inA, hook)
	require.NoError(t, err)
	require.Len(t, log, 1)
	require.Equal(t, "failed", log[0].Status)
	require.Equal(t, webhookMaxAttempts, log[0].Attempts)
	require.Equal(t, http.StatusInternalServerError, *log[0].LastStatusCode)
	require.Equal(t, "bad_status", *log[0].LastError)
	require.Nil(t, log[0].NextAttemptAt)
	require.Equal(t, WebhookEventIssueUpdated, log[0].Event)
}

// KUA-197: a delivery in flight when its instance stops stays pending, without
// a counted attempt, and another instance sends it.
func TestWebhookDeliveriesSurviveARestart(t *testing.T) {
	f := newWebhookFixture(t)
	ws, owner, team := f.workspace()
	var hits atomic.Int64
	arrived := make(chan string, 4)
	done := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Reading the body lets the server notice when the client hangs up.
		_, _ = io.ReadAll(r.Body)
		arrived <- r.Header.Get("X-Webhook-Delivery")
		if hits.Add(1) == 1 {
			select { // hung until the first instance gives up
			case <-r.Context().Done():
			case <-done:
			}
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(done) })
	f.hook(ws, server.URL, WebhookEventIssueUpdated)

	f.emit(ws, owner, team, f.svc, "ENG-1")
	var first string
	select {
	case first = <-arrived:
	case <-time.After(5 * time.Second):
		t.Fatal("the first instance did not send the delivery")
	}
	f.svc.Close() // the instance stops with the request in flight

	rows := f.stored(ws)
	require.Len(t, rows, 1)
	require.Equal(t, "pending", rows[0].Status, "an interrupted delivery is kept")
	require.Zero(t, rows[0].Attempts, "an attempt aborted by shutdown does not count")
	require.Nil(t, rows[0].LastError)

	next := f.instance()
	next.Start()
	select {
	case second := <-arrived:
		require.Equal(t, first, second, "the same delivery is sent again")
	case <-time.After(5 * time.Second):
		t.Fatal("the next instance did not send the stored delivery")
	}
	f.eventually(func() bool { return f.stored(ws)[0].Status == "delivered" }, "the delivery was not finished")
	require.Equal(t, 1, f.stored(ws)[0].Attempts)
}

// KUA-197: with two instances a workspace still has one delivery in flight,
// and another workspace is not starved.
func TestWebhookOneDeliveryInFlightPerWorkspaceAcrossInstances(t *testing.T) {
	f := newWebhookFixture(t)
	other := f.instance()
	f.svc.Start()
	other.Start()

	hung := newHungReceiver(t)
	ws, owner, team := f.workspace()
	f.hook(ws, hung.server.URL, WebhookEventIssueUpdated)
	for i := range 5 {
		instance := f.svc
		if i%2 == 1 {
			instance = other
		}
		f.emit(ws, owner, team, instance, "ENG-1")
	}
	hung.waitArrival(t)
	hung.noArrival(t, 300*time.Millisecond)

	fastWS, fastOwner, fastTeam := f.workspace()
	f.receiver(fastWS, "fast", "fast-secret", true, WebhookEventIssueUpdated)
	f.emit(fastWS, fastOwner, fastTeam, other, "ENG-2")
	require.Equal(t, "fast", f.next().receiver, "another workspace is not starved")
	require.Equal(t, int64(1), hung.maxInFlight.Load(), "one delivery per workspace at a time, across instances")

	hung.close()
	f.eventually(func() bool { return hung.hits.Load() == 5 }, "the workspace's deliveries were not all sent")
	require.Equal(t, int64(1), hung.maxInFlight.Load())
}

// KUA-197: the breaker's state is stored, so a webhook paused by one instance
// is paused for all, its deliveries wait instead of being dropped, and a
// success after the pause resumes it.
func TestWebhookBreakerIsSharedAcrossInstancesAndKeepsDeliveries(t *testing.T) {
	f := newWebhookFixture(t)
	other := f.instance()
	ws, owner, team := f.workspace()
	receiver := newScriptedReceiver(t, http.StatusInternalServerError)
	hook := f.hook(ws, receiver.server.URL, WebhookEventIssueUpdated)

	logHook := logtest.NewGlobal()
	defer logrus.StandardLogger().ReplaceHooks(make(logrus.LevelHooks))

	for range webhookBreakerFailures {
		f.emit(ws, owner, team, f.svc, "ENG-1")
	}
	f.eventually(func() bool { return len(receiver.received()) == webhookBreakerFailures }, "the failing webhook was not tried")
	var pausedUntil *time.Time
	f.eventually(func() bool {
		_ = f.db.Get(&pausedUntil, `SELECT paused_until FROM webhook_circuits WHERE webhook_id=$1`, hook)
		return pausedUntil != nil
	}, "three failures did not pause the webhook")
	require.WithinDuration(t, time.Now().Add(webhookBreakerCooldown), *pausedUntil, 5*time.Second)

	other.Start()
	f.emit(ws, owner, team, other, "ENG-2")
	f.emit(ws, owner, team, other, "ENG-3")
	time.Sleep(300 * time.Millisecond)
	require.Len(t, receiver.received(), webhookBreakerFailures, "no instance sends to a paused webhook")
	rows := f.stored(ws)
	require.Len(t, rows, 5, "deliveries made while paused are kept")
	for _, row := range rows[webhookBreakerFailures:] {
		require.Equal(t, "pending", row.Status)
		require.Zero(t, row.Attempts)
	}

	paused := logEntriesWithMessage(logHook, "webhook paused after repeated failed deliveries")
	require.Len(t, paused, 1, "the pause is logged once")
	require.Equal(t, hook, paused[0].Data["webhook_id"])
	formatted, err := paused[0].String()
	require.NoError(t, err)
	require.NotContains(t, formatted, receiver.server.URL)

	// The cooldown ends and the receiver is back.
	receiver.answer(http.StatusNoContent)
	_, err = f.db.Exec(`UPDATE webhook_circuits SET paused_until=now()-interval '1 second' WHERE webhook_id=$1`, hook)
	require.NoError(t, err)
	f.eventually(func() bool {
		rows := f.stored(ws)
		return rows[3].Status == "delivered" && rows[4].Status == "delivered"
	}, "the deliveries kept during the pause were not sent")
	var failures int
	require.NoError(t, f.db.Get(&failures, `SELECT COALESCE((SELECT consecutive_failures FROM webhook_circuits WHERE webhook_id=$1),0)`, hook))
	require.Zero(t, failures, "a success closes the circuit")
	for _, row := range f.stored(ws)[:webhookBreakerFailures] {
		require.Equal(t, "pending", row.Status, "the failed ones wait for their retry")
		require.Equal(t, 1, row.Attempts)
	}
}

// KUA-197: the bounds are counts in the store, so they hold whichever
// instance emits, and one full workspace or webhook does not crowd out
// another.
func TestWebhookBacklogBoundsAreCountedInTheStore(t *testing.T) {
	f := newWebhookFixture(t)
	other := f.instance()
	limits := webhookLimits{jobs: 1 << 20, bytes: 1 << 30, workspaceJobs: 3, workspaceBytes: 1 << 20, webhookJobs: 2}
	for _, svc := range []*WebhookService{f.svc, other} {
		svc.limits = limits
		svc.workers = 0 // nothing is sent: the rows stay pending
	}
	logHook := logtest.NewGlobal()
	defer logrus.StandardLogger().ReplaceHooks(make(logrus.LevelHooks))

	ws, owner, team := f.workspace()
	first := f.hook(ws, "https://first.example.com/secret-token-one", WebhookEventIssueUpdated)
	f.backdateHook(first, time.Minute)
	second := f.hook(ws, "https://second.example.com/hook", WebhookEventIssueUpdated)
	ctx := domain.WithAccess(context.Background(), domain.Access{UserID: owner, WorkspaceID: ws})
	issues := []domain.Issue{{ID: uuid.New(), TeamID: team, Identifier: "ENG-1"}, {ID: uuid.New(), TeamID: team, Identifier: "ENG-2"}}
	f.svc.IssueEvents(ctx, ws, WebhookEventIssueUpdated, issues, owner)
	rows := f.stored(ws)
	require.Len(t, rows, 3, "the workspace holds three")
	require.Equal(t, []uuid.UUID{first, second, first}, []uuid.UUID{rows[0].WebhookID, rows[1].WebhookID, rows[2].WebhookID})

	other.IssueEvent(ctx, ws, WebhookEventIssueUpdated, &issues[0], owner)
	require.Len(t, f.stored(ws), 3, "another instance sees the same backlog")

	dropped := logEntriesWithMessage(logHook, "webhook deliveries dropped: the delivery queue is full")
	require.Len(t, dropped, 2, "one line per emitting call")
	require.Equal(t, 1, dropped[0].Data["dropped"])
	require.Equal(t, 2, dropped[1].Data["dropped"])
	for _, entry := range dropped {
		formatted, err := entry.String()
		require.NoError(t, err)
		require.NotContains(t, formatted, "secret-token")
	}

	// A second workspace is not crowded out by the first.
	ws2, owner2, team2 := f.workspace()
	f.hook(ws2, "https://third.example.com/hook", WebhookEventIssueUpdated)
	f.emit(ws2, owner2, team2, other, "ENG-9")
	require.Len(t, f.stored(ws2), 1)
	// A store holding the bound for all workspaces takes no more. Other
	// packages' tests may hold pending rows too, so the bound is what the
	// store holds now.
	var pending int
	require.NoError(t, f.db.Get(&pending, `SELECT count(*) FROM webhook_deliveries WHERE status='pending'`))
	f.svc.limits.jobs = pending
	ws3, owner3, team3 := f.workspace()
	f.hook(ws3, "https://fourth.example.com/hook", WebhookEventIssueUpdated)
	f.emit(ws3, owner3, team3, f.svc, "ENG-10")
	require.Empty(t, f.stored(ws3), "the global bound holds")

	// One webhook's share leaves room for the workspace's other webhook.
	ws4, owner4, team4 := f.workspace()
	dead := f.hook(ws4, "https://dead.example.com/hook", WebhookEventIssueUpdated)
	f.backdateHook(dead, time.Minute)
	f.hook(ws4, "https://alive.example.com/hook", WebhookEventIssueCreated)
	for _, svc := range []*WebhookService{f.svc, other} {
		svc.limits = webhookLimits{jobs: 1 << 20, bytes: 1 << 30, workspaceJobs: 10, workspaceBytes: 1 << 20, webhookJobs: 2}
	}
	for i := range 3 {
		f.emit(ws4, owner4, team4, f.svc, "ENG-"+string(rune('a'+i)))
	}
	ctx4 := domain.WithAccess(context.Background(), domain.Access{UserID: owner4, WorkspaceID: ws4})
	f.svc.IssueEvent(ctx4, ws4, WebhookEventIssueCreated, &domain.Issue{ID: uuid.New(), TeamID: team4, Identifier: "ENG-1"}, owner4)
	byHook := map[uuid.UUID]int{}
	for _, row := range f.stored(ws4) {
		byHook[row.WebhookID]++
	}
	require.Equal(t, 2, byHook[dead], "a webhook holds at most its share")
	require.Len(t, f.stored(ws4), 3, "the other webhook still gets its delivery")

	// Payload bytes are bounded too.
	ws5, owner5, team5 := f.workspace()
	f.hook(ws5, "https://bytes.example.com/hook", WebhookEventIssueUpdated)
	description := strings.Repeat("b", 400)
	ctx5 := domain.WithAccess(context.Background(), domain.Access{UserID: owner5, WorkspaceID: ws5})
	f.svc.IssueEvent(ctx5, ws5, WebhookEventIssueUpdated, &domain.Issue{ID: uuid.New(), TeamID: team5, Identifier: "ENG-1", Description: &description}, owner5)
	require.Len(t, f.stored(ws5), 1)
	size := len(f.stored(ws5)[0].Body)
	f.svc.limits = webhookLimits{jobs: 1 << 20, bytes: 1 << 30, workspaceJobs: 10, workspaceBytes: size + size/2, webhookJobs: 10}
	f.svc.IssueEvent(ctx5, ws5, WebhookEventIssueUpdated, &domain.Issue{ID: uuid.New(), TeamID: team5, Identifier: "ENG-2", Description: &description}, owner5)
	require.Len(t, f.stored(ws5), 1, "the second body exceeds the workspace's byte bound")
}

// Deliveries that waited longer than the maximum age are given up together
// when their workspace next takes a turn, and logged once.
func TestWebhookStaleDeliveriesExpireTogether(t *testing.T) {
	f := newWebhookFixture(t)
	f.svc.workers = 0
	ws, owner, team := f.workspace()
	f.receiver(ws, "hook", "hook-secret", true, WebhookEventIssueUpdated)
	for i := range 3 {
		f.emit(ws, owner, team, f.svc, "ENG-"+string(rune('1'+i)))
	}
	_, err := f.db.Exec(`UPDATE webhook_deliveries SET created_at=now()-interval '25 hours' WHERE id IN (SELECT id FROM webhook_deliveries WHERE workspace_id=$1 ORDER BY seq LIMIT 2)`, ws)
	require.NoError(t, err)

	logHook := logtest.NewGlobal()
	defer logrus.StandardLogger().ReplaceHooks(make(logrus.LevelHooks))

	next := f.instance()
	next.Start()
	f.next()
	f.none()
	rows := f.stored(ws)
	require.Equal(t, []string{"failed", "failed", "delivered"}, []string{rows[0].Status, rows[1].Status, rows[2].Status})
	require.Equal(t, "expired", *rows[0].LastError)
	require.Zero(t, rows[0].Attempts)
	require.Empty(t, rows[0].Body)
	expired := logEntriesWithMessage(logHook, "webhook deliveries dropped: they waited too long")
	require.Len(t, expired, 1, "one aggregated line")
	require.Equal(t, int64(2), expired[0].Data["dropped"])
}

func TestWebhookDeliveryRetentionPurgesOldFinishedRows(t *testing.T) {
	f := newWebhookFixture(t)
	ws, _, _ := f.workspace()
	hook := f.hook(ws, "https://example.com/hook", WebhookEventIssueUpdated)
	insert := func(status string, age time.Duration) uuid.UUID {
		t.Helper()
		var id uuid.UUID
		completed := "now()-$4::interval"
		next := "NULL"
		if status == "pending" {
			completed, next = "NULL", "now()"
		}
		require.NoError(t, f.db.Get(&id, `INSERT INTO webhook_deliveries(webhook_id,workspace_id,event,body,status,next_attempt_at,completed_at,created_at)
			VALUES($1,$2,'issue.updated','',$3,`+next+`,`+completed+`,now()-$4::interval) RETURNING id`, hook, ws, status, age.String()))
		return id
	}
	oldDelivered := insert("delivered", 8*24*time.Hour)
	oldFailed := insert("failed", 8*24*time.Hour)
	recent := insert("delivered", 24*time.Hour)
	oldPending := insert("pending", 8*24*time.Hour)

	purged, err := f.svc.PurgeDeliveries(context.Background())
	require.NoError(t, err)
	require.GreaterOrEqual(t, purged, 2)
	var kept []uuid.UUID
	require.NoError(t, f.db.Select(&kept, `SELECT id FROM webhook_deliveries WHERE workspace_id=$1`, ws))
	require.ElementsMatch(t, []uuid.UUID{recent, oldPending}, kept)
	require.NotContains(t, kept, oldDelivered)
	require.NotContains(t, kept, oldFailed)
}

func TestWebhookDeliveryLogIsScopedToTheWorkspace(t *testing.T) {
	f := newWebhookFixture(t)
	f.svc.workers = 0
	wsA, ownerA, teamA := f.workspace()
	wsB, _, _ := f.workspace()
	hookA := f.hook(wsA, "https://a.example.com/hook", WebhookEventIssueUpdated)
	hookB := f.hook(wsB, "https://b.example.com/hook", WebhookEventIssueUpdated)
	f.emit(wsA, ownerA, teamA, f.svc, "ENG-1")
	f.emit(wsA, ownerA, teamA, f.svc, "ENG-2")

	inA := domain.WithAccess(context.Background(), domain.Access{UserID: ownerA, WorkspaceID: wsA})
	log, err := f.svc.ListDeliveries(inA, hookA)
	require.NoError(t, err)
	require.Len(t, log, 2)
	rows := f.stored(wsA)
	require.Equal(t, rows[1].ID, log[0].ID, "newest first")
	require.Equal(t, "pending", log[0].Status)
	require.NotNil(t, log[0].NextAttemptAt)

	_, err = f.svc.ListDeliveries(inA, hookB)
	require.ErrorIs(t, err, ErrWebhookNotFound, "another workspace's webhook is not found")
}

// A workspace that turns private after its deliveries were stored sends none
// of them: they are given up when its workers take them.
func TestWebhookStoredDeliveriesAreDroppedWhenTheWorkspaceTurnsPrivate(t *testing.T) {
	f := newWebhookFixture(t)
	f.svc.workers = 0
	ws, owner, team := f.workspace()
	f.receiver(ws, "hook", "hook-secret", true, WebhookEventIssueUpdated)
	f.emit(ws, owner, team, f.svc, "ENG-1")
	f.emit(ws, owner, team, f.svc, "ENG-2")
	_, err := f.db.Exec(`UPDATE teams SET is_private=TRUE WHERE id=$1`, team)
	require.NoError(t, err)

	f.instance().Start()
	f.eventually(func() bool {
		for _, row := range f.stored(ws) {
			if row.Status != "failed" {
				return false
			}
		}
		return true
	}, "the stored deliveries were not given up")
	f.none()
	for _, row := range f.stored(ws) {
		require.Equal(t, "workspace_private", *row.LastError)
		require.Empty(t, row.Body, "no payload of the workspace is kept")
		require.Zero(t, row.Attempts)
	}
}

// A webhook disabled after a delivery was stored does not receive it.
func TestWebhookStoredDeliveryToADisabledWebhookIsDropped(t *testing.T) {
	f := newWebhookFixture(t)
	f.svc.workers = 0
	ws, owner, team := f.workspace()
	hook := f.receiver(ws, "hook", "hook-secret", true, WebhookEventIssueUpdated)
	f.emit(ws, owner, team, f.svc, "ENG-1")
	_, err := f.db.Exec(`UPDATE webhooks SET is_active=FALSE WHERE id=$1`, hook)
	require.NoError(t, err)

	f.instance().Start()
	f.eventually(func() bool { return f.stored(ws)[0].Status == "failed" }, "the delivery was not given up")
	f.none()
	require.Equal(t, "webhook_disabled", *f.stored(ws)[0].LastError)
}

func TestWebhookEmitStoresDeliveriesWithoutRunningWorkers(t *testing.T) {
	f := newWebhookFixture(t)
	f.svc.workers = 0
	ws, owner, team := f.workspace()
	f.receiver(ws, "a", "a-secret", true, WebhookEventIssueUpdated)
	f.receiver(ws, "b", "b-secret", true, WebhookEventIssueUpdated)
	start := time.Now()
	f.emit(ws, owner, team, f.svc, "ENG-1")
	require.Less(t, time.Since(start), time.Second)
	rows := f.stored(ws)
	require.Len(t, rows, 2, "one delivery per subscribed webhook")
	require.Equal(t, rows[0].Body, rows[1].Body, "both carry the same body")
	for _, row := range rows {
		require.Equal(t, "pending", row.Status)
		require.Equal(t, WebhookEventIssueUpdated, row.Event)
	}
	var lanes int
	require.NoError(t, f.db.Get(&lanes, `SELECT count(*) FROM webhook_lanes WHERE workspace_id=$1`, ws))
	require.Equal(t, 1, lanes)
}
