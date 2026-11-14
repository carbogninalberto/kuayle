package repository

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// worktreeScanFixture is a task active on a running machine with its ready
// checkout, so scans can be requested while the agent works.
func worktreeScanFixture(t *testing.T) (*sqlx.DB, *DevMachineRepository, context.Context, domain.AgentTask, *domain.AgentTask, environmentRaceFixture) {
	t.Helper()
	db, tasks, ctx, task, fixture := taskRecoveryFixture(t)
	claimed := prepareTaskStart(t, db, tasks, ctx, task, fixture)
	installation, repositoryID := uuid.New(), uuid.New()
	_, err := db.Exec(`INSERT INTO github_installations(id,workspace_id,installation_id,account_login,account_type,installed_by) VALUES($1,$2,1,'fixture','Organization',$3)`, installation, task.WorkspaceID, task.CreatorID)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO github_repos(id,installation_id,workspace_id,github_repo_id,full_name,default_branch) VALUES($1,$2,$3,1,'fixture/repo','main')`, repositoryID, installation, task.WorkspaceID)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO dev_machine_checkouts(id,workspace_id,machine_id,issue_id,github_repo_id,repository_full_name,base_branch,working_branch,workspace_path,status,scope_key) VALUES($1,$2,$3,$4,$5,'fixture/repo','main','kuayle/rec-1','/workspace/tasks/rec-1','ready',$6)`,
		task.CheckoutID, task.WorkspaceID, *claimed.MachineID, task.IssueID, repositoryID, task.ID.String())
	require.NoError(t, err)
	return db, NewDevMachineRepository(db), ctx, task, claimed, fixture
}

func taskScanTarget(task domain.AgentTask) domain.WorktreeScanTarget {
	return domain.WorktreeScanTarget{TaskID: &task.ID}
}

func TestWorktreeScanCreationQueuesTheOperationWithTheScanID(t *testing.T) {
	db, repo, ctx, task, _, _ := worktreeScanFixture(t)
	request := domain.WorktreeScanRequest{Kind: domain.WorktreeScanGrep, Pattern: "needle", Limit: 10}

	scan, err := repo.CreateWorktreeScan(ctx, task.WorkspaceID, task.CreatorID, taskScanTarget(task), request)
	require.NoError(t, err)
	require.Equal(t, "/workspace/tasks/rec-1", scan.WorkspacePath)
	require.Equal(t, "kuayle/rec-1", scan.WorkingBranch)
	require.Equal(t, "main", scan.BaseBranch)
	require.Equal(t, "fixture/repo", scan.Repository)
	require.NotEmpty(t, scan.MachineName)
	require.Nil(t, scan.Result)
	require.Nil(t, scan.CompletedAt)

	var operation domain.DevMachineOperation
	require.NoError(t, db.Get(&operation, `SELECT * FROM dev_machine_operations WHERE id=$1`, scan.ID))
	require.Equal(t, scan.ID, operation.ID)
	require.Equal(t, domain.DevMachineOpScanWorktree, operation.Action)
	require.Equal(t, domain.DevMachineOpStatusPending, operation.Status)
	require.Equal(t, "worktree-scan:"+scan.ID.String(), operation.IdempotencyKey)
	require.Equal(t, 1, operation.MaxAttempts)
	require.NotNil(t, operation.CheckoutID)
	require.Equal(t, task.CheckoutID, *operation.CheckoutID)
	require.NotNil(t, operation.AgentTaskID)
	require.Equal(t, task.ID, *operation.AgentTaskID)
	require.NotNil(t, operation.RequestedByUserID)
	require.Equal(t, task.CreatorID, *operation.RequestedByUserID)

	stored, err := repo.GetWorktreeScan(ctx, task.WorkspaceID, scan.ID)
	require.NoError(t, err)
	require.NotNil(t, stored)
	require.Equal(t, scan.ID, stored.ID)
	require.Equal(t, "fixture/repo", stored.Repository)
	require.Equal(t, "/workspace/tasks/rec-1", stored.WorkspacePath)
}

func TestWorktreeScanRejectsAnotherUsersTaskAsNotFound(t *testing.T) {
	_, repo, ctx, task, _, _ := worktreeScanFixture(t)
	_, err := repo.CreateWorktreeScan(ctx, task.WorkspaceID, uuid.New(), taskScanTarget(task),
		domain.WorktreeScanRequest{Kind: domain.WorktreeScanStatus})
	require.ErrorIs(t, err, ErrConversationNotFound)
}

