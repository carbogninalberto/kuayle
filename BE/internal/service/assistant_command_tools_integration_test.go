package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// temporaryMachinesFake stands in for the Dev Machine service: it records a
// running temporary machine of the preset, as provisioning ends.
type temporaryMachinesFake struct {
	db      *sqlx.DB
	started int
}

func (f *temporaryMachinesFake) StartTemporaryMachine(_ context.Context, ws, user, conversation uuid.UUID) (*domain.DevMachine, error) {
	f.started++
	preset := domain.TemporaryMachineResources
	machine := &domain.DevMachine{}
	err := f.db.Get(machine, `INSERT INTO dev_machines(id,workspace_id,created_by_user_id,routing_key,name,status,desired_status,generation,repo_url,repo_provider,repo_owner,repo_name,base_branch,working_branch,machine_size,profile,cpu_millis,memory_mb,disk_gb,temporary_conversation_id,temporary_expires_at)
 VALUES(gen_random_uuid(),$1,$2,substr(md5(random()::text),1,16),'tmp-1a2b3c4d','running','running',1,'','github','','','','','custom','headless',$3,$4,$5,$6,now()+interval '30 minutes') RETURNING *`,
		ws, user, preset.CPUMillis, preset.MemoryMB, preset.DiskGB, conversation)
	return machine, err
}

type commandToolsFixture struct {
	toolFixture
	commands  *repository.AssistantCommandRepository
	temporary *temporaryMachinesFake
}

func newCommandToolsFixture(t *testing.T) commandToolsFixture {
	t.Helper()
	f := newToolFixture(t)
	_, err := f.db.Exec(`INSERT INTO dev_machine_workspace_policies(workspace_id,enabled) VALUES($1,true)`, f.ws)
	require.NoError(t, err)
	f.service.SetApprovals(NewAssistantApprovals(f.repo, nil))
	commands := repository.NewAssistantCommandRepository(f.db)
	temporary := &temporaryMachinesFake{db: f.db}
	f.service.tools.SetCommands(commands, temporary)
	f.service.approvals.SetCommands(commands, temporary)
	return commandToolsFixture{toolFixture: f, commands: commands, temporary: temporary}
}

// ownMachine gives the user a running machine of their own.
func (f commandToolsFixture) ownMachine(t *testing.T, owner uuid.UUID, name string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := f.db.Exec(`INSERT INTO dev_machines(id,workspace_id,created_by_user_id,routing_key,name,status,desired_status,generation,repo_url,repo_provider,repo_owner,repo_name,base_branch,working_branch,machine_size,cpu_millis,memory_mb,disk_gb,last_activity_at)
 VALUES($1,$2,$3,$4,$5,'running','running',1,'','github','','','','','medium',4000,8192,40,now())`, id, f.ws, owner, strings.ReplaceAll(id.String(), "-", "")[:16], name)
	require.NoError(t, err)
	return id
}

// call drives a reply in a conversation that calls one tool, and returns what
// the model was told; attempt moves on to the conversation's next reply.
func (f commandToolsFixture) call(t *testing.T, attempt *domain.ConversationAttempt, tool, arguments string) string {
	t.Helper()
	provider, requests := scriptedProvider(t, toolCallStream(tool, arguments), finalAnswer)
	require.NoError(t, f.service.converse(f.ctx, *attempt, provider.URL, "key", []chatCompletionMessage{{Role: "user", Content: tool}}, func(string) error { return nil }))
	history := (*requests)[1]["messages"].([]any)
	*attempt = f.next(t, *attempt, "next")
	return history[len(history)-1].(map[string]any)["content"].(string)
}

// next starts another reply in the same conversation.
func (f commandToolsFixture) next(t *testing.T, a domain.ConversationAttempt, content string) domain.ConversationAttempt {
	t.Helper()
	_, err := f.db.Exec(`UPDATE conversation_messages SET status='completed',lease_token=NULL,lease_expires_at=NULL WHERE id=$1`, a.Message.ID)
	require.NoError(t, err)
	reply, err := f.repo.Send(f.ctx, f.ws, a.Conversation.ID, uuid.New(), content, "deepseek", "model")
	require.NoError(t, err)
	var message domain.ConversationMessage
	require.NoError(t, f.db.Get(&message, `UPDATE conversation_messages SET status='streaming',lease_token=gen_random_uuid(),lease_expires_at=now()+interval '2 minutes' WHERE id=$1 RETURNING *`, reply.ID))
	return domain.ConversationAttempt{Conversation: a.Conversation, Message: message}
}

