package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
)

// The workflow runner executes a task's pinned workflow version node by node.
// The task worker reports each outcome of the node the task is on (the agent
// finished or failed, the check passed, the pull request was published); the
// runner evaluates waiting nodes (a reviewer's verdict, CI, a timeout) when
// an event wakes them, and applies people's decisions. Every move is one
// fenced transaction (repository.ApplyWorkflow), so a restart resumes from
// the persisted cursor and nothing runs twice. Each node reuses the task's
// existing machinery: an implementing agent step is an iteration, a reviewer
// step a review task, a tests check the independent validation, a publish
// step the publication, and waiting nodes wait in awaiting_review.

type workflowTaskStore interface {
	ApplyWorkflow(context.Context, domain.AgentTask, repository.WorkflowApply) (*domain.AgentTask, error)
	CurrentWorkflowStep(context.Context, domain.AgentTask) (*domain.WorkflowStep, error)
	WorkflowReview(context.Context, domain.AgentTask, uuid.UUID) (*repository.WorkflowReview, error)
	LatestWorkflowReview(context.Context, domain.AgentTask) (string, error)
	WorkflowCI(context.Context, domain.AgentTask) (*repository.WorkflowCI, error)
	HasPublishedWork(context.Context, domain.AgentTask) (bool, error)
	ValidationOutput(context.Context, domain.AgentTask) (string, error)
	WorkflowRun(context.Context, domain.AgentTask) (*repository.WorkflowRun, error)
	SetWorkflowWake(context.Context, domain.AgentTask, *time.Time) error
	ScopeInstructions(context.Context, domain.AgentTask) (string, error)
	WorkflowDecisionTask(context.Context, uuid.UUID, uuid.UUID) (*domain.AgentTask, error)
	DecideWorkflow(context.Context, uuid.UUID, uuid.UUID, repository.WorkflowApply) (*domain.AgentTask, error)
}

type workflowDefinitionStore interface {
	Version(context.Context, uuid.UUID, uuid.UUID) (*domain.WorkflowVersion, error)
	AllProfiles(context.Context, uuid.UUID) (map[uuid.UUID]domain.AgentProfile, error)
}

// workflowPolicy checks an agent profile against the workspace policy
// (allowed providers, model checks) before each step that runs it.
type workflowPolicy func(ctx context.Context, ws uuid.UUID, profile domain.AgentProfile) error

// WorkflowRunner advances tasks that run a configurable workflow.
type WorkflowRunner struct {
	tasks       workflowTaskStore
	definitions workflowDefinitionStore
	policy      workflowPolicy
	now         func() time.Time

	mu     sync.Mutex
	graphs map[uuid.UUID]*domain.WorkflowGraph
}

func newWorkflowRunner(tasks workflowTaskStore, definitions workflowDefinitionStore, policy workflowPolicy) *WorkflowRunner {
	return &WorkflowRunner{tasks: tasks, definitions: definitions, policy: policy, now: time.Now, graphs: map[uuid.UUID]*domain.WorkflowGraph{}}
}

// ciSettle lets other CI suites of the same commit report a failure before a
// success decides the check.
const ciSettle = time.Minute

// ErrWorkflowDecision is a decision the task's current step does not accept.
var ErrWorkflowDecision = errors.New("the workflow is not waiting for this decision")

// workflowStepMarker starts the workflow section of an iteration prompt; it
// shares the follow-up marker so the section replaces the previous one.
const workflowStepMarker = followUpMarker + "the Kuayle workflow, step "

func (w *WorkflowRunner) graph(ctx context.Context, task domain.AgentTask) (*domain.WorkflowGraph, map[uuid.UUID]domain.AgentProfile, error) {
	if !task.RunsWorkflow() {
		return nil, nil, ErrInvalidOperation
	}
	w.mu.Lock()
	graph := w.graphs[*task.WorkflowVersionID]
	w.mu.Unlock()
	if graph == nil {
		version, err := w.definitions.Version(ctx, task.WorkspaceID, *task.WorkflowVersionID)
		if err != nil {
			return nil, nil, err
		}
		if graph, err = domain.ParseWorkflowGraph(version.Graph); err != nil {
			return nil, nil, err
		}
		// Versions are immutable, so a parsed graph never goes stale.
		w.mu.Lock()
		w.graphs[*task.WorkflowVersionID] = graph
		w.mu.Unlock()
	}
	// Archived profiles still run the versions that pinned them.
	profiles, err := w.definitions.AllProfiles(ctx, task.WorkspaceID)
	return graph, profiles, err
}

