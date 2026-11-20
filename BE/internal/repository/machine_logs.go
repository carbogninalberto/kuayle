package repository

import (
	"context"

	"github.com/google/uuid"
)

type MachineLogChunk struct {
	Channel      string
	Content      string
	Sequence     int64
	PartialStart bool
	Truncated    bool
}

// RecentAgentLogs reads a bounded window, newest first. A task's records
// require the same conversation and issue scope, even for direct machine reads.
func (r *DevMachineRepository) RecentAgentLogs(ctx context.Context, ws, machine, user uuid.UUID, task *uuid.UUID) ([]MachineLogChunk, bool, error) {
	const maxChunks, maxBytes = 256, 256 << 10
	rows, err := r.db.QueryxContext(ctx, `SELECT concat_ws(':',r.id,l.service_id,l.stream) AS channel,
		l.sequence,l.content,l.truncated
		FROM dev_machine_log_chunks l
		JOIN dev_machines m ON m.id=l.machine_id AND m.workspace_id=l.workspace_id
		JOIN workspace_members wm ON wm.workspace_id=m.workspace_id AND wm.user_id=m.created_by_user_id
		JOIN dev_machine_agent_runs r ON r.id=l.agent_run_id AND r.machine_id=m.id AND r.workspace_id=m.workspace_id
		WHERE m.workspace_id=$1 AND m.id=$2 AND m.created_by_user_id=$3 AND wm.role IN ('owner','admin','member')
		  AND ($4::uuid IS NULL OR r.task_id=$4)
		  AND (r.task_id IS NULL OR EXISTS (
		    SELECT 1 FROM agent_tasks t JOIN conversations c ON c.id=t.conversation_id AND c.workspace_id=t.workspace_id
		    WHERE t.id=r.task_id AND t.workspace_id=m.workspace_id AND t.creator_id=$3 AND c.creator_id=$3
		      AND t.issue_id=c.issue_id AND t.team_id=c.team_id AND `+conversationScopePredicate("c")+`))
		ORDER BY l.id DESC LIMIT $5`, ws, machine, user, task, maxChunks+1)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	var chunks []MachineLogChunk
	remaining := maxBytes
	for rows.Next() {
		if len(chunks) == maxChunks || remaining == 0 {
			return chunks, true, nil
		}
		var chunk MachineLogChunk
		if err := rows.StructScan(&chunk); err != nil {
			return nil, false, err
		}
		if len(chunk.Content) > remaining {
			chunk.Content = chunk.Content[len(chunk.Content)-remaining:]
			// This record may begin in the middle of a credential.
			chunk.PartialStart = true
			return append(chunks, chunk), true, nil
		}
		remaining -= len(chunk.Content)
		chunks = append(chunks, chunk)
	}
	return chunks, false, rows.Err()
}
