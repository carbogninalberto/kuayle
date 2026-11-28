package machine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// environmentFakeState holds the cached environment check of managerStoreFake.
type environmentFakeState struct {
	check   *domain.DevMachineEnvironmentCheck
	upserts int
	deleted int
	refresh []string
}

func (f *managerStoreFake) GetEnvironmentCheck(context.Context, uuid.UUID) (*domain.DevMachineEnvironmentCheck, error) {
	if f.envCheck.check == nil {
		return nil, nil
	}
	check := *f.envCheck.check
	return &check, nil
}

func (f *managerStoreFake) UpsertPackageIndexProbe(_ context.Context, machineID, workspaceID uuid.UUID, container, state string, mtime *time.Time) error {
	f.envCheck.upserts++
	previous := f.envCheck.check
	check := domain.DevMachineEnvironmentCheck{MachineID: machineID, WorkspaceID: workspaceID, ContainerID: container,
		PackageIndexState: state, PackageIndexMtime: mtime, CheckedAt: time.Now(), RefreshState: domain.PackageIndexRefreshIdle}
	if previous != nil && previous.ContainerID == container {
		check.RefreshState, check.RefreshExitCode, check.RefreshOutput = previous.RefreshState, previous.RefreshExitCode, previous.RefreshOutput
	}
	f.envCheck.check = &check
	return nil
}

func (f *managerStoreFake) SetPackageIndexRefresh(_ context.Context, _ uuid.UUID, container, state string, exitCode *int, output *string) error {
	f.envCheck.refresh = append(f.envCheck.refresh, state)
	if check := f.envCheck.check; check != nil && (check.ContainerID == container || container == "") {
		check.RefreshState, check.RefreshExitCode, check.RefreshOutput = state, exitCode, output
	}
	return nil
}

func (f *managerStoreFake) DeleteEnvironmentCheck(context.Context, uuid.UUID) error {
	f.envCheck.deleted++
	f.envCheck.check = nil
	return nil
}

// environmentRuntimeFake is the package index side of runtimeFake.
type environmentRuntimeFake struct {
	probe        PackageIndexProbe
	probeErr     error
	probes       int
	refreshExit  int
	refreshOut   string
	refreshErr   error
	refreshes    int
	afterRefresh *PackageIndexProbe
}

func (f *runtimeFake) ProbePackageIndex(context.Context, []domain.DevMachineService) (PackageIndexProbe, error) {
	f.environment.probes++
	return f.environment.probe, f.environment.probeErr
}

func (f *runtimeFake) RefreshPackageIndex(context.Context, []domain.DevMachineService) (int, string, error) {
	f.environment.refreshes++
	if f.environment.afterRefresh != nil && f.environment.refreshErr == nil && f.environment.refreshExit == 0 {
		f.environment.probe = *f.environment.afterRefresh
	}
	return f.environment.refreshExit, f.environment.refreshOut, f.environment.refreshErr
}

func refreshOperation(store *managerStoreFake) *domain.DevMachineOperation {
	return &domain.DevMachineOperation{ID: uuid.New(), MachineID: store.machine.ID, WorkspaceID: store.machine.WorkspaceID,
		Action: domain.DevMachineOpRefreshPackageIndex, Generation: store.machine.Generation}
}

func TestParsePackageIndexProbe(t *testing.T) {
	probe, err := parsePackageIndexProbe("absent\n")
	require.NoError(t, err)
	require.False(t, probe.Present)

	probe, err = parsePackageIndexProbe("present 1763000000\n")
	require.NoError(t, err)
	require.True(t, probe.Present)
	require.Equal(t, time.Unix(1763000000, 0).UTC(), probe.Mtime)

	for _, output := range []string{"", "present", "present soon", "present 0", "sh: ls: not found", "absent extra"} {
		_, err = parsePackageIndexProbe(output)
		require.Error(t, err, "%q is not a probe result", output)
	}
}

