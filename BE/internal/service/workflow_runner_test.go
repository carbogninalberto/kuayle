package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// workflowWorld is the durable state a runner reads and writes: the task row,
// its step log, and what the machine side recorded (runs, publications,
// reviews, CI). Moves go through repository.ComputeWorkflowApply, the same
// computation the database transaction uses, so a fresh runner on the same
// world behaves like a restarted process.
type workflowWorld struct {
	t         *testing.T
	now       time.Time
	task      domain.AgentTask
	steps     map[int]*domain.WorkflowStep
	runs      map[uuid.UUID]*repository.WorkflowRun
	published map[uuid.UUID]bool
	reviews   map[uuid.UUID]*repository.WorkflowReview
	ci        map[uuid.UUID]*repository.WorkflowCI
	// checks is what GitHub lists for each published commit (keyed by run).
	checks map[uuid.UUID][]PullRequestCheck
	// heads names the commit a run published when it is not the run's own
	// (an attempt that pushed nothing new publishes the same commit again),
	// and shaChecks what GitHub lists for such a commit.
	heads     map[uuid.UUID]string
	shaChecks map[string][]PullRequestCheck
	coverage  ChecksCoverage
	noGitHub  bool
	version   domain.WorkflowVersion
	profiles  map[uuid.UUID]domain.AgentProfile
	entries   []repository.WorkflowEntry
	// notes are what applied moves added to the task's notebook.
	notes []domain.NotebookNote
	// decider is the person deciding approvals; notices counts the times
	// the task's creator was told a person is needed again.
	decider uuid.UUID
	notices int
}

func (w *workflowWorld) apply(task domain.AgentTask, apply repository.WorkflowApply) (*domain.AgentTask, error) {
	if !w.task.RunsWorkflow() || w.task.WorkflowSequence != apply.Sequence || task.Generation != w.task.Generation {
		return nil, repository.ErrAgentTaskConflict
	}
	// As applyWorkflowTx: nothing moves while the workflow is paused.
	if w.task.WorkflowPausedAt != nil {
		return nil, repository.ErrAgentTaskConflict
	}
	switch w.task.State {
	case "running", "validating", "publishing", "awaiting_review", "blocked":
	default:
		return nil, repository.ErrAgentTaskConflict
	}
	w.notes = append(w.notes, apply.Notes...)
	current := w.steps[apply.Sequence]
	if apply.Hold {
		// As applyWorkflowTx: only an unfinished step is held, without moves.
		if len(apply.Moves) > 0 || current.FinishedAt != nil {
			return nil, repository.ErrAgentTaskConflict
		}
		want := domain.WorkflowStatusWaiting
		if apply.Entry.Kind == repository.WorkflowEnterWait {
			want = domain.WorkflowStatusPaused
		}
		if *w.task.WorkflowStatus != want {
			return nil, repository.ErrAgentTaskConflict
		}
		// A done end that waited for its merge completes on it (KUA-216).
		if apply.Entry.Kind == repository.WorkflowEnterComplete {
			if current.NodeType != domain.WorkflowNodeEnd {
				return nil, repository.ErrAgentTaskConflict
			}
			outcome := apply.Outcome
			current.Outcome, current.FinishedAt, current.Detail = &outcome, &w.now, apply.Detail
		}
	} else {
		if apply.Outcome != "" {
			current.Outcome = &apply.Outcome
		}
		if apply.Detail != "" {
			current.Detail = apply.Detail
		}
		if current.FinishedAt == nil {
			current.FinishedAt = &w.now
		}
		if apply.Plan != "" || apply.PlanFailed {
			plan := apply.Plan
			current.Plan = &plan
		}
		if apply.CI != nil {
			current.CI = rawJSON(w.t, apply.CI)
		}
		if apply.StepMerge != nil {
			current.Merge = rawJSON(w.t, apply.StepMerge)
		}
	}
	result := repository.ComputeWorkflowApply(w.task, apply, w.now)
	for i := range result.Steps {
		step := result.Steps[i]
		w.steps[step.Sequence] = &step
	}
	if result.StopReason != "" {
		w.steps[result.Sequence].Detail = strings.TrimSpace(w.steps[result.Sequence].Detail + "\n" + result.StopReason)
	}
	visits, _ := json.Marshal(result.Cursor.Visits)
	node, status := result.Cursor.Node, result.Status
	w.task.WorkflowNode, w.task.WorkflowVisits, w.task.WorkflowIterations = &node, visits, result.Cursor.Iterations
	w.task.WorkflowSequence, w.task.WorkflowStatus, w.task.WorkflowWakeAt = result.Sequence, &status, result.Wake
	w.task.WorkflowExtensions = result.Cursor.Extensions
	if !result.Cursor.BudgetFrom.IsZero() {
		from := result.Cursor.BudgetFrom
		w.task.WorkflowBudgetFrom = &from
	}
	if result.Cursor.HeldSince.IsZero() {
		w.task.WorkflowHeldSince = nil
	} else {
		held := result.Cursor.HeldSince
		w.task.WorkflowHeldSince = &held
	}
	entry := apply.Entry
	w.entries = append(w.entries, entry)
	switch entry.Kind {
	case repository.WorkflowEnterIteration:
		w.task.Generation++
		w.task.RunID = uuid.New()
		w.task.Prompt, w.task.AgentInstructions = entry.Prompt, entry.Instructions
		w.task.Provider, w.task.Model, w.task.Thinking = entry.Provider, entry.Model, entry.Thinking
		generation := w.task.Generation
		w.steps[result.FinalSequence].Generation = &generation
	case repository.WorkflowEnterReview:
		if !w.published[w.task.RunID] {
			return nil, repository.ErrNoReviewTarget
		}
		review := uuid.New()
		w.reviews[review] = &repository.WorkflowReview{State: "queued"}
		w.steps[result.FinalSequence].ReviewTaskID = &review
	case repository.WorkflowEnterWait:
		if entry.Merge != nil {
			data, err := json.Marshal(entry.Merge)
			if err != nil {
				return nil, err
			}
			raw := json.RawMessage(data)
			w.steps[result.FinalSequence].Merge = &raw
		}
	}
	if entry.Kind == repository.WorkflowEnterFail && apply.FailureClass != "" {
		class := string(apply.FailureClass)
		w.task.FailureClass = &class
	}
	w.task.State, w.task.Reason = repository.WorkflowEntryState(entry.Kind), entry.Reason
	copy := w.task
	return &copy, nil
}

type worldStore struct{ w *workflowWorld }

