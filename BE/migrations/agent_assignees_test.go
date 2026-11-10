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

// Agent assignees are added beside people: existing issue_assignees rows,
// tasks and history are untouched, and rolling back removes only what the
// migration added.
func TestAgentAssigneesMigration(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", url)
	require.NoError(t, err)
	name := "kuayle_agent_assignees_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	require.NoError(t, m.Migrate(138))
	var issue, team, conversation, task, profile uuid.UUID
	require.NoError(t, db.QueryRow(`SELECT id,team_id FROM issues WHERE workspace_id=$1 LIMIT 1`, fixture.workspaceA).Scan(&issue, &team))
	_, err = db.Exec(`INSERT INTO issue_assignees(issue_id,user_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, issue, fixture.userA)
	require.NoError(t, err)
	require.NoError(t, db.QueryRow(`INSERT INTO conversations(workspace_id,creator_id,title,issue_id,team_id) VALUES($1,$2,'Task',$3,$4) RETURNING id`, fixture.workspaceA, fixture.userA, issue, team).Scan(&conversation))
	require.NoError(t, db.QueryRow(`INSERT INTO agent_tasks(workspace_id,conversation_id,creator_id,issue_id,team_id,request_id,prompt,provider) VALUES($1,$2,$3,$4,$5,$6,'Implement','codex') RETURNING id`, fixture.workspaceA, conversation, fixture.userA, issue, team, uuid.New()).Scan(&task))
	require.NoError(t, db.QueryRow(`INSERT INTO agent_profiles(workspace_id,name,role,harness) VALUES($1,'Fixer','implementer','codex') RETURNING id`, fixture.workspaceA).Scan(&profile))

	require.NoError(t, m.Migrate(139))
	var existing *uuid.UUID
	require.NoError(t, db.QueryRow(`SELECT agent_profile_id FROM agent_tasks WHERE id=$1`, task).Scan(&existing))
	require.Nil(t, existing, "an existing task runs as no agent identity")
	var people int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM issue_assignees WHERE issue_id=$1`, issue).Scan(&people))
	require.Equal(t, 1, people, "people stay assigned")

	_, err = db.Exec(`INSERT INTO agent_identities(profile_id,workspace_id,avatar_color) VALUES($1,$2,'red')`, profile, fixture.workspaceA)
	require.Error(t, err, "the avatar colour is a #rrggbb value")
	_, err = db.Exec(`INSERT INTO agent_team_policies(profile_id,team_id,workspace_id) VALUES($1,$2,$3)`, profile, team, fixture.workspaceA)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO issue_agent_assignees(issue_id,profile_id,workspace_id,assigned_by,task_id) VALUES($1,$2,$3,$4,$5)`, issue, profile, fixture.workspaceA, fixture.userA, task)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO issue_agent_assignees(issue_id,profile_id,workspace_id) VALUES($1,$2,$3)`, issue, profile, fixture.workspaceA)
	require.Error(t, err, "an agent is assigned to an issue once")
	_, err = db.Exec(`UPDATE agent_tasks SET agent_profile_id=$2 WHERE id=$1`, task, profile)
	require.NoError(t, err)

	// Deleting the profile removes its assignments and policies; its task
	// stays and only loses the link.
	_, err = db.Exec(`DELETE FROM agent_profiles WHERE id=$1`, profile)
	require.NoError(t, err)
	var count int
	require.NoError(t, db.QueryRow(`SELECT (SELECT count(*) FROM issue_agent_assignees)+(SELECT count(*) FROM agent_team_policies)`).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM agent_tasks WHERE id=$1 AND agent_profile_id IS NULL`, task).Scan(&count))
	require.Equal(t, 1, count)

	require.NoError(t, m.Steps(-1))
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM information_schema.tables WHERE table_name IN ('agent_identities','agent_team_policies','issue_agent_assignees')`).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM information_schema.columns WHERE column_name='agent_profile_id' AND table_name IN ('agent_tasks','issue_history')`).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM issue_assignees WHERE issue_id=$1`, issue).Scan(&people))
	require.Equal(t, 1, people)
	require.NoError(t, m.Migrate(139), "the migration re-applies cleanly")
}
