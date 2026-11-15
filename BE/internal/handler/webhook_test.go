package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/labstack/echo/v5"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// Real handler, service and SQL: Update validates the request body, and the
// service decides which names a changed event list may carry.
func TestWebhookUpdateValidationPostgres(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	owner, workspace, hook := uuid.New(), uuid.New(), uuid.New()
	_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Webhook owner','hash')`, owner, owner.String()+"@example.test")
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, owner) })
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Webhook handler',$2,$3)`, workspace, workspace.String(), owner)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, workspace) })
	_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner')`, workspace, owner)
	require.NoError(t, err)

	const jwtSecret = "webhook-handler-test-secret-0123456789"
	const storedURL = "https://hooks.example.com/kuayle"
	secret, err := crypto.Encrypt("hook-secret", crypto.DeriveKey(jwtSecret+":webhook"))
	require.NoError(t, err)
	stored := []string{"issue.updated", "cycle.completed"}
	_, err = db.Exec(`INSERT INTO webhooks(id,workspace_id,url,secret,events,is_active) VALUES($1,$2,$3,$4,$5,true)`,
		hook, workspace, storedURL, secret, pq.StringArray(stored))
	require.NoError(t, err)

	svc := service.NewWebhookService(repository.NewWebhookRepository(db), jwtSecret)
	h := NewWebhookHandler(svc)
	ctx := domain.WithAccess(context.Background(), domain.Access{UserID: owner, WorkspaceID: workspace})
	patch := func(t *testing.T, body string) *httptest.ResponseRecorder {
		t.Helper()
		e := echo.New()
		req := httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(body)).WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.Set("workspace", &domain.Workspace{ID: workspace})
		c.SetPathValues(echo.PathValues{{Name: "id", Value: hook.String()}})
		require.NoError(t, h.Update(c))
		return rec
	}
	readEvents := func(t *testing.T) []string {
		t.Helper()
		var events pq.StringArray
		require.NoError(t, db.Get(&events, `SELECT events FROM webhooks WHERE id=$1`, hook))
		return events
	}

	cases := []struct {
		name   string
		body   string
		status int
		code   string
		events []string
	}{
		{"resending the stored legacy list", `{"events":["issue.updated","cycle.completed"]}`, http.StatusOK, "", stored},
		{"an empty event list", `{"events":[]}`, http.StatusBadRequest, "VALIDATION_ERROR", stored},
		{"a changed list with an unknown name", `{"events":["issue.updated","made.up"]}`, http.StatusBadRequest, "BAD_REQUEST", stored},
		{"a changed list that keeps the legacy name", `{"events":["issue.updated","cycle.completed","issue.created"]}`, http.StatusBadRequest, "BAD_REQUEST", stored},
		{"an invalid URL", `{"url":"not a url"}`, http.StatusBadRequest, "VALIDATION_ERROR", stored},
		{"only is_active", `{"is_active":false}`, http.StatusOK, "", stored},
		// Last: it replaces the legacy stored list.
		{"a delivered event list", `{"events":["issue.created"]}`, http.StatusOK, "", []string{"issue.created"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := patch(t, tc.body)
			require.Equal(t, tc.status, rec.Code, rec.Body.String())
			if tc.code != "" {
				var body dto.ErrorResponse
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
				require.Equal(t, tc.code, body.Error.Code, rec.Body.String())
			}
			require.Equal(t, tc.events, readEvents(t))
			var gotURL string
			require.NoError(t, db.Get(&gotURL, `SELECT url FROM webhooks WHERE id=$1`, hook))
			require.Equal(t, storedURL, gotURL)
		})
	}
}

// webhookHandlerFixture is a workspace with an owner, one public team and the
// real handler, service and SQL.
type webhookHandlerFixture struct {
	db                     *sqlx.DB
	h                      *WebhookHandler
	owner, workspace, team uuid.UUID
	ctx                    context.Context
}