func (s worldStore) ApplyWorkflow(_ context.Context, task domain.AgentTask, apply repository.WorkflowApply) (*domain.AgentTask, error) {
	return s.w.apply(task, apply)
}
func (s worldStore) CurrentWorkflowStep(_ context.Context, task domain.AgentTask) (*domain.WorkflowStep, error) {
	step := *s.w.steps[task.WorkflowSequence]
	return &step, nil
}
func (s worldStore) WorkflowReview(_ context.Context, _ domain.AgentTask, id uuid.UUID) (*repository.WorkflowReview, error) {
	return s.w.reviews[id], nil
}
func (s worldStore) LatestWorkflowReview(_ context.Context, _ domain.AgentTask) (string, error) {
	for seq := s.w.task.WorkflowSequence; seq > 0; seq-- {
		if step := s.w.steps[seq]; step != nil && step.ReviewTaskID != nil && step.FinishedAt != nil {
			if review := s.w.reviews[*step.ReviewTaskID]; review.Findings != nil {
				return *review.Findings, nil
			}
		}
	}
	return "", nil
}
func (s worldStore) LatestWorkflowPlan(_ context.Context, _ domain.AgentTask) (string, error) {
	for seq := s.w.task.WorkflowSequence; seq > 0; seq-- {
		if step := s.w.steps[seq]; step != nil && step.Plan != nil {
			return *step.Plan, nil
		}
	}
	return "", nil
}
func (s worldStore) WorkflowCI(_ context.Context, task domain.AgentTask) (*repository.WorkflowCI, error) {
	ci := repository.WorkflowCI{}
	if recorded := s.w.ci[task.RunID]; recorded != nil {
		ci = *recorded
	}
	if s.w.published[task.RunID] {
		ci.Published, ci.Repository, ci.CommitSHA = true, "acme/app", s.w.head(task.RunID)
	}
	return &ci, nil
}

// head is the commit the run published.
func (w *workflowWorld) head(run uuid.UUID) string {
	if head := w.heads[run]; head != "" {
		return head
	}
	return run.String()
}

// CommitChecks is GitHub's full check list of a published commit.
func (s worldStore) CommitChecks(_ context.Context, _ uuid.UUID, repository, sha string) ([]PullRequestCheck, ChecksCoverage, error) {
	if repository != "acme/app" {
		return nil, ChecksCoverage{}, errors.New("unknown repository")
	}
	if checks, ok := s.w.shaChecks[sha]; ok {
		return checks, s.w.coverage, nil
	}
	run, err := uuid.Parse(sha)
	if err != nil {
		return nil, ChecksCoverage{}, err
	}
	return s.w.checks[run], s.w.coverage, nil
}
func (s worldStore) HasPublishedWork(_ context.Context, task domain.AgentTask) (bool, error) {
	return s.w.published[task.RunID], nil
}
func (s worldStore) ValidationOutput(context.Context, domain.AgentTask) (string, error) {
	return "", nil
}
func (s worldStore) WorkflowRun(_ context.Context, task domain.AgentTask) (*repository.WorkflowRun, error) {
	if run := s.w.runs[task.RunID]; run != nil {
		return run, nil
	}
	return &repository.WorkflowRun{}, nil
}
func (s worldStore) SetWorkflowWake(_ context.Context, task domain.AgentTask, at *time.Time) error {
	if task.WorkflowSequence != s.w.task.WorkflowSequence {
		return repository.ErrAgentTaskConflict
	}
	s.w.task.WorkflowWakeAt = at
	return nil
}
func (s worldStore) ScopeInstructions(context.Context, domain.AgentTask) (string, error) {
	return "Project instructions", nil
}
func (s worldStore) WorkflowDecisionTask(context.Context, uuid.UUID, uuid.UUID) (*domain.AgentTask, error) {
	copy := s.w.task
	return &copy, nil
}
func (s worldStore) DecideWorkflow(_ context.Context, _, _ uuid.UUID, apply repository.WorkflowApply) (*domain.AgentTask, error) {
	return s.w.apply(s.w.task, apply)
}
func (s worldStore) WorkflowCIHistory(_ context.Context, _ domain.AgentTask, head string) ([]domain.WorkflowStep, error) {
	var steps []domain.WorkflowStep
	for seq := 1; seq <= s.w.task.WorkflowSequence; seq++ {
		if step := s.w.steps[seq]; step != nil && step.NodeType == domain.WorkflowNodeCheck {
			if record := step.CIRecord(); record != nil && record.Head == head {
				steps = append(steps, *step)
			}
		}
	}
	return steps, nil
}

// RecordWorkflowCI applies repository.RecordWorkflowCI's checks and writes.
func (s worldStore) RecordWorkflowCI(_ context.Context, task domain.AgentTask, record domain.WorkflowCIRecord, wake *time.Time) error {
	step := s.w.steps[s.w.task.WorkflowSequence]
	if task.WorkflowSequence != s.w.task.WorkflowSequence || *s.w.task.WorkflowStatus != domain.WorkflowStatusWaiting || step.FinishedAt != nil || step.NodeType != domain.WorkflowNodeCheck {
		return repository.ErrAgentTaskConflict
	}
	step.CI = rawJSON(s.w.t, &record)
	s.w.task.WorkflowWakeAt = wake
	return nil
}

// DeferredShipMerge applies repository.DeferredShipMerge's rule.
func (s worldStore) DeferredShipMerge(context.Context, domain.AgentTask) (*domain.MergeWait, error) {
	for seq := s.w.task.WorkflowSequence; seq > 0; seq-- {
		step := s.w.steps[seq]
		if step == nil {
			continue
		}
		if step.NodeType == domain.WorkflowNodeApproval && step.FinishedAt != nil {
			if wait := step.MergeWait(); wait != nil && wait.Deferred {
				return wait, nil
			}
		}
		if step.NodeType != domain.WorkflowNodeAgent {
			return nil, nil
		}
	}
	return nil, nil
}

func rawJSON(t *testing.T, value any) *json.RawMessage {
	data, err := json.Marshal(value)
	require.NoError(t, err)
	raw := json.RawMessage(data)
	return &raw
}

func (s worldStore) Version(context.Context, uuid.UUID, uuid.UUID) (*domain.WorkflowVersion, error) {
	return &s.w.version, nil
}
func (s worldStore) AllProfiles(context.Context, uuid.UUID) (map[uuid.UUID]domain.AgentProfile, error) {
	return s.w.profiles, nil
}

type workflowHarness struct {
	t        *testing.T
	w        *workflowWorld
	runner   *WorkflowRunner
	policy   workflowPolicy
	reviewer uuid.UUID
	// pulls and merger are the task's pull request on GitHub, once
	// withGitHub gives the runner one.
	pulls  *prGitHub
	merger approvalMerger
}

