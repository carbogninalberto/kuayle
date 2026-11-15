package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

const webhookTestJWTSecret = "webhook-delivery-test-secret-0123456789"

type receivedDelivery struct {
	receiver  string
	event     string
	signature string
	body      []byte
}

type webhookFixture struct {
	t        *testing.T
	db       *sqlx.DB
	svc      *WebhookService
	received chan receivedDelivery
}

func newWebhookFixture(t *testing.T) *webhookFixture {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	svc := NewWebhookService(repository.NewWebhookRepository(db), webhookTestJWTSecret)
	svc.client = newWebhookClient(true) // loopback receivers
	return &webhookFixture{t: t, db: db, svc: svc, received: make(chan receivedDelivery, 32)}
}

// workspace creates a workspace owned by a new user with one public team.
func (f *webhookFixture) workspace() (ws, owner, team uuid.UUID) {
	f.t.Helper()
	ws, owner, team = uuid.New(), uuid.New(), uuid.New()
	_, err := f.db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Webhook owner','hash')`, owner, owner.String()+"@example.test")
	require.NoError(f.t, err)
	f.t.Cleanup(func() { _, _ = f.db.Exec(`DELETE FROM users WHERE id=$1`, owner) })
	_, err = f.db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Webhook delivery',$2,$3)`, ws, ws.String(), owner)
	require.NoError(f.t, err)
	f.t.Cleanup(func() { _, _ = f.db.Exec(`DELETE FROM workspaces WHERE id=$1`, ws) })
	_, err = f.db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner')`, ws, owner)
	require.NoError(f.t, err)
	_, err = f.db.Exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Engineering','ENG')`, team, ws)
	require.NoError(f.t, err)
	return ws, owner, team
}

// receiver starts a loopback endpoint and registers it as a webhook.
func (f *webhookFixture) receiver(ws uuid.UUID, name, secret string, active bool, events ...string) uuid.UUID {
	f.t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.received <- receivedDelivery{receiver: name, event: r.Header.Get("X-Webhook-Event"), signature: r.Header.Get("X-Webhook-Signature"), body: body}
		w.WriteHeader(http.StatusNoContent)
	}))
	f.t.Cleanup(server.Close)
	encrypted, err := crypto.Encrypt(secret, crypto.DeriveKey(webhookTestJWTSecret+":webhook"))
	require.NoError(f.t, err)
	id := uuid.New()
	_, err = f.db.Exec(`INSERT INTO webhooks(id,workspace_id,url,secret,events,is_active) VALUES($1,$2,$3,$4,$5,$6)`, id, ws, server.URL, encrypted, pq.StringArray(events), active)
	require.NoError(f.t, err)
	return id
}

func (f *webhookFixture) next() receivedDelivery {
	f.t.Helper()
	select {
	case d := <-f.received:
		return d
	case <-time.After(5 * time.Second):
		f.t.Fatal("webhook was not delivered")
		return receivedDelivery{}
	}
}

func (f *webhookFixture) none() {
	f.t.Helper()
	select {
	case d := <-f.received:
		f.t.Fatalf("unexpected delivery to %s: %s", d.receiver, d.body)
	case <-time.After(300 * time.Millisecond):
	}
}

type deliveryEnvelope struct {
	Event     string          `json:"event"`
	Payload   json.RawMessage `json:"payload"`
	Timestamp string          `json:"timestamp"`
}

