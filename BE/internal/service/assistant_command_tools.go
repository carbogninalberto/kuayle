package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
)

// The command tools run short commands on a Dev Machine the user owns: one of
// their running machines (reused, in a folder of the conversation) or a small
// temporary machine started for the conversation. Every command and starting a
// temporary machine need the user's approval; the command runs in a
// credential-free check container with a hard timeout and output cap.

const (
	// The model sees this much of a command's output, from the end. The tool
	// result, JSON-escaped, must stay within assistantToolOutputBytes.
	assistantCommandOutputBytes = 12 << 10
	// get_command_run waits this long for a queued or running command.
	assistantCommandWait = domain.CommandRunTimeout + 15*time.Second
)

// noCommandMachine is what the assistant can do when no machine is available.
const noCommandMachine = "No machine is available to run commands. Call start_temporary_machine to propose a small temporary machine (1 vCPU, 2 GB, 30 min, no credentials), or give machine_id of one of your running machines."

// notMachineOwner refuses a machine the user does not own or that is not
// running, with the next step.
const notMachineOwner = "only the machine's owner can run commands on it, and it must be running; give machine_id of one of your running machines or call start_temporary_machine"

type assistantCommandStore interface {
	QueueRun(ctx context.Context, ws, actor uuid.UUID, req repository.CommandRunRequest) (*domain.CommandRun, error)
	Run(ctx context.Context, ws, id, actor uuid.UUID) (*domain.CommandRun, error)
	RunForApproval(ctx context.Context, ws, approval, actor uuid.UUID) (*domain.CommandRun, error)
	UsableMachine(ctx context.Context, ws, user, machine uuid.UUID) (*domain.DevMachine, error)
	ConversationMachine(ctx context.Context, ws, user, conversation uuid.UUID) (*domain.DevMachine, error)
	ReusableMachine(ctx context.Context, ws, user uuid.UUID) (*domain.DevMachine, error)
	TemporaryMachineQuota(ctx context.Context, ws uuid.UUID) (int, int, error)
}

// assistantTemporaryMachines starts temporary machines with the Dev Machine
// service's policy, quota and demo checks.
type assistantTemporaryMachines interface {
	StartTemporaryMachine(ctx context.Context, ws, user, conversation uuid.UUID) (*domain.DevMachine, error)
}

// SetCommands enables start_temporary_machine, run_command, run_command_write
// and get_command_run.
func (t *AssistantTools) SetCommands(commands assistantCommandStore, machines assistantTemporaryMachines) {
	t.commands, t.temporaryMachines = commands, machines
	if t.commandWait == 0 {
		t.commandWait = assistantCommandWait
	}
}

// SetCommands enables approving start_temporary_machine, run_command and
// run_command_write.
func (s *AssistantApprovals) SetCommands(commands assistantCommandStore, machines assistantTemporaryMachines) {
	s.commands, s.temporaryMachines = commands, machines
}

// assistantCommandToolCatalog joins the registry in assistantTools(), so
// Settings → Assistant lists these tools and their switches apply.
var assistantCommandToolCatalog = []assistantTool{
	{name: "start_temporary_machine", commands: true, approval: true, description: "Get a machine to run commands on, only when no other tool can answer. If the user has a running machine of their own it is reused at once, in a folder of this conversation, without approval. Otherwise ask the user to approve starting a small temporary machine for this conversation (1 vCPU, 2 GB memory, 20 GB disk, no repository and no credentials, internet only through the workspace's proxy), which is permanently deleted 30 minutes after it starts. Say which machine is used, its size and whether it was reused.", parameters: `{"type":"object","properties":{},"additionalProperties":false}`},
	{name: "run_command", commands: true, approval: true, description: "Ask the user to approve running one short shell command (sh -c, at most 60 seconds, output capped) read-only on a machine: /workspace is mounted read-only and only /tmp and $HOME are writable, so it can compute, check a snippet (python3 -c, node -e), inspect files or call a public URL, but changes nothing. Without machine_id it uses this conversation's temporary machine or the user's own running machine. working_directory is relative to /workspace (default /workspace). It runs in a container without credentials. Use it only when no other tool can answer; the command runs only after approval, so never claim a result before get_command_run returns it.", parameters: `{"type":"object","properties":{"command":{"type":"string","minLength":1,"maxLength":4000},"machine_id":{"type":"string","maxLength":36},"working_directory":{"type":"string","maxLength":500}},"required":["command"],"additionalProperties":false}`},
	{name: "run_command_write", commands: true, approval: true, description: "Ask the user to approve running one short shell command (sh -c, at most 60 seconds, output capped) that may write files: it starts in this conversation's folder /workspace/kuayle-assistant/<conversation> and /workspace is writable. Use run_command instead unless the command must create or change files, and only when the user asked for it. It runs in a container without credentials, only after approval.", parameters: `{"type":"object","properties":{"command":{"type":"string","minLength":1,"maxLength":4000},"machine_id":{"type":"string","maxLength":36}},"required":["command"],"additionalProperties":false}`},
	{name: "get_command_run", commands: true, description: "Read a command run by run_id, or the run an approval started by approval_id: status, exit code, the end of its output, and the machine it ran on with its size, whether it was reused and when a temporary machine is deleted. It waits up to about a minute for a running command. Use it after the user approved run_command or run_command_write.", parameters: `{"type":"object","properties":{"run_id":{"type":"string","maxLength":36},"approval_id":{"type":"string","maxLength":36}},"additionalProperties":false}`},
}

