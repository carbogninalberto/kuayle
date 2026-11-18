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
	gh "github.com/kuayle/kuayle-backend/pkg/github"
	"github.com/stretchr/testify/require"
)

// rerunChecks is GitHub as a CI step sees it, with the re-run of failed jobs
// (KUA-215) and, when details is set, the background read of failed checks.
type rerunChecks struct {
	worldStore
	reruns  [][]int64
	err     error
	onRerun func()
}

func (c *rerunChecks) RerunFailedJobs(_ context.Context, _ uuid.UUID, repository string, runs []int64) error {
	if repository != "acme/app" {
		return errors.New("unknown repository")
	}
	c.reruns = append(c.reruns, runs)
	if c.err != nil {
		return c.err
	}
	if c.onRerun != nil {
		c.onRerun()
	}
	return nil
}

// withRerun lets the harness's CI steps run failed jobs again.
func (h *workflowHarness) withRerun() *rerunChecks {
	checks := &rerunChecks{worldStore: worldStore{h.w}}
	h.runner.SetCIChecks(checks)
	return checks
}

// The CI result of KUA-185: one Actions job of run 44137520077 failed on a
// flaky end-to-end test.
func flakyJob(job int, state string) PullRequestCheck {
	return PullRequestCheck{Name: "Tests / CI Result", State: state, URL: "https://github.com/acme/app/actions/runs/44137520077/job/" + strings.Repeat("1", job)}
}

// toCI runs the example workflow to its CI step.
func (h *workflowHarness) toCI(summary string) {
	h.t.Helper()
	h.agentFinishes(summary)
	h.published()
	h.review("approve", "Looks good")
	require.Equal(h.t, "ci", h.current().NodeID)
}

// unchangedAttempt runs the next attempt to CI without a new commit: the
// agent changed nothing, and publishing published the same head again.
func (h *workflowHarness) unchangedAttempt(head string) {
	h.t.Helper()
	require.Equal(h.t, "implement", h.current().NodeID)
	h.w.heads[h.w.task.RunID] = head
	h.toCI("CI failed on a flaky test; nothing to change")
}

// due wakes the waiting step now, as an event would.
func (h *workflowHarness) due() bool {
	h.t.Helper()
	at := h.w.now
	h.w.task.WorkflowWakeAt = &at
	return h.tick()
}

func (h *workflowHarness) ciRecord() *domain.WorkflowCIRecord {
	h.t.Helper()
	record := h.current().CIRecord()
	require.NotNil(h.t, record)
	return record
}

// KUA-185: CI failed on a flaky test, the agent pushed nothing, and the CI
// step failed again on the same Actions run until the budget ran out. Now the
// same failure on the same commit is not a new result: Kuayle runs the failed
// jobs again once and judges that run.
func TestCIOnAnUnchangedCommitRunsTheFailedJobsAgainInsteadOfLooping(t *testing.T) {
	h := newWorkflowHarness(t, domain.WorkflowBudget{MaxIterations: 8})
	checks := h.withRerun()
	h.toCI("Added a theme toggle")
	head := h.head()
	h.w.shaChecks[head] = []PullRequestCheck{flakyJob(1, "failed")}
	require.True(t, h.due())
	require.Equal(t, "implement", h.current().NodeID, "the first failure is a result: back to the agent")
	failedOn := h.w.steps[h.w.task.WorkflowSequence-1].CIRecord()
	require.NotNil(t, failedOn)
	require.Equal(t, head, failedOn.Head)
	require.Equal(t, []string{flakyJob(1, "").URL}, failedOn.Failed, "the step records what it judged")
	iterations := h.w.task.WorkflowIterations

	h.unchangedAttempt(head)
	require.Equal(t, "ci", h.current().NodeID, "the same failure on the same commit is not judged again")
	require.Equal(t, "awaiting_review", h.w.task.State)
	require.Equal(t, [][]int64{{44137520077}}, checks.reruns, "Kuayle asked GitHub to run the failed jobs again")
	record := h.ciRecord()
	require.NotNil(t, record.RerunAt)
	require.Equal(t, []string{flakyJob(1, "").URL}, record.Rerun)
	require.Equal(t, "Polling running checks", ciWaiting(h))

	// GitHub has not started them yet: its list still shows the old failure.
	require.True(t, h.later(ciPoll))
	require.Equal(t, "ci", h.current().NodeID, "the old failure is not taken as the re-run's result")

	// The re-run passes: CI passed, without another iteration.
	h.w.shaChecks[head] = []PullRequestCheck{flakyJob(2, "pending")}
	require.True(t, h.later(ciPoll))
	require.Equal(t, "ci", h.current().NodeID)
	h.w.shaChecks[head] = []PullRequestCheck{flakyJob(2, "passed")}
	require.True(t, h.later(ciPoll))
	require.Equal(t, "ship", h.current().NodeID)
	require.Contains(t, h.w.steps[h.w.task.WorkflowSequence-1].Detail, "which failed CI before")
	require.Equal(t, iterations, h.w.task.WorkflowIterations, "judging the re-run started no iteration")
	require.Len(t, checks.reruns, 1)
}

