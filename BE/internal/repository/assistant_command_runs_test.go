package repository

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func leaseCommandRun(t *testing.T, f skillFixture, run *domain.CommandRun) domain.DevMachineOperation {
	t.Helper()
	var op domain.DevMachineOperation
	require.NoError(t, f.db.Get(&op, `UPDATE dev_machine_operations SET status='leased',lease_owner='test',lease_expires_at=now()+interval '1 minute',attempts=1 WHERE idempotency_key=$1 RETURNING *`, "command-run:"+run.ID.String()))
	return op
}

// A command runs only on a running machine the requester owns, once per
// approval, one at a time; the machine's name and size stay on the run.
func TestCommandRunsAreQueuedOnlyOnTheRequestersMachine(t *testing.T) {
	f := newSkillFixture(t)
	commands := NewAssistantCommandRepository(f.db)
	machines := NewDevMachineRepository(f.db)
	mine := machineFor(t, f, f.member)
	theirs := machineFor(t, f, f.admin)
	member := f.as(f.member, f.ws)
	request := func(machine uuid.UUID) CommandRunRequest {
		return CommandRunRequest{MachineID: machine, Command: "python3 -c 'print(2**100)'", WorkingDirectory: "/workspace"}
	}

	_, err := commands.QueueRun(member, f.ws, f.member, request(theirs))
	require.ErrorIs(t, err, ErrCommandMachineUnavailable, "never on another user's machine")
	_, err = commands.QueueRun(member, f.ws, f.outsider, request(mine))
	require.ErrorIs(t, err, ErrConversationNotFound, "outsiders cannot run commands")
	invalid := request(mine)
	invalid.WorkingDirectory = "/etc"
	_, err = commands.QueueRun(member, f.ws, f.member, invalid)
	require.ErrorIs(t, err, ErrConversationInvalid)

	run, err := commands.QueueRun(member, f.ws, f.member, request(mine))
	require.NoError(t, err)
	require.Equal(t, "queued", run.Status)
	require.Equal(t, "Tools", run.MachineName)
	require.Equal(t, 1000, run.CPUMillis)
	require.Equal(t, 2048, run.MemoryMB)
	require.Equal(t, 20, run.DiskGB)
	require.False(t, run.Temporary)
	_, err = commands.QueueRun(member, f.ws, f.member, request(mine))
	require.ErrorIs(t, err, ErrMachineBusy, "one command at a time, like checks")

	op := leaseCommandRun(t, f, run)
	require.Equal(t, domain.DevMachineOpRunCommand, op.Action)
	require.Equal(t, 1, op.MaxAttempts, "a command is never run twice")
	execution, err := machines.GetCommandRun(t.Context(), op)
	require.NoError(t, err)
	require.Equal(t, run.Command, execution.Command)
	require.NoError(t, machines.StartCommandRun(t.Context(), op))
	require.NoError(t, machines.AppendCommandRunOutput(t.Context(), op, strings.Repeat("x", domain.CommandOutputBytes), false))
	require.NoError(t, machines.AppendCommandRunOutput(t.Context(), op, "1267650600228229401496703205376\n", false))
	require.NoError(t, machines.CompleteCommandRun(t.Context(), op, 0))
	done, err := commands.Run(member, f.ws, run.ID, f.member)
	require.NoError(t, err)
	require.Equal(t, "succeeded", done.Status)
	require.Equal(t, 0, *done.ExitCode)
	require.True(t, done.OutputTruncated)
	require.Len(t, done.Output, domain.CommandOutputBytes, "only the tail is kept")
	require.True(t, strings.HasSuffix(done.Output, "1267650600228229401496703205376\n"))
	require.NotNil(t, done.DurationMS)
	_, err = commands.Run(f.as(f.admin, f.ws), f.ws, run.ID, f.admin)
	require.ErrorIs(t, err, ErrCommandRunNotFound, "only the requester reads a run")
}