// commandPrompt tells the model when to use the command tools, or "" when
// run_command is not offered.
func (t *AssistantTools) commandPrompt(ctx context.Context, ws uuid.UUID) string {
	if t == nil || t.commands == nil || t.temporaryMachines == nil {
		return ""
	}
	disabled, err := t.disabledTools(ctx, ws)
	if err != nil || disabled["run_command"] {
		return ""
	}
	prompt := " You can run a short command on a Dev Machine with run_command (read-only) or run_command_write (writes files), each after the user's approval, and read its result with get_command_run. Use them only when no other tool can answer: prefer repository scans, worktree reads, get_agent_work, run_check and custom tools. Prefer the user's own running machine; propose start_temporary_machine only when they have none."
	if disabled["start_temporary_machine"] {
		prompt = strings.Replace(prompt, " Prefer the user's own running machine; propose start_temporary_machine only when they have none.", " Commands run only on the user's own running machines.", 1)
	}
	return prompt + " Always report the machine, its size and whether it was reused; never invent a command's output."
}

// commandUser checks that the user may run commands, with the next step when
// they may not.
func (t *AssistantTools) commandUser(ctx context.Context, ws, user uuid.UUID) error {
	member, err := t.members.GetMember(ctx, ws, user)
	if err != nil {
		return err
	}
	if member == nil {
		return toolFailure("%s", noWorkspaceAccess)
	}
	if member.Role == domain.RoleGuest {
		return toolFailure("Guests cannot run commands; ask a workspace member to run it, or ask an admin to change your role.")
	}
	if !domain.HasPermission(member.Role, domain.PermDevMachineManage) {
		return toolFailure("Your role cannot use Dev Machines; ask a workspace member to run it, or ask an admin to change your role.")
	}
	return nil
}

// commandMachine is the machine a command runs on: the given one, which the
// user must own, else the conversation's temporary machine, else the user's
// own running machine (reused).
func (t *AssistantTools) commandMachine(ctx context.Context, ws, user, conversation uuid.UUID, machineID string) (*domain.DevMachine, bool, error) {
	if text := strings.TrimSpace(machineID); text != "" {
		id, err := uuid.Parse(text)
		if err != nil {
			return nil, false, toolFailure("machine_id is not a machine id")
		}
		machine, err := t.commands.UsableMachine(ctx, ws, user, id)
		if errors.Is(err, repository.ErrCommandMachineUnavailable) {
			return nil, false, toolFailure("%s", upperFirst(notMachineOwner))
		}
		if err != nil {
			return nil, false, err
		}
		return machine, machine.TemporaryExpiresAt == nil, nil
	}
	machine, err := t.commands.ConversationMachine(ctx, ws, user, conversation)
	if err == nil {
		if machine.Status != domain.DevMachineStatusRunning {
			return nil, false, toolFailure("The temporary machine %s is still starting; ask again in a minute.", machine.Name)
		}
		return machine, false, nil
	}
	if !errors.Is(err, repository.ErrCommandMachineUnavailable) {
		return nil, false, err
	}
	machine, err = t.commands.ReusableMachine(ctx, ws, user)
	if errors.Is(err, repository.ErrCommandMachineUnavailable) {
		return nil, false, toolFailure("%s", noCommandMachine)
	}
	if err != nil {
		return nil, false, err
	}
	return machine, true, nil
}