// When the jobs run again and still fail on a commit nothing was pushed to,
// the workflow pauses for a person instead of sending the same failure back.
// Continuing sends it back; the next unchanged attempt does not run the jobs
// a second time.
func TestCIThatStillFailsWithoutChangesPausesForAPerson(t *testing.T) {
	h := newWorkflowHarness(t, domain.WorkflowBudget{MaxIterations: 8})
	checks := h.withRerun()
	h.toCI("Added a theme toggle")
	head := h.head()
	h.w.shaChecks[head] = []PullRequestCheck{flakyJob(1, "failed")}
	require.True(t, h.due())
	h.unchangedAttempt(head)
	require.Len(t, checks.reruns, 1)

	h.w.shaChecks[head] = []PullRequestCheck{flakyJob(2, "failed")}
	require.True(t, h.later(ciPoll))
	require.Equal(t, "blocked", h.w.task.State)
	require.Equal(t, domain.WorkflowStatusPaused, *h.w.task.WorkflowStatus)
	require.Contains(t, h.w.task.Reason, "No changes were made since CI failed on "+shortSHA(head)+", and CI still fails on Tests / CI Result after Kuayle ran the failed jobs again")
	require.Contains(t, h.w.task.Reason, "Continue to send the failing checks to Implementer, or stop the workflow")
	ci := h.current()
	require.Equal(t, "ci", ci.NodeID, "the workflow stays on the CI step")
	require.Equal(t, domain.WorkflowChecksFailed, *ci.Outcome)
	require.Contains(t, ci.Detail, flakyJob(2, "").URL)
	require.Equal(t, []string{flakyJob(2, "").URL}, ci.CIRecord().Failed)
	iterations := h.w.task.WorkflowIterations

	// Continuing sends the failing checks back, without the pause's reason.
	h.decide(WorkflowDecisionContinue)
	require.Equal(t, "implement", h.current().NodeID)
	require.Equal(t, "queued", h.w.task.State)
	require.Contains(t, h.w.task.Prompt, flakyJob(2, "").URL)
	require.NotContains(t, h.w.task.Prompt, ciNoChanges, "the pause's reason is not check output")
	require.Equal(t, iterations+1, h.w.task.WorkflowIterations)

	// Nothing pushed again: no second re-run, a person decides again at once.
	h.unchangedAttempt(head)
	require.Equal(t, "blocked", h.w.task.State)
	require.Len(t, checks.reruns, 1, "the failed jobs run again once per commit")
	require.Contains(t, h.w.task.Reason, "and CI still fails on Tests / CI Result")
	h.decide(WorkflowDecisionStop)
	require.Equal(t, "cancelling", h.w.task.State)
	require.Equal(t, domain.WorkflowStatusFailed, *h.w.task.WorkflowStatus)

	// A new commit is a new result as usual.
	h = newWorkflowHarness(t, domain.WorkflowBudget{})
	checks = h.withRerun()
	h.toCI("Added a theme toggle")
	h.w.shaChecks[h.head()] = []PullRequestCheck{flakyJob(1, "failed")}
	require.True(t, h.due())
	h.toCI("Fixed the test")
	h.github(PullRequestCheck{Name: "Tests / CI Result", State: "failed", URL: "https://github.com/acme/app/actions/runs/9/job/3"})
	require.True(t, h.due())
	require.Equal(t, "implement", h.current().NodeID, "a failure on a new commit goes back to the agent")
	require.Empty(t, checks.reruns)
}

