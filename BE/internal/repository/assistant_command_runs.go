package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// An assistant command run executes one approved command on a Dev Machine the
// requester owns, in the same credential-free check container as custom tool
// runs and under the same machine lock: never beside an agent attempt, a check,
// a tool run or another command. Its run_command operation is attempted once,
// so a command never runs twice.

// commandRunKeyPrefix starts a run_command operation's idempotency key; the
// rest is the ID of the run the operation executes.
const commandRunKeyPrefix = "command-run:"

var (
	// ErrCommandMachineUnavailable means the machine is not a running machine
	// of the requester's.
	ErrCommandMachineUnavailable = errors.New("the command needs a running machine of yours")
	// ErrCommandRunForbidden means the requester's role cannot run commands.
	ErrCommandRunForbidden = errors.New("your role cannot run commands on Dev Machines")
	// ErrCommandRunGuest means the requester is a guest of the workspace.
	ErrCommandRunGuest = errors.New("guests cannot run commands on Dev Machines")
	// ErrCommandRunNotFound means no run is readable by the requester.
	ErrCommandRunNotFound = errors.New("command run not found")
	// ErrTemporaryMachinesOff means the workspace policy allows no temporary
	// machines.
	ErrTemporaryMachinesOff = errors.New("temporary machines are turned off in this workspace")
	// ErrMigrationActionNotFound means no migration action of that name is
	// configured on a machine the caller may use.
	ErrMigrationActionNotFound = errors.New("no migration action is configured on that machine")
	// ErrMigrationActionForbidden means the caller may not configure the
	// machine's migration actions: not the machine's creator and not a
	// current workspace owner or admin.
	ErrMigrationActionForbidden = errors.New("only workspace owners, admins or the machine's creator can configure migration actions")
	// ErrMigrationActionConflict means the action's name is already used on
	// the machine.
	ErrMigrationActionConflict = errors.New("a migration action of that name already exists on this machine")
)

// CommandRunRequest is a validated command for a machine the requester owns.
type CommandRunRequest struct {
	MachineID uuid.UUID
	Command   string
	Write     bool
	// Migration marks a run_migration run; ActionName names the
	// admin-configured action whose command it carries.
	Migration        bool
	ActionName       string
	TimeoutSeconds   int
	WorkingDirectory string
	// Reused records that the machine is one of the user's own machines the
	// assistant chose instead of starting a temporary one.
	Reused         bool
	ConversationID *uuid.UUID
	ToolCallID     *uuid.UUID
	ApprovalID     *uuid.UUID
}

type AssistantCommandRepository struct {
	db *sqlx.DB
}

func NewAssistantCommandRepository(db *sqlx.DB) *AssistantCommandRepository {
	return &AssistantCommandRepository{db: db}
}

const commandRunColumns = `r.id,r.workspace_id,r.machine_id,r.machine_name,r.cpu_millis,r.memory_mb,r.disk_gb,r.temporary,r.reused,r.requested_by_user_id,r.conversation_id,
  r.tool_call_id,r.approval_id,r.command,r.write,r.migration,r.action_name,r.timeout_seconds,r.working_directory,r.status,r.exit_code,r.output,r.output_truncated,r.error,r.created_at,r.started_at,r.finished_at,
 (EXTRACT(EPOCH FROM (r.finished_at-r.started_at))*1000)::bigint AS duration_ms,
 (SELECT m.temporary_expires_at FROM dev_machines m WHERE m.id=r.machine_id) AS expires_at`

// lockCommandRunner holds the requester's membership until commit and checks
// that their role may use Dev Machines: ErrConversationNotFound for a
// non-member, ErrCommandRunGuest for a guest and ErrCommandRunForbidden for
// another role without Dev Machine access.
func lockCommandRunner(ctx context.Context, tx *sqlx.Tx, ws, actor uuid.UUID) error {
	var role string
	err := tx.GetContext(ctx, &role, `SELECT role FROM workspace_members WHERE workspace_id=$1 AND user_id=$2 FOR SHARE`, ws, actor)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrConversationNotFound
	}
	if err != nil {
		return err
	}
	if role == domain.RoleGuest {
		return ErrCommandRunGuest
	}
	if !domain.HasPermission(role, domain.PermDevMachineManage) {
		return ErrCommandRunForbidden
	}
	return nil
}

// usableMachine is a machine the user may run commands on: theirs, not being
// deleted and, when temporary, not expired.
const usableMachine = `m.workspace_id=$1 AND m.created_by_user_id=$2 AND m.delete_requested_at IS NULL
 AND (m.temporary_expires_at IS NULL OR m.temporary_expires_at > now())`