// prepareCommand binds run_command or run_command_write to the exact command,
// the machine and the working directory shown on the approval card.
func (t *AssistantTools) prepareCommand(ctx context.Context, a domain.ConversationAttempt, action, arguments string) (domain.CommandRunPayload, error) {
	ws, user := a.Conversation.WorkspaceID, a.Conversation.CreatorID
	var input struct {
		Command          string `json:"command"`
		MachineID        string `json:"machine_id"`
		WorkingDirectory string `json:"working_directory"`
	}
	if err := decodeToolInput(arguments, &input); err != nil {
		return domain.CommandRunPayload{}, err
	}
	if err := t.commandUser(ctx, ws, user); err != nil {
		return domain.CommandRunPayload{}, err
	}
	command := strings.TrimSpace(input.Command)
	if command == "" || len(command) > domain.CommandMaxBytes || !utf8.ValidString(command) || strings.ContainsRune(command, 0) {
		return domain.CommandRunPayload{}, toolFailure("command must be 1 to %d bytes of text", domain.CommandMaxBytes)
	}
	write := action == "run_command_write"
	directory := domain.CommandFolder(a.Conversation.ID)
	if !write {
		var ok bool
		if directory, ok = domain.CommandWorkingDirectory(input.WorkingDirectory); !ok {
			return domain.CommandRunPayload{}, toolFailure("working_directory must be a folder inside /workspace")
		}
	}
	machine, reused, err := t.commandMachine(ctx, ws, user, a.Conversation.ID, input.MachineID)
	if err != nil {
		return domain.CommandRunPayload{}, err
	}
	return domain.CommandRunPayload{CommandMachine: domain.CommandMachineOf(machine, reused), Command: command, Write: write, WorkingDirectory: directory,
		TimeoutSeconds: int(domain.CommandRunTimeout / time.Second)}, nil
}

// prepareTemporaryMachine answers start_temporary_machine: an existing machine
// is returned at once (reused, or this conversation's temporary one) and needs
// no approval; otherwise the request is checked against the quota and becomes
// an approval card.
func (t *AssistantTools) prepareTemporaryMachine(ctx context.Context, a domain.ConversationAttempt, arguments string) (*domain.TemporaryMachinePayload, map[string]any, error) {
	ws, user := a.Conversation.WorkspaceID, a.Conversation.CreatorID
	if err := decodeToolInput(arguments, &struct{}{}); err != nil {
		return nil, nil, err
	}
	if err := t.commandUser(ctx, ws, user); err != nil {
		return nil, nil, err
	}
	folder := domain.CommandFolder(a.Conversation.ID)
	existing := func(machine *domain.DevMachine, reused bool, note string) map[string]any {
		result := commandMachineResult(domain.CommandMachineOf(machine, reused))
		result["status"], result["working_directory"], result["note"] = machine.Status, folder, note
		return result
	}
	if machine, err := t.commands.ConversationMachine(ctx, ws, user, a.Conversation.ID); err == nil {
		return nil, existing(machine, false, "This conversation already has a temporary machine; no other was started. Commands still need approval."), nil
	} else if !errors.Is(err, repository.ErrCommandMachineUnavailable) {
		return nil, nil, err
	}
	if machine, err := t.commands.ReusableMachine(ctx, ws, user); err == nil {
		return nil, existing(machine, true, "No machine was started: commands run on your own machine in this conversation's folder. Each command still needs approval."), nil
	} else if !errors.Is(err, repository.ErrCommandMachineUnavailable) {
		return nil, nil, err
	}
	used, limit, err := t.commands.TemporaryMachineQuota(ctx, ws)
	if err != nil {
		return nil, nil, err
	}
	if limit == 0 {
		return nil, nil, toolFailure("Temporary machines are turned off in this workspace (its Dev Machine policy allows none); give machine_id of one of your running machines, or ask an admin to allow them.")
	}
	if used >= limit {
		return nil, nil, toolFailure("The workspace already has %d of %d allowed temporary machines; try again when one expires, or give machine_id of one of your running machines.", used, limit)
	}
	preset := domain.TemporaryMachineResources
	return &domain.TemporaryMachinePayload{CPUMillis: preset.CPUMillis, MemoryMB: preset.MemoryMB, DiskGB: preset.DiskGB, Profile: domain.DevMachineProfileHeadless,
		TTLMinutes: int(domain.TemporaryMachineTTL / time.Minute), QuotaUsed: used, QuotaLimit: limit, WorkingDirectory: folder}, nil, nil
}

