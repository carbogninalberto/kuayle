package repository

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// TaskFileSource tells the task's creator where its files are read
// (KUA-280), under the task's access rules (lockAgentTask): the repository
// and head Kuayle published, by the board's rule (GitHub's head observed
// after the publication, otherwise the published commit; no head before a
// publication), the files the newest attempt that recorded any changed, and
// whether ref is a commit Kuayle published or any attempt of the task
// recorded.
func (r *AgentTaskRepository) TaskFileSource(ctx context.Context, ws, id uuid.UUID, ref string) (*domain.TaskFileSource, error) {
	if _, err := conversationActor(ctx, ws); err != nil {
		return nil, err
	}
	tx, _, err := r.transaction(ctx, id, true)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var row struct {
		Repository   string          `db:"repository_full_name"`
		Head         string          `db:"head_sha"`
		Changed      json.RawMessage `db:"changed_files"`
		RefPublished bool            `db:"ref_published"`
		RefRecorded  bool            `db:"ref_recorded"`
	}
	err = tx.GetContext(ctx, &row, `SELECT COALESCE(p.repository_full_name,k.repository_full_name,'') AS repository_full_name,COALESCE(head.sha,'') AS head_sha,
 COALESCE((SELECT r.changed_files FROM dev_machine_agent_runs r WHERE r.task_id=t.id AND r.workspace_id=t.workspace_id AND jsonb_typeof(r.changed_files)='array' AND r.changed_files<>'[]'::jsonb ORDER BY r.created_at DESC,r.id DESC LIMIT 1),'[]') AS changed_files,
 EXISTS(SELECT 1 FROM dev_machine_git_refs g JOIN dev_machine_agent_runs r ON r.id=g.agent_run_id AND r.workspace_id=g.workspace_id
  WHERE r.task_id=t.id AND r.workspace_id=t.workspace_id AND g.ref_type='commit' AND g.source='published' AND g.commit_sha=$3) AS ref_published,
 EXISTS(SELECT 1 FROM dev_machine_git_refs g JOIN dev_machine_agent_runs r ON r.id=g.agent_run_id AND r.workspace_id=g.workspace_id
  WHERE r.task_id=t.id AND r.workspace_id=t.workspace_id AND g.ref_type='commit' AND g.commit_sha=$3) AS ref_recorded
 FROM agent_tasks t`+boardJoins+` WHERE t.id=$1 AND t.workspace_id=$2`, id, ws, ref)
	if err != nil {
		return nil, err
	}
	source := &domain.TaskFileSource{Repository: row.Repository, Head: row.Head, RefPublished: row.RefPublished, RefRecorded: row.RefRecorded}
	_ = json.Unmarshal(row.Changed, &source.Changed)
	return source, nil
}
