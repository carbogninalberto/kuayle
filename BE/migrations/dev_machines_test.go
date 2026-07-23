package migrations_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestDevMachineMigrationsAreConsolidated(t *testing.T) {
	up, err := os.ReadFile("000033_dev_machines.up.sql")
	require.NoError(t, err)
	down, err := os.ReadFile("000033_dev_machines.down.sql")
	require.NoError(t, err)

	for version := 34; version <= 36; version++ {
		matches, globErr := filepath.Glob(fmt.Sprintf("%06d_*.sql", version))
		require.NoError(t, globErr)
		require.Empty(t, matches)
	}

	upSQL := string(up)
	require.NotContains(t, upSQL, "ALTER TYPE dev_machine_operation_action")
	require.NotContains(t, upSQL, "FOR constraint_name IN")
	require.Contains(t, upSQL, "'checkout_issue', 'snapshot_environment'")
	require.Contains(t, upSQL, "UNIQUE NULLS NOT DISTINCT")
	require.Contains(t, upSQL, "fk_dev_machine_access_tickets_service")
	require.Contains(t, upSQL, "fk_dev_machine_resource_samples_machine")
	require.Contains(t, string(down), "rollback intentionally destroys all Dev Machine control-plane data")
}

func TestDevMachineMigrationRoundTripAndTenantConstraints(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, err := sql.Open("pgx", databaseURL)
	require.NoError(t, err)
	require.NoError(t, admin.PingContext(ctx))

	databaseName := "kuayle_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = admin.ExecContext(ctx, `CREATE DATABASE "`+databaseName+`"`)
	if postgresCode(err) == "42501" {
		_ = admin.Close()
		t.Skip("DATABASE_URL user cannot create an isolated migration database")
	}
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = admin.ExecContext(context.Background(), `DROP DATABASE "`+databaseName+`" WITH (FORCE)`)
		_ = admin.Close()
	})

	testURL := databaseURLWithName(t, databaseURL, databaseName)
	migrator, err := migrate.New("file://.", testURL)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = migrator.Close()
	})
	require.NoError(t, migrator.Migrate(32))

	db, err := sql.Open("pgx", testURL)
	require.NoError(t, err)
	require.NoError(t, db.PingContext(ctx))
	t.Cleanup(func() { _ = db.Close() })

	fixture := insertBaseMigrationFixture(t, db)
	require.NoError(t, migrator.Steps(1))
	requireMigrationVersion(t, migrator, 33)
	requireFinalDevMachineSchema(t, db)
	rows := insertFinalDevMachineFixture(t, db, fixture)
	requireTenantConstraints(t, db, fixture, rows)

	require.NoError(t, migrator.Steps(-1))
	requireMigrationVersion(t, migrator, 32)
	requireDevMachineSchemaAbsent(t, db)
	var workspaceCount, retainedConstraintCount int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM workspaces WHERE id IN ($1,$2)`, fixture.workspaceA, fixture.workspaceB).Scan(&workspaceCount))
	require.Equal(t, 2, workspaceCount)
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM pg_constraint WHERE conname IN (
		'teams_workspace_id_id_key','projects_workspace_id_id_key','issues_workspace_id_id_key','github_repos_workspace_id_id_key'
	)`).Scan(&retainedConstraintCount))
	require.Zero(t, retainedConstraintCount)

	require.NoError(t, migrator.Steps(1))
	requireMigrationVersion(t, migrator, 33)
	requireFinalDevMachineSchema(t, db)
	var machineCount int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM dev_machines`).Scan(&machineCount))
	require.Zero(t, machineCount, "the documented rollback is destructive")
}

type baseMigrationFixture struct {
	userA, userB                 uuid.UUID
	workspaceA, workspaceB       uuid.UUID
	teamA, teamB                 uuid.UUID
	projectA, projectB           uuid.UUID
	issueA, issueB               uuid.UUID
	repositoryA, repositoryB     uuid.UUID
	installationA, installationB uuid.UUID
}

func insertBaseMigrationFixture(t *testing.T, db *sql.DB) baseMigrationFixture {
	t.Helper()
	fixture := baseMigrationFixture{
		userA: uuid.New(), userB: uuid.New(), workspaceA: uuid.New(), workspaceB: uuid.New(),
		teamA: uuid.New(), teamB: uuid.New(), projectA: uuid.New(), projectB: uuid.New(),
		issueA: uuid.New(), issueB: uuid.New(), repositoryA: uuid.New(), repositoryB: uuid.New(),
		installationA: uuid.New(), installationB: uuid.New(),
	}
	insertTenant := func(label string, userID, workspaceID, teamID, projectID, issueID, installationID, repositoryID uuid.UUID, externalID int64) {
		_, err := db.Exec(`INSERT INTO users (id,email,name,password_hash) VALUES ($1,$2,$3,'test')`, userID, label+"@example.test", label)
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO workspaces (id,name,slug,owner_id) VALUES ($1,$2,$3,$4)`, workspaceID, label, "migration-"+label, userID)
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO workspace_members (workspace_id,user_id,role) VALUES ($1,$2,'owner')`, workspaceID, userID)
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO teams (id,workspace_id,name,key) VALUES ($1,$2,$3,$4)`, teamID, workspaceID, label, strings.ToUpper(label))
		require.NoError(t, err)
		statusID := uuid.New()
		_, err = db.Exec(`INSERT INTO team_statuses (id,team_id,name,slug,category,is_default) VALUES ($1,$2,'Todo','todo','unstarted',TRUE)`, statusID, teamID)
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO projects (id,workspace_id,team_id,name) VALUES ($1,$2,$3,$4)`, projectID, workspaceID, teamID, label)
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO issues (id,workspace_id,team_id,project_id,number,identifier_text,title,creator_id,status_id)
			VALUES ($1,$2,$3,$4,1,$5,$6,$7,$8)`, issueID, workspaceID, teamID, projectID, strings.ToUpper(label)+"-1", label, userID, statusID)
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO github_installations (id,workspace_id,installation_id,account_login,account_type,installed_by)
			VALUES ($1,$2,$3,$4,'Organization',$5)`, installationID, workspaceID, externalID, label, userID)
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO github_repos (id,installation_id,workspace_id,github_repo_id,full_name)
			VALUES ($1,$2,$3,$4,$5)`, repositoryID, installationID, workspaceID, externalID, "kuayle/"+label)
		require.NoError(t, err)
	}
	insertTenant("miga", fixture.userA, fixture.workspaceA, fixture.teamA, fixture.projectA, fixture.issueA, fixture.installationA, fixture.repositoryA, 1001)
	insertTenant("migb", fixture.userB, fixture.workspaceB, fixture.teamB, fixture.projectB, fixture.issueB, fixture.installationB, fixture.repositoryB, 1002)
	return fixture
}

