package service

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/google/uuid"
)

const assistantMaxListedTasks = 50

var agentTaskStates = []string{"queued", "preparing", "running", "waiting_input", "validating", "publishing", "awaiting_review", "blocked", "completed", "failed", "cancelling", "cancelled"}

// listAgentWork reports the workspace's recent coding tasks on issues the user
// can see. The repository scopes the query to the caller's access in ctx.
func (t *AssistantTools) listAgentWork(ctx context.Context, ws uuid.UUID, arguments string) (any, *uuid.UUID, error) {
	var input struct {
		State string `json:"state"`
		Limit *int   `json:"limit"`
	}
	if err := decodeToolInput(arguments, &input); err != nil {
		return nil, nil, err
	}
	if input.State != "" && !slices.Contains(agentTaskStates, input.State) {
		return nil, nil, toolFailure("invalid state")
	}
	limit := assistantMaxListedTasks
	if input.Limit != nil {
		if *input.Limit < 1 || *input.Limit > assistantMaxListedTasks {
			return nil, nil, toolFailure("limit must be 1 to %d", assistantMaxListedTasks)
		}
		limit = *input.Limit
	}
	work, err := t.work.RecentAgentWork(ctx, ws, input.State, limit)
	if err != nil {
		return nil, nil, err
	}
	tasks := make([]map[string]any, 0, len(work))
	for _, item := range work {
		task := map[string]any{
			"identifier": item.Identifier, "title": truncateRunes(item.Title, 120), "task_id": item.TaskID,
			"state": item.State, "reason": truncateRunes(item.Reason, 160), "agent": item.Provider,
			"attempt": item.Attempt, "updated_at": item.UpdatedAt,
		}
		if item.PullRequestURL != nil {
			task["pull_request_url"] = *item.PullRequestURL
		}
		if item.Review != nil {
			task["review"] = *item.Review
		}
		if item.AgentReview != nil {
			task["agent_review"] = *item.AgentReview
		}
		tasks = append(tasks, task)
	}
	// Drop the oldest entries rather than failing when the list is too large.
	for truncated := false; ; truncated = true {
		result := map[string]any{"tasks": tasks}
		if truncated {
			result["truncated"] = true
		}
		data, err := json.Marshal(result)
		if err != nil {
			return nil, nil, err
		}
		if len(data) <= assistantToolOutputBytes || len(tasks) == 0 {
			return result, nil, nil
		}
		tasks = tasks[:len(tasks)-1]
	}
}
