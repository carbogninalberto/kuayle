package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// The workflow runner's durable state lives on the task (its cursor) and in
// agent_task_workflow_steps (the path taken). Every change of position is one
// transaction fenced like any task write: by the worker lease, or for a
// person's decision by the expected step sequence, plus a per-task advisory
// lock so a single runner advances a task at a time.

var workflowNamespace = uuid.MustParse("2b0f8f36-79a4-4d47-9b55-8f1c0a0b6a95")

// Workflow entry kinds: what entering the final node of a move does.
const (
	WorkflowEnterIteration = "iteration"
	WorkflowEnterReview    = "review"
	WorkflowEnterPublish   = "publish"
	WorkflowEnterWait      = "wait"
	WorkflowEnterComplete  = "complete"
	WorkflowEnterFail      = "fail"
	WorkflowEnterPause     = "pause"
)

// WorkflowMove enters one node. Immediate finishes it at once with that
// outcome (a reviewer with nothing published, a step the policy refuses);
// the next move then leaves it.
type WorkflowMove struct {
	Decision  domain.WorkflowDecision
	Immediate string
	Detail    string
	// Stop records a decision without entering its node: a reached limit, or
	// an outcome no transition follows. It is always the last move.
	Stop bool
}

// WorkflowEntry is what entering the last move's node does to the task.
type WorkflowEntry struct {
	Kind   string
	Reason string
	// Iteration: the prompt (original instructions plus this step's section),
	// the trusted instructions and the profile's harness and model.
	Prompt, Instructions, Provider, Model, Thinking string
	ModelServerID                                   *uuid.UUID
	// Review: the reviewer's harness, model and focus.
	Review *domain.AgentReviewRequest
	// Wait: when to evaluate the node again without an event (its timeout).
	WakeAt *time.Time
	// Wait: the node waits for a person (an approval, the merge), so the
	// task's creator is told that a review is needed. Not for a wait nobody
	// has to act on: CI, an agent review, a wait a person just continued.
	NeedsPerson bool
	// Wait: the approved merge the entered node carries out (a merge step
	// right after a person approved the ship approval), stored on its step.
	Merge *domain.MergeWait
}

// WorkflowMerge records an approved merge on the step a task waits on (its
// ship approval, or the merge step after it): the merge's progress, the
// task's reason, when the worker evaluates the step again and whether the
// task's creator is told a person is needed again (the merge failed).
type WorkflowMerge struct {
	Sequence    int
	Merge       *domain.MergeWait
	Reason      string
	Wake        *time.Time
	NeedsPerson bool
	// Actor is the person whose approval started the merge; nil for the runner.
	Actor *uuid.UUID
	Notes []domain.NotebookNote
	// Branch is what the merge read and did, stored in the step's ci
	// (KUA-265): set when it refused.
	Branch *domain.WorkflowBranchRecord
}

// WorkflowApply closes the task's current step with Outcome and applies the
// moves. Sequence must be the task's current step sequence.
type WorkflowApply struct {
	Sequence int
	Outcome  string
	Detail   string
	Moves    []WorkflowMove
	Entry    WorkflowEntry
	// Iteration: the last move starts a coding iteration (counted in the budget).
	Iteration bool
	// Actor is the person whose decision this is; nil for the runner.
	Actor *uuid.UUID
	// Version is the workflow version the moves were planned on. A task that
	// was moved to another version in between refuses them: its sequence did
	// not change, but its transitions did.
	Version *uuid.UUID
	// StageFinished hands an open stage back to its native operation. RestoreRun
	// retains the incoming attempt and its evidence when the worktree was unchanged.
	StageFinished bool
	RestoreRun    *uuid.UUID
	// Hold keeps the task on its current step, unfinished and without moves:
	// the time budget ran out while it waited (Entry fails or pauses), or a
	// person continued past that and it waits again (Entry waits).
	Hold bool
	// EnterCurrent executes the step already recorded by a workflow switch.
	EnterCurrent bool
	// ExtendBudget grants another iteration and time budget of the same size
	// (a person continued past a budget limit).
	ExtendBudget bool
	// Plan is the plan the step being closed produced (a planning step that
	// finished), stored whole on it.
	Plan string
	// PlanFailed: the step being closed is a planning step that failed. It
	// stores an empty plan, so the plan of an earlier visit is no longer the
	// task's latest and is not handed to the agents after it.
	PlanFailed bool
	// Notes are what the move adds to the task's notebook without the
	// agent: the review or check findings that send the work back, a pause,
	// a person's decision.
	Notes []domain.NotebookNote
	// FailureClass is who or what made an agent's iteration fail, when the
	// move ends the workflow in failure. The step's detail keeps the cause.
	FailureClass domain.FailureClass
	// CI is what the CI check step being closed judged (KUA-215).
	CI *domain.WorkflowCIRecord
	// StepMerge is the merge a person approved at the ship approval being
	// closed, deferred until the agent steps after it reach the done end
	// (KUA-216).
	StepMerge *domain.MergeWait
}

func workflowEvent(ctx context.Context, tx *sqlx.Tx, task domain.AgentTask) error {
	if task.ConversationID == nil {
		return nil
	}
	_, err := conversationEvent(ctx, tx, *task.ConversationID, "task.workflow_updated", map[string]any{"task_id": task.ID, "sequence": task.WorkflowSequence, "node": task.WorkflowNode, "status": task.WorkflowStatus})
	return err
}

// transitionWorkflowTask changes the task's state for a workflow node, or
// only its reason when it already is in that state (one waiting node after
// another). ValidWorkflowTaskTransition forbids entering or leaving
// waiting_input, so the waiting_input clock hold lives in transitionAgentTask;
// a workflow node's own person-wait hold comes from ComputeWorkflowApply.
func transitionWorkflowTask(ctx context.Context, tx *sqlx.Tx, task *domain.AgentTask, to, reason string, actor *uuid.UUID) error {
	if len(reason) > 2048 {
		reason = reason[:2048]
	}
	if task.State == to {
		if err := tx.GetContext(ctx, task, `UPDATE agent_tasks SET reason=$2,updated_at=now() WHERE id=$1 RETURNING *`, task.ID, reason); err != nil {
			return err
		}
		if task.ConversationID != nil {
			if _, err := conversationEvent(ctx, tx, *task.ConversationID, "task.updated", task); err != nil {
				return err
			}
		}
		return nil
	}
	entering := task.WorkflowStatus != nil && *task.WorkflowStatus == domain.WorkflowStatusEntering &&
		to == "awaiting_review" && (task.State == "queued" || task.State == "preparing" || task.State == "waiting_for_disk")
	if (!entering && !domain.ValidWorkflowTaskTransition(task.State, to)) || strings.TrimSpace(reason) == "" {
		return ErrAgentTaskConflict
	}
	from := task.State
	if err := tx.GetContext(ctx, task, `UPDATE agent_tasks SET state=$2,reason=$3,updated_at=now() WHERE id=$1 RETURNING *`, task.ID, to, reason); err != nil {
		return err
	}
	return recordAgentTaskTransition(ctx, tx, *task, from, actor)
}

