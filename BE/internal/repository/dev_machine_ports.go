package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/lib/pq"
)

var (
	// ErrPortExists means the machine already exposes that port.
	ErrPortExists = errors.New("the machine already exposes this port")
	// ErrPreviewUnavailable means a task preview cannot be started now.
	ErrPreviewUnavailable = errors.New("the task has no preview command or no ready checkout on a running machine")
)

// revokePortSessions revokes the browser sessions of the app port hosts named
// by ports. A port host's first label is "<routing key>-<port>", so this needs
// no machine domain and never matches the machine's IDE, terminal or browser.
func revokePortSessions(ctx context.Context, tx *sqlx.Tx, machineID uuid.UUID, ports []int) error {
	if len(ports) == 0 {
		return nil
	}
	_, err := tx.ExecContext(ctx, `UPDATE dev_machine_access_sessions a SET revoked_at=now() FROM dev_machines m
 WHERE m.id=$1 AND a.machine_id=m.id AND a.workspace_id=m.workspace_id AND a.revoked_at IS NULL
 AND split_part(a.bound_host,'.',1) IN (SELECT m.routing_key||'-'||p FROM unnest($2::int[]) p)`, machineID, pq.Array(ports))
	return err
}

// ListPorts returns a machine's app ports in port order. The task identifier
// is included only when the reader created the task and may see its issue.
func (r *DevMachineRepository) ListPorts(ctx context.Context, workspaceID, machineID uuid.UUID) ([]domain.DevMachinePort, error) {
	viewer := "CAST(NULL AS uuid)"
	if access, ok := domain.AccessFromContext(ctx); ok && access.UserID != uuid.Nil {
		viewer = uuidSQL(access.UserID)
	}
	ports := []domain.DevMachinePort{}
	err := r.db.SelectContext(ctx, &ports, `SELECT p.*,
 CASE WHEN t.creator_id=`+viewer+` AND `+teamVisible(ctx, "i.team_id")+` THEN i.identifier_text END AS task_identifier
 FROM dev_machine_ports p
 LEFT JOIN agent_tasks t ON t.id=p.agent_task_id AND t.workspace_id=p.workspace_id
 LEFT JOIN issues i ON i.id=t.issue_id AND i.workspace_id=t.workspace_id
 WHERE p.workspace_id=$1 AND p.machine_id=$2 ORDER BY p.port`, workspaceID, machineID)
	return ports, err
}

// MachinePorts returns a machine's app ports for the manager, without task
// identifiers.
func (r *DevMachineRepository) MachinePorts(ctx context.Context, machineID uuid.UUID) ([]domain.DevMachinePort, error) {
	ports := []domain.DevMachinePort{}
	err := r.db.SelectContext(ctx, &ports, `SELECT p.*,NULL::text AS task_identifier FROM dev_machine_ports p WHERE p.machine_id=$1 ORDER BY p.port`, machineID)
	return ports, err
}

func (r *DevMachineRepository) GetPort(ctx context.Context, workspaceID, machineID, portID uuid.UUID) (*domain.DevMachinePort, error) {
	var port domain.DevMachinePort
	err := r.db.GetContext(ctx, &port, `SELECT p.*,NULL::text AS task_identifier FROM dev_machine_ports p WHERE p.workspace_id=$1 AND p.machine_id=$2 AND p.id=$3`, workspaceID, machineID, portID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &port, err
}

// CreatePort exposes a port. Ports of one machine are created one at a time
// under the machine row lock. A port that, at the latest scan, only the
// machine's Docker daemon publishes is served by the docker service.
func (r *DevMachineRepository) CreatePort(ctx context.Context, port *domain.DevMachinePort) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = createPort(ctx, tx, port); err != nil {
		return err
	}
	return tx.Commit()
}

func createPort(ctx context.Context, tx *sqlx.Tx, port *domain.DevMachinePort) error {
	var target string
	err := tx.GetContext(ctx, &target, `SELECT CASE WHEN $3=ANY(m.docker_listening_ports) THEN 'docker' ELSE 'developer' END FROM dev_machines m
 WHERE m.id=$1 AND m.workspace_id=$2 FOR NO KEY UPDATE`, port.MachineID, port.WorkspaceID, port.Port)
	if errors.Is(err, sql.ErrNoRows) {
		return sql.ErrNoRows
	}
	if err != nil {
		return err
	}
	var exists bool
	if err = tx.GetContext(ctx, &exists, `SELECT EXISTS(SELECT 1 FROM dev_machine_ports WHERE machine_id=$1 AND port=$2)`, port.MachineID, port.Port); err != nil {
		return err
	}
	if exists {
		return ErrPortExists
	}
	// The task, when named, must be the same workspace's and use this machine.
	return tx.GetContext(ctx, port, `INSERT INTO dev_machine_ports(workspace_id,machine_id,port,label,source,agent_task_id,created_by_user_id,target)
 SELECT $1,$2,$3,$4,$5,$6,$7,$8 WHERE $6::uuid IS NULL OR EXISTS(SELECT 1 FROM agent_tasks t WHERE t.id=$6 AND t.workspace_id=$1 AND t.machine_id=$2)
 RETURNING *,NULL::text AS task_identifier`, port.WorkspaceID, port.MachineID, port.Port, port.Label, port.Source, port.AgentTaskID, port.CreatedByUserID, target)
}

