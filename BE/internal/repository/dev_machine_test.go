package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

var captureDriverSequence atomic.Uint64

type captureExecDriver struct {
	conn *captureExecConn
}

func (d captureExecDriver) Open(string) (driver.Conn, error) {
	return d.conn, nil
}

type captureExecConn struct {
	mu           sync.Mutex
	query        string
	args         []driver.NamedValue
	rowsAffected int64
	queryColumns []string
	queryValues  []driver.Value
}

func (c *captureExecConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare is not implemented")
}

func (c *captureExecConn) Close() error { return nil }

func (c *captureExecConn) Begin() (driver.Tx, error) {
	return nil, errors.New("transactions are not implemented")
}

func (c *captureExecConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.query = query
	c.args = append([]driver.NamedValue(nil), args...)
	return captureExecResult(c.rowsAffected), nil
}

func (c *captureExecConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.query = query
	c.args = append([]driver.NamedValue(nil), args...)
	columns := append([]string(nil), c.queryColumns...)
	if len(columns) == 0 {
		columns = []string{"value"}
	}
	values := append([]driver.Value(nil), c.queryValues...)
	if values == nil {
		values = []driver.Value{false}
	}
	return &captureRows{columns: columns, values: values}, nil
}

func (c *captureExecConn) captured() (string, []driver.NamedValue) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.query, append([]driver.NamedValue(nil), c.args...)
}

type captureExecResult int64

func (r captureExecResult) LastInsertId() (int64, error) { return 0, nil }
func (r captureExecResult) RowsAffected() (int64, error) { return int64(r), nil }

type captureRows struct {
	columns []string
	values  []driver.Value
	read    bool
}

func (r *captureRows) Columns() []string { return r.columns }
func (r *captureRows) Close() error      { return nil }

func (r *captureRows) Next(dest []driver.Value) error {
	if r.read {
		return io.EOF
	}
	r.read = true
	copy(dest, r.values)
	return nil
}

