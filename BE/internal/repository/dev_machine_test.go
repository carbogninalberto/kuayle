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

func TestUpdateAgentRunStartedRequiresStateTransition(t *testing.T) {
	repo, conn := newCaptureDevMachineRepository(t, 0)

	err := repo.UpdateAgentRunStarted(context.Background(), uuid.New())

	require.ErrorIs(t, err, sql.ErrNoRows)
	query, _ := conn.captured()
	require.Contains(t, query, "status IN ('queued','starting')")

	repo, _ = newCaptureDevMachineRepository(t, 1)
	require.NoError(t, repo.UpdateAgentRunStarted(context.Background(), uuid.New()))
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

func TestDevMachineOffsetPaginationUsesStableTieBreakers(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	userID, workspaceID := uuid.New(), uuid.New()
	suffix := strings.ReplaceAll(workspaceID.String(), "-", "")
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, workspaceID)
		_, _ = db.Exec(`DELETE FROM users WHERE id=$1`, userID)
	})
	_, err = db.Exec(`INSERT INTO users (id,email,name,password_hash) VALUES ($1,$2,'Pagination Test','test')`,
		userID, suffix+"@example.test")
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO workspaces (id,name,slug,owner_id) VALUES ($1,'Pagination Test',$2,$3)`,
		workspaceID, "pagination-"+suffix, userID)
	require.NoError(t, err)

	machinePrefix := uuid.New()
	machineIDs := make([]uuid.UUID, 4)
	for index, suffix := range []byte{2, 4, 1, 3} {
		machineIDs[index] = machinePrefix
		machineIDs[index][15] = suffix
	}
	createdAt := time.Now().UTC().Truncate(time.Second)
	for index, machineID := range machineIDs {
		routingKey := strings.ReplaceAll(machineID.String(), "-", "")
		_, err = db.Exec(`INSERT INTO dev_machines
			(id,workspace_id,created_by_user_id,routing_key,name,status,desired_status,generation,
			 repo_url,repo_provider,repo_owner,repo_name,base_branch,working_branch,
			 machine_size,cpu_millis,memory_mb,disk_gb,max_runtime_minutes,created_at,updated_at,expires_at)
			VALUES ($1,$2,$3,$4,$5,'stopped','stopped',1,'','github','','','','',
			 'small',1000,2048,20,480,$6::timestamptz,$6::timestamptz,$6::timestamptz+INTERVAL '1 hour')`,
			machineID, workspaceID, userID, routingKey[len(routingKey)-16:], fmt.Sprintf("pagination-machine-%d", index), createdAt)
		require.NoError(t, err)
	}

	repo := NewDevMachineRepository(db)
	expectedMachineIDs := []uuid.UUID{machineIDs[1], machineIDs[3], machineIDs[0], machineIDs[2]}
	assertMachinePages := func(t *testing.T, list func(int, int) ([]domain.DevMachine, int, error)) {
		t.Helper()
		var actual []uuid.UUID
		for offset := 0; offset < len(expectedMachineIDs); offset += 2 {
			page, total, listErr := list(2, offset)
			require.NoError(t, listErr)
			require.Equal(t, len(expectedMachineIDs), total)
			require.Len(t, page, 2)
			for _, machine := range page {
				actual = append(actual, machine.ID)
			}
		}
		require.Equal(t, expectedMachineIDs, actual)
		require.Len(t, uniqueUUIDs(actual), len(expectedMachineIDs), "adjacent pages must not contain duplicates")
	}

	t.Run("machines", func(t *testing.T) {
		assertMachinePages(t, func(limit, offset int) ([]domain.DevMachine, int, error) {
			return repo.ListMachines(context.Background(), workspaceID, "", nil, limit, offset)
		})
	})
	t.Run("machines for user", func(t *testing.T) {
		assertMachinePages(t, func(limit, offset int) ([]domain.DevMachine, int, error) {
			return repo.ListMachinesForUser(context.Background(), workspaceID, userID, "", nil, limit, offset)
		})
	})

	runPrefix := uuid.New()
	runIDs := make([]uuid.UUID, 4)
	for index, suffix := range []byte{2, 4, 1, 3} {
		runIDs[index] = runPrefix
		runIDs[index][15] = suffix
	}
	for _, runID := range runIDs {
		_, err = db.Exec(`INSERT INTO dev_machine_agent_runs
			(id,machine_id,workspace_id,requested_by_user_id,provider_id,mode,status,prompt,
			 command_argv,max_runtime_seconds,created_at)
			VALUES ($1,$2,$3,$4,'opencode','autonomous','succeeded','pagination test','["true"]',60,$5)`,
			runID, machineIDs[0], workspaceID, userID, createdAt)
		require.NoError(t, err)
	}

	expectedRunIDs := []uuid.UUID{runIDs[1], runIDs[3], runIDs[0], runIDs[2]}
	assertRunPages := func(t *testing.T, list func(int, int) ([]domain.DevMachineAgentRun, int, error)) {
		t.Helper()
		var actual []uuid.UUID
		for offset := 0; offset < len(expectedRunIDs); offset += 2 {
			page, total, listErr := list(2, offset)
			require.NoError(t, listErr)
			require.Equal(t, len(expectedRunIDs), total)
			require.Len(t, page, 2)
			for _, run := range page {
				actual = append(actual, run.ID)
			}
		}
		require.Equal(t, expectedRunIDs, actual)
		require.Len(t, uniqueUUIDs(actual), len(expectedRunIDs), "adjacent pages must not contain duplicates")
	}

	t.Run("agent runs", func(t *testing.T) {
		assertRunPages(t, func(limit, offset int) ([]domain.DevMachineAgentRun, int, error) {
			return repo.ListAgentRuns(context.Background(), workspaceID, nil, limit, offset)
		})
	})
	t.Run("agent runs for user", func(t *testing.T) {
		assertRunPages(t, func(limit, offset int) ([]domain.DevMachineAgentRun, int, error) {
			return repo.ListAgentRunsForUser(context.Background(), workspaceID, userID, nil, limit, offset)
		})
	})
}

func uniqueUUIDs(ids []uuid.UUID) map[uuid.UUID]struct{} {
	unique := make(map[uuid.UUID]struct{}, len(ids))
	for _, id := range ids {
		unique[id] = struct{}{}
	}
	return unique
}

func TestAgentRunCreationSerializesWithLifecycle(t *testing.T) {
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
		{name: "teardown", action: domain.DevMachineOpTeardown, desired: domain.DevMachineStatusDestroyed},
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

type environmentRaceFixture struct {
	repository    *DevMachineRepository
	db            *sqlx.DB
	userID        uuid.UUID
	workspaceID   uuid.UUID
	environmentID uuid.UUID
}

func newEnvironmentRaceFixture(t *testing.T, db *sqlx.DB) environmentRaceFixture {
	t.Helper()
	fixture := environmentRaceFixture{
		repository: NewDevMachineRepository(db), db: db,
		userID: uuid.New(), workspaceID: uuid.New(), environmentID: uuid.New(),
	}
	suffix := strings.ReplaceAll(fixture.environmentID.String(), "-", "")
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, fixture.workspaceID)
		_, _ = db.Exec(`DELETE FROM users WHERE id=$1`, fixture.userID)
	})
	_, err := db.Exec(`INSERT INTO users (id,email,name,password_hash) VALUES ($1,$2,'Environment Race','test')`,
		fixture.userID, suffix+"@example.test")
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO workspaces (id,name,slug,owner_id) VALUES ($1,'Environment Race',$2,$3)`,
		fixture.workspaceID, "environment-race-"+suffix, fixture.userID)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO workspace_members (workspace_id,user_id,role) VALUES ($1,$2,'owner')`, fixture.workspaceID, fixture.userID)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO dev_machine_workspace_policies (workspace_id,enabled) VALUES ($1,TRUE)`, fixture.workspaceID)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO dev_machine_environments
		(id,workspace_id,name,image_ref,status,created_by_user_id) VALUES ($1,$2,$3,$4,'ready',$5)`,
		fixture.environmentID, fixture.workspaceID, "environment-"+suffix, "sha256:"+strings.Repeat("a", 64), fixture.userID)
	require.NoError(t, err)
	return fixture
}

func (f environmentRaceFixture) insertMachine(t *testing.T, status, desired domain.DevMachineStatus, builder bool) uuid.UUID {
	t.Helper()
	machineID := uuid.New()
	suffix := strings.ReplaceAll(machineID.String(), "-", "")
	_, err := f.db.Exec(`INSERT INTO dev_machines
		(id,workspace_id,created_by_user_id,routing_key,name,status,desired_status,generation,
		 repo_url,repo_provider,repo_owner,repo_name,base_branch,working_branch,
		 machine_size,cpu_millis,memory_mb,disk_gb,max_runtime_minutes,expires_at,environment_id,environment_builder)
		VALUES ($1,$2,$3,$4,$5,$6,$7,1,'','github','','','','','small',1000,2048,20,480,
		 NOW()+INTERVAL '1 hour',$8,$9)`, machineID, f.workspaceID, f.userID, suffix[:16], "machine-"+suffix,
		status, desired, f.environmentID, builder)
	require.NoError(t, err)
	return machineID
}

func runEnvironmentRace(left, right func() error) (error, error) {
	start := make(chan struct{})
	leftResult, rightResult := make(chan error, 1), make(chan error, 1)
	go func() {
		<-start
		leftResult <- left()
	}()
	go func() {
		<-start
		rightResult <- right()
	}()
	close(start)
	return <-leftResult, <-rightResult
}

func TestEnvironmentDeletionSerializesWithMachineCreation(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	fixture := newEnvironmentRaceFixture(t, db)
	machineID := uuid.New()
	suffix := strings.ReplaceAll(machineID.String(), "-", "")
	machine := &domain.DevMachine{
		ID: machineID, WorkspaceID: fixture.workspaceID, CreatedByUserID: &fixture.userID,
		RoutingKey: suffix[:16], Name: "machine-" + suffix, Status: domain.DevMachineStatusQueued,
		DesiredStatus: domain.DevMachineStatusRunning, Generation: 1, RepoProvider: "github",
		MachineSize: "small", CPUMillis: 1000, MemoryMB: 2048, DiskGB: 20, PidsLimit: 512,
		MaxRuntimeMinutes: 480, ServicesConfig: []byte(`{}`), Labels: []byte(`{}`),
		ExpiresAt: time.Now().Add(time.Hour), EnvironmentID: &fixture.environmentID,
	}
	operation := &domain.DevMachineOperation{
		ID: uuid.New(), MachineID: machineID, WorkspaceID: fixture.workspaceID, Action: domain.DevMachineOpSpawn,
		Status: domain.DevMachineOpStatusPending, Generation: 1, IdempotencyKey: "spawn:1",
		RequestedByUserID: &fixture.userID, MaxAttempts: 5,
	}

	createErr, deleteErr := runEnvironmentRace(
		func() error {
			return fixture.repository.CreateBundle(context.Background(), machine, nil, nil, nil, nil, nil, operation)
		},
		func() error {
			return fixture.repository.RequestEnvironmentDeletion(context.Background(), fixture.workspaceID, fixture.environmentID)
		},
	)
	require.NotEqual(t, createErr == nil, deleteErr == nil, "exactly one conflicting transaction must commit")

	var environmentStatus string
	var machineCount int
	require.NoError(t, db.Get(&environmentStatus, `SELECT status FROM dev_machine_environments WHERE id=$1`, fixture.environmentID))
	require.NoError(t, db.Get(&machineCount, `SELECT COUNT(*) FROM dev_machines WHERE id=$1`, machineID))
	if createErr == nil {
		require.ErrorIs(t, deleteErr, ErrEnvironmentInUse)
		require.Equal(t, "ready", environmentStatus)
		require.Equal(t, 1, machineCount)
	} else {
		require.ErrorIs(t, createErr, ErrEnvironmentUnavailable)
		require.NoError(t, deleteErr)
		require.Equal(t, "delete_requested", environmentStatus)
		require.Zero(t, machineCount)
	}
}

func TestEnvironmentDeletionSerializesWithStartAndRecovery(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	for _, test := range []struct {
		name          string
		status        domain.DevMachineStatus
		desired       domain.DevMachineStatus
		action        domain.DevMachineOperationAction
		resultDesired domain.DevMachineStatus
	}{
		{name: "start", status: domain.DevMachineStatusStopped, desired: domain.DevMachineStatusStopped, action: domain.DevMachineOpStart, resultDesired: domain.DevMachineStatusRunning},
		{name: "failed recovery", status: domain.DevMachineStatusFailed, desired: domain.DevMachineStatusFailed, action: domain.DevMachineOpStart, resultDesired: domain.DevMachineStatusRunning},
		{name: "expired recovery", status: domain.DevMachineStatusExpired, desired: domain.DevMachineStatusExpired, action: domain.DevMachineOpReconcile, resultDesired: domain.DevMachineStatusRunning},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newEnvironmentRaceFixture(t, db)
			machineID := fixture.insertMachine(t, test.status, test.desired, false)
			operation := &domain.DevMachineOperation{
				ID: uuid.New(), MachineID: machineID, WorkspaceID: fixture.workspaceID, Action: test.action,
				Status: domain.DevMachineOpStatusPending, Generation: 2, IdempotencyKey: string(test.action) + ":2",
				RequestedByUserID: &fixture.userID, MaxAttempts: 5,
			}

			lifecycleErr, deleteErr := runEnvironmentRace(
				func() error {
					return fixture.repository.SetDesiredAndEnqueue(context.Background(), fixture.workspaceID, machineID, test.resultDesired, operation)
				},
				func() error {
					return fixture.repository.RequestEnvironmentDeletion(context.Background(), fixture.workspaceID, fixture.environmentID)
				},
			)
			require.NoError(t, lifecycleErr)
			require.ErrorIs(t, deleteErr, ErrEnvironmentInUse)

			var environmentStatus string
			var desired domain.DevMachineStatus
			require.NoError(t, db.Get(&environmentStatus, `SELECT status FROM dev_machine_environments WHERE id=$1`, fixture.environmentID))
			require.NoError(t, db.Get(&desired, `SELECT desired_status FROM dev_machines WHERE id=$1`, machineID))
			require.Equal(t, "ready", environmentStatus)
			require.Equal(t, test.resultDesired, desired)
		})
	}
}

func TestEnvironmentDeletionSerializesWithScopeSelection(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	fixture := newEnvironmentRaceFixture(t, db)
	setting := &domain.DevMachineScopeSetting{WorkspaceID: fixture.workspaceID, EnvironmentID: &fixture.environmentID}

	settingErr, deleteErr := runEnvironmentRace(
		func() error { return fixture.repository.UpsertScopeSetting(context.Background(), setting) },
		func() error {
			return fixture.repository.RequestEnvironmentDeletion(context.Background(), fixture.workspaceID, fixture.environmentID)
		},
	)
	require.NotEqual(t, settingErr == nil, deleteErr == nil, "exactly one conflicting transaction must commit")

	var environmentStatus string
	var settingCount int
	require.NoError(t, db.Get(&environmentStatus, `SELECT status FROM dev_machine_environments WHERE id=$1`, fixture.environmentID))
	require.NoError(t, db.Get(&settingCount, `SELECT COUNT(*) FROM dev_machine_scope_settings WHERE environment_id=$1`, fixture.environmentID))
	if settingErr == nil {
		require.ErrorIs(t, deleteErr, ErrEnvironmentInUse)
		require.Equal(t, "ready", environmentStatus)
		require.Equal(t, 1, settingCount)
	} else {
		require.ErrorIs(t, settingErr, ErrEnvironmentUnavailable)
		require.NoError(t, deleteErr)
		require.Equal(t, "delete_requested", environmentStatus)
		require.Zero(t, settingCount)
	}
}

func TestScopeSettingUpsertPreservesIdentityUnderConcurrentWrites(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	fixture := newEnvironmentRaceFixture(t, db)
	teamID, statusID, projectID, issueID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	_, err = db.Exec(`INSERT INTO teams (id,workspace_id,name,key) VALUES ($1,$2,'Scope Test','SCP')`, teamID, fixture.workspaceID)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO team_statuses (id,team_id,name,slug,category,is_default)
		VALUES ($1,$2,'Todo','todo','unstarted',TRUE)`, statusID, teamID)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO projects (id,workspace_id,name) VALUES ($1,$2,'Scope Project')`, projectID, fixture.workspaceID)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO issues
		(id,workspace_id,team_id,number,identifier_text,title,creator_id,status_id)
		VALUES ($1,$2,$3,1,'SCP-1','Scope issue',$4,$5)`, issueID, fixture.workspaceID, teamID, fixture.userID, statusID)
	require.NoError(t, err)

	stringPointer := func(value string) *string { return &value }
	for _, test := range []struct {
		name                       string
		teamID, projectID, issueID *uuid.UUID
	}{
		{name: "workspace"},
		{name: "team", teamID: &teamID},
		{name: "project", projectID: &projectID},
		{name: "issue", issueID: &issueID},
	} {
		t.Run(test.name, func(t *testing.T) {
			settings := []*domain.DevMachineScopeSetting{
				{ID: uuid.New(), WorkspaceID: fixture.workspaceID, TeamID: test.teamID, ProjectID: test.projectID, IssueID: test.issueID, BaseBranch: stringPointer("first")},
				{ID: uuid.New(), WorkspaceID: fixture.workspaceID, TeamID: test.teamID, ProjectID: test.projectID, IssueID: test.issueID, BaseBranch: stringPointer("second")},
			}
			candidateIDs := []uuid.UUID{settings[0].ID, settings[1].ID}
			start := make(chan struct{})
			results := make(chan error, len(settings))
			for index := range settings {
				setting := settings[index]
				go func() {
					<-start
					results <- fixture.repository.UpsertScopeSetting(context.Background(), setting)
				}()
			}
			close(start)
			for range settings {
				require.NoError(t, <-results)
			}
			require.Equal(t, settings[0].ID, settings[1].ID)
			require.Contains(t, candidateIDs, settings[0].ID)
			require.Equal(t, settings[0].CreatedAt, settings[1].CreatedAt)

			var count int
			require.NoError(t, db.Get(&count, `SELECT COUNT(*) FROM dev_machine_scope_settings
				WHERE workspace_id=$1 AND team_id IS NOT DISTINCT FROM $2 AND project_id IS NOT DISTINCT FROM $3
				AND issue_id IS NOT DISTINCT FROM $4`, fixture.workspaceID, test.teamID, test.projectID, test.issueID))
			require.Equal(t, 1, count)

			time.Sleep(2 * time.Millisecond)
			updated := &domain.DevMachineScopeSetting{
				ID: uuid.New(), WorkspaceID: fixture.workspaceID, TeamID: test.teamID, ProjectID: test.projectID, IssueID: test.issueID,
				BaseBranch: stringPointer("updated"), EnvironmentID: &fixture.environmentID,
			}
			require.NoError(t, fixture.repository.UpsertScopeSetting(context.Background(), updated))
			require.Equal(t, settings[0].ID, updated.ID)
			require.Equal(t, settings[0].CreatedAt, updated.CreatedAt)
			require.True(t, updated.UpdatedAt.After(settings[1].UpdatedAt))

			stored, err := fixture.repository.GetScopeSetting(context.Background(), fixture.workspaceID, test.teamID, test.projectID, test.issueID)
			require.NoError(t, err)
			require.NotNil(t, stored)
			require.Equal(t, updated.ID, stored.ID)
			require.Equal(t, "updated", *stored.BaseBranch)
			require.Equal(t, fixture.environmentID, *stored.EnvironmentID)
		})
	}
}