// UpdatePortLabel renames an app port; it returns nil when there is none.
func (r *DevMachineRepository) UpdatePortLabel(ctx context.Context, workspaceID, machineID, portID uuid.UUID, label *string) (*domain.DevMachinePort, error) {
	var port domain.DevMachinePort
	err := r.db.GetContext(ctx, &port, `UPDATE dev_machine_ports SET label=$4,updated_at=now() WHERE workspace_id=$1 AND machine_id=$2 AND id=$3 RETURNING *,NULL::text AS task_identifier`, workspaceID, machineID, portID, label)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &port, err
}

// DeletePort removes an app port and revokes its host's sessions. Removing a
// task's port also stops that task's preview process.
func (r *DevMachineRepository) DeletePort(ctx context.Context, workspaceID, machineID, portID uuid.UUID, actor uuid.UUID) (*domain.DevMachinePort, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var port domain.DevMachinePort
	err = tx.GetContext(ctx, &port, `DELETE FROM dev_machine_ports WHERE workspace_id=$1 AND machine_id=$2 AND id=$3 RETURNING *,NULL::text AS task_identifier`, workspaceID, machineID, portID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err = revokePortSessions(ctx, tx, machineID, []int{port.Port}); err != nil {
		return nil, err
	}
	if port.Source == domain.DevMachinePortSourceTask && port.AgentTaskID != nil {
		if err = queueStopPreview(ctx, tx, *port.AgentTaskID, &actor); err != nil {
			return nil, err
		}
	}
	return &port, tx.Commit()
}

// DeleteAgentPort removes a port an agent exposed; agents cannot remove the
// owner's or a task's ports. It reports whether one was removed.
func (r *DevMachineRepository) DeleteAgentPort(ctx context.Context, machineID uuid.UUID, port int) (bool, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `DELETE FROM dev_machine_ports WHERE machine_id=$1 AND port=$2 AND source='agent'`, machineID, port)
	if err != nil {
		return false, err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return false, nil
	}
	if err = revokePortSessions(ctx, tx, machineID, []int{port}); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// SetListeningPorts records the latest port scan: every listening port and
// those published by the machine's Docker daemon. An exposed port moves to the
// container that now listens on it (the daemon when it publishes the port);
// a port nothing listens on keeps its target.
func (r *DevMachineRepository) SetListeningPorts(ctx context.Context, machineID uuid.UUID, listening, docker []int) error {
	if docker == nil {
		docker = []int{}
	}
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE dev_machines SET listening_ports=$2,docker_listening_ports=$3,listening_ports_at=now() WHERE id=$1`,
		machineID, pq.Array(listening), pq.Array(docker)); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE dev_machine_ports SET target=CASE WHEN port=ANY($3::int[]) THEN 'docker' ELSE 'developer' END,updated_at=now()
 WHERE machine_id=$1 AND port=ANY($2::int[]) AND target<>CASE WHEN port=ANY($3::int[]) THEN 'docker' ELSE 'developer' END`,
		machineID, pq.Array(listening), pq.Array(docker)); err != nil {
		return err
	}
	return tx.Commit()
}

// ListPortScanMachines returns running machines, least recently scanned first.
func (r *DevMachineRepository) ListPortScanMachines(ctx context.Context, limit int) ([]domain.DevMachine, error) {
	machines := []domain.DevMachine{}
	err := r.db.SelectContext(ctx, &machines, `SELECT * FROM dev_machines WHERE status='running' AND desired_status='running'
 ORDER BY listening_ports_at NULLS FIRST,id LIMIT $1`, limit)
	return machines, err
}

// TaskForWorktree returns the task whose checkout is at path on the machine,
// preferring one whose attempt is active, or nil.
func (r *DevMachineRepository) TaskForWorktree(ctx context.Context, machineID uuid.UUID, path string) (*uuid.UUID, error) {
	var id uuid.UUID
	err := r.db.GetContext(ctx, &id, `SELECT t.id FROM agent_tasks t JOIN dev_machine_checkouts c ON c.id=t.checkout_id AND c.machine_id=t.machine_id
 WHERE t.machine_id=$1 AND c.workspace_path=$2
 ORDER BY t.state IN ('preparing','running','waiting_input','validating','publishing') DESC,t.updated_at DESC LIMIT 1`, machineID, path)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &id, err
}