func newCaptureDevMachineRepository(t *testing.T, rowsAffected int64) (*DevMachineRepository, *captureExecConn) {
	t.Helper()
	driverName := fmt.Sprintf("devmachine_capture_%d", captureDriverSequence.Add(1))
	conn := &captureExecConn{rowsAffected: rowsAffected}
	sql.Register(driverName, captureExecDriver{conn: conn})
	db, err := sql.Open(driverName, "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return NewDevMachineRepository(sqlx.NewDb(db, "postgres")), conn
}

func TestBulkPurgeMachinesKeepsUnsafeRuntimeRowsGuarded(t *testing.T) {
	repo, conn := newCaptureDevMachineRepository(t, 2)
	workspaceID := uuid.New()
	machineID := uuid.New()

	count, err := repo.BulkPurgeMachines(context.Background(), workspaceID, []uuid.UUID{machineID}, time.Now().Add(-7*24*time.Hour), true, true)

	require.NoError(t, err)
	require.Equal(t, 2, count)
	query, args := conn.captured()
	require.Contains(t, query, "m.status IN")
	require.Contains(t, query, "NOT EXISTS (SELECT 1 FROM dev_machine_operations")
	require.Contains(t, query, "m.status='destroyed' OR (m.docker_network_name IS NULL AND m.workspace_volume_name IS NULL)")
	require.Len(t, args, 6)
}

func TestMachineNameExistsForUserScopesByCreator(t *testing.T) {
	repo, conn := newCaptureDevMachineRepository(t, 0)
	workspaceID, userID := uuid.New(), uuid.New()

	exists, err := repo.MachineNameExistsForUser(context.Background(), workspaceID, userID, "builder-01")

	require.NoError(t, err)
	require.False(t, exists)
	query, args := conn.captured()
	require.Contains(t, query, "created_by_user_id=$2")
	require.Contains(t, query, "LOWER(name)=LOWER($3)")
	require.Len(t, args, 3)
	require.Equal(t, workspaceID.String(), args[0].Value)
	require.Equal(t, userID.String(), args[1].Value)
	require.Equal(t, "builder-01", args[2].Value)
}

func TestCreateAccessTicketQueryRevalidatesCreatorAndTuple(t *testing.T) {
	repo, conn := newCaptureDevMachineRepository(t, 0)
	conn.queryColumns = []string{"created_at"}
	conn.queryValues = []driver.Value{time.Now().UTC()}
	workspaceID, machineID, serviceID, userID := uuid.New(), uuid.New(), uuid.New(), uuid.New()

	err := repo.CreateAccessTicket(context.Background(), &domain.DevMachineAccessTicket{
		ID: uuid.New(), WorkspaceID: workspaceID, MachineID: machineID, ServiceID: serviceID, UserID: userID,
		TokenHash: strings.Repeat("a", 64), Status: domain.DevMachineAccessTicketStatusActive,
		BoundHost: "0123456789abcdef0123.machines.example.com", ExpiresAt: time.Now().Add(time.Minute),
	})

	require.NoError(t, err)
	query, _ := conn.captured()
	require.Contains(t, query, "SELECT $1, m.workspace_id, m.id, s.id, $5")
	require.Contains(t, query, "JOIN dev_machine_services s ON s.id=$4 AND s.machine_id=m.id")
	require.Contains(t, query, "JOIN workspace_members wm ON wm.workspace_id=m.workspace_id AND wm.user_id=$5")
	require.Contains(t, query, "m.workspace_id=$2 AND m.id=$3 AND m.created_by_user_id=$5")
	require.Contains(t, query, "m.status='running' AND m.desired_status='running' AND m.expires_at>NOW()")
	require.Contains(t, query, "s.status='running' AND $9>NOW() AND $9<=m.expires_at")
}

func TestConsumeAccessTicketQueryRevalidatesCreatorAndTuple(t *testing.T) {
	repo, conn := newCaptureDevMachineRepository(t, 0)
	conn.queryColumns, conn.queryValues = accessTicketColumnsAndValuesForTest()

	ticket, err := repo.ConsumeAccessTicket(context.Background(), strings.Repeat("b", 64), "0123456789abcdef0123.machines.example.com")

	require.NoError(t, err)
	require.NotNil(t, ticket)
	query, _ := conn.captured()
	requireAccessTicketAuthorizationQuery(t, query, "t")
}

func TestCreateAccessSessionQueryRevalidatesCreatorAndTuple(t *testing.T) {
	repo, conn := newCaptureDevMachineRepository(t, 0)
	conn.queryColumns = []string{"created_at", "last_seen_at"}
	now := time.Now().UTC()
	conn.queryValues = []driver.Value{now, now}
	workspaceID, machineID, serviceID, userID := uuid.New(), uuid.New(), uuid.New(), uuid.New()

	err := repo.CreateAccessSession(context.Background(), &domain.DevMachineAccessSession{
		ID: uuid.New(), WorkspaceID: workspaceID, MachineID: machineID, ServiceID: serviceID, UserID: userID,
		TokenHash: strings.Repeat("c", 64), BoundHost: "0123456789abcdef0123.machines.example.com",
		ExpiresAt: time.Now().Add(time.Hour),
	})

	require.NoError(t, err)
	query, _ := conn.captured()
	require.Contains(t, query, "SELECT $1, m.workspace_id, m.id, s.id, $5")
	require.Contains(t, query, "JOIN dev_machine_services s ON s.id=$4 AND s.machine_id=m.id")
	require.Contains(t, query, "JOIN workspace_members wm ON wm.workspace_id=m.workspace_id AND wm.user_id=$5")
	require.Contains(t, query, "m.workspace_id=$2 AND m.id=$3 AND m.created_by_user_id=$5")
	require.Contains(t, query, "m.status='running' AND m.desired_status='running' AND m.expires_at>NOW()")
	require.Contains(t, query, "s.status='running' AND $8>NOW() AND $8<=m.expires_at")
}

func TestUpsertRuntimeCredentialQueryUsesMachineFingerprintConflict(t *testing.T) {
	repo, conn := newCaptureDevMachineRepository(t, 0)
	conn.queryColumns = []string{"id", "created_at", "updated_at"}
	now := time.Now().UTC()
	conn.queryValues = []driver.Value{uuid.New().String(), now, now}
	machineID := uuid.New()
	fingerprint := strings.Repeat("a", 64)

	err := repo.UpsertRuntimeCredential(context.Background(), &domain.DevMachineRuntimeCredential{
		ID: uuid.New(), MachineID: machineID, Scope: domain.DevMachineRuntimeCredentialScopeMachine,
		CredentialType: domain.DevMachineRuntimeCredentialTypeGitHubToken, FingerprintSHA256: fingerprint,
		EncryptedValue: "encrypted-runtime-token", EncryptionKeyVersion: 1, ExpiresAt: now.Add(time.Hour),
	})

	require.NoError(t, err)
	query, args := conn.captured()
	require.Contains(t, query, "INSERT INTO dev_machine_runtime_credentials")
	require.Contains(t, query, "ON CONFLICT (machine_id, fingerprint_sha256) DO UPDATE")
	require.Contains(t, query, "encrypted_value=EXCLUDED.encrypted_value")
	require.Contains(t, query, "expires_at=EXCLUDED.expires_at")
	require.Len(t, args, 8)
	require.Equal(t, machineID.String(), args[1].Value)
	require.Equal(t, domain.DevMachineRuntimeCredentialScopeMachine, args[2].Value)
	require.Equal(t, domain.DevMachineRuntimeCredentialTypeGitHubToken, args[3].Value)
	require.Equal(t, fingerprint, args[4].Value)
	require.Equal(t, "encrypted-runtime-token", args[5].Value)
}

func TestListRuntimeCredentialsQueryReturnsOnlyUnexpired(t *testing.T) {
	repo, conn := newCaptureDevMachineRepository(t, 0)
	machineID := uuid.New()
	now := time.Now().UTC()
	conn.queryColumns = []string{
		"id", "machine_id", "scope", "credential_type", "fingerprint_sha256",
		"encrypted_value", "encryption_key_version", "expires_at", "created_at", "updated_at",
	}
	conn.queryValues = []driver.Value{
		uuid.New().String(), machineID.String(), domain.DevMachineRuntimeCredentialScopeMachine,
		domain.DevMachineRuntimeCredentialTypeGitHubToken, strings.Repeat("b", 64), "encrypted", int64(1), now.Add(time.Hour), now, now,
	}

	credentials, err := repo.ListRuntimeCredentials(context.Background(), machineID)

	require.NoError(t, err)
	require.Len(t, credentials, 1)
	query, args := conn.captured()
	require.Contains(t, query, "FROM dev_machine_runtime_credentials")
	require.Contains(t, query, "expires_at>NOW()")
	require.Len(t, args, 1)
	require.Equal(t, machineID.String(), args[0].Value)
}

func TestPurgeExpiredRuntimeCredentialsDeletesByExpiry(t *testing.T) {
	repo, conn := newCaptureDevMachineRepository(t, 3)
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)

	count, err := repo.PurgeExpiredRuntimeCredentials(context.Background(), now)

	require.NoError(t, err)
	require.Equal(t, 3, count)
	query, args := conn.captured()
	require.Contains(t, query, "DELETE FROM dev_machine_runtime_credentials WHERE expires_at<=$1")
	require.Len(t, args, 1)
	require.Equal(t, now, args[0].Value)
}

