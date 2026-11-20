package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/agent"
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
// existing machinery: an agent step that plans or changes code is an
// iteration, a reviewing step a review task, a publish step the publication,
// and waiting nodes wait in
// awaiting_review. What an agent step is comes from its profile's behaviour.

type workflowTaskStore interface {
	ApplyWorkflow(context.Context, domain.AgentTask, repository.WorkflowApply) (*domain.AgentTask, error)
	CurrentWorkflowStep(context.Context, domain.AgentTask) (*domain.WorkflowStep, error)
	WorkflowReview(context.Context, domain.AgentTask, uuid.UUID) (*repository.WorkflowReview, error)
	LatestWorkflowReview(context.Context, domain.AgentTask) (string, error)
	LatestWorkflowPlan(context.Context, domain.AgentTask) (string, error)
	WorkflowCI(context.Context, domain.AgentTask) (*repository.WorkflowCI, error)
	HasPublishedWork(context.Context, domain.AgentTask) (bool, error)
	WorkflowRun(context.Context, domain.AgentTask) (*repository.WorkflowRun, error)
	SetWorkflowWake(context.Context, domain.AgentTask, *time.Time) error
	ScopeInstructions(context.Context, domain.AgentTask) (string, error)
	WorkflowDecisionTask(context.Context, uuid.UUID, uuid.UUID) (*domain.AgentTask, error)
	DecideWorkflow(context.Context, uuid.UUID, uuid.UUID, repository.WorkflowApply) (*domain.AgentTask, error)
	ParkWorkflow(context.Context, domain.AgentTask) error
	// The task's pull request and the merges ship approvals carry out (KUA-209).
	WorkflowPullRequest(context.Context, domain.AgentTask) (*domain.AgentReviewTarget, error)
	RecordWorkflowMerge(context.Context, domain.AgentTask, repository.WorkflowMerge) (*domain.AgentTask, error)
	DecideWorkflowMerge(context.Context, uuid.UUID, uuid.UUID, repository.WorkflowMerge) (*domain.AgentTask, error)
	// The merge a ship approval deferred past its agent steps (KUA-216).
	DeferredShipMerge(context.Context, domain.AgentTask) (*domain.MergeWait, error)
	// What CI steps judged on a commit (KUA-215).
	WorkflowCIHistory(context.Context, domain.AgentTask, string) ([]domain.WorkflowStep, error)
	RecordWorkflowCI(context.Context, domain.AgentTask, domain.WorkflowCIRecord, *time.Time) error
}

type workflowDefinitionStore interface {
	Version(context.Context, uuid.UUID, uuid.UUID) (*domain.WorkflowVersion, error)
	AllProfiles(context.Context, uuid.UUID) (map[uuid.UUID]domain.AgentProfile, error)
}

// workflowCIChecks reads every check run and commit status of a commit from
// GitHub, including the ones still running (GitHubService.CommitChecks).
type workflowCIChecks interface {
	CommitChecks(ctx context.Context, ws uuid.UUID, repository, sha string) ([]PullRequestCheck, ChecksCoverage, error)
}

// workflowPolicy checks an agent profile against the workspace policy
// (allowed providers, model checks) before each step that runs it.
type workflowPolicy func(ctx context.Context, ws uuid.UUID, profile domain.AgentProfile) error

// WorkflowRunner advances tasks that run a configurable workflow.
type WorkflowRunner struct {
	tasks       workflowTaskStore
	definitions workflowDefinitionStore
	policy      workflowPolicy
	// checks confirms a CI check node's commit with GitHub; without it a CI
	// node can only fail (from a reported failure) or time out.
	checks workflowCIChecks
	// pulls and merger let ship approvals merge the task's pull request and
	// bring branches up to date (SetPullRequests, KUA-209); without them an
	// approval leads on without merging, as before.
	pulls  pullRequestReader
	merger approvalMerger
	now    func() time.Time

	mu     sync.Mutex
	graphs map[uuid.UUID]*domain.WorkflowGraph
}

func newWorkflowRunner(tasks workflowTaskStore, definitions workflowDefinitionStore, policy workflowPolicy) *WorkflowRunner {
	return &WorkflowRunner{tasks: tasks, definitions: definitions, policy: policy, now: time.Now, graphs: map[uuid.UUID]*domain.WorkflowGraph{}}
}

// A CI check node reads the commit's checks again after ciPoll while some
// are running, and after ciIdlePoll while none started or GitHub could not
// be read; a reported failure or a finished review wakes it sooner.
const (
	ciPoll     = time.Minute
	ciIdlePoll = 5 * time.Minute
	// ciDetailsPoll is how often a CI node looks again while Kuayle reads a
	// failed suite's checks; recording them wakes it sooner.
	ciDetailsPoll = 15 * time.Second
	// reviewCITimeout bounds the read of the published commit's checks for
	// a reviewer step.
	reviewCITimeout = 5 * time.Second
)

// ciFailureDetailsReader starts reading a failed suite's checks in the
// background unless a read is running (GitHubService.ensureCIFailureDetails);
// it never blocks the worker.
type ciFailureDetailsReader interface {
	ensureCIFailureDetails(ws uuid.UUID, repository, headSHA string)
}

// SetCIChecks lets CI check nodes confirm a commit's checks with GitHub.
func (w *WorkflowRunner) SetCIChecks(checks workflowCIChecks) { w.checks = checks }

