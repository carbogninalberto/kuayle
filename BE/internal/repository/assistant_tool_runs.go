package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// A custom tool run executes one tool version on a dev machine in the same
// credential-free check container as manual checks, under the same machine
// lock: never beside an agent attempt, an independent check or another run.

// toolRunKeyPrefix starts a run_tool operation's idempotency key; the rest is
// the ID of the run the operation executes.
const toolRunKeyPrefix = "tool-run:"

var (
	// ErrToolUnavailable means the tool is no longer active and enabled at the
	// version that was validated (a test run needs only the version).
	ErrToolUnavailable = errors.New("the tool changed or was turned off; ask again")
	// ErrToolMachineUnavailable means there is no running machine the
	// requester owns to run on: the task's machine with its checkout, or one
	// of their machines.
	ErrToolMachineUnavailable = errors.New("the tool needs a running machine of yours: the task's machine with its checkout, or one of your machines")
	// ErrToolRunForbidden means the requester's role cannot run tools.
	ErrToolRunForbidden = errors.New("your role cannot run custom tools")
	// ErrToolTaskChanged means the task started another attempt since the
	// request was shown.
	ErrToolTaskChanged = errors.New("the task started a new attempt after the run was requested; ask again")
)

// ToolRunRequest is a validated request to run a tool version. Exactly one of
// TaskID (the task's machine and checkout) and MachineID (a machine the
// requester owns, at /workspace) is set.
type ToolRunRequest struct {
	ToolID     uuid.UUID
	Version    int
	Inputs     json.RawMessage
	TaskID     *uuid.UUID
	Generation *int64
	MachineID  *uuid.UUID
	// IssueID records the issue a chat run was asked about.
	IssueID        *uuid.UUID
	ConversationID *uuid.UUID
	ToolCallID     *uuid.UUID
	ApprovalID     *uuid.UUID
	// Test is an owner's or admin's test run from Settings; it may run any
	// saved version, including a draft or a disabled tool.
	Test bool
}

const toolRunColumns = `r.id,r.workspace_id,r.tool_id,r.tool_version,r.tool_name,r.inputs,r.requested_by_user_id,r.conversation_id,r.tool_call_id,r.approval_id,
 r.machine_id,r.issue_id,r.task_id,r.agent_run_id,r.working_directory,r.timeout_seconds,r.test,r.status,r.exit_code,r.output,r.output_truncated,r.error,
 r.created_at,r.started_at,r.finished_at,(EXTRACT(EPOCH FROM (r.finished_at-r.started_at))*1000)::bigint AS duration_ms`

// lockToolRunner holds the requester's membership until commit and checks
// that their role may run tools (test runs: owners and admins only).
func lockToolRunner(ctx context.Context, tx *sqlx.Tx, ws, actor uuid.UUID, test bool) error {
	var role string
	err := tx.GetContext(ctx, &role, `SELECT role FROM workspace_members WHERE workspace_id=$1 AND user_id=$2 FOR SHARE`, ws, actor)
	if errors.Is(err, sql.ErrNoRows) || role == domain.RoleGuest {
		return ErrConversationNotFound
	}
	if err != nil {
		return err
	}
	if test && role != domain.RoleOwner && role != domain.RoleAdmin {
		return ErrAssistantToolDenied
	}
	if !domain.HasPermission(role, domain.PermDevMachineManage) {
		return ErrToolRunForbidden
	}
	return nil
}