// Kuayle only runs GitHub Actions jobs again, and only with the app's
// permission; otherwise the workflow pauses at once with why.
func TestCIThatCannotRunAgainPausesWithTheReason(t *testing.T) {
	for _, tc := range []struct {
		name  string
		check PullRequestCheck
		err   error
		want  string
	}{
		{"another CI", PullRequestCheck{Name: "Buildkite", State: "failed", URL: "https://buildkite.example/builds/7"}, nil, "Kuayle can only run GitHub Actions jobs again"},
		{"no permission", flakyJob(1, "failed"), &gh.APIError{Status: 403, Message: "Resource not accessible by integration"}, "lacks the Actions write permission"},
		{"turned off", flakyJob(1, "failed"), &GitHubBotAbilityDisabledError{Ability: domain.GitHubBotPullRequestActions}, "turned off"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newWorkflowHarness(t, domain.WorkflowBudget{})
			checks := h.withRerun()
			checks.err = tc.err
			h.toCI("Added a theme toggle")
			head := h.head()
			h.w.shaChecks[head] = []PullRequestCheck{tc.check}
			require.True(t, h.due())
			h.unchangedAttempt(head)
			require.Equal(t, "blocked", h.w.task.State)
			require.Contains(t, h.w.task.Reason, ciNoChanges+shortSHA(head))
			require.Contains(t, strings.ToLower(h.w.task.Reason), strings.ToLower(tc.want))
			require.Contains(t, h.w.task.Reason, "Run the failed jobs again on GitHub, then continue")
		})
	}

	// GitHub that cannot be reached is asked again later.
	h := newWorkflowHarness(t, domain.WorkflowBudget{})
	checks := h.withRerun()
	checks.err = &gh.APIError{Status: 502, Message: "Bad gateway"}
	h.toCI("Added a theme toggle")
	head := h.head()
	h.w.shaChecks[head] = []PullRequestCheck{flakyJob(1, "failed")}
	require.True(t, h.due())
	h.unchangedAttempt(head)
	require.Equal(t, "ci", h.current().NodeID)
	require.Equal(t, "Waiting for CI", ciWaiting(h))
	checks.err = nil
	require.True(t, h.later(ciIdlePoll))
	require.Len(t, checks.reruns, 2)
	require.NotNil(t, h.ciRecord().RerunAt)
}

// GitHub that never starts the jobs again does not keep the step forever.
func TestCIRerunThatNeverStartsPausesAfterAWhile(t *testing.T) {
	h := newWorkflowHarness(t, domain.WorkflowBudget{})
	h.withRerun()
	h.toCI("Added a theme toggle")
	head := h.head()
	h.w.shaChecks[head] = []PullRequestCheck{flakyJob(1, "failed")}
	require.True(t, h.due())
	h.unchangedAttempt(head)
	for range 9 {
		require.True(t, h.later(ciPoll))
		require.Equal(t, "ci", h.current().NodeID)
	}
	require.True(t, h.later(ciPoll))
	require.Equal(t, "blocked", h.w.task.State)
	require.Contains(t, h.w.task.Reason, "CI still fails on Tests / CI Result after Kuayle ran the failed jobs again")
}

// detailsReader is GitHub with the background read of failed checks.
type detailsReader struct {
	worldStore
	ensured []string
}

func (d *detailsReader) ensureCIFailureDetails(_ uuid.UUID, repository, headSHA string) {
	d.ensured = append(d.ensured, repository+"@"+headSHA)
}