// QueueRun queues an approved command for the actor on a running machine they
// own that nothing else uses. An approval queues at most one run. A migration
// run names its action and never writes files.
func (r *AssistantCommandRepository) QueueRun(ctx context.Context, ws, actor uuid.UUID, req CommandRunRequest) (*domain.CommandRun, error) {
	if req.TimeoutSeconds == 0 {
		req.TimeoutSeconds = domain.DefaultCommandTimeoutSeconds
	}
	if !domain.ValidCommandTimeout(req.TimeoutSeconds) || (req.Migration && req.TimeoutSeconds != domain.DefaultCommandTimeoutSeconds) {
		return nil, ErrConversationInvalid
	}
	if strings.TrimSpace(req.Command) == "" || len(req.Command) > domain.CommandMaxBytes || strings.ContainsRune(req.Command, 0) {
		return nil, ErrConversationInvalid
	}
	if _, ok := domain.CommandWorkingDirectory(req.WorkingDirectory); !ok || !strings.HasPrefix(req.WorkingDirectory, "/workspace") {
		return nil, ErrConversationInvalid
	}
	if req.Migration && (!domain.ValidMigrationActionName(req.ActionName) || req.Write) {
		return nil, ErrConversationInvalid
	}
	if !req.Migration && req.ActionName != "" {
		return nil, ErrConversationInvalid
	}
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if req.ApprovalID != nil {
		existing := &domain.CommandRun{}
		err = tx.GetContext(ctx, existing, `SELECT `+commandRunColumns+` FROM assistant_command_runs r WHERE r.approval_id=$1 AND r.workspace_id=$2`, req.ApprovalID, ws)
		if err == nil {
			return existing, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	if err = lockCommandRunner(ctx, tx, ws, actor); err != nil {
		return nil, err
	}
	var machine domain.DevMachine
	err = tx.GetContext(ctx, &machine, `SELECT m.* FROM dev_machines m WHERE m.id=$3 AND `+usableMachine, ws, actor, req.MachineID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCommandMachineUnavailable
	}
	if err != nil {
		return nil, err
	}
	generation, err := lockMachineForCheck(ctx, tx, ws, machine.ID)
	if errors.Is(err, ErrCheckUnavailable) {
		return nil, ErrCommandMachineUnavailable
	}
	if err != nil {
		return nil, err
	}
	run := &domain.CommandRun{}
	if err = tx.GetContext(ctx, run, `WITH inserted AS (INSERT INTO assistant_command_runs(workspace_id,machine_id,machine_name,cpu_millis,memory_mb,disk_gb,temporary,reused,requested_by_user_id,
  conversation_id,tool_call_id,approval_id,command,write,migration,action_name,working_directory,timeout_seconds,status) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,'queued') RETURNING *)
  SELECT `+commandRunColumns+` FROM inserted r`,
		ws, machine.ID, machine.Name, machine.CPUMillis, machine.MemoryMB, machine.DiskGB, machine.TemporaryExpiresAt != nil, req.Reused, actor,
		req.ConversationID, req.ToolCallID, req.ApprovalID, req.Command, req.Write, req.Migration, req.ActionName, req.WorkingDirectory, req.TimeoutSeconds); err != nil {
		return nil, err
	}
	run.ExpiresAt = machine.TemporaryExpiresAt
	op := &domain.DevMachineOperation{WorkspaceID: ws, MachineID: machine.ID, Action: domain.DevMachineOpRunCommand, Status: domain.DevMachineOpStatusPending, Generation: generation,
		IdempotencyKey: commandRunKeyPrefix + run.ID.String(), RequestedByUserID: &actor, MaxAttempts: 1}
	if err = enqueueOperation(ctx, tx, op); err != nil {
		return nil, err
	}
	return run, tx.Commit()
}

// commandRunReadable lets a current non-guest member read the runs they asked
// for; the output comes from their own machine.
const commandRunReadable = `EXISTS(SELECT 1 FROM workspace_members wm WHERE wm.workspace_id=r.workspace_id AND wm.user_id=$3 AND wm.role<>'guest') AND r.requested_by_user_id=$3`

// Run returns one run to its requester.
func (r *AssistantCommandRepository) Run(ctx context.Context, ws, id, actor uuid.UUID) (*domain.CommandRun, error) {
	return r.readRun(ctx, `r.id=$2`, ws, id, actor)
}

// RunForApproval returns the run an approval queued, to its requester.
func (r *AssistantCommandRepository) RunForApproval(ctx context.Context, ws, approval, actor uuid.UUID) (*domain.CommandRun, error) {
	return r.readRun(ctx, `r.approval_id=$2`, ws, approval, actor)
}

func (r *AssistantCommandRepository) readRun(ctx context.Context, match string, ws, id, actor uuid.UUID) (*domain.CommandRun, error) {
	run := &domain.CommandRun{}
	err := r.db.GetContext(ctx, run, `SELECT `+commandRunColumns+` FROM assistant_command_runs r WHERE r.workspace_id=$1 AND `+match+` AND `+commandRunReadable, ws, id, actor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCommandRunNotFound
	}
	return run, err
}

// UsableMachine returns one of the user's machines commands can run on, by ID,
// or ErrCommandMachineUnavailable.
func (r *AssistantCommandRepository) UsableMachine(ctx context.Context, ws, user, machine uuid.UUID) (*domain.DevMachine, error) {
	return r.machine(ctx, `m.id=$3 AND m.status='running' AND m.desired_status='running'`, ws, user, machine)
}

// ConversationMachine returns the conversation's live temporary machine, in any
// state short of deletion, or ErrCommandMachineUnavailable.
func (r *AssistantCommandRepository) ConversationMachine(ctx context.Context, ws, user, conversation uuid.UUID) (*domain.DevMachine, error) {
	return r.machine(ctx, `m.temporary_conversation_id=$3 AND `+activeMachine+` ORDER BY m.created_at DESC`, ws, user, conversation)
}

// ReusableMachine returns the user's own running, non-temporary machine the
// assistant uses instead of starting one: machines of no issue first, then the
// most recently active.
func (r *AssistantCommandRepository) ReusableMachine(ctx context.Context, ws, user uuid.UUID) (*domain.DevMachine, error) {
	return r.machine(ctx, `m.temporary_expires_at IS NULL AND m.status='running' AND m.desired_status='running' AND $3::boolean
 ORDER BY (m.issue_id IS NULL) DESC, m.last_activity_at DESC NULLS LAST, m.created_at DESC`, ws, user, true)
}

// UserTemporaryMachine returns the user's own live temporary machine in the
// workspace, whichever conversation started it, or ErrCommandMachineUnavailable.
func (r *AssistantCommandRepository) UserTemporaryMachine(ctx context.Context, ws, user uuid.UUID) (*domain.DevMachine, error) {
	return r.machine(ctx, `m.temporary_expires_at IS NOT NULL AND `+activeMachine+` AND $3::boolean ORDER BY m.created_at DESC`, ws, user, true)
}

const activeMachine = `m.desired_status='running' AND m.status NOT IN ('destroyed','failed','expired','stopped')`

func (r *AssistantCommandRepository) machine(ctx context.Context, match string, ws, user uuid.UUID, arg any) (*domain.DevMachine, error) {
	machine := &domain.DevMachine{}
	err := r.db.GetContext(ctx, machine, `SELECT m.* FROM dev_machines m WHERE `+usableMachine+` AND `+match+` LIMIT 1`, ws, user, arg)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCommandMachineUnavailable
	}
	return machine, err
}

// CheckRunner returns nil when the user is a member whose role may run
// commands, else lockCommandRunner's refusal.
func (r *AssistantCommandRepository) CheckRunner(ctx context.Context, ws, user uuid.UUID) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = lockCommandRunner(ctx, tx, ws, user); err != nil {
		return err
	}
	return tx.Commit()
}