// startWorkflow pins the version on a new task and enters its first agent
// step, which is the iteration delegation creates.
func startWorkflow(ctx context.Context, tx *sqlx.Tx, task *domain.AgentTask, version domain.WorkflowVersion) error {
	if domain.WorkflowKind(version.Graph) == domain.WorkflowKindPipeline {
		return startPipeline(ctx, tx, task, version)
	}
	graph, err := domain.ParseWorkflowGraph(version.Graph)
	if err != nil {
		return err
	}
	start, ok := graph.Start()
	if !ok {
		return ErrConversationInvalid
	}
	if start.Supervised() {
		visits, _ := json.Marshal(map[string]int{start.ID: 1})
		if err = tx.GetContext(ctx, task, `UPDATE agent_tasks SET workflow_version_id=$2,workflow_node=$3,workflow_visits=$4,workflow_iterations=1,workflow_status='running',workflow_sequence=1 WHERE id=$1 RETURNING *`, task.ID, version.ID, start.ID, visits); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO agent_task_workflow_steps(task_id,sequence,node_id,node_type,visit,reason,generation,stage_first_run_id) VALUES($1,1,$2,'start',1,'Task delegated', $3,$4)`, task.ID, start.ID, task.Generation, task.RunID)
		return err
	}
	decision := graph.Decide(domain.WorkflowCursor{Node: start.ID, Visits: map[string]int{}}, domain.WorkflowElse, time.Now(), nil)
	if decision.Stuck || decision.To.Type != domain.WorkflowNodeAgent {
		return ErrConversationInvalid
	}
	visits, _ := json.Marshal(map[string]int{start.ID: 1, decision.To.ID: 1})
	// The time budget starts when the task first leaves queued for real work
	// (preparing), not at creation: a task blocked on its blocker never runs
	// the clock (KUA-171). transitionAgentTask sets workflow_started_at then.
	if err = tx.GetContext(ctx, task, `UPDATE agent_tasks SET workflow_version_id=$2,workflow_node=$3,workflow_visits=$4,workflow_iterations=1,workflow_status='running',workflow_sequence=2 WHERE id=$1 RETURNING *`,
		task.ID, version.ID, decision.To.ID, visits); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO agent_task_workflow_steps(task_id,sequence,node_id,node_type,visit,reason,outcome,finished_at) VALUES($1,1,$2,'start',1,'Task delegated',$3,now())`, task.ID, start.ID, domain.WorkflowElse); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO agent_task_workflow_steps(task_id,sequence,node_id,node_type,visit,from_node,edge_id,condition,reason,generation) VALUES($1,2,$2,$3,1,$4,$5,$6,$7,$8)`,
		task.ID, decision.To.ID, decision.To.Type, start.ID, decision.Edge.ID, decision.Condition, decision.Reason, task.Generation)
	return err
}

// ApplyWorkflow advances a claimed task, fenced by the worker lease.
func (r *AgentTaskRepository) ApplyWorkflow(ctx context.Context, claimed domain.AgentTask, apply WorkflowApply) (*domain.AgentTask, error) {
	tx, task, err := r.transaction(ctx, claimed.ID, true)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = fenceAgentTask(ctx, tx, *task, claimed); err != nil {
		return nil, err
	}
	if err = applyWorkflowTx(ctx, tx, task, apply); err != nil {
		return nil, err
	}
	return task, tx.Commit()
}

// WorkflowDecisionTask reads a task for a person's decision on its workflow:
// its creator, or a workspace owner or admin who can see its issue. Guests
// and other members get ErrConversationNotFound, like every task route.
func (r *AgentTaskRepository) WorkflowDecisionTask(ctx context.Context, ws, id uuid.UUID) (*domain.AgentTask, error) {
	tx, task, err := r.decisionTransaction(ctx, ws, id)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	return task, nil
}

func (r *AgentTaskRepository) decisionTransaction(ctx context.Context, ws, id uuid.UUID) (*sqlx.Tx, *domain.AgentTask, error) {
	actor, err := conversationActor(ctx, ws)
	if err != nil {
		return nil, nil, err
	}
	tx, err := beginConversationTransaction(ctx, r.db)
	if err != nil {
		return nil, nil, err
	}
	fail := func(err error) (*sqlx.Tx, *domain.AgentTask, error) {
		tx.Rollback()
		return nil, nil, err
	}
	var creator uuid.UUID
	err = tx.GetContext(ctx, &creator, `SELECT creator_id FROM agent_tasks WHERE id=$1 AND workspace_id=$2 AND kind='implement'`, id, ws)
	if errors.Is(err, sql.ErrNoRows) {
		return fail(ErrConversationNotFound)
	}
	if err != nil {
		return fail(err)
	}
	task, err := lockAgentTask(ctx, tx, id, actor == creator)
	if err != nil {
		return fail(err)
	}
	if actor != creator {
		var role string
		err = tx.GetContext(ctx, &role, `SELECT role FROM workspace_members WHERE workspace_id=$1 AND user_id=$2 FOR SHARE`, ws, actor)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && role != domain.RoleOwner && role != domain.RoleAdmin) {
			return fail(ErrConversationNotFound)
		}
		if err != nil {
			return fail(err)
		}
		var visible bool
		if err = tx.GetContext(ctx, &visible, `SELECT EXISTS(SELECT 1 FROM issues i WHERE i.id=$1 AND i.workspace_id=$2 AND i.team_id=$3 AND `+teamVisible(ctx, "i.team_id")+`)`, task.IssueID, ws, task.TeamID); err != nil {
			return fail(err)
		}
		if !visible {
			return fail(ErrConversationNotFound)
		}
	}
	return tx, task, nil
}

// DecideWorkflow applies a person's decision (an approval, or continuing
// past a reached limit) for the task's creator or an owner/admin who can see
// the issue. The expected sequence fences it against a concurrent runner.
func (r *AgentTaskRepository) DecideWorkflow(ctx context.Context, ws, id uuid.UUID, apply WorkflowApply) (*domain.AgentTask, error) {
	actor, err := conversationActor(ctx, ws)
	if err != nil {
		return nil, err
	}
	tx, task, err := r.decisionTransaction(ctx, ws, id)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	apply.Actor = &actor
	if err = applyWorkflowTx(ctx, tx, task, apply); err != nil {
		return nil, err
	}
	if err = workflowAudit(ctx, tx, ws, &actor, "task_workflow", id, "decision", map[string]any{"sequence": apply.Sequence, "outcome": apply.Outcome}); err != nil {
		return nil, err
	}
	return task, tx.Commit()
}

// RecordWorkflowMerge stores the runner's progress on the approved merge of
// the step the task waits on, fenced by the worker lease.
func (r *AgentTaskRepository) RecordWorkflowMerge(ctx context.Context, claimed domain.AgentTask, merge WorkflowMerge) (*domain.AgentTask, error) {
	tx, task, err := r.transaction(ctx, claimed.ID, true)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = fenceAgentTask(ctx, tx, *task, claimed); err != nil {
		return nil, err
	}
	merge.Actor = nil
	if err = recordWorkflowMergeTx(ctx, tx, task, merge); err != nil {
		return nil, err
	}
	return task, tx.Commit()
}

// DecideWorkflowMerge records a person's approval of a ship approval as a
// merge to carry out, for the task's creator or an owner/admin who can see
// the issue (as DecideWorkflow). The expected sequence fences it.
func (r *AgentTaskRepository) DecideWorkflowMerge(ctx context.Context, ws, id uuid.UUID, merge WorkflowMerge) (*domain.AgentTask, error) {
	actor, err := conversationActor(ctx, ws)
	if err != nil {
		return nil, err
	}
	tx, task, err := r.decisionTransaction(ctx, ws, id)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	merge.Actor = &actor
	if err = recordWorkflowMergeTx(ctx, tx, task, merge); err != nil {
		return nil, err
	}
	if err = workflowAudit(ctx, tx, ws, &actor, "task_workflow", id, "decision", map[string]any{"sequence": merge.Sequence, "outcome": domain.WorkflowApprovalApproved, "merge": true}); err != nil {
		return nil, err
	}
	return task, tx.Commit()
}

// recordWorkflowMergeTx writes a merge on the open step the task waits on.
// The task must still wait there in awaiting_review, not paused.
func recordWorkflowMergeTx(ctx context.Context, tx *sqlx.Tx, task *domain.AgentTask, merge WorkflowMerge) error {
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "workflow-runner:"+task.ID.String()); err != nil {
		return err
	}
	if !task.RunsWorkflow() || task.WorkflowSequence != merge.Sequence || task.WorkflowStatus == nil || *task.WorkflowStatus != domain.WorkflowStatusWaiting ||
		task.State != "awaiting_review" || task.WorkflowPausedAt != nil || merge.Merge == nil {
		return ErrAgentTaskConflict
	}
	data, err := json.Marshal(merge.Merge)
	if err != nil {
		return err
	}
	branch, err := jsonOrNil(merge.Branch)
	if err != nil {
		return err
	}
	updated, err := tx.ExecContext(ctx, `UPDATE agent_task_workflow_steps SET merge=$3,decided_by=COALESCE($4,decided_by),
 ci=CASE WHEN $5::jsonb IS NULL THEN ci ELSE jsonb_set(COALESCE(ci,'{}'::jsonb),'{branch}',$5::jsonb) END
 WHERE task_id=$1 AND sequence=$2 AND finished_at IS NULL`, task.ID, merge.Sequence, data, merge.Actor, branch)
	if err != nil {
		return err
	}
	if rows, err := updated.RowsAffected(); err != nil || rows != 1 {
		if err == nil {
			err = ErrAgentTaskConflict
		}
		return err
	}
	if err = noteNotebook(ctx, tx, task.ID, merge.Actor, merge.Notes...); err != nil {
		return err
	}
	if err = tx.GetContext(ctx, task, `UPDATE agent_tasks SET workflow_wake_at=$2 WHERE id=$1 RETURNING *`, task.ID, merge.Wake); err != nil {
		return err
	}
	// The same state: only the reason changes, which the task's conversation shows.
	if err = transitionWorkflowTask(ctx, tx, task, "awaiting_review", merge.Reason, merge.Actor); err != nil {
		return err
	}
	if merge.NeedsPerson {
		if err = markWorkflowReviewNotice(ctx, tx, *task, true, nil); err != nil {
			return err
		}
		// The merge failed and waits for a person again: stop the budget clock
		// until someone acts. A merge Kuayle is still running records no hold.
		if err = tx.GetContext(ctx, task, `UPDATE agent_tasks SET workflow_held_since=COALESCE(workflow_held_since,now()) WHERE id=$1 RETURNING *`, task.ID); err != nil {
			return err
		}
	}
	return workflowEvent(ctx, tx, *task)
}

// WorkflowPullRequest is the published pull request and commit for this step.
func (r *AgentTaskRepository) WorkflowPullRequest(ctx context.Context, task domain.AgentTask) (*domain.AgentReviewTarget, error) {
	task, err := workflowPublicationTask(ctx, r.db, task)
	if err != nil {
		return nil, err
	}
	return reviewTarget(ctx, r.db, task)
}

const retainedWorkflowPublicationRun = `(SELECT r.id FROM dev_machine_agent_runs r
 JOIN dev_machine_agent_run_steps s ON s.agent_run_id=r.id AND s.step_type='publication' AND s.status='succeeded'
 WHERE r.task_id=t.id AND r.workspace_id=t.workspace_id AND r.checkout_id=t.checkout_id
 ORDER BY s.completed_at DESC NULLS LAST,s.id DESC LIMIT 1)`

// Used by review and merge lookups, which already restrict tasks to awaiting_review.
const reviewPublicationRun = `COALESCE(CASE WHEN t.workflow_switch_sequence > 0 AND t.workflow_status='waiting' THEN ` + retainedWorkflowPublicationRun + ` END,t.run_id)`

// A switched workflow can enter a check, review or merge before its new
// agent publishes again. Use the most recent successful publication of this
// task's kept checkout, even when a later attempt stopped before publishing.
func workflowPublicationTask(ctx context.Context, q sqlx.QueryerContext, task domain.AgentTask) (domain.AgentTask, error) {
	if task.WorkflowSwitchSequence == 0 {
		return task, nil
	}
	var runID uuid.UUID
	err := sqlx.GetContext(ctx, q, &runID, `SELECT COALESCE(`+retainedWorkflowPublicationRun+`,t.run_id)
 FROM agent_tasks t WHERE t.id=$1 AND t.workspace_id=$2 AND t.checkout_id=$3`, task.ID, task.WorkspaceID, task.CheckoutID)
	if errors.Is(err, sql.ErrNoRows) {
		return task, nil
	}
	if err == nil {
		task.RunID = runID
	}
	return task, err
}

// WorkflowResult is what applying moves does to a task's workflow: its new
// cursor, status and wake time, and the steps to record. It is computed
// without I/O so the runner's tests apply moves exactly as the database does.
type WorkflowResult struct {
	Cursor   domain.WorkflowCursor
	Sequence int
	Status   string
	Wake     *time.Time
	// Steps are the nodes entered; the last one is the node the task is on
	// unless the apply stopped before entering it.
	Steps []domain.WorkflowStep
	// StopReason is added to the detail of the step the task stays on when
	// a limit or a missing transition stopped it.
	StopReason string
	// FinalSequence is the step the entry's effect (iteration, review)
	// belongs to; 0 when no node was entered.
	FinalSequence int
}

// ComputeWorkflowApply applies moves to the task's cursor.
func ComputeWorkflowApply(task domain.AgentTask, apply WorkflowApply, now time.Time) WorkflowResult {
	result := WorkflowResult{Cursor: task.Cursor(), Sequence: apply.Sequence, FinalSequence: apply.Sequence, Status: domain.WorkflowStatusRunning}
	last := len(apply.Moves) - 1
	for i, move := range apply.Moves {
		if i == last && (move.Stop || apply.Entry.Kind == WorkflowEnterPause) {
			result.StopReason = truncateBytes(move.Decision.Reason, 2048)
			break
		}
		to := move.Decision.To
		result.Sequence++
		result.Cursor.Visits[to.ID]++
		result.Cursor.Node = to.ID
		step := domain.WorkflowStep{TaskID: task.ID, Sequence: result.Sequence, NodeID: to.ID, NodeType: to.Type, Visit: result.Cursor.Visits[to.ID],
			Reason: truncateBytes(move.Decision.Reason, 2048), Detail: truncateBytes(move.Detail, 16000), EnteredAt: now}
		if move.Decision.Edge != nil {
			edge, condition := move.Decision.Edge.ID, move.Decision.Condition
			step.EdgeID, step.Condition = &edge, &condition
		}
		if move.Decision.From.ID != "" {
			from := move.Decision.From.ID
			step.FromNode = &from
		}
		if i < last {
			outcome := move.Immediate
			step.Outcome, step.FinishedAt = &outcome, &now
		} else if to.Type == domain.WorkflowNodeEnd && apply.Entry.Kind != WorkflowEnterWait && apply.Entry.Kind != WorkflowEnterIteration {
			// An end is finished when it is entered, unless the task waits
			// there for the merge a ship approval deferred to it (KUA-216).
			outcome := to.Outcome
			step.Outcome, step.FinishedAt = &outcome, &now
		}
		result.Steps = append(result.Steps, step)
		result.FinalSequence = result.Sequence
	}
	if apply.Iteration && apply.Entry.Kind == WorkflowEnterIteration {
		result.Cursor.Iterations++
	}
	// Waiting on a person (an approval or a merge) stops the time budget: the
	// clock is held from now (or kept held if it already was). Every other
	// move ends a hold and adds the time waited to the budget start (KUA-171).
	personWait := apply.Entry.Kind == WorkflowEnterWait && apply.Entry.NeedsPerson
	if personWait {
		if result.Cursor.HeldSince.IsZero() {
			result.Cursor.HeldSince = now
		}
	} else {
		result.Cursor = result.Cursor.Released(now)
	}
	if apply.ExtendBudget {
		// A granted budget starts again now; a person wait stays held from
		// now, so the next budget counts only the working time after it ends.
		result.Cursor.Extensions++
		result.Cursor.BudgetFrom = now
		if personWait {
			result.Cursor.HeldSince = now
		} else {
			result.Cursor.HeldSince = time.Time{}
		}
	}
	switch apply.Entry.Kind {
	case WorkflowEnterWait, WorkflowEnterReview:
		result.Status, result.Wake = domain.WorkflowStatusWaiting, apply.Entry.WakeAt
	case WorkflowEnterComplete:
		result.Status = domain.WorkflowStatusDone
	case WorkflowEnterFail:
		result.Status = domain.WorkflowStatusFailed
	case WorkflowEnterPause:
		result.Status = domain.WorkflowStatusPaused
	}
	return result
}

// WorkflowEntryState is the task state entering a node of this kind leads
// to ("cancelling" for a failure, which cleanup then settles as failed).
func WorkflowEntryState(kind string) string {
	switch kind {
	case WorkflowEnterIteration:
		return "queued"
	case WorkflowEnterReview, WorkflowEnterWait:
		return "awaiting_review"
	case WorkflowEnterPublish:
		return "publishing"
	case WorkflowEnterComplete:
		return "completed"
	case WorkflowEnterFail:
		return "cancelling"
	case WorkflowEnterPause:
		return "blocked"
	}
	return ""
}

func applyWorkflowTx(ctx context.Context, tx *sqlx.Tx, task *domain.AgentTask, apply WorkflowApply) error {
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "workflow-runner:"+task.ID.String()); err != nil {
		return err
	}
	if !task.RunsWorkflow() || task.WorkflowSequence != apply.Sequence || (len(apply.Moves) == 0) != apply.Hold || WorkflowEntryState(apply.Entry.Kind) == "" {
		return ErrAgentTaskConflict
	}
	if apply.Version != nil && *apply.Version != *task.WorkflowVersionID {
		return ErrAgentTaskConflict
	}
	// Nothing moves while a person's pause holds the workflow.
	if task.WorkflowPausedAt != nil {
		return ErrAgentTaskConflict
	}
	if apply.EnterCurrent {
		if !apply.Hold || task.State != "awaiting_review" || task.WorkflowStatus == nil || *task.WorkflowStatus != domain.WorkflowStatusEntering {
			return ErrAgentTaskConflict
		}
		if _, err := tx.ExecContext(ctx, `UPDATE agent_task_workflow_steps SET entered_at=now() WHERE task_id=$1 AND sequence=$2 AND finished_at IS NULL`, task.ID, apply.Sequence); err != nil {
			return err
		}
	} else if apply.StageFinished {
		if task.State != "running" || task.WorkflowStatus == nil || *task.WorkflowStatus != domain.WorkflowStatusRunning {
			return ErrAgentTaskConflict
		}
		if apply.RestoreRun != nil {
			if err := tx.GetContext(ctx, task, `UPDATE agent_tasks t SET run_id=s.stage_input_run_id FROM agent_task_workflow_steps s WHERE t.id=$1 AND s.task_id=t.id AND s.sequence=$2 AND s.finished_at IS NULL AND s.stage_input_run_id=$3 RETURNING t.*`, task.ID, apply.Sequence, *apply.RestoreRun); errors.Is(err, sql.ErrNoRows) {
				return ErrAgentTaskConflict
			} else if err != nil {
				return err
			}
		}
	} else if apply.Hold {
		// Only an unfinished step is held: a waiting one stops (fails or
		// pauses), and a paused one waits again. A hold keeps the sequence,
		// so the status is what fences a replayed decision.
		// A done end waiting for its deferred merge completes once merged
		// (KUA-216).
		want, endOnly := domain.WorkflowStatusWaiting, false
		switch apply.Entry.Kind {
		case WorkflowEnterWait:
			want = domain.WorkflowStatusPaused
		case WorkflowEnterComplete:
			endOnly = true
		case WorkflowEnterFail, WorkflowEnterPause:
		default:
			return ErrAgentTaskConflict
		}
		if task.WorkflowStatus == nil || *task.WorkflowStatus != want {
			return ErrAgentTaskConflict
		}
		var open bool
		if err := tx.GetContext(ctx, &open, `SELECT EXISTS(SELECT 1 FROM agent_task_workflow_steps WHERE task_id=$1 AND sequence=$2 AND finished_at IS NULL AND (NOT $3 OR node_type='end'))`, task.ID, apply.Sequence, endOnly); err != nil {
			return err
		}
		if !open {
			return ErrAgentTaskConflict
		}
	}
	switch task.State {
	case "running", "validating", "publishing", "awaiting_review", "blocked":
	default:
		return ErrAgentTaskConflict
	}
	// A step left (or a held wait that fails) no longer needs its review.
	if !apply.Hold || apply.Entry.Kind == WorkflowEnterFail {
		if err := cancelWorkflowReviews(ctx, tx, task.ID, apply.Sequence); err != nil {
			return err
		}
	}
	if apply.Hold {
		if apply.Actor != nil {
			if _, err := tx.ExecContext(ctx, `UPDATE agent_task_workflow_steps SET decided_by=$3 WHERE task_id=$1 AND sequence=$2`, task.ID, apply.Sequence, apply.Actor); err != nil {
				return err
			}
		}
		if apply.Entry.Kind == WorkflowEnterComplete || (apply.StageFinished && apply.Entry.Kind == WorkflowEnterFail) {
			if _, err := tx.ExecContext(ctx, `UPDATE agent_task_workflow_steps SET outcome=COALESCE(NULLIF($3,''),outcome),detail=CASE WHEN $4='' THEN detail ELSE $4 END,finished_at=COALESCE(finished_at,now()) WHERE task_id=$1 AND sequence=$2`,
				task.ID, apply.Sequence, apply.Outcome, truncateBytes(apply.Detail, 16000)); err != nil {
				return err
			}
		}
	} else if _, err := tx.ExecContext(ctx, `UPDATE agent_task_workflow_steps SET outcome=COALESCE(NULLIF($3,''),outcome),detail=CASE WHEN $4='' THEN detail ELSE $4 END,decided_by=COALESCE($5,decided_by),finished_at=COALESCE(finished_at,now()) WHERE task_id=$1 AND sequence=$2`,
		task.ID, apply.Sequence, apply.Outcome, truncateBytes(apply.Detail, 16000), apply.Actor); err != nil {
		return err
	}
	// What the closed CI step judged, and the merge a person approved at the
	// closed ship approval for the done end after its agent steps.
	if !apply.Hold && (apply.CI != nil || apply.StepMerge != nil) {
		ci, err := jsonOrNil(apply.CI)
		if err != nil {
			return err
		}
		merge, err := jsonOrNil(apply.StepMerge)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE agent_task_workflow_steps SET ci=COALESCE($3::jsonb,ci),merge=COALESCE($4::jsonb,merge) WHERE task_id=$1 AND sequence=$2`, task.ID, apply.Sequence, ci, merge); err != nil {
			return err
		}
	}
	if (apply.Plan != "" || apply.PlanFailed) && !apply.Hold {
		if _, err := tx.ExecContext(ctx, `UPDATE agent_task_workflow_steps SET plan=$3 WHERE task_id=$1 AND sequence=$2`, task.ID, apply.Sequence, apply.Plan); err != nil {
			return err
		}
	}
	// The notebook records the plan and what Kuayle knows, attributed to
	// the step being closed and its attempt, before the task moves on.
	if (apply.Plan != "" || apply.PlanFailed) && !apply.Hold {
		if err := setNotebookPlan(ctx, tx, task.ID, apply.Plan); err != nil {
			return err
		}
	}
	if err := noteNotebook(ctx, tx, task.ID, apply.Actor, apply.Notes...); err != nil {
		return err
	}
	result := ComputeWorkflowApply(*task, apply, time.Now())
	for _, step := range result.Steps {
		if _, err := tx.ExecContext(ctx, `INSERT INTO agent_task_workflow_steps(task_id,sequence,node_id,node_type,visit,from_node,edge_id,condition,reason,outcome,detail,finished_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,CASE WHEN $12 THEN now() END)`, task.ID, step.Sequence, step.NodeID, step.NodeType, step.Visit, step.FromNode, step.EdgeID, step.Condition, step.Reason, step.Outcome, step.Detail, step.FinishedAt != nil); err != nil {
			return err
		}
	}
	if result.StopReason != "" {
		if _, err := tx.ExecContext(ctx, `UPDATE agent_task_workflow_steps SET detail=left(CASE WHEN detail='' THEN $3 ELSE detail||E'\n'||$3 END,16000) WHERE task_id=$1 AND sequence=$2`, task.ID, result.Sequence, result.StopReason); err != nil {
			return err
		}
	}
	visits, err := json.Marshal(result.Cursor.Visits)
	if err != nil {
		return err
	}
	if err = tx.GetContext(ctx, task, `UPDATE agent_tasks SET workflow_node=$2,workflow_visits=$3,workflow_iterations=$4,workflow_sequence=$5,workflow_status=$6,workflow_wake_at=$7,workflow_budget_extensions=$8,workflow_budget_from=$9,workflow_held_since=$10 WHERE id=$1 RETURNING *`,
		task.ID, result.Cursor.Node, visits, result.Cursor.Iterations, result.Sequence, result.Status, result.Wake, result.Cursor.Extensions, nullTime(result.Cursor.BudgetFrom), nullTime(result.Cursor.HeldSince)); err != nil {
		return err
	}
	reason := apply.Entry.Reason
	if apply.Entry.Merge != nil && (len(result.Steps) > 0 || apply.StageFinished || apply.EnterCurrent) {
		var data []byte
		if data, err = json.Marshal(apply.Entry.Merge); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE agent_task_workflow_steps SET merge=$3 WHERE task_id=$1 AND sequence=$2`, task.ID, result.FinalSequence, data); err != nil {
			return err
		}
	}
	switch apply.Entry.Kind {
	case WorkflowEnterIteration:
		inputRun := task.RunID
		if err = workflowIteration(ctx, tx, task, apply.Entry, apply.Actor); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE agent_task_workflow_steps SET generation=$3,stage_first_run_id=CASE WHEN node_type<>'agent' THEN $5::uuid END,stage_input_run_id=CASE WHEN node_type<>'agent' THEN $4::uuid END WHERE task_id=$1 AND sequence=$2`, task.ID, result.FinalSequence, task.Generation, inputRun, task.RunID)
	case WorkflowEnterWait:
		err = transitionWorkflowTask(ctx, tx, task, WorkflowEntryState(apply.Entry.Kind), reason, apply.Actor)
	case WorkflowEnterReview:
		var review *domain.AgentTask
		if review, err = workflowReview(ctx, tx, task, apply.Entry, result.FinalSequence); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE agent_task_workflow_steps SET review_task_id=$3 WHERE task_id=$1 AND sequence=$2`, task.ID, result.FinalSequence, review.ID); err != nil {
			return err
		}
		err = transitionWorkflowTask(ctx, tx, task, "awaiting_review", reason, apply.Actor)
	case WorkflowEnterFail:
		err = beginAgentTaskFailure(ctx, tx, task, apply.FailureClass, reason)
	default:
		err = transitionWorkflowTask(ctx, tx, task, WorkflowEntryState(apply.Entry.Kind), reason, apply.Actor)
	}
	if err != nil {
		return err
	}
	if WorkflowEntryState(apply.Entry.Kind) == "awaiting_review" {
		if err = markWorkflowReviewNotice(ctx, tx, *task, apply.Entry.NeedsPerson, apply.Actor); err != nil {
			return err
		}
	}
	return workflowEvent(ctx, tx, *task)
}

// markWorkflowReviewNotice decides whether the creator of a workflow task
// that now waits in awaiting_review is told that a review is needed: only
// when the step waits for a person, and not for the creator's own decision.
// A workflow passes through several waits in that state (an agent review, CI,
// an approval, the merge) and records a transition only for the first, so the
// mark is set on the transition that entered the state rather than taken
// from recordAgentTaskTransition. That row may have been claimed before: a
// later step waiting for a person is announced again.
func markWorkflowReviewNotice(ctx context.Context, tx *sqlx.Tx, task domain.AgentTask, needsPerson bool, actor *uuid.UUID) error {
	pending := needsPerson && task.CreatorID != uuid.Nil && (actor == nil || *actor != task.CreatorID)
	_, err := tx.ExecContext(ctx, `UPDATE agent_task_transitions SET notification_pending=$2
 WHERE sequence=(SELECT max(sequence) FROM agent_task_transitions WHERE task_id=$1 AND to_state='awaiting_review')`, task.ID, pending)
	if err != nil || !needsPerson {
		return err
	}
	// A watch for awaiting_review is due when a person is needed (KUA-165),
	// also when the creator made the change and is not notified.
	return markTaskWatchersDue(ctx, tx, task, "awaiting_review", pending)
}

// jsonOrNil encodes a value for a JSONB parameter; nil stays NULL.
func jsonOrNil[T any](value *T) (*string, error) {
	if value == nil {
		return nil, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	text := string(data)
	return &text, nil
}

func nullTime(at time.Time) *time.Time {
	if at.IsZero() {
		return nil
	}
	return &at
}

func truncateBytes(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	for limit > 0 && (value[limit]&0xC0) == 0x80 {
		limit--
	}
	return value[:limit]
}

// workflowIteration starts the next coding iteration of the task like a
// retry: a new generation and run on the same worktree and branch, with the
// agent step's prompt and profile. Nothing of the previous run may still be
// active.
func workflowIteration(ctx context.Context, tx *sqlx.Tx, task *domain.AgentTask, entry WorkflowEntry, actor *uuid.UUID) error {
	var pending bool
	err := tx.GetContext(ctx, &pending, `SELECT EXISTS(SELECT 1 FROM dev_machine_agent_runs WHERE task_id=$1 AND status IN ('queued','starting','running','waiting_input'))
 OR EXISTS(SELECT 1 FROM dev_machine_operations o WHERE (o.checkout_id=$2 OR o.agent_run_id IN (SELECT id FROM dev_machine_agent_runs WHERE task_id=$1)) AND o.status IN ('pending','leased') AND o.action NOT IN ('start_preview','stop_preview'))`, task.ID, task.CheckoutID)
	if err != nil {
		return err
	}
	if pending {
		return ErrAgentTaskConflict
	}
	if strings.TrimSpace(entry.Prompt) == "" || len(entry.Prompt) > 32768 || entry.Provider == "" {
		return ErrConversationInvalid
	}
	if entry.ModelServerID == nil {
		if entry.ModelServerID, err = defaultModelServer(ctx, tx, task.WorkspaceID, entry.Provider); err != nil {
			return err
		}
	}
	if err = tx.GetContext(ctx, task, `UPDATE agent_tasks t SET prompt=$2,agent_instructions=$3,provider=$4,model=$5,thinking=$6,model_server_id=$7,generation=generation+1,run_id=gen_random_uuid(),failure_count=0,failure_pending=false,failure_class=NULL,sign_in_required=false,usage_limit_message=NULL,usage_limit_resets_at=NULL,usage_limit_resume_at=NULL,provider_credit_exhausted=false,resume_native_session=false,platform_retries=0,platform_retry_pending=false,lease_token=NULL,lease_expires_at=NULL,next_poll_at=now(),issue_status_id=i.status_id,issue_updated_at=i.updated_at
 FROM issues i WHERE t.id=$1 AND i.id=t.issue_id AND i.workspace_id=t.workspace_id AND i.team_id=t.team_id RETURNING t.*`,
		task.ID, entry.Prompt, entry.Instructions, entry.Provider, entry.Model, entry.Thinking, entry.ModelServerID); err != nil {
		return err
	}
	return transitionWorkflowTask(ctx, tx, task, "queued", entry.Reason, actor)
}

// workflowReview queues a reviewer step as a review task of the published
// pull request, in the task creator's own issue conversation, so it runs
// with the creator's permissions like the task itself.
func workflowReview(ctx context.Context, tx *sqlx.Tx, task *domain.AgentTask, entry WorkflowEntry, sequence int) (*domain.AgentTask, error) {
	if entry.Review == nil || task.ConversationID == nil {
		return nil, ErrConversationInvalid
	}
	reviewed, err := workflowPublicationTask(ctx, tx, *task)
	if err != nil {
		return nil, err
	}
	published, err := reviewTarget(ctx, tx, reviewed)
	if err != nil {
		return nil, err
	}
	input := *entry.Review
	// One review per step: replaying the step's transaction finds it again.
	input.RequestID = uuid.NewSHA1(workflowNamespace, []byte(fmt.Sprintf("%s/review/%d", task.ID, sequence)))
	review, _, err := insertReviewTask(ctx, tx, reviewed, *task.ConversationID, task.CreatorID, input, published)
	return review, err
}

// defaultModelServer is the workspace's default model server when it speaks
// the harness's protocol, as the task composer and requested reviews use it;
// nil otherwise. Workflow steps carry no model server of their own.
func defaultModelServer(ctx context.Context, q sqlx.QueryerContext, ws uuid.UUID, provider string) (*uuid.UUID, error) {
	var server domain.AgentModelServer
	err := sqlx.GetContext(ctx, q, &server, `SELECT * FROM agent_model_servers WHERE workspace_id=$1 AND is_default`, ws)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil || !server.Serves(provider) {
		return nil, err
	}
	return &server.ID, nil
}

// recordedReviewCI is what webhooks reported about CI on the reviewed
// commit of the task's current attempt. GitHub only reports completed check
// suites, so the list is partial; with none it is unknown.
func recordedReviewCI(ctx context.Context, q sqlx.QueryerContext, reviewed domain.AgentTask, commit string) (domain.AgentReviewCI, error) {
	var suites []struct {
		Name    string `db:"name"`
		Outcome string `db:"outcome"`
	}
	if err := sqlx.SelectContext(ctx, q, &suites, `SELECT name,outcome FROM agent_task_signals WHERE task_id=$1 AND run_id=$2 AND kind='ci' AND head_sha=$3 AND head_sha<>'' ORDER BY created_at,id LIMIT 30`, reviewed.ID, reviewed.RunID, commit); err != nil {
		return domain.AgentReviewCI{}, err
	}
	if len(suites) == 0 {
		return domain.AgentReviewCI{}, nil
	}
	ci := domain.AgentReviewCI{Read: true, Partial: true}
	for _, suite := range suites {
		state := "unknown"
		switch {
		case passedCIOutcomes[suite.Outcome]:
			state = "passed"
		case failedCIOutcomes[suite.Outcome]:
			state = "failed"
		}
		name := suite.Name
		if name == "" {
			name = "CI"
		}
		ci.Checks = append(ci.Checks, domain.AgentReviewCheck{Name: name + " (check suite)", State: state})
	}
	return ci, nil
}

// insertReviewTask creates a review task of the reviewed task's published
// work and its review record, in the caller's transaction.
func insertReviewTask(ctx context.Context, tx *sqlx.Tx, reviewed domain.AgentTask, conversation, creator uuid.UUID, input domain.AgentReviewRequest, published *domain.AgentReviewTarget) (*domain.AgentTask, *domain.AgentReview, error) {
	var attempt int
	if err := tx.GetContext(ctx, &attempt, `SELECT COALESCE(max(attempt),0)+1 FROM agent_reviews WHERE reviewed_task_id=$1 AND commit_sha=$2 AND provider=$3`, reviewed.ID, published.CommitSHA, input.Provider); err != nil {
		return nil, nil, err
	}
	// The reviewer is told what Kuayle knows about CI on the commit: what a
	// workflow just read from GitHub, or else the suites webhooks reported.
	ci := domain.AgentReviewCI{}
	if input.CI != nil {
		ci = *input.CI
	} else {
		var err error
		if ci, err = recordedReviewCI(ctx, tx, reviewed, published.CommitSHA); err != nil {
			return nil, nil, err
		}
	}
	task, err := insertReviewAgentTask(ctx, tx, reviewed, conversation, creator, input, domain.AgentReviewPrompt(*published, input.Focus, ci), "Waiting for a machine to review the pull request")
	if err != nil {
		return nil, nil, err
	}
	comment := "not_requested"
	if input.PostToPullRequest {
		comment = "pending"
	}
	review := &domain.AgentReview{}
	err = tx.GetContext(ctx, review, `INSERT INTO agent_reviews(task_id,workspace_id,reviewed_task_id,requested_by,provider,commit_sha,repository_full_name,pull_request_number,pull_request_url,base_branch,focus,attempt,post_to_pull_request,comment_state)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING *`,
		task.ID, reviewed.WorkspaceID, reviewed.ID, creator, input.Provider, published.CommitSHA, published.Repository, published.PullRequestNumber, published.PullRequestURL, published.BaseBranch, input.Focus, attempt, input.PostToPullRequest, comment)
	if err != nil {
		return nil, nil, err
	}
	if err = recordAgentTaskTransition(ctx, tx, *task, "", &creator); err != nil {
		return nil, nil, err
	}
	return task, review, nil
}

// insertReviewAgentTask inserts a review task of the reviewed task's work
// with prompt: kind review (never published, no repository credentials) on
// the same issue, in the creator's conversation, so that it runs with the
// creator's permissions. Without a model server it runs on the workspace
// default when that speaks the harness's protocol. The caller records its
// creation (recordAgentTaskTransition) once what goes with it is stored.
func insertReviewAgentTask(ctx context.Context, tx *sqlx.Tx, reviewed domain.AgentTask, conversation, creator uuid.UUID, input domain.AgentReviewRequest, prompt, reason string) (*domain.AgentTask, error) {
	if input.ModelServerID == nil {
		var err error
		if input.ModelServerID, err = defaultModelServer(ctx, tx, reviewed.WorkspaceID, input.Provider); err != nil {
			return nil, err
		}
	}
	var task domain.AgentTask
	err := tx.GetContext(ctx, &task, `INSERT INTO agent_tasks(workspace_id,conversation_id,creator_id,issue_id,team_id,request_id,prompt,provider,model,thinking,model_server_id,reason,issue_status_id,issue_updated_at,kind,review_of_task_id)
 SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,i.status_id,i.updated_at,'review',$13 FROM issues i WHERE i.id=$4 AND i.workspace_id=$1 AND i.team_id=$5 RETURNING *`,
		reviewed.WorkspaceID, conversation, creator, reviewed.IssueID, reviewed.TeamID, input.RequestID, prompt, input.Provider, input.Model, input.Thinking, input.ModelServerID, reason, reviewed.ID)
	if err != nil {
		return nil, err
	}
	return &task, nil
}

// closeWorkflowOnFinish records the end of a workflow task that finished
// outside the runner (accepted, merged on GitHub, cancelled or failed by
// cleanup), in the finishing transition's transaction.
func closeWorkflowOnFinish(ctx context.Context, tx *sqlx.Tx, task domain.AgentTask) error {
	if !task.RunsWorkflow() {
		return nil
	}
	status := ""
	switch task.State {
	case "completed":
		status = domain.WorkflowStatusDone
	case "failed", "cancelled":
		status = domain.WorkflowStatusFailed
	default:
		return nil
	}
	if err := cancelWorkflowReviews(ctx, tx, task.ID, 0); err != nil {
		return err
	}
	if err := cancelMemberTurns(ctx, tx, task); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE agent_task_workflow_steps SET outcome=COALESCE(outcome,$2),finished_at=now() WHERE task_id=$1 AND finished_at IS NULL`, task.ID, "task_"+task.State); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE agent_tasks SET workflow_status=$2,workflow_wake_at=NULL,workflow_paused_at=NULL,workflow_pause_request=NULL WHERE id=$1`, task.ID, status)
	return err
}

// cancelWorkflowReviews asks the review tasks of steps the workflow leaves
// before their verdict (a timeout, a budget, a person's stop, the task ending
// outside the runner) to stop, as a cancellation request does: nothing would
// read their result. sequence names the step; 0 means every open step.
// Reviews lock themselves before their parent (wakeWorkflowParent), so one
// locked by its own transition right now is skipped rather than risking a
// deadlock; its result is then ignored.
func cancelWorkflowReviews(ctx context.Context, tx *sqlx.Tx, taskID uuid.UUID, sequence int) error {
	var reviews []domain.AgentTask
	if err := tx.SelectContext(ctx, &reviews, `SELECT t.* FROM agent_tasks t JOIN agent_task_workflow_steps s ON s.review_task_id=t.id
 WHERE s.task_id=$1 AND t.kind='review' AND t.review_of_task_id=$1 AND (s.sequence=$2 OR ($2=0 AND s.finished_at IS NULL))
 AND t.state NOT IN ('completed','failed','cancelled','cancelling') FOR UPDATE OF t SKIP LOCKED`, taskID, sequence); err != nil {
		return err
	}
	for i := range reviews {
		if err := transitionAgentTask(ctx, tx, &reviews[i], "cancelling", "The workflow left this review's step before it finished", nil); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE agent_tasks SET lease_token=NULL,lease_expires_at=NULL,next_poll_at=now() WHERE id=$1`, reviews[i].ID); err != nil {
			return err
		}
	}
	return nil
}

