package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

const agentTaskAttachmentColumns = `task_id,run_id,position,asset_id,storage_key,path,content_type,size`

// IssueImages lists raster images embedded in the task issue's description and
// comments that the task creator can read, newest first.
func (r *AgentTaskRepository) IssueImages(ctx context.Context, task domain.AgentTask, limit int) ([]domain.Asset, error) {
	assets := []domain.Asset{}
	err := r.db.SelectContext(ctx, &assets, `SELECT a.* FROM assets a
		WHERE a.workspace_id=$2 AND a.content_type IN ('image/png','image/jpeg','image/webp','image/gif')
		AND a.id IN (SELECT ref.asset_id FROM private_asset_reference_index ref JOIN issues i ON i.id=ref.owner_id
			WHERE ref.owner_table='issues' AND ref.owner_id=$1 AND ref.source_table IN ('issues','comments') AND i.workspace_id=$2)
		AND ((a.team_id IS NULL AND NOT EXISTS(SELECT 1 FROM workspace_privacy p WHERE p.workspace_id=a.workspace_id))
			OR (a.team_id IS NOT NULL AND kuayle_team_access(a.team_id,$3,a.workspace_id)))
		ORDER BY a.created_at DESC,a.id LIMIT $4`, task.IssueID, task.WorkspaceID, task.CreatorID, limit)
	return assets, err
}

// RunAttachments returns the images recorded for the task's current attempt.
func (r *AgentTaskRepository) RunAttachments(ctx context.Context, task domain.AgentTask) ([]domain.AgentTaskAttachment, error) {
	items := []domain.AgentTaskAttachment{}
	err := r.db.SelectContext(ctx, &items, `SELECT `+agentTaskAttachmentColumns+` FROM agent_task_attachments WHERE run_id=$1 AND task_id=$2 ORDER BY position`, task.RunID, task.ID)
	return items, err
}

// RecordRunAttachments fixes the attempt's images once; a concurrent or
// repeated call keeps the first list.
func (r *AgentTaskRepository) RecordRunAttachments(ctx context.Context, task domain.AgentTask, items []domain.AgentTaskAttachment) ([]domain.AgentTaskAttachment, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT 1 FROM agent_tasks WHERE id=$1 FOR UPDATE`, task.ID); err != nil {
		return nil, err
	}
	var exists bool
	if err = tx.GetContext(ctx, &exists, `SELECT EXISTS(SELECT 1 FROM agent_task_attachments WHERE run_id=$1)`, task.RunID); err != nil {
		return nil, err
	}
	if !exists {
		for _, item := range items {
			if _, err = tx.ExecContext(ctx, `INSERT INTO agent_task_attachments(`+agentTaskAttachmentColumns+`) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`,
				task.ID, task.RunID, item.Position, item.AssetID, item.StorageKey, item.Path, item.ContentType, item.Size); err != nil {
				return nil, err
			}
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return r.RunAttachments(ctx, task)
}

// ListRunAttachments is the manager's read of the images to copy for a run.
func (r *DevMachineRepository) ListRunAttachments(ctx context.Context, run uuid.UUID) ([]domain.AgentTaskAttachment, error) {
	items := []domain.AgentTaskAttachment{}
	err := r.db.SelectContext(ctx, &items, `SELECT `+agentTaskAttachmentColumns+` FROM agent_task_attachments WHERE run_id=$1 ORDER BY position LIMIT 10`, run)
	return items, err
}