func TestGetAccessSessionQueryRevalidatesCreatorAndTuple(t *testing.T) {
	repo, conn := newCaptureDevMachineRepository(t, 0)
	conn.queryColumns, conn.queryValues = accessSessionColumnsAndValuesForTest()

	session, err := repo.GetAccessSession(context.Background(), strings.Repeat("d", 64), "0123456789abcdef0123.machines.example.com")

	require.NoError(t, err)
	require.NotNil(t, session)
	query, _ := conn.captured()
	requireAccessSessionAuthorizationQuery(t, query, "a")
}

func accessTicketColumnsAndValuesForTest() ([]string, []driver.Value) {
	now := time.Now().UTC()
	return []string{
			"id", "workspace_id", "machine_id", "service_id", "user_id", "token_hash",
			"status", "bound_host", "expires_at", "used_at", "created_at", "revoked_at",
		}, []driver.Value{
			uuid.New().String(), uuid.New().String(), uuid.New().String(), uuid.New().String(), uuid.New().String(),
			strings.Repeat("b", 64), string(domain.DevMachineAccessTicketStatusUsed), "0123456789abcdef0123.machines.example.com",
			now.Add(time.Minute), now, now, nil,
		}
}

func accessSessionColumnsAndValuesForTest() ([]string, []driver.Value) {
	now := time.Now().UTC()
	return []string{
			"id", "workspace_id", "machine_id", "service_id", "user_id", "token_hash",
			"bound_host", "expires_at", "last_seen_at", "created_at", "revoked_at",
		}, []driver.Value{
			uuid.New().String(), uuid.New().String(), uuid.New().String(), uuid.New().String(), uuid.New().String(),
			strings.Repeat("d", 64), "0123456789abcdef0123.machines.example.com", now.Add(time.Hour), now, now, nil,
		}
}