// cancelMemberTurns asks the turns the members of a pipeline task that ended
// are taking to stop, as cancelWorkflowReviews does for a graph's reviews:
// nothing would read their result. A member task locked by its own
// transition right now is skipped; its turn ends on its own.
func cancelMemberTurns(ctx context.Context, tx *sqlx.Tx, root domain.AgentTask) error {
	if !root.Pipeline {
		return nil
	}
	var turns []domain.AgentTask
	if err := tx.SelectContext(ctx, &turns, `SELECT t.* FROM agent_tasks t JOIN task_members m ON m.member_task_id=t.id AND m.workspace_id=t.workspace_id
 WHERE m.task_id=$1 AND m.workspace_id=$2 AND t.state IN ('queued','preparing','waiting_for_disk','running','waiting_input') FOR UPDATE OF t SKIP LOCKED`, root.ID, root.WorkspaceID); err != nil {
		return err
	}
	for i := range turns {
		if err := transitionAgentTask(ctx, tx, &turns[i], "cancelling", "The task ended before this turn did", nil); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE agent_tasks SET lease_token=NULL,lease_expires_at=NULL,next_poll_at=now() WHERE id=$1`, turns[i].ID); err != nil {
			return err
		}
	}
	return nil
}

// wakeWorkflowParent asks the worker to evaluate the workflow step waiting
// for a review task that just finished.
func wakeWorkflowParent(ctx context.Context, tx *sqlx.Tx, review domain.AgentTask) error {
	if review.Kind != domain.AgentTaskKindReview || review.ReviewOfTaskID == nil {
		return nil
	}
	switch review.State {
	case "completed", "failed", "cancelled":
	default:
		return nil
	}
	_, err := tx.ExecContext(ctx, `UPDATE agent_tasks SET workflow_wake_at=now() WHERE id=$1 AND workspace_id=$2 AND workflow_version_id IS NOT NULL AND state='awaiting_review'
 AND EXISTS(SELECT 1 FROM agent_task_workflow_steps s WHERE s.task_id=$1 AND s.review_task_id=$3 AND s.finished_at IS NULL)`, *review.ReviewOfTaskID, review.WorkspaceID, review.ID)
	return err
}

// WorkflowSteps lists the path a task took, under the task's authorization.
func (r *AgentTaskRepository) WorkflowSteps(ctx context.Context, ws, id uuid.UUID) ([]domain.WorkflowStep, error) {
	if _, err := conversationActor(ctx, ws); err != nil {
		return nil, err
	}
	tx, _, err := r.transaction(ctx, id, true)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	steps := []domain.WorkflowStep{}
	err = tx.SelectContext(ctx, &steps, `SELECT * FROM agent_task_workflow_steps WHERE task_id=$1 ORDER BY sequence`, id)
	return steps, err
}

// stepRunColumns reads a run as the timeline shows it. The summary is only
// the harness's final message; a run without one keeps its raw output private
// to the logs.
// $3 is the completion time up to which the reader already has summaries.
const stepRunColumns = `r.id,r.task_id,r.provider_id,COALESCE(r.model,'') AS model,r.status::text AS status,r.created_at,r.started_at,r.completed_at,
 CASE WHEN COALESCE(r.result->>'final_message','')='true' AND ($3::timestamptz IS NULL OR r.completed_at IS NULL OR r.completed_at>$3) THEN r.summary END AS summary,COALESCE(r.result->>'summary_truncated','')='true' AS summary_truncated,
 COALESCE(r.result->>'replies_unknown','')='true' AS replies_unknown,
 r.input_tokens,r.output_tokens,r.cache_read_tokens,r.cache_write_tokens,r.cost_usd`

// StepRuns lists the task's agent runs for its timeline, oldest first, under
// the task's authorization: its newest 200 runs, each on the agent step that
// was the latest one entered when the run was created (a resumed step has
// several), the runs of the review tasks its reviewer steps created, and on
// a pipeline task the runs of its members' tasks, each with the role it
// played (the root's own runs are its writer's). A review or member task has
// the creator, conversation and issue of the task it reviews, so whoever
// reads the task reads them. A finished run's summary never changes:
// summariesSince leaves out those of runs that completed at or before it,
// which a view polling the task already holds.
func (r *AgentTaskRepository) StepRuns(ctx context.Context, ws, id uuid.UUID, summariesSince *time.Time) ([]domain.WorkflowStepRun, error) {
	if _, err := conversationActor(ctx, ws); err != nil {
		return nil, err
	}
	tx, task, err := r.transaction(ctx, id, true)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	runs := []domain.WorkflowStepRun{}
	err = tx.SelectContext(ctx, &runs, `SELECT * FROM (
 (SELECT `+stepRunColumns+`,(SELECT max(s.sequence) FROM agent_task_workflow_steps s WHERE s.task_id=r.task_id AND s.generation IS NOT NULL AND s.entered_at<=r.created_at)::int AS step,
  (SELECT m.role FROM task_members m WHERE m.task_id=$1 AND m.workspace_id=$2 AND m.member_task_id IS NULL) AS role
  FROM dev_machine_agent_runs r WHERE r.task_id=$1 AND r.workspace_id=$2 ORDER BY r.created_at DESC,r.id DESC LIMIT 200)
 UNION ALL
 (SELECT `+stepRunColumns+`,s.sequence AS step,NULL::text AS role
  FROM agent_task_workflow_steps s JOIN agent_tasks t ON t.id=s.review_task_id AND t.workspace_id=$2 AND t.kind='review' AND t.review_of_task_id=$1
  JOIN dev_machine_agent_runs r ON r.task_id=t.id AND r.workspace_id=$2 WHERE s.task_id=$1 ORDER BY r.created_at DESC,r.id DESC LIMIT 200)
 UNION ALL
 (SELECT `+stepRunColumns+`,NULL::int AS step,m.role
  FROM task_members m JOIN agent_tasks t ON t.id=m.member_task_id AND t.workspace_id=$2 AND t.kind='review' AND t.review_of_task_id=$1 AND t.creator_id=$4
  JOIN dev_machine_agent_runs r ON r.task_id=t.id AND r.workspace_id=$2 WHERE m.task_id=$1 AND m.workspace_id=$2 ORDER BY r.created_at DESC,r.id DESC LIMIT 200)
 ) runs ORDER BY created_at,id`, id, ws, summariesSince, task.CreatorID)
	return runs, err
}

// CurrentWorkflowStep is the step the task is on (runner-internal read).
func (r *AgentTaskRepository) CurrentWorkflowStep(ctx context.Context, task domain.AgentTask) (*domain.WorkflowStep, error) {
	step := &domain.WorkflowStep{}
	err := r.db.GetContext(ctx, step, `SELECT * FROM agent_task_workflow_steps WHERE task_id=$1 AND sequence=$2`, task.ID, task.WorkflowSequence)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAgentTaskConflict
	}
	return step, err
}

// WorkflowReview is what a reviewer step's review task reported.
type WorkflowReview struct {
	State    string  `db:"state"`
	Verdict  *string `db:"verdict"`
	Findings *string `db:"findings"`
	// Reason is the review task's reason, and FailureRule the rule that
	// classified its current run when that run did not succeed (KUA-218).
	Reason      string  `db:"reason"`
	FailureRule *string `db:"failure_rule"`
}

func (r *AgentTaskRepository) WorkflowReview(ctx context.Context, task domain.AgentTask, review uuid.UUID) (*WorkflowReview, error) {
	out := &WorkflowReview{}
	err := r.db.GetContext(ctx, out, `SELECT t.state,t.reason,v.verdict,v.findings,run.failure_rule FROM agent_tasks t LEFT JOIN agent_reviews v ON v.task_id=t.id
 LEFT JOIN dev_machine_agent_runs run ON run.id=t.run_id AND run.task_id=t.id
 WHERE t.id=$1 AND t.workspace_id=$2 AND t.review_of_task_id=$3`, review, task.WorkspaceID, task.ID)
	return out, err
}

// LatestWorkflowReview returns the findings of the newest finished reviewer
// step, for the next agent's {{review_comments}}.
func (r *AgentTaskRepository) LatestWorkflowReview(ctx context.Context, task domain.AgentTask) (string, error) {
	var findings sql.NullString
	err := r.db.GetContext(ctx, &findings, `SELECT v.findings FROM agent_task_workflow_steps s JOIN agent_reviews v ON v.task_id=s.review_task_id
 WHERE s.task_id=$1 AND s.finished_at IS NOT NULL AND v.completed_at IS NOT NULL ORDER BY s.sequence DESC LIMIT 1`, task.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return findings.String, err
}

// LatestWorkflowPlan returns the plan of the newest planning step that
// finished, whole, for the next agents' {{plan}}: empty when that step
// failed, whatever an earlier visit produced.
func (r *AgentTaskRepository) LatestWorkflowPlan(ctx context.Context, task domain.AgentTask) (string, error) {
	var plan string
	err := r.db.GetContext(ctx, &plan, `SELECT plan FROM agent_task_workflow_steps WHERE task_id=$1 AND plan IS NOT NULL ORDER BY sequence DESC LIMIT 1`, task.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return plan, err
}

// WorkflowCI is the CI the current attempt's published commit reported
// through webhooks. GitHub only sends completed check suites, so Passed never
// proves that nothing is still running: the runner reads the commit's full
// check list from GitHub (Repository, CommitSHA) before it passes CI.
type WorkflowCI struct {
	Failed     []domain.AgentTaskSignal
	Passed     []domain.AgentTaskSignal
	Published  bool
	Repository string
	CommitSHA  string
}

// failedCIOutcomes and passedCIOutcomes classify GitHub check suite conclusions.
var (
	failedCIOutcomes = map[string]bool{"failure": true, "timed_out": true, "cancelled": true, "action_required": true, "startup_failure": true}
	passedCIOutcomes = map[string]bool{"success": true, "neutral": true, "skipped": true}
)

// WorkflowCI reads the suites webhooks reported for the published commit of
// the current attempt only (KUA-215): a suite of an earlier commit, even of
// the same attempt, says nothing about this one. Without a published commit
// there is nothing to report.
func (r *AgentTaskRepository) WorkflowCI(ctx context.Context, task domain.AgentTask) (*WorkflowCI, error) {
	out := &WorkflowCI{}
	task, err := workflowPublicationTask(ctx, r.db, task)
	if err != nil {
		return nil, err
	}
	if target, err := reviewTarget(ctx, r.db, task); err == nil {
		out.Published, out.Repository, out.CommitSHA = true, target.Repository, target.CommitSHA
	} else if !errors.Is(err, ErrNoReviewTarget) {
		return nil, err
	}
	if out.CommitSHA == "" {
		return out, nil
	}
	var signals []domain.AgentTaskSignal
	if err := r.db.SelectContext(ctx, &signals, `SELECT * FROM agent_task_signals WHERE task_id=$1 AND run_id=$2 AND kind='ci' AND head_sha=$3 ORDER BY updated_at`, task.ID, task.RunID, out.CommitSHA); err != nil {
		return nil, err
	}
	for _, signal := range signals {
		switch {
		case failedCIOutcomes[signal.Outcome]:
			out.Failed = append(out.Failed, signal)
		case passedCIOutcomes[signal.Outcome]:
			out.Passed = append(out.Passed, signal)
		}
	}
	return out, nil
}

// HasPublishedWork reports whether this step has a published pull request.
func (r *AgentTaskRepository) HasPublishedWork(ctx context.Context, task domain.AgentTask) (bool, error) {
	_, err := r.WorkflowPullRequest(ctx, task)
	if errors.Is(err, ErrNoReviewTarget) {
		return false, nil
	}
	return err == nil, err
}

// ValidationOutput is the failing check's recorded summary for the next
// agent's {{failing_checks}}.
// WorkflowRun is the task's current run as the next step sees it: its
// status, the agent's final message and the files it changed.
type WorkflowRun struct {
	Status       string
	Summary      string
	ChangedFiles []string
	// FinalMessage: Summary is the final message the harness reported, not
	// its raw output; SummaryTruncated: that message was cut to fit.
	FinalMessage     bool
	SummaryTruncated bool
	// WorktreeChanged is whether the run changed the task's worktree
	// (uncommitted changes and commits alike), from Kuayle's own comparison
	// before and after it; nil when it could not be made. WorktreeFiles
	// lists what changed.
	WorktreeBefore, WorktreeAfter string
	WorktreeChanged               *bool
	WorktreeFiles                 []string
	// NotebookNote says that the run did not update the task notebook and
	// Kuayle recorded its final message instead; empty when it did.
	NotebookNote string
}

func (r *AgentTaskRepository) WorkflowRun(ctx context.Context, task domain.AgentTask) (*WorkflowRun, error) {
	var row struct {
		Status    string          `db:"status"`
		Summary   *string         `db:"summary"`
		Files     json.RawMessage `db:"changed_files"`
		Final     bool            `db:"final_message"`
		Truncated bool            `db:"summary_truncated"`
		Before    string          `db:"worktree_before"`
		After     string          `db:"worktree_after"`
		Changed   *bool           `db:"worktree_changed"`
		Worktree  json.RawMessage `db:"worktree_changed_files"`
		Notebook  *string         `db:"notebook_problem"`
	}
	err := r.db.GetContext(ctx, &row, `SELECT status::text AS status,summary,changed_files,worktree_changed,worktree_changed_files,notebook_problem,COALESCE(worktree_before,'') AS worktree_before,COALESCE(environment->>'worktree_after','') AS worktree_after,
 COALESCE(result->>'final_message','')='true' AS final_message,COALESCE(result->>'summary_truncated','')='true' AS summary_truncated
 FROM dev_machine_agent_runs WHERE id=$1 AND task_id=$2`, task.RunID, task.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return &WorkflowRun{}, nil
	}
	if err != nil {
		return nil, err
	}
	run := &WorkflowRun{Status: row.Status, FinalMessage: row.Final, SummaryTruncated: row.Truncated, WorktreeChanged: row.Changed, WorktreeBefore: row.Before, WorktreeAfter: row.After}
	_ = json.Unmarshal(row.Files, &run.ChangedFiles)
	_ = json.Unmarshal(row.Worktree, &run.WorktreeFiles)
	if row.Summary != nil {
		run.Summary = *row.Summary
	}
	if row.Notebook != nil && domain.IsNotebookNotUpdated(*row.Notebook) {
		run.NotebookNote = *row.Notebook
	}
	return run, nil
}

// SetWorkflowWake records when the worker should evaluate the waiting step
// again (nil: only on an event), fenced by the worker lease.
func (r *AgentTaskRepository) SetWorkflowWake(ctx context.Context, claimed domain.AgentTask, at *time.Time) error {
	res, err := r.db.ExecContext(ctx, `UPDATE agent_tasks SET workflow_wake_at=$3 WHERE id=$1 AND lease_token=$2 AND workflow_sequence=$4`, claimed.ID, claimed.LeaseToken, at, claimed.WorkflowSequence)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrAgentTaskConflict
	}
	return nil
}

// RecordWorkflowCI stores what the CI check step the task waits on knows so
// far (KUA-215: the details asked for, a re-run requested) and when to
// evaluate it again, fenced by the worker lease like SetWorkflowWake.
func (r *AgentTaskRepository) RecordWorkflowCI(ctx context.Context, claimed domain.AgentTask, record domain.WorkflowCIRecord, wake *time.Time) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE agent_tasks SET workflow_wake_at=$3 WHERE id=$1 AND lease_token=$2 AND workflow_sequence=$4 AND workflow_status='waiting'`, claimed.ID, claimed.LeaseToken, wake, claimed.WorkflowSequence)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrAgentTaskConflict
	}
	res, err = tx.ExecContext(ctx, `UPDATE agent_task_workflow_steps SET ci=$3 WHERE task_id=$1 AND sequence=$2 AND finished_at IS NULL AND node_type='check'`, claimed.ID, claimed.WorkflowSequence, data)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrAgentTaskConflict
	}
	return tx.Commit()
}

