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

func TestTestCommandMigrationRetainsDataAndRefusesLoss(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", databaseURL)
	require.NoError(t, err)
	name := "kuayle_commands_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = admin.Exec(`CREATE DATABASE "` + name + `"`)
	require.NoError(t, err)
	t.Cleanup(func() {
		admin.ExecContext(context.Background(), `DROP DATABASE "`+name+`" WITH (FORCE)`)
		admin.Close()
	})
	testURL := databaseURLWithName(t, databaseURL, name)
	m, err := migrate.New("file://.", testURL)
	require.NoError(t, err)
	t.Cleanup(func() { m.Close() })
	require.NoError(t, m.Migrate(32))
	db, err := sql.Open("pgx", testURL)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	fixture := insertBaseMigrationFixture(t, db)
	require.NoError(t, m.Migrate(56))
	var issue, team, conversation, task uuid.UUID
	require.NoError(t, db.QueryRow(`SELECT id,team_id FROM issues WHERE workspace_id=$1 LIMIT 1`, fixture.workspaceA).Scan(&issue, &team))
	require.NoError(t, db.QueryRow(`INSERT INTO conversations(workspace_id,creator_id,title,issue_id,team_id) VALUES($1,$2,'Commands',$3,$4) RETURNING id`, fixture.workspaceA, fixture.userA, issue, team).Scan(&conversation))
	require.NoError(t, db.QueryRow(`INSERT INTO agent_tasks(workspace_id,conversation_id,creator_id,issue_id,team_id,request_id,prompt,provider) VALUES($1,$2,$3,$4,$5,$6,'Implement','codex') RETURNING id`, fixture.workspaceA, conversation, fixture.userA, issue, team, uuid.New()).Scan(&task))
	require.NoError(t, m.Migrate(57))
	var command string
	var requested sql.NullString
	require.NoError(t, db.QueryRow(`SELECT test_command,validation_command FROM agent_tasks WHERE id=$1`, task).Scan(&requested, &command))
	require.False(t, requested.Valid)
	require.JSONEq(t, `[]`, command)
	require.NoError(t, m.Steps(-1))
	require.NoError(t, m.Steps(1))
	_, err = db.Exec(`UPDATE agent_tasks SET validation_command='["npm","test"]' WHERE id=$1`, task)
	require.NoError(t, err)
	require.Error(t, m.Steps(-1))
	require.NoError(t, m.Force(57))
	require.NoError(t, db.QueryRow(`SELECT validation_command FROM agent_tasks WHERE id=$1`, task).Scan(&command))
	require.JSONEq(t, `["npm","test"]`, command)
	_, err = db.Exec(`UPDATE agent_tasks SET validation_command='[]',test_command='[]' WHERE id=$1`, task)
	require.NoError(t, err)
	require.Error(t, m.Steps(-1))
	require.NoError(t, m.Force(57))
	_, err = db.Exec(`UPDATE agent_tasks SET test_command=NULL WHERE id=$1`, task)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO dev_machine_scope_settings(workspace_id,test_command) VALUES($1,'[]')`, fixture.workspaceA)
	require.NoError(t, err)
	require.Error(t, m.Steps(-1))
	require.NoError(t, m.Force(57))
	_, err = db.Exec(`UPDATE dev_machine_scope_settings SET test_command=NULL WHERE workspace_id=$1`, fixture.workspaceA)
	require.NoError(t, err)
	require.NoError(t, m.Steps(-1))
	var state string
	require.NoError(t, db.QueryRow(`SELECT state FROM agent_tasks WHERE id=$1`, task).Scan(&state))
	require.Equal(t, "queued", state)
}