func (f commandToolsFixture) approvals(t *testing.T, a domain.ConversationAttempt) []domain.ConversationApproval {
	t.Helper()
	h, err := f.repo.History(f.ctx, f.ws, a.Conversation.ID, 0)
	require.NoError(t, err)
	return h.Approvals
}

// runQueued plays the machine manager: it runs the queued command and stores
// its output and exit code.
func (f commandToolsFixture) runQueued(t *testing.T, run uuid.UUID, output string) {
	t.Helper()
	var op domain.DevMachineOperation
	require.NoError(t, f.db.Get(&op, `UPDATE dev_machine_operations SET status='leased',lease_owner='test',lease_expires_at=now()+interval '1 minute',attempts=1 WHERE idempotency_key=$1 RETURNING *`, "command-run:"+run.String()))
	machines := repository.NewDevMachineRepository(f.db)
	require.NoError(t, machines.StartCommandRun(f.ctx, op))
	require.NoError(t, machines.AppendCommandRunOutput(f.ctx, op, output, false))
	require.NoError(t, machines.CompleteCommandRun(f.ctx, op, 0))
}

// The acceptance path: with no machine of their own the user approves a
// temporary machine, then the command; the result is the real output and
// names the machine, its size and that it was not reused.
func TestAssistantComputesOnATemporaryMachineAfterApprovals(t *testing.T) {
	f := newCommandToolsFixture(t)
	a := f.attempt(t, "compute 2**100 in python")

	content := f.call(t, &a, "run_command", `{"command":"python3 -c 'print(2**100)'"}`)
	require.Contains(t, content, noCommandMachine, "it says what it can do without a machine")

	content = f.call(t, &a, "start_temporary_machine", `{}`)
	require.Contains(t, content, "pending_user_approval")
	require.Zero(t, f.temporary.started, "asking starts nothing")
	approvals := f.approvals(t, a)
	require.Len(t, approvals, 1)
	var machinePayload domain.TemporaryMachinePayload
	require.NoError(t, json.Unmarshal(approvals[0].Payload, &machinePayload))
	require.Equal(t, domain.TemporaryMachinePayload{CPUMillis: 1000, MemoryMB: 2048, DiskGB: 20, Profile: "headless", TTLMinutes: 30, QuotaUsed: 0, QuotaLimit: 2,
		WorkingDirectory: domain.CommandFolder(a.Conversation.ID)}, machinePayload)
	require.Equal(t, uuid.Nil, approvals[0].IssueID, "it belongs to no issue")
	approved, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approvals[0].ID, true)
	require.NoError(t, err)
	require.Equal(t, "approved", approved.Status)
	again, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approvals[0].ID, true)
	require.NoError(t, err)
	require.Equal(t, "approved", again.Status)
	require.Equal(t, 1, f.temporary.started, "approving again starts no second machine")

	content = f.call(t, &a, "run_command", `{"command":"python3 -c 'print(2**100)'"}`)
	require.Contains(t, content, "pending_user_approval")
	approvals = f.approvals(t, a)
	require.Len(t, approvals, 2)
	command := approvals[1]
	require.Equal(t, "run_command", command.Action)
	var payload domain.CommandRunPayload
	require.NoError(t, json.Unmarshal(command.Payload, &payload))
	require.Equal(t, "python3 -c 'print(2**100)'", payload.Command)
	require.Equal(t, "tmp-1a2b3c4d", payload.Machine)
	require.Equal(t, 1000, payload.CPUMillis)
	require.Equal(t, 2048, payload.MemoryMB)
	require.True(t, payload.Temporary)
	require.False(t, payload.Reused)
	require.False(t, payload.Write)
	require.NotNil(t, payload.ExpiresAt)
	require.Equal(t, "/workspace", payload.WorkingDirectory)
	require.Equal(t, 60, payload.TimeoutSeconds)

	var queued int
	require.NoError(t, f.db.Get(&queued, `SELECT count(*) FROM assistant_command_runs WHERE workspace_id=$1`, f.ws))
	require.Zero(t, queued, "nothing runs before approval")
	approved, err = f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, command.ID, true)
	require.NoError(t, err)
	require.Equal(t, "approved", approved.Status)
	run, err := f.commands.RunForApproval(f.ctx, f.ws, command.ID, f.user)
	require.NoError(t, err)
	f.runQueued(t, run.ID, "1267650600228229401496703205376\n")

	content = f.call(t, &a, "get_command_run", `{"approval_id":"`+command.ID.String()+`"}`)
	var result struct {
		Data map[string]any `json:"untrusted_data"`
	}
	require.NoError(t, json.Unmarshal([]byte(content), &result))
	require.Equal(t, "succeeded", result.Data["status"])
	require.Equal(t, "1267650600228229401496703205376\n", result.Data["output_tail"])
	require.Equal(t, "tmp-1a2b3c4d", result.Data["machine"])
	require.EqualValues(t, 1000, result.Data["cpu_millis"])
	require.EqualValues(t, 2048, result.Data["memory_mb"])
	require.EqualValues(t, 20, result.Data["disk_gb"])
	require.Equal(t, false, result.Data["reused"])
	require.Equal(t, true, result.Data["temporary"])
	require.NotEmpty(t, result.Data["expires_at"])
	require.EqualValues(t, 0, result.Data["exit_code"])
}

