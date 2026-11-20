package repository

import (
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestMachineLogReadsAreOwnerAndTaskScoped(t *testing.T) {
	db, repo, ctx, task, claimed, _ := worktreeScanFixture(t)
	ws, owner, machine := task.WorkspaceID, task.CreatorID, *claimed.MachineID
	manual := uuid.New()
	_, err := db.Exec(`INSERT INTO dev_machine_agent_runs(id,workspace_id,machine_id,provider_id,mode,prompt,command_argv,max_runtime_seconds,status)
		VALUES($1,$2,$3,'codex','autonomous','Manual work','["codex"]',60,'succeeded')`, manual, ws, machine)
	require.NoError(t, err)
	for _, run := range []uuid.UUID{manual, task.RunID} {
		_, err := db.Exec(`INSERT INTO dev_machine_log_chunks(workspace_id,machine_id,agent_run_id,stream,sequence,content)
			SELECT $1,$2,$3::uuid,'stdout',n,$3::text||':'||n FROM generate_series(1,4) n ORDER BY n`, ws, machine, run)
		require.NoError(t, err)
	}
	check := func(workspace, user, id uuid.UUID, taskID *uuid.UUID, expected ...string) {
		t.Helper()
		chunks, clipped, err := repo.RecentAgentLogs(ctx, workspace, id, user, taskID)
		require.False(t, clipped)
		require.NoError(t, err)
		var contents []string
		for _, chunk := range chunks[:min(2, len(chunks))] {
			contents = append(contents, chunk.Content)
		}
		require.Equal(t, expected, contents)
	}
	check(ws, owner, machine, &task.ID, task.RunID.String()+":4", task.RunID.String()+":3")
	check(ws, owner, machine, nil, task.RunID.String()+":4", task.RunID.String()+":3")
	check(ws, uuid.New(), machine, nil)
	check(uuid.New(), owner, machine, nil)
	check(ws, owner, uuid.New(), nil)
	otherTask := uuid.New()
	check(ws, owner, machine, &otherTask)
	for _, req := range []domain.WorktreeScanRequest{
		{Kind: domain.MachineScanContainers, Limit: 3},
		{Kind: domain.MachineScanContainerLogs, Limit: 2, Container: "api-1"},
	} {
		target := domain.WorktreeScanTarget{MachineID: &machine}
		_, err = repo.CreateWorktreeScan(ctx, ws, uuid.New(), target, req)
		require.ErrorIs(t, err, ErrWorktreeUnavailable)
		_, err = repo.CreateWorktreeScan(ctx, ws, owner, taskScanTarget(task), req)
		require.ErrorIs(t, err, ErrConversationInvalid)
		scan, err := repo.CreateWorktreeScan(ctx, ws, owner, target, req)
		require.NoError(t, err)
		require.Nil(t, scan.CheckoutID)
	}
	otherTeam := uuid.New()
	_, err = db.Exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Other','OTH')`, otherTeam, ws)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE issues SET team_id=$2 WHERE id=$1`, task.IssueID, otherTeam)
	require.NoError(t, err)
	check(ws, owner, machine, &task.ID)
	check(ws, owner, machine, nil, manual.String()+":4", manual.String()+":3")
}