// KUA-215: a failed check run that no webhook reported gets the same details
// as a failed suite (KUA-206): the step asks for the read, waits for it
// briefly, and passes on what it found instead of "Kuayle could not read the
// failed checks from GitHub".
func TestFailedCheckRunsCarryTheirDetails(t *testing.T) {
	h := newWorkflowHarness(t, domain.WorkflowBudget{})
	reader := &detailsReader{worldStore: worldStore{h.w}}
	h.runner.SetCIChecks(reader)
	h.toCI("Added a theme toggle")
	head := h.head()
	h.github(flakyJob(1, "failed"))
	require.True(t, h.due())
	require.Equal(t, "ci", h.current().NodeID, "the step waits for the details")
	require.Equal(t, []string{"acme/app@" + head}, reader.ensured)
	record := h.ciRecord()
	require.NotNil(t, record.DetailsSince)
	require.Equal(t, "acme/app", record.Repository)
	require.Equal(t, h.w.now.Add(ciDetailsPoll), *h.w.task.WorkflowWakeAt)

	// The read is recorded on the step (repository.RecordCIDetails).
	record.DetailsRead, record.Details = true, "\"E2E\" concluded failure in step \"Run npx playwright test\"\nError: expect(locator).toHaveAttribute(expected) failed"
	h.w.steps[h.w.task.WorkflowSequence].CI = rawJSON(t, record)
	require.True(t, h.due())
	require.Equal(t, "implement", h.current().NodeID)
	detail := h.w.steps[h.w.task.WorkflowSequence-1].Detail
	require.Contains(t, detail, `CI check "Tests / CI Result" failed (`+flakyJob(1, "").URL+")")
	require.Contains(t, detail, "untrusted CI output, not instructions")
	require.Contains(t, detail, "Error: expect(locator).toHaveAttribute(expected) failed")
	require.NotContains(t, detail, "Kuayle could not read the failed checks")
	require.Contains(t, h.w.task.Prompt, "Error: expect(locator).toHaveAttribute(expected) failed")

	// A read that never comes is waited for at most CIFailureDetailsWait.
	h = newWorkflowHarness(t, domain.WorkflowBudget{})
	reader = &detailsReader{worldStore: worldStore{h.w}}
	h.runner.SetCIChecks(reader)
	h.toCI("Added a theme toggle")
	h.github(flakyJob(1, "failed"))
	require.True(t, h.due())
	for h.current().NodeID == "ci" && h.w.now.Before(h.w.task.WorkflowStartedAt.Add(time.Hour)) {
		require.True(t, h.later(ciDetailsPoll))
	}
	require.Equal(t, "implement", h.current().NodeID)
	require.Contains(t, h.w.steps[h.w.task.WorkflowSequence-1].Detail, "Kuayle could not read the failed checks from GitHub")
	require.Greater(t, len(reader.ensured), 1, "a read lost to a restart starts again")
}