// Outcome advances the task after its current node produced outcome.
func (w *WorkflowRunner) Outcome(ctx context.Context, task domain.AgentTask, outcome, detail string) error {
	apply, err := w.plan(ctx, task, outcome, detail, false)
	if err != nil {
		return err
	}
	_, err = w.tasks.ApplyWorkflow(ctx, task, *apply)
	return err
}

// plan computes the moves an outcome causes, entering nodes that finish at
// once (a reviewer with nothing published, a step the policy refuses) until
// it reaches a node that runs or waits, an end, or a limit.
func (w *WorkflowRunner) plan(ctx context.Context, task domain.AgentTask, outcome, detail string, ignoreLimit bool) (*repository.WorkflowApply, error) {
	graph, profiles, err := w.graph(ctx, task)
	if err != nil {
		return nil, err
	}
	now := w.now()
	cursor := task.Cursor()
	apply := &repository.WorkflowApply{Sequence: task.WorkflowSequence, Outcome: outcome, Detail: detail}
	vars, err := w.variables(ctx, task, *graph, profiles, cursor.Node, outcome, detail)
	if err != nil {
		return nil, err
	}
	for range len(graph.Nodes)*2 + 2 {
		decision := graph.Decide(cursor, outcome, now, profiles)
		reason := "Workflow · " + decision.Reason
		if decision.Stuck {
			apply.Moves = append(apply.Moves, repository.WorkflowMove{Decision: decision, Stop: true})
			apply.Entry = repository.WorkflowEntry{Kind: repository.WorkflowEnterFail, Reason: reason}
			return apply, nil
		}
		if decision.Limit != "" && !ignoreLimit {
			apply.Moves = append(apply.Moves, repository.WorkflowMove{Decision: decision, Stop: true})
			if graph.Budget.Policy() == domain.WorkflowOnLimitAsk {
				apply.Entry = repository.WorkflowEntry{Kind: repository.WorkflowEnterPause, Reason: reason + ". Continue or stop the workflow"}
			} else {
				apply.Entry = repository.WorkflowEntry{Kind: repository.WorkflowEnterFail, Reason: reason}
			}
			return apply, nil
		}
		ignoreLimit = false
		move := repository.WorkflowMove{Decision: decision}
		entry, immediate, why, err := w.enter(ctx, task, *graph, profiles, cursor, decision, reason, vars, now)
		if err != nil {
			return nil, err
		}
		if immediate == "" {
			apply.Moves = append(apply.Moves, move)
			apply.Entry = entry
			apply.Iteration = graph.Iteration(decision.To, profiles)
			return apply, nil
		}
		// The node finishes as soon as it is entered; leave it by its outcome.
		move.Immediate, move.Detail = immediate, why
		apply.Moves = append(apply.Moves, move)
		cursor.Node = decision.To.ID
		cursor.Visits[decision.To.ID]++
		outcome = immediate
	}
	return nil, fmt.Errorf("workflow %s: too many immediate transitions", task.ID)
}