type devMachineRows struct {
	environmentA, environmentB uuid.UUID
	machineA, machineB         uuid.UUID
	checkoutA, checkoutB       uuid.UUID
	runA, runB                 uuid.UUID
	serviceA, serviceB         uuid.UUID
	terminalA, terminalB       uuid.UUID
}

func insertFinalDevMachineFixture(t *testing.T, db *sql.DB, fixture baseMigrationFixture) devMachineRows {
	t.Helper()
	rows := devMachineRows{
		environmentA: uuid.New(), environmentB: uuid.New(), machineA: uuid.New(), machineB: uuid.New(),
		checkoutA: uuid.New(), checkoutB: uuid.New(), runA: uuid.New(), runB: uuid.New(),
		serviceA: uuid.New(), serviceB: uuid.New(), terminalA: uuid.New(), terminalB: uuid.New(),
	}
	insertTenant := func(label string, workspaceID, userID, projectID, issueID, repositoryID, environmentID, machineID, checkoutID, runID, serviceID, terminalID uuid.UUID) {
		_, err := db.Exec(`INSERT INTO dev_machine_workspace_policies (workspace_id,enabled) VALUES ($1,TRUE)`, workspaceID)
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO dev_machine_environments (id,workspace_id,name,image_ref,status,created_by_user_id)
			VALUES ($1,$2,$3,$4,'ready',$5)`, environmentID, workspaceID, label, "sha256:"+strings.Repeat(label[:1], 64), userID)
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO dev_machines
			(id,workspace_id,project_id,issue_id,created_by_user_id,routing_key,name,status,desired_status,
			 machine_size,cpu_millis,memory_mb,disk_gb,max_runtime_minutes,expires_at,environment_id,repository_affinity_id)
			VALUES ($1,$2,$3,$4,$5,$6,$7,'running','running','small',2000,4096,20,120,NOW()+INTERVAL '1 hour',$8,$9)`,
			machineID, workspaceID, projectID, issueID, userID, label+"000000000000", label, environmentID, repositoryID)
		require.NoError(t, err)
		_, err = db.Exec(`UPDATE dev_machine_environments SET source_machine_id=$1 WHERE id=$2`, machineID, environmentID)
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO dev_machine_checkouts
			(id,workspace_id,machine_id,issue_id,github_repo_id,repository_full_name,base_branch,working_branch,workspace_path,status)
			VALUES ($1,$2,$3,$4,$5,$6,'main',$7,$8,'ready')`, checkoutID, workspaceID, machineID, issueID, repositoryID,
			"kuayle/"+label, label+"-branch", "/workspace/"+label)
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO dev_machine_agent_runs
			(id,machine_id,workspace_id,issue_id,checkout_id,requested_by_user_id,provider_id,mode,prompt,command_argv,max_runtime_seconds)
			VALUES ($1,$2,$3,$4,$5,$6,'opencode','autonomous','test','["test"]',60)`, runID, machineID, workspaceID, issueID, checkoutID, userID)
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO dev_machine_services
			(id,workspace_id,machine_id,service_type,service_key,container_name,image_ref,internal_host,internal_port,status)
			VALUES ($1,$2,$3,'terminal','terminal',$4,'test-image',$5,7681,'running')`, serviceID, workspaceID, machineID, label+"-terminal", label+"-terminal")
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO dev_machine_terminal_sessions
			(id,workspace_id,machine_id,checkout_id,user_id,name,runtime_session_name)
			VALUES ($1,$2,$3,$4,$5,'Terminal',$6)`, terminalID, workspaceID, machineID, checkoutID, userID, label+"-terminal")
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO dev_machine_operations
			(machine_id,agent_run_id,checkout_id,environment_id,terminal_session_id,workspace_id,action,generation,idempotency_key)
			VALUES ($1,$2,$3,$4,$5,$6,'reconcile',1,$7)`, machineID, runID, checkoutID, environmentID, terminalID, workspaceID, label)
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO dev_machine_events (workspace_id,machine_id,agent_run_id,source,event_type)
			VALUES ($1,$2,$3,'test','test.event')`, workspaceID, machineID, runID)
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO dev_machine_log_chunks (workspace_id,machine_id,agent_run_id,service_id,stream,sequence,content)
			VALUES ($1,$2,$3,$4,'stdout',1,'test')`, workspaceID, machineID, runID, serviceID)
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO dev_machine_resource_samples (workspace_id,machine_id) VALUES ($1,$2)`, workspaceID, machineID)
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO dev_machine_access_tickets
			(workspace_id,machine_id,service_id,terminal_session_id,user_id,token_hash,bound_host,expires_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,NOW()+INTERVAL '1 minute')`, workspaceID, machineID, serviceID, terminalID, userID,
			strings.Repeat(label[3:4], 64), label+".example.test")
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO dev_machine_access_sessions
			(workspace_id,machine_id,service_id,user_id,token_hash,bound_host,expires_at)
			VALUES ($1,$2,$3,$4,$5,$6,NOW()+INTERVAL '1 hour')`, workspaceID, machineID, serviceID, userID,
			strings.Repeat(strings.ToUpper(label[3:4]), 64), label+".example.test")
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO dev_machine_scope_settings (workspace_id,issue_id,github_repo_id,environment_id)
			VALUES ($1,$2,$3,$4)`, workspaceID, issueID, repositoryID, environmentID)
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO dev_machine_runtime_credentials
			(machine_id,credential_type,fingerprint_sha256,encrypted_value,expires_at)
			VALUES ($1,'github_installation_token',$2,'encrypted',NOW()+INTERVAL '1 hour')`, machineID, strings.Repeat(label[3:4], 64))
		require.NoError(t, err)
	}
	insertTenant("miga", fixture.workspaceA, fixture.userA, fixture.projectA, fixture.issueA, fixture.repositoryA,
		rows.environmentA, rows.machineA, rows.checkoutA, rows.runA, rows.serviceA, rows.terminalA)
	insertTenant("migb", fixture.workspaceB, fixture.userB, fixture.projectB, fixture.issueB, fixture.repositoryB,
		rows.environmentB, rows.machineB, rows.checkoutB, rows.runB, rows.serviceB, rows.terminalB)
	return rows
}

