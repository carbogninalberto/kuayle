package repository

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// Saving or removing a sign-in queues one sign-in sync for each running
// machine the user owns in that workspace: never for another user's machine,
// a machine in another workspace or one that is not running, and repeated
// changes before the manager picks it up converge on one pending sync.
func TestAgentSignInChangesQueueASyncForTheOwnersRunningMachines(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	fixture := newEnvironmentRaceFixture(t, db)
	elsewhere := newEnvironmentRaceFixture(t, db)
	store := fixture.repository

	owned := fixture.insertMachine(t, domain.DevMachineStatusRunning, domain.DevMachineStatusRunning, false)
	stopped := fixture.insertMachine(t, domain.DevMachineStatusStopped, domain.DevMachineStatusStopped, false)
	other := uuid.New()
	_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Other','test')`, other, other.String()+"@example.test")
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, other) })
	othersMachine := fixture.insertMachine(t, domain.DevMachineStatusRunning, domain.DevMachineStatusRunning, false)
	_, err = db.Exec(`UPDATE dev_machines SET created_by_user_id=$2 WHERE id=$1`, othersMachine, other)
	require.NoError(t, err)
	// The same user's machine in another workspace.
	_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'member')`, elsewhere.workspaceID, fixture.userID)
	require.NoError(t, err)
	foreign := elsewhere.insertMachine(t, domain.DevMachineStatusRunning, domain.DevMachineStatusRunning, false)
	_, err = db.Exec(`UPDATE dev_machines SET created_by_user_id=$2 WHERE id=$1`, foreign, fixture.userID)
	require.NoError(t, err)

	syncs := func(machine uuid.UUID) []domain.DevMachineOperation {
		t.Helper()
		var operations []domain.DevMachineOperation
		require.NoError(t, db.Select(&operations, `SELECT * FROM dev_machine_operations WHERE machine_id=$1 AND action='sync_agent_sign_ins'`, machine))
		return operations
	}

	require.NoError(t, store.SaveAgentSignInToken(ctx, fixture.workspaceID, fixture.userID, "claude-code", "sealed-token", time.Now().Add(time.Hour)))
	require.NoError(t, store.SaveAgentSignIn(ctx, fixture.workspaceID, fixture.userID, "codex", "sealed-file"))
	queued := syncs(owned)
	require.Len(t, queued, 1, "changes before the manager runs converge on one sync")
	require.Equal(t, domain.DevMachineOpStatusPending, queued[0].Status)
	require.Equal(t, int64(1), queued[0].Generation)
	require.Equal(t, fixture.userID, *queued[0].RequestedByUserID)
	require.True(t, strings.HasPrefix(queued[0].IdempotencyKey, "agent-sign-ins:"))
	require.Empty(t, syncs(stopped), "a machine that is not running syncs when it starts")
	require.Empty(t, syncs(othersMachine), "another user's machine never gets this user's sign-in")
	require.Empty(t, syncs(foreign), "a machine in another workspace never gets this workspace's sign-in")

	// Once the manager has taken the sync, a later change queues a new one.
	_, err = db.Exec(`UPDATE dev_machine_operations SET status='completed' WHERE id=$1`, queued[0].ID)
	require.NoError(t, err)
	removed, err := store.DeleteAgentSignIns(ctx, fixture.workspaceID, fixture.userID, "claude-code", "")
	require.NoError(t, err)
	require.Equal(t, int64(1), removed)
	require.Len(t, syncs(owned), 2, "removing a sign-in syncs the terminal too")

	// Removing nothing changes nothing.
	_, err = db.Exec(`UPDATE dev_machine_operations SET status='completed' WHERE machine_id=$1`, owned)
	require.NoError(t, err)
	_, err = store.DeleteAgentSignIns(ctx, fixture.workspaceID, fixture.userID, "claude-code", "")
	require.NoError(t, err)
	require.Len(t, syncs(owned), 2)
}