func TestWorktreeScanNeedsAReadyCheckoutOnARunningMachine(t *testing.T) {
	db, repo, ctx, task, claimed, _ := worktreeScanFixture(t)
	request := domain.WorktreeScanRequest{Kind: domain.WorktreeScanStatus}

	_, err := db.Exec(`UPDATE dev_machine_checkouts SET status='preparing' WHERE id=$1`, task.CheckoutID)
	require.NoError(t, err)
	_, err = repo.CreateWorktreeScan(ctx, task.WorkspaceID, task.CreatorID, taskScanTarget(task), request)
	require.ErrorIs(t, err, ErrWorktreeUnavailable)

	_, err = db.Exec(`UPDATE dev_machine_checkouts SET status='ready' WHERE id=$1`, task.CheckoutID)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE dev_machines SET status='stopped',desired_status='stopped' WHERE id=$1`, *claimed.MachineID)
	require.NoError(t, err)
	_, err = repo.CreateWorktreeScan(ctx, task.WorkspaceID, task.CreatorID, taskScanTarget(task), request)
	require.ErrorIs(t, err, ErrWorktreeUnavailable)

	_, err = db.Exec(`UPDATE dev_machines SET status='running',desired_status='running' WHERE id=$1`, *claimed.MachineID)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE dev_machines SET delete_requested_at=now() WHERE id=$1`, *claimed.MachineID)
	require.NoError(t, err)
	_, err = repo.CreateWorktreeScan(ctx, task.WorkspaceID, task.CreatorID, taskScanTarget(task), request)
	require.ErrorIs(t, err, ErrWorktreeUnavailable)
}

func TestWorktreeScanIsAllowedWhileTheAgentRuns(t *testing.T) {
	_, repo, ctx, task, _, _ := worktreeScanFixture(t)
	var state string
	require.NoError(t, repo.db.GetContext(ctx, &state, `SELECT state FROM agent_tasks WHERE id=$1`, task.ID))
	require.NotEqual(t, "awaiting_review", state, "the attempt is active")
	_, err := repo.CreateWorktreeScan(ctx, task.WorkspaceID, task.CreatorID, taskScanTarget(task),
		domain.WorktreeScanRequest{Kind: domain.WorktreeScanStatus})
	require.NoError(t, err, "a read-only scan does not need an idle task")
}

func TestWorktreeScanMachineOnlyTargetIsForListScans(t *testing.T) {
	_, repo, ctx, task, claimed, _ := worktreeScanFixture(t)
	target := domain.WorktreeScanTarget{MachineID: claimed.MachineID}

	scan, err := repo.CreateWorktreeScan(ctx, task.WorkspaceID, task.CreatorID, target,
		domain.WorktreeScanRequest{Kind: domain.WorktreeScanList})
	require.NoError(t, err)
	require.Nil(t, scan.CheckoutID)
	require.Nil(t, scan.TaskID)
	require.Empty(t, scan.WorkspacePath)
	require.NotEmpty(t, scan.MachineName)

	_, err = repo.CreateWorktreeScan(ctx, task.WorkspaceID, task.CreatorID, target,
		domain.WorktreeScanRequest{Kind: domain.WorktreeScanGrep, Pattern: "needle"})
	require.ErrorIs(t, err, ErrConversationInvalid)

	_, err = repo.CreateWorktreeScan(ctx, task.WorkspaceID, uuid.New(), target,
		domain.WorktreeScanRequest{Kind: domain.WorktreeScanList})
	require.ErrorIs(t, err, ErrWorktreeUnavailable)
}

func TestWorktreeScanValidatesItsRequest(t *testing.T) {
	_, repo, ctx, task, claimed, _ := worktreeScanFixture(t)
	machineTarget := domain.WorktreeScanTarget{MachineID: claimed.MachineID}
	for _, test := range []struct {
		name    string
		target  domain.WorktreeScanTarget
		request domain.WorktreeScanRequest
	}{
		{"unknown kind", taskScanTarget(task), domain.WorktreeScanRequest{Kind: "symbols"}},
		{"missing pattern", taskScanTarget(task), domain.WorktreeScanRequest{Kind: domain.WorktreeScanGrep}},
		{"long pattern", taskScanTarget(task), domain.WorktreeScanRequest{Kind: domain.WorktreeScanGrep, Pattern: strings.Repeat("x", domain.WorktreeScanLineRunes+1)}},
		{"long glob", taskScanTarget(task), domain.WorktreeScanRequest{Kind: domain.WorktreeScanGlob, Glob: strings.Repeat("x", 301)}},
		{"absolute read path", taskScanTarget(task), domain.WorktreeScanRequest{Kind: domain.WorktreeScanRead, Path: "/etc/passwd", StartLine: 1, EndLine: 1}},
		{"escaping read path", taskScanTarget(task), domain.WorktreeScanRequest{Kind: domain.WorktreeScanRead, Path: "src/../../secret", StartLine: 1, EndLine: 1}},
		{"nul read path", taskScanTarget(task), domain.WorktreeScanRequest{Kind: domain.WorktreeScanRead, Path: "src\x00file", StartLine: 1, EndLine: 1}},
		{"empty read path", taskScanTarget(task), domain.WorktreeScanRequest{Kind: domain.WorktreeScanRead, StartLine: 1, EndLine: 1}},
		{"zero start line", taskScanTarget(task), domain.WorktreeScanRequest{Kind: domain.WorktreeScanRead, Path: "README.md", EndLine: 1}},
		{"reversed lines", taskScanTarget(task), domain.WorktreeScanRequest{Kind: domain.WorktreeScanRead, Path: "README.md", StartLine: 4, EndLine: 2}},
		{"grep limit", taskScanTarget(task), domain.WorktreeScanRequest{Kind: domain.WorktreeScanGrep, Pattern: "x", Limit: domain.WorktreeScanMaxMatches + 1}},
		{"glob limit", taskScanTarget(task), domain.WorktreeScanRequest{Kind: domain.WorktreeScanGlob, Glob: "*.go", Limit: domain.WorktreeScanMaxFiles + 1}},
		{"negative limit", taskScanTarget(task), domain.WorktreeScanRequest{Kind: domain.WorktreeScanGrep, Pattern: "x", Limit: -1}},
		{"machine target for grep", machineTarget, domain.WorktreeScanRequest{Kind: domain.WorktreeScanGrep, Pattern: "x"}},
		{"no target", domain.WorktreeScanTarget{}, domain.WorktreeScanRequest{Kind: domain.WorktreeScanList}},
		{"both targets", domain.WorktreeScanTarget{TaskID: &task.ID, MachineID: claimed.MachineID}, domain.WorktreeScanRequest{Kind: domain.WorktreeScanList}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := repo.CreateWorktreeScan(ctx, task.WorkspaceID, task.CreatorID, test.target, test.request)
			require.ErrorIs(t, err, ErrConversationInvalid)
		})
	}
}