func TestEnvironmentDeletionSerializesWithBuilderSnapshot(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	fixture := newEnvironmentRaceFixture(t, db)
	machineID := fixture.insertMachine(t, domain.DevMachineStatusStopped, domain.DevMachineStatusStopped, true)
	snapshotID := uuid.New()
	environment := &domain.DevMachineEnvironment{
		ID: snapshotID, WorkspaceID: fixture.workspaceID, Name: "snapshot-" + snapshotID.String(),
		ImageRef: "snapshot:" + snapshotID.String(), Status: "pending", SourceMachineID: &machineID,
		CreatedByUserID: &fixture.userID,
	}
	operation := &domain.DevMachineOperation{
		ID: uuid.New(), MachineID: machineID, EnvironmentID: &snapshotID, WorkspaceID: fixture.workspaceID,
		Action: domain.DevMachineOpSnapshotEnvironment, Status: domain.DevMachineOpStatusPending,
		Generation: 1, IdempotencyKey: "snapshot:" + snapshotID.String(), RequestedByUserID: &fixture.userID, MaxAttempts: 2,
	}

	snapshotErr, deleteErr := runEnvironmentRace(
		func() error {
			return fixture.repository.CreateEnvironment(context.Background(), environment, operation)
		},
		func() error {
			return fixture.repository.RequestEnvironmentDeletion(context.Background(), fixture.workspaceID, fixture.environmentID)
		},
	)
	require.NoError(t, snapshotErr)
	require.ErrorIs(t, deleteErr, ErrEnvironmentInUse)

	var snapshotCount int
	require.NoError(t, db.Get(&snapshotCount, `SELECT COUNT(*) FROM dev_machine_environments WHERE id=$1 AND status='pending'`, snapshotID))
	require.Equal(t, 1, snapshotCount)
}

