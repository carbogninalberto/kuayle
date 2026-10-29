package repository

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func toolInput(name, status string) domain.AssistantCustomToolInput {
	return domain.AssistantCustomToolInput{Name: name, Description: "Run a Playwright spec", Parameters: json.RawMessage(`{"type":"object","properties":{"spec":{"type":"string","maxLength":200}},"required":["spec"],"additionalProperties":false}`),
		Script: `npx playwright test "$KUAYLE_ARG_SPEC"`, Interpreter: "sh", RunMode: domain.AssistantToolRunApproval, TimeoutSeconds: 600, Status: status, AuthorKind: "user"}
}

// machineFor gives the workspace an enabled policy and a running machine
// created by owner.
func machineFor(t *testing.T, f skillFixture, owner uuid.UUID) uuid.UUID {
	t.Helper()
	// Dev Machines need a workspace that never had private teams.
	_, err := f.db.Exec(`DELETE FROM teams WHERE id=$1`, f.privateTeam)
	require.NoError(t, err)
	_, err = f.db.Exec(`DELETE FROM workspace_privacy WHERE workspace_id=$1`, f.ws)
	require.NoError(t, err)
	_, err = f.db.Exec(`INSERT INTO dev_machine_workspace_policies(workspace_id,enabled) VALUES($1,true) ON CONFLICT (workspace_id) DO NOTHING`, f.ws)
	require.NoError(t, err)
	machine := uuid.New()
	_, err = f.db.Exec(`INSERT INTO dev_machines(id,workspace_id,created_by_user_id,routing_key,name,status,desired_status,generation,repo_url,repo_provider,repo_owner,repo_name,base_branch,working_branch,machine_size,cpu_millis,memory_mb,disk_gb,max_runtime_minutes,expires_at)
 VALUES($1,$2,$3,$4,'Tools','running','running',1,'','github','','','','','small',1000,2048,20,60,now()+interval '1 hour')`, machine, f.ws, owner, strings.ReplaceAll(machine.String(), "-", "")[:16])
	require.NoError(t, err)
	return machine
}

func TestCustomToolsAreWrittenByAdminsAndVersioned(t *testing.T) {
	f := newSkillFixture(t)
	repo := NewAssistantCustomToolRepository(f.db)
	_, err := repo.Create(f.as(f.member, f.ws), f.ws, f.member, toolInput("run_e2e", "active"))
	require.ErrorIs(t, err, ErrAssistantToolDenied)
	_, err = repo.Create(f.as(f.outsider, f.ws), f.ws, f.outsider, toolInput("run_e2e", "active"))
	require.ErrorIs(t, err, ErrAssistantToolDenied, "an admin of another workspace cannot write here")
	tool, err := repo.Create(f.as(f.admin, f.ws), f.ws, f.admin, toolInput("run_e2e", "active"))
	require.NoError(t, err)
	require.Equal(t, 1, tool.CurrentVersion)
	_, err = repo.Create(f.as(f.owner, f.ws), f.ws, f.owner, toolInput("run_e2e", "active"))
	require.ErrorIs(t, err, ErrAssistantToolNameTaken)

	// The assistant's edit of an active tool is a draft; the tool keeps working.
	proposed := toolInput("run_e2e", "")
	proposed.Script, proposed.RunMode = "echo changed", domain.AssistantToolRunAuto
	proposed.ReadOnly = true
	tool, version, err := repo.Propose(f.as(f.admin, f.ws), f.ws, f.admin, proposed)
	require.NoError(t, err)
	require.Equal(t, 2, version)
	require.Equal(t, 1, tool.CurrentVersion)
	require.Equal(t, 2, *tool.PendingVersion)
	require.Contains(t, tool.Script, "playwright", "the draft is not in effect")
	saved, err := repo.Version(f.as(f.member, f.ws), f.ws, tool.ID, 2)
	require.NoError(t, err)
	require.Equal(t, domain.AssistantToolRunApproval, saved.RunMode, "proposals never ask for auto runs")
	require.Equal(t, "assistant", saved.AuthorKind)
	_, err = repo.Activate(f.as(f.member, f.ws), f.ws, tool.ID, f.member, nil)
	require.ErrorIs(t, err, ErrAssistantToolDenied)
	tool, err = repo.Activate(f.as(f.owner, f.ws), f.ws, tool.ID, f.owner, nil)
	require.NoError(t, err)
	require.Equal(t, 2, tool.CurrentVersion)
	require.Equal(t, "echo changed", tool.Script)
	tool, err = repo.Restore(f.as(f.owner, f.ws), f.ws, tool.ID, f.owner, 1)
	require.NoError(t, err)
	require.Equal(t, 3, tool.CurrentVersion)
	require.Contains(t, tool.Script, "playwright")
	versions, err := repo.Versions(f.as(f.member, f.ws), f.ws, tool.ID)
	require.NoError(t, err)
	require.Len(t, versions, 3)
	list, err := repo.List(f.as(f.member, f.ws), f.ws)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Empty(t, list[0].Script, "lists leave scripts out")
	_, err = repo.Get(f.as(f.outsider, f.other), f.ws, tool.ID)
	require.ErrorIs(t, err, ErrAssistantToolNotFound)

	auto := toolInput("list_specs", "active")
	auto.RunMode = domain.AssistantToolRunAuto
	_, err = repo.Create(f.as(f.admin, f.ws), f.ws, f.admin, auto)
	require.Error(t, err, "an auto tool must be read-only")
	require.NoError(t, repo.Delete(f.as(f.admin, f.ws), f.ws, tool.ID, f.admin))
	_, err = repo.Get(f.as(f.admin, f.ws), f.ws, tool.ID)
	require.ErrorIs(t, err, ErrAssistantToolNotFound)
}

