package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// ErrWatchedTaskNotFound means the task to watch is not in the workspace.
var ErrWatchedTaskNotFound = errors.New("watched task not found")

// WatchTask registers or replaces the one-shot watch of one user, one
// conversation and one task (KUA-165), unless the task already reached what
// the watch waits for. It returns the task's state and whether a watch was
// registered. until is one of awaiting_review, completed, failed, or empty
// for any end state (completed, failed or cancelled). A task waiting in
// awaiting_review reached until=awaiting_review only when it waits for a
// person: a workflow step that waits for an agent review or CI does not.
//
// The state is read under a share lock on the task row, which every
// transition updates before it marks watches due, so a transition either
// shows in that state or sees the new watch: none falls in between.
func (r *AgentTaskRepository) WatchTask(ctx context.Context, ws, user, task, conversation, parent uuid.UUID, until string) (string, bool, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback()
	var current struct {
		State      string `db:"state"`
		PersonWait bool   `db:"person_wait"`
	}
	err = tx.GetContext(ctx, &current, `SELECT state,`+taskWaitsForPerson+` AS person_wait FROM agent_tasks t WHERE t.id=$1 AND t.workspace_id=$2 FOR SHARE`, task, ws)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, ErrWatchedTaskNotFound
	}
	if err != nil {
		return "", false, err
	}
	if watchReached(until, current.State, current.PersonWait) {
		return current.State, false, nil
	}
	var untilState any
	if until != "" {
		untilState = until
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO agent_task_watchers(workspace_id,user_id,task_id,conversation_id,parent_message_id,until_state)
VALUES($1,$2,$3,$4,$5,$6)
ON CONFLICT (task_id,user_id,conversation_id) DO UPDATE SET
 parent_message_id=EXCLUDED.parent_message_id,until_state=EXCLUDED.until_state,due_state=NULL,notified=false,created_at=now()`,
		ws, user, task, conversation, parent, untilState); err != nil {
		return "", false, err
	}
	return current.State, true, tx.Commit()
}

// taskWaitsForPerson is true for a task row t that waits in awaiting_review
// for a person: a workflow task whose step holds the time budget for one (an
// approval or a merge), or any other task, which enters the state only for
// its review. It matches the creator's review notice (markWorkflowReviewNotice).
const taskWaitsForPerson = `(t.state='awaiting_review' AND (t.workflow_version_id IS NULL OR t.kind='review' OR t.workflow_held_since IS NOT NULL))`

// watchReached reports whether a task in state already reached what a watch
// for until waits for, so registering it would never fire.
func watchReached(until, state string, personWait bool) bool {
	if until == "" {
		return domain.IsAgentTaskEndState(state)
	}
	if until == "awaiting_review" {
		return personWait
	}
	return state == until
}

// markTaskWatchersDue marks the task's watches that wait for state as due, in
// the transaction that records the event, once per watch: a transition into
// an end state, or a wait in awaiting_review that needs a person. notified is
// whether the creator's notice for this event is pending, in which case it
// already reaches a watcher who is the creator.
func markTaskWatchersDue(ctx context.Context, tx *sqlx.Tx, task domain.AgentTask, state string, notified bool) error {
	_, err := tx.ExecContext(ctx, `UPDATE agent_task_watchers SET due_state=$2,notified=($4::boolean AND user_id=$3)
 WHERE task_id=$1 AND due_state IS NULL AND (until_state=$2 OR (until_state IS NULL AND $2 IN ('completed','failed','cancelled')))`,
		task.ID, state, task.CreatorID, notified)
	return err
}

// ClaimTaskWatchers deletes and returns up to limit due watches, oldest
// first. Deleting while claiming makes each watch one-shot, also with several
// workers; a delivery that then fails is not repeated.
func (r *AgentTaskRepository) ClaimTaskWatchers(ctx context.Context, limit int) ([]domain.AgentTaskWatch, error) {
	watches := []domain.AgentTaskWatch{}
	err := r.db.SelectContext(ctx, &watches, `WITH claimed AS (
 DELETE FROM agent_task_watchers WHERE id IN (
  SELECT id FROM agent_task_watchers WHERE due_state IS NOT NULL ORDER BY created_at,id FOR UPDATE SKIP LOCKED LIMIT $1)
 RETURNING id,task_id,workspace_id,user_id,conversation_id,parent_message_id,until_state,due_state,notified,created_at)
SELECT c.task_id,c.workspace_id,c.user_id,c.conversation_id,c.parent_message_id,COALESCE(c.until_state,'') AS until_state,c.due_state AS state,c.notified,t.kind,t.issue_id,i.identifier_text AS issue_identifier,i.title AS issue_title
FROM claimed c
JOIN agent_tasks t ON t.id=c.task_id
JOIN issues i ON i.id=t.issue_id AND i.workspace_id=t.workspace_id
ORDER BY c.created_at,c.id`, limit)
	return watches, err
}