// ErrWorkflowDecision is a decision the task's current step does not accept.
var ErrWorkflowDecision = errors.New("the workflow is not waiting for this decision")

// workflowStepBytes bounds the prompt of an iteration that carries a plan, as
// the workflow step writes it (the original instructions and the step's
// section): a whole plan (agent.MaxSummaryBytes) and the step's template fit
// in it, and it leaves the attempt room for its handoff within the attempt
// limit, so nothing later has to shorten the plan.
const workflowStepBytes = 28000

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

// Outcome advances the task after its current node produced outcome, unless
// a person asked to pause: the pause then takes effect instead (see
// AgentFinished).
func (w *WorkflowRunner) Outcome(ctx context.Context, task domain.AgentTask, outcome, detail string) error {
	return w.outcome(ctx, task, outcome, detail, "", nil)
}

// OutcomeFailure advances the task after its agent's iteration failed for
// class. The step's detail keeps the whole cause; a workflow that ends here
// tells a person the class and one short line of it.
func (w *WorkflowRunner) OutcomeFailure(ctx context.Context, task domain.AgentTask, detail string, class domain.FailureClass) error {
	return w.outcome(ctx, task, domain.WorkflowAgentFailed, detail, class, nil)
}

// outcome advances the task; ci is what a CI check step judged, stored on
// it as it closes.
func (w *WorkflowRunner) outcome(ctx context.Context, task domain.AgentTask, outcome, detail string, class domain.FailureClass, ci *domain.WorkflowCIRecord) error {
	if task.WorkflowPauseRequest != nil {
		return w.tasks.ParkWorkflow(ctx, task)
	}
	apply, err := w.plan(ctx, task, outcome, detail, false)
	if err != nil {
		return err
	}
	if class != "" && apply.Entry.Kind == repository.WorkflowEnterFail {
		apply.FailureClass = class
		apply.Entry.Reason += " · " + domain.TaskFailureReason(class, detail)
	}
	apply.CI = ci
	_, err = w.tasks.ApplyWorkflow(ctx, task, *apply)
	return err
}

// AgentFinished advances the task after the iteration of its agent step
// succeeded. A step that changes code is finished. A planning step is
// finished only when it left the worktree as it found it and its final
// message is a plan: the plan is then stored whole on the step and given to
// the agent steps after it. Otherwise the step failed, and says why.
func (w *WorkflowRunner) AgentFinished(ctx context.Context, task domain.AgentTask) error {
	// A pause a person asked for takes effect now that the step has an
	// outcome: the step stays open, and the worker reports the same finished
	// run again once the workflow is resumed.
	if task.WorkflowPauseRequest != nil {
		return w.tasks.ParkWorkflow(ctx, task)
	}
	graph, profiles, err := w.graph(ctx, task)
	if err != nil {
		return err
	}
	node, _ := graph.Node(task.Cursor().Node)
	run, err := w.tasks.WorkflowRun(ctx, task)
	if err != nil {
		return err
	}
	// A step whose agent did not update the notebook still finished; the
	// step's detail says so.
	if node.Supervised() {
		return w.finishStage(ctx, task, *graph, profiles, node, run)
	}
	if node.Type != domain.WorkflowNodeAgent || node.ProfileID == nil || !profiles[*node.ProfileID].Plans() {
		return w.Outcome(ctx, task, domain.WorkflowAgentFinished, run.NotebookNote)
	}
	if problem := planProblem(node.Label, *run); problem != "" {
		apply, err := w.plan(ctx, task, domain.WorkflowAgentFailed, problem, false)
		if err != nil {
			return err
		}
		// A workflow that ends here says what was wrong with the plan, not
		// only that the agent failed.
		if last := len(apply.Moves) - 1; apply.Entry.Kind == repository.WorkflowEnterFail && last >= 0 && !apply.Moves[last].Stop {
			apply.Entry.Reason += ". " + problem
		}
		_, err = w.tasks.ApplyWorkflow(ctx, task, *apply)
		return err
	}
	produced := neutralisePlan(run.Summary)
	apply, err := w.planWith(ctx, task, domain.WorkflowAgentFinished, run.NotebookNote, produced, false)
	if err != nil {
		return err
	}
	apply.Plan = produced
	_, err = w.tasks.ApplyWorkflow(ctx, task, *apply)
	return err
}

// planProblem says why a planning step's finished iteration is not a plan,
// from what every harness's run records: the worktree before and after it
// (compared by Kuayle, not reported by the agent) and its final message.
func planProblem(step string, run repository.WorkflowRun) string {
	switch {
	case run.WorktreeChanged == nil:
		return step + ": Kuayle could not confirm that the planning step left the worktree unchanged"
	case *run.WorktreeChanged:
		what := "a commit"
		if len(run.WorktreeFiles) > 0 {
			what = changedFileSummary(run.WorktreeFiles)
		}
		return step + ": a planning step only writes a plan, but it changed the worktree (" + what + ")"
	case !run.FinalMessage || strings.TrimSpace(run.Summary) == "":
		return step + ": the planning step produced no plan; its final message is the plan, and it wrote none"
	case run.SummaryTruncated:
		return fmt.Sprintf("%s: the plan is longer than %d KB and would not reach the next agent whole; ask for a shorter plan", step, agent.MaxSummaryBytes>>10)
	}
	return ""
}

