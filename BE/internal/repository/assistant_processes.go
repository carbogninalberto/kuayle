package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

const processOperationPrefix = "assistant-process:"

var (
	ErrProcessQuota        = errors.New("process limit reached. Each user and machine can run one process, and each workspace can run four")
	ErrProcessPortReserved = errors.New("the port is reserved for a machine service")
)

const processActive = `p.status IN ('queued','running','stopping')`

// A process loses its route immediately when it expires or loses its scope.
// Reconciliation then removes the container and releases its reservation.
const processAllowed = `kuayle_assistant_process_available(p.id,false)`

// The gateway role can check a route through this function without access
// to either the process records or their conversations.
const appPortAvailable = `(port.target<>'process' OR kuayle_assistant_process_available(port.id,true))`

const processFields = `p.id,p.workspace_id,p.machine_id,p.routing_key,p.command,p.working_directory,p.port,p.expires_at`

// QueueAssistantProcess reserves the quota, port and start operation in one
// transaction. The policy row serializes workspace quota checks and the
// machine row serializes port allocation with task and user app ports.
func (r *ConversationRepository) QueueAssistantProcess(ctx context.Context, ws, conversation, id uuid.UUID, req domain.AssistantProcessPayload) error {
	if dir, ok := domain.CommandWorkingDirectory(req.WorkingDirectory); !ok || dir != req.WorkingDirectory ||
		!domain.ValidAppPort(req.Port) || strings.TrimSpace(req.Command) == "" || len(req.Command) > domain.CommandMaxBytes || strings.ContainsRune(req.Command, 0) {
		return ErrConversationInvalid
	}
	tx, chat, err := r.authorized(ctx, ws, conversation)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	approval, err := lockApproval(ctx, tx, chat.ID, id, chat.CreatorID)
	if err != nil {
		return err
	}
	if approval.Action != "start_process" || approval.Status != "approving" {
		return ErrConversationInvalid
	}
	var exists bool
	if err = tx.GetContext(ctx, &exists, `SELECT EXISTS(SELECT 1 FROM assistant_processes WHERE id=$1)`, id); err != nil || exists {
		return err
	}
	if err = lockCommandRunner(ctx, tx, ws, chat.CreatorID); err != nil {
		return err
	}
	var enabled bool
	err = tx.GetContext(ctx, &enabled, `SELECT enabled FROM dev_machine_workspace_policies WHERE workspace_id=$1 FOR NO KEY UPDATE`, ws)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !enabled) {
		return ErrCommandMachineUnavailable
	}
	if err != nil {
		return err
	}
	var machine domain.DevMachine
	err = tx.GetContext(ctx, &machine, `SELECT m.* FROM dev_machines m WHERE m.id=$3 AND `+usableMachine+
		` AND m.status='running' AND m.desired_status='running' FOR NO KEY UPDATE`, ws, chat.CreatorID, req.MachineID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrCommandMachineUnavailable
	}
	if err != nil {
		return err
	}

	var full bool
	err = tx.GetContext(ctx, &full, `SELECT count(*) >= $4 OR COALESCE(bool_or(p.user_id=$2 OR p.machine_id=$3),false)
 FROM assistant_processes p WHERE p.workspace_id=$1 AND `+processActive, ws, chat.CreatorID, machine.ID, domain.AssistantProcessQuota)
	if err != nil {
		return err
	}
	if full {
		return ErrProcessQuota
	}
	if err = tx.GetContext(ctx, &exists, `SELECT EXISTS(SELECT 1 FROM dev_machine_services WHERE machine_id=$1 AND internal_port=$2)`, machine.ID, req.Port); err != nil {
		return err
	}
	if exists {
		return ErrProcessPortReserved
	}
	if err = tx.GetContext(ctx, &exists, `SELECT EXISTS(SELECT 1 FROM dev_machine_ports WHERE machine_id=$1 AND port=$2)`, machine.ID, req.Port); err != nil {
		return err
	}
	if exists {
		return ErrPortExists
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO assistant_processes(id,workspace_id,conversation_id,machine_id,user_id,generation,routing_key,command,working_directory,port,status,expires_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'queued',least(now()+make_interval(secs=>$11),$12::timestamptz))`,
		id, ws, chat.ID, machine.ID, chat.CreatorID, machine.Generation, machine.RoutingKey, req.Command, req.WorkingDirectory, req.Port,
		domain.AssistantProcessTTL.Seconds(), machine.TemporaryExpiresAt)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO dev_machine_ports(id,workspace_id,machine_id,port,source,target,label,created_by_user_id)
 VALUES($1,$2,$3,$4,'process','process','Assistant process',$5)`, id, ws, machine.ID, req.Port, chat.CreatorID); err != nil {
		return err
	}
	if err = enqueueOperation(ctx, tx, &domain.DevMachineOperation{
		WorkspaceID: ws, MachineID: machine.ID, Action: domain.DevMachineOpStartProcess, Status: domain.DevMachineOpStatusPending,
		Generation: machine.Generation, IdempotencyKey: processOperationPrefix + id.String(), RequestedByUserID: &chat.CreatorID, MaxAttempts: 1,
	}); err != nil {
		return err
	}
	return tx.Commit()
}