// The user's own running machine is reused at once, without an approval card,
// and a write command starts in the conversation's folder on it.
func TestAssistantReusesTheUsersOwnMachine(t *testing.T) {
	f := newCommandToolsFixture(t)
	machine := f.ownMachine(t, f.user, "laptop-box")
	a := f.attempt(t, "check this snippet")

	content := f.call(t, &a, "start_temporary_machine", `{}`)
	require.NotContains(t, content, "pending_user_approval")
	require.Contains(t, content, `"reused":true`)
	require.Contains(t, content, `"machine":"laptop-box"`)
	require.Contains(t, content, `"cpu_millis":4000`)
	require.Contains(t, content, domain.CommandFolder(a.Conversation.ID))
	require.Empty(t, f.approvals(t, a), "nothing starts, so nothing needs approval")
	require.Zero(t, f.temporary.started)

	content = f.call(t, &a, "run_command_write", `{"command":"echo hi > note.txt"}`)
	require.Contains(t, content, "pending_user_approval", "commands always need approval, even on the user's own machine")
	approvals := f.approvals(t, a)
	require.Len(t, approvals, 1)
	var payload domain.CommandRunPayload
	require.NoError(t, json.Unmarshal(approvals[0].Payload, &payload))
	require.Equal(t, machine, payload.MachineID)
	require.True(t, payload.Reused)
	require.True(t, payload.Write)
	require.False(t, payload.Temporary)
	require.Equal(t, domain.CommandFolder(a.Conversation.ID), payload.WorkingDirectory)

	approved, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approvals[0].ID, true)
	require.NoError(t, err)
	require.Equal(t, "approved", approved.Status)
	run, err := f.commands.RunForApproval(f.ctx, f.ws, approvals[0].ID, f.user)
	require.NoError(t, err)
	require.True(t, run.Write)
	require.True(t, run.Reused)
	require.Equal(t, machine, *run.MachineID)

	// The approval card reads the run of its own conversation only.
	card, err := f.service.CommandRun(f.ctx, f.ws, a.Conversation.ID, approvals[0].ID)
	require.NoError(t, err)
	require.Equal(t, "laptop-box", card["machine"])
	require.Equal(t, true, card["reused"])
	require.Equal(t, "queued", card["status"])
	_, err = f.service.CommandRun(f.ctx, f.ws, uuid.New(), approvals[0].ID)
	require.ErrorIs(t, err, repository.ErrConversationNotFound)
}