// Runs need an active, enabled tool at the checked version (test runs by
// admins excepted), a machine the requester owns that nothing else uses, and
// an approval queues one run at most.
func TestCustomToolRunsAreQueuedOnlyWhenAllowed(t *testing.T) {
	f := newSkillFixture(t)
	repo := NewAssistantCustomToolRepository(f.db)
	machines := NewDevMachineRepository(f.db)
	draft, err := repo.Create(f.as(f.admin, f.ws), f.ws, f.admin, toolInput("draft_tool", "draft"))
	require.NoError(t, err)
	tool, err := repo.Create(f.as(f.admin, f.ws), f.ws, f.admin, toolInput("run_e2e", "active"))
	require.NoError(t, err)
	mine := machineFor(t, f, f.member)
	theirs := machineFor(t, f, f.admin)
	inputs := json.RawMessage(`{"spec":"; rm -rf /"}`)
	member := f.as(f.member, f.ws)
	request := func(id uuid.UUID, machine uuid.UUID) ToolRunRequest {
		return ToolRunRequest{ToolID: id, Version: 1, Inputs: inputs, MachineID: &machine}
	}

	_, err = repo.QueueRun(member, f.ws, f.member, request(draft.ID, mine))
	require.ErrorIs(t, err, ErrToolUnavailable, "a draft tool cannot run")
	_, err = repo.QueueRun(member, f.ws, f.member, request(tool.ID, theirs))
	require.ErrorIs(t, err, ErrToolMachineUnavailable, "only on the requester's own machine")
	stale := request(tool.ID, mine)
	stale.Version = 2
	_, err = repo.QueueRun(member, f.ws, f.member, stale)
	require.ErrorIs(t, err, ErrAssistantToolNotFound)
	_, err = repo.SetEnabled(f.as(f.admin, f.ws), f.ws, tool.ID, f.admin, false)
	require.NoError(t, err)
	_, err = repo.QueueRun(member, f.ws, f.member, request(tool.ID, mine))
	require.ErrorIs(t, err, ErrToolUnavailable, "a disabled tool cannot run")
	_, err = repo.SetEnabled(f.as(f.admin, f.ws), f.ws, tool.ID, f.admin, true)
	require.NoError(t, err)
	test := request(draft.ID, mine)
	test.Test = true
	_, err = repo.QueueRun(member, f.ws, f.member, test)
	require.ErrorIs(t, err, ErrAssistantToolDenied, "members cannot test-run")

	run, err := repo.QueueRun(member, f.ws, f.member, request(tool.ID, mine))
	require.NoError(t, err)
	require.Equal(t, "queued", run.Status)
	require.Equal(t, "/workspace", run.WorkingDirectory)
	require.JSONEq(t, string(inputs), string(run.Inputs))
	_, err = repo.QueueRun(member, f.ws, f.member, request(tool.ID, mine))
	require.ErrorIs(t, err, ErrMachineBusy, "one run at a time, like checks")

	// The machine stays held until the run finishes or fails.
	var op domain.DevMachineOperation
	require.NoError(t, f.db.Get(&op, `UPDATE dev_machine_operations SET status='leased',lease_owner='test',lease_expires_at=now()+interval '1 minute',attempts=1 WHERE idempotency_key=$1 RETURNING *`, "tool-run:"+run.ID.String()))
	require.Equal(t, domain.DevMachineOpRunTool, op.Action)
	execution, err := machines.GetToolRun(t.Context(), op)
	require.NoError(t, err)
	require.Equal(t, `npx playwright test "$KUAYLE_ARG_SPEC"`, execution.Script)
	require.NoError(t, machines.StartToolRun(t.Context(), op, "container"))
	require.NoError(t, machines.AppendToolRunOutput(t.Context(), op, strings.Repeat("x", domain.CustomToolOutputChars), false))
	require.NoError(t, machines.AppendToolRunOutput(t.Context(), op, "tail\n", false))
	require.NoError(t, machines.CompleteToolRun(t.Context(), op, 3))
	done, err := repo.Run(member, f.ws, run.ID, f.member)
	require.NoError(t, err)
	require.Equal(t, "failed", done.Status)
	require.Equal(t, 3, *done.ExitCode)
	require.True(t, done.OutputTruncated)
	require.Len(t, done.Output, domain.CustomToolOutputChars, "only the tail is kept")
	require.True(t, strings.HasSuffix(done.Output, "tail\n"))
	require.NotNil(t, done.DurationMS)

	// Owners and admins read every run; other members only their own.
	_, err = repo.Run(f.as(f.admin, f.ws), f.ws, run.ID, f.admin)
	require.NoError(t, err)
	_, err = repo.Run(f.as(f.teamMember, f.ws), f.ws, run.ID, f.teamMember)
	require.ErrorIs(t, err, ErrAssistantToolNotFound)
	runs, err := repo.Runs(f.as(f.teamMember, f.ws), f.ws, tool.ID, f.teamMember, 10)
	require.NoError(t, err)
	require.Empty(t, runs)

	// A disabled tool's queued run is refused at execution.
	_, err = f.db.Exec(`UPDATE dev_machine_operations SET status='completed' WHERE id=$1`, op.ID)
	require.NoError(t, err)
	next, err := repo.QueueRun(member, f.ws, f.member, request(tool.ID, mine))
	require.NoError(t, err)
	require.NoError(t, f.db.Get(&op, `UPDATE dev_machine_operations SET status='leased',lease_owner='test',lease_expires_at=now()+interval '1 minute',attempts=1 WHERE idempotency_key=$1 RETURNING *`, "tool-run:"+next.ID.String()))
	_, err = repo.SetEnabled(f.as(f.admin, f.ws), f.ws, tool.ID, f.admin, false)
	require.NoError(t, err)
	_, err = machines.GetToolRun(t.Context(), op)
	require.ErrorIs(t, err, ErrToolUnavailable)
	require.ErrorIs(t, machines.FailToolRun(t.Context(), op, "refused"), ErrAgentTaskConflict, "only a failed operation fails its run")
	_, err = f.db.Exec(`UPDATE dev_machine_operations SET status='failed' WHERE id=$1`, op.ID)
	require.NoError(t, err)
	require.NoError(t, machines.FailToolRun(t.Context(), op, "The tool changed or was turned off before it ran"))
	failed, err := repo.Run(member, f.ws, next.ID, f.member)
	require.NoError(t, err)
	require.Equal(t, "failed", failed.Status)
	require.Equal(t, "The tool changed or was turned off before it ran", *failed.Error)
}