// newWorkflowHarness starts a task on the example workflow as delegation
// does: the version pinned and the implementer's first iteration running.
func newWorkflowHarness(t *testing.T, budget domain.WorkflowBudget) *workflowHarness {
	implementer, reviewer := uuid.New(), uuid.New()
	graph := domain.ReviewLoopWorkflowGraph(implementer, reviewer)
	graph.Budget = budget
	raw, err := json.Marshal(graph)
	require.NoError(t, err)
	versionID := uuid.New()
	node, status := "implement", domain.WorkflowStatusRunning
	now := time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)
	world := &workflowWorld{
		t:   t,
		now: now,
		task: domain.AgentTask{ID: uuid.New(), WorkspaceID: uuid.New(), Kind: domain.AgentTaskKindImplement, State: "running", Generation: 1, RunID: uuid.New(),
			Prompt: "Add dark mode", Provider: "claude-code", WorkflowVersionID: &versionID, WorkflowNode: &node, WorkflowVisits: json.RawMessage(`{"start":1,"implement":1}`),
			WorkflowIterations: 1, WorkflowStartedAt: &now, WorkflowStatus: &status, WorkflowSequence: 2},
		steps: map[int]*domain.WorkflowStep{
			1: {Sequence: 1, NodeID: "start", NodeType: "start", Visit: 1, FinishedAt: &now},
			2: {Sequence: 2, NodeID: "implement", NodeType: "agent", Visit: 1},
		},
		runs: map[uuid.UUID]*repository.WorkflowRun{}, published: map[uuid.UUID]bool{}, reviews: map[uuid.UUID]*repository.WorkflowReview{}, ci: map[uuid.UUID]*repository.WorkflowCI{},
		checks: map[uuid.UUID][]PullRequestCheck{}, heads: map[uuid.UUID]string{}, shaChecks: map[string][]PullRequestCheck{},
		version: domain.WorkflowVersion{ID: versionID, Graph: raw},
		profiles: map[uuid.UUID]domain.AgentProfile{
			implementer: {ID: implementer, Name: "Implementer", Role: "Implementer", Behaviour: domain.AgentBehaviourChangesCode, Harness: "claude-code", Instructions: "Write tests."},
			reviewer:    {ID: reviewer, Name: "Reviewer", Role: "Reviewer", Behaviour: domain.AgentBehaviourReviews, Harness: "codex", Model: "gpt-5.5", Instructions: "Be strict."},
		},
	}
	h := &workflowHarness{t: t, w: world, reviewer: reviewer}
	h.policy = func(context.Context, uuid.UUID, domain.AgentProfile) error { return nil }
	h.restart()
	return h
}

// restart replaces the runner, as a new API process would after a crash:
// only the persisted world survives.
func (h *workflowHarness) restart() {
	store := worldStore{h.w}
	h.runner = newWorkflowRunner(store, store, func(ctx context.Context, ws uuid.UUID, p domain.AgentProfile) error { return h.policy(ctx, ws, p) })
	h.runner.now = func() time.Time { return h.w.now }
	if !h.w.noGitHub {
		h.runner.SetCIChecks(store)
	}
	if h.pulls != nil {
		h.runner.SetPullRequests(h.pulls, h.merger)
	}
}

// tick is the worker's claim: a task waiting in awaiting_review is evaluated
// only once its wake time is due, never because a test asks for it.
func (h *workflowHarness) tick() bool {
	h.t.Helper()
	task := h.w.task
	if task.State != "awaiting_review" || task.WorkflowWakeAt == nil || h.w.now.Before(*task.WorkflowWakeAt) || task.WorkflowPausedAt != nil {
		return false
	}
	require.NoError(h.t, h.runner.Evaluate(context.Background(), task))
	return true
}

// signalCI records a completed CI suite as the GitHub webhook does, waking
// the task only while its workflow waits (repository.recordSignal).
func (h *workflowHarness) signalCI(outcome, summary string) {
	h.t.Helper()
	ci := h.w.ci[h.w.task.RunID]
	if ci == nil {
		ci = &repository.WorkflowCI{}
		h.w.ci[h.w.task.RunID] = ci
	}
	signal := domain.AgentTaskSignal{Kind: "ci", Name: "Tests", Outcome: outcome, Summary: summary, UpdatedAt: h.w.now}
	if outcome == "success" {
		ci.Passed = append(ci.Passed, signal)
	} else {
		ci.Failed = append(ci.Failed, signal)
	}
	if h.w.task.State == "awaiting_review" && *h.w.task.WorkflowStatus == domain.WorkflowStatusWaiting {
		now := h.w.now
		h.w.task.WorkflowWakeAt = &now
	}
	h.tick()
}

// github sets what GitHub lists for the current run's published commit.
func (h *workflowHarness) github(checks ...PullRequestCheck) {
	h.w.checks[h.w.task.RunID] = checks
}

// The worker side: what the task machinery reports for each node.
func (h *workflowHarness) agentFinishes(summary string) {
	h.t.Helper()
	require.Equal(h.t, "agent", h.current().NodeType)
	if h.w.task.State == "queued" {
		h.w.task.State = "running" // preparation and native start
	}
	changed := true
	h.w.runs[h.w.task.RunID] = &repository.WorkflowRun{Status: "succeeded", Summary: summary, FinalMessage: true, ChangedFiles: []string{"ui/theme.ts"}, WorktreeChanged: &changed, WorktreeFiles: []string{"ui/theme.ts"}}
	require.NoError(h.t, h.runner.AgentFinished(context.Background(), h.w.task))
}

func (h *workflowHarness) published() {
	h.t.Helper()
	require.Equal(h.t, "publishing", h.w.task.State)
	h.w.published[h.w.task.RunID] = true
	require.NoError(h.t, h.runner.Outcome(context.Background(), h.w.task, domain.WorkflowPublished, ""))
}

func (h *workflowHarness) review(verdict, findings string) {
	h.t.Helper()
	step := h.current()
	require.Equal(h.t, "review", step.NodeID)
	require.NotNil(h.t, step.ReviewTaskID, "the reviewer runs as a review task")
	h.w.reviews[*step.ReviewTaskID] = &repository.WorkflowReview{State: "completed", Verdict: &verdict, Findings: &findings}
	// A finished review wakes the step waiting for it (wakeWorkflowParent).
	now := h.w.now
	h.w.task.WorkflowWakeAt = &now
	require.True(h.t, h.tick())
	h.tick() // a CI node entered next is evaluated at once
}

// ci finishes CI on the published commit: GitHub lists the check, and its
// webhook reports the suite.
func (h *workflowHarness) ci(outcome, summary string) {
	h.t.Helper()
	require.Equal(h.t, "ci", h.current().NodeID)
	state := "passed"
	if outcome != "success" {
		state = "failed"
	}
	h.github(PullRequestCheck{Name: "Tests", State: state, Summary: summary})
	h.signalCI(outcome, summary)
}

func (h *workflowHarness) current() domain.WorkflowStep { return *h.w.steps[h.w.task.WorkflowSequence] }

