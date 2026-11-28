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

// Migration 216 removes graph workflows and keeps pipelines. A task that ran
// a graph keeps its state and history but no longer names the version; one
// still in progress has its workflow cursor cleared, so it continues on the
// Standard flow. Automations and scope defaults that chose a graph fall back
// to the default; pipeline workflows, defaults and tasks are untouched.
func TestPipelineOnlyWorkflowsMigrationRemovesGraphs(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", databaseURL)
	require.NoError(t, err)
	name := "kuayle_pipeline_only_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	require.NoError(t, m.Migrate(215))
	db, err := sql.Open("pgx", testURL)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	fixture := insertBaseMigrationFixture(t, db)
	ws, user, team, project, issue := fixture.workspaceA, fixture.userA, fixture.teamA, fixture.projectA, fixture.issueA
	exec := func(query string, args ...any) {
		t.Helper()
		_, err := db.Exec(query, args...)
		require.NoError(t, err)
	}

	graph, standardCopy, pipeline := uuid.New(), uuid.New(), uuid.New()
	graphV1, standardV1, pipelineV1 := uuid.New(), uuid.New(), uuid.New()
	workflow := func(id, version uuid.UUID, name, body string, builtin *string) {
		t.Helper()
		exec(`INSERT INTO workflows (id,workspace_id,name,latest_version,builtin_key,created_by) VALUES ($1,$2,$3,1,$4,$5)`, id, ws, name, builtin, user)
		exec(`INSERT INTO workflow_versions (id,workflow_id,workspace_id,version,graph) VALUES ($1,$2,$3,1,$4)`, version, id, ws, body)
	}
	standard := "standard"
	workflow(graph, graphV1, "Review loop", `{"nodes":[],"edges":[]}`, nil)
	workflow(standardCopy, standardV1, "Standard", `{"kind":"graph","nodes":[],"edges":[]}`, &standard)
	workflow(pipeline, pipelineV1, "Team", `{"kind":"pipeline","phases":[],"roster":[]}`, nil)
	exec(`INSERT INTO workflow_defaults (workspace_id,scope,scope_id,workflow_id) VALUES ($1,'workspace',$1,$2)`, ws, graph)
	exec(`INSERT INTO workflow_defaults (workspace_id,scope,scope_id,workflow_id) VALUES ($1,'project',$2,$3)`, ws, project, pipeline)
	graphAutomation, pipelineAutomation := uuid.New(), uuid.New()
	automation := func(id, workflowID uuid.UUID) {
		t.Helper()
		exec(`INSERT INTO agent_automations (id,workspace_id,team_id,owner_id,name,instructions,status_id,workflow_id,pinned_version)
			VALUES ($1,$2,$3,$4,'nightly','fix flaky tests',(SELECT id FROM team_statuses WHERE team_id=$3 LIMIT 1),$5,1)`, id, ws, team, user, workflowID)
	}
	automation(graphAutomation, graph)
	automation(pipelineAutomation, pipeline)

	running, finished, onPipeline := uuid.New(), uuid.New(), uuid.New()
	task := func(id uuid.UUID, state string, version uuid.UUID, isPipeline bool, status string) {
		t.Helper()
		exec(`INSERT INTO agent_tasks (id,workspace_id,team_id,issue_id,creator_id,request_id,provider,prompt,state,
			workflow_version_id,workflow_node,workflow_status,workflow_wake_at,workflow_iterations,workflow_started_at,pipeline)
			VALUES ($1,$2,$3,$4,$5,$1,'codex','Fix it',$6,$7,'review',$8,now(),3,now(),$9)`,
			id, ws, team, issue, user, state, version, status, isPipeline)
		exec(`INSERT INTO agent_task_workflow_steps (task_id,sequence,node_id,node_type,visit,reason) VALUES ($1,1,'implement','agent',1,'started')`, id)
	}
	task(running, "awaiting_review", graphV1, false, "waiting")
	task(finished, "completed", standardV1, false, "done")
	task(onPipeline, "awaiting_review", pipelineV1, true, "waiting")

	require.NoError(t, m.Migrate(216))

	var workflows []string
	rows, err := db.Query(`SELECT name FROM workflows WHERE workspace_id=$1 ORDER BY name`, ws)
	require.NoError(t, err)
	for rows.Next() {
		var n string
		require.NoError(t, rows.Scan(&n))
		workflows = append(workflows, n)
	}
	require.NoError(t, rows.Close())
	require.Equal(t, []string{"Team"}, workflows, "graphs and the edited Standard copy are removed; the pipeline stays")
	var versions int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM workflow_versions WHERE workspace_id=$1`, ws).Scan(&versions))
	require.Equal(t, 1, versions)

	var defaults []string
	rows, err = db.Query(`SELECT scope FROM workflow_defaults WHERE workspace_id=$1 ORDER BY scope`, ws)
	require.NoError(t, err)
	for rows.Next() {
		var scope string
		require.NoError(t, rows.Scan(&scope))
		defaults = append(defaults, scope)
	}
	require.NoError(t, rows.Close())
	require.Equal(t, []string{"project"}, defaults, "the workspace falls back to Standard; the pipeline default stays")

	var workflowID sql.NullString
	var pinned sql.NullInt64
	require.NoError(t, db.QueryRow(`SELECT workflow_id, pinned_version FROM agent_automations WHERE id=$1`, graphAutomation).Scan(&workflowID, &pinned))
	require.False(t, workflowID.Valid)
	require.False(t, pinned.Valid)
	require.NoError(t, db.QueryRow(`SELECT workflow_id, pinned_version FROM agent_automations WHERE id=$1`, pipelineAutomation).Scan(&workflowID, &pinned))
	require.Equal(t, pipeline.String(), workflowID.String)
	require.Equal(t, int64(1), pinned.Int64)

	type taskRow struct {
		state                 string
		version, node, status sql.NullString
		wake                  sql.NullTime
		iterations, steps     int
	}
	read := func(id uuid.UUID) taskRow {
		t.Helper()
		var r taskRow
		require.NoError(t, db.QueryRow(`SELECT state, workflow_version_id::text, workflow_node, workflow_status, workflow_wake_at, workflow_iterations,
			(SELECT count(*) FROM agent_task_workflow_steps s WHERE s.task_id=t.id) FROM agent_tasks t WHERE id=$1`, id).
			Scan(&r.state, &r.version, &r.node, &r.status, &r.wake, &r.iterations, &r.steps))
		return r
	}
	for _, id := range []uuid.UUID{running, finished} {
		r := read(id)
		require.False(t, r.version.Valid, "a task no longer names a removed graph")
		require.False(t, r.node.Valid)
		require.False(t, r.status.Valid)
		require.False(t, r.wake.Valid, "nothing wakes it for a graph step")
		require.Zero(t, r.iterations)
		require.Equal(t, 1, r.steps, "the path it took stays as history")
	}
	require.Equal(t, "awaiting_review", read(running).state, "it waits for a person on the Standard flow")
	require.Equal(t, "completed", read(finished).state)
	kept := read(onPipeline)
	require.Equal(t, pipelineV1.String(), kept.version.String)
	require.Equal(t, "review", kept.node.String)
	require.Equal(t, "waiting", kept.status.String)
	require.Equal(t, 3, kept.iterations)

	require.NoError(t, m.Migrate(215), "rolling back keeps the data as it is")
	require.NoError(t, m.Migrate(216))
}
