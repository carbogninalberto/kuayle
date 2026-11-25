package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// Capacity of member turns (KUA-298). A member's turn is an agent task, so
// it takes a task slot on its machine like any task: the machine's own
// max_tasks, or the workspace policy's task_machine_max_tasks
// (taskSlotLimit), bounds the tasks and the turns working there together.
// The tick starts a turn only into a free slot; the turns that wait for one
// machine start oldest unread first, across tasks.

// ErrNoTaskSlot is a member's turn that waits for a task slot: its machine
// runs as many tasks as its limit allows, or a turn of another task that has
// waited longer on its room waits for the same machine.
var ErrNoTaskSlot = errors.New("the turn waits for a free task slot on its machine")

// memberSlot is the machine whose task slot a member's turn takes: its
// task's machine, or for a first turn (or a machine gone since) the root
// task's; uuid.Nil when neither is live, and the turn's placement picks one
// (ReserveMachine). It holds the workspace lock that placement and attempts
// take (ReserveMachine, CreateAgentRun) until the caller's transaction ends,
// so two ticks cannot both take the last slot. The turn waits
// (ErrNoTaskSlot) while the machine's slots are all taken, or while a turn
// of another task that waits for the same machine (the roles its tick left
// in pipeline_state's queue) has an older oldest unread post; ties go by
// task and role. task is nil for a first turn; read is the role's room
// cursor.
func memberSlot(ctx context.Context, tx *sqlx.Tx, root domain.AgentTask, role string, task *domain.AgentTask, read int64) (uuid.UUID, error) {
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))`, root.WorkspaceID); err != nil {
		return uuid.Nil, err
	}
	var own, self, rootMachine any
	if root.MachineID != nil {
		rootMachine = *root.MachineID
	}
	if task != nil {
		self = task.ID
		if task.MachineID != nil {
			own = *task.MachineID
		}
	}
	var slot struct {
		ID    uuid.UUID `db:"id"`
		Tasks int       `db:"tasks"`
		Limit int       `db:"slots"`
	}
	err := tx.GetContext(ctx, &slot, `SELECT m.id,(SELECT count(*) FROM agent_tasks t WHERE t.machine_id=m.id AND t.id IS DISTINCT FROM $4::uuid
  AND `+holdsTaskSlot("t")+`) AS tasks,
 COALESCE(`+taskSlotLimit+`,$5) AS slots
 FROM dev_machines m LEFT JOIN dev_machine_workspace_policies policy ON policy.workspace_id=m.workspace_id
 WHERE m.workspace_id=$1 AND m.id IN ($2,$3) AND m.status<>'destroyed' AND m.delete_requested_at IS NULL
 ORDER BY m.id IS NOT DISTINCT FROM $2::uuid DESC LIMIT 1`, root.WorkspaceID, own, rootMachine, self, domain.DefaultTaskMachineMaxTasks)
	if errors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, nil
	}
	if err != nil {
		return uuid.Nil, err
	}
	if slot.Tasks >= slot.Limit {
		return uuid.Nil, ErrNoTaskSlot
	}
	var behind bool
	if err = tx.GetContext(ctx, &behind, `SELECT EXISTS(SELECT 1 FROM agent_tasks r
 CROSS JOIN LATERAL jsonb_array_elements_text(CASE WHEN jsonb_typeof(r.pipeline_state->'queue')='array' THEN r.pipeline_state->'queue' ELSE '[]' END) q(role)
 LEFT JOIN task_members w ON w.task_id=r.id AND w.workspace_id=r.workspace_id AND w.role=q.role
 LEFT JOIN agent_tasks wt ON wt.id=w.member_task_id AND wt.workspace_id=r.workspace_id
 WHERE r.workspace_id=$1 AND r.pipeline AND r.state='awaiting_review' AND r.workflow_paused_at IS NULL AND r.id<>$2
 AND (wt.id IS NULL OR (wt.state IN ('blocked','failed','cancelled') AND wt.usage_limit_message IS NULL))
 AND COALESCE(wt.machine_id,r.machine_id)=$3
 AND (COALESCE(`+oldestUnreadSQL("r.id", "q.role", "COALESCE(w.last_read_seq,0)")+`,'infinity'),r.id,q.role)
  < (COALESCE(`+oldestUnreadSQL("$2", "$4", "$5")+`,'infinity'),$2::uuid,$4::text))`,
		root.WorkspaceID, root.ID, slot.ID, role, read); err != nil {
		return uuid.Nil, err
	}
	if behind {
		return uuid.Nil, ErrNoTaskSlot
	}
	return slot.ID, nil
}

// oldestUnreadSQL is when the oldest post of a role's inbox after its room
// cursor (domain.RoomInbox) was written, NULL when there is none: task, role
// and cursor are SQL expressions.
func oldestUnreadSQL(task, role, cursor string) string {
	return `(SELECT min(p.created_at) FROM task_messages p WHERE p.task_id=` + task + ` AND p.seq>` + cursor + `
  AND CASE WHEN p.author_kind='member' AND p.author_role=` + role + ` THEN p.reject_reason IS NOT NULL
  ELSE p.reject_reason IS NULL AND (cardinality(p.to_roles)=0 OR ` + role + `=ANY(p.to_roles)) END)`
}