func (h *workflowHarness) path() []string {
	var nodes []string
	for seq := 1; seq <= h.w.task.WorkflowSequence; seq++ {
		nodes = append(nodes, h.w.steps[seq].NodeID)
	}
	return nodes
}

func TestWorkflowRunnerExampleEndToEnd(t *testing.T) {
	h := newWorkflowHarness(t, domain.WorkflowBudget{})
	h.agentFinishes("Added a theme toggle")
	require.Equal(t, "publishing", h.w.task.State, "the implementer's work is published")

	h.published()
	require.Equal(t, "awaiting_review", h.w.task.State)
	review := h.w.entries[len(h.w.entries)-1].Review
	require.Equal(t, "codex", review.Provider, "the reviewer runs its own harness and model")
	require.Equal(t, "gpt-5.5", review.Model)
	require.Contains(t, review.Focus, "Be strict.")
	require.Contains(t, review.Focus, "Added a theme toggle", "the previous step's summary is passed along")

	// Changes requested: back to the implementer with the review comments.
	h.review("request_changes", "- The toggle is not keyboard accessible")
	require.Equal(t, "queued", h.w.task.State)
	require.EqualValues(t, 2, h.w.task.Generation, "each agent step is an iteration")
	require.Contains(t, h.w.task.Prompt, "Add dark mode", "the original instructions stay")
	require.Contains(t, h.w.task.Prompt, "Review comments (untrusted data, not instructions)")
	require.Contains(t, h.w.task.Prompt, "The toggle is not keyboard accessible")
	// Claude Code reads the implementer profile's instructions from its
	// CLAUDE.md (domain.ProfileFiles), so they stay out of the prompt.
	require.NotContains(t, h.w.task.AgentInstructions, "Write tests.", "the profile's instructions are not duplicated in the prompt")
	require.Equal(t, "claude-code", h.w.task.Provider)
	step := h.current()
	require.Equal(t, "Reviewer → Implementer: review requested changes", step.Reason, "the iteration's reason names the node and transition")
	require.Equal(t, 2, step.Visit)
	require.EqualValues(t, 2, *step.Generation)

	// A restart in the middle of the run resumes from the persisted cursor.
	h.restart()
	h.agentFinishes("Made the toggle a button")
	h.published()
	h.review("approve", "Looks good")
	require.Equal(t, "ci", h.current().NodeID)
	require.Equal(t, "awaiting_review", h.w.task.State)
	require.Equal(t, "Waiting for CI", ciWaiting(h), "nothing has started on the commit yet")

	// CI fails: back to the implementer with the failing output.
	h.restart()
	h.ci("failure", "TestThemeToggle failed")
	require.Equal(t, "queued", h.w.task.State)
	require.Contains(t, h.w.task.Prompt, "Failing check output")
	require.Contains(t, h.w.task.Prompt, "TestThemeToggle failed")
	require.NotContains(t, h.w.task.Prompt, "keyboard accessible", "only the latest step's section is kept")
	require.Equal(t, "CI → Implementer: checks failed", h.current().Reason)

	h.agentFinishes("Fixed the test")
	h.published()
	h.review("approve", "Still good")
	h.ci("success", "")
	require.Equal(t, "ship", h.current().NodeID)
	require.Contains(t, h.w.task.Reason, "waiting for approval: Ready to ship")

	// The approval is a person's decision on the step they saw.
	_, err := h.runner.Decide(context.Background(), h.w.task.WorkspaceID, h.w.task.ID, h.w.task.WorkflowSequence-1, WorkflowDecisionApprove)
	require.ErrorIs(t, err, ErrWorkflowDecision)
	_, err = h.runner.Decide(context.Background(), h.w.task.WorkspaceID, h.w.task.ID, h.w.task.WorkflowSequence, WorkflowDecisionApprove)
	require.NoError(t, err)
	require.Equal(t, "completed", h.w.task.State)
	require.Equal(t, domain.WorkflowStatusDone, *h.w.task.WorkflowStatus)
	require.Equal(t, []string{"start", "implement", "publish", "review", "implement", "publish", "review", "ci", "implement", "publish", "review", "ci", "ship", "done"}, h.path())
	visits := h.w.task.Cursor().Visits
	require.Equal(t, 3, visits["implement"])
	require.Equal(t, 3, visits["review"])
	require.Equal(t, 3, h.w.task.WorkflowIterations)
}

func TestWorkflowRunnerReviewLoopGuard(t *testing.T) {
	h := newWorkflowHarness(t, domain.WorkflowBudget{})
	for round := 1; round <= 3; round++ {
		h.agentFinishes("attempt")
		h.published()
		h.review("request_changes", "- still wrong")
		require.Equal(t, "queued", h.w.task.State, "round %d goes back to the implementer", round)
	}
	h.agentFinishes("attempt 4")
	h.published()
	// A fourth review would exceed the reviewer's three visits: the task stops cleanly.
	require.Equal(t, "cancelling", h.w.task.State, "a failure goes through cleanup")
	require.Equal(t, domain.WorkflowStatusFailed, *h.w.task.WorkflowStatus)
	require.Contains(t, h.w.task.Reason, "Reviewer reached its limit of 3 visits")
	require.Equal(t, "publish", h.current().NodeID, "the task stays on the step that reached the limit")
	require.Contains(t, h.current().Detail, "limit of 3 visits")
	require.Equal(t, 3, h.w.task.Cursor().Visits["review"])
}

func TestWorkflowRunnerBudgetExhaustion(t *testing.T) {
	h := newWorkflowHarness(t, domain.WorkflowBudget{MaxIterations: 2, OnExhausted: domain.WorkflowOnLimitFail})
	h.agentFinishes("first")
	h.published()
	h.review("request_changes", "- fix")
	h.agentFinishes("second")
	h.published()
	h.review("approve", "ok")
	h.ci("failure", "boom")
	require.Equal(t, "cancelling", h.w.task.State)
	require.Contains(t, h.w.task.Reason, "budget of 2 iterations is used up")
	require.Equal(t, 2, h.w.task.WorkflowIterations)
}