func newWebhookHandlerFixture(t *testing.T) *webhookHandlerFixture {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	f := &webhookHandlerFixture{db: db, owner: uuid.New(), workspace: uuid.New(), team: uuid.New()}
	_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Webhook owner','hash')`, f.owner, f.owner.String()+"@example.test")
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, f.owner) })
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Webhook handler',$2,$3)`, f.workspace, f.workspace.String(), f.owner)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, f.workspace) })
	_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner')`, f.workspace, f.owner)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Engineering','ENG')`, f.team, f.workspace)
	require.NoError(t, err)
	svc := service.NewWebhookService(repository.NewWebhookRepository(db), "webhook-handler-test-secret-0123456789")
	t.Cleanup(svc.Close)
	f.h = NewWebhookHandler(svc)
	f.ctx = domain.WithAccess(context.Background(), domain.Access{UserID: f.owner, WorkspaceID: f.workspace})
	return f
}

func (f *webhookHandlerFixture) call(t *testing.T, method, id, body string, handle func(*echo.Context) error) *httptest.ResponseRecorder {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(method, "/", strings.NewReader(body)).WithContext(f.ctx)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("workspace", &domain.Workspace{ID: f.workspace})
	if id != "" {
		c.SetPathValues(echo.PathValues{{Name: "id", Value: id}})
	}
	require.NoError(t, handle(c))
	return rec
}

func webhookErrorBody(t *testing.T, rec *httptest.ResponseRecorder) dto.ErrorBody {
	t.Helper()
	var body dto.ErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body.Error
}

// KUA-198: creating or enabling a webhook in a workspace that has or had a
// private team is refused with a stable code and the reason.
func TestWebhookCreateAndEnableInPrivateWorkspaceAreRefusedPostgres(t *testing.T) {
	f := newWebhookHandlerFixture(t)
	create := `{"url":"https://example.com/kuayle","secret":"long-enough","events":["issue.created"]}`
	rec := f.call(t, http.MethodPost, "", create, f.h.Create)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var created dto.WebhookResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	rec = f.call(t, http.MethodPatch, created.ID, `{"is_active":false}`, f.h.Update)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	_, err := f.db.Exec(`UPDATE teams SET is_private=TRUE WHERE id=$1`, f.team)
	require.NoError(t, err)

	for _, refused := range []*httptest.ResponseRecorder{
		f.call(t, http.MethodPost, "", create, f.h.Create),
		f.call(t, http.MethodPatch, created.ID, `{"is_active":true}`, f.h.Update),
	} {
		require.Equal(t, http.StatusForbidden, refused.Code, refused.Body.String())
		body := webhookErrorBody(t, refused)
		require.Equal(t, "PRIVATE_WORKSPACE_OPERATION_DISABLED", body.Code)
		require.Equal(t, "Webhooks are off because this workspace has a private team", body.Message)
	}
	rec = f.call(t, http.MethodPatch, created.ID, `{"events":["comment.created","comment.updated"]}`, f.h.Update)
	require.Equal(t, http.StatusOK, rec.Code, "editing an inactive webhook still works: %s", rec.Body.String())
	rec = f.call(t, http.MethodDelete, created.ID, ``, f.h.Delete)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

// The delivery log lists a webhook's recent deliveries without their payload,
// and another workspace's webhook is not found.
func TestWebhookDeliveriesEndpointPostgres(t *testing.T) {
	f := newWebhookHandlerFixture(t)
	other := newWebhookHandlerFixture(t)
	secret, err := crypto.Encrypt("hook-secret", crypto.DeriveKey("webhook-handler-test-secret-0123456789:webhook"))
	require.NoError(t, err)
	hook, foreign := uuid.New(), uuid.New()
	_, err = f.db.Exec(`INSERT INTO webhooks(id,workspace_id,url,secret,events) VALUES($1,$2,'https://hooks.example.com/a',$3,ARRAY['issue.updated'])`, hook, f.workspace, secret)
	require.NoError(t, err)
	_, err = f.db.Exec(`INSERT INTO webhooks(id,workspace_id,url,secret,events) VALUES($1,$2,'https://hooks.example.com/b',$3,ARRAY['issue.updated'])`, foreign, other.workspace, secret)
	require.NoError(t, err)
	_, err = f.db.Exec(`INSERT INTO webhook_deliveries(webhook_id,workspace_id,event,body,status,attempts,next_attempt_at,last_status_code,last_error)
		VALUES($1,$2,'issue.updated','{"secret":"payload"}','pending',2,now()+interval '2 minutes',503,'bad_status')`, hook, f.workspace)
	require.NoError(t, err)
	_, err = f.db.Exec(`INSERT INTO webhook_deliveries(webhook_id,workspace_id,event,body,status,next_attempt_at) VALUES($1,$2,'issue.updated','{}','pending',now())`, foreign, other.workspace)
	require.NoError(t, err)

	rec := f.call(t, http.MethodGet, hook.String(), ``, f.h.ListDeliveries)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NotContains(t, rec.Body.String(), "payload", "the log carries no payload")
	var log []dto.WebhookDeliveryResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &log))
	require.Len(t, log, 1)
	got := log[0]
	require.Equal(t, "issue.updated", got.Event)
	require.Equal(t, "pending", got.Status)
	require.Equal(t, 2, got.Attempts)
	require.Equal(t, 503, *got.LastStatusCode)
	require.Equal(t, "bad_status", *got.LastError)
	require.NotNil(t, got.NextAttemptAt)

	rec = f.call(t, http.MethodGet, foreign.String(), ``, f.h.ListDeliveries)
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	rec = f.call(t, http.MethodGet, "not-a-uuid", ``, f.h.ListDeliveries)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}
