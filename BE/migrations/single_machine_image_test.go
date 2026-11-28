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

// Migration 215 leaves one machine type on one image. Standard machines become
// headless: a terminal that shared the IDE container keeps its record with no
// image (so the manager recreates it from the machine image), a machine
// created without the IDE gets a terminal, and only terminal and egress
// services remain. Variables for the IDE reach the terminal without
// overriding the terminal's own; collector and browser variables, collector
// tokens and Development Environments are removed. Rollback restores the
// schema of migration 214 without the removed data.
func TestSingleMachineImageMigrationConvertsStandardMachinesAndRollsBack(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", databaseURL)
	require.NoError(t, err)
	name := "kuayle_single_image_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	require.NoError(t, m.Migrate(214))
	db, err := sql.Open("pgx", testURL)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	fixture := insertBaseMigrationFixture(t, db)
	ws, user := fixture.workspaceA, fixture.userA
	exec := func(query string, args ...any) {
		t.Helper()
		_, err := db.Exec(query, args...)
		require.NoError(t, err)
	}

	exec(`INSERT INTO dev_machine_workspace_policies (workspace_id,enabled) VALUES ($1,TRUE)`, ws)
	shared, noIDE, environment := uuid.New(), uuid.New(), uuid.New()
	sharedKey, noIDEKey := "sharedide0000000", "noide00000000000"
	insertMachine := func(id uuid.UUID, routingKey string, environmentID *uuid.UUID) {
		t.Helper()
		exec(`INSERT INTO dev_machines
			(id,workspace_id,created_by_user_id,routing_key,name,status,desired_status,
			 machine_size,cpu_millis,memory_mb,disk_gb,profile,environment_id,environment_builder,services_config)
			VALUES ($1,$2,$3,$4,$4,'running','running','small',2000,4096,20,'standard',$5,FALSE,'{"ide":true}')`,
			id, ws, user, routingKey, environmentID)
	}
	insertMachine(shared, sharedKey, nil)
	insertMachine(noIDE, noIDEKey, nil)
	exec(`INSERT INTO dev_machine_environments (id,workspace_id,name,image_ref,status,source_machine_id,created_by_user_id)
		VALUES ($1,$2,'snapshot','kuayle/dev-environment:snapshot','ready',$3,$4)`, environment, ws, shared, user)
	exec(`UPDATE dev_machines SET environment_id=$1 WHERE id=$2`, environment, noIDE)
	exec(`INSERT INTO dev_machine_scope_settings (workspace_id,environment_id) VALUES ($1,$2)`, ws, environment)
	exec(`INSERT INTO dev_machine_scope_settings (workspace_id,team_id,environment_id,agent_instructions) VALUES ($1,$2,$3,'use pnpm')`, ws, fixture.teamA, environment)

	// The standard machine's terminal ran in the IDE container.
	ideContainer := "kuayle-" + sharedKey + "-ide"
	terminal := uuid.New()
	service := func(id uuid.UUID, serviceType, containerName, image string, port int) {
		t.Helper()
		exec(`INSERT INTO dev_machine_services
			(id,workspace_id,machine_id,service_type,service_key,container_name,image_ref,internal_host,internal_port,status)
			VALUES ($1,$2,$3,$4,$4,$5,$6,$5,$7,'running')`, id, ws, shared, serviceType, containerName, image, port)
	}
	service(uuid.New(), "ide", ideContainer, "kuayle/ide:old", 8080)
	service(terminal, "terminal", ideContainer, "kuayle/ide:old", 7681)
	service(uuid.New(), "browser", "kuayle-"+sharedKey+"-browser", "kuayle/browser:old", 9222)
	service(uuid.New(), "collector", "kuayle-"+sharedKey+"-collector", "kuayle/collector:old", 9090)
	service(uuid.New(), "egress", "kuayle-"+sharedKey+"-egress", "kuayle/egress:old", 3128)
	envVar := func(target, name, value string) {
		t.Helper()
		exec(`INSERT INTO dev_machine_env_vars (machine_id,target_service,name,encrypted_value) VALUES ($1,$2,$3,$4)`,
			shared, target, name, value)
	}
	envVar("terminal", "SHARED", "terminal-value")
	envVar("ide", "SHARED", "ide-value")
	envVar("ide", "IDE_ONLY", "ide-only")
	envVar("collector", "COLLECTOR_TOKEN", "collector")
	envVar("browser", "BROWSER_TOKEN", "browser")
	envVar("agent", "AGENT_ONLY", "agent")
	exec(`INSERT INTO dev_machine_tokens (machine_id,token_hash,scopes) VALUES ($1,$2,'["events:write"]')`, shared, strings.Repeat("a", 64))
	snapshot, reconcile := uuid.New(), uuid.New()
	exec(`INSERT INTO dev_machine_operations (id,machine_id,environment_id,workspace_id,action,generation,idempotency_key)
		VALUES ($1,$2,$3,$4,'snapshot_environment',1,'snapshot')`, snapshot, shared, environment, ws)
	exec(`INSERT INTO dev_machine_operations (id,machine_id,workspace_id,action,generation,idempotency_key)
		VALUES ($1,$2,$3,'reconcile',1,'reconcile')`, reconcile, shared, ws)

	require.NoError(t, m.Migrate(215))
	requireMigrationVersion(t, m, 215)

	var standard int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM dev_machines WHERE profile<>'headless'`).Scan(&standard))
	require.Zero(t, standard, "every machine is headless")
	_, err = db.Exec(`UPDATE dev_machines SET profile='standard' WHERE id=$1`, shared)
	require.Error(t, err, "the standard profile no longer exists")

	type serviceRow struct{ id, serviceType, containerName, image, host string }
	services := func(machine uuid.UUID) []serviceRow {
		t.Helper()
		rows, err := db.Query(`SELECT id::text,service_type,container_name,image_ref,internal_host FROM dev_machine_services
			WHERE machine_id=$1 ORDER BY service_type`, machine)
		require.NoError(t, err)
		defer rows.Close()
		var result []serviceRow
		for rows.Next() {
			var row serviceRow
			require.NoError(t, rows.Scan(&row.id, &row.serviceType, &row.containerName, &row.image, &row.host))
			result = append(result, row)
		}
		require.NoError(t, rows.Err())
		return result
	}
	sharedServices := services(shared)
	require.Len(t, sharedServices, 2, "only the terminal and the egress proxy remain")
	require.Equal(t, "egress", sharedServices[0].serviceType)
	require.Equal(t, "kuayle/egress:old", sharedServices[0].image, "the egress proxy is untouched")
	require.Equal(t, serviceRow{id: terminal.String(), serviceType: "terminal", containerName: ideContainer, image: "kuayle/ide:old", host: ideContainer},
		sharedServices[1], "the terminal keeps its record and its IDE image until it is recreated from the machine image")
	require.Equal(t, []serviceRow{{
		serviceType: "terminal", containerName: "kuayle-" + noIDEKey + "-terminal", image: "", host: noIDEKey + "-terminal",
	}}, func() []serviceRow {
		rows := services(noIDE)
		for index := range rows {
			require.NotEmpty(t, rows[index].id)
			rows[index].id = ""
		}
		return rows
	}(), "a machine created without the IDE gets a terminal")
	_, err = db.Exec(`INSERT INTO dev_machine_services
		(workspace_id,machine_id,service_type,service_key,container_name,image_ref,internal_host,internal_port)
		VALUES ($1,$2,'ide','ide','ide','ide','ide',8080)`, ws, shared)
	require.Error(t, err, "the IDE service type no longer exists")

	vars := map[string]string{}
	rows, err := db.Query(`SELECT target_service||':'||name,encrypted_value FROM dev_machine_env_vars WHERE machine_id=$1`, shared)
	require.NoError(t, err)
	for rows.Next() {
		var key, value string
		require.NoError(t, rows.Scan(&key, &value))
		vars[key] = value
	}
	require.NoError(t, rows.Err())
	rows.Close()
	require.Equal(t, map[string]string{
		"terminal:SHARED":   "terminal-value",
		"terminal:IDE_ONLY": "ide-only",
		"agent:AGENT_ONLY":  "agent",
	}, vars, "IDE variables reach the terminal without overriding its own; collector and browser variables are gone")

	var tables, columns int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM information_schema.tables
		WHERE table_name IN ('dev_machine_tokens','dev_machine_environments')`).Scan(&tables))
	require.Zero(t, tables, "collector tokens and Development Environments are removed")
	removedColumns := `SELECT count(*) FROM information_schema.columns
		WHERE (table_name='dev_machines' AND column_name IN ('environment_id','environment_builder','services_config'))
		   OR (table_name IN ('dev_machine_operations','dev_machine_scope_settings') AND column_name='environment_id')`
	require.NoError(t, db.QueryRow(removedColumns).Scan(&columns))
	require.Zero(t, columns)
	var operations []string
	rows, err = db.Query(`SELECT id::text FROM dev_machine_operations WHERE machine_id=$1`, shared)
	require.NoError(t, err)
	for rows.Next() {
		var id string
		require.NoError(t, rows.Scan(&id))
		operations = append(operations, id)
	}
	require.NoError(t, rows.Err())
	rows.Close()
	require.Equal(t, []string{reconcile.String()}, operations, "only the environment snapshot operation is removed")
	var settings int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM dev_machine_scope_settings WHERE workspace_id=$1`, ws).Scan(&settings))
	require.Equal(t, 1, settings, "a setting that only chose an environment is removed; the others keep their other values")
	var instructions string
	require.NoError(t, db.QueryRow(`SELECT agent_instructions FROM dev_machine_scope_settings WHERE workspace_id=$1 AND team_id=$2`, ws, fixture.teamA).Scan(&instructions))
	require.Equal(t, "use pnpm", instructions)

	_, err = db.Exec(`UPDATE teams SET is_private=TRUE WHERE id=$1`, fixture.teamA)
	require.ErrorContains(t, err, "Remove development machines and scope settings before enabling private teams")
	require.Equal(t, "23514", postgresCode(err))

	require.NoError(t, m.Migrate(214))
	requireMigrationVersion(t, m, 214)
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM information_schema.tables
		WHERE table_name IN ('dev_machine_tokens','dev_machine_environments')`).Scan(&tables))
	require.Equal(t, 2, tables, "rollback recreates the removed tables")
	require.NoError(t, db.QueryRow(removedColumns).Scan(&columns))
	require.Equal(t, 5, columns, "rollback restores the removed columns")
	var restoredRows int
	require.NoError(t, db.QueryRow(`SELECT (SELECT count(*) FROM dev_machine_tokens)+(SELECT count(*) FROM dev_machine_environments)`).Scan(&restoredRows))
	require.Zero(t, restoredRows, "the removed data is not restored")
	var profiles int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM dev_machines WHERE profile='headless'`).Scan(&profiles))
	require.Equal(t, 2, profiles, "machines stay headless after rollback")
	require.NoError(t, m.Migrate(215), "the migration can be applied again")
}