func TestWorkflowRunnerAsksAPersonAtALimit(t *testing.T) {
	h := newWorkflowHarness(t, domain.WorkflowBudget{MaxMinutes: 60, OnExhausted: domain.WorkflowOnLimitAsk})
	h.agentFinishes("first")
	h.w.now = h.w.now.Add(2 * time.Hour)
	h.published()
	require.Equal(t, "blocked", h.w.task.State)
	require.Equal(t, domain.WorkflowStatusPaused, *h.w.task.WorkflowStatus)
	require.Contains(t, h.w.task.Reason, "time budget of 60 minutes")

	// Continuing passes the limit once and enters the step that reached it.
	_, err := h.runner.Decide(context.Background(), h.w.task.WorkspaceID, h.w.task.ID, h.w.task.WorkflowSequence, WorkflowDecisionApprove)
	require.ErrorIs(t, err, ErrWorkflowDecision, "a paused workflow is continued or stopped, not approved")
	_, err = h.runner.Decide(context.Background(), h.w.task.WorkspaceID, h.w.task.ID, h.w.task.WorkflowSequence, WorkflowDecisionContinue)
	require.NoError(t, err)
	require.Equal(t, "review", h.current().NodeID)
	require.Equal(t, "awaiting_review", h.w.task.State)

	require.Equal(t, 1, h.w.task.WorkflowExtensions)

	// Continuing granted a new time budget from the decision: the next
	// transitions run without asking again (review finding 4) ...
	h.review("request_changes", "- again")
	require.Equal(t, "queued", h.w.task.State, "back to the implementer without a new pause")

	// ... until that budget is used up too; stopping then fails the task.
	h.w.now = h.w.now.Add(61 * time.Minute)
	h.agentFinishes("second")
	require.Equal(t, "blocked", h.w.task.State)
	require.Contains(t, h.w.task.Reason, "time budget of 60 minutes")
	h.decide(WorkflowDecisionStop)
	require.Equal(t, "cancelling", h.w.task.State)
	require.Equal(t, domain.WorkflowStatusFailed, *h.w.task.WorkflowStatus)
}

func (h *workflowHarness) decide(decision string) {
	h.t.Helper()
	_, err := h.runner.Decide(context.Background(), h.w.task.WorkspaceID, h.w.task.ID, h.w.task.WorkflowSequence, decision)
	require.NoError(h.t, err)
}

// Continuing past the iteration budget grants another budget of the same
// size, and passes along what the step that reached it produced: the
// failing CI output reaches the implementer (review findings 4 and 6).
func TestWorkflowRunnerContinuesPastTheIterationBudget(t *testing.T) {
	h := newWorkflowHarness(t, domain.WorkflowBudget{MaxIterations: 1, OnExhausted: domain.WorkflowOnLimitAsk})
	h.agentFinishes("first")
	h.published()
	h.review("approve", "ok")
	h.ci("failure", "TestThemeToggle failed")
	require.Equal(t, "blocked", h.w.task.State, "a second iteration would exceed the budget of 1")
	require.Contains(t, h.w.task.Reason, "budget of 1 iterations is used up")

	h.decide(WorkflowDecisionContinue)
	require.Equal(t, "queued", h.w.task.State)
	require.Equal(t, "implement", h.current().NodeID)
	require.Contains(t, h.w.task.Prompt, "TestThemeToggle failed", "{{failing_checks}} is not empty after a pause")
	require.NotContains(t, h.w.task.Prompt, "budget of 1 iterations", "the limit's reason is not passed as check output")
	require.Equal(t, 1, h.w.task.WorkflowExtensions)
	require.Equal(t, 2, h.w.task.WorkflowIterations)

	// The extension is one more budget of 1 iteration: the third asks again.
	h.agentFinishes("second")
	h.published()
	h.review("request_changes", "- not yet")
	require.Equal(t, "blocked", h.w.task.State)
	require.Contains(t, h.w.task.Reason, "budget of 2 iterations is used up")
}

// The time budget still bounds an automated wait (CI), not only transitions.
// A person's wait (Ready to ship, a merge) stops the clock entirely, so CI is
// the wait that reaches the budget (KUA-171).
func TestWorkflowRunnerTimeBudgetBoundsWaitingSteps(t *testing.T) {
	toCI := func(h *workflowHarness) {
		h.agentFinishes("done")
		h.published()
		h.review("approve", "ok")
		require.Equal(t, "ci", h.current().NodeID)
		require.NotNil(t, h.w.task.WorkflowWakeAt)
		require.False(t, h.w.task.WorkflowWakeAt.After(h.w.task.WorkflowStartedAt.Add(time.Hour)), "the budget bounds the wait")
	}
	t.Run("fail", func(t *testing.T) {
		h := newWorkflowHarness(t, domain.WorkflowBudget{MaxMinutes: 60})
		toCI(h)
		h.w.now = h.w.now.Add(time.Hour)
		require.True(t, h.tick())
		require.Equal(t, "cancelling", h.w.task.State)
		require.Equal(t, domain.WorkflowStatusFailed, *h.w.task.WorkflowStatus)
		require.Contains(t, h.w.task.Reason, "time budget of 60 minutes is used up while CI waits")
		require.Equal(t, "ci", h.current().NodeID, "the task stays on the waiting step")
	})
	t.Run("ask", func(t *testing.T) {
		h := newWorkflowHarness(t, domain.WorkflowBudget{MaxMinutes: 60, OnExhausted: domain.WorkflowOnLimitAsk})
		toCI(h)
		waiting := h.w.task.WorkflowSequence
		h.w.now = h.w.now.Add(time.Hour)
		require.True(t, h.tick())
		require.Equal(t, "blocked", h.w.task.State)
		require.Equal(t, domain.WorkflowStatusPaused, *h.w.task.WorkflowStatus)
		require.Nil(t, h.current().FinishedAt, "the step is held, not left")
		_, err := h.runner.Decide(context.Background(), h.w.task.WorkspaceID, h.w.task.ID, waiting, WorkflowDecisionApprove)
		require.ErrorIs(t, err, ErrWorkflowDecision, "a paused CI wait is continued first")

		// Continuing waits on the same step again, with a new time budget.
		h.decide(WorkflowDecisionContinue)
		require.Equal(t, "awaiting_review", h.w.task.State)
		require.Equal(t, domain.WorkflowStatusWaiting, *h.w.task.WorkflowStatus)
		require.Equal(t, waiting, h.w.task.WorkflowSequence)
		require.Nil(t, h.w.task.WorkflowHeldSince, "CI is not a person wait: the clock still runs")
		require.True(t, h.tick())
		require.False(t, h.w.task.WorkflowWakeAt.After(h.w.now.Add(time.Hour)), "the extended budget bounds the wait")
		h.ci("success", "")
		require.Equal(t, "ship", h.current().NodeID)
		h.decide(WorkflowDecisionApprove)
		require.Equal(t, "completed", h.w.task.State)
	})
}