// TaskPreviewTarget is what the manager needs to run a task's preview.
type TaskPreviewTarget struct {
	Task       domain.AgentTask
	Identifier string
	// Port is the task's current app port, if any.
	Port *domain.DevMachinePort
}

// PreviewCommand decodes the task's preview argv.
func (t *TaskPreviewTarget) PreviewCommand() []string {
	var argv []string
	if json.Unmarshal(t.Task.PreviewCommand, &argv) != nil {
		return nil
	}
	return argv
}

// PreviewTarget returns a task's preview inputs, or nil without a task.
func (r *DevMachineRepository) PreviewTarget(ctx context.Context, taskID uuid.UUID) (*TaskPreviewTarget, error) {
	var target TaskPreviewTarget
	if err := r.db.GetContext(ctx, &target.Task, `SELECT * FROM agent_tasks WHERE id=$1`, taskID); errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	if err := r.db.GetContext(ctx, &target.Identifier, `SELECT identifier_text FROM issues WHERE id=$1 AND workspace_id=$2`, target.Task.IssueID, target.Task.WorkspaceID); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	var port domain.DevMachinePort
	err := r.db.GetContext(ctx, &port, `SELECT p.*,NULL::text AS task_identifier FROM dev_machine_ports p WHERE p.agent_task_id=$1 AND p.source='task' AND p.workspace_id=$2 ORDER BY p.created_at LIMIT 1`, taskID, target.Task.WorkspaceID)
	if err == nil {
		target.Port = &port
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	return &target, nil
}

// SetTaskPreview records a task's preview state and app port, and notifies
// its conversation.
func (r *DevMachineRepository) SetTaskPreview(ctx context.Context, taskID uuid.UUID, state string, message *string, port *domain.DevMachinePort) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var portID *uuid.UUID
	var number *int
	if port != nil {
		portID, number = &port.ID, &port.Port
	}
	if message != nil && len(*message) > 500 {
		short := (*message)[:500]
		message = &short
	}
	var task domain.AgentTask
	err = tx.GetContext(ctx, &task, `UPDATE agent_tasks SET preview_state=$2,preview_error=$3,preview_port_id=$4,preview_app_port=$5 WHERE id=$1 RETURNING *`, taskID, state, message, portID, number)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if err = previewChanged(ctx, tx, task); err != nil {
		return err
	}
	return tx.Commit()
}

// StopTaskPreview removes the task's preview port, revokes its sessions and
// records the preview as stopped.
func (r *DevMachineRepository) StopTaskPreview(ctx context.Context, taskID uuid.UUID) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = stopTaskPreviews(ctx, tx, `WHERE id=$1`, taskID); err != nil {
		return err
	}
	return tx.Commit()
}

