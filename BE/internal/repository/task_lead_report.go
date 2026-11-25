package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/lib/pq"
)

// recordLeadReport runs in the transaction that accepted the room post, after
// room deduplication. Only a visible, enabled inbox notice can queue Telegram.
func recordLeadReport(ctx context.Context, tx *sqlx.Tx, task domain.AgentTask, post domain.TaskMessage) error {
	_, err := tx.ExecContext(ctx, `WITH candidate AS (
 SELECT $1::uuid AS user_id,i.workspace_id,i.id AS issue_id,ARRAY[i.team_id] AS source_team_ids,'issue_updated'::text AS type,i.identifier_text
 FROM issues i LEFT JOIN user_preferences p ON p.user_id=$1
 WHERE i.workspace_id=$2 AND i.id=$3 AND (p.notification_events->'issue_updated') IS DISTINCT FROM 'false'::jsonb
), stored AS (
 INSERT INTO notifications(id,user_id,workspace_id,issue_id,source_team_ids,type,title)
 SELECT $4,n.user_id,n.workspace_id,n.issue_id,n.source_team_ids,n.type,'Lead report on '||n.identifier_text||': '||$5
 FROM candidate n WHERE `+notificationVisible(ctx, "n")+`
 RETURNING user_id,workspace_id
)
 INSERT INTO telegram_deliveries(workspace_id,user_id,telegram_user_id,conversation_id,text)
 SELECT n.workspace_id,n.user_id,l.telegram_user_id,c.id,'Lead report on '||i.identifier_text||E':\n'||$7
 FROM stored n JOIN telegram_links l USING(workspace_id,user_id)
 JOIN telegram_settings s ON s.workspace_id=n.workspace_id AND s.enabled
 JOIN conversations c ON c.id=$6 AND c.workspace_id=n.workspace_id AND c.creator_id=n.user_id
 JOIN issues i ON i.id=$3 AND i.workspace_id=n.workspace_id`,
		task.CreatorID, task.WorkspaceID, task.IssueID, post.ID, post.Body, task.ConversationID, telegramAgentExcerpt(post.Body))
	return err
}

// LeadReportNotices returns the creator's still-visible inbox rows for the
// room posts the tick read. A muted report has no row.
func (r *AgentTaskRepository) LeadReportNotices(ctx context.Context, task domain.AgentTask, ids []uuid.UUID) ([]domain.Notification, error) {
	var notices []domain.Notification
	err := r.db.SelectContext(ctx, &notices, `SELECT `+notifSelectCols+` FROM notifications n
 JOIN issues i ON i.id=n.issue_id AND i.workspace_id=n.workspace_id
 WHERE n.workspace_id=$1 AND n.user_id=$2 AND n.issue_id=$3 AND n.id=ANY($4)
 AND `+notificationVisible(ctx, "n")+` ORDER BY array_position($4::uuid[], n.id)`,
		task.WorkspaceID, task.CreatorID, task.IssueID, pq.Array(ids))
	return notices, err
}