func TestEnvironmentDeletionChecksDesiredStateAndPendingOperations(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	t.Run("desired recovery", func(t *testing.T) {
		fixture := newEnvironmentRaceFixture(t, db)
		fixture.insertMachine(t, domain.DevMachineStatusDestroyed, domain.DevMachineStatusRunning, false)

		err := fixture.repository.RequestEnvironmentDeletion(context.Background(), fixture.workspaceID, fixture.environmentID)

		require.ErrorIs(t, err, ErrEnvironmentInUse)
	})

	t.Run("pending lifecycle operation", func(t *testing.T) {
		fixture := newEnvironmentRaceFixture(t, db)
		machineID := fixture.insertMachine(t, domain.DevMachineStatusDestroyed, domain.DevMachineStatusDestroyed, false)
		operation := &domain.DevMachineOperation{
			ID: uuid.New(), MachineID: machineID, WorkspaceID: fixture.workspaceID, Action: domain.DevMachineOpStart,
			Status: domain.DevMachineOpStatusPending, Generation: 2, IdempotencyKey: "start:2", MaxAttempts: 5,
		}
		require.NoError(t, fixture.repository.EnqueueInternalOperation(context.Background(), operation))

		err := fixture.repository.RequestEnvironmentDeletion(context.Background(), fixture.workspaceID, fixture.environmentID)

		require.ErrorIs(t, err, ErrEnvironmentInUse)
	})
}

