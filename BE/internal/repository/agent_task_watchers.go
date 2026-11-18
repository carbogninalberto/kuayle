package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// WatchTask registers or replaces the one-shot watch of one user, one
// conversation and one task (KUA-165). from_sequence is the task's last
// recorded transition, so the watch fires only on transitions after
// registration, never for a state the task already had. until is one of
// awaiting_review, completed, failed, or empty for any end state (completed,
// failed or cancelled). The task must belong to the workspace.
func (r *AgentTaskRepository) WatchTask(ctx context.Context, ws, user, task, conversation, parent uuid.UUID, until string) error {
	var untilState any
	if until != "" {
		untilState = until
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO agent_task_watchers(workspace_id,user_id,task_id,conversation_id,parent_message_id,until_state,from_sequence)
SELECT $1,$2,$3,$4,$5,$6,COALESCE((SELECT MAX(sequence) FROM agent_task_transitions WHERE task_id=$3),0)
WHERE EXISTS(SELECT 1 FROM agent_tasks t WHERE t.id=$3 AND t.workspace_id=$1)
ON CONFLICT (task_id,user_id,conversation_id) DO UPDATE SET
 parent_message_id=EXCLUDED.parent_message_id,until_state=EXCLUDED.until_state,from_sequence=EXCLUDED.from_sequence,created_at=now()`,
		ws, user, task, conversation, parent, untilState)
	return err
}

// ClaimTaskWatchers deletes and returns up to limit watches whose task
// recorded a matching transition after registration, oldest first. Deleting
// while claiming makes each watch one-shot, also with several workers; a
// delivery that then fails is not repeated. A watch without a chosen state
// fires on the first end state (completed, failed or cancelled).
func (r *AgentTaskRepository) ClaimTaskWatchers(ctx context.Context, limit int) ([]domain.AgentTaskWatch, error) {
	watches := []domain.AgentTaskWatch{}
	err := r.db.SelectContext(ctx, &watches, `WITH matched AS (
 SELECT w.id,w.task_id,w.workspace_id,w.user_id,w.conversation_id,w.parent_message_id,COALESCE(w.until_state,'') AS until_state,w.created_at,h.state
 FROM agent_task_watchers w
 JOIN LATERAL (SELECT g.to_state AS state FROM agent_task_transitions g
  WHERE g.task_id=w.task_id AND g.sequence>w.from_sequence
   AND ((w.until_state IS NOT NULL AND g.to_state=w.until_state)
    OR (w.until_state IS NULL AND g.to_state IN ('completed','failed','cancelled')))
  ORDER BY g.sequence LIMIT 1) h ON true
 ORDER BY w.created_at,w.id FOR UPDATE OF w SKIP LOCKED LIMIT $1
), claimed AS (
 DELETE FROM agent_task_watchers w USING matched m WHERE w.id=m.id RETURNING w.id
)
SELECT m.task_id,m.workspace_id,m.user_id,m.conversation_id,m.parent_message_id,m.until_state,m.state,t.issue_id,i.identifier_text AS issue_identifier,i.title AS issue_title
FROM claimed c JOIN matched m ON m.id=c.id
JOIN agent_tasks t ON t.id=m.task_id
JOIN issues i ON i.id=t.issue_id AND i.workspace_id=t.workspace_id
ORDER BY m.created_at,m.id`, limit)
	return watches, err
}