func TestCommandRunsAreQueuedOncePerApprovalAndReCheckTheRequester(t *testing.T) {
	f := newSkillFixture(t)
	commands := NewAssistantCommandRepository(f.db)
	machines := NewDevMachineRepository(f.db)
	mine := machineFor(t, f, f.member)
	member := f.as(f.member, f.ws)
	conversation, toolCall, approval := uuid.New(), uuid.New(), uuid.New()
	_, err := f.db.Exec(`INSERT INTO conversations(id,workspace_id,creator_id,title) VALUES($1,$2,$3,'Commands')`, conversation, f.ws, f.member)
	require.NoError(t, err)
	message := uuid.New()
	_, err = f.db.Exec(`INSERT INTO conversation_messages(id,conversation_id,request_id,role,content,status) VALUES($1,$2,gen_random_uuid(),'user','run it','completed')`, message, conversation)
	require.NoError(t, err)
	_, err = f.db.Exec(`INSERT INTO conversation_tool_calls(id,conversation_id,message_id,sequence,call_id,name,input,status) VALUES($1,$3,$2,0,'call','run_command_write','{}','running')`, toolCall, message, conversation)
	require.NoError(t, err)
	_, err = f.db.Exec(`INSERT INTO conversation_approvals(id,conversation_id,tool_call_id,action,actor_id,payload,status,expires_at) VALUES($1,$2,$3,'run_command_write',$4,'{}','approving',now()+interval '1 hour')`, approval, conversation, toolCall, f.member)
	require.NoError(t, err)
	request := CommandRunRequest{MachineID: mine, Command: "touch out.txt", Write: true, WorkingDirectory: domain.CommandFolder(conversation), Reused: true,
		ConversationID: &conversation, ToolCallID: &toolCall, ApprovalID: &approval}

	run, err := commands.QueueRun(member, f.ws, f.member, request)
	require.NoError(t, err)
	again, err := commands.QueueRun(member, f.ws, f.member, request)
	require.NoError(t, err)
	require.Equal(t, run.ID, again.ID, "approving again converges on the same run")
	byApproval, err := commands.RunForApproval(member, f.ws, approval, f.member)
	require.NoError(t, err)
	require.Equal(t, run.ID, byApproval.ID)
	require.True(t, byApproval.Write)
	require.True(t, byApproval.Reused)

	// The requester lost their role before the run started.
	op := leaseCommandRun(t, f, run)
	_, err = f.db.Exec(`UPDATE workspace_members SET role='guest' WHERE workspace_id=$1 AND user_id=$2`, f.ws, f.member)
	require.NoError(t, err)
	_, err = machines.GetCommandRun(t.Context(), op)
	require.ErrorIs(t, err, ErrCommandRunForbidden)
	require.ErrorIs(t, machines.FailCommandRun(t.Context(), op, "refused"), ErrAgentTaskConflict, "only a failed operation fails its run")
	_, err = f.db.Exec(`UPDATE dev_machine_operations SET status='failed' WHERE id=$1`, op.ID)
	require.NoError(t, err)
	require.NoError(t, machines.FailCommandRun(t.Context(), op, "The run is no longer allowed on this machine"))
	var status, reason string
	require.NoError(t, f.db.QueryRow(`SELECT status,error FROM assistant_command_runs WHERE id=$1`, run.ID).Scan(&status, &reason))
	require.Equal(t, "failed", status)
	require.Equal(t, "The run is no longer allowed on this machine", reason)
}

// Temporary machines count against the workspace quota and one per user;
// reuse prefers the user's own running machines without an issue.
func TestTemporaryMachinesAreCountedAndReuseFindsTheUsersMachine(t *testing.T) {
	f := newSkillFixture(t)
	commands := NewAssistantCommandRepository(f.db)
	machines := NewDevMachineRepository(f.db)
	_, err := commands.ReusableMachine(f.as(f.member, f.ws), f.ws, f.member)
	require.ErrorIs(t, err, ErrCommandMachineUnavailable)

	mine := machineFor(t, f, f.member)
	machineFor(t, f, f.admin)
	reused, err := commands.ReusableMachine(f.as(f.member, f.ws), f.ws, f.member)
	require.NoError(t, err)
	require.Equal(t, mine, reused.ID)

	_, err = f.db.Exec(`UPDATE dev_machines SET name=$2 WHERE created_by_user_id=$1`, f.admin, "admin-own")
	require.NoError(t, err)
	temporary := machineFor(t, f, f.admin)
	conversation := uuid.New()
	_, err = f.db.Exec(`INSERT INTO conversations(id,workspace_id,creator_id,title) VALUES($1,$2,$3,'Temporary')`, conversation, f.ws, f.admin)
	require.NoError(t, err)
	_, err = f.db.Exec(`UPDATE dev_machines SET temporary_expires_at=now()+interval '30 minutes',temporary_conversation_id=$2 WHERE id=$1`, temporary, conversation)
	require.NoError(t, err)
	used, limit, mineCount, err := commands.TemporaryMachineQuota(f.as(f.admin, f.ws), f.ws, f.admin)
	require.NoError(t, err)
	require.Equal(t, 1, used)
	require.Equal(t, domain.DefaultMaxTemporaryMachines, limit)
	require.Equal(t, 1, mineCount, "the live temporary machine belongs to the admin")
	_, _, mineCount, err = commands.TemporaryMachineQuota(f.as(f.member, f.ws), f.ws, f.member)
	require.NoError(t, err)
	require.Zero(t, mineCount, "it is not the member's")
	userTemporary, err := commands.UserTemporaryMachine(f.as(f.admin, f.ws), f.ws, f.admin)
	require.NoError(t, err)
	require.Equal(t, temporary, userTemporary.ID, "found whichever conversation started it")
	_, err = commands.UserTemporaryMachine(f.as(f.member, f.ws), f.ws, f.member)
	require.ErrorIs(t, err, ErrCommandMachineUnavailable, "only the owner's")
	found, err := commands.ConversationMachine(f.as(f.admin, f.ws), f.ws, f.admin, conversation)
	require.NoError(t, err)
	require.Equal(t, temporary, found.ID)
	_, err = commands.ConversationMachine(f.as(f.member, f.ws), f.ws, f.member, conversation)
	require.ErrorIs(t, err, ErrCommandMachineUnavailable, "only the owner's")
	adminReuse, err := commands.ReusableMachine(f.as(f.admin, f.ws), f.ws, f.admin)
	require.NoError(t, err)
	require.NotEqual(t, temporary, adminReuse.ID, "a temporary machine is never reused as the user's own")

	tx, err := f.db.Beginx()
	require.NoError(t, err)
	err = checkTemporaryMachineQuota(t.Context(), tx, f.ws, &f.admin)
	require.ErrorIs(t, err, ErrMachineQuota, "one temporary machine per user")
	require.NoError(t, checkTemporaryMachineQuota(t.Context(), tx, f.ws, &f.member))
	_, err = tx.Exec(`UPDATE dev_machine_workspace_policies SET max_temporary_machines=1 WHERE workspace_id=$1`, f.ws)
	require.NoError(t, err)
	err = checkTemporaryMachineQuota(t.Context(), tx, f.ws, &f.member)
	require.ErrorIs(t, err, ErrMachineQuota)
	require.Contains(t, err.Error(), "1 of 1 allowed temporary machines")
	_, err = tx.Exec(`UPDATE dev_machine_workspace_policies SET max_temporary_machines=0 WHERE workspace_id=$1`, f.ws)
	require.NoError(t, err)
	require.ErrorIs(t, checkTemporaryMachineQuota(t.Context(), tx, f.ws, &f.member), ErrTemporaryMachinesOff)
	require.NoError(t, tx.Rollback())

	// Only expired machines not yet being deleted are swept.
	expired, err := machines.ListExpiredTemporaryMachines(t.Context(), time.Now().Add(time.Hour), 100)
	require.NoError(t, err)
	require.Contains(t, machineIDs(expired), temporary)
	expired, err = machines.ListExpiredTemporaryMachines(t.Context(), time.Now(), 100)
	require.NoError(t, err)
	require.NotContains(t, machineIDs(expired), temporary, "a live temporary machine stays")
	require.NotContains(t, machineIDs(expired), mine)
}