func TestEnvironmentDeletionCancelsStuckSnapshotOperations(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	for _, test := range []struct {
		name              string
		environmentStatus string
		leaseExpiresAt    *time.Time
		wantConflict      bool
	}{
		{name: "pending operation", environmentStatus: "pending"},
		{name: "expired build lease", environmentStatus: "building", leaseExpiresAt: dmTimePtr(time.Now().Add(-time.Minute))},
		{name: "active build lease", environmentStatus: "building", leaseExpiresAt: dmTimePtr(time.Now().Add(time.Minute)), wantConflict: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newEnvironmentRaceFixture(t, db)
			_, err := db.Exec(`UPDATE dev_machine_environments SET status=$2 WHERE id=$1`, fixture.environmentID, test.environmentStatus)
			require.NoError(t, err)
			machineID := fixture.insertMachine(t, domain.DevMachineStatusStopped, domain.DevMachineStatusStopped, true)
			_, err = db.Exec(`UPDATE dev_machines SET environment_id=NULL WHERE id=$1`, machineID)
			require.NoError(t, err)
			operation := &domain.DevMachineOperation{
				ID: uuid.New(), MachineID: machineID, EnvironmentID: &fixture.environmentID, WorkspaceID: fixture.workspaceID,
				Action: domain.DevMachineOpSnapshotEnvironment, Status: domain.DevMachineOpStatusPending,
				Generation: 1, IdempotencyKey: "snapshot:" + fixture.environmentID.String(), MaxAttempts: 2,
			}
			require.NoError(t, fixture.repository.EnqueueInternalOperation(context.Background(), operation))
			if test.leaseExpiresAt != nil {
				_, err = db.Exec(`UPDATE dev_machine_operations SET status='leased',lease_owner='worker',lease_expires_at=$2 WHERE id=$1`, operation.ID, test.leaseExpiresAt)
				require.NoError(t, err)
			}

			err = fixture.repository.RequestEnvironmentDeletion(context.Background(), fixture.workspaceID, fixture.environmentID)

			var environmentStatus string
			var operationStatus domain.DevMachineOperationStatus
			require.NoError(t, db.Get(&environmentStatus, `SELECT status FROM dev_machine_environments WHERE id=$1`, fixture.environmentID))
			require.NoError(t, db.Get(&operationStatus, `SELECT status FROM dev_machine_operations WHERE id=$1`, operation.ID))
			if test.wantConflict {
				require.ErrorIs(t, err, ErrEnvironmentDeletionConflict)
				require.Equal(t, "building", environmentStatus)
				require.Equal(t, domain.DevMachineOpStatusLeased, operationStatus)
				return
			}
			require.NoError(t, err)
			require.Equal(t, "delete_requested", environmentStatus)
			require.Equal(t, domain.DevMachineOpStatusCancelled, operationStatus)

			require.ErrorIs(t, fixture.repository.UpdateEnvironmentState(context.Background(), fixture.environmentID, "ready", "sha256:late", nil), ErrEnvironmentDeletionConflict)
			require.NoError(t, db.Get(&environmentStatus, `SELECT status FROM dev_machine_environments WHERE id=$1`, fixture.environmentID))
			require.Equal(t, "delete_requested", environmentStatus)
		})
	}
}

