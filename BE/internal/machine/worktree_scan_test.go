package machine

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

type worktreeScanStoreFake struct {
	*managerStoreFake
	scan        *domain.WorktreeScan
	result      json.RawMessage
	scanError   *string
	completions int
}

func (f *worktreeScanStoreFake) WorktreeScanForOperation(context.Context, uuid.UUID) (*domain.WorktreeScan, error) {
	return f.scan, nil
}

func (f *worktreeScanStoreFake) CompleteWorktreeScan(_ context.Context, _ uuid.UUID, result json.RawMessage, errMsg *string) error {
	f.completions++
	f.result = result
	f.scanError = errMsg
	return nil
}

type worktreeScanRuntimeFake struct {
	Runtime
	job    WorktreeScanJob
	result *domain.WorktreeScanResult
	err    error
}

func (f *worktreeScanRuntimeFake) ScanWorktree(_ context.Context, _ *domain.DevMachine, _ []domain.DevMachineService, job WorktreeScanJob) (*domain.WorktreeScanResult, error) {
	f.job = job
	return f.result, f.err
}

func worktreeScanManager(t *testing.T, request domain.WorktreeScanRequest, result *domain.WorktreeScanResult, runtimeErr error) (*Manager, *worktreeScanStoreFake, *worktreeScanRuntimeFake, *domain.DevMachineOperation) {
	t.Helper()
	machine := &domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New(), Status: domain.DevMachineStatusRunning, Generation: 1}
	payload, err := json.Marshal(request)
	require.NoError(t, err)
	scan := &domain.WorktreeScan{
		ID: uuid.New(), WorkspaceID: machine.WorkspaceID, MachineID: machine.ID, Request: payload,
		WorkingBranch: "kuayle/rec-1", BaseBranch: "main", WorkspacePath: "/workspace/tasks/rec-1",
	}
	store := &worktreeScanStoreFake{
		managerStoreFake: &managerStoreFake{machine: machine, services: []domain.DevMachineService{{ServiceType: "terminal", ImageRef: "developer:fixture"}}},
		scan:             scan,
	}
	runtime := &worktreeScanRuntimeFake{result: result, err: runtimeErr}
	manager := &Manager{store: store, runtime: runtime}
	operation := &domain.DevMachineOperation{ID: scan.ID, WorkspaceID: machine.WorkspaceID, MachineID: machine.ID,
		Action: domain.DevMachineOpScanWorktree, Generation: machine.Generation}
	return manager, store, runtime, operation
}

func TestWorktreeScanOperationStoresTheResult(t *testing.T) {
	expected := &domain.WorktreeScanResult{Branch: "kuayle/rec-1", Commit: "abc123", Dirty: true,
		Matches: []domain.WorktreeMatch{{Path: "src/app.go", Line: 2, Text: "needle"}}}
	manager, store, runtime, operation := worktreeScanManager(t,
		domain.WorktreeScanRequest{Kind: domain.WorktreeScanGrep, Pattern: "needle"}, expected, nil)

	require.NoError(t, manager.processOperation(context.Background(), operation))
	require.Equal(t, "/workspace/tasks/rec-1", runtime.job.Path)
	require.Equal(t, "kuayle/rec-1", runtime.job.Branch)
	require.Equal(t, "main", runtime.job.BaseBranch)
	require.Equal(t, 1, store.completions)
	require.Nil(t, store.scanError)
	var stored domain.WorktreeScanResult
	require.NoError(t, json.Unmarshal(store.result, &stored))
	require.Equal(t, *expected, stored)
}

func TestWorktreeListScanRunsAtTheTasksRoot(t *testing.T) {
	manager, _, runtime, operation := worktreeScanManager(t,
		domain.WorktreeScanRequest{Kind: domain.WorktreeScanList}, &domain.WorktreeScanResult{}, nil)

	require.NoError(t, manager.processOperation(context.Background(), operation))
	require.Equal(t, "/workspace/tasks", runtime.job.Path)
}

func TestWorktreeScanFailureIsRecordedOnTheScan(t *testing.T) {
	manager, store, _, operation := worktreeScanManager(t,
		domain.WorktreeScanRequest{Kind: domain.WorktreeScanStatus}, nil, errors.New("container is unavailable"))

	require.NoError(t, manager.processOperation(context.Background(), operation), "a scan failure is not an operation retry")
	require.Equal(t, 1, store.completions)
	require.Nil(t, store.result)
	require.NotNil(t, store.scanError)
	require.Contains(t, *store.scanError, "scan")
}