// The role is re-checked under the transaction that creates a temporary
// machine: a guest, or a user who left the workspace, gets none, each with its
// own error.
func TestTemporaryMachineBundleRechecksTheOwnersRole(t *testing.T) {
	f := newSkillFixture(t)
	machineFor(t, f, f.admin) // enables Dev Machines in the workspace
	machines := NewDevMachineRepository(f.db)
	commands := NewAssistantCommandRepository(f.db)
	create := func(owner uuid.UUID) error {
		id := uuid.New()
		routingKey := strings.ReplaceAll(id.String(), "-", "")[:16]
		expires := time.Now().Add(30 * time.Minute)
		return machines.CreateBundle(t.Context(), &domain.DevMachine{
			ID: id, WorkspaceID: f.ws, CreatedByUserID: &owner, RoutingKey: routingKey, Name: "tmp-" + routingKey[:8],
			Status: domain.DevMachineStatusQueued, DesiredStatus: domain.DevMachineStatusRunning, Generation: 1, RepoProvider: "github",
			MachineSize: "custom", Profile: domain.DevMachineProfileHeadless, CPUMillis: 1000, MemoryMB: 2048, DiskGB: 20, PidsLimit: 512,
			ServicesConfig: []byte(`{}`), Labels: []byte(`{}`), TemporaryExpiresAt: &expires,
		}, nil, nil, nil, nil, nil, &domain.DevMachineOperation{
			ID: uuid.New(), MachineID: id, WorkspaceID: f.ws, Action: domain.DevMachineOpSpawn,
			Status: domain.DevMachineOpStatusPending, Generation: 1, IdempotencyKey: "spawn:1", RequestedByUserID: &owner, MaxAttempts: 5,
		})
	}
	temporaryMachines := func(owner uuid.UUID) int {
		var n int
		require.NoError(t, f.db.Get(&n, `SELECT count(*) FROM dev_machines WHERE workspace_id=$1 AND created_by_user_id=$2 AND temporary_expires_at IS NOT NULL`, f.ws, owner))
		return n
	}

	_, err := f.db.Exec(`UPDATE workspace_members SET role='guest' WHERE workspace_id=$1 AND user_id=$2`, f.ws, f.member)
	require.NoError(t, err)
	require.ErrorIs(t, create(f.member), ErrCommandRunGuest)
	require.ErrorIs(t, commands.CheckRunner(t.Context(), f.ws, f.member), ErrCommandRunGuest)
	require.Zero(t, temporaryMachines(f.member))

	require.ErrorIs(t, create(f.outsider), ErrConversationNotFound, "a non-member gets none")
	require.ErrorIs(t, commands.CheckRunner(t.Context(), f.ws, f.outsider), ErrConversationNotFound)
	require.Zero(t, temporaryMachines(f.outsider))

	require.NoError(t, commands.CheckRunner(t.Context(), f.ws, f.admin))
	require.NoError(t, create(f.admin))
	require.Equal(t, 1, temporaryMachines(f.admin))
}

func machineIDs(machines []domain.DevMachine) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(machines))
	for _, machine := range machines {
		ids = append(ids, machine.ID)
	}
	return ids
}