func dmTimePtr(value time.Time) *time.Time {
	return &value
}

func TestSnapshotCreationSerializesWithResume(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	fixture := newEnvironmentRaceFixture(t, db)
	machineID := fixture.insertMachine(t, domain.DevMachineStatusPaused, domain.DevMachineStatusPaused, true)
	snapshotID := uuid.New()
	environment := &domain.DevMachineEnvironment{
		ID: snapshotID, WorkspaceID: fixture.workspaceID, Name: "snapshot-" + snapshotID.String(),
		ImageRef: "snapshot:" + snapshotID.String(), Status: "pending", SourceMachineID: &machineID,
		CreatedByUserID: &fixture.userID,
	}
	snapshotOperation := &domain.DevMachineOperation{
		ID: uuid.New(), MachineID: machineID, EnvironmentID: &snapshotID, WorkspaceID: fixture.workspaceID,
		Action: domain.DevMachineOpSnapshotEnvironment, Status: domain.DevMachineOpStatusPending,
		Generation: 1, IdempotencyKey: "snapshot:" + snapshotID.String(), RequestedByUserID: &fixture.userID, MaxAttempts: 2,
	}
	resumeOperation := &domain.DevMachineOperation{
		ID: uuid.New(), MachineID: machineID, WorkspaceID: fixture.workspaceID, Action: domain.DevMachineOpStart,
		Status: domain.DevMachineOpStatusPending, Generation: 2, IdempotencyKey: "start:2",
		RequestedByUserID: &fixture.userID, MaxAttempts: 5,
	}

	snapshotErr, resumeErr := runEnvironmentRace(
		func() error {
			return fixture.repository.CreateEnvironment(context.Background(), environment, snapshotOperation)
		},
		func() error {
			return fixture.repository.SetDesiredAndEnqueue(
				context.Background(), fixture.workspaceID, machineID, domain.DevMachineStatusRunning, resumeOperation,
			)
		},
	)
	require.NotEqual(t, snapshotErr == nil, resumeErr == nil, "exactly one conflicting transaction must commit")

	var snapshotCount int
	var desired domain.DevMachineStatus
	require.NoError(t, db.Get(&snapshotCount, `SELECT COUNT(*) FROM dev_machine_environments WHERE id=$1`, snapshotID))
	require.NoError(t, db.Get(&desired, `SELECT desired_status FROM dev_machines WHERE id=$1`, machineID))
	if snapshotErr == nil {
		require.ErrorIs(t, resumeErr, ErrMachineStateConflict)
		require.Equal(t, 1, snapshotCount)
		require.Equal(t, domain.DevMachineStatusPaused, desired)
	} else {
		require.ErrorIs(t, snapshotErr, ErrMachineStateConflict)
		require.NoError(t, resumeErr)
		require.Zero(t, snapshotCount)
		require.Equal(t, domain.DevMachineStatusRunning, desired)
	}
}