func requireAccessTicketAuthorizationQuery(t *testing.T, query, alias string) {
	t.Helper()
	require.Contains(t, query, alias+".bound_host=$2")
	require.Contains(t, query, alias+".status='active'")
	require.Contains(t, query, alias+".expires_at>NOW()")
	requireAccessAuthorizationQuery(t, query, alias)
}

func requireAccessSessionAuthorizationQuery(t *testing.T, query, alias string) {
	t.Helper()
	require.Contains(t, query, alias+".bound_host=$2")
	require.Contains(t, query, alias+".revoked_at IS NULL")
	require.Contains(t, query, alias+".expires_at>NOW()")
	requireAccessAuthorizationQuery(t, query, alias)
}

func requireAccessAuthorizationQuery(t *testing.T, query, alias string) {
	t.Helper()
	require.Contains(t, query, "m.id="+alias+".machine_id")
	require.Contains(t, query, alias+".workspace_id=m.workspace_id")
	require.Contains(t, query, "m.created_by_user_id="+alias+".user_id")
	require.Contains(t, query, "m.status='running' AND m.desired_status='running' AND m.expires_at>NOW()")
	require.Contains(t, query, "s.id="+alias+".service_id AND s.machine_id=m.id AND s.status='running'")
	require.Contains(t, query, "wm.workspace_id=m.workspace_id AND wm.user_id="+alias+".user_id")
	require.Contains(t, query, "wm.role IN ('owner','admin','member')")
}

func TestDevMachineNameUniqueIndexIsCreatorScoped(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	var indexDef string
	err = db.Get(&indexDef, `SELECT pg_get_indexdef(indexrelid) FROM pg_index WHERE indexrelid = 'idx_dev_machines_workspace_name'::regclass`)
	require.NoError(t, err)
	require.Contains(t, indexDef, "workspace_id")
	require.Contains(t, indexDef, "created_by_user_id")
	require.Contains(t, indexDef, "lower")
}

func TestRuntimeCredentialsSchemaHasCascadeUniqueAndExpiryIndex(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	var fkDeleteAction string
	err = db.Get(&fkDeleteAction, `
		SELECT confdeltype
		FROM pg_constraint
		WHERE conrelid = 'dev_machine_runtime_credentials'::regclass
		AND contype = 'f'
		AND conname = 'dev_machine_runtime_credentials_machine_id_fkey'
	`)
	require.NoError(t, err)
	require.Equal(t, "c", fkDeleteAction)

	var uniqueDef string
	err = db.Get(&uniqueDef, `
		SELECT pg_get_constraintdef(oid)
		FROM pg_constraint
		WHERE conrelid = 'dev_machine_runtime_credentials'::regclass
		AND contype = 'u'
	`)
	require.NoError(t, err)
	require.Contains(t, uniqueDef, "machine_id")
	require.Contains(t, uniqueDef, "fingerprint_sha256")

	var indexDef string
	err = db.Get(&indexDef, `SELECT pg_get_indexdef('idx_dev_machine_runtime_credentials_expiry'::regclass)`)
	require.NoError(t, err)
	require.Contains(t, indexDef, "expires_at")
}