// QueueRun queues a run of a tool version for the actor. A chat run needs the
// tool active and enabled at exactly that version; the machine must be the
// task's (created by the actor, idle, with a ready checkout) or one the actor
// owns, running and free. An approval queues at most one run.
func (r *AssistantCustomToolRepository) QueueRun(ctx context.Context, ws, actor uuid.UUID, req ToolRunRequest) (*domain.AssistantToolRun, error) {
	if (req.TaskID == nil) == (req.MachineID == nil) || req.Version < 1 {
		return nil, ErrConversationInvalid
	}
	var tx *sqlx.Tx
	var task *domain.AgentTask
	var err error
	if req.TaskID != nil {
		// The task transaction authorizes its creator and locks its chat.
		if tx, task, err = NewAgentTaskRepository(r.db).transaction(ctx, *req.TaskID, true); err != nil {
			return nil, err
		}
		if task.WorkspaceID != ws {
			tx.Rollback()
			return nil, ErrConversationNotFound
		}
	} else if tx, err = r.db.BeginTxx(ctx, nil); err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if req.ApprovalID != nil {
		existing := &domain.AssistantToolRun{}
		err = tx.GetContext(ctx, existing, `SELECT `+toolRunColumns+` FROM assistant_tool_runs r WHERE r.approval_id=$1 AND r.workspace_id=$2`, req.ApprovalID, ws)
		if err == nil {
			return existing, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	if err = lockToolRunner(ctx, tx, ws, actor, req.Test); err != nil {
		return nil, err
	}
	var tool struct {
		Name           string `db:"name"`
		Status         string `db:"status"`
		Enabled        bool   `db:"enabled"`
		CurrentVersion int    `db:"current_version"`
		TimeoutSeconds int    `db:"timeout_seconds"`
	}
	err = tx.GetContext(ctx, &tool, `SELECT v.name,t.status,t.enabled,t.current_version,v.timeout_seconds FROM assistant_custom_tools t
 JOIN assistant_custom_tool_versions v ON v.tool_id=t.id AND v.version=$3 WHERE t.workspace_id=$1 AND t.id=$2 FOR SHARE OF t`, ws, req.ToolID, req.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAssistantToolNotFound
	}
	if err != nil {
		return nil, err
	}
	if !req.Test && (tool.Status != domain.AssistantSkillActive || !tool.Enabled || tool.CurrentVersion != req.Version) {
		return nil, ErrToolUnavailable
	}
	run := &domain.AssistantToolRun{WorkspaceID: ws, ToolID: req.ToolID, ToolVersion: req.Version, ToolName: tool.Name, Inputs: req.Inputs, RequestedByUserID: &actor,
		ConversationID: req.ConversationID, ToolCallID: req.ToolCallID, ApprovalID: req.ApprovalID, IssueID: req.IssueID, TimeoutSeconds: tool.TimeoutSeconds, Test: req.Test, WorkingDirectory: "/workspace"}
	var machine uuid.UUID
	if task != nil {
		if req.Generation != nil && task.Generation != *req.Generation {
			return nil, ErrToolTaskChanged
		}
		if slices.Contains(activeTaskStates, task.State) {
			return nil, ErrMachineBusy
		}
		if !slices.Contains(manualCheckStates, task.State) || task.Kind == domain.AgentTaskKindReview || task.MachineID == nil {
			return nil, ErrToolMachineUnavailable
		}
		var path string
		err = tx.GetContext(ctx, &path, `SELECT c.workspace_path FROM dev_machine_checkouts c JOIN dev_machine_agent_runs a ON a.id=$4 AND a.task_id=$5 AND a.machine_id=c.machine_id AND a.workspace_id=c.workspace_id
 WHERE c.id=$1 AND c.machine_id=$2 AND c.workspace_id=$3 AND c.status='ready'`, task.CheckoutID, task.MachineID, ws, task.RunID, task.ID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrToolMachineUnavailable
		}
		if err != nil {
			return nil, err
		}
		machine, run.WorkingDirectory, run.TaskID, run.AgentRunID, run.IssueID = *task.MachineID, path, &task.ID, &task.RunID, &task.IssueID
	} else {
		var owned bool
		if err = tx.GetContext(ctx, &owned, `SELECT EXISTS(SELECT 1 FROM dev_machines WHERE id=$1 AND workspace_id=$2 AND created_by_user_id=$3 AND delete_requested_at IS NULL)`, req.MachineID, ws, actor); err != nil {
			return nil, err
		}
		if !owned {
			return nil, ErrToolMachineUnavailable
		}
		machine = *req.MachineID
	}
	generation, err := lockMachineForCheck(ctx, tx, ws, machine)
	if errors.Is(err, ErrCheckUnavailable) {
		return nil, ErrToolMachineUnavailable
	}
	if err != nil {
		return nil, err
	}
	run.MachineID = &machine
	if err = tx.GetContext(ctx, run, `INSERT INTO assistant_tool_runs(workspace_id,tool_id,tool_version,tool_name,inputs,requested_by_user_id,conversation_id,tool_call_id,approval_id,machine_id,issue_id,task_id,agent_run_id,working_directory,timeout_seconds,test,status)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,'queued') RETURNING id,workspace_id,tool_id,tool_version,tool_name,inputs,requested_by_user_id,conversation_id,tool_call_id,approval_id,
 machine_id,issue_id,task_id,agent_run_id,working_directory,timeout_seconds,test,status,exit_code,output,output_truncated,error,created_at,started_at,finished_at`,
		ws, run.ToolID, run.ToolVersion, run.ToolName, run.Inputs, actor, run.ConversationID, run.ToolCallID, run.ApprovalID, machine, run.IssueID, run.TaskID, run.AgentRunID, run.WorkingDirectory, run.TimeoutSeconds, run.Test); err != nil {
		return nil, err
	}
	op := &domain.DevMachineOperation{WorkspaceID: ws, MachineID: machine, Action: domain.DevMachineOpRunTool, Status: domain.DevMachineOpStatusPending, Generation: generation,
		IdempotencyKey: toolRunKeyPrefix + run.ID.String(), RequestedByUserID: &actor, MaxAttempts: 3}
	if err = enqueueOperation(ctx, tx, op); err != nil {
		return nil, err
	}
	if err = toolRunEvent(ctx, tx, run); err != nil {
		return nil, err
	}
	return run, tx.Commit()
}

// toolRunEvent tells a run's conversation that the run changed, with the end
// of its output.
func toolRunEvent(ctx context.Context, tx *sqlx.Tx, run *domain.AssistantToolRun) error {
	if run.ConversationID == nil {
		return nil
	}
	summary := *run
	summary.Output = outputTail(run.Output, 4096)
	_, err := conversationEvent(ctx, tx, *run.ConversationID, "tool_run.updated", summary)
	return err
}

// outputTail keeps the last limit bytes of output on a UTF-8 boundary.
func outputTail(output string, limit int) string {
	if len(output) <= limit {
		return output
	}
	tail := output[len(output)-limit:]
	for len(tail) > 0 && !isRuneStart(tail[0]) {
		tail = tail[1:]
	}
	return tail
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }

// Run returns one run to its requester or a current owner or admin.
func (r *AssistantCustomToolRepository) Run(ctx context.Context, ws, id, actor uuid.UUID) (*domain.AssistantToolRun, error) {
	run := &domain.AssistantToolRun{}
	err := r.db.GetContext(ctx, run, `SELECT `+toolRunColumns+` FROM assistant_tool_runs r WHERE r.workspace_id=$1 AND r.id=$2 AND `+toolRunReadable("r", "$3"), ws, id, actor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAssistantToolNotFound
	}
	if err != nil {
		return nil, err
	}
	return run, nil
}

// RunForApproval returns the run an approval queued, to its requester.
func (r *AssistantCustomToolRepository) RunForApproval(ctx context.Context, ws, approval, actor uuid.UUID) (*domain.AssistantToolRun, error) {
	run := &domain.AssistantToolRun{}
	err := r.db.GetContext(ctx, run, `SELECT `+toolRunColumns+` FROM assistant_tool_runs r WHERE r.workspace_id=$1 AND r.approval_id=$2 AND `+toolRunReadable("r", "$3"), ws, approval, actor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAssistantToolNotFound
	}
	if err != nil {
		return nil, err
	}
	return run, nil
}

// Runs lists a tool's runs the actor may read, newest first.
func (r *AssistantCustomToolRepository) Runs(ctx context.Context, ws, tool, actor uuid.UUID, limit int) ([]domain.AssistantToolRun, error) {
	if _, err := r.Get(ctx, ws, tool); err != nil {
		return nil, err
	}
	runs := []domain.AssistantToolRun{}
	err := r.db.SelectContext(ctx, &runs, `SELECT `+toolRunColumns+` FROM assistant_tool_runs r WHERE r.workspace_id=$1 AND r.tool_id=$2 AND `+toolRunReadable("r", "$3")+`
 ORDER BY r.created_at DESC LIMIT $4`, ws, tool, actor, limit)
	return runs, err
}

// toolRunReadable lets a current non-guest member read the runs they asked
// for, and owners and admins read every run: output comes from the
// requester's machine.
func toolRunReadable(alias, actor string) string {
	return `EXISTS(SELECT 1 FROM workspace_members m WHERE m.workspace_id=` + alias + `.workspace_id AND m.user_id=` + actor + `
 AND (m.role IN ('owner','admin') OR (m.role<>'guest' AND ` + alias + `.requested_by_user_id=` + actor + `)))`
}

func toolRunID(op domain.DevMachineOperation) (uuid.UUID, bool) {
	raw, ok := strings.CutPrefix(op.IdempotencyKey, toolRunKeyPrefix)
	if !ok || op.Action != domain.DevMachineOpRunTool {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(raw)
	return id, err == nil
}

// toolRunTransaction is a tool run's write authority: the manager still holds
// the operation's lease on the machine generation it was queued for, the
// requester may still run tools on that machine, and a chat run's tool is
// still active and enabled at the run's version.
func (r *DevMachineRepository) toolRunTransaction(ctx context.Context, op domain.DevMachineOperation) (*sqlx.Tx, *domain.AssistantToolRun, error) {
	id, ok := toolRunID(op)
	if !ok || op.LeaseOwner == nil || op.RequestedByUserID == nil {
		return nil, nil, ErrAgentTaskUnauthorized
	}
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	fail := func(e error) (*sqlx.Tx, *domain.AssistantToolRun, error) {
		tx.Rollback()
		return nil, nil, e
	}
	run := &domain.AssistantToolRun{}
	err = tx.GetContext(ctx, run, `SELECT `+toolRunColumns+` FROM assistant_tool_runs r WHERE r.id=$1 AND r.workspace_id=$2 AND r.machine_id=$3 AND r.requested_by_user_id=$4 FOR UPDATE`, id, op.WorkspaceID, op.MachineID, op.RequestedByUserID)
	if errors.Is(err, sql.ErrNoRows) {
		return fail(ErrToolUnavailable)
	}
	if err != nil {
		return fail(err)
	}
	var current domain.DevMachineOperation
	err = tx.GetContext(ctx, &current, `SELECT * FROM dev_machine_operations WHERE id=$1 AND action='run_tool' AND workspace_id=$2 AND machine_id=$3 AND generation=$4 AND status='leased' AND lease_owner=$5 AND attempts=$6 AND lease_expires_at>clock_timestamp() FOR SHARE`,
		op.ID, op.WorkspaceID, op.MachineID, op.Generation, *op.LeaseOwner, op.Attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return fail(ErrAgentTaskConflict)
	}
	if err != nil {
		return fail(err)
	}
	var running bool
	if err = tx.GetContext(ctx, &running, `SELECT EXISTS(SELECT 1 FROM dev_machines m JOIN dev_machine_workspace_policies p ON p.workspace_id=m.workspace_id AND p.enabled
 WHERE m.id=$1 AND m.workspace_id=$2 AND m.generation=$3 AND m.status='running' AND m.desired_status='running')`, op.MachineID, op.WorkspaceID, op.Generation); err != nil {
		return fail(err)
	}
	if !running {
		return fail(ErrToolMachineUnavailable)
	}
	if err = lockToolRunner(ctx, tx, op.WorkspaceID, *op.RequestedByUserID, run.Test); err != nil {
		return fail(ErrToolRunForbidden)
	}
	if !run.Test {
		var active bool
		if err = tx.GetContext(ctx, &active, `SELECT EXISTS(SELECT 1 FROM assistant_custom_tools WHERE id=$1 AND workspace_id=$2 AND status='active' AND enabled AND current_version=$3)`, run.ToolID, run.WorkspaceID, run.ToolVersion); err != nil {
			return fail(err)
		}
		if !active {
			return fail(ErrToolUnavailable)
		}
	}
	if run.TaskID != nil {
		// The task must still be idle on the same attempt, machine and creator.
		var idle bool
		if err = tx.GetContext(ctx, &idle, `SELECT EXISTS(SELECT 1 FROM agent_tasks WHERE id=$1 AND workspace_id=$2 AND creator_id=$3 AND machine_id=$4 AND run_id=$5 AND state = ANY($6))`,
			run.TaskID, run.WorkspaceID, op.RequestedByUserID, op.MachineID, run.AgentRunID, manualCheckStates); err != nil {
			return fail(err)
		}
		if !idle {
			return fail(ErrToolMachineUnavailable)
		}
	} else {
		var owned bool
		if err = tx.GetContext(ctx, &owned, `SELECT EXISTS(SELECT 1 FROM dev_machines WHERE id=$1 AND created_by_user_id=$2)`, op.MachineID, op.RequestedByUserID); err != nil {
			return fail(err)
		}
		if !owned {
			return fail(ErrToolMachineUnavailable)
		}
	}
	return tx, run, nil
}

// GetToolRun returns what the manager needs to execute a run, under the run's
// authority.
func (r *DevMachineRepository) GetToolRun(ctx context.Context, op domain.DevMachineOperation) (*domain.AssistantToolRunExecution, error) {
	tx, run, err := r.toolRunTransaction(ctx, op)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	execution := &domain.AssistantToolRunExecution{Run: *run}
	if err = tx.QueryRowContext(ctx, `SELECT script,interpreter FROM assistant_custom_tool_versions WHERE tool_id=$1 AND version=$2`, run.ToolID, run.ToolVersion).Scan(&execution.Script, &execution.Interpreter); err != nil {
		return nil, err
	}
	if run.AgentRunID != nil {
		if err = tx.GetContext(ctx, &execution.EncryptedOutputSecrets, `SELECT encrypted_output_secrets FROM dev_machine_agent_runs WHERE id=$1 AND machine_id=$2`, run.AgentRunID, op.MachineID); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	return execution, tx.Commit()
}

// StartToolRun marks a run running. Output is replayed from the beginning
// whenever execution resumes, so the stored output restarts too.
func (r *DevMachineRepository) StartToolRun(ctx context.Context, op domain.DevMachineOperation, _ string) error {
	tx, run, err := r.toolRunTransaction(ctx, op)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if run.Status != "queued" && run.Status != "running" {
		return ErrAgentTaskConflict
	}
	if err = tx.GetContext(ctx, run, `UPDATE assistant_tool_runs r SET status='running',started_at=COALESCE(started_at,now()),output='',output_truncated=false WHERE id=$1 RETURNING `+toolRunColumns, run.ID); err != nil {
		return err
	}
	if err = toolRunEvent(ctx, tx, run); err != nil {
		return err
	}
	return tx.Commit()
}

// AppendToolRunOutput adds redacted output, keeping only the stored tail.
func (r *DevMachineRepository) AppendToolRunOutput(ctx context.Context, op domain.DevMachineOperation, content string, truncated bool) error {
	tx, run, err := r.toolRunTransaction(ctx, op)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if run.Status != "running" {
		return ErrAgentTaskConflict
	}
	if _, err = tx.ExecContext(ctx, `UPDATE assistant_tool_runs SET output=right(output||$2,$4),output_truncated=output_truncated OR $3 OR char_length(output||$2)>$4 WHERE id=$1`,
		run.ID, content, truncated, domain.CustomToolOutputChars); err != nil {
		return err
	}
	return tx.Commit()
}

// CompleteToolRun records the script's exit status.
func (r *DevMachineRepository) CompleteToolRun(ctx context.Context, op domain.DevMachineOperation, exit int) error {
	tx, run, err := r.toolRunTransaction(ctx, op)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if run.Status != "running" {
		return ErrAgentTaskConflict
	}
	status := "failed"
	if exit == 0 {
		status = "succeeded"
	}
	if err = tx.GetContext(ctx, run, `UPDATE assistant_tool_runs r SET status=$2,exit_code=$3,finished_at=now() WHERE id=$1 RETURNING `+toolRunColumns, run.ID, status, exit); err != nil {
		return err
	}
	if err = toolRunEvent(ctx, tx, run); err != nil {
		return err
	}
	return tx.Commit()
}

// FailToolRun records a run whose operation failed for good, so it never
// stays queued or running. It needs only the failed operation, not the lease.
func (r *DevMachineRepository) FailToolRun(ctx context.Context, op domain.DevMachineOperation, summary string) error {
	id, ok := toolRunID(op)
	if !ok {
		return ErrAgentTaskUnauthorized
	}
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var failed bool
	if err = tx.GetContext(ctx, &failed, `SELECT EXISTS(SELECT 1 FROM dev_machine_operations WHERE id=$1 AND machine_id=$2 AND action='run_tool' AND idempotency_key=$3 AND status='failed')`, op.ID, op.MachineID, op.IdempotencyKey); err != nil {
		return err
	}
	if !failed {
		return ErrAgentTaskConflict
	}
	run := &domain.AssistantToolRun{}
	err = tx.GetContext(ctx, run, `UPDATE assistant_tool_runs r SET status='failed',error=left($3,2048),finished_at=now() WHERE id=$1 AND machine_id=$2 AND status IN ('queued','running') RETURNING `+toolRunColumns, id, op.MachineID, summary)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if err = toolRunEvent(ctx, tx, run); err != nil {
		return err
	}
	return tx.Commit()
}