func TestFailOperationHonorsRetryableFlag(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	fixture := newEnvironmentRaceFixture(t, db)
	machineID := fixture.insertMachine(t, domain.DevMachineStatusPaused, domain.DevMachineStatusPaused, false)
	operation := &domain.DevMachineOperation{
		ID: uuid.New(), MachineID: machineID, WorkspaceID: fixture.workspaceID,
		Action: domain.DevMachineOpReconcile, Status: domain.DevMachineOpStatusPending,
		Generation: 1, IdempotencyKey: "reconcile:1", MaxAttempts: 5,
	}
	require.NoError(t, fixture.repository.EnqueueInternalOperation(context.Background(), operation))
	_, err = db.Exec(`UPDATE dev_machine_operations SET status='leased',lease_owner='worker',attempts=1 WHERE id=$1`, operation.ID)
	require.NoError(t, err)

	retry, err := fixture.repository.FailOperation(
		context.Background(), operation.ID, "worker", "snapshot_stale", "machine generation changed", false,
	)
	require.NoError(t, err)
	require.False(t, retry)

	var status domain.DevMachineOperationStatus
	var completedAt *time.Time
	require.NoError(t, db.QueryRow(`SELECT status,completed_at FROM dev_machine_operations WHERE id=$1`, operation.ID).Scan(&status, &completedAt))
	require.Equal(t, domain.DevMachineOpStatusFailed, status)
	require.NotNil(t, completedAt)

	_, err = db.Exec(`UPDATE dev_machine_operations SET status='leased',lease_owner='worker',attempts=1,completed_at=NULL WHERE id=$1`, operation.ID)
	require.NoError(t, err)
	retry, err = fixture.repository.FailOperation(
		context.Background(), operation.ID, "worker", "runtime_error", "temporary failure", true,
	)
	require.NoError(t, err)
	require.True(t, retry)
	require.NoError(t, db.QueryRow(`SELECT status,completed_at FROM dev_machine_operations WHERE id=$1`, operation.ID).Scan(&status, &completedAt))
	require.Equal(t, domain.DevMachineOpStatusPending, status)
	require.Nil(t, completedAt)
}