// StopMachinePreviews records that a machine's task preview processes ended
// with its containers: their ports are removed, other ports are kept.
func (r *DevMachineRepository) StopMachinePreviews(ctx context.Context, machineID uuid.UUID) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = stopTaskPreviews(ctx, tx, `WHERE machine_id=$1 AND preview_state IN ('starting','running')`, machineID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE dev_machines SET listening_ports='{}',docker_listening_ports='{}',listening_ports_at=NULL WHERE id=$1`, machineID); err != nil {
		return err
	}
	return tx.Commit()
}

// stopTaskPreviews applies to the tasks matched by where (one $1 argument).
func stopTaskPreviews(ctx context.Context, tx *sqlx.Tx, where string, arg uuid.UUID) error {
	ids := []uuid.UUID{}
	if err := tx.SelectContext(ctx, &ids, `SELECT id FROM agent_tasks `+where+` ORDER BY id FOR NO KEY UPDATE`, arg); err != nil {
		return err
	}
	for _, id := range ids {
		var removed []struct {
			MachineID uuid.UUID `db:"machine_id"`
			Port      int       `db:"port"`
		}
		if err := tx.SelectContext(ctx, &removed, `DELETE FROM dev_machine_ports WHERE agent_task_id=$1 AND source='task' RETURNING machine_id,port`, id); err != nil {
			return err
		}
		for _, port := range removed {
			if err := revokePortSessions(ctx, tx, port.MachineID, []int{port.Port}); err != nil {
				return err
			}
		}
		var task domain.AgentTask
		if err := tx.GetContext(ctx, &task, `UPDATE agent_tasks SET preview_state='stopped',preview_error=NULL,preview_port_id=NULL,preview_app_port=NULL WHERE id=$1 RETURNING *`, id); err != nil {
			return err
		}
		if err := previewChanged(ctx, tx, task); err != nil {
			return err
		}
	}
	return nil
}

func previewChanged(ctx context.Context, tx *sqlx.Tx, task domain.AgentTask) error {
	if task.ConversationID == nil {
		return nil
	}
	_, err := conversationEvent(ctx, tx, *task.ConversationID, "task.updated", task)
	return err
}

// queueStopPreview queues the stop of a task's preview process, when its
// machine still exists, or records it as stopped otherwise.
func queueStopPreview(ctx context.Context, tx *sqlx.Tx, taskID uuid.UUID, actor *uuid.UUID) error {
	var target struct {
		WorkspaceID uuid.UUID  `db:"workspace_id"`
		MachineID   *uuid.UUID `db:"machine_id"`
		Generation  *int64     `db:"generation"`
	}
	err := tx.GetContext(ctx, &target, `SELECT t.workspace_id,t.machine_id,m.generation FROM agent_tasks t
 LEFT JOIN dev_machines m ON m.id=t.machine_id AND m.workspace_id=t.workspace_id AND m.status<>'destroyed' WHERE t.id=$1`, taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if target.MachineID == nil || target.Generation == nil {
		return stopTaskPreviews(ctx, tx, `WHERE id=$1`, taskID)
	}
	return enqueueOperation(ctx, tx, &domain.DevMachineOperation{
		WorkspaceID: target.WorkspaceID, MachineID: *target.MachineID, AgentTaskID: &taskID,
		Action: domain.DevMachineOpStopPreview, Status: domain.DevMachineOpStatusPending, Generation: *target.Generation,
		IdempotencyKey: "task-preview-stop:" + taskID.String() + ":" + uuid.NewString(), RequestedByUserID: actor, MaxAttempts: 3,
	})
}

// RequestPreview starts or stops a task's preview for its creator. A start
// needs a preview command and the task's ready checkout on a running machine.
func (r *AgentTaskRepository) RequestPreview(ctx context.Context, ws, id uuid.UUID, start bool) (*domain.AgentTask, error) {
	if _, err := conversationActor(ctx, ws); err != nil {
		return nil, err
	}
	tx, task, err := r.transaction(ctx, id, true)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if task.WorkspaceID != ws {
		return nil, ErrConversationNotFound
	}
	if !start {
		if task.PreviewState == nil || *task.PreviewState == domain.AgentTaskPreviewStopped {
			return task, nil
		}
		if err = queueStopPreview(ctx, tx, task.ID, &task.CreatorID); err != nil {
			return nil, err
		}
		if err = tx.GetContext(ctx, task, `SELECT * FROM agent_tasks WHERE id=$1`, id); err != nil {
			return nil, err
		}
		return task, tx.Commit()
	}
	if task.MachineID == nil || len(previewArgv(task.PreviewCommand)) == 0 {
		return nil, ErrPreviewUnavailable
	}
	if task.PreviewState != nil && *task.PreviewState == domain.AgentTaskPreviewStarting {
		return task, nil
	}
	var generation int64
	err = tx.GetContext(ctx, &generation, `SELECT m.generation FROM dev_machines m JOIN dev_machine_checkouts c ON c.machine_id=m.id AND c.id=$3 AND c.status='ready'
 WHERE m.id=$1 AND m.workspace_id=$2 AND m.status='running' AND m.desired_status='running'`, task.MachineID, task.WorkspaceID, task.CheckoutID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrPreviewUnavailable
	}
	if err != nil {
		return nil, err
	}
	if err = queueStartPreview(ctx, tx, task, generation, "task-preview-start:"+task.ID.String()+":"+uuid.NewString()); err != nil {
		return nil, err
	}
	if err = previewChanged(ctx, tx, *task); err != nil {
		return nil, err
	}
	return task, tx.Commit()
}

func previewArgv(raw json.RawMessage) []string {
	var argv []string
	if len(raw) == 0 || json.Unmarshal(raw, &argv) != nil {
		return nil
	}
	return argv
}

// queueStartPreview queues a start under key and marks the preview starting
// when the operation is new; an existing key changes nothing.
func queueStartPreview(ctx context.Context, tx *sqlx.Tx, task *domain.AgentTask, generation int64, key string) error {
	operation := &domain.DevMachineOperation{
		ID: uuid.New(), WorkspaceID: task.WorkspaceID, MachineID: *task.MachineID, CheckoutID: &task.CheckoutID, AgentTaskID: &task.ID,
		Action: domain.DevMachineOpStartPreview, Status: domain.DevMachineOpStatusPending, Generation: generation,
		IdempotencyKey: key, RequestedByUserID: &task.CreatorID, MaxAttempts: 3,
	}
	queued := operation.ID
	if err := enqueueOperation(ctx, tx, operation); err != nil {
		return err
	}
	if operation.ID != queued {
		return nil
	}
	return tx.GetContext(ctx, task, `UPDATE agent_tasks SET preview_state='starting',preview_error=NULL WHERE id=$1 RETURNING *`, task.ID)
}