// enter returns what entering the decision's target does, or the outcome
// it finishes with at once and why.
func (w *WorkflowRunner) enter(ctx context.Context, task domain.AgentTask, graph domain.WorkflowGraph, profiles map[uuid.UUID]domain.AgentProfile, cursor domain.WorkflowCursor, decision domain.WorkflowDecision, reason string, vars domain.WorkflowVariables, now time.Time) (repository.WorkflowEntry, string, string, error) {
	to := decision.To
	wake := func() *time.Time {
		if to.TimeoutMinutes <= 0 {
			return nil
		}
		at := now.Add(time.Duration(to.TimeoutMinutes) * time.Minute)
		return &at
	}
	switch to.Type {
	case domain.WorkflowNodeEnd:
		if to.Outcome == domain.WorkflowEndDone {
			return repository.WorkflowEntry{Kind: repository.WorkflowEnterComplete, Reason: reason}, "", "", nil
		}
		return repository.WorkflowEntry{Kind: repository.WorkflowEnterFail, Reason: reason}, "", "", nil
	case domain.WorkflowNodeAgent:
		profile, ok := profiles[*to.ProfileID]
		if !ok {
			return repository.WorkflowEntry{}, domain.WorkflowAgentFailed, to.Label + ": its agent profile no longer exists", nil
		}
		// Profiles never widen permissions: every step is checked against the
		// workspace policy as it is now.
		if err := w.policy(ctx, task.WorkspaceID, profile); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return repository.WorkflowEntry{}, "", "", err
			}
			return repository.WorkflowEntry{}, domain.WorkflowAgentFailed, fmt.Sprintf("The workspace policy does not allow %s to run %s: %v", profile.Name, harnessName(profile.Harness), err), nil
		}
		rendered := domain.RenderWorkflowPrompt(to.Prompt, vars)
		if domain.ReviewerRole(profile.Role) {
			published, err := w.tasks.HasPublishedWork(ctx, task)
			if err != nil {
				return repository.WorkflowEntry{}, "", "", err
			}
			if !published {
				return repository.WorkflowEntry{}, domain.WorkflowAgentFailed, to.Label + ": there is no published pull request to review", nil
			}
			focus := strings.TrimSpace(strings.TrimSpace(profile.Instructions) + "\n\n" + rendered)
			return repository.WorkflowEntry{Kind: repository.WorkflowEnterReview, Reason: reason + "; " + profile.Name + " reviews the pull request",
				Review: &domain.AgentReviewRequest{Provider: profile.Harness, Model: profile.Model, Thinking: profile.Effort, Focus: truncateUTF8Bytes(focus, 4000)}, WakeAt: wake()}, "", "", nil
		}
		scope, err := w.tasks.ScopeInstructions(ctx, task)
		if err != nil {
			return repository.WorkflowEntry{}, "", "", err
		}
		base, _, _ := strings.Cut(task.Prompt, followUpMarker)
		if rendered == "" {
			rendered = "Continue the task on the existing branch."
		}
		section := fmt.Sprintf("%s%s (visit %d):\n%s", workflowStepMarker, to.Label, cursor.Visits[to.ID]+1, rendered)
		if room := 32000 - len(section); len(base) > room {
			base = truncateRunes(base, max(room/4, 0))
		}
		return repository.WorkflowEntry{Kind: repository.WorkflowEnterIteration, Reason: reason,
			Prompt: base + section, Instructions: workflowInstructions(scope, profile, domain.WorkflowNode{}),
			Provider: profile.Harness, Model: profile.Model, Thinking: profile.Effort}, "", "", nil
	case domain.WorkflowNodeCheck:
		if to.Check == domain.WorkflowCheckCI {
			published, err := w.tasks.HasPublishedWork(ctx, task)
			if err != nil {
				return repository.WorkflowEntry{}, "", "", err
			}
			if !published {
				return repository.WorkflowEntry{}, domain.WorkflowChecksFailed, to.Label + ": nothing is published for CI to check; add a publish step before it", nil
			}
			return repository.WorkflowEntry{Kind: repository.WorkflowEnterWait, Reason: reason + "; waiting for CI on the pull request", WakeAt: wake()}, "", "", nil
		}
		run, err := w.tasks.WorkflowRun(ctx, task)
		if err != nil {
			return repository.WorkflowEntry{}, "", "", err
		}
		if run.Status != string(domain.DevMachineAgentRunStatusSucceeded) {
			return repository.WorkflowEntry{}, domain.WorkflowChecksFailed, to.Label + ": the last agent iteration did not finish, so there is nothing to check", nil
		}
		return repository.WorkflowEntry{Kind: repository.WorkflowEnterValidate, Reason: reason + "; independent validation is pending"}, "", "", nil
	case domain.WorkflowNodePublish:
		run, err := w.tasks.WorkflowRun(ctx, task)
		if err != nil {
			return repository.WorkflowEntry{}, "", "", err
		}
		if run.Status != string(domain.DevMachineAgentRunStatusSucceeded) {
			return repository.WorkflowEntry{}, domain.WorkflowPublishFailed, to.Label + ": the last agent iteration did not finish, so there is nothing to publish", nil
		}
		return repository.WorkflowEntry{Kind: repository.WorkflowEnterPublish, Reason: reason + "; publishing the pull request"}, "", "", nil
	case domain.WorkflowNodeApproval:
		question := strings.TrimSpace(to.Prompt)
		if question == "" {
			question = to.Label
		}
		return repository.WorkflowEntry{Kind: repository.WorkflowEnterWait, Reason: reason + "; waiting for approval: " + oneLine(question, 300), WakeAt: wake()}, "", "", nil
	case domain.WorkflowNodeMerge:
		return repository.WorkflowEntry{Kind: repository.WorkflowEnterWait, Reason: reason + "; waiting for the pull request to be merged (ship it from chat or merge it on GitHub)", WakeAt: wake()}, "", "", nil
	}
	return repository.WorkflowEntry{}, "", "", ErrInvalidOperation
}

