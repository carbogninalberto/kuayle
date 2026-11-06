package repository

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestResourceHistoryAveragesBucketsAndIsPrunedPerMachine(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	fixture := newEnvironmentRaceFixture(t, db)
	ctx := context.Background()
	machineID := fixture.insertMachine(t, domain.DevMachineStatusRunning, domain.DevMachineStatusRunning, false)
	otherID := fixture.insertMachine(t, domain.DevMachineStatusRunning, domain.DevMachineStatusRunning, false)
	bucketStart := time.Now().UTC().Truncate(time.Hour).Add(-time.Hour)
	insert := func(machine any, at time.Time, cpu float64, memory, rx int64) {
		_, err := db.Exec(`INSERT INTO dev_machine_resource_samples (workspace_id, machine_id, cpu_percent, memory_bytes, network_rx_bytes, created_at)
			VALUES ($1,$2,$3,$4,$5,$6)`, fixture.workspaceID, machine, cpu, memory, rx, at)
		require.NoError(t, err)
	}
	insert(machineID, bucketStart.Add(10*time.Second), 100, 1000, 5)
	insert(machineID, bucketStart.Add(40*time.Second), 300, 3000, 9)
	insert(machineID, bucketStart.Add(70*time.Second), 50, 500, 12)
	insert(machineID, bucketStart.Add(-48*time.Hour), 999, 999, 1)
	insert(otherID, bucketStart.Add(20*time.Second), 800, 8000, 1)

	history, err := fixture.repository.ListResourceHistory(ctx, fixture.workspaceID, machineID, bucketStart.Add(-time.Minute), time.Minute)
	require.NoError(t, err)
	require.Len(t, history, 2, "two one-minute buckets of this machine only")
	require.True(t, history[0].CreatedAt.Equal(bucketStart), "a bucket is labelled with its start: %s", history[0].CreatedAt)
	require.Equal(t, 200.0, history[0].CPUPercent)
	require.Equal(t, int64(2000), history[0].MemoryBytes)
	require.Equal(t, int64(9), history[0].NetworkRxBytes, "cumulative counters keep the highest value")
	require.Equal(t, 50.0, history[1].CPUPercent)
	empty, err := fixture.repository.ListResourceHistory(ctx, uuid.New(), machineID, bucketStart.Add(-time.Minute), time.Minute)
	require.NoError(t, err)
	require.Empty(t, empty, "the machine is scoped to its workspace")

	require.NoError(t, fixture.repository.PruneResourceSamples(ctx, machineID, bucketStart.Add(-24*time.Hour)))
	var count int
	require.NoError(t, db.Get(&count, `SELECT COUNT(*) FROM dev_machine_resource_samples WHERE machine_id=$1`, machineID))
	require.Equal(t, 3, count, "only samples older than the retention are pruned")
	require.NoError(t, fixture.repository.DeleteResourceSamples(ctx, machineID))
	require.NoError(t, db.Get(&count, `SELECT COUNT(*) FROM dev_machine_resource_samples WHERE machine_id=$1`, machineID))
	require.Zero(t, count)
	require.NoError(t, db.Get(&count, `SELECT COUNT(*) FROM dev_machine_resource_samples WHERE machine_id=$1`, otherID))
	require.Equal(t, 1, count, "other machines keep their history")
}