// TemporaryMachineQuota returns how many temporary machines are live in the
// workspace, how many its policy allows, and how many of the live ones belong
// to the user. Unlike activeMachine, liveTemporaryMachine counts machines that
// cannot run commands yet (for example a failed one still desired running).
func (r *AssistantCommandRepository) TemporaryMachineQuota(ctx context.Context, ws, user uuid.UUID) (used, limit, mine int, err error) {
	var quota struct {
		Used  int `db:"used"`
		Mine  int `db:"mine"`
		Limit int `db:"max_temporary_machines"`
	}
	err = r.db.GetContext(ctx, &quota, `SELECT p.max_temporary_machines,
  (SELECT COUNT(*) FROM dev_machines m WHERE m.workspace_id=p.workspace_id AND `+liveTemporaryMachine+`) AS used,
  (SELECT COUNT(*) FROM dev_machines m WHERE m.workspace_id=p.workspace_id AND m.created_by_user_id=$2 AND `+liveTemporaryMachine+`) AS mine
  FROM dev_machine_workspace_policies p WHERE p.workspace_id=$1`, ws, user)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, 0, nil
	}
	return quota.Used, quota.Limit, quota.Mine, err
}

// liveTemporaryMachine counts toward the temporary machine quotas until it is
// asked to be deleted.
const liveTemporaryMachine = `m.temporary_expires_at IS NOT NULL AND m.delete_requested_at IS NULL AND ` + `(m.status NOT IN ('destroyed','failed','expired','stopped') OR m.desired_status='running')`