func changedFileSummary(files []string) string {
	more := ""
	if len(files) > 10 {
		more = fmt.Sprintf(" and %d more", len(files)-10)
		files = files[:10]
	}
	return oneLine(strings.Join(files, ", "), 600) + more
}

// plan computes the moves an outcome causes, entering nodes that finish at
// once (a reviewer with nothing published, a step the policy refuses) until
// it reaches a node that runs or waits, an end, or a limit.
func (w *WorkflowRunner) plan(ctx context.Context, task domain.AgentTask, outcome, detail string, ignoreLimit bool) (*repository.WorkflowApply, error) {
	return w.planWith(ctx, task, outcome, detail, "", ignoreLimit)
}

// planWith is plan for a step that just produced a plan, which is not stored
// yet: the steps entered now get it.
func (w *WorkflowRunner) planWith(ctx context.Context, task domain.AgentTask, outcome, detail, produced string, ignoreLimit bool) (*repository.WorkflowApply, error) {
	graph, profiles, err := w.graph(ctx, task)
	if err != nil {
		return nil, err
	}
	now := w.now()
	// Leaving a person's wait adds the held time to the budget before the
	// limits are checked: a decision to leave an approval uses working time
	// only (KUA-171).
	cursor := task.Cursor().Released(now)
	apply := &repository.WorkflowApply{Sequence: task.WorkflowSequence, Version: task.WorkflowVersionID, Outcome: outcome, Detail: detail}
	vars, err := w.variables(ctx, task, *graph, profiles, cursor.Node, outcome, detail)
	if err != nil {
		return nil, err
	}
	if produced != "" {
		vars.Plan = produced
	}
	// What sends the work back is recorded in the task's notebook, so
	// every later step sees it, not only the next one.
	current, _ := graph.Node(cursor.Node)
	switch {
	case outcome == domain.WorkflowBranchConflict:
		vars.Rebase = detail
		apply.Notes = append(apply.Notes, domain.NotebookNote{Event: "Kuayle sent the work back: the pull request conflicts with its base"})
	case outcome == domain.WorkflowReviewChangesRequested && strings.TrimSpace(vars.ReviewComments) != "":
		apply.Notes = append(apply.Notes, domain.NotebookNote{FindingSource: domain.NotebookFindingReview, Finding: vars.ReviewComments})
	case outcome == domain.WorkflowChecksFailed && strings.TrimSpace(vars.FailingChecks) != "":
		source := domain.NotebookFindingCheck
		if current.Check == domain.WorkflowCheckCI {
			source = domain.NotebookFindingCI
		}
		apply.Notes = append(apply.Notes, domain.NotebookNote{FindingSource: source, Finding: vars.FailingChecks})
	}
	// A planning step that failed left no plan (an earlier visit's is stale
	// now) and maybe a changed worktree.
	from, _ := graph.Node(cursor.Node)
	failedPlan := outcome == domain.WorkflowAgentFailed && from.Type == domain.WorkflowNodeAgent && from.ProfileID != nil && profiles[*from.ProfileID].Plans()
	if failedPlan {
		apply.PlanFailed, vars.Plan = true, ""
	}
	for range len(graph.Nodes)*2 + 2 {
		decision := graph.Decide(cursor, outcome, now, profiles)
		if outcome == domain.WorkflowBranchConflict || outcome == domain.WorkflowCheckoutBlocked {
			// No transition names a conflict: the work goes back to the step
			// that changes code (KUA-209).
			decision = graph.SendBack(cursor, outcome, now, profiles)
		}
		// So the workflow may only plan again, ask a person or end; any
		// other step (also through an "else" transition) fails it instead.
		if failedPlan && !decision.Stuck && !afterFailedPlan(decision.To, profiles) {
			decision.Reason = fmt.Sprintf("%s failed, and a failed plan cannot lead to %s", from.Label, decision.To.Label)
			if detail != "" {
				decision.Reason += ". " + detail
			}
			apply.Moves = append(apply.Moves, repository.WorkflowMove{Decision: decision, Stop: true})
			apply.Entry = repository.WorkflowEntry{Kind: repository.WorkflowEnterFail, Reason: domain.WorkflowReasonPrefix + decision.Reason}
			return apply, nil
		}
		failedPlan = false
		reason := domain.WorkflowReasonPrefix + decision.Reason
		if (outcome == domain.WorkflowCheckoutBlocked || outcome == domain.WorkflowPublishFailed) && detail != "" {
			reason += ". " + detail
		}
		if outcome == domain.WorkflowBranchConflict && (decision.Stuck || decision.Limit != "") {
			reason += ". " + oneLine(vars.Rebase, 1000)
		}
		if decision.Stuck {
			apply.Moves = append(apply.Moves, repository.WorkflowMove{Decision: decision, Stop: true})
			apply.Entry = repository.WorkflowEntry{Kind: repository.WorkflowEnterFail, Reason: reason}
			return apply, nil
		}
		// Nothing was published because nothing changed: the task is
		// finished, on the step it is at, and says why.
		if decision.Finish {
			apply.Moves = append(apply.Moves, repository.WorkflowMove{Decision: decision, Stop: true})
			if detail != "" {
				reason = domain.WorkflowReasonPrefix + decision.From.Label + ": " + detail
			}
			apply.Entry = repository.WorkflowEntry{Kind: repository.WorkflowEnterComplete, Reason: reason}
			return apply, nil
		}
		if decision.Limit != "" && !ignoreLimit {
			apply.Moves = append(apply.Moves, repository.WorkflowMove{Decision: decision, Stop: true})
			if graph.Budget.Policy() == domain.WorkflowOnLimitAsk {
				apply.Entry = repository.WorkflowEntry{Kind: repository.WorkflowEnterPause, Reason: reason + ". Continue or stop the workflow"}
				apply.Notes = append(apply.Notes, domain.NotebookNote{Event: "Paused for a person's decision: " + decision.Reason})
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
			// A step that continues the round in progress (a plan handed to
			// the implementer) is no new iteration (KUA-215).
			apply.Iteration = graph.StartsIteration(decision, profiles)
			return apply, nil
		}
		// The node finishes as soon as it is entered; leave it by its outcome.
		move.Immediate, move.Detail = immediate, why
		apply.Moves = append(apply.Moves, move)
		cursor.Node = decision.To.ID
		cursor.Visits[decision.To.ID]++
		outcome = immediate
		if rebase := branchConflict(decision.To, immediate, why, profiles); rebase != "" {
			// The pull request conflicts with its base: the next agent is told
			// to merge the base, and the notebook records why.
			vars.Rebase = rebase
			apply.Notes = append(apply.Notes, domain.NotebookNote{Event: "Kuayle sent the work back at " + decision.To.Label + ": the pull request conflicts with its base"})
		}
	}
	return nil, fmt.Errorf("workflow %s: too many immediate transitions", task.ID)
}

// afterFailedPlan reports whether a failed planning step may lead to the
// node: a planning step (itself again, or another), an approval or an end.
func afterFailedPlan(to domain.WorkflowNode, profiles map[uuid.UUID]domain.AgentProfile) bool {
	switch to.Type {
	case domain.WorkflowNodeEnd, domain.WorkflowNodeApproval:
		return !to.Supervised()
	case domain.WorkflowNodeAgent:
		return to.ProfileID != nil && profiles[*to.ProfileID].Plans()
	}
	return false
}

// enter returns what entering the decision's target does, or the outcome
// it finishes with at once and why.
func (w *WorkflowRunner) enter(ctx context.Context, task domain.AgentTask, graph domain.WorkflowGraph, profiles map[uuid.UUID]domain.AgentProfile, cursor domain.WorkflowCursor, decision domain.WorkflowDecision, reason string, vars domain.WorkflowVariables, now time.Time) (repository.WorkflowEntry, string, string, error) {
	to := decision.To
	if to.Type == domain.WorkflowNodeAgent {
		return w.enterAgent(ctx, task, to, profiles, cursor, reason, vars, now)
	}
	var merge *domain.MergeWait
	if to.Type == domain.WorkflowNodeEnd && to.Outcome == domain.WorkflowEndDone {
		// Claim the deferred approval as we enter the end. A supervised end
		// keeps it on its own step until the assistant hands back control.
		var err error
		merge, err = w.deferredShipMerge(ctx, task, now)
		if err != nil {
			return repository.WorkflowEntry{}, "", "", err
		}
	}
	if to.Supervised() {
		to.Prompt = decision.Reason + "\n\n{{previous_summary}}\n\n{{diff}}"
		entry, outcome, detail, err := w.enterAgent(ctx, task, to, profiles, cursor, reason, vars, now)
		entry.Merge = merge
		return entry, outcome, detail, err
	}
	return w.enterNative(ctx, task, graph, profiles, to, reason, now, merge)
}

func (w *WorkflowRunner) enterNative(ctx context.Context, task domain.AgentTask, graph domain.WorkflowGraph, profiles map[uuid.UUID]domain.AgentProfile, to domain.WorkflowNode, reason string, now time.Time, merge *domain.MergeWait) (repository.WorkflowEntry, string, string, error) {
	// A waiting node is evaluated as soon as it is entered: what it waits for
	// may already be there (CI finished during the review), and Evaluate then
	// sets its next wake-up (its timeout, the time budget, a CI poll).
	wake := func() *time.Time {
		at := now
		return &at
	}
	switch to.Type {
	case domain.WorkflowNodeEnd:
		if to.Outcome == domain.WorkflowEndDone {
			if merge != nil {
				return repository.WorkflowEntry{Kind: repository.WorkflowEnterWait, Merge: merge, WakeAt: wake(),
					Reason: fmt.Sprintf("%s; Kuayle merges pull request #%d, as approved, once every check passes", reason, merge.PullRequest)}, "", "", nil
			}
			return repository.WorkflowEntry{Kind: repository.WorkflowEnterComplete, Reason: reason}, "", "", nil
		}
		return repository.WorkflowEntry{Kind: repository.WorkflowEnterFail, Reason: reason}, "", "", nil
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
		// Older workflow versions can still contain a local check.
		return repository.WorkflowEntry{}, domain.WorkflowChecksSkipped, "Continuing past a retired workflow step", nil
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
		suffix := ""
		if graph.ShipApproval(to, profiles) != "" {
			// The go-ahead to merge: the pull request is made ready to merge
			// first, or sent back when it conflicts (KUA-209).
			done, conflict := w.prepareBranch(ctx, task, true)
			if conflict != "" {
				return repository.WorkflowEntry{}, domain.WorkflowBranchConflict, conflict, nil
			}
			if len(done) > 0 {
				suffix = " (Kuayle " + strings.Join(done, " and ") + ")"
			}
		}
		return repository.WorkflowEntry{Kind: repository.WorkflowEnterWait, Reason: reason + "; waiting for approval: " + oneLine(question, 300) + suffix, WakeAt: wake(), NeedsPerson: true}, "", "", nil
	case domain.WorkflowNodeMerge:
		if merge != nil {
			return repository.WorkflowEntry{Kind: repository.WorkflowEnterWait, Merge: merge, WakeAt: wake(), Reason: reason + "; Kuayle merges the pull request once every check passes"}, "", "", nil
		}
		return repository.WorkflowEntry{Kind: repository.WorkflowEnterWait, Reason: reason + "; waiting for the pull request to be merged (ship it from chat or merge it on GitHub)", WakeAt: wake(), NeedsPerson: true}, "", "", nil
	}
	return repository.WorkflowEntry{}, "", "", ErrInvalidOperation
}

func (w *WorkflowRunner) enterAgent(ctx context.Context, task domain.AgentTask, to domain.WorkflowNode, profiles map[uuid.UUID]domain.AgentProfile, cursor domain.WorkflowCursor, reason string, vars domain.WorkflowVariables, now time.Time) (repository.WorkflowEntry, string, string, error) {
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
	if profile.Reviews() {
		published, err := w.tasks.HasPublishedWork(ctx, task)
		if err != nil {
			return repository.WorkflowEntry{}, "", "", err
		}
		if !published {
			return repository.WorkflowEntry{}, domain.WorkflowAgentFailed, to.Label + ": there is no published pull request to review", nil
		}
		// The review sees the branch with its base's newest commits; one
		// that conflicts goes back as changes requested (KUA-209).
		if _, conflict := w.prepareBranch(ctx, task, false); conflict != "" {
			return repository.WorkflowEntry{}, domain.WorkflowReviewChangesRequested, conflict, nil
		}
		// A review's focus is short, so the plan is not added to it
		// unless the step's prompt asks for it.
		reviewVars := vars
		if !strings.Contains(to.Prompt, "{{plan}}") {
			reviewVars.Plan = ""
		}
		focus := strings.TrimSpace(strings.TrimSpace(profile.Instructions) + "\n\n" + domain.RenderWorkflowPrompt(to.Prompt, reviewVars))
		// CI is the CI step's; the reviewer is told what it is on the
		// commit it reviews so it does not block on a run it cannot see.
		ci, err := w.reviewCI(ctx, task)
		if err != nil {
			return repository.WorkflowEntry{}, "", "", err
		}
		return repository.WorkflowEntry{Kind: repository.WorkflowEnterReview, Reason: reason + "; " + profile.Name + " reviews the pull request",
			Review: &domain.AgentReviewRequest{Provider: profile.Harness, Model: profile.Model, Thinking: profile.Effort, Focus: truncateUTF8Bytes(focus, 4000), CI: ci}, WakeAt: &now}, "", "", nil
	}
	scope, err := w.tasks.ScopeInstructions(ctx, task)
	if err != nil {
		return repository.WorkflowEntry{}, "", "", err
	}
	base, _, _ := strings.Cut(task.Prompt, followUpMarker)
	heading := fmt.Sprintf("%s%s (visit %d):\n", workflowStepMarker, to.Label, cursor.Visits[to.ID]+1)
	// The plan is passed whole: the untrusted variables and then the
	// original instructions give way to it.
	limit := 32000
	if vars.Plan != "" {
		limit = workflowStepBytes
	}
	rendered := domain.RenderWorkflowPromptWithin(to.Prompt, vars, limit-len(heading)-len(vars.Rebase))
	if rendered == "" {
		rendered = "Continue the task on the existing branch."
	}
	if vars.Rebase != "" {
		rendered = vars.Rebase + "\n\n" + rendered
	}
	section := heading + rendered
	if room := limit - len(section); len(base) > room {
		base = truncateRunes(base, max(room/4, 0))
	}
	return repository.WorkflowEntry{Kind: repository.WorkflowEnterIteration, Reason: reason,
		Prompt: base + section, Instructions: workflowInstructions(scope, profile, domain.WorkflowNode{}),
		Provider: profile.Harness, Model: profile.Model, Thinking: profile.Effort}, "", "", nil
}

// reviewCI reads each check of the published commit from GitHub for a
// reviewer step, within reviewCITimeout. It is nil when GitHub cannot be
// read: the repository then uses the suites webhooks reported, or tells the
// reviewer CI is unknown.
func (w *WorkflowRunner) reviewCI(ctx context.Context, task domain.AgentTask) (*domain.AgentReviewCI, error) {
	if w.checks == nil {
		return nil, nil
	}
	ci, err := w.tasks.WorkflowCI(ctx, task)
	if err != nil {
		return nil, err
	}
	if ci.Repository == "" || ci.CommitSHA == "" {
		return nil, nil
	}
	readCtx, cancel := context.WithTimeout(ctx, reviewCITimeout)
	defer cancel()
	checks, coverage, err := w.checks.CommitChecks(readCtx, task.WorkspaceID, ci.Repository, ci.CommitSHA)
	if err != nil {
		return nil, ctx.Err()
	}
	out := &domain.AgentReviewCI{Read: true, Partial: coverage.incomplete()}
	for _, check := range checks {
		out.Checks = append(out.Checks, domain.AgentReviewCheck{Name: check.Name, State: check.State})
	}
	return out, nil
}

// workflowInstructions are the trusted instructions of an agent step: the
// inherited project instructions, then its profile's (unless the harness reads
// them from a profile file), then (for the first iteration, which has no
// variables yet) the step's template.
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
	// Harnesses that read profile files get the instructions there, whole
	// and outside the prompt's size limit (domain.ProfileFiles).
	if !domain.ProfileFilesSupported(profile.Harness) {
		section(workflowProfileTitle+profile.Title(), profile.Instructions)
	}
	if node.ID != "" {
		section(workflowStepTitle+node.Label, domain.RenderWorkflowPrompt(node.Prompt, domain.WorkflowVariables{}))
	}
	return truncateUTF8Bytes(b.String(), 32000)
}

