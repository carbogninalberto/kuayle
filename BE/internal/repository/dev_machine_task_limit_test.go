package repository

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// The machine's own task limit is stored for its workspace and machine only;
// clearing it restores the workspace policy and touches nothing else.
func TestUpdateMachineMaxTasksSetsAndClearsOnlyTheLimit(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	db := isolatedPrivacyDatabase(t, databaseURL)
	fixture := newEnvironmentRaceFixture(t, db)
	ctx := context.Background()
	machineID := fixture.insertMachine(t, domain.DevMachineStatusRunning, domain.DevMachineStatusRunning, false)
	before, err := fixture.repository.GetMachineInternal(ctx, machineID)
	require.NoError(t, err)

	machine, err := fixture.repository.UpdateMachineMaxTasks(ctx, fixture.workspaceID, machineID, ptr(4))
	require.NoError(t, err)
	require.NotNil(t, machine)
	require.Equal(t, 4, *machine.MaxTasks)
	require.True(t, before.UpdatedAt.Before(machine.UpdatedAt), "updated_at moves")
	require.Equal(t, []int{before.CPUMillis, before.MemoryMB, before.DiskGB}, []int{machine.CPUMillis, machine.MemoryMB, machine.DiskGB})

	machine, err = fixture.repository.UpdateMachineMaxTasks(ctx, uuid.New(), machineID, ptr(3))
	require.NoError(t, err)
	require.Nil(t, machine, "another workspace cannot change the machine")

	machine, err = fixture.repository.UpdateMachineMaxTasks(ctx, fixture.workspaceID, machineID, nil)
	require.NoError(t, err)
	require.Nil(t, machine.MaxTasks, "clearing follows the workspace policy again")
}

func ptr(value int) *int { return &value }
