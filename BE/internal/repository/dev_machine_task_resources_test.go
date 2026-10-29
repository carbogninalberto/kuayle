package repository

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// The policy stores task machine resources; a policy written without them
// (an in-memory policy) stores the 8 GiB default instead of failing.
func TestPolicyStoresTaskMachineResources(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db := isolatedPrivacyDatabase(t, url)
	fixture := newEnvironmentRaceFixture(t, db)
	repo := NewDevMachineRepository(db)
	ctx := context.Background()
	policy := &domain.DevMachineWorkspacePolicy{
		WorkspaceID: fixture.workspaceID, Enabled: true, MaxConcurrentMachines: 5, MaxMachinesPerUser: 5,
		MaxDailyAgentRuns: 25, MaxRuntimeMinutes: 480, MaxDiskGB: 600, IdlePauseMinutes: 240,
		AllowedProviders: json.RawMessage(`[]`), AllowedRepositories: json.RawMessage(`[]`),
	}
	require.NoError(t, repo.UpsertPolicy(ctx, policy))
	stored, err := repo.GetPolicy(ctx, fixture.workspaceID)
	require.NoError(t, err)
	require.Equal(t, domain.DefaultTaskMachineResources, stored.TaskMachine())
	require.Equal(t, 8192, stored.TaskMachineMemoryMB)

	policy.TaskMachineCPUMillis, policy.TaskMachineMemoryMB, policy.TaskMachineDiskGB = 8000, 16384, 512
	require.NoError(t, repo.UpsertPolicy(ctx, policy))
	stored, err = repo.GetPolicy(ctx, fixture.workspaceID)
	require.NoError(t, err)
	require.Equal(t, domain.TaskMachineResources{CPUMillis: 8000, MemoryMB: 16384, DiskGB: 512}, stored.TaskMachine())
	require.Equal(t, 600, stored.MaxDiskGB)

	policy.MaxDiskGB = 400
	require.Error(t, repo.UpsertPolicy(ctx, policy), "the schema keeps the task disk within the workspace limit")
}