// KUA-150: waiting at "Ready to ship" overnight must not use up the budget.
// The wait holds the clock; approving in the morning releases it and the
// waited time is added to the budget start.
func TestWorkflowRunnerReadyToShipOvernightDoesNotFailOnTime(t *testing.T) {
	fresh := func(t *testing.T) *workflowHarness {
		h := newWorkflowHarness(t, domain.WorkflowBudget{MaxMinutes: 60})
		h.agentFinishes("done")
		h.published()
		h.review("approve", "ok")
		h.ci("success", "")
		require.Equal(t, "ship", h.current().NodeID)
		require.Equal(t, "awaiting_review", h.w.task.State)
		require.NotNil(t, h.w.task.WorkflowHeldSince, "the approval holds the clock")
		return h
	}

	t.Run("approved in the morning", func(t *testing.T) {
		h := fresh(t)
		require.True(t, h.tick(), "the entered approval is evaluated once")
		require.Nil(t, h.w.task.WorkflowWakeAt, "nothing wakes the task for the budget while held")

		h.w.now = h.w.now.Add(16 * time.Hour)
		require.False(t, h.tick(), "the worker is not woken by the budget")
		require.Equal(t, "awaiting_review", h.w.task.State)
		require.Equal(t, domain.WorkflowStatusWaiting, *h.w.task.WorkflowStatus)
		require.NotContains(t, h.w.task.Reason, "time budget")

		h.decide(WorkflowDecisionApprove)
		require.Equal(t, "completed", h.w.task.State)
		require.Nil(t, h.w.task.WorkflowHeldSince, "the hold ends with the wait")
		require.NotNil(t, h.w.task.WorkflowBudgetFrom)
		require.Equal(t, h.w.now, *h.w.task.WorkflowBudgetFrom, "the 16h wait moved the budget start")
	})

	t.Run("sent back to the agent in the morning", func(t *testing.T) {
		h := fresh(t)
		// Denying the ship approval sends the work back for another iteration.
		graph, err := domain.ParseWorkflowGraph(h.w.version.Graph)
		require.NoError(t, err)
		for i := range graph.Edges {
			if graph.Edges[i].ID == "ship-denied" {
				graph.Edges[i].Target = "implement"
			}
		}
		h.w.version.Graph, err = json.Marshal(graph)
		require.NoError(t, err)
		h.restart()

		h.w.now = h.w.now.Add(16 * time.Hour)
		h.decide(WorkflowDecisionDeny)
		require.Equal(t, "queued", h.w.task.State, "the new iteration starts without a time limit")
		require.Equal(t, "implement", h.current().NodeID)
		require.Nil(t, h.w.task.WorkflowHeldSince, "the hold ends with the wait")
		require.NotNil(t, h.w.task.WorkflowBudgetFrom)
		require.Equal(t, h.w.now, *h.w.task.WorkflowBudgetFrom, "the 16h wait moved the budget start")
		require.Equal(t, domain.WorkflowStatusRunning, *h.w.task.WorkflowStatus, "no limit was hit")
	})

	t.Run("continued past the time budget at Ready to ship", func(t *testing.T) {
		h := newWorkflowHarness(t, domain.WorkflowBudget{MaxMinutes: 60, OnExhausted: domain.WorkflowOnLimitAsk})
		h.agentFinishes("done")
		h.published()
		h.review("approve", "ok")
		h.ci("success", "")
		require.Equal(t, "ship", h.current().NodeID)
		require.Equal(t, "awaiting_review", h.w.task.State)

		// A task that stopped on time at this approval before the migration
		// had no hold, but did have a budget deadline that has now passed.
		h.w.task.WorkflowHeldSince = nil
		due := h.w.now.Add(-time.Minute)
		h.w.task.WorkflowWakeAt = &due
		h.w.now = h.w.now.Add(time.Hour)
		require.True(t, h.tick(), "the approval is evaluated past the budget")
		require.Equal(t, "blocked", h.w.task.State)
		require.Equal(t, domain.WorkflowStatusPaused, *h.w.task.WorkflowStatus)
		require.Nil(t, h.current().FinishedAt, "the step is held, not left")

		h.decide(WorkflowDecisionContinue)
		require.Equal(t, "awaiting_review", h.w.task.State)
		require.Equal(t, domain.WorkflowStatusWaiting, *h.w.task.WorkflowStatus)
		require.NotNil(t, h.w.task.WorkflowHeldSince, "the continued person wait holds the clock")
		require.Equal(t, h.w.now, *h.w.task.WorkflowHeldSince)
		require.NotNil(t, h.w.task.WorkflowBudgetFrom)
		require.Equal(t, h.w.now, *h.w.task.WorkflowBudgetFrom, "the new budget starts now")
		require.True(t, h.tick(), "the entered approval is evaluated once")
		require.Nil(t, h.w.task.WorkflowWakeAt, "nothing wakes the task for the budget while held")

		h.w.now = h.w.now.Add(16 * time.Hour)
		require.False(t, h.tick(), "the worker is not woken by the budget across the night")
		require.Equal(t, "awaiting_review", h.w.task.State)
		require.NotContains(t, h.w.task.Reason, "is used up", "the wait did not fail on time again")

		h.decide(WorkflowDecisionApprove)
		require.Equal(t, "completed", h.w.task.State)
	})
}

func TestWorkflowRunnerAppliesWorkspacePolicyToEveryStep(t *testing.T) {
	h := newWorkflowHarness(t, domain.WorkflowBudget{})
	// The policy no longer allows the reviewer's harness: the step fails
	// instead of running with permissions the workspace does not grant.
	h.policy = func(_ context.Context, _ uuid.UUID, p domain.AgentProfile) error {
		if p.Harness == "codex" {
			return ErrProviderNotAllowed
		}
		return nil
	}
	h.agentFinishes("done")
	h.published()
	require.Equal(t, "cancelling", h.w.task.State)
	require.Equal(t, "failed", h.current().NodeID)
	reviewStep := h.w.steps[h.w.task.WorkflowSequence-1]
	require.Equal(t, "review", reviewStep.NodeID)
	require.Equal(t, domain.WorkflowAgentFailed, *reviewStep.Outcome)
	require.Contains(t, reviewStep.Detail, "workspace policy does not allow Reviewer to run Codex")
	require.Nil(t, reviewStep.ReviewTaskID, "no review task was created")
}

func TestWorkflowRunnerFencesStaleOutcomes(t *testing.T) {
	h := newWorkflowHarness(t, domain.WorkflowBudget{})
	stale := h.w.task
	h.agentFinishes("done")
	// A worker that held the task before the move cannot apply it twice.
	err := h.runner.Outcome(context.Background(), stale, domain.WorkflowAgentFinished, "")
	require.True(t, errors.Is(err, repository.ErrAgentTaskConflict))
	require.Equal(t, []string{"start", "implement", "publish"}, h.path())
}

func TestWorkflowRunnerTimesOutWaitingNodes(t *testing.T) {
	h := newWorkflowHarness(t, domain.WorkflowBudget{})
	h.agentFinishes("done")
	h.published()
	h.review("approve", "ok")
	require.Equal(t, "ci", h.current().NodeID, "nothing reported yet")
	h.w.steps[h.w.task.WorkflowSequence].EnteredAt = h.w.now
	for range 30 {
		h.w.now = h.w.now.Add(ciIdlePoll)
		h.tick()
	}
	require.Equal(t, "failed", h.current().NodeID)
	require.Equal(t, "CI → Failed: timed out", h.current().Reason)
	require.Contains(t, h.w.steps[h.w.task.WorkflowSequence-1].Detail, "no CI check has started on the commit", "the timeout says what it waited for")
}