// RecordWorkflowBranch stores what the step the task waits on did on its
// pull request's branch, with the facts it read (KUA-265), and when to
// evaluate it again, fenced by the worker lease like SetWorkflowWake. Any
// waiting step records it in its ci, next to what a CI step judged.
func (r *AgentTaskRepository) RecordWorkflowBranch(ctx context.Context, claimed domain.AgentTask, branch domain.WorkflowBranchRecord, wake *time.Time) error {
	data, err := json.Marshal(branch)
	if err != nil {
		return err
	}
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE agent_tasks SET workflow_wake_at=$3 WHERE id=$1 AND lease_token=$2 AND workflow_sequence=$4 AND workflow_status='waiting'`, claimed.ID, claimed.LeaseToken, wake, claimed.WorkflowSequence)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrAgentTaskConflict
	}
	res, err = tx.ExecContext(ctx, `UPDATE agent_task_workflow_steps SET ci=jsonb_set(COALESCE(ci,'{}'::jsonb),'{branch}',$3::jsonb) WHERE task_id=$1 AND sequence=$2 AND finished_at IS NULL`, claimed.ID, claimed.WorkflowSequence, data)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrAgentTaskConflict
	}
	return tx.Commit()
}

// WorkflowCIHistory lists the task's CI check steps that judged the commit,
// oldest first, the open one included (KUA-215).
func (r *AgentTaskRepository) WorkflowCIHistory(ctx context.Context, task domain.AgentTask, head string) ([]domain.WorkflowStep, error) {
	steps := []domain.WorkflowStep{}
	if head == "" {
		return steps, nil
	}
	err := r.db.SelectContext(ctx, &steps, `SELECT * FROM agent_task_workflow_steps WHERE task_id=$1 AND node_type='check' AND ci->>'head'=$2 AND sequence >= $3 ORDER BY sequence`, task.ID, head, task.WorkflowSwitchSequence)
	return steps, err
}

// DeferredShipMerge is the merge a person approved at a ship approval whose
// approved path runs through agent steps to a done end (KUA-216): the newest
// step carrying a deferred merge, as long as every step entered after it is an
// agent step. Once the work left those agent steps (to a check, a
// publication, another approval), the approval no longer covers what the task
// ships, and it returns nil.
func (r *AgentTaskRepository) DeferredShipMerge(ctx context.Context, task domain.AgentTask) (*domain.MergeWait, error) {
	var step domain.WorkflowStep
	err := r.db.GetContext(ctx, &step, `SELECT * FROM agent_task_workflow_steps s WHERE s.task_id=$1 AND s.node_type='approval' AND s.finished_at IS NOT NULL AND s.merge->>'deferred'='true' AND s.sequence >= $2
 AND NOT EXISTS(SELECT 1 FROM agent_task_workflow_steps x WHERE x.task_id=s.task_id AND x.sequence>s.sequence AND x.node_type<>'agent')
 ORDER BY s.sequence DESC LIMIT 1`, task.ID, task.WorkflowSwitchSequence)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return step.MergeWait(), nil
}

// RetryWorkflow places a task a person retries back on its latest
// implementing agent step, as a new visit outside the guards: retrying is
// the person's decision. A task whose budget is used up gets another budget
// of the same size and its time budget restarts, as when a person continues
// past the budget. Called in Retry's transaction.
func retryWorkflow(ctx context.Context, tx *sqlx.Tx, task *domain.AgentTask, actor uuid.UUID) error {
	if !task.RunsWorkflow() {
		return nil
	}
	if task.Pipeline {
		// A pipeline task starts again with its writer's turn, on another
		// budget of the same size; its tick asks every role again and counts
		// stalls anew.
		return tx.GetContext(ctx, task, `UPDATE agent_tasks SET workflow_status='running',workflow_wake_at=NULL,workflow_sequence=workflow_sequence+1,workflow_iterations=workflow_iterations+1,
 workflow_budget_extensions=workflow_budget_extensions+1,workflow_budget_from=now(),workflow_held_since=NULL,pipeline_state=pipeline_state-'stall'-'asked'-'merge' WHERE id=$1 RETURNING *`, task.ID)
	}
	// The step is looked up in the version the task runs now: a task moved to
	// a newer version goes back to its latest implementing step that version
	// still has, never to a step that no longer exists.
	var raw json.RawMessage
	if err := tx.GetContext(ctx, &raw, `SELECT graph FROM workflow_versions WHERE id=$1 AND workspace_id=$2`, *task.WorkflowVersionID, task.WorkspaceID); err != nil {
		return err
	}
	graph, err := domain.ParseWorkflowGraph(raw)
	if err != nil {
		return err
	}
	var steps []domain.WorkflowStep
	if err = tx.SelectContext(ctx, &steps, `SELECT * FROM agent_task_workflow_steps WHERE task_id=$1 AND generation IS NOT NULL AND sequence >= $2 ORDER BY sequence DESC`, task.ID, task.WorkflowSwitchSequence); err != nil {
		return err
	}
	var step domain.WorkflowStep
	found := false
	for _, candidate := range steps {
		if node, ok := graph.Node(candidate.NodeID); ok && (node.Type == domain.WorkflowNodeAgent || node.Supervised()) {
			step, found = candidate, true
			break
		}
	}
	if !found {
		return ErrAgentTaskConflict
	}
	cursor := task.Cursor()
	if _, err = tx.ExecContext(ctx, `UPDATE agent_task_workflow_steps SET outcome=COALESCE(outcome,'retried'),finished_at=COALESCE(finished_at,now()) WHERE task_id=$1 AND sequence=$2`, task.ID, task.WorkflowSequence); err != nil {
		return err
	}
	cursor.Visits[step.NodeID]++
	// A retry of a task whose budget is used up continues past it, exactly as
	// a person continuing does: another budget of the same size, and the time
	// budget starts again now.
	now := time.Now()
	// A retry leaves any wait, including a person's, so the held time is added
	// to the budget before deciding whether this retry needs a new one.
	cursor = cursor.Released(now)
	exhausted := (graph.Budget.MaxIterations > 0 && cursor.Iterations >= graph.Budget.IterationAllowance(cursor)) ||
		(graph.BudgetDeadline(cursor) != nil && !now.Before(*graph.BudgetDeadline(cursor)))
	if exhausted {
		cursor.Extensions++
		cursor.BudgetFrom = now
	}
	cursor.Iterations++
	visits, _ := json.Marshal(cursor.Visits)
	sequence := task.WorkflowSequence + 1
	// A retry of the same supervised stage keeps the approval and merge
	// evidence already attached to that visit.
	var merge *json.RawMessage
	var observed bool
	if step.StageFirstRunID != nil && step.Sequence == task.WorkflowSequence {
		merge, observed = step.Merge, step.StageMergeObserved
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO agent_task_workflow_steps(task_id,sequence,node_id,node_type,visit,from_node,reason,generation,decided_by,stage_first_run_id,stage_input_run_id,merge,stage_merge_observed) VALUES($1,$2,$3,$4,$5,$6,'Retried by a person',$7,$8,$9,$10,$11,$12)`,
		task.ID, sequence, step.NodeID, step.NodeType, cursor.Visits[step.NodeID], cursor.Node, task.Generation, actor, step.StageFirstRunID, step.StageInputRunID, merge, observed); err != nil {
		return err
	}
	return tx.GetContext(ctx, task, `UPDATE agent_tasks SET workflow_node=$2,workflow_visits=$3,workflow_iterations=$4,workflow_sequence=$5,workflow_status='running',workflow_wake_at=NULL,workflow_budget_extensions=$6,workflow_budget_from=$7,workflow_held_since=NULL WHERE id=$1 RETURNING *`,
		task.ID, step.NodeID, visits, cursor.Iterations, sequence, cursor.Extensions, nullTime(cursor.BudgetFrom))
}