func TestWebhookIssueEventOutlivesTheRequest(t *testing.T) {
	f := newWebhookFixture(t)
	ws, owner, team := f.workspace()
	f.receiver(ws, "subscribed", "subscribed-secret", true, WebhookEventIssueUpdated)
	f.receiver(ws, "inactive", "inactive-secret", false, WebhookEventIssueUpdated)
	f.receiver(ws, "other-event", "other-secret", true, WebhookEventIssueCreated, "cycle.completed")

	issue := &domain.Issue{ID: uuid.New(), WorkspaceID: ws, TeamID: team, Number: 7, Identifier: "ENG-7", Title: "Ship webhooks"}
	request, cancel := context.WithCancel(domain.WithAccess(context.Background(), domain.Access{UserID: owner, WorkspaceID: ws}))
	f.svc.IssueEvent(request, ws, WebhookEventIssueUpdated, issue, owner)
	cancel() // the response was written
	issue.Title = "Changed after the call"

	got := f.next()
	require.Equal(t, "subscribed", got.receiver)
	require.Equal(t, WebhookEventIssueUpdated, got.event)
	require.Equal(t, "sha256="+crypto.HMACSha256(got.body, "subscribed-secret"), got.signature)

	var envelope deliveryEnvelope
	require.NoError(t, json.Unmarshal(got.body, &envelope))
	require.Equal(t, WebhookEventIssueUpdated, envelope.Event)
	_, err := time.Parse(time.RFC3339, envelope.Timestamp)
	require.NoError(t, err)
	var payload struct {
		Issue   domain.Issue `json:"issue"`
		ActorID *uuid.UUID   `json:"actor_id"`
	}
	require.NoError(t, json.Unmarshal(envelope.Payload, &payload))
	require.Equal(t, "ENG-7", payload.Issue.Identifier)
	require.Equal(t, "Ship webhooks", payload.Issue.Title, "the body is fixed when the event is emitted")
	require.Equal(t, team, payload.Issue.TeamID)
	require.NotNil(t, payload.ActorID)
	require.Equal(t, owner, *payload.ActorID)

	f.none() // neither the inactive nor the unsubscribed webhook
}

func TestWebhookAutomationEventsAreDelivered(t *testing.T) {
	f := newWebhookFixture(t)
	ws, _, team := f.workspace()
	f.receiver(ws, "hook", "hook-secret", true, WebhookEventIssueTriaged)

	automation := domain.WithAccess(context.Background(), domain.Access{UserID: uuid.Nil, WorkspaceID: ws})
	f.svc.IssueEvent(automation, ws, WebhookEventIssueTriaged, &domain.Issue{ID: uuid.New(), TeamID: team, Identifier: "ENG-1"}, uuid.Nil)

	got := f.next()
	var envelope deliveryEnvelope
	require.NoError(t, json.Unmarshal(got.body, &envelope))
	var payload map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(envelope.Payload, &payload))
	require.JSONEq(t, `null`, string(payload["actor_id"]))
}

func TestWebhookCommentCreatedPayload(t *testing.T) {
	f := newWebhookFixture(t)
	ws, owner, team := f.workspace()
	f.receiver(ws, "hook", "hook-secret", true, WebhookEventCommentCreated)

	issue := &domain.Issue{ID: uuid.New(), WorkspaceID: ws, TeamID: team, Identifier: "ENG-3", Title: "Not sent"}
	comment := &domain.Comment{ID: uuid.New(), IssueID: issue.ID, UserID: owner, Body: "Looks good"}
	f.svc.CommentCreated(domain.WithAccess(context.Background(), domain.Access{UserID: owner, WorkspaceID: ws}), ws, issue, comment, owner)

	got := f.next()
	require.Equal(t, WebhookEventCommentCreated, got.event)
	var envelope deliveryEnvelope
	require.NoError(t, json.Unmarshal(got.body, &envelope))
	require.Equal(t, WebhookEventCommentCreated, envelope.Event)
	var payload struct {
		Comment domain.Comment `json:"comment"`
		Issue   map[string]any `json:"issue"`
		ActorID *uuid.UUID     `json:"actor_id"`
	}
	require.NoError(t, json.Unmarshal(envelope.Payload, &payload))
	require.Equal(t, comment.ID, payload.Comment.ID)
	require.Equal(t, "Looks good", payload.Comment.Body)
	require.Equal(t, map[string]any{"id": issue.ID.String(), "identifier": "ENG-3", "team_id": team.String()}, payload.Issue)
	require.NotNil(t, payload.ActorID)
	require.Equal(t, owner, *payload.ActorID)
}