// commandMachineResult names a machine, its size and whether it was reused.
func commandMachineResult(machine domain.CommandMachine) map[string]any {
	result := map[string]any{"machine": machine.Machine, "machine_id": machine.MachineID, "cpu_millis": machine.CPUMillis, "memory_mb": machine.MemoryMB,
		"disk_gb": machine.DiskGB, "reused": machine.Reused, "temporary": machine.Temporary}
	if machine.ExpiresAt != nil {
		result["expires_at"] = machine.ExpiresAt
	}
	return result
}

// commandRunResult is a run as shown to the model and in the chat.
func commandRunResult(run *domain.CommandRun) map[string]any {
	result := commandMachineResult(domain.CommandMachine{Machine: run.MachineName, CPUMillis: run.CPUMillis, MemoryMB: run.MemoryMB, DiskGB: run.DiskGB,
		Reused: run.Reused, Temporary: run.Temporary, ExpiresAt: run.ExpiresAt})
	if run.MachineID != nil {
		result["machine_id"] = *run.MachineID
	} else {
		result["machine_id"] = nil
	}
	output, truncated := run.Output, run.OutputTruncated
	if len(output) > assistantCommandOutputBytes {
		output, truncated = output[len(output)-assistantCommandOutputBytes:], true
		for len(output) > 0 && !utf8.RuneStart(output[0]) {
			output = output[1:]
		}
	}
	result["run_id"], result["status"], result["command"], result["write"], result["working_directory"] = run.ID, run.Status, run.Command, run.Write, run.WorkingDirectory
	result["exit_code"], result["output_tail"], result["output_truncated"], result["duration_ms"] = run.ExitCode, output, truncated, run.DurationMS
	if run.Error != nil {
		result["error"] = *run.Error
	}
	switch {
	case run.Status == "queued" || run.Status == "running":
		result["note"] = "Still running; read the result later with get_command_run."
	case run.Temporary && run.ExpiresAt != nil:
		result["note"] = "The temporary machine is permanently deleted at expires_at."
	}
	return result
}

// awaitCommandRun polls a run until it finishes, the wait ends or the reply
// must answer; it returns the newest state it read.
func (t *AssistantTools) awaitCommandRun(ctx context.Context, ws, user uuid.UUID, run *domain.CommandRun) *domain.CommandRun {
	wait := time.NewTimer(t.commandWait)
	defer wait.Stop()
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for run.Status == "queued" || run.Status == "running" {
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < assistantFinalAnswerReserve {
			return run
		}
		select {
		case <-ctx.Done():
			return run
		case <-wait.C:
			return run
		case <-tick.C:
		}
		current, err := t.commands.Run(ctx, ws, run.ID, user)
		if err != nil {
			return run
		}
		run = current
	}
	return run
}

// getCommandRun runs get_command_run.
func (t *AssistantTools) getCommandRun(ctx context.Context, ws, user uuid.UUID, arguments string) (any, *uuid.UUID, error) {
	var input struct {
		RunID      string `json:"run_id"`
		ApprovalID string `json:"approval_id"`
	}
	if err := decodeToolInput(arguments, &input); err != nil {
		return nil, nil, err
	}
	if err := t.commandUser(ctx, ws, user); err != nil {
		return nil, nil, err
	}
	var run *domain.CommandRun
	var err error
	if id, parseErr := uuid.Parse(strings.TrimSpace(input.RunID)); parseErr == nil {
		run, err = t.commands.Run(ctx, ws, id, user)
	} else if id, parseErr := uuid.Parse(strings.TrimSpace(input.ApprovalID)); parseErr == nil {
		run, err = t.commands.RunForApproval(ctx, ws, id, user)
	} else {
		return nil, nil, toolFailure("give run_id or approval_id")
	}
	if errors.Is(err, repository.ErrCommandRunNotFound) {
		return nil, nil, toolFailure("no run was found; an approval starts its command only once approved")
	}
	if err != nil {
		return nil, nil, err
	}
	return commandRunResult(t.awaitCommandRun(ctx, ws, user, run)), nil, nil
}

