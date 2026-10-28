package repository

import (
	"context"

	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// syncAgentSession is the single place a task's agent session changes. Every
// task transition calls it in the transition's own transaction, so a session
// never disagrees with its task. The task's creation opens the session; the
// current attempt supplies the machine and the harness's reported session ID.
func syncAgentSession(ctx context.Context, tx *sqlx.Tx, task domain.AgentTask) error {
	purpose := task.Kind
	if purpose == "" {
		purpose = domain.AgentTaskKindImplement
	}
	state, changes := domain.AgentSessionState(purpose, task.Generation, task.State)
	_, err := tx.ExecContext(ctx, `INSERT INTO agent_sessions(workspace_id,task_id,machine_id,provider,purpose,state)
 VALUES($1,$2,$3,$4,$5,CASE WHEN $6 THEN $7 ELSE 'created' END)
 ON CONFLICT (task_id) DO UPDATE SET
  state=CASE WHEN $6 THEN $7 ELSE agent_sessions.state END,
  machine_id=COALESCE(EXCLUDED.machine_id,agent_sessions.machine_id),
  native_session_id=COALESCE((SELECT r.native_session_id FROM dev_machine_agent_runs r WHERE r.id=$8 AND r.workspace_id=$1 AND r.task_id=$2),agent_sessions.native_session_id),
  started_at=CASE WHEN $6 AND $7='active' THEN COALESCE(agent_sessions.started_at,now()) ELSE agent_sessions.started_at END,
  ended_at=CASE WHEN NOT $6 THEN agent_sessions.ended_at WHEN $7 IN ('completed','failed','cancelled') THEN now() ELSE NULL END,
  updated_at=now()
 WHERE agent_sessions.workspace_id=$1`, task.WorkspaceID, task.ID, task.MachineID, task.Provider, purpose, changes, state, task.RunID)
	return err
}