func TestWebhookPrivacyWorkspaceReceivesNothing(t *testing.T) {
	f := newWebhookFixture(t)
	ws, owner, team := f.workspace()
	f.receiver(ws, "hook", "hook-secret", true, WebhookEvents...)
	_, err := f.db.Exec(`UPDATE teams SET is_private=TRUE WHERE id=$1`, team)
	require.NoError(t, err)

	ctx := domain.WithAccess(context.Background(), domain.Access{UserID: owner, WorkspaceID: ws})
	issue := &domain.Issue{ID: uuid.New(), TeamID: team, Identifier: "ENG-1"}
	f.svc.IssueEvent(ctx, ws, WebhookEventIssueCreated, issue, owner)
	f.svc.CommentCreated(ctx, ws, issue, &domain.Comment{ID: uuid.New(), IssueID: issue.ID, Body: "secret"}, owner)
	f.svc.IssueEvent(context.Background(), ws, WebhookEventIssueDeleted, issue, uuid.Nil)
	f.none()

	require.Zero(t, f.svc.queue.dropped.Load(), "the precheck queued nothing")
	f.svc.queue.mu.Lock()
	require.Zero(t, f.svc.queue.queued)
	f.svc.queue.mu.Unlock()
}

func TestWebhookIgnoresUnknownAndMisroutedEvents(t *testing.T) {
	f := newWebhookFixture(t)
	ws, owner, team := f.workspace()
	f.receiver(ws, "hook", "hook-secret", true, append([]string{"cycle.completed"}, WebhookEvents...)...)
	ctx := domain.WithAccess(context.Background(), domain.Access{UserID: owner, WorkspaceID: ws})
	issue := &domain.Issue{ID: uuid.New(), TeamID: team, Identifier: "ENG-1"}
	f.svc.IssueEvent(ctx, ws, "cycle.completed", issue, owner)
	f.svc.IssueEvent(ctx, ws, WebhookEventCommentCreated, issue, owner)
	f.svc.IssueEvent(ctx, ws, WebhookEventIssueCreated, nil, owner)
	var nilService *WebhookService
	nilService.IssueEvent(ctx, ws, WebhookEventIssueCreated, issue, owner)
	nilService.CommentCreated(ctx, ws, issue, &domain.Comment{}, owner)
	f.none()
}

func TestWebhookManagementIsScopedAndValidatesEvents(t *testing.T) {
	f := newWebhookFixture(t)
	wsA, owner, _ := f.workspace()
	wsB, _, _ := f.workspace()
	// owner manages both workspaces.
	_, err := f.db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'admin')`, wsB, owner)
	require.NoError(t, err)
	hookB := f.receiver(wsB, "b", "b-secret", true, WebhookEventIssueCreated)
	legacy := f.receiver(wsA, "legacy", "legacy-secret", true, "project.updated")

	inA := domain.WithAccess(context.Background(), domain.Access{UserID: owner, WorkspaceID: wsA})
	_, err = f.svc.Update(inA, hookB, dto.UpdateWebhookRequest{IsActive: new(bool)})
	require.Error(t, err, "a request scoped to workspace A cannot change B's webhook")
	require.Error(t, f.svc.Delete(inA, hookB))
	var active bool
	require.NoError(t, f.db.Get(&active, `SELECT is_active FROM webhooks WHERE id=$1`, hookB))
	require.True(t, active)

	_, err = f.svc.Create(inA, wsA, dto.CreateWebhookRequest{URL: "https://example.com/hook", Secret: "long-enough", Events: []string{"issue.created", "project.updated"}})
	require.ErrorContains(t, err, `unknown webhook event "project.updated"`)
	require.ErrorContains(t, err, "issue.created, issue.updated, issue.deleted, issue.triaged, comment.created")
	_, err = f.svc.Update(inA, legacy, dto.UpdateWebhookRequest{Events: []string{"cycle.completed"}})
	require.ErrorContains(t, err, "unknown webhook event")

	// A stored legacy name keeps the webhook editable, also when a client
	// sends the stored list back unchanged; a changed list is validated.
	inactive := false
	updated, err := f.svc.Update(inA, legacy, dto.UpdateWebhookRequest{IsActive: &inactive})
	require.NoError(t, err)
	require.Equal(t, []string{"project.updated"}, []string(updated.Events))
	updated, err = f.svc.Update(inA, legacy, dto.UpdateWebhookRequest{Events: []string{"project.updated"}, IsActive: &inactive})
	require.NoError(t, err)
	require.Equal(t, []string{"project.updated"}, []string(updated.Events))
	_, err = f.svc.Update(inA, legacy, dto.UpdateWebhookRequest{Events: []string{"project.updated", WebhookEventIssueCreated}})
	require.ErrorContains(t, err, `unknown webhook event "project.updated"`)
}