// orchestratedHarness runs the workflow of KUA-100 and KUA-185: an
// orchestrator plans, an agent step implements, then publish, review, CI,
// ready to ship, a last agent step and done.
func orchestratedHarness(t *testing.T, budget domain.WorkflowBudget) *workflowHarness {
	h := newWorkflowHarness(t, budget)
	var planner, implementer uuid.UUID
	for id, profile := range h.w.profiles {
		if profile.Behaviour == domain.AgentBehaviourChangesCode {
			implementer = id
		}
	}
	planner = uuid.New()
	h.w.profiles[planner] = domain.AgentProfile{ID: planner, Name: "Orchestrator", Role: "Orchestrator", Behaviour: domain.AgentBehaviourPlans, Harness: "claude-code"}
	graph := domain.WorkflowGraph{
		Nodes: []domain.WorkflowNode{
			{ID: "start", Type: domain.WorkflowNodeStart, Label: "Start"},
			{ID: "implement", Type: domain.WorkflowNodeAgent, Label: "Orchestrator", ProfileID: &planner, Prompt: "{{plan}}"},
			{ID: "agent-1", Type: domain.WorkflowNodeAgent, Label: "Agent step", ProfileID: &implementer, Prompt: "{{plan}}\n\n{{review_comments}}\n\n{{failing_checks}}"},
			{ID: "publish", Type: domain.WorkflowNodePublish, Label: "Publish"},
			{ID: "review", Type: domain.WorkflowNodeAgent, Label: "Reviewer", ProfileID: &h.reviewer, MaxVisits: 20},
			{ID: "ci", Type: domain.WorkflowNodeCheck, Label: "CI", Check: domain.WorkflowCheckCI, TimeoutMinutes: 120},
			{ID: "ship", Type: domain.WorkflowNodeApproval, Label: "Ready to ship"},
			{ID: "agent-2", Type: domain.WorkflowNodeAgent, Label: "Agent step", ProfileID: &implementer, MaxVisits: 3},
			{ID: "done", Type: domain.WorkflowNodeEnd, Label: "Done", Outcome: domain.WorkflowEndDone},
			{ID: "failed", Type: domain.WorkflowNodeEnd, Label: "Failed", Outcome: domain.WorkflowEndFailed},
		},
		Edges: []domain.WorkflowEdge{
			{ID: "e1", Source: "start", Target: "implement", Condition: domain.WorkflowElse},
			{ID: "e2", Source: "implement", Target: "agent-1", Condition: domain.WorkflowAgentFinished},
			{ID: "e3", Source: "implement", Target: "failed", Condition: domain.WorkflowAgentFailed},
			{ID: "e4", Source: "agent-1", Target: "publish", Condition: domain.WorkflowAgentFinished},
			{ID: "e5", Source: "agent-1", Target: "failed", Condition: domain.WorkflowAgentFailed},
			{ID: "e6", Source: "publish", Target: "review", Condition: domain.WorkflowPublished},
			{ID: "e7", Source: "publish", Target: "failed", Condition: domain.WorkflowPublishFailed},
			{ID: "e8", Source: "review", Target: "ci", Condition: domain.WorkflowReviewApproved},
			{ID: "e9", Source: "review", Target: "implement", Condition: domain.WorkflowReviewChangesRequested},
			{ID: "e10", Source: "review", Target: "failed", Condition: domain.WorkflowAgentFailed},
			{ID: "e11", Source: "ci", Target: "ship", Condition: domain.WorkflowChecksPassed},
			{ID: "e12", Source: "ci", Target: "implement", Condition: domain.WorkflowChecksFailed},
			{ID: "e13", Source: "ci", Target: "failed", Condition: domain.WorkflowTimeout},
			{ID: "e14", Source: "ship", Target: "agent-2", Condition: domain.WorkflowApprovalApproved},
			{ID: "e15", Source: "ship", Target: "failed", Condition: domain.WorkflowApprovalDenied},
			{ID: "e16", Source: "agent-2", Target: "done", Condition: domain.WorkflowAgentFinished},
			{ID: "e17", Source: "agent-2", Target: "failed", Condition: domain.WorkflowAgentFailed},
		},
		Budget: budget,
	}
	require.Empty(t, domain.ValidateWorkflowGraph(graph, h.w.profiles))
	raw, err := json.Marshal(graph)
	require.NoError(t, err)
	h.w.version.Graph = raw
	h.restart()
	return h
}

// plans finishes the orchestrator's planning iteration with a plan.
func (h *workflowHarness) plans(plan string) {
	h.t.Helper()
	require.Equal(h.t, "implement", h.current().NodeID)
	if h.w.task.State == "queued" {
		h.w.task.State = "running"
	}
	h.w.runs[h.w.task.RunID] = &repository.WorkflowRun{Status: "succeeded", Summary: plan, FinalMessage: true, WorktreeChanged: unchanged()}
	require.NoError(h.t, h.runner.AgentFinished(context.Background(), h.w.task))
	require.Equal(h.t, "agent-1", h.current().NodeID)
}

// KUA-215: the orchestrator's plan and the agent step that implements it are
// one iteration, so a budget of 8 is 8 rounds of review (KUA-100 ran out
// after 4).
func TestAnOrchestratedRoundIsOneIteration(t *testing.T) {
	h := orchestratedHarness(t, domain.WorkflowBudget{MaxIterations: 8, OnExhausted: domain.WorkflowOnLimitFail})
	for round := 1; round <= 8; round++ {
		h.plans("1. Do round " + strings.Repeat("x", round))
		require.Equal(t, round, h.w.task.WorkflowIterations, "round %d", round)
		h.agentFinishes("Implemented the plan")
		h.published()
		h.review("request_changes", "- one more thing")
		if round < 8 {
			require.Equal(t, "implement", h.current().NodeID, "round %d goes back to the orchestrator", round)
			require.Equal(t, "queued", h.w.task.State)
		}
	}
	require.Equal(t, "cancelling", h.w.task.State, "a ninth round exceeds the budget")
	require.Contains(t, h.w.task.Reason, "budget of 8 iterations is used up")
	require.Equal(t, 8, h.w.task.WorkflowIterations)
	require.Equal(t, 8, h.w.task.Cursor().Visits["agent-1"], "each round ran the implementer")
}