func TestReconcileOrphanedEnvironments(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	fixture := newEnvironmentRaceFixture(t, db)
	machineID := fixture.insertMachine(t, domain.DevMachineStatusStopped, domain.DevMachineStatusStopped, true)
	_, err = db.Exec(`UPDATE dev_machines SET environment_id=NULL WHERE id=$1`, machineID)
	require.NoError(t, err)

	insertEnvironment := func(status string) uuid.UUID {
		environmentID := uuid.New()
		_, insertErr := db.Exec(`INSERT INTO dev_machine_environments
			(id,workspace_id,name,image_ref,status,source_machine_id,created_by_user_id)
			VALUES ($1,$2,$3,$4,$5,$6,$7)`, environmentID, fixture.workspaceID,
			"orphan-"+environmentID.String(), "snapshot:"+environmentID.String(), status, machineID, fixture.userID)
		require.NoError(t, insertErr)
		return environmentID
	}
	insertOperation := func(environmentID uuid.UUID, generation int64) uuid.UUID {
		operation := &domain.DevMachineOperation{
			ID: uuid.New(), MachineID: machineID, EnvironmentID: &environmentID, WorkspaceID: fixture.workspaceID,
			Action: domain.DevMachineOpSnapshotEnvironment, Status: domain.DevMachineOpStatusPending,
			Generation: generation, IdempotencyKey: "snapshot:" + environmentID.String(), MaxAttempts: 2,
		}
		require.NoError(t, fixture.repository.EnqueueInternalOperation(context.Background(), operation))
		return operation.ID
	}

	missingOperationID := insertEnvironment("pending")
	terminalOperationID := insertEnvironment("pending")
	terminalID := insertOperation(terminalOperationID, 1)
	_, err = db.Exec(`UPDATE dev_machine_operations SET status='failed',completed_at=NOW() WHERE id=$1`, terminalID)
	require.NoError(t, err)
	supersededID := insertEnvironment("pending")
	supersededOperationID := insertOperation(supersededID, 2)
	expiredLeaseID := insertEnvironment("building")
	expiredOperationID := insertOperation(expiredLeaseID, 1)
	_, err = db.Exec(`UPDATE dev_machine_operations SET status='leased',lease_owner='old-worker',lease_expires_at=NOW()-INTERVAL '1 minute' WHERE id=$1`, expiredOperationID)
	require.NoError(t, err)
	validPendingID := insertEnvironment("pending")
	validOperationID := insertOperation(validPendingID, 1)
	activeLeaseID := insertEnvironment("building")
	activeOperationID := insertOperation(activeLeaseID, 2)
	_, err = db.Exec(`UPDATE dev_machine_operations SET status='leased',lease_owner='active-worker',lease_expires_at=NOW()+INTERVAL '1 minute' WHERE id=$1`, activeOperationID)
	require.NoError(t, err)

	count, err := fixture.repository.ReconcileOrphanedEnvironments(context.Background(), 100)

	require.NoError(t, err)
	require.Equal(t, 4, count)
	for _, environmentID := range []uuid.UUID{missingOperationID, terminalOperationID, supersededID, expiredLeaseID} {
		var status string
		require.NoError(t, db.Get(&status, `SELECT status FROM dev_machine_environments WHERE id=$1`, environmentID))
		require.Equal(t, "failed", status)
	}
	for environmentID, expected := range map[uuid.UUID]string{validPendingID: "pending", activeLeaseID: "building"} {
		var status string
		require.NoError(t, db.Get(&status, `SELECT status FROM dev_machine_environments WHERE id=$1`, environmentID))
		require.Equal(t, expected, status)
	}
	for operationID, expected := range map[uuid.UUID]domain.DevMachineOperationStatus{
		terminalID:            domain.DevMachineOpStatusFailed,
		supersededOperationID: domain.DevMachineOpStatusCancelled,
		expiredOperationID:    domain.DevMachineOpStatusCancelled,
		validOperationID:      domain.DevMachineOpStatusPending,
		activeOperationID:     domain.DevMachineOpStatusLeased,
	} {
		var status domain.DevMachineOperationStatus
		require.NoError(t, db.Get(&status, `SELECT status FROM dev_machine_operations WHERE id=$1`, operationID))
		require.Equal(t, expected, status)
	}
}