func TestOutputTailKeepsTheLastLines(t *testing.T) {
	var lines []string
	for i := 1; i <= 100; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	tail := outputTail(strings.Join(lines, "\r\n")+"\n", 40, 8192)
	got := strings.Split(tail, "\n")
	require.Len(t, got, 40)
	require.Equal(t, "line 61", got[0])
	require.Equal(t, "line 100", got[39])

	long := outputTail(strings.Repeat("é", 100)+"\n"+strings.Repeat("x", 50), 40, 60)
	require.Equal(t, strings.Repeat("x", 50), long, "a byte cap cuts at a line boundary")
	require.LessOrEqual(t, len(outputTail(strings.Repeat("é", 100), 40, 51)), 51)
	require.True(t, strings.HasPrefix(outputTail(strings.Repeat("é", 100), 40, 51), "é"), "never half a character")
}

func TestEnvironmentStatusRecordsAnAbsentIndex(t *testing.T) {
	manager, store, runtime := newPortManager()
	require.NoError(t, manager.syncMachinePorts(context.Background(), store.machine, store.services))
	require.Equal(t, 1, runtime.environment.probes)
	require.NotNil(t, store.envCheck.check)
	require.Equal(t, domain.PackageIndexAbsent, store.envCheck.check.PackageIndexState)
	require.Equal(t, "developer", store.envCheck.check.ContainerID)
	require.Nil(t, store.envCheck.check.PackageIndexMtime)
	require.Equal(t, domain.DevMachineStatusRunning, store.machine.Status, "the check never changes the machine")
	require.Equal(t, "running", store.services[0].Status, "nor its services")
}

func TestEnvironmentStatusRecordsAPresentIndexWithItsTime(t *testing.T) {
	manager, store, runtime := newPortManager()
	fetched := time.Unix(1763000000, 0).UTC()
	runtime.environment.probe = PackageIndexProbe{Present: true, Mtime: fetched}
	require.NoError(t, manager.syncEnvironmentStatus(context.Background(), store.machine, store.services))
	require.Equal(t, domain.PackageIndexPresent, store.envCheck.check.PackageIndexState)
	require.Equal(t, fetched, *store.envCheck.check.PackageIndexMtime)
}

func TestEnvironmentStatusRecordsAFailedProbeAsUnknown(t *testing.T) {
	manager, store, runtime := newPortManager()
	runtime.environment.probeErr = context.DeadlineExceeded
	require.NoError(t, manager.syncMachinePorts(context.Background(), store.machine, store.services), "a probe failure never fails the port scan")
	require.Equal(t, domain.PackageIndexUnknown, store.envCheck.check.PackageIndexState)
}

func TestEnvironmentStatusNeverRunsInAStoppedContainer(t *testing.T) {
	manager, store, runtime := newPortManager()
	for i := range store.services {
		store.services[i].Status = "stopped"
	}
	require.NoError(t, manager.syncEnvironmentStatus(context.Background(), store.machine, store.services))
	store.services[0].Status = "running"
	store.machine.Status = domain.DevMachineStatusPaused
	require.NoError(t, manager.syncEnvironmentStatus(context.Background(), store.machine, store.services))
	require.Zero(t, runtime.environment.probes, "nothing runs while the machine or its developer container is not running")
	require.Nil(t, store.envCheck.check)
}

func TestEnvironmentStatusReusesAFreshCheckOfTheSameContainer(t *testing.T) {
	manager, store, runtime := newPortManager()
	require.NoError(t, manager.syncEnvironmentStatus(context.Background(), store.machine, store.services))
	require.NoError(t, manager.syncEnvironmentStatus(context.Background(), store.machine, store.services))
	require.Equal(t, 1, runtime.environment.probes, "a check younger than a minute is reused")

	store.envCheck.check.CheckedAt = time.Now().Add(-environmentCheckMaxAge - time.Second)
	require.NoError(t, manager.syncEnvironmentStatus(context.Background(), store.machine, store.services))
	require.Equal(t, 2, runtime.environment.probes, "an old check is probed again")

	recreated := "developer-2"
	for i := range store.services {
		store.services[i].ContainerID = &recreated
	}
	require.NoError(t, manager.syncEnvironmentStatus(context.Background(), store.machine, store.services))
	require.Equal(t, 3, runtime.environment.probes, "a recreated container is probed at once")
	require.Equal(t, recreated, store.envCheck.check.ContainerID)
}