func requireTenantConstraints(t *testing.T, db *sql.DB, fixture baseMigrationFixture, rows devMachineRows) {
	t.Helper()
	assertForeignKeyViolation(t, db, `UPDATE dev_machines SET project_id=$1 WHERE id=$2`, fixture.projectB, rows.machineA)
	assertForeignKeyViolation(t, db, `UPDATE dev_machines SET issue_id=$1 WHERE id=$2`, fixture.issueB, rows.machineA)
	assertForeignKeyViolation(t, db, `UPDATE dev_machines SET repository_affinity_id=$1 WHERE id=$2`, fixture.repositoryB, rows.machineA)
	assertForeignKeyViolation(t, db, `UPDATE dev_machines SET environment_id=$1 WHERE id=$2`, rows.environmentB, rows.machineA)
	assertForeignKeyViolation(t, db, `UPDATE dev_machine_environments SET source_machine_id=$1 WHERE id=$2`, rows.machineB, rows.environmentA)
	assertForeignKeyViolation(t, db, `INSERT INTO dev_machine_services
		(workspace_id,machine_id,service_type,service_key,container_name,image_ref,internal_host,internal_port)
		VALUES ($1,$2,'ide','cross','cross','test','cross',8080)`, fixture.workspaceB, rows.machineA)
	assertForeignKeyViolation(t, db, `INSERT INTO dev_machine_resource_samples (workspace_id,machine_id) VALUES ($1,$2)`, fixture.workspaceB, rows.machineA)
	assertForeignKeyViolation(t, db, `INSERT INTO dev_machine_events (workspace_id,machine_id,source,event_type)
		VALUES ($1,$2,'test','cross')`, fixture.workspaceB, rows.machineA)
	assertForeignKeyViolation(t, db, `INSERT INTO dev_machine_checkouts
		(workspace_id,machine_id,issue_id,github_repo_id,repository_full_name,base_branch,working_branch,workspace_path)
		VALUES ($1,$2,$3,$4,'kuayle/cross','main','cross','/workspace/cross')`, fixture.workspaceA, rows.machineB, fixture.issueA, fixture.repositoryA)
	assertForeignKeyViolation(t, db, `INSERT INTO dev_machine_checkouts
		(workspace_id,machine_id,issue_id,github_repo_id,repository_full_name,base_branch,working_branch,workspace_path)
		VALUES ($1,$2,$3,$4,'kuayle/cross-issue','main','cross-issue','/workspace/cross-issue')`, fixture.workspaceA, rows.machineA, fixture.issueB, fixture.repositoryA)
	assertForeignKeyViolation(t, db, `INSERT INTO dev_machine_agent_runs
		(machine_id,workspace_id,requested_by_user_id,provider_id,mode,status,prompt,command_argv,max_runtime_seconds)
		VALUES ($1,$2,$3,'opencode','autonomous','succeeded','cross','["test"]',60)`, rows.machineB, fixture.workspaceA, fixture.userA)
	assertForeignKeyViolation(t, db, `INSERT INTO dev_machine_log_chunks
		(workspace_id,machine_id,service_id,stream,sequence,content) VALUES ($1,$2,$3,'stdout',2,'cross')`, fixture.workspaceA, rows.machineA, rows.serviceB)
	assertForeignKeyViolation(t, db, `INSERT INTO dev_machine_terminal_sessions
		(workspace_id,machine_id,checkout_id,user_id,name,runtime_session_name) VALUES ($1,$2,$3,$4,'Cross','cross')`,
		fixture.workspaceA, rows.machineA, rows.checkoutB, fixture.userA)
	assertForeignKeyViolation(t, db, `INSERT INTO dev_machine_access_tickets
		(workspace_id,machine_id,service_id,user_id,token_hash,bound_host,expires_at)
		VALUES ($1,$2,$3,$4,$5,'cross.example.test',NOW()+INTERVAL '1 minute')`, fixture.workspaceA, rows.machineA, rows.serviceB, fixture.userA, strings.Repeat("c", 64))
	assertForeignKeyViolation(t, db, `INSERT INTO dev_machine_access_sessions
		(workspace_id,machine_id,service_id,user_id,token_hash,bound_host,expires_at)
		VALUES ($1,$2,$3,$4,$5,'cross.example.test',NOW()+INTERVAL '1 minute')`, fixture.workspaceA, rows.machineA, rows.serviceB, fixture.userA, strings.Repeat("d", 64))
	assertForeignKeyViolation(t, db, `INSERT INTO dev_machine_scope_settings (workspace_id,team_id,environment_id)
		VALUES ($1,$2,$3)`, fixture.workspaceA, fixture.teamA, rows.environmentB)
	assertForeignKeyViolation(t, db, `INSERT INTO dev_machine_operations
		(machine_id,agent_run_id,workspace_id,action,generation,idempotency_key)
		VALUES ($1,$2,$3,'run_agent',2,'cross-run')`, rows.machineA, rows.runB, fixture.workspaceA)

	assertUniqueViolation(t, db, `INSERT INTO dev_machine_log_chunks
		(workspace_id,machine_id,agent_run_id,service_id,stream,sequence,content)
		VALUES ($1,$2,$3,$4,'stdout',1,'duplicate')`, fixture.workspaceA, rows.machineA, rows.runA, rows.serviceA)
	assertUniqueViolation(t, db, `INSERT INTO dev_machine_agent_runs
		(machine_id,workspace_id,requested_by_user_id,provider_id,mode,prompt,command_argv,max_runtime_seconds)
		VALUES ($1,$2,$3,'codex','autonomous','second active','["test"]',60)`, rows.machineA, fixture.workspaceA, fixture.userA)
}

func requireFinalDevMachineSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, table := range []string{
		"dev_machines", "dev_machine_environments", "dev_machine_scope_settings", "dev_machine_checkouts",
		"dev_machine_terminal_sessions", "dev_machine_agent_runs", "dev_machine_services", "dev_machine_operations",
		"dev_machine_events", "dev_machine_log_chunks", "dev_machine_access_tickets", "dev_machine_access_sessions",
		"dev_machine_resource_samples", "dev_machine_runtime_credentials",
	} {
		var exists bool
		require.NoError(t, db.QueryRow(`SELECT to_regclass('public.' || $1) IS NOT NULL`, table).Scan(&exists))
		require.True(t, exists, table)
	}
	for _, index := range []string{
		"idx_dev_machines_expiry", "idx_dev_machines_idle", "idx_dev_machine_environments_delete_requested",
		"idx_dev_machine_operations_ready", "idx_dev_machine_agent_runs_one_active",
		"idx_dev_machine_events_agent_run_cursor", "idx_dev_machine_log_chunks_agent_run_cursor",
		"idx_dev_machine_access_tickets_expiry", "idx_dev_machine_runtime_credentials_expiry",
	} {
		var exists bool
		require.NoError(t, db.QueryRow(`SELECT to_regclass('public.' || $1) IS NOT NULL`, index).Scan(&exists))
		require.True(t, exists, index)
	}
	var actions []string
	require.NoError(t, db.QueryRow(`SELECT enum_range(NULL::dev_machine_operation_action)::text[]`).Scan(pq.Array(&actions)))
	require.Equal(t, []string{"spawn", "start", "stop", "pause", "teardown", "reconcile", "run_agent", "cancel_agent", "checkout_issue", "snapshot_environment", "terminate_terminal"}, actions)
}