// joinInstructions appends a workflow step's trusted instructions (its
// profile's and the step's own, written by owners and admins) to the
// inherited project instructions.
func joinInstructions(inherited, workflow string) string {
	inherited, workflow = strings.TrimSpace(inherited), strings.TrimSpace(workflow)
	if inherited == "" {
		return workflow
	}
	if workflow == "" {
		return inherited
	}
	return inherited + "\n\n" + workflow
}

// ScopeInstructions are the inherited agent instructions of the task's
// issue, which each workflow iteration combines with its step's profile.
func (r *AgentTaskRepository) ScopeInstructions(ctx context.Context, task domain.AgentTask) (string, error) {
	tx, err := r.db.BeginTxx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	return resolveScopeDefault(ctx, tx, task.WorkspaceID, task.IssueID, "agent_instructions")
}

// ProfileBundle returns the instructions and skills of the agent profile the
// task's current workflow step runs, or nil when the task follows no workflow
// or its step has no profile. Skills are read as they are now: only active,
// enabled skills shared with the whole workspace are given, so a skill turned
// off or moved to a team after it was attached never reaches the agent.
func (r *AgentTaskRepository) ProfileBundle(ctx context.Context, task domain.AgentTask) (*domain.AgentProfileBundle, error) {
	// A task run as an agent identity (an assigned or mentioned agent) uses
	// that profile; a workflow task uses its current step's.
	profileID := task.AgentProfileID
	var stageInstructions string
	if task.RunsWorkflow() {
		if task.WorkflowNode == nil {
			return nil, nil
		}
		var raw json.RawMessage
		err := r.db.GetContext(ctx, &raw, `SELECT graph FROM workflow_versions WHERE id=$1 AND workspace_id=$2`, task.WorkflowVersionID, task.WorkspaceID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		if task.Pipeline {
			// A pipeline task's turns are its writer's.
			pipeline, err := domain.ParsePipeline(raw)
			if err != nil {
				return nil, err
			}
			writer, _ := pipeline.Writer()
			profileID = &writer.ProfileID
		} else {
			var graph domain.WorkflowGraph
			if err = json.Unmarshal(raw, &graph); err != nil {
				return nil, err
			}
			node, ok := graph.Node(*task.WorkflowNode)
			if !ok {
				return nil, nil
			}
			profileID = node.ProfileID
			if node.Supervised() {
				stageInstructions = graph.StageInstructions(node)
			}
		}
	}
	if profileID == nil {
		return nil, nil
	}
	var profile domain.AgentProfile
	err := r.db.GetContext(ctx, &profile, `SELECT * FROM agent_profiles WHERE id=$1 AND workspace_id=$2`, *profileID, task.WorkspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	bundle := &domain.AgentProfileBundle{StageInstructions: stageInstructions, Name: profile.Name, Role: profile.Role, Behaviour: profile.Behaviour, Instructions: profile.Instructions, Skills: []domain.AgentProfileSkill{}}
	err = r.db.SelectContext(ctx, &bundle.Skills, `SELECT s.name AS name,s.description AS description,s.body AS body FROM agent_profile_skills ps
 JOIN assistant_skills s ON s.id=ps.skill_id AND s.workspace_id=ps.workspace_id
 WHERE ps.profile_id=$1 AND ps.workspace_id=$2 AND s.team_id IS NULL AND s.status='active' AND s.enabled ORDER BY ps.position`, profile.ID, task.WorkspaceID)
	return bundle, err
}