// variables collects what the finished node passes to the next agent: the
// previous agent's summary and changed files, the reviewer's findings and
// the failing checks' output, all untrusted, and the latest plan a planning
// step of the task stored, which is passed as instructions.
func (w *WorkflowRunner) variables(ctx context.Context, task domain.AgentTask, graph domain.WorkflowGraph, profiles map[uuid.UUID]domain.AgentProfile, from, outcome, detail string) (domain.WorkflowVariables, error) {
	vars := domain.WorkflowVariables{}
	run, err := w.tasks.WorkflowRun(ctx, task)
	if err != nil {
		return vars, err
	}
	vars.PreviousSummary = run.Summary
	if vars.Plan, err = w.tasks.LatestWorkflowPlan(ctx, task); err != nil {
		return vars, err
	}
	if len(run.ChangedFiles) > 0 {
		files := run.ChangedFiles
		if len(files) > 100 {
			files = files[:100]
		}
		vars.Diff = "Files changed by the last iteration:\n- " + strings.Join(files, "\n- ")
	}
	node, _ := graph.Node(from)
	switch {
	case node.Type == domain.WorkflowNodeAgent && node.ProfileID != nil && profiles[*node.ProfileID].Reviews() && outcome == domain.WorkflowReviewChangesRequested:
		if vars.ReviewComments, err = w.tasks.LatestWorkflowReview(ctx, task); err != nil {
			return vars, err
		}
		if vars.ReviewComments == "" {
			vars.ReviewComments = detail
		}
	case node.Type == domain.WorkflowNodeCheck && outcome == domain.WorkflowChecksFailed:
		vars.FailingChecks = detail
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
	if step.StageMergeObserved {
		outcome := domain.WorkflowMerged
		if node.Type == domain.WorkflowNodeApproval {
			outcome = domain.WorkflowApprovalApproved
		}
		return w.Outcome(ctx, task, outcome, "The published pull request was merged")
	}
	// A person approved the merge this step carries out: the merge, not the
	// step's timeout, decides while it runs (KUA-209). A done end carries the
	// merge a ship approval deferred to it (KUA-216).
	if wait := step.MergeWait(); wait != nil && wait.Failure == "" && !wait.Deferred && (node.Type == domain.WorkflowNodeApproval || node.Type == domain.WorkflowNodeMerge || node.Type == domain.WorkflowNodeEnd) {
		handled, err := w.driveMerge(ctx, task, node, *step, wait)
		if err != nil || handled {
			return err
		}
		// The merge ended without merging; the step waits for a person again,
		// with its timeout and the time budget as before.
	}
	now := w.now()
	var deadline *time.Time
	if node.TimeoutMinutes > 0 {
		// Time a person kept the step paused is not part of its timeout.
		at := step.EnteredAt.Add(time.Duration(node.TimeoutMinutes)*time.Minute + time.Duration(step.PausedSeconds)*time.Second)
		deadline = &at
	}
	outcome, detail, waiting := "", "", ""
	var wake *time.Time
	var ci *ciVerdict
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
			// Only an explicit approval approves; a comment sends the work
			// back. A review without a verdict fails instead (KUA-227);
			// one completed as "unknown" before that also sends it back.
			if review.Verdict != nil && *review.Verdict == "approve" {
				outcome, detail = domain.WorkflowReviewApproved, findings
			} else {
				outcome, detail = domain.WorkflowReviewChangesRequested, findings
			}
		case "failed", "cancelled":
			if reviewModelNotFound(review) {
				return w.failForReviewer(ctx, task, node, review.Reason)
			}
			// The review task's reason says why, for example that the
			// reviewer's answer had no verdict (KUA-227).
			outcome, detail = domain.WorkflowAgentFailed, "The review did not finish ("+review.State+")"
			if reason := strings.TrimSpace(review.Reason); reason != "" {
				detail += ": " + reason
			}
		}
	case node.Type == domain.WorkflowNodeCheck && node.Check == domain.WorkflowCheckCI:
		published, err := w.tasks.WorkflowCI(ctx, task)
		if err != nil {
			return err
		}
		verdict, err := w.judgeCI(ctx, task, step, published, now)
		if err != nil {
			return err
		}
		if verdict.stop != "" {
			return w.stopCI(ctx, task, *graph, profiles, verdict)
		}
		ci = &verdict
		outcome, detail, waiting, wake = verdict.outcome, verdict.detail, verdict.waiting, verdict.wake
	}
	if outcome == "" && deadline != nil && !now.Before(*deadline) {
		outcome, detail = domain.WorkflowTimeout, fmt.Sprintf("%s waited %d minutes", node.Label, node.TimeoutMinutes)
		if waiting != "" {
			detail += ": " + waiting
		}
	}
	if outcome == "" {
		// The time budget also bounds a wait (a reviewer, an approval, CI):
		// the task stays on the step and fails or asks a person.
		budget := graph.BudgetDeadline(task.Cursor())
		if budget != nil && !now.Before(*budget) {
			reason := domain.WorkflowReasonPrefix + fmt.Sprintf("The workflow's time budget of %d minutes is used up while %s waits", graph.Budget.MaxMinutes, node.Label)
			if waiting != "" {
				reason += " (" + waiting + ")"
			}
			apply := repository.WorkflowApply{Sequence: task.WorkflowSequence, Version: task.WorkflowVersionID, Hold: true, Entry: repository.WorkflowEntry{Kind: repository.WorkflowEnterFail, Reason: reason}}
			if graph.Budget.Policy() == domain.WorkflowOnLimitAsk {
				apply.Entry = repository.WorkflowEntry{Kind: repository.WorkflowEnterPause, Reason: reason + ". Continue or stop the workflow"}
			}
			_, err = w.tasks.ApplyWorkflow(ctx, task, apply)
			return err
		}
		for _, at := range []*time.Time{deadline, budget} {
			if at != nil && (wake == nil || at.Before(*wake)) {
				wake = at
			}
		}
		if ci != nil && ci.save {
			// What the CI step knows changed (details asked for, a re-run
			// requested): stored with the wake-up, before the read starts.
			if err := w.tasks.RecordWorkflowCI(ctx, task, *ci.record, wake); err != nil {
				return err
			}
			if ci.after != nil {
				ci.after()
			}
			return nil
		}
		return w.tasks.SetWorkflowWake(ctx, task, wake)
	}
	var record *domain.WorkflowCIRecord
	if ci != nil {
		record = ci.record
	}
	return w.outcome(ctx, task, outcome, detail, "", record)
}