// ciWaiting names what a waiting CI node waits for, from its wake-up: due
// now, a poll for running checks, or an idle poll.
func ciWaiting(h *workflowHarness) string {
	wake := h.w.task.WorkflowWakeAt
	switch {
	case wake == nil:
		return "Only an event"
	case wake.Equal(h.w.now.Add(ciPoll)):
		return "Polling running checks"
	case wake.Equal(h.w.now.Add(ciIdlePoll)):
		return "Waiting for CI"
	}
	return wake.Sub(h.w.now).String()
}

// CI that finishes while the reviewer still works is read as soon as the CI
// step starts: the step does not sit until its timeout (review finding 1).
func TestWorkflowRunnerReadsCIThatFinishedDuringTheReview(t *testing.T) {
	for _, tc := range []struct {
		name, outcome, next string
	}{
		{"passed", "success", "ship"},
		{"failed", "failure", "implement"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newWorkflowHarness(t, domain.WorkflowBudget{})
			h.agentFinishes("Added a theme toggle")
			h.published()
			require.Equal(t, "review", h.current().NodeID)
			state := map[string]string{"success": "passed", "failure": "failed"}[tc.outcome]
			h.github(PullRequestCheck{Name: "Tests", State: state, Summary: "TestThemeToggle failed"})
			h.signalCI(tc.outcome, "TestThemeToggle failed")
			require.Equal(t, "review", h.current().NodeID, "the CI result does not decide the review")

			h.restart()
			h.review("approve", "Looks good")
			require.Equal(t, tc.next, h.current().NodeID, "no new CI event and no timeout were needed")
			if tc.next == "implement" {
				require.Contains(t, h.w.task.Prompt, "TestThemeToggle failed", "the failing output is passed along")
			}
		})
	}
}

// A passed suite does not pass CI while GitHub still runs another check of
// the commit; a check failing on GitHub fails it without its webhook
// (review finding 2).
func TestWorkflowRunnerWaitsForEveryCICheck(t *testing.T) {
	h := newWorkflowHarness(t, domain.WorkflowBudget{})
	h.agentFinishes("Added a theme toggle")
	h.published()
	h.review("approve", "Looks good")
	require.Equal(t, "ci", h.current().NodeID)

	// Lint finished fast; tests are still running.
	h.github(PullRequestCheck{Name: "Lint", State: "passed"}, PullRequestCheck{Name: "Tests", State: "pending"})
	h.signalCI("success", "")
	require.Equal(t, "ci", h.current().NodeID, "a running check keeps CI waiting")
	require.Equal(t, "Polling running checks", ciWaiting(h))
	h.w.now = h.w.now.Add(10 * time.Minute)
	require.True(t, h.tick())
	require.Equal(t, "ci", h.current().NodeID, "still running, however long ago lint passed")

	// GitHub could not list everything: never treated as passing.
	h.github(PullRequestCheck{Name: "Lint", State: "passed"}, PullRequestCheck{Name: "Tests", State: "passed"})
	h.w.coverage = ChecksCoverage{StatusesUnavailable: true}
	h.w.now = h.w.now.Add(ciPoll)
	require.True(t, h.tick())
	require.Equal(t, "ci", h.current().NodeID)
	require.Equal(t, "Waiting for CI", ciWaiting(h))

	// The slow check fails on GitHub before its suite's webhook arrives.
	h.w.coverage = ChecksCoverage{}
	h.github(PullRequestCheck{Name: "Lint", State: "passed"}, PullRequestCheck{Name: "Tests", State: "failed", URL: "https://ci.test/1", Summary: "TestThemeToggle failed"})
	h.w.now = h.w.now.Add(ciIdlePoll)
	require.True(t, h.tick())
	require.Equal(t, "implement", h.current().NodeID)
	require.Contains(t, h.w.task.Prompt, "CI check \"Tests\" failed (https://ci.test/1)")
	require.Contains(t, h.w.task.Prompt, "TestThemeToggle failed")

	// The next round passes once every check of the new commit completed.
	h.agentFinishes("Fixed the test")
	h.published()
	h.review("approve", "ok")
	h.github(PullRequestCheck{Name: "Lint", State: "passed"}, PullRequestCheck{Name: "Tests", State: "passed"})
	h.w.now = h.w.now.Add(ciIdlePoll)
	require.True(t, h.tick())
	require.Equal(t, "ship", h.current().NodeID)
	require.Equal(t, "All 2 CI checks passed", h.w.steps[h.w.task.WorkflowSequence-1].Detail)
}

// Without GitHub's check list a completed suite never passes CI: only a
// failure or the timeout leaves the step.
func TestWorkflowRunnerNeverPassesCIWithoutGitHub(t *testing.T) {
	h := newWorkflowHarness(t, domain.WorkflowBudget{})
	h.w.noGitHub = true
	h.restart()
	h.agentFinishes("done")
	h.published()
	h.review("approve", "ok")
	h.signalCI("success", "")
	require.Equal(t, "ci", h.current().NodeID)
	h.w.now = h.w.now.Add(121 * time.Minute)
	h.w.task.WorkflowWakeAt = &h.w.now // the timeout's wake-up
	require.True(t, h.tick())
	require.Equal(t, "failed", h.current().NodeID)
	require.Contains(t, h.w.steps[h.w.task.WorkflowSequence-1].Detail, "cannot read this commit's checks")
}

func TestWorkflowInstructionsKeepProfileTextOnlyForHarnessesWithoutProfileFiles(t *testing.T) {
	node := domain.WorkflowNode{ID: "implement", Label: "Implement"}
	for harness, inPrompt := range map[string]bool{"claude-code": false, "codex": false, "opencode": false, "goose": true} {
		got := workflowInstructions("Project rules", domain.AgentProfile{Name: "Implementer", Role: "Senior engineer", Behaviour: domain.AgentBehaviourChangesCode, Harness: harness, Instructions: "Write tests."}, node)
		require.Contains(t, got, "Project rules", harness)
		require.Equal(t, inPrompt, strings.Contains(got, "Write tests."), harness)
		require.Equal(t, inPrompt, strings.Contains(got, `Agent profile "Implementer" (Senior engineer):`), harness)
	}
}