// AssistantProcess reads or stops one process of an authorized conversation.
// The stop is idempotent and immediately disables the preview route.
func (r *ConversationRepository) AssistantProcess(ctx context.Context, ws, conversation, id uuid.UUID, stop bool) (*domain.AssistantProcess, error) {
	tx, chat, err := r.authorized(ctx, ws, conversation)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	args := []any{id, ws, chat.ID, chat.CreatorID}
	const match = `p.id=$1 AND p.workspace_id=$2 AND p.conversation_id=$3 AND p.user_id=$4`
	if stop {
		if _, err = tx.ExecContext(ctx, `UPDATE assistant_processes p SET status='stopping' WHERE `+match+` AND `+processActive, args...); err != nil {
			return nil, err
		}
	}
	var process domain.AssistantProcess
	err = tx.GetContext(ctx, &process, `SELECT `+processFields+`,CASE WHEN `+processActive+` AND NOT (`+processAllowed+`)
 THEN 'stopping' ELSE p.status END AS status FROM assistant_processes p WHERE `+match, args...)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrConversationNotFound
	}
	if err != nil {
		return nil, err
	}
	return &process, tx.Commit()
}

// processStartAllowed fences startup and publication of the port by the
// operation's current lease. A stop during container creation wins.
const processStartAllowed = `p.status='queued' AND o.id=$1 AND o.lease_owner=$2 AND o.status='leased' AND o.lease_expires_at>now()
 AND o.action='start_process' AND o.workspace_id=p.workspace_id AND o.machine_id=p.machine_id
 AND o.generation=p.generation AND o.idempotency_key='` + processOperationPrefix + `'||p.id
 AND ` + processAllowed

func (r *DevMachineRepository) AssistantProcessToStart(ctx context.Context, op domain.DevMachineOperation) (*domain.AssistantProcess, error) {
	var process domain.AssistantProcess
	err := r.db.GetContext(ctx, &process, `SELECT `+processFields+`,p.status FROM assistant_processes p, dev_machine_operations o WHERE `+processStartAllowed, op.ID, op.LeaseOwner)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &process, err
}

func (r *DevMachineRepository) MarkAssistantProcessRunning(ctx context.Context, op domain.DevMachineOperation) (bool, error) {
	result, err := r.db.ExecContext(ctx, `UPDATE assistant_processes p SET status='running'
 FROM dev_machine_operations o WHERE `+processStartAllowed, op.ID, op.LeaseOwner)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows == 1, err
}

// AssistantProcessesToCheck includes running containers and expired, stopped
// or failed starts. While a start has the lease it owns physical cleanup.
func (r *DevMachineRepository) AssistantProcessesToCheck(ctx context.Context) ([]domain.AssistantProcess, error) {
	var processes []domain.AssistantProcess
	err := r.db.SelectContext(ctx, &processes, `SELECT `+processFields+`,
 CASE WHEN p.status='stopping' OR NOT (`+processAllowed+`) THEN 'stopping' ELSE p.status END AS status
 FROM assistant_processes p
 LEFT JOIN dev_machine_operations o ON o.machine_id=p.machine_id AND o.idempotency_key='`+processOperationPrefix+`'||p.id
 WHERE `+processActive+`
 AND (p.status<>'queued' OR NOT (`+processAllowed+`) OR o.status IS NULL OR o.status NOT IN ('pending','leased'))
 AND (o.status IS DISTINCT FROM 'leased' OR o.lease_expires_at<=now())`)
	return processes, err
}

// FinishAssistantProcess releases the port and quota only after container
// removal. Completion is idempotent across managers and retries.
func (r *DevMachineRepository) FinishAssistantProcess(ctx context.Context, id uuid.UUID, status string) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `UPDATE assistant_processes p SET status=CASE WHEN status='stopping' THEN 'stopped' ELSE $2 END
 WHERE id=$1 AND `+processActive, id, status)
	if err != nil {
		return err
	}
	var port domain.DevMachinePort
	err = tx.GetContext(ctx, &port, `DELETE FROM dev_machine_ports WHERE id=$1 AND source='process' RETURNING *,NULL::text AS task_identifier`, id)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		if err = revokeAssistantProcessPort(ctx, tx, &port); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func revokeAssistantProcessPort(ctx context.Context, tx *sqlx.Tx, port *domain.DevMachinePort) error {
	if err := revokePortSessions(ctx, tx, port.MachineID, []int{port.Port}); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE dev_machine_access_tickets t SET status='revoked',revoked_at=now() FROM dev_machines m
 WHERE m.id=$1 AND t.machine_id=m.id AND t.workspace_id=m.workspace_id AND t.status='active'
 AND split_part(t.bound_host,'.',1)=m.routing_key||'-'||($2::int)::text`, port.MachineID, port.Port)
	return err
}