func requireDevMachineSchemaAbsent(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, table := range []string{
		"dev_machines", "dev_machine_environments", "dev_machine_scope_settings", "dev_machine_checkouts",
		"dev_machine_terminal_sessions", "dev_machine_agent_runs", "dev_machine_services", "dev_machine_operations",
		"dev_machine_events", "dev_machine_log_chunks", "dev_machine_access_tickets", "dev_machine_access_sessions",
		"dev_machine_resource_samples", "dev_machine_runtime_credentials",
	} {
		var missing bool
		require.NoError(t, db.QueryRow(`SELECT to_regclass('public.' || $1) IS NULL`, table).Scan(&missing))
		require.True(t, missing, table)
	}
	var typeCount int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM pg_type WHERE typname LIKE 'dev_machine_%'`).Scan(&typeCount))
	require.Zero(t, typeCount)
}

func databaseURLWithName(t *testing.T, databaseURL, databaseName string) string {
	t.Helper()
	parsed, err := url.Parse(databaseURL)
	require.NoError(t, err)
	require.NotEmpty(t, parsed.Scheme)
	parsed.Path = "/" + databaseName
	parsed.RawPath = ""
	return parsed.String()
}

func requireMigrationVersion(t *testing.T, migrator *migrate.Migrate, expected uint) {
	t.Helper()
	version, dirty, err := migrator.Version()
	require.NoError(t, err)
	require.False(t, dirty)
	require.Equal(t, expected, version)
}

func assertForeignKeyViolation(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	_, err := db.Exec(query, args...)
	require.Equal(t, "23503", postgresCode(err), err)
}

func assertUniqueViolation(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	_, err := db.Exec(query, args...)
	require.Equal(t, "23505", postgresCode(err), err)
}

func postgresCode(err error) string {
	if err == nil {
		return ""
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		return postgresError.Code
	}
	return ""
}
