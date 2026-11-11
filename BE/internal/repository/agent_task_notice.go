package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// ClaimTaskNotices takes up to limit transitions whose creator is still to be
// notified, oldest first. A transition is claimed once, also with several
// workers: claiming clears its pending mark, so a notification that then
// fails is not repeated.
func (r *AgentTaskRepository) ClaimTaskNotices(ctx context.Context, limit int) ([]domain.AgentTaskNotice, error) {
	notices := []domain.AgentTaskNotice{}
	err := r.db.SelectContext(ctx, &notices, `WITH claimed AS (
 UPDATE agent_task_transitions SET notification_pending=false WHERE sequence IN (
  SELECT sequence FROM agent_task_transitions WHERE notification_pending ORDER BY sequence FOR UPDATE SKIP LOCKED LIMIT $1)
 RETURNING sequence,task_id,to_state)
 SELECT t.id AS task_id,t.workspace_id,t.creator_id,t.issue_id,t.kind,c.to_state AS state,i.identifier_text AS issue_identifier,i.title AS issue_title
 FROM claimed c JOIN agent_tasks t ON t.id=c.task_id JOIN issues i ON i.id=t.issue_id AND i.workspace_id=t.workspace_id
 ORDER BY c.sequence`, limit)
	return notices, err
}

// taskTransitionNotifies reports whether entering the task's state is worth
// telling its creator: the task finished, failed or waits for review, and the
// creator did not make the change themselves. recordAgentTaskTransition also
// skips a workflow's wait for an agent review, which needs nobody.
func taskTransitionNotifies(task domain.AgentTask, actor *uuid.UUID) bool {
	if domain.AgentTaskNotificationType(task.State) == "" || task.CreatorID == uuid.Nil {
		return false
	}
	return actor == nil || *actor != task.CreatorID
}
