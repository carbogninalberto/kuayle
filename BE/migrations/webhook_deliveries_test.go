package migrations_test

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// KUA-197: existing webhooks keep working after the upgrade, a delivery is
// pending exactly while it has a next attempt, deliveries and breaker state go
// with their webhook, and rolling back removes only the new tables.
func TestWebhookDeliveriesMigration(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", url)
	require.NoError(t, err)
	name := "kuayle_webhook_deliveries_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = admin.Exec(`CREATE DATABASE "` + name + `"`)
	require.NoError(t, err)
	t.Cleanup(func() {
		admin.ExecContext(context.Background(), `DROP DATABASE "`+name+`" WITH (FORCE)`)
		admin.Close()
	})
	target := databaseURLWithName(t, url, name)
	m, err := migrate.New("file://.", target)
	require.NoError(t, err)
	t.Cleanup(func() { m.Close() })
	require.NoError(t, m.Migrate(166))
	db, err := sql.Open("pgx", target)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	user, workspace, hook := uuid.New(), uuid.New(), uuid.New()
	_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,'hooks@example.test','Hooks','x')`, user)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Hooks','hooks',$2)`, workspace, user)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO webhooks(id,workspace_id,url,secret,events) VALUES($1,$2,'https://example.com/hook','enc',ARRAY['issue.created'])`, hook, workspace)
	require.NoError(t, err)

	require.NoError(t, m.Migrate(167))

	var events []byte
	require.NoError(t, db.QueryRow(`SELECT events::text FROM webhooks WHERE id=$1`, hook).Scan(&events))
	require.Equal(t, "{issue.created}", string(events), "existing webhooks are unchanged")

	insert := `INSERT INTO webhook_deliveries(webhook_id,workspace_id,event,body,status,next_attempt_at) VALUES($1,$2,'issue.created','{}',$3,$4)`
	_, err = db.Exec(insert, hook, workspace, "pending", nil)
	require.Error(t, err, "a pending delivery needs its next attempt")
	_, err = db.Exec(insert, hook, workspace, "delivered", "2026-01-01T00:00:00Z")
	require.Error(t, err, "a finished delivery has no next attempt")
	_, err = db.Exec(insert, hook, workspace, "lost", nil)
	require.Error(t, err, "unknown statuses are refused")
	_, err = db.Exec(insert, hook, workspace, "pending", "2026-01-01T00:00:00Z")
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO webhook_circuits(webhook_id,consecutive_failures) VALUES($1,3)`, hook)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO webhook_lanes(workspace_id) VALUES($1)`, workspace)
	require.NoError(t, err)

	_, err = db.Exec(`DELETE FROM webhooks WHERE id=$1`, hook)
	require.NoError(t, err)
	var deliveries, circuits int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM webhook_deliveries`).Scan(&deliveries))
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM webhook_circuits`).Scan(&circuits))
	require.Zero(t, deliveries, "deliveries go with their webhook")
	require.Zero(t, circuits, "breaker state goes with its webhook")
	_, err = db.Exec(`DELETE FROM workspaces WHERE id=$1`, workspace)
	require.NoError(t, err)
	var lanes int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM webhook_lanes`).Scan(&lanes))
	require.Zero(t, lanes, "lanes go with their workspace")

	require.NoError(t, m.Steps(-1))
	for _, table := range []string{"webhook_deliveries", "webhook_circuits", "webhook_lanes"} {
		var exists bool
		require.NoError(t, db.QueryRow(`SELECT to_regclass($1) IS NOT NULL`, table).Scan(&exists))
		require.False(t, exists, table)
	}
	require.NoError(t, db.QueryRow(`SELECT to_regclass('webhooks') IS NOT NULL`).Scan(new(bool)))
}