func TestRefreshPackageIndexSucceedsAndTurnsTheIndexPresent(t *testing.T) {
	manager, store, runtime := newPortManager()
	store.machine.SudoEnabled = true
	require.NoError(t, manager.syncEnvironmentStatus(context.Background(), store.machine, store.services))
	fetched := time.Now().UTC().Truncate(time.Second)
	runtime.environment.afterRefresh = &PackageIndexProbe{Present: true, Mtime: fetched}
	runtime.environment.refreshOut = "Hit:1 http://deb.debian.org/debian bookworm InRelease\nReading package lists...\n"

	require.NoError(t, manager.processOperation(context.Background(), refreshOperation(store)))
	require.Equal(t, 1, runtime.environment.refreshes)
	require.Equal(t, []string{domain.PackageIndexRefreshRunning, domain.PackageIndexRefreshSucceeded}, store.envCheck.refresh)
	check := store.envCheck.check
	require.Equal(t, domain.PackageIndexRefreshSucceeded, check.RefreshState)
	require.Equal(t, 0, *check.RefreshExitCode)
	require.Equal(t, "Hit:1 http://deb.debian.org/debian bookworm InRelease\nReading package lists...", *check.RefreshOutput)
	require.Equal(t, domain.PackageIndexPresent, check.PackageIndexState, "the index is probed again right away")
	require.Equal(t, fetched, *check.PackageIndexMtime)
}

func TestRefreshPackageIndexReportsAFailedUpdateWithItsOutput(t *testing.T) {
	manager, store, runtime := newPortManager()
	store.machine.SudoEnabled = true
	require.NoError(t, manager.syncEnvironmentStatus(context.Background(), store.machine, store.services))
	var output []string
	for i := 1; i <= 60; i++ {
		output = append(output, fmt.Sprintf("Err:%d http://deb.debian.org/debian Could not connect", i))
	}
	runtime.environment.refreshExit = 100
	runtime.environment.refreshOut = strings.Join(output, "\n")

	require.NoError(t, manager.processOperation(context.Background(), refreshOperation(store)), "apt failing is a result, not an operation failure")
	check := store.envCheck.check
	require.Equal(t, domain.PackageIndexRefreshFailed, check.RefreshState)
	require.Equal(t, 100, *check.RefreshExitCode)
	lines := strings.Split(*check.RefreshOutput, "\n")
	require.Len(t, lines, refreshOutputLines)
	require.Equal(t, "Err:60 http://deb.debian.org/debian Could not connect", lines[len(lines)-1])
	require.Equal(t, domain.PackageIndexAbsent, check.PackageIndexState)
}

func TestRefreshPackageIndexRecordsARuntimeErrorAndDoesNotRetry(t *testing.T) {
	manager, store, runtime := newPortManager()
	store.machine.SudoEnabled = true
	require.NoError(t, manager.syncEnvironmentStatus(context.Background(), store.machine, store.services))
	runtime.environment.refreshErr = errors.New("exec failed")

	err := manager.processOperation(context.Background(), refreshOperation(store))
	var terminal *terminalOperationError
	require.ErrorAs(t, err, &terminal)
	require.Equal(t, domain.PackageIndexRefreshFailed, store.envCheck.check.RefreshState)
	require.Equal(t, -1, *store.envCheck.check.RefreshExitCode)
	require.Contains(t, *store.envCheck.check.RefreshOutput, "exec failed")
}

func TestRefreshPackageIndexRefusesAMachineWithoutSudo(t *testing.T) {
	manager, store, runtime := newPortManager()
	require.NoError(t, manager.syncEnvironmentStatus(context.Background(), store.machine, store.services))

	err := manager.processOperation(context.Background(), refreshOperation(store))
	var terminal *terminalOperationError
	require.ErrorAs(t, err, &terminal)
	require.Equal(t, "sudo_disabled", terminal.code)
	require.Zero(t, runtime.environment.refreshes, "nothing runs without sudo")
	require.Equal(t, domain.PackageIndexRefreshFailed, store.envCheck.check.RefreshState)
}

func TestStoppingTheMachineForgetsItsEnvironmentCheck(t *testing.T) {
	manager, store, _ := newPortManager()
	require.NoError(t, manager.syncEnvironmentStatus(context.Background(), store.machine, store.services))
	manager.environmentEnded(context.Background(), store.machine)
	require.Nil(t, store.envCheck.check)
	require.Equal(t, 1, store.envCheck.deleted)
}