// stopCI pauses the workflow at a CI step that found CI still failing on a
// commit nothing was pushed to since CI failed on it (KUA-215), instead of
// sending the same failure back: the step closes as checks failed and the
// transition it would take is recorded without being entered. Continuing
// takes it, with the failing checks; stopping fails the task. A transition
// that ends the workflow, or that no transition follows, is taken as usual:
// nothing loops there.
func (w *WorkflowRunner) stopCI(ctx context.Context, task domain.AgentTask, graph domain.WorkflowGraph, profiles map[uuid.UUID]domain.AgentProfile, v ciVerdict) error {
	if task.WorkflowPauseRequest != nil {
		return w.tasks.ParkWorkflow(ctx, task)
	}
	now := w.now()
	decision := graph.Decide(task.Cursor().Released(now), domain.WorkflowChecksFailed, now, profiles)
	if decision.Stuck || decision.Finish || decision.Limit != "" || decision.To.Type == domain.WorkflowNodeEnd {
		return w.outcome(ctx, task, domain.WorkflowChecksFailed, strings.TrimSpace(v.detail+"\n\n"+v.stop), "", v.record)
	}
	decision.Reason = v.stop
	apply := repository.WorkflowApply{Sequence: task.WorkflowSequence, Version: task.WorkflowVersionID, Outcome: domain.WorkflowChecksFailed, Detail: v.detail, CI: v.record,
		Moves: []repository.WorkflowMove{{Decision: decision, Stop: true}},
		Entry: repository.WorkflowEntry{Kind: repository.WorkflowEnterPause, Reason: fmt.Sprintf("%s%s. Continue to send the failing checks to %s, or stop the workflow", domain.WorkflowReasonPrefix, v.stop, decision.To.Label)},
		Notes: []domain.NotebookNote{{FindingSource: domain.NotebookFindingCI, Finding: v.detail}, {Event: "Paused for a person's decision: " + v.stop}}}
	_, err := w.tasks.ApplyWorkflow(ctx, task, apply)
	return err
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
	// Nobody decides a step while the workflow is paused.
	if !task.RunsWorkflow() || task.WorkflowSequence != sequence || task.WorkflowStatus == nil || task.WorkflowPausedAt != nil {
		return nil, ErrWorkflowDecision
	}
	graph, profiles, err := w.graph(ctx, *task)
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
		step, err := w.tasks.CurrentWorkflowStep(ctx, *task)
		if err != nil {
			return nil, err
		}
		if mergeActive(*step) {
			// Approved and merging: the merge's result decides now.
			return nil, ErrWorkflowDecision
		}
		// Approving the ship approval merges the pull request (KUA-209): on
		// the approval itself, at the merge step it leads to, or once the
		// agent steps after it reach the done end (KUA-216).
		var merge, deferred *domain.MergeWait
		if outcome == domain.WorkflowApprovalApproved && w.pulls != nil && w.merger != nil {
			switch graph.ShipApproval(node, profiles) {
			case domain.ShipMergesOnApproval:
				merged, started, err := w.startMerge(ctx, ws, *task, node)
				if err != nil || started {
					return merged, err
				}
			case domain.ShipMergesAtMergeStep:
				if merge, _, err = w.approvedMerge(ctx, *task); err != nil {
					return nil, err
				}
			case domain.ShipMergesAfterAgents:
				if deferred, _, err = w.approvedMerge(ctx, *task); err != nil {
					return nil, err
				}
			}
		}
		if apply, err = w.plan(ctx, *task, outcome, "", false); err != nil {
			return nil, err
		}
		if last := len(apply.Moves) - 1; merge != nil && last >= 0 && !apply.Moves[last].Stop && apply.Moves[last].Decision.To.Type == domain.WorkflowNodeMerge && (apply.Entry.Kind == repository.WorkflowEnterWait || apply.Entry.Kind == repository.WorkflowEnterIteration) {
			// Kuayle merges now; a person is needed again only if it cannot.
			apply.Entry.Merge, apply.Entry.NeedsPerson = merge, false
			apply.Entry.WakeAt = ptr(w.now())
			apply.Entry.Reason = domain.WorkflowReasonPrefix + apply.Moves[last].Decision.Reason + "; Kuayle merges the pull request once every check passes"
		}
		if deferred != nil {
			// The approval is recorded with the merge it starts once the agent
			// steps after it finish; the done end carries it out.
			deferred.Deferred = true
			apply.StepMerge = deferred
			if apply.Entry.Kind == repository.WorkflowEnterIteration {
				apply.Entry.Reason += fmt.Sprintf("; Kuayle merges pull request #%d once the workflow reaches its end and every check passes", deferred.PullRequest)
			}
		}
	case domain.WorkflowStatusPaused:
		step, err := w.tasks.CurrentWorkflowStep(ctx, *task)
		if err != nil {
			return nil, err
		}
		switch decision {
		case WorkflowDecisionContinue:
			if apply, err = w.continuePast(ctx, *task, *graph, profiles, node, *step); err != nil {
				return nil, err
			}
		case WorkflowDecisionStop:
			apply = &repository.WorkflowApply{Sequence: task.WorkflowSequence, Version: task.WorkflowVersionID,
				Moves: []repository.WorkflowMove{{Decision: domain.WorkflowDecision{From: node, Reason: "Stopped by a person at the workflow's limit"}, Stop: true}},
				Entry: repository.WorkflowEntry{Kind: repository.WorkflowEnterFail, Reason: domain.WorkflowReasonPrefix + "stopped by a person at its limit"}}
		default:
			return nil, ErrWorkflowDecision
		}
	default:
		return nil, ErrWorkflowDecision
	}
	// The decision is a person's: the notebook records it as an event, with
	// the person in its history.
	apply.Notes = append(apply.Notes, domain.NotebookNote{Event: fmt.Sprintf("A person chose %q at %s", decision, node.Label)})
	return w.tasks.DecideWorkflow(ctx, ws, id, *apply)
}

