package repository

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// The sudo switch is off by default, round-trips through the policy without
// touching the Docker switch, and a machine keeps the choice it was created
// with after the switch changes.
func TestSudoSwitchAndMachineRecordPersist(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	fixture := newEnvironmentRaceFixture(t, db)
	ctx := context.Background()
	store := fixture.repository

	allowed, err := store.SudoAllowed(ctx, fixture.workspaceID)
	require.NoError(t, err)
	require.False(t, allowed, "off by default")
	allowed, err = store.SudoAllowed(ctx, uuid.New())
	require.NoError(t, err)
	require.False(t, allowed, "a workspace without a policy has no sudo")

	policy := &domain.DevMachineWorkspacePolicy{
		WorkspaceID: fixture.workspaceID, Enabled: true, MaxConcurrentMachines: 5, MaxMachinesPerUser: 5,
		MaxDailyAgentRuns: 25, MaxDiskGB: 100,
		AllowedProviders: json.RawMessage(`[]`), AllowedRepositories: json.RawMessage(`[]`), AllowSudo: true,
	}
	require.NoError(t, store.UpsertPolicy(ctx, policy))
	stored, err := store.GetPolicy(ctx, fixture.workspaceID)
	require.NoError(t, err)
	require.True(t, stored.AllowSudo)
	require.False(t, stored.AllowDocker, "the switches are independent")
	allowed, err = store.SudoAllowed(ctx, fixture.workspaceID)
	require.NoError(t, err)
	require.True(t, allowed)

	machineID := uuid.New()
	routingKey := strings.ReplaceAll(machineID.String(), "-", "")[:16]
	machine := &domain.DevMachine{
		ID: machineID, WorkspaceID: fixture.workspaceID, CreatedByUserID: &fixture.userID,
		RoutingKey: routingKey, Name: "sudo-" + routingKey, Status: domain.DevMachineStatusQueued,
		DesiredStatus: domain.DevMachineStatusRunning, Generation: 1, RepoProvider: "github",
		MachineSize: "small", CPUMillis: 2000, MemoryMB: 4096, DiskGB: 20, PidsLimit: 512,
		ServicesConfig: []byte(`{}`), Labels: []byte(`{}`),
		SudoEnabled: true,
	}
	operation := &domain.DevMachineOperation{
		ID: uuid.New(), MachineID: machineID, WorkspaceID: fixture.workspaceID, Action: domain.DevMachineOpSpawn,
		Status: domain.DevMachineOpStatusPending, Generation: 1, IdempotencyKey: "spawn:1",
		RequestedByUserID: &fixture.userID, MaxAttempts: 5,
	}
	require.NoError(t, store.CreateBundle(ctx, machine, nil, nil, nil, nil, nil, operation))

	policy.AllowSudo = false
	require.NoError(t, store.UpsertPolicy(ctx, policy))
	allowed, err = store.SudoAllowed(ctx, fixture.workspaceID)
	require.NoError(t, err)
	require.False(t, allowed)
	loaded, err := store.GetMachineInternal(ctx, machineID)
	require.NoError(t, err)
	require.True(t, loaded.SudoEnabled, "the machine keeps the choice it was created with")
	unprivileged := fixture.insertMachine(t, domain.DevMachineStatusStopped, domain.DevMachineStatusStopped, false)
	loaded, err = store.GetMachineInternal(ctx, unprivileged)
	require.NoError(t, err)
	require.False(t, loaded.SudoEnabled, "machines default to no sudo")
}
