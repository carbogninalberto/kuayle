package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/lib/pq"
)

// TraceLimits bounds how many rows of each source the debug trace reads. The
// repository returns at most one row more than the limit, so the service can
// say that more rows exist without reading them.
type TraceLimits struct {
	Rows        int
	Transitions int
}

// TraceRunPrompt is one attempt's prompt, split by source.
type TraceRunPrompt struct {
	RunID   uuid.UUID             `db:"run_id"`
	Sources []domain.PromptSource `db:"-"`
}

// TraceRetry is one retry request as the trace shows it.
type TraceRetry struct {
	TaskID             uuid.UUID  `db:"task_id"`
	RequestID          uuid.UUID  `db:"request_id"`
	ExpectedGeneration int64      `db:"expected_generation"`
	Prompt             string     `db:"prompt"`
	Model              string     `db:"model"`
	ModelServerID      *uuid.UUID `db:"model_server_id"`
	CreatedAt          time.Time  `db:"created_at"`
}

// TraceAuditEvent is one pause, resume or version move of the task's workflow.
type TraceAuditEvent struct {
	ID        int64           `db:"id"`
	Action    string          `db:"action"`
	ActorID   *uuid.UUID      `db:"actor_id"`
	Detail    json.RawMessage `db:"detail"`
	CreatedAt time.Time       `db:"created_at"`
}

// TaskTraceRows is the raw material of a task's debug trace, read under the
// task's current access rules. Each slice is oldest first and may contain one
// row more than the caller's limit; the service trims it and records the cut.
type TaskTraceRows struct {
	Task             domain.AgentTask
	Identifier       string
	Title            string
	Transitions      []domain.AgentTaskTransition
	Steps            []domain.WorkflowStep
	Runs             []domain.DevMachineAgentRun
	Prompts          []TraceRunPrompt
	RunSteps         []domain.DevMachineAgentRunStep
	Operations       []domain.DevMachineOperation
	Retries          []TraceRetry
	LiveMessages     []domain.AgentLiveMessage
	AgentMessages    []domain.AgentMessage
	Reviews          []domain.AgentReview
	Signals          []domain.AgentTaskSignal
	Audits           []TraceAuditEvent
	NotebookChanges  []domain.NotebookChange
	Notebook         *domain.Notebook
	NotebookRevision int
}