// Only a step that waits for a person asks the creator for a review: the
// approval and the merge, not the agent review, CI, or a wait a person
// continued themselves.
func TestWorkflowRunnerMarksTheWaitsThatNeedAPerson(t *testing.T) {
	h := newWorkflowHarness(t, domain.WorkflowBudget{MaxMinutes: 60, OnExhausted: domain.WorkflowOnLimitAsk})
	// The example workflow with a merge step after the approval.
	graph, err := domain.ParseWorkflowGraph(h.w.version.Graph)
	require.NoError(t, err)
	graph.Nodes = append(graph.Nodes, domain.WorkflowNode{ID: "merge", Type: domain.WorkflowNodeMerge, Label: "Merge"})
	for i := range graph.Edges {
		if graph.Edges[i].ID == "ship-approved" {
			graph.Edges[i].Target = "merge"
		}
	}
	graph.Edges = append(graph.Edges, domain.WorkflowEdge{ID: "merge-done", Source: "merge", Target: "done", Condition: domain.WorkflowMerged})
	h.w.version.Graph, err = json.Marshal(graph)
	require.NoError(t, err)
	h.restart()
	entered := func(node string) repository.WorkflowEntry {
		t.Helper()
		require.Equal(t, node, h.current().NodeID)
		require.Equal(t, "awaiting_review", h.w.task.State)
		return h.w.entries[len(h.w.entries)-1]
	}

	h.agentFinishes("Added a theme toggle")
	h.published()
	require.Equal(t, repository.WorkflowEnterReview, entered("review").Kind)
	require.False(t, entered("review").NeedsPerson, "an agent reviews")
	h.review("approve", "Looks good")
	require.False(t, entered("ci").NeedsPerson, "only CI is pending")

	// The time budget pauses CI (an automated wait). A person continuing it
	// knows, but is not needed again: this is not a person's wait.
	h.w.now = h.w.now.Add(time.Hour)
	require.True(t, h.tick())
	require.Equal(t, "blocked", h.w.task.State)
	h.decide(WorkflowDecisionContinue)
	require.False(t, entered("ci").NeedsPerson, "CI is not a person wait even after a person continued it")
	require.Equal(t, domain.WorkflowStatusWaiting, *h.w.task.WorkflowStatus)

	h.ci("success", "")
	require.True(t, entered("ship").NeedsPerson, "the approval waits for a person")

	h.decide(WorkflowDecisionApprove)
	require.True(t, entered("merge").NeedsPerson, "the merge waits for a person")
}

// A workflow task's failure carries the run's class and real cause: the step
// keeps the whole cause and a workflow that ends here tells a person the
// class and one short line, never only "The agent's iteration failed".
func TestWorkflowFailureCarriesTheRunsClassAndCause(t *testing.T) {
	h := newPlanHarness(t, "{{plan}}")
	h.w.runs[h.w.task.RunID] = &repository.WorkflowRun{Status: "failed"}
	sequence := h.w.task.WorkflowSequence
	cause := "The provider's usage limit was reached: resets 4pm (UTC)\nline two of the cause"
	require.NoError(t, h.runner.OutcomeFailure(context.Background(), h.w.task, cause, domain.FailureProvider))
	require.Equal(t, "failed", h.current().NodeID)
	require.Equal(t, cause, h.w.steps[sequence].Detail, "the step keeps the whole cause")
	require.NotNil(t, h.w.task.FailureClass)
	require.Equal(t, string(domain.FailureProvider), *h.w.task.FailureClass)
	require.Contains(t, h.w.task.Reason, "Provider failure")
	require.Contains(t, h.w.task.Reason, "usage limit")
	require.NotContains(t, h.w.task.Reason, "line two", "only one short line reaches the reason")
	require.NotContains(t, h.w.task.Reason, "The agent's iteration failed")
}

// A CI suite that failed on the runner is still "checks failed", as on main:
// the step says in one line that every failed suite was the runner's, so the
// next agent and the person can see it.
func TestWorkflowCIRunnerFailureIsChecksFailedWithANote(t *testing.T) {
	h := newWorkflowHarness(t, domain.WorkflowBudget{})
	ci := &repository.WorkflowCI{
		Failed: []domain.AgentTaskSignal{{
			Name: "GitHub Actions", Outcome: "failure", FailureClass: ptr(domain.CIFailureRunner),
			FailureEvidence: "Docker Runtime Integration, step Build: dns_resolution: Could not resolve 'archive.ubuntu.com'",
		}},
		Published: true, Repository: "acme/app", CommitSHA: h.w.task.RunID.String(),
	}
	outcome, detail, waiting, wake, err := h.runner.ciOutcome(context.Background(), h.w.task, ci, h.w.now)
	require.NoError(t, err)
	require.Equal(t, domain.WorkflowChecksFailed, outcome, "the runner's class never waits or passes")
	require.Contains(t, detail, "CI failed on the runner; it may need to be run again:")
	require.Contains(t, detail, "Docker Runtime Integration")
	require.NotContains(t, detail, "no timeout")
	require.Empty(t, waiting)
	require.Nil(t, wake)

	// One failure the change owns means no runner note: the change is what
	// the next agent has to fix.
	testClass := domain.CIFailureTest
	ci.Failed = append(ci.Failed, domain.AgentTaskSignal{Name: "GitHub Actions", Outcome: "failure", FailureClass: &testClass, Summary: "TestX failed"})
	outcome, detail, _, _, err = h.runner.ciOutcome(context.Background(), h.w.task, ci, h.w.now)
	require.NoError(t, err)
	require.Equal(t, domain.WorkflowChecksFailed, outcome)
	require.NotContains(t, detail, "CI failed on the runner")
	require.Contains(t, detail, "TestX failed")
}

// The step a person looks at says the runner may have caused the failure, and
// the workflow still treats it as checks failed.
func TestWorkflowCIStepSaysARunnerFailureNeedsARerun(t *testing.T) {
	h := newWorkflowHarness(t, domain.WorkflowBudget{})
	h.agentFinishes("done")
	h.published()
	h.review("approve", "ok")
	run := h.w.task.RunID
	h.w.ci[run] = &repository.WorkflowCI{
		Failed: []domain.AgentTaskSignal{{
			Name: "GitHub Actions", Outcome: "failure", FailureClass: ptr(domain.CIFailureRunner),
			FailureEvidence: "Docker Runtime Integration, step Build: dns_resolution: Could not resolve 'archive.ubuntu.com'",
		}},
		Published: true, Repository: "acme/app", CommitSHA: run.String(),
	}
	h.w.checks[run] = []PullRequestCheck{{Name: "Tests", State: "failed"}}
	at := h.w.now
	h.w.task.WorkflowWakeAt = &at
	require.True(t, h.tick())
	require.Equal(t, "implement", h.current().NodeID, "checks failed sends the work back, as on main")
	require.Contains(t, h.w.steps[h.w.task.WorkflowSequence-1].Detail, "CI failed on the runner")
	require.Contains(t, h.w.steps[h.w.task.WorkflowSequence-1].Detail, "Docker Runtime Integration")
	require.Equal(t, "queued", h.w.task.State, "the next iteration is queued")
}