func TestAgentRunCreationSerializesWithPauseAndStop(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	for _, test := range []struct {
		name    string
		action  domain.DevMachineOperationAction
		desired domain.DevMachineStatus
	}{
		{name: "pause", action: domain.DevMachineOpPause, desired: domain.DevMachineStatusPaused},
		{name: "stop", action: domain.DevMachineOpStop, desired: domain.DevMachineStatusStopped},
	} {
		t.Run(test.name, func(t *testing.T) {
			userID, workspaceID, machineID := uuid.New(), uuid.New(), uuid.New()
			suffix := strings.ReplaceAll(machineID.String(), "-", "")
			t.Cleanup(func() {
				_, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, workspaceID)
				_, _ = db.Exec(`DELETE FROM users WHERE id=$1`, userID)
			})
			_, err := db.Exec(`INSERT INTO users (id,email,name,password_hash) VALUES ($1,$2,'Lifecycle Test','test')`,
				userID, suffix+"@example.test")
			require.NoError(t, err)
			_, err = db.Exec(`INSERT INTO workspaces (id,name,slug,owner_id) VALUES ($1,'Lifecycle Test',$2,$3)`,
				workspaceID, "lifecycle-"+suffix, userID)
			require.NoError(t, err)
			_, err = db.Exec(`INSERT INTO workspace_members (workspace_id,user_id,role) VALUES ($1,$2,'owner')`, workspaceID, userID)
			require.NoError(t, err)
			_, err = db.Exec(`INSERT INTO dev_machine_workspace_policies (workspace_id,enabled) VALUES ($1,TRUE)`, workspaceID)
			require.NoError(t, err)
			_, err = db.Exec(`INSERT INTO dev_machines
				(id,workspace_id,created_by_user_id,routing_key,name,status,desired_status,generation,
				 repo_url,repo_provider,repo_owner,repo_name,base_branch,working_branch,
				 machine_size,cpu_millis,memory_mb,disk_gb,max_runtime_minutes,expires_at)
				VALUES ($1,$2,$3,$4,$5,'running','running',1,'','github','','','','',
				 'small',1000,2048,20,480,NOW()+INTERVAL '1 hour')`,
				machineID, workspaceID, userID, suffix[:16], "machine-"+suffix)
			require.NoError(t, err)

			repo := NewDevMachineRepository(db)
			run := &domain.DevMachineAgentRun{
				ID: uuid.New(), MachineID: machineID, WorkspaceID: workspaceID, RequestedByUserID: &userID,
				ProviderID: "opencode", Mode: "autonomous", Status: domain.DevMachineAgentRunStatusQueued,
				Prompt: "test", AcceptanceCriteria: []byte(`[]`), AllowedCommands: []byte(`[]`),
				ForbiddenPaths: []byte(`[]`), AllowedSecrets: []byte(`[]`), CommandArgv: []byte(`["true"]`),
				MaxRuntimeSeconds: 60,
			}
			runOperation := &domain.DevMachineOperation{
				ID: uuid.New(), MachineID: machineID, WorkspaceID: workspaceID, Action: domain.DevMachineOpRunAgent,
				Status: domain.DevMachineOpStatusPending, Generation: 1, IdempotencyKey: "run-agent:" + run.ID.String(),
				RequestedByUserID: &userID, MaxAttempts: 3,
			}
			lifecycleOperation := &domain.DevMachineOperation{
				ID: uuid.New(), MachineID: machineID, WorkspaceID: workspaceID, Action: test.action,
				Status: domain.DevMachineOpStatusPending, Generation: 2, IdempotencyKey: string(test.action) + ":2",
				RequestedByUserID: &userID, MaxAttempts: 5,
			}

			start := make(chan struct{})
			runResult, lifecycleResult := make(chan error, 1), make(chan error, 1)
			go func() {
				<-start
				runResult <- repo.CreateAgentRun(context.Background(), run, runOperation)
			}()
			go func() {
				<-start
				lifecycleResult <- repo.SetDesiredAndEnqueue(context.Background(), workspaceID, machineID, test.desired, lifecycleOperation)
			}()
			close(start)
			runErr, lifecycleErr := <-runResult, <-lifecycleResult
			require.NotEqual(t, runErr == nil, lifecycleErr == nil, "exactly one conflicting transaction must commit")

			var desired domain.DevMachineStatus
			var generation, runCount int
			require.NoError(t, db.QueryRow(`SELECT desired_status,generation FROM dev_machines WHERE id=$1`, machineID).Scan(&desired, &generation))
			require.NoError(t, db.Get(&runCount, `SELECT COUNT(*) FROM dev_machine_agent_runs WHERE machine_id=$1`, machineID))
			if runErr == nil {
				require.ErrorIs(t, lifecycleErr, ErrActiveAgentRun)
				require.Equal(t, domain.DevMachineStatusRunning, desired)
				require.Equal(t, 1, generation)
				require.Equal(t, 1, runCount)
			} else {
				require.ErrorIs(t, runErr, ErrMachineStateConflict)
				require.NoError(t, lifecycleErr)
				require.Equal(t, test.desired, desired)
				require.Equal(t, 2, generation)
				require.Zero(t, runCount)
			}
		})
	}
}

func TestListAgentRunsScansPersistedRows(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	var nullableRun domain.DevMachineAgentRun
	require.NoError(t, db.Get(&nullableRun, `SELECT NULL::jsonb AS test_command,NULL::jsonb AS result`))
	require.Nil(t, nullableRun.TestCommand)
	require.Nil(t, nullableRun.Result)

	var workspaceID, machineID uuid.UUID
	err = db.QueryRowx(`SELECT workspace_id,machine_id FROM dev_machine_agent_runs ORDER BY created_at DESC LIMIT 1`).Scan(&workspaceID, &machineID)
	if errors.Is(err, sql.ErrNoRows) {
		t.Skip("no persisted agent runs")
	}
	require.NoError(t, err)

	runs, total, err := NewDevMachineRepository(db).ListAgentRuns(context.Background(), workspaceID, &machineID, 50, 0)

	require.NoError(t, err)
	require.GreaterOrEqual(t, total, 1)
	require.NotEmpty(t, runs)
}

