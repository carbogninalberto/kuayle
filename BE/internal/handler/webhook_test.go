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