func TestReconcileOrphanedCheckouts(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	fixture := newEnvironmentRaceFixture(t, db)
	machineID := fixture.insertMachine(t, domain.DevMachineStatusRunning, domain.DevMachineStatusRunning, false)
	teamID, statusID, installationID, repositoryID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	_, err = db.Exec(`INSERT INTO teams (id,workspace_id,name,key) VALUES ($1,$2,'Checkout Test','CHK')`, teamID, fixture.workspaceID)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO team_statuses (id,team_id,name,slug,category,is_default)
		VALUES ($1,$2,'Todo','todo','unstarted',TRUE)`, statusID, teamID)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO github_installations
		(id,workspace_id,installation_id,account_login,account_type,installed_by)
		VALUES ($1,$2,$3,'checkout-test','Organization',$4)`, installationID, fixture.workspaceID, time.Now().UnixNano(), fixture.userID)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO github_repos
		(id,installation_id,workspace_id,github_repo_id,full_name,default_branch)
		VALUES ($1,$2,$3,$4,'kuayle/checkout-test','main')`, repositoryID, installationID, fixture.workspaceID, time.Now().UnixNano())
	require.NoError(t, err)

	issueNumber := 0
	insertCheckout := func(status string) (uuid.UUID, uuid.UUID) {
		issueNumber++
		issueID, checkoutID := uuid.New(), uuid.New()
		identifier := fmt.Sprintf("CHK-%d", issueNumber)
		_, insertErr := db.Exec(`INSERT INTO issues
			(id,workspace_id,team_id,number,identifier_text,title,creator_id,status_id)
			VALUES ($1,$2,$3,$4,$5,'Checkout test',$6,$7)`,
			issueID, fixture.workspaceID, teamID, issueNumber, identifier, fixture.userID, statusID)
		require.NoError(t, insertErr)
		_, insertErr = db.Exec(`INSERT INTO dev_machine_checkouts
			(id,workspace_id,machine_id,issue_id,github_repo_id,repository_full_name,base_branch,working_branch,workspace_path,status)
			VALUES ($1,$2,$3,$4,$5,'kuayle/checkout-test','main',$6,$7,$8)`,
			checkoutID, fixture.workspaceID, machineID, issueID, repositoryID,
			"kuayle/"+strings.ToLower(identifier), "/workspace/tasks/"+strings.ToLower(identifier), status)
		require.NoError(t, insertErr)
		return checkoutID, issueID
	}
	insertOperation := func(checkoutID uuid.UUID, status domain.DevMachineOperationStatus, generation int64, leaseExpiresAt *time.Time) uuid.UUID {
		operationID := uuid.New()
		var leaseOwner *string
		if status == domain.DevMachineOpStatusLeased {
			owner := "checkout-test"
			leaseOwner = &owner
		}
		_, insertErr := db.Exec(`INSERT INTO dev_machine_operations
			(id,machine_id,workspace_id,action,status,generation,idempotency_key,requested_by_user_id,checkout_id,lease_owner,lease_expires_at)
			VALUES ($1,$2,$3,'checkout_issue',$4,$5,$6,$7,$8,$9,$10)`,
			operationID, machineID, fixture.workspaceID, status, generation, "checkout:"+operationID.String(),
			fixture.userID, checkoutID, leaseOwner, leaseExpiresAt)
		require.NoError(t, insertErr)
		return operationID
	}

	missingID, missingIssueID := insertCheckout("queued")
	terminalCheckoutID, _ := insertCheckout("preparing")
	terminalOperationID := insertOperation(terminalCheckoutID, domain.DevMachineOpStatusFailed, 1, nil)
	supersededID, _ := insertCheckout("queued")
	supersededOperationID := insertOperation(supersededID, domain.DevMachineOpStatusPending, 2, nil)
	expiredLeaseID, _ := insertCheckout("preparing")
	expiredOperationID := insertOperation(expiredLeaseID, domain.DevMachineOpStatusLeased, 1, dmTimePtr(time.Now().Add(-time.Minute)))
	validPendingID, _ := insertCheckout("queued")
	validOperationID := insertOperation(validPendingID, domain.DevMachineOpStatusPending, 1, nil)
	activeLeaseID, _ := insertCheckout("preparing")
	activeOperationID := insertOperation(activeLeaseID, domain.DevMachineOpStatusLeased, 2, dmTimePtr(time.Now().Add(time.Minute)))

	count, err := fixture.repository.ReconcileOrphanedCheckouts(context.Background(), 100)

	require.NoError(t, err)
	require.Equal(t, 4, count)
	for _, checkoutID := range []uuid.UUID{missingID, terminalCheckoutID, supersededID, expiredLeaseID} {
		var status string
		var lastError *string
		require.NoError(t, db.QueryRow(`SELECT status,last_error FROM dev_machine_checkouts WHERE id=$1`, checkoutID).Scan(&status, &lastError))
		require.Equal(t, "failed", status)
		require.NotNil(t, lastError)
		require.Contains(t, *lastError, "try again")
	}
	for checkoutID, expected := range map[uuid.UUID]string{validPendingID: "queued", activeLeaseID: "preparing"} {
		var status string
		require.NoError(t, db.Get(&status, `SELECT status FROM dev_machine_checkouts WHERE id=$1`, checkoutID))
		require.Equal(t, expected, status)
	}
	for operationID, expected := range map[uuid.UUID]domain.DevMachineOperationStatus{
		terminalOperationID:   domain.DevMachineOpStatusFailed,
		supersededOperationID: domain.DevMachineOpStatusCancelled,
		expiredOperationID:    domain.DevMachineOpStatusCancelled,
		validOperationID:      domain.DevMachineOpStatusPending,
		activeOperationID:     domain.DevMachineOpStatusLeased,
	} {
		var status domain.DevMachineOperationStatus
		require.NoError(t, db.Get(&status, `SELECT status FROM dev_machine_operations WHERE id=$1`, operationID))
		require.Equal(t, expected, status)
	}

	retryCheckout := &domain.DevMachineCheckout{
		ID: uuid.New(), WorkspaceID: fixture.workspaceID, MachineID: machineID, IssueID: missingIssueID,
		GitHubRepoID: repositoryID, RepositoryFullName: "kuayle/checkout-test", BaseBranch: "main",
		WorkingBranch: "kuayle/chk-1", WorkspacePath: "/workspace/tasks/chk-1", Status: "queued",
	}
	retryOperation := &domain.DevMachineOperation{
		ID: uuid.New(), MachineID: machineID, WorkspaceID: fixture.workspaceID, Action: domain.DevMachineOpCheckoutIssue,
		Status: domain.DevMachineOpStatusPending, Generation: 1, IdempotencyKey: "checkout-retry:" + uuid.NewString(),
		RequestedByUserID: &fixture.userID, MaxAttempts: 3,
	}
	require.NoError(t, fixture.repository.CreateCheckout(context.Background(), retryCheckout, retryOperation))
	require.Equal(t, missingID, retryCheckout.ID)
	require.NotNil(t, retryOperation.CheckoutID)
	require.Equal(t, missingID, *retryOperation.CheckoutID)
	var retryStatus string
	var retryLastError *string
	require.NoError(t, db.QueryRow(`SELECT status,last_error FROM dev_machine_checkouts WHERE id=$1`, missingID).Scan(&retryStatus, &retryLastError))
	require.Equal(t, "queued", retryStatus)
	require.Nil(t, retryLastError)
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
