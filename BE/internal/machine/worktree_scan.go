package machine

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/kuayle/kuayle-backend/internal/domain"
)

// worktreeScanRoot is where the developer container keeps one worktree per
// task; a list scan reads the directories below it.
const worktreeScanRoot = "/workspace/tasks"

// worktreeScanResultMax is the schema's bound on a stored result.
const worktreeScanResultMax = 65536

// WorktreeScanJob is what one runtime scan reads.
type WorktreeScanJob struct {
	Request    domain.WorktreeScanRequest
	Path       string
	Branch     string
	BaseBranch string
}

// runWorktreeScan executes a read-only scan and stores its result or a short
// error on the scan row. A scan that cannot run is a completed scan with an
// error, never an operation retry: the operation itself succeeds.
func (m *Manager) runWorktreeScan(ctx context.Context, machine *domain.DevMachine, operation *domain.DevMachineOperation) error {
	scan, err := m.store.WorktreeScanForOperation(ctx, operation.ID)
	if err != nil {
		return err
	}
	if scan == nil || scan.CompletedAt != nil {
		return nil
	}
	fail := func(message string) error {
		return m.store.CompleteWorktreeScan(ctx, scan.ID, nil, &message)
	}
	if machine.Status != domain.DevMachineStatusRunning || machine.DeleteRequestedAt != nil {
		return fail("The machine is not running")
	}
	var request domain.WorktreeScanRequest
	if err := json.Unmarshal(scan.Request, &request); err != nil {
		return fail("The scan request is invalid")
	}
	services, err := m.store.ListServices(ctx, machine.WorkspaceID, machine.ID)
	if err != nil {
		return err
	}
	path := scan.WorkspacePath
	if request.Kind == domain.WorktreeScanList {
		path = worktreeScanRoot
	}
	result, err := m.runtime.ScanWorktree(ctx, machine, services, WorktreeScanJob{
		Request: request, Path: path, Branch: scan.WorkingBranch, BaseBranch: scan.BaseBranch,
	})
	if err != nil {
		return fail(worktreeScanFailure(err))
	}
	payload, err := marshalWorktreeScanResult(result)
	if err != nil {
		return fail(worktreeScanFailure(err))
	}
	return m.store.CompleteWorktreeScan(ctx, scan.ID, payload, nil)
}

// completeStaleWorktreeScan finishes a scan whose machine changed before it
// ran, so the requester sees an answer instead of a scan that never ends.
func (m *Manager) completeStaleWorktreeScan(ctx context.Context, operation *domain.DevMachineOperation) error {
	message := "The machine changed before the scan ran"
	return m.store.CompleteWorktreeScan(ctx, operation.ID, nil, &message)
}

// worktreeScanFailure is the short reason stored on a failed scan. A runtime
// error can carry container paths and command output, so it is not exposed.
func worktreeScanFailure(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "The scan took too long"
	}
	var terminal *terminalOperationError
	if errors.As(err, &terminal) {
		return terminal.message
	}
	return "The scan could not finish"
}

// marshalWorktreeScanResult keeps the stored JSON within the schema's bound by
// dropping the arrays a scan fills, from the end. It reports Truncated.
func marshalWorktreeScanResult(result *domain.WorktreeScanResult) (json.RawMessage, error) {
	if result == nil {
		return nil, errors.New("worktree scan returned no result")
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	if len(payload) <= worktreeScanResultMax {
		return payload, nil
	}
	trimmed := *result
	trimmed.Truncated = true
	for len(payload) > worktreeScanResultMax {
		switch {
		case len(trimmed.Matches) > 0:
			trimmed.Matches = trimmed.Matches[:len(trimmed.Matches)/2]
		case len(trimmed.Files) > 0:
			trimmed.Files = trimmed.Files[:len(trimmed.Files)/2]
		case len(trimmed.Changed) > 0:
			trimmed.Changed = trimmed.Changed[:len(trimmed.Changed)/2]
		case len(trimmed.Worktrees) > 0:
			trimmed.Worktrees = trimmed.Worktrees[:len(trimmed.Worktrees)/2]
		case len(trimmed.Content) > 0:
			trimmed.Content = trimmed.Content[:len(trimmed.Content)/2]
		default:
			return nil, errors.New("worktree scan result is too large")
		}
		if payload, err = json.Marshal(trimmed); err != nil {
			return nil, err
		}
	}
	return payload, nil
}