func TestWorktreeScanHousekeepingRemovesOnlyThisWorkspacesOldScans(t *testing.T) {
	db, repo, ctx, task, claimed, _ := worktreeScanFixture(t)
	oldID := uuid.New()
	_, err := db.Exec(`INSERT INTO dev_machine_worktree_scans(id,workspace_id,machine_id,requested_by,request,created_at)
		VALUES($1,$2,$3,$4,$5,now()-interval '2 hours')`, oldID, task.WorkspaceID, *claimed.MachineID, task.CreatorID, json.RawMessage(`{"kind":"status"}`))
	require.NoError(t, err)

	scan, err := repo.CreateWorktreeScan(ctx, task.WorkspaceID, task.CreatorID, taskScanTarget(task),
		domain.WorktreeScanRequest{Kind: domain.WorktreeScanStatus})
	require.NoError(t, err)
	require.NotNil(t, scan)

	var oldCount int
	require.NoError(t, db.Get(&oldCount, `SELECT COUNT(*) FROM dev_machine_worktree_scans WHERE id=$1`, oldID))
	require.Zero(t, oldCount, "a scan older than an hour is housekeeping")
}

func TestLeaseOperationsRunsScansBesideAgentWork(t *testing.T) {
	db, repo, ctx, task, claimed, _ := worktreeScanFixture(t)
	machine := *claimed.MachineID
	var generation int64
	require.NoError(t, db.Get(&generation, `SELECT generation FROM dev_machines WHERE id=$1`, machine))
	agentOperation := &domain.DevMachineOperation{ID: uuid.New(), WorkspaceID: task.WorkspaceID, MachineID: machine,
		AgentRunID: &task.RunID, Action: domain.DevMachineOpRunAgent, Status: domain.DevMachineOpStatusPending,
		Generation: generation, IdempotencyKey: "agent-before-scan", MaxAttempts: 1}
	require.NoError(t, repo.EnqueueInternalOperation(ctx, agentOperation))
	leased, err := repo.LeaseOperations(ctx, "scan-fixture-agent", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, leased, 1)
	require.Equal(t, agentOperation.ID, leased[0].ID)

	scan, err := repo.CreateWorktreeScan(ctx, task.WorkspaceID, task.CreatorID, taskScanTarget(task),
		domain.WorktreeScanRequest{Kind: domain.WorktreeScanStatus})
	require.NoError(t, err)
	leased, err = repo.LeaseOperations(ctx, "scan-fixture-scan", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, leased, 1, "a scan leases beside a running agent attempt")
	require.Equal(t, scan.ID, leased[0].ID)
	require.Equal(t, domain.DevMachineOpScanWorktree, leased[0].Action)

	require.NoError(t, repo.CompleteOperation(ctx, agentOperation.ID, "scan-fixture-agent"))
	other := &domain.DevMachineOperation{ID: uuid.New(), WorkspaceID: task.WorkspaceID, MachineID: machine,
		Action: domain.DevMachineOpRunCheck, Status: domain.DevMachineOpStatusPending,
		Generation: generation, IdempotencyKey: "check-beside-scan", MaxAttempts: 1}
	require.NoError(t, repo.EnqueueInternalOperation(ctx, other))
	leased, err = repo.LeaseOperations(ctx, "scan-fixture-check", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, leased, 1, "a running scan does not block other work")
	require.Equal(t, other.ID, leased[0].ID)
}
