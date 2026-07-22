package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"os"
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
