package migrations_test

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Migration 173 (KUA-171) starts the workflow clock only at real work and
// records when a person's wait began. A queued task that never started loses
// its clock; a task waiting at an approval is held from the time it entered.
func TestWorkflowClockMigrationBackfillsLiveTasks(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", url)
	require.NoError(t, err)
	name := "kuayle_workflow_clock_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	require.NoError(t, m.Migrate(32))
	db, err := sql.Open("pgx", target)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	fixture := insertBaseMigrationFixture(t, db)
	require.NoError(t, m.Migrate(172))

	var issue, team, conversation uuid.UUID
	require.NoError(t, db.QueryRow(`SELECT id,team_id FROM issues WHERE workspace_id=$1 LIMIT 1`, fixture.workspaceA).Scan(&issue, &team))
	require.NoError(t, db.QueryRow(`INSERT INTO conversations(workspace_id,creator_id,title,issue_id,team_id) VALUES($1,$2,'Clock',$3,$4) RETURNING id`, fixture.workspaceA, fixture.userA, issue, team).Scan(&conversation))
	insertTask := func(state string, started *time.Time, status *string, sequence int) uuid.UUID {
		var id uuid.UUID
		require.NoError(t, db.QueryRow(`INSERT INTO agent_tasks(workspace_id,conversation_id,creator_id,issue_id,team_id,request_id,prompt,provider,state,workflow_started_at,workflow_status,workflow_sequence) VALUES($1,$2,$3,$4,$5,$6,'Implement','codex',$7,$8,$9,$10) RETURNING id`,
			fixture.workspaceA, conversation, fixture.userA, issue, team, uuid.New(), state, started, status, sequence).Scan(&id))
		return id
	}
	ago := time.Now().Add(-48 * time.Hour)
	blocked := insertTask("queued", &ago, nil, 2)
	started := insertTask("queued", &ago, nil, 2)
	status := "waiting"
	waiting := insertTask("awaiting_review", &ago, &status, 2)
	_, err = db.Exec(`INSERT INTO agent_task_transitions(task_id,from_state,to_state,reason) VALUES($1,'queued','preparing','Preparing')`, started)
	require.NoError(t, err)
	entered := time.Now().Add(-16 * time.Hour)
	_, err = db.Exec(`INSERT INTO agent_task_workflow_steps(task_id,sequence,node_id,node_type,visit,reason,entered_at) VALUES($1,2,'ship','approval',1,'Waiting for approval',$2)`, waiting, entered)
	require.NoError(t, err)

	// A workflow task waiting for a person's input holds the clock from when
	// it entered waiting_input. A Standard task waiting for input is not held.
	workflow, version := uuid.New(), uuid.New()
	_, err = db.Exec(`INSERT INTO workflows(id,workspace_id,name) VALUES($1,$2,'Clock flow')`, workflow, fixture.workspaceA)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO workflow_versions(id,workflow_id,workspace_id,version,graph) VALUES($1,$2,$3,1,'{}'::jsonb)`, version, workflow, fixture.workspaceA)
	require.NoError(t, err)
	inputNode, inputStatus := "implement", "waiting"
	var waitingInput uuid.UUID
	require.NoError(t, db.QueryRow(`INSERT INTO agent_tasks(workspace_id,conversation_id,creator_id,issue_id,team_id,request_id,prompt,provider,state,kind,workflow_version_id,workflow_node,workflow_status,workflow_sequence) VALUES($1,$2,$3,$4,$5,$6,'Implement','codex','waiting_input','implement',$7,$8,$9,2) RETURNING id`,
		fixture.workspaceA, conversation, fixture.userA, issue, team, uuid.New(), version, inputNode, inputStatus).Scan(&waitingInput))
	inputAt := time.Now().Add(-3 * time.Hour)
	_, err = db.Exec(`INSERT INTO agent_task_transitions(task_id,from_state,to_state,reason,created_at) VALUES($1,'running','waiting_input','Needs input',$2)`, waitingInput, inputAt)
	require.NoError(t, err)
	var standardInput uuid.UUID
	require.NoError(t, db.QueryRow(`INSERT INTO agent_tasks(workspace_id,conversation_id,creator_id,issue_id,team_id,request_id,prompt,provider,state) VALUES($1,$2,$3,$4,$5,$6,'Implement','codex','waiting_input') RETURNING id`,
		fixture.workspaceA, conversation, fixture.userA, issue, team, uuid.New()).Scan(&standardInput))

	require.NoError(t, m.Migrate(173))

	var column bool
	require.NoError(t, db.QueryRow(`SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_name='agent_tasks' AND column_name='workflow_held_since')`).Scan(&column))
	require.True(t, column)

	var cleared *time.Time
	require.NoError(t, db.QueryRow(`SELECT workflow_started_at FROM agent_tasks WHERE id=$1`, blocked).Scan(&cleared))
	require.Nil(t, cleared, "a queued task that never started gets a fresh clock")
	require.NoError(t, db.QueryRow(`SELECT workflow_started_at FROM agent_tasks WHERE id=$1`, started).Scan(&cleared))
	require.NotNil(t, cleared, "a task that did start keeps its clock")

	var held *time.Time
	require.NoError(t, db.QueryRow(`SELECT workflow_held_since FROM agent_tasks WHERE id=$1`, waiting).Scan(&held))
	require.NotNil(t, held)
	require.WithinDuration(t, entered, *held, time.Second, "the approval hold starts when its step was entered")

	var inputHeld *time.Time
	require.NoError(t, db.QueryRow(`SELECT workflow_held_since FROM agent_tasks WHERE id=$1`, waitingInput).Scan(&inputHeld))
	require.NotNil(t, inputHeld, "a workflow task waiting for input is held")
	require.WithinDuration(t, inputAt, *inputHeld, time.Second, "the hold starts when it entered waiting_input")
	require.NoError(t, db.QueryRow(`SELECT workflow_held_since FROM agent_tasks WHERE id=$1`, standardInput).Scan(&inputHeld))
	require.Nil(t, inputHeld, "a Standard task waiting for input is not held")

	require.NoError(t, m.Steps(-1))
	require.NoError(t, db.QueryRow(`SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_name='agent_tasks' AND column_name='workflow_held_since')`).Scan(&column))
	require.False(t, column, "the rollback drops the column")
}