// A guest, and anyone naming a machine they do not own, is refused with the
// next step.
func TestAssistantCommandsRefuseGuestsAndOtherUsersMachines(t *testing.T) {
	f := newCommandToolsFixture(t)
	other := uuid.New()
	_, err := f.db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Other','hash')`, other, other.String()+"@test.invalid")
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = f.db.Exec(`DELETE FROM users WHERE id=$1`, other) })
	_, err = f.db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'member')`, f.ws, other)
	require.NoError(t, err)
	theirs := f.ownMachine(t, other, "their-box")
	a := f.attempt(t, "run it on their machine")

	content := f.call(t, &a, "run_command", `{"command":"cat /workspace/secret","machine_id":"`+theirs.String()+`"}`)
	require.Contains(t, content, "Only the machine's owner can run commands on it")
	require.Contains(t, content, "start_temporary_machine", "with the next step")
	require.Empty(t, f.approvals(t, a))

	// Conversations already refuse guests; the tools refuse them too, with the
	// next step, wherever a guest's request could reach them.
	_, err = f.db.Exec(`UPDATE workspace_members SET role='guest' WHERE workspace_id=$1 AND user_id=$2`, f.ws, f.user)
	require.NoError(t, err)
	guest := "Guests cannot run commands; ask a workspace member to run it, or ask an admin to change your role."
	for _, tool := range []string{"run_command", "run_command_write"} {
		_, err = f.service.tools.prepareCommand(f.ctx, a, tool, `{"command":"ls"}`)
		require.EqualError(t, err, guest, tool)
	}
	_, _, err = f.service.tools.prepareTemporaryMachine(f.ctx, a, `{}`)
	require.EqualError(t, err, guest)
	_, _, err = f.service.tools.getCommandRun(f.ctx, f.ws, f.user, `{"run_id":"`+uuid.NewString()+`"}`)
	require.EqualError(t, err, guest)
}

// An approval re-checks the machine: one that stopped after the card was
// shown runs nothing.
func TestAssistantCommandApprovalRechecksTheMachine(t *testing.T) {
	f := newCommandToolsFixture(t)
	machine := f.ownMachine(t, f.user, "laptop-box")
	a := f.attempt(t, "list files")
	require.Contains(t, f.call(t, &a, "run_command", `{"command":"ls"}`), "pending_user_approval")
	approval := f.approvals(t, a)[0]
	_, err := f.db.Exec(`UPDATE dev_machines SET status='stopped',desired_status='stopped' WHERE id=$1`, machine)
	require.NoError(t, err)
	result, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approval.ID, true)
	require.NoError(t, err)
	require.Equal(t, "failed", result.Status)
	require.Contains(t, *result.Error, "Only the machine's owner can run commands on it, and it must be running")
	var runs int
	require.NoError(t, f.db.Get(&runs, `SELECT count(*) FROM assistant_command_runs WHERE workspace_id=$1`, f.ws))
	require.Zero(t, runs)
}

func TestAssistantTemporaryMachineQuotaIsExplained(t *testing.T) {
	f := newCommandToolsFixture(t)
	a := f.attempt(t, "start a machine")
	_, err := f.db.Exec(`UPDATE dev_machine_workspace_policies SET max_temporary_machines=0 WHERE workspace_id=$1`, f.ws)
	require.NoError(t, err)
	require.Contains(t, f.call(t, &a, "start_temporary_machine", `{}`), "Temporary machines are turned off in this workspace")
	require.Empty(t, f.approvals(t, a))
}

func TestAssistantCommandToolsHonourTheirSwitches(t *testing.T) {
	f := newCommandToolsFixture(t)
	f.ownMachine(t, f.user, "laptop-box")
	f.service.tools.SetSettings(worktreeSettings{"run_command": true})
	a := f.attempt(t, "list files")
	require.Contains(t, f.call(t, &a, "run_command", `{"command":"ls"}`), "run_command is disabled in this workspace")
	require.Empty(t, f.approvals(t, a))
}

// The guidance is in the system prompt only while run_command is offered.
func TestCommandPromptOnlyWhenTheToolIsOffered(t *testing.T) {
	tools := &AssistantTools{}
	require.Empty(t, tools.commandPrompt(context.Background(), uuid.New()), "not wired")
	tools.SetCommands(&repository.AssistantCommandRepository{}, &temporaryMachinesFake{})
	prompt := tools.commandPrompt(context.Background(), uuid.New())
	require.Contains(t, prompt, "only when no other tool can answer")
	require.Contains(t, prompt, "propose start_temporary_machine only when they have none")
	require.Contains(t, prompt, "machine, its size and whether it was reused")
	tools.SetSettings(worktreeSettings{"start_temporary_machine": true})
	require.NotContains(t, tools.commandPrompt(context.Background(), uuid.New()), "start_temporary_machine")
	tools.SetSettings(worktreeSettings{"run_command": true})
	require.Empty(t, tools.commandPrompt(context.Background(), uuid.New()))
	require.Equal(t, time.Duration(assistantCommandWait), tools.commandWait)
}
