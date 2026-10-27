package repository

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

var taskNotificationStates = map[string]string{
	"awaiting_review": "ready for review",
	"completed":       "completed",
	"failed":          "failed",
	"blocked":         "blocked",
}

// notifyTaskTelegram queues a Telegram notice for the task's creator in the same
// transaction as the transition, when they have linked Telegram on an enabled
// bot. The delivery worker re-checks the allowlist, membership, binding and the
// issue conversation's scope before sending, as for any reply.
func notifyTaskTelegram(ctx context.Context, tx *sqlx.Tx, task domain.AgentTask) error {
	label, notify := taskNotificationStates[task.State]
	if !notify || task.ConversationID == nil {
		return nil
	}
	var linked bool
	if err := tx.GetContext(ctx, &linked, `SELECT EXISTS(SELECT 1 FROM telegram_links l JOIN telegram_settings s ON s.workspace_id=l.workspace_id WHERE l.workspace_id=$1 AND l.user_id=$2 AND s.enabled)`, task.WorkspaceID, task.CreatorID); err != nil || !linked {
		return err
	}
	var detail struct {
		Identifier  string  `db:"identifier_text"`
		Title       string  `db:"title"`
		PullRequest *string `db:"pull_request_url"`
	}
	// The pull request link is shown only once its publication was recorded.
	if err := tx.GetContext(ctx, &detail, `SELECT i.identifier_text,i.title,(SELECT r.pull_request_url FROM dev_machine_agent_runs r JOIN dev_machine_agent_run_steps s ON s.agent_run_id=r.id AND s.sequence=1 AND s.step_type='publication' AND s.status='succeeded' WHERE r.id=$2) AS pull_request_url FROM issues i WHERE i.id=$1`, task.IssueID, task.RunID); err != nil {
		return err
	}
	text := fmt.Sprintf("%s · %s: coding task %s.\n%s", detail.Identifier, detail.Title, label, task.Reason)
	if detail.PullRequest != nil && *detail.PullRequest != "" {
		text += "\nPull request: " + *detail.PullRequest
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO telegram_deliveries(workspace_id,user_id,telegram_user_id,conversation_id,text)
 SELECT l.workspace_id,l.user_id,l.telegram_user_id,$3,$4 FROM telegram_links l WHERE l.workspace_id=$1 AND l.user_id=$2`, task.WorkspaceID, task.CreatorID, task.ConversationID, text)
	return err
}
