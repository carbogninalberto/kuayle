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

func TestResourceHistoryFillsEveryBucketAndIsPrunedForEveryMachine(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	fixture := newEnvironmentRaceFixture(t, db)
	ctx := context.Background()
	machineID := fixture.insertMachine(t, domain.DevMachineStatusRunning, domain.DevMachineStatusRunning)
	otherID := fixture.insertMachine(t, domain.DevMachineStatusRunning, domain.DevMachineStatusRunning)
	stoppedID := fixture.insertMachine(t, domain.DevMachineStatusStopped, domain.DevMachineStatusStopped)
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
	// The stopped machine's samples are old and recent: only the old go.
	insert(stoppedID, bucketStart.Add(-30*time.Hour), 1, 1, 1)
	insert(stoppedID, bucketStart.Add(-25*time.Hour), 1, 1, 1)
	insert(stoppedID, bucketStart.Add(-time.Hour), 2, 2, 2)

	// Buckets 0 and 1 hold samples; 2 and 3 are a gap; 4 holds the last sample.
	insert(machineID, bucketStart.Add(4*time.Minute+5*time.Second), 70, 700, 20)
	history, err := fixture.repository.ListResourceHistory(ctx, fixture.workspaceID, machineID,
		bucketStart.Add(-time.Minute), bucketStart.Add(5*time.Minute+30*time.Second), time.Minute)
	require.NoError(t, err)
	require.Len(t, history, 7, "every one-minute bucket from the one holding since to the one holding until")
	require.True(t, history[0].CreatedAt.Equal(bucketStart.Add(-time.Minute)), "buckets start at since's bucket: %s", history[0].CreatedAt)
	require.Zero(t, history[0].SampleCount)
	require.Nil(t, history[0].CPUPercent, "an empty bucket has no value, not zero")
	require.Nil(t, history[0].MemoryBytes)
	require.Nil(t, history[0].NetworkRxBytes)

	require.True(t, history[1].CreatedAt.Equal(bucketStart), "a bucket is labelled with its start: %s", history[1].CreatedAt)
	require.Equal(t, 2, history[1].SampleCount)
	require.Equal(t, 200.0, *history[1].CPUPercent)
	require.Equal(t, int64(2000), *history[1].MemoryBytes)
	require.Equal(t, int64(9), *history[1].NetworkRxBytes, "cumulative counters keep the highest value")
	require.Equal(t, 1, history[2].SampleCount)
	require.Equal(t, 50.0, *history[2].CPUPercent)
	require.Zero(t, history[3].SampleCount, "a gap in the middle is returned as an empty bucket")
	require.Nil(t, history[3].CPUPercent)
	require.Zero(t, history[4].SampleCount)
	require.Equal(t, 1, history[5].SampleCount)
	require.Equal(t, 70.0, *history[5].CPUPercent)
	require.Zero(t, history[6].SampleCount)
	for i := 1; i < len(history); i++ {
		require.True(t, history[i].CreatedAt.Equal(history[i-1].CreatedAt.Add(time.Minute)), "buckets are contiguous")
	}

	// A machine with no samples in the window still gets every bucket.
	idle, err := fixture.repository.ListResourceHistory(ctx, fixture.workspaceID, stoppedID,
		bucketStart.Add(-10*time.Minute), bucketStart.Add(-5*time.Minute), time.Minute)
	require.NoError(t, err)
	require.Len(t, idle, 6)
	for _, bucket := range idle {
		require.Zero(t, bucket.SampleCount)
		require.Nil(t, bucket.CPUPercent)
	}

	empty, err := fixture.repository.ListResourceHistory(ctx, uuid.New(), machineID, bucketStart.Add(-time.Minute), bucketStart.Add(time.Minute), time.Minute)
	require.NoError(t, err)
	require.Empty(t, empty, "the machine is scoped to its workspace")

	// Retention is a rolling window for every machine, running or not.
	before := bucketStart.Add(-24 * time.Hour)
	pruned, err := fixture.repository.PruneExpiredResourceSamples(ctx, before, 1)
	require.NoError(t, err)
	require.Equal(t, 1, pruned, "a batch is bounded by its limit")
	for {
		pruned, err = fixture.repository.PruneExpiredResourceSamples(ctx, before, 1000)
		require.NoError(t, err)
		if pruned < 1000 {
			break
		}
	}
	count := func(id uuid.UUID) (n int) {
		require.NoError(t, db.Get(&n, `SELECT COUNT(*) FROM dev_machine_resource_samples WHERE machine_id=$1`, id))
		return n
	}
	require.Equal(t, 4, count(machineID), "the running machine loses only its 48 hour old sample")
	require.Equal(t, 1, count(stoppedID), "the stopped machine's expired samples are pruned too")
	require.Equal(t, 1, count(otherID))
	pruned, err = fixture.repository.PruneExpiredResourceSamples(ctx, before, 0)
	require.NoError(t, err)
	require.Zero(t, pruned)

	require.NoError(t, fixture.repository.DeleteResourceSamples(ctx, machineID))
	require.Zero(t, count(machineID))
	require.Equal(t, 1, count(otherID), "other machines keep their history")
}

// Sampling covers running machines even while an operation is queued for them,
// so the usage chart has no hole while a machine resizes.
func TestListSamplingMachinesReturnsRunningMachinesOnly(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	fixture := newEnvironmentRaceFixture(t, db)
	running := fixture.insertMachine(t, domain.DevMachineStatusRunning, domain.DevMachineStatusRunning)
	busy := fixture.insertMachine(t, domain.DevMachineStatusRunning, domain.DevMachineStatusRunning)
	stopped := fixture.insertMachine(t, domain.DevMachineStatusStopped, domain.DevMachineStatusStopped)
	_, err = db.Exec(`INSERT INTO dev_machine_operations (workspace_id, machine_id, action, status, generation, idempotency_key)
		VALUES ($1,$2,'resize','pending',2,$3)`, fixture.workspaceID, busy, "sampling-"+busy.String())
	require.NoError(t, err)

	var ids []uuid.UUID
	for offset := 0; ; offset += 50 {
		page, err := fixture.repository.ListSamplingMachines(context.Background(), 50, offset)
		require.NoError(t, err)
		for _, machine := range page {
			require.Equal(t, domain.DevMachineStatusRunning, machine.Status)
			ids = append(ids, machine.ID)
		}
		if len(page) < 50 {
			break
		}
	}
	require.Contains(t, ids, running)
	require.Contains(t, ids, busy, "a machine with a queued operation is still sampled")
	require.NotContains(t, ids, stopped)
}