// workflowInstructions are the trusted instructions of an agent step: the
// inherited project instructions, then its profile's, then (for the first
// iteration, which has no variables yet) the step's template.
func workflowInstructions(scope string, profile domain.AgentProfile, node domain.WorkflowNode) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(scope))
	section := func(title, body string) {
		if body = strings.TrimSpace(body); body == "" {
			return
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(title + ":\n" + body)
	}
	section(fmt.Sprintf("Agent profile %q (%s)", profile.Name, strings.ReplaceAll(profile.Role, "_", " ")), profile.Instructions)
	if node.ID != "" {
		section("Workflow step "+node.Label, domain.RenderWorkflowPrompt(node.Prompt, domain.WorkflowVariables{}))
	}
	return truncateUTF8Bytes(b.String(), 32000)
}

// variables collects what the finished node passes to the next agent: the
// previous agent's summary and changed files, the reviewer's findings and
// the failing checks' output. All of it is untrusted.
func (w *WorkflowRunner) variables(ctx context.Context, task domain.AgentTask, graph domain.WorkflowGraph, profiles map[uuid.UUID]domain.AgentProfile, from, outcome, detail string) (domain.WorkflowVariables, error) {
	vars := domain.WorkflowVariables{}
	run, err := w.tasks.WorkflowRun(ctx, task)
	if err != nil {
		return vars, err
	}
	vars.PreviousSummary = run.Summary
	if len(run.ChangedFiles) > 0 {
		files := run.ChangedFiles
		if len(files) > 100 {
			files = files[:100]
		}
		vars.Diff = "Files changed by the last iteration:\n- " + strings.Join(files, "\n- ")
	}
	node, _ := graph.Node(from)
	switch {
	case node.Type == domain.WorkflowNodeAgent && node.ProfileID != nil && domain.ReviewerRole(profiles[*node.ProfileID].Role) && outcome == domain.WorkflowReviewChangesRequested:
		if vars.ReviewComments, err = w.tasks.LatestWorkflowReview(ctx, task); err != nil {
			return vars, err
		}
		if vars.ReviewComments == "" {
			vars.ReviewComments = detail
		}
	case node.Type == domain.WorkflowNodeCheck && outcome == domain.WorkflowChecksFailed:
		vars.FailingChecks = detail
		if node.Check == domain.WorkflowCheckTests {
			output, err := w.tasks.ValidationOutput(ctx, task)
			if err != nil {
				return vars, err
			}
			vars.FailingChecks = strings.TrimSpace(output + "\n" + detail)
		}
	}
	return vars, nil
}

// Evaluate decides a waiting node from what was recorded since: the review
// task's verdict, the CI results of the published commit, or its timeout.
func (w *WorkflowRunner) Evaluate(ctx context.Context, task domain.AgentTask) error {
	graph, profiles, err := w.graph(ctx, task)
	if err != nil {
		return err
	}
	step, err := w.tasks.CurrentWorkflowStep(ctx, task)
	if err != nil {
		return err
	}
	node, ok := graph.Node(step.NodeID)
	if !ok || task.WorkflowStatus == nil || *task.WorkflowStatus != domain.WorkflowStatusWaiting || step.FinishedAt != nil {
		return w.tasks.SetWorkflowWake(ctx, task, nil)
	}
	now := w.now()
	var deadline *time.Time
	if node.TimeoutMinutes > 0 {
		at := step.EnteredAt.Add(time.Duration(node.TimeoutMinutes) * time.Minute)
		deadline = &at
	}
	outcome, detail := "", ""
	var wake *time.Time
	switch {
	case node.Type == domain.WorkflowNodeAgent && step.ReviewTaskID != nil:
		review, err := w.tasks.WorkflowReview(ctx, task, *step.ReviewTaskID)
		if err != nil {
			return err
		}
		switch review.State {
		case "completed":
			findings := ""
			if review.Findings != nil {
				findings = *review.Findings
			}
			// Only an explicit approval approves; a comment or a missing
			// verdict sends the work back.
			if review.Verdict != nil && *review.Verdict == "approve" {
				outcome, detail = domain.WorkflowReviewApproved, findings
			} else {
				outcome, detail = domain.WorkflowReviewChangesRequested, findings
			}
		case "failed", "cancelled":
			outcome, detail = domain.WorkflowAgentFailed, "The review did not finish ("+review.State+")"
		}
	case node.Type == domain.WorkflowNodeCheck && node.Check == domain.WorkflowCheckCI:
		ci, err := w.tasks.WorkflowCI(ctx, task)
		if err != nil {
			return err
		}
		if len(ci.Failed) > 0 {
			var lines []string
			for _, signal := range ci.Failed {
				line := fmt.Sprintf("CI check suite %q concluded %q", signal.Name, signal.Outcome)
				if signal.URL != "" {
					line += " (" + signal.URL + ")"
				}
				if strings.TrimSpace(signal.Summary) != "" {
					line += ":\n" + strings.TrimSpace(signal.Summary)
				}
				lines = append(lines, line)
			}
			outcome, detail = domain.WorkflowChecksFailed, strings.Join(lines, "\n\n")
		} else if len(ci.Passed) > 0 {
			latest := ci.Passed[len(ci.Passed)-1].UpdatedAt
			if settled := latest.Add(ciSettle); !now.Before(settled) {
				outcome, detail = domain.WorkflowChecksPassed, fmt.Sprintf("%d CI check suites passed", len(ci.Passed))
			} else {
				wake = &settled
			}
		}
	}
	_ = profiles
	if outcome == "" && deadline != nil && !now.Before(*deadline) {
		outcome, detail = domain.WorkflowTimeout, fmt.Sprintf("%s waited %d minutes", node.Label, node.TimeoutMinutes)
	}
	if outcome == "" {
		if wake == nil || (deadline != nil && deadline.Before(*wake)) {
			wake = deadline
		}
		return w.tasks.SetWorkflowWake(ctx, task, wake)
	}
	return w.Outcome(ctx, task, outcome, detail)
}

