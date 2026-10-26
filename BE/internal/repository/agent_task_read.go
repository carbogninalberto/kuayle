package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

func (r *AgentTaskRepository) Conversation(ctx context.Context, ws, id uuid.UUID) (*domain.Conversation, error) {
	tx, c, err := NewConversationRepository(r.db).authorized(ctx, ws, id)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	return c, nil
}

func (r *AgentTaskRepository) Page(ctx context.Context, ws, conversation uuid.UUID, before *uuid.UUID) (*domain.AgentTaskPage, error) {
	tx, _, err := NewConversationRepository(r.db).authorized(ctx, ws, conversation)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if before != nil {
		var exists bool
		if err = tx.GetContext(ctx, &exists, `SELECT EXISTS(SELECT 1 FROM agent_tasks WHERE id=$1 AND conversation_id=$2)`, before, conversation); err != nil {
			return nil, err
		}
		if !exists {
			return nil, ErrConversationNotFound
		}
	}
	result := &domain.AgentTaskPage{Tasks: []domain.AgentTask{}}
	err = tx.SelectContext(ctx, &result.Tasks, `SELECT * FROM agent_tasks WHERE conversation_id=$1 AND ($2::uuid IS NULL OR (created_at,id)<(SELECT created_at,id FROM agent_tasks WHERE id=$2 AND conversation_id=$1)) ORDER BY created_at DESC,id DESC LIMIT 26`, conversation, before)
	if err != nil {
		return nil, err
	}
	if len(result.Tasks) > 25 {
		result.Tasks = result.Tasks[:25]
		id := result.Tasks[24].ID
		result.NextBefore = &id
	}
	return result, nil
}

func (r *AgentTaskRepository) History(ctx context.Context, ws, id uuid.UUID, after int64) (*domain.AgentTaskHistory, error) {
	if after < 0 {
		return nil, ErrConversationInvalid
	}
	if _, err := conversationActor(ctx, ws); err != nil {
		return nil, err
	}
	tx, _, err := r.transaction(ctx, id, true)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	result := &domain.AgentTaskHistory{Transitions: []domain.AgentTaskTransition{}}
	err = tx.SelectContext(ctx, &result.Transitions, `SELECT * FROM agent_task_transitions WHERE task_id=$1 AND sequence>$2 ORDER BY sequence LIMIT 101`, id, after)
	if err != nil {
		return nil, err
	}
	if len(result.Transitions) > 100 {
		result.Transitions = result.Transitions[:100]
		next := result.Transitions[99].Sequence
		result.NextAfter = &next
	}
	return result, nil
}

func (r *AgentTaskRepository) Runs(ctx context.Context, ws, id uuid.UUID, before *uuid.UUID) (*domain.AgentTaskRuns, error) {
	if _, err := conversationActor(ctx, ws); err != nil {
		return nil, err
	}
	tx, _, err := r.transaction(ctx, id, true)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if before != nil {
		var exists bool
		if err = tx.GetContext(ctx, &exists, `SELECT EXISTS(SELECT 1 FROM dev_machine_agent_runs WHERE id=$1 AND task_id=$2 AND workspace_id=$3)`, before, id, ws); err != nil {
			return nil, err
		}
		if !exists {
			return nil, ErrConversationNotFound
		}
	}
	result := &domain.AgentTaskRuns{Runs: []domain.DevMachineAgentRun{}}
	err = tx.SelectContext(ctx, &result.Runs, `SELECT * FROM dev_machine_agent_runs WHERE task_id=$1 AND workspace_id=$2 AND ($3::uuid IS NULL OR (created_at,id)<(SELECT created_at,id FROM dev_machine_agent_runs WHERE id=$3 AND task_id=$1)) ORDER BY created_at DESC,id DESC LIMIT 26`, id, ws, before)
	if err != nil {
		return nil, err
	}
	if len(result.Runs) > 25 {
		result.Runs = result.Runs[:25]
		next := result.Runs[24].ID
		result.NextBefore = &next
	}
	return result, nil
}

func (r *AgentTaskRepository) Logs(ctx context.Context, ws, id, run uuid.UUID, after int64) (*domain.AgentTaskLogs, error) {
	if after < 0 {
		return nil, ErrConversationInvalid
	}
	if _, err := conversationActor(ctx, ws); err != nil {
		return nil, err
	}
	tx, _, err := r.transaction(ctx, id, true)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var owned uuid.UUID
	err = tx.GetContext(ctx, &owned, `SELECT id FROM dev_machine_agent_runs WHERE id=$1 AND task_id=$2 AND workspace_id=$3`, run, id, ws)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrConversationNotFound
	}
	if err != nil {
		return nil, err
	}
	result := &domain.AgentTaskLogs{Logs: []domain.DevMachineLogChunk{}}
	err = tx.SelectContext(ctx, &result.Logs, `SELECT * FROM dev_machine_log_chunks WHERE agent_run_id=$1 AND workspace_id=$2 AND id>$3 ORDER BY id LIMIT 101`, run, ws, after)
	if err != nil {
		return nil, err
	}
	if len(result.Logs) > 100 {
		result.Logs = result.Logs[:100]
		next := result.Logs[99].ID
		result.NextAfter = &next
	}
	return result, nil
}