func TestListAgentRunStepsReturnsEmptyForMissingRun(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	steps, err := NewDevMachineRepository(db).ListAgentRunSteps(context.Background(), uuid.New())

	require.NoError(t, err)
	require.Empty(t, steps)
}

func TestListAgentRunEventsReturnsEmptyForMissingRun(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	events, err := NewDevMachineRepository(db).ListAgentRunEvents(context.Background(), uuid.New(), 0, 100)

	require.NoError(t, err)
	require.Empty(t, events)
}

func TestListAgentRunLogsReturnsEmptyForMissingRun(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	logs, err := NewDevMachineRepository(db).ListAgentRunLogs(context.Background(), uuid.New(), 0, 100)

	require.NoError(t, err)
	require.Empty(t, logs)
}

func TestListAgentRunLogsCursorPagination(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	// Find a run with log chunks
	var runID uuid.UUID
	err = db.QueryRowx(`SELECT DISTINCT agent_run_id FROM dev_machine_log_chunks WHERE agent_run_id IS NOT NULL LIMIT 1`).Scan(&runID)
	if errors.Is(err, sql.ErrNoRows) {
		t.Skip("no persisted agent-run log chunks")
	}
	require.NoError(t, err)

	repo := NewDevMachineRepository(db)

	// First page
	page1, err := repo.ListAgentRunLogs(context.Background(), runID, 0, 2)
	require.NoError(t, err)

	if len(page1) < 2 {
		t.Skip("not enough log chunks for cursor pagination test")
	}

	// Second page using cursor
	afterID := page1[len(page1)-1].ID
	page2, err := repo.ListAgentRunLogs(context.Background(), runID, afterID, 2)
	require.NoError(t, err)

	// Verify no overlap
	ids1 := make(map[int64]bool)
	for _, l := range page1 {
		ids1[l.ID] = true
	}
	for _, l := range page2 {
		require.False(t, ids1[l.ID], "cursor pagination returned duplicate log chunk %d", l.ID)
	}
}

func TestListAgentRunEventsCursorPagination(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	var runID uuid.UUID
	err = db.QueryRowx(`SELECT DISTINCT agent_run_id FROM dev_machine_events WHERE agent_run_id IS NOT NULL LIMIT 1`).Scan(&runID)
	if errors.Is(err, sql.ErrNoRows) {
		t.Skip("no persisted agent-run events")
	}
	require.NoError(t, err)

	repo := NewDevMachineRepository(db)

	page1, err := repo.ListAgentRunEvents(context.Background(), runID, 0, 2)
	require.NoError(t, err)

	if len(page1) < 2 {
		t.Skip("not enough events for cursor pagination test")
	}

	afterID := page1[len(page1)-1].ID
	page2, err := repo.ListAgentRunEvents(context.Background(), runID, afterID, 2)
	require.NoError(t, err)

	ids1 := make(map[int64]bool)
	for _, e := range page1 {
		ids1[e.ID] = true
	}
	for _, e := range page2 {
		require.False(t, ids1[e.ID], "cursor pagination returned duplicate event %d", e.ID)
	}
}

func TestCreateLogChunkOnConflictMatchesSchema(t *testing.T) {
	// Verify the ON CONFLICT in CreateLogChunk matches the database unique constraint:
	// UNIQUE NULLS NOT DISTINCT (machine_id, agent_run_id, service_id, stream, sequence)
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	var constraintDef string
	err = db.Get(&constraintDef, `
		SELECT pg_get_constraintdef(c.oid)
		FROM pg_constraint c
		JOIN pg_class t ON t.oid = c.conrelid
		WHERE t.relname = 'dev_machine_log_chunks'
		AND c.conname = 'dev_machine_log_chunks_run_sequence_key'
	`)
	require.NoError(t, err)
	require.Contains(t, constraintDef, "machine_id")
	require.Contains(t, constraintDef, "agent_run_id")
	require.Contains(t, constraintDef, "stream")
	require.Contains(t, constraintDef, "sequence")
}