// commandApprovalFailure is the user-safe reason an approved command or
// temporary machine did not start, or "" for a transient error.
func commandApprovalFailure(err error) string {
	switch {
	case errors.Is(err, repository.ErrCommandMachineUnavailable):
		return upperFirst(notMachineOwner)
	case errors.Is(err, repository.ErrMachineBusy):
		return "The machine is busy with an agent, a check or another command; try again when it is free"
	case errors.Is(err, repository.ErrConversationNotFound), errors.Is(err, repository.ErrCommandRunForbidden):
		return "Guests cannot run commands; ask a workspace member to run it, or ask an admin to change your role"
	case errors.Is(err, repository.ErrConversationInvalid):
		return "This approval cannot be executed"
	case errors.Is(err, ErrTemporaryMachinesOff):
		return "Temporary machines are turned off in this workspace; ask an admin to allow them in its Dev Machine policy"
	case errors.Is(err, ErrDevMachinesDisabled):
		return "Dev Machines are not enabled for this workspace"
	case errors.Is(err, ErrMachineQuota):
		return upperFirst(err.Error())
	case errors.Is(err, ErrInvalidMachineInput), errors.Is(err, ErrMachineNameConflict):
		return "The temporary machine could not be created; ask again"
	}
	return ""
}

// runCommand queues the approved command, as the approver; the repository
// re-checks that they own the running machine and may use it, and an approval
// queues at most one run. The card shows the result.
func (s *AssistantApprovals) runCommand(ctx context.Context, ws, conversation uuid.UUID, approval *domain.ConversationApproval) (*domain.ConversationApproval, error) {
	var payload domain.CommandRunPayload
	if s.commands == nil || json.Unmarshal(approval.Payload, &payload) != nil || payload.Write != (approval.Action == "run_command_write") {
		return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, nil, "This approval cannot be executed")
	}
	_, err := s.commands.QueueRun(ctx, ws, approval.ActorID, repository.CommandRunRequest{MachineID: payload.MachineID, Command: payload.Command, Write: payload.Write,
		WorkingDirectory: payload.WorkingDirectory, Reused: payload.Reused, ConversationID: &conversation, ToolCallID: &approval.ToolCallID, ApprovalID: &approval.ID})
	if err != nil {
		if message := commandApprovalFailure(err); message != "" {
			return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, nil, message)
		}
		// A transient failure leaves the claim in place; approving again resumes it.
		return nil, err
	}
	return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, nil, "")
}

// startTemporaryMachine starts the approved temporary machine as the approver,
// through the Dev Machine service's policy, quota and demo checks. Approving
// again converges on the machine already started for the conversation.
func (s *AssistantApprovals) startTemporaryMachine(ctx context.Context, ws, conversation uuid.UUID, approval *domain.ConversationApproval) (*domain.ConversationApproval, error) {
	if s.commands == nil || s.temporaryMachines == nil {
		return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, nil, "This approval cannot be executed")
	}
	if _, err := s.commands.ConversationMachine(ctx, ws, approval.ActorID, conversation); err == nil {
		return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, nil, "")
	} else if !errors.Is(err, repository.ErrCommandMachineUnavailable) {
		return nil, err
	}
	if _, err := s.temporaryMachines.StartTemporaryMachine(ctx, ws, approval.ActorID, conversation); err != nil {
		if message := commandApprovalFailure(err); message != "" {
			return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, nil, message)
		}
		return nil, err
	}
	return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, nil, "")
}

// CommandRun returns the command run an approval of the conversation started,
// to its requester, for the approval card. A run not yet queued, or another
// user's, is not found.
func (s *ConversationService) CommandRun(ctx context.Context, ws, conversation, approval uuid.UUID) (map[string]any, error) {
	access, ok := domain.AccessFromContext(ctx)
	if !ok || s.tools == nil || s.tools.commands == nil {
		return nil, repository.ErrConversationNotFound
	}
	run, err := s.tools.commands.RunForApproval(ctx, ws, approval, access.UserID)
	if errors.Is(err, repository.ErrCommandRunNotFound) || (err == nil && (run.ConversationID == nil || *run.ConversationID != conversation)) {
		return nil, repository.ErrConversationNotFound
	}
	if err != nil {
		return nil, err
	}
	return commandRunResult(run), nil
}