// checkTemporaryMachineQuota refuses a temporary machine for a user who may
// not run commands (re-checked under this transaction, so a role lost after
// the approval card was shown still refuses it), beyond the workspace's quota
// or a second one for the user. The caller holds the workspace's machine lock.
func checkTemporaryMachineQuota(ctx context.Context, tx *sqlx.Tx, ws uuid.UUID, user *uuid.UUID) error {
	if user == nil {
		return ErrConversationNotFound
	}
	if err := lockCommandRunner(ctx, tx, ws, *user); err != nil {
		return err
	}
	var limit, used, mine int
	if err := tx.QueryRowContext(ctx, `SELECT max_temporary_machines FROM dev_machine_workspace_policies WHERE workspace_id=$1`, ws).Scan(&limit); err != nil {
		return err
	}
	if limit == 0 {
		return ErrTemporaryMachinesOff
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*), COUNT(*) FILTER (WHERE m.created_by_user_id=$2) FROM dev_machines m WHERE m.workspace_id=$1 AND `+liveTemporaryMachine, ws, user).Scan(&used, &mine); err != nil {
		return err
	}
	if mine > 0 {
		return &machineQuotaError{message: "you already have a temporary machine; it is deleted when it expires, or delete it yourself"}
	}
	if used >= limit {
		return &machineQuotaError{message: fmt.Sprintf("the workspace already has %d of %d allowed temporary machines", used, limit)}
	}
	return nil
}

// ListExpiredTemporaryMachines returns temporary machines past their expiry
// that are not yet asked to be deleted.
func (r *DevMachineRepository) ListExpiredTemporaryMachines(ctx context.Context, now time.Time, limit int) ([]domain.DevMachine, error) {
	machines := []domain.DevMachine{}
	err := r.db.SelectContext(ctx, &machines, `SELECT * FROM dev_machines WHERE temporary_expires_at IS NOT NULL AND temporary_expires_at <= $1 AND delete_requested_at IS NULL
 ORDER BY temporary_expires_at LIMIT $2`, now, limit)
	return machines, err
}

func commandRunID(op domain.DevMachineOperation) (uuid.UUID, bool) {
	raw, ok := strings.CutPrefix(op.IdempotencyKey, commandRunKeyPrefix)
	if !ok || op.Action != domain.DevMachineOpRunCommand {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(raw)
	return id, err == nil
}

// commandRunTransaction is a command run's write authority: the manager still
// holds the operation's lease on the machine generation it was queued for, the
// machine is still running and usable, and the requester still owns it and may
// use Dev Machines.
func (r *DevMachineRepository) commandRunTransaction(ctx context.Context, op domain.DevMachineOperation) (*sqlx.Tx, *domain.CommandRun, error) {
	id, ok := commandRunID(op)
	if !ok || op.LeaseOwner == nil || op.RequestedByUserID == nil {
		return nil, nil, ErrAgentTaskUnauthorized
	}
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	fail := func(e error) (*sqlx.Tx, *domain.CommandRun, error) {
		tx.Rollback()
		return nil, nil, e
	}
	run := &domain.CommandRun{}
	err = tx.GetContext(ctx, run, `SELECT `+commandRunColumns+` FROM assistant_command_runs r WHERE r.id=$1 AND r.workspace_id=$2 AND r.machine_id=$3 AND r.requested_by_user_id=$4 FOR UPDATE`, id, op.WorkspaceID, op.MachineID, op.RequestedByUserID)
	if errors.Is(err, sql.ErrNoRows) {
		return fail(ErrCommandMachineUnavailable)
	}
	if err != nil {
		return fail(err)
	}
	var current domain.DevMachineOperation
	err = tx.GetContext(ctx, &current, `SELECT * FROM dev_machine_operations WHERE id=$1 AND action='run_command' AND workspace_id=$2 AND machine_id=$3 AND generation=$4 AND status='leased' AND lease_owner=$5 AND attempts=$6 AND lease_expires_at>clock_timestamp() FOR SHARE`,
		op.ID, op.WorkspaceID, op.MachineID, op.Generation, *op.LeaseOwner, op.Attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return fail(ErrAgentTaskConflict)
	}
	if err != nil {
		return fail(err)
	}
	var usable bool
	if err = tx.GetContext(ctx, &usable, `SELECT EXISTS(SELECT 1 FROM dev_machines m JOIN dev_machine_workspace_policies p ON p.workspace_id=m.workspace_id AND p.enabled
 WHERE m.id=$3 AND m.generation=$4 AND m.status='running' AND m.desired_status='running' AND `+usableMachine+`)`, op.WorkspaceID, op.RequestedByUserID, op.MachineID, op.Generation); err != nil {
		return fail(err)
	}
	if !usable {
		return fail(ErrCommandMachineUnavailable)
	}
	if err = lockCommandRunner(ctx, tx, op.WorkspaceID, *op.RequestedByUserID); err != nil {
		return fail(ErrCommandRunForbidden)
	}
	return tx, run, nil
}

// GetCommandRun returns the run an operation executes, under its authority.
func (r *DevMachineRepository) GetCommandRun(ctx context.Context, op domain.DevMachineOperation) (*domain.CommandRun, error) {
	tx, run, err := r.commandRunTransaction(ctx, op)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	return run, tx.Commit()
}

// StartCommandRun marks a run running.
func (r *DevMachineRepository) StartCommandRun(ctx context.Context, op domain.DevMachineOperation) error {
	tx, run, err := r.commandRunTransaction(ctx, op)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if run.Status != "queued" && run.Status != "running" {
		return ErrAgentTaskConflict
	}
	if _, err = tx.ExecContext(ctx, `UPDATE assistant_command_runs SET status='running',started_at=COALESCE(started_at,now()),output='',output_truncated=false WHERE id=$1`, run.ID); err != nil {
		return err
	}
	return tx.Commit()
}

// AppendCommandRunOutput adds output, keeping only the stored tail.
func (r *DevMachineRepository) AppendCommandRunOutput(ctx context.Context, op domain.DevMachineOperation, content string, truncated bool) error {
	tx, run, err := r.commandRunTransaction(ctx, op)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if run.Status != "running" {
		return ErrAgentTaskConflict
	}
	output := run.Output + content
	if len(output) > domain.CommandOutputBytes {
		output, truncated = outputTail(output, domain.CommandOutputBytes), true
	}
	if _, err = tx.ExecContext(ctx, `UPDATE assistant_command_runs SET output=$2,output_truncated=output_truncated OR $3 WHERE id=$1`, run.ID, output, truncated); err != nil {
		return err
	}
	return tx.Commit()
}

// CompleteCommandRun records the command's exit status.
func (r *DevMachineRepository) CompleteCommandRun(ctx context.Context, op domain.DevMachineOperation, exit int) error {
	tx, run, err := r.commandRunTransaction(ctx, op)
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
	if _, err = tx.ExecContext(ctx, `UPDATE assistant_command_runs SET status=$2,exit_code=$3,finished_at=now() WHERE id=$1`, run.ID, status, exit); err != nil {
		return err
	}
	return tx.Commit()
}

// FailCommandRun records a run whose operation failed for good, so it never
// stays queued or running. It needs only the failed operation, not the lease.
func (r *DevMachineRepository) FailCommandRun(ctx context.Context, op domain.DevMachineOperation, summary string) error {
	id, ok := commandRunID(op)
	if !ok {
		return ErrAgentTaskUnauthorized
	}
	result, err := r.db.ExecContext(ctx, `UPDATE assistant_command_runs SET status='failed',error=left($3,2048),finished_at=now() WHERE id=$1 AND machine_id=$2 AND status IN ('queued','running')
 AND EXISTS(SELECT 1 FROM dev_machine_operations WHERE id=$4 AND machine_id=$2 AND action='run_command' AND idempotency_key=$5 AND status='failed')`, id, op.MachineID, summary, op.ID, op.IdempotencyKey)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		var failed bool
		if err = r.db.GetContext(ctx, &failed, `SELECT EXISTS(SELECT 1 FROM dev_machine_operations WHERE id=$1 AND action='run_command' AND status='failed')`, op.ID); err != nil {
			return err
		}
		if !failed {
			return ErrAgentTaskConflict
		}
	}
	return nil
}
