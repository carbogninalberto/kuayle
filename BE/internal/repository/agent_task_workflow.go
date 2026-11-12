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
	WorkflowEnterValidate  = "validate"
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
	// Hold keeps the task on its current step, unfinished and without moves:
	// the time budget ran out while it waited (Entry fails or pauses), or a
	// person continued past that and it waits again (Entry waits).
	Hold bool
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
// another).
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
	if !domain.ValidWorkflowTaskTransition(task.State, to) || strings.TrimSpace(reason) == "" {
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
	graph, err := domain.ParseWorkflowGraph(version.Graph)
	if err != nil {
		return err
	}
	start, ok := graph.Start()
	if !ok {
		return ErrConversationInvalid
	}
	decision := graph.Decide(domain.WorkflowCursor{Node: start.ID, Visits: map[string]int{}}, domain.WorkflowElse, time.Now(), nil)
	if decision.Stuck || decision.To.Type != domain.WorkflowNodeAgent {
		return ErrConversationInvalid
	}
	visits, _ := json.Marshal(map[string]int{start.ID: 1, decision.To.ID: 1})
	if err = tx.GetContext(ctx, task, `UPDATE agent_tasks SET workflow_version_id=$2,workflow_node=$3,workflow_visits=$4,workflow_iterations=1,workflow_started_at=now(),workflow_status='running',workflow_sequence=2 WHERE id=$1 RETURNING *`,
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
	result := WorkflowResult{Cursor: task.Cursor(), Sequence: apply.Sequence, Status: domain.WorkflowStatusRunning}
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
		} else if to.Type == domain.WorkflowNodeEnd {
			outcome := to.Outcome
			step.Outcome, step.FinishedAt = &outcome, &now
		}
		result.Steps = append(result.Steps, step)
		result.FinalSequence = result.Sequence
	}
	if apply.Iteration && apply.Entry.Kind == WorkflowEnterIteration {
		result.Cursor.Iterations++
	}
	if apply.ExtendBudget {
		result.Cursor.Extensions++
		result.Cursor.BudgetFrom = now
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
	case WorkflowEnterValidate:
		return "validating"
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
	if apply.Hold {
		// Only an unfinished step is held: a waiting one stops (fails or
		// pauses), and a paused one waits again. A hold keeps the sequence,
		// so the status is what fences a replayed decision.
		want := domain.WorkflowStatusWaiting
		switch apply.Entry.Kind {
		case WorkflowEnterWait:
			want = domain.WorkflowStatusPaused
		case WorkflowEnterFail, WorkflowEnterPause:
		default:
			return ErrAgentTaskConflict
		}
		if task.WorkflowStatus == nil || *task.WorkflowStatus != want {
			return ErrAgentTaskConflict
		}
		var open bool
		if err := tx.GetContext(ctx, &open, `SELECT EXISTS(SELECT 1 FROM agent_task_workflow_steps WHERE task_id=$1 AND sequence=$2 AND finished_at IS NULL)`, task.ID, apply.Sequence); err != nil {
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
	} else if _, err := tx.ExecContext(ctx, `UPDATE agent_task_workflow_steps SET outcome=COALESCE(NULLIF($3,''),outcome),detail=CASE WHEN $4='' THEN detail ELSE $4 END,decided_by=COALESCE($5,decided_by),finished_at=COALESCE(finished_at,now()) WHERE task_id=$1 AND sequence=$2`,
		task.ID, apply.Sequence, apply.Outcome, truncateBytes(apply.Detail, 16000), apply.Actor); err != nil {
		return err
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
	if err = tx.GetContext(ctx, task, `UPDATE agent_tasks SET workflow_node=$2,workflow_visits=$3,workflow_iterations=$4,workflow_sequence=$5,workflow_status=$6,workflow_wake_at=$7,workflow_budget_extensions=$8,workflow_budget_from=$9 WHERE id=$1 RETURNING *`,
		task.ID, result.Cursor.Node, visits, result.Cursor.Iterations, result.Sequence, result.Status, result.Wake, result.Cursor.Extensions, nullTime(result.Cursor.BudgetFrom)); err != nil {
		return err
	}
	reason := apply.Entry.Reason
	switch apply.Entry.Kind {
	case WorkflowEnterIteration:
		if err = workflowIteration(ctx, tx, task, apply.Entry, apply.Actor); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE agent_task_workflow_steps SET generation=$3 WHERE task_id=$1 AND sequence=$2`, task.ID, result.FinalSequence, task.Generation)
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
	return err
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
	if err = tx.GetContext(ctx, task, `UPDATE agent_tasks t SET prompt=$2,agent_instructions=$3,provider=$4,model=$5,thinking=$6,model_server_id=$7,generation=generation+1,run_id=gen_random_uuid(),failure_count=0,failure_pending=false,failure_class=NULL,sign_in_required=false,usage_limit_message=NULL,usage_limit_resets_at=NULL,usage_limit_resume_at=NULL,resume_native_session=false,lease_token=NULL,lease_expires_at=NULL,next_poll_at=now(),issue_status_id=i.status_id,issue_updated_at=i.updated_at
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
	published, err := reviewTarget(ctx, tx, *task)
	if err != nil {
		return nil, err
	}
	input := *entry.Review
	// One review per step: replaying the step's transaction finds it again.
	input.RequestID = uuid.NewSHA1(workflowNamespace, []byte(fmt.Sprintf("%s/review/%d", task.ID, sequence)))
	review, _, err := insertReviewTask(ctx, tx, *task, *task.ConversationID, task.CreatorID, input, published)
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

// insertReviewTask creates a review task of the reviewed task's published
// work and its review record, in the caller's transaction.
func insertReviewTask(ctx context.Context, tx *sqlx.Tx, reviewed domain.AgentTask, conversation, creator uuid.UUID, input domain.AgentReviewRequest, published *domain.AgentReviewTarget) (*domain.AgentTask, *domain.AgentReview, error) {
	if input.ModelServerID == nil {
		var err error
		if input.ModelServerID, err = defaultModelServer(ctx, tx, reviewed.WorkspaceID, input.Provider); err != nil {
			return nil, nil, err
		}
	}
	var attempt int
	if err := tx.GetContext(ctx, &attempt, `SELECT COALESCE(max(attempt),0)+1 FROM agent_reviews WHERE reviewed_task_id=$1 AND commit_sha=$2 AND provider=$3`, reviewed.ID, published.CommitSHA, input.Provider); err != nil {
		return nil, nil, err
	}
	prompt := domain.AgentReviewPrompt(*published, input.Focus)
	var task domain.AgentTask
	err := tx.GetContext(ctx, &task, `INSERT INTO agent_tasks(workspace_id,conversation_id,creator_id,issue_id,team_id,request_id,prompt,provider,model,thinking,model_server_id,reason,issue_status_id,issue_updated_at,kind,review_of_task_id)
 SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'Waiting for a machine to review the pull request',i.status_id,i.updated_at,'review',$12 FROM issues i WHERE i.id=$4 AND i.workspace_id=$1 AND i.team_id=$5 RETURNING *`,
		reviewed.WorkspaceID, conversation, creator, reviewed.IssueID, reviewed.TeamID, input.RequestID, prompt, input.Provider, input.Model, input.Thinking, input.ModelServerID, reviewed.ID)
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
	if err = recordAgentTaskTransition(ctx, tx, task, "", &creator); err != nil {
		return nil, nil, err
	}
	return &task, review, nil
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
 COALESCE(r.result->>'replies_unknown','')='true' AS replies_unknown`

// StepRuns lists the task's agent runs for its timeline, oldest first, under
// the task's authorization: its newest 200 runs, each on the agent step that
// was the latest one entered when the run was created (a resumed step has
// several), and the runs of the review tasks its reviewer steps created. A
// review task has the creator, conversation and issue of the task it reviews,
// so whoever reads the task reads its reviews. A finished run's summary never
// changes: summariesSince leaves out those of runs that completed at or
// before it, which a view polling the task already holds.
func (r *AgentTaskRepository) StepRuns(ctx context.Context, ws, id uuid.UUID, summariesSince *time.Time) ([]domain.WorkflowStepRun, error) {
	if _, err := conversationActor(ctx, ws); err != nil {
		return nil, err
	}
	tx, _, err := r.transaction(ctx, id, true)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	runs := []domain.WorkflowStepRun{}
	err = tx.SelectContext(ctx, &runs, `SELECT * FROM (
 (SELECT `+stepRunColumns+`,(SELECT max(s.sequence) FROM agent_task_workflow_steps s WHERE s.task_id=r.task_id AND s.generation IS NOT NULL AND s.entered_at<=r.created_at)::int AS step
  FROM dev_machine_agent_runs r WHERE r.task_id=$1 AND r.workspace_id=$2 ORDER BY r.created_at DESC,r.id DESC LIMIT 200)
 UNION ALL
 (SELECT `+stepRunColumns+`,s.sequence AS step
  FROM agent_task_workflow_steps s JOIN agent_tasks t ON t.id=s.review_task_id AND t.workspace_id=$2 AND t.kind='review' AND t.review_of_task_id=$1
  JOIN dev_machine_agent_runs r ON r.task_id=t.id AND r.workspace_id=$2 WHERE s.task_id=$1 ORDER BY r.created_at DESC,r.id DESC LIMIT 200)
 ) runs ORDER BY created_at,id`, id, ws, summariesSince)
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
}

func (r *AgentTaskRepository) WorkflowReview(ctx context.Context, task domain.AgentTask, review uuid.UUID) (*WorkflowReview, error) {
	out := &WorkflowReview{}
	err := r.db.GetContext(ctx, out, `SELECT t.state,v.verdict,v.findings FROM agent_tasks t LEFT JOIN agent_reviews v ON v.task_id=t.id
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

func (r *AgentTaskRepository) WorkflowCI(ctx context.Context, task domain.AgentTask) (*WorkflowCI, error) {
	out := &WorkflowCI{}
	if target, err := reviewTarget(ctx, r.db, task); err == nil {
		out.Published, out.Repository, out.CommitSHA = true, target.Repository, target.CommitSHA
	} else if !errors.Is(err, ErrNoReviewTarget) {
		return nil, err
	}
	var signals []domain.AgentTaskSignal
	if err := r.db.SelectContext(ctx, &signals, `SELECT * FROM agent_task_signals WHERE task_id=$1 AND run_id=$2 AND kind='ci' ORDER BY updated_at`, task.ID, task.RunID); err != nil {
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

// HasPublishedWork reports whether the task's current attempt has a pull
// request Kuayle published (what reviewers and CI checks need).
func (r *AgentTaskRepository) HasPublishedWork(ctx context.Context, task domain.AgentTask) (bool, error) {
	_, err := reviewTarget(ctx, r.db, task)
	if errors.Is(err, ErrNoReviewTarget) {
		return false, nil
	}
	return err == nil, err
}

// ValidationOutput is the failing check's recorded summary for the next
// agent's {{failing_checks}}.
func (r *AgentTaskRepository) ValidationOutput(ctx context.Context, task domain.AgentTask) (string, error) {
	var summary sql.NullString
	err := r.db.GetContext(ctx, &summary, `SELECT COALESCE(summary,'') FROM dev_machine_agent_run_steps WHERE agent_run_id=$1 AND sequence=0 AND step_type='validation'`, task.RunID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return summary.String, err
}

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
	WorktreeChanged *bool
	WorktreeFiles   []string
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
		Changed   *bool           `db:"worktree_changed"`
		Worktree  json.RawMessage `db:"worktree_changed_files"`
		Notebook  *string         `db:"notebook_problem"`
	}
	err := r.db.GetContext(ctx, &row, `SELECT status::text AS status,summary,changed_files,worktree_changed,worktree_changed_files,notebook_problem,
 COALESCE(result->>'final_message','')='true' AS final_message,COALESCE(result->>'summary_truncated','')='true' AS summary_truncated
 FROM dev_machine_agent_runs WHERE id=$1 AND task_id=$2`, task.RunID, task.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return &WorkflowRun{}, nil
	}
	if err != nil {
		return nil, err
	}
	run := &WorkflowRun{Status: row.Status, FinalMessage: row.Final, SummaryTruncated: row.Truncated, WorktreeChanged: row.Changed}
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

// RetryWorkflow places a task a person retries back on its latest
// implementing agent step, as a new visit outside the guards: retrying is
// the person's decision. A task whose budget is used up gets another budget
// of the same size and its time budget restarts, as when a person continues
// past the budget. Called in Retry's transaction.
func retryWorkflow(ctx context.Context, tx *sqlx.Tx, task *domain.AgentTask, actor uuid.UUID) error {
	if !task.RunsWorkflow() {
		return nil
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
	if err = tx.SelectContext(ctx, &steps, `SELECT * FROM agent_task_workflow_steps WHERE task_id=$1 AND generation IS NOT NULL ORDER BY sequence DESC`, task.ID); err != nil {
		return err
	}
	var step domain.WorkflowStep
	found := false
	for _, candidate := range steps {
		if node, ok := graph.Node(candidate.NodeID); ok && node.Type == domain.WorkflowNodeAgent {
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
	exhausted := (graph.Budget.MaxIterations > 0 && cursor.Iterations >= graph.Budget.IterationAllowance(cursor)) ||
		(graph.BudgetDeadline(cursor) != nil && !now.Before(*graph.BudgetDeadline(cursor)))
	if exhausted {
		cursor.Extensions++
		cursor.BudgetFrom = now
	}
	cursor.Iterations++
	visits, _ := json.Marshal(cursor.Visits)
	sequence := task.WorkflowSequence + 1
	if _, err = tx.ExecContext(ctx, `INSERT INTO agent_task_workflow_steps(task_id,sequence,node_id,node_type,visit,from_node,reason,generation,decided_by) VALUES($1,$2,$3,$4,$5,$6,'Retried by a person',$7,$8)`,
		task.ID, sequence, step.NodeID, step.NodeType, cursor.Visits[step.NodeID], cursor.Node, task.Generation, actor); err != nil {
		return err
	}
	return tx.GetContext(ctx, task, `UPDATE agent_tasks SET workflow_node=$2,workflow_visits=$3,workflow_iterations=$4,workflow_sequence=$5,workflow_status='running',workflow_wake_at=NULL,workflow_budget_extensions=$6,workflow_budget_from=$7 WHERE id=$1 RETURNING *`,
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
		var graph domain.WorkflowGraph
		if err = json.Unmarshal(raw, &graph); err != nil {
			return nil, err
		}
		node, ok := graph.Node(*task.WorkflowNode)
		if !ok {
			return nil, nil
		}
		profileID = node.ProfileID
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
	bundle := &domain.AgentProfileBundle{Name: profile.Name, Role: profile.Role, Behaviour: profile.Behaviour, Instructions: profile.Instructions, Skills: []domain.AgentProfileSkill{}}
	err = r.db.SelectContext(ctx, &bundle.Skills, `SELECT s.name AS name,s.description AS description,s.body AS body FROM agent_profile_skills ps
 JOIN assistant_skills s ON s.id=ps.skill_id AND s.workspace_id=ps.workspace_id
 WHERE ps.profile_id=$1 AND ps.workspace_id=$2 AND s.team_id IS NULL AND s.status='active' AND s.enabled ORDER BY ps.position`, profile.ID, task.WorkspaceID)
	return bundle, err
}