// DebugTrace reads everything the debug trace documents about a task: its
// transitions, workflow steps, attempts (including the attempts of its review
// tasks), prompts, run steps, machine operations, retries, messages, reviews,
// CI signals, pauses and version moves, and notebook. It authorizes with the
// same decision rules as a person's workflow decision: the task's creator, or
// a workspace owner/admin who can see the issue, including private teams.
// Everyone else gets ErrConversationNotFound. The read never commits.
func (r *AgentTaskRepository) DebugTrace(ctx context.Context, ws, id uuid.UUID, limits TraceLimits) (*TaskTraceRows, error) {
	if _, err := conversationActor(ctx, ws); err != nil {
		return nil, err
	}
	tx, task, err := r.decisionTransaction(ctx, ws, id)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if task.WorkspaceID != ws {
		return nil, ErrConversationNotFound
	}
	if limits.Rows <= 0 {
		limits.Rows = 500
	}
	if limits.Transitions <= 0 {
		limits.Transitions = 2000
	}
	out := &TaskTraceRows{Task: *task, Transitions: []domain.AgentTaskTransition{}, Steps: []domain.WorkflowStep{}, Runs: []domain.DevMachineAgentRun{},
		Prompts: []TraceRunPrompt{}, RunSteps: []domain.DevMachineAgentRunStep{}, Operations: []domain.DevMachineOperation{}, Retries: []TraceRetry{},
		LiveMessages: []domain.AgentLiveMessage{}, AgentMessages: []domain.AgentMessage{}, Reviews: []domain.AgentReview{}, Signals: []domain.AgentTaskSignal{},
		Audits: []TraceAuditEvent{}, NotebookChanges: []domain.NotebookChange{}}

	// The issue gives the document its human name. Its absence is not a reason
	// to fail the trace: the task row still says everything else.
	err = tx.GetContext(ctx, &out.Identifier, `SELECT identifier_text FROM issues WHERE id=$1 AND workspace_id=$2`, task.IssueID, ws)
	if errors.Is(err, sql.ErrNoRows) {
		out.Identifier, err = "", nil
	}
	if err != nil {
		return nil, err
	}
	_ = tx.GetContext(ctx, &out.Title, `SELECT title FROM issues WHERE id=$1 AND workspace_id=$2`, task.IssueID, ws)

	const runScope = `(SELECT r.id FROM dev_machine_agent_runs r WHERE r.workspace_id=$2 AND (r.task_id=$1 OR r.task_id IN (SELECT rv.task_id FROM agent_reviews rv WHERE rv.reviewed_task_id=$1)))`
	const runTaskScope = `(r.task_id=$1 OR r.task_id IN (SELECT rv.task_id FROM agent_reviews rv WHERE rv.reviewed_task_id=$1))`

	if err = tx.SelectContext(ctx, &out.Transitions, `SELECT sequence,task_id,from_state,to_state,actor_id,reason,created_at FROM agent_task_transitions WHERE task_id=$1 ORDER BY sequence LIMIT $2`, id, limits.Transitions+1); err != nil {
		return nil, err
	}
	if err = tx.SelectContext(ctx, &out.Steps, `SELECT task_id,sequence,node_id,node_type,visit,from_node,edge_id,condition,reason,generation,review_task_id,outcome,detail,plan,decided_by,entered_at,finished_at,paused_seconds
 FROM agent_task_workflow_steps WHERE task_id=$1 ORDER BY sequence LIMIT $2`, id, limits.Rows+1); err != nil {
		return nil, err
	}
	if err = tx.SelectContext(ctx, &out.Runs, `SELECT r.id,r.task_id,r.machine_id,r.workspace_id,r.issue_id,r.checkout_id,r.requested_by_user_id,r.provider_id,r.mode,r.status,r.model,r.thinking,r.model_server_id,
 r.summary,r.branch,r.pull_request_url,r.session_id,r.notebook_required,r.notebook_updates,r.notebook_unchanged,r.notebook_problem,
  r.closing_asked_at,r.closing_seconds,r.worktree_read_only,r.failure_class,r.failure_rule,r.failure_cause,r.environment,r.output_cuts,
  r.exit_code,r.error_message,r.created_at,r.started_at,r.completed_at,r.cancelled_at
 FROM dev_machine_agent_runs r WHERE r.workspace_id=$2 AND `+runTaskScope+` ORDER BY r.created_at,r.id LIMIT $3`, id, ws, limits.Rows+1); err != nil {
		return nil, err
	}
	var prompts []struct {
		RunID   uuid.UUID       `db:"run_id"`
		Sources domain.NullJSON `db:"sources"`
	}
	if err = tx.SelectContext(ctx, &prompts, `SELECT p.run_id,p.sources FROM dev_machine_agent_run_prompts p WHERE p.workspace_id=$2 AND p.run_id IN `+runScope+`
 ORDER BY p.created_at,p.run_id LIMIT $3`, id, ws, limits.Rows+1); err != nil {
		return nil, err
	}
	for _, prompt := range prompts {
		entry := TraceRunPrompt{RunID: prompt.RunID, Sources: []domain.PromptSource{}}
		if len(prompt.Sources) > 0 {
			if err = json.Unmarshal(prompt.Sources, &entry.Sources); err != nil {
				return nil, err
			}
		}
		out.Prompts = append(out.Prompts, entry)
	}
	if err = tx.SelectContext(ctx, &out.RunSteps, `SELECT s.id,s.agent_run_id,s.sequence,s.step_type,s.name,s.status,s.command_argv,s.summary,s.exit_code,s.started_at,s.completed_at,s.created_at,
 s.compose_file,s.command_source,s.commands,s.command_results,s.agent_summary
 FROM dev_machine_agent_run_steps s JOIN dev_machine_agent_runs r ON r.id=s.agent_run_id
 WHERE r.workspace_id=$2 AND `+runTaskScope+` ORDER BY s.created_at,s.id LIMIT $3`, id, ws, limits.Rows+1); err != nil {
		return nil, err
	}
	if err = tx.SelectContext(ctx, &out.Operations, `SELECT o.id,o.machine_id,o.agent_run_id,o.checkout_id,o.environment_id,o.terminal_session_id,o.agent_task_id,o.workspace_id,o.action,o.status,
 o.generation,o.idempotency_key,o.requested_by_user_id,o.attempts,o.max_attempts,o.error_code,o.error_message,o.available_at,o.created_at,o.updated_at,o.completed_at
 FROM dev_machine_operations o WHERE o.workspace_id=$2 AND (o.agent_task_id=$1 OR o.agent_run_id IN `+runScope+`) ORDER BY o.created_at,o.id LIMIT $3`, id, ws, limits.Rows+1); err != nil {
		return nil, err
	}
	if err = tx.SelectContext(ctx, &out.Retries, `SELECT task_id,request_id,expected_generation,prompt,model,model_server_id,created_at FROM agent_task_retries WHERE task_id=$1 ORDER BY created_at,request_id LIMIT $2`, id, limits.Rows+1); err != nil {
		return nil, err
	}
	if err = tx.SelectContext(ctx, &out.LiveMessages, `SELECT id,task_id,direction,seq,sender,author_id,kind,body,state,delivered_via,run_id,reject_reason,created_at,delivered_at,reply_to,deliveries,workflow_node
 FROM agent_live_messages WHERE task_id=$1 AND workspace_id=$2 ORDER BY created_at,id LIMIT $3`, id, ws, limits.Rows+1); err != nil {
		return nil, err
	}
	if err = tx.SelectContext(ctx, &out.AgentMessages, `SELECT id,workspace_id,from_task_id,from_issue_id,from_run_id,to_ref,to_issue_id,to_task_id,channel,body,state,reject_reason,held_by,held_at,released_by,released_at,
 dropped_by,dropped_at,delivered_run_id,delivered_at,comment_state,comment_id,created_at,updated_at
 FROM agent_messages WHERE workspace_id=$2 AND (from_task_id=$1 OR to_task_id=$1) ORDER BY created_at,id LIMIT $3`, id, ws, limits.Rows+1); err != nil {
		return nil, err
	}
	if err = tx.SelectContext(ctx, &out.Reviews, `SELECT task_id,workspace_id,reviewed_task_id,requested_by,provider,commit_sha,repository_full_name,pull_request_number,pull_request_url,base_branch,focus,attempt,
 post_to_pull_request,verdict,findings,comment_state,comment_attempts,comment_lease_expires_at,completed_at,created_at,updated_at
 FROM agent_reviews WHERE workspace_id=$2 AND (task_id=$1 OR reviewed_task_id=$1) ORDER BY created_at,task_id LIMIT $3`, id, ws, limits.Rows+1); err != nil {
		return nil, err
	}
	if err = tx.SelectContext(ctx, &out.Signals, `SELECT id,task_id,run_id,kind,external_id,outcome,name,summary,url,action,failure_class,failure_evidence,request_id,follow_up_generation,created_at,updated_at
 FROM agent_task_signals WHERE task_id=$1 ORDER BY created_at,id LIMIT $2`, id, limits.Rows+1); err != nil {
		return nil, err
	}
	if err = tx.SelectContext(ctx, &out.Audits, `SELECT id,action,actor_id,detail,created_at FROM workflow_audit_events
 WHERE workspace_id=$1 AND entity='task_workflow' AND entity_id=$2 AND action IN ($3,$4,$5,$6,$7) ORDER BY id LIMIT $8`,
		ws, id, domain.WorkflowPauseActionRequested, domain.WorkflowPauseActionPaused, domain.WorkflowPauseActionResumed, domain.WorkflowPauseActionWithdrawn, domain.WorkflowSwitchAction, limits.Rows+1); err != nil {
		return nil, err
	}
	var changes []struct {
		domain.NotebookChange
		Sections pq.StringArray `db:"sections"`
	}
	if err = tx.SelectContext(ctx, &changes, `SELECT revision,author,user_id,run_id,generation,step_sequence,step_label,sections,summary,created_at
 FROM agent_task_notebook_changes WHERE task_id=$1 ORDER BY revision LIMIT $2`, id, limits.Rows+1); err != nil {
		return nil, err
	}
	for _, change := range changes {
		entry := change.NotebookChange
		entry.Sections = append([]string{}, change.Sections...)
		out.NotebookChanges = append(out.NotebookChanges, entry)
	}
	var notebook struct {
		Document json.RawMessage `db:"document"`
		Revision int             `db:"revision"`
	}
	err = tx.GetContext(ctx, &notebook, `SELECT document,revision FROM agent_task_notebooks WHERE task_id=$1 AND workspace_id=$2`, id, ws)
	if errors.Is(err, sql.ErrNoRows) {
		initial := initialNotebook(notebookTask{ID: task.ID, WorkspaceID: task.WorkspaceID, CreatorID: task.CreatorID, Prompt: task.Prompt, CreatedAt: task.CreatedAt})
		out.Notebook = &initial
	} else if err != nil {
		return nil, err
	} else {
		decoded, decodeErr := domain.DecodeNotebook(notebook.Document)
		if decodeErr != nil {
			return nil, decodeErr
		}
		out.Notebook = &decoded
		out.NotebookRevision = notebook.Revision
	}
	return out, nil
}