// continuePast resumes a workflow a person continued past a limit. The
// time budget reached during a wait leaves the step unfinished: it waits
// again with another time budget. A budget reached at a transition grants
// another iteration and time budget of the same size, so later transitions
// do not pause again at once; a node's visit limit is passed once. Either
// way the step's outcome and saved detail (failing checks, review comments)
// are passed along as they would have been.
func (w *WorkflowRunner) continuePast(ctx context.Context, task domain.AgentTask, graph domain.WorkflowGraph, profiles map[uuid.UUID]domain.AgentProfile, node domain.WorkflowNode, step domain.WorkflowStep) (*repository.WorkflowApply, error) {
	if step.FinishedAt == nil {
		return &repository.WorkflowApply{Sequence: task.WorkflowSequence, Version: task.WorkflowVersionID, Hold: true, ExtendBudget: true,
			Entry: repository.WorkflowEntry{Kind: repository.WorkflowEnterWait, NeedsPerson: node.Type == domain.WorkflowNodeApproval || node.Type == domain.WorkflowNodeMerge,
				Reason: domain.WorkflowReasonPrefix + "continued past the time budget by a person; " + node.Label + " waits again", WakeAt: ptr(w.now())}}, nil
	}
	outcome := ""
	if step.Outcome != nil {
		outcome = *step.Outcome
	}
	probe := graph.Decide(task.Cursor().Released(w.now()), outcome, w.now(), profiles)
	// The limit's reason was added to the step's detail when it stopped, and
	// so was a CI step's reason for pausing (KUA-215).
	detail := strings.TrimSuffix(strings.TrimSuffix(step.Detail, probe.Reason), "\n")
	if at := strings.LastIndex(detail, "\n"+ciNoChanges); at >= 0 {
		detail = detail[:at]
	}
	switch probe.Limit {
	case domain.WorkflowLimitIterations, domain.WorkflowLimitTime:
		extended, now := task, w.now()
		extended.WorkflowExtensions, extended.WorkflowBudgetFrom = task.WorkflowExtensions+1, &now
		apply, err := w.plan(ctx, extended, outcome, detail, false)
		if err != nil {
			return nil, err
		}
		apply.ExtendBudget = true
		return apply, nil
	}
	return w.plan(ctx, task, outcome, detail, true)
}

func ptr[T any](value T) *T { return &value }

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