// Workflow decisions a person can make.
const (
	WorkflowDecisionApprove  = "approve"
	WorkflowDecisionDeny     = "deny"
	WorkflowDecisionContinue = "continue"
	WorkflowDecisionStop     = "stop"
)

// Decide applies a person's decision to the step the task is on: approving
// or denying an approval node, or continuing or stopping a workflow paused
// at a limit. Only the task's creator or a workspace owner or admin who can
// see the issue decides; sequence must name the step they saw.
func (w *WorkflowRunner) Decide(ctx context.Context, ws, id uuid.UUID, sequence int, decision string) (*domain.AgentTask, error) {
	task, err := w.tasks.WorkflowDecisionTask(ctx, ws, id)
	if err != nil {
		return nil, err
	}
	if !task.RunsWorkflow() || task.WorkflowSequence != sequence || task.WorkflowStatus == nil {
		return nil, ErrWorkflowDecision
	}
	graph, _, err := w.graph(ctx, *task)
	if err != nil {
		return nil, err
	}
	node, _ := graph.Node(*task.WorkflowNode)
	var apply *repository.WorkflowApply
	switch *task.WorkflowStatus {
	case domain.WorkflowStatusWaiting:
		if node.Type != domain.WorkflowNodeApproval || task.State != "awaiting_review" {
			return nil, ErrWorkflowDecision
		}
		outcome := domain.WorkflowApprovalApproved
		switch decision {
		case WorkflowDecisionApprove:
		case WorkflowDecisionDeny:
			outcome = domain.WorkflowApprovalDenied
		default:
			return nil, ErrWorkflowDecision
		}
		if apply, err = w.plan(ctx, *task, outcome, "", false); err != nil {
			return nil, err
		}
	case domain.WorkflowStatusPaused:
		step, err := w.tasks.CurrentWorkflowStep(ctx, *task)
		if err != nil {
			return nil, err
		}
		switch decision {
		case WorkflowDecisionContinue:
			// Continue past the limit once, with the outcome that reached it.
			outcome := ""
			if step.Outcome != nil {
				outcome = *step.Outcome
			}
			if apply, err = w.plan(ctx, *task, outcome, "", true); err != nil {
				return nil, err
			}
		case WorkflowDecisionStop:
			apply = &repository.WorkflowApply{Sequence: task.WorkflowSequence,
				Moves: []repository.WorkflowMove{{Decision: domain.WorkflowDecision{From: node, Reason: "Stopped by a person at the workflow's limit"}, Stop: true}},
				Entry: repository.WorkflowEntry{Kind: repository.WorkflowEnterFail, Reason: "Workflow · stopped by a person at its limit"}}
		default:
			return nil, ErrWorkflowDecision
		}
	default:
		return nil, ErrWorkflowDecision
	}
	return w.tasks.DecideWorkflow(ctx, ws, id, *apply)
}

func truncateUTF8Bytes(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	for limit > 0 && (value[limit]&0xC0) == 0x80 {
		limit--
	}
	return value[:limit]
}

func harnessName(provider string) string {
	if name := agentDisplayNames[provider]; name != "" {
		return name
	}
	return provider
}
