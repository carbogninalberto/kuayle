package service

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	gh "github.com/kuayle/kuayle-backend/pkg/github"
	"github.com/stretchr/testify/require"
)

// A failed check suite is classified from the checks Kuayle reads and from
// the suite's own conclusion when that read fails.
func TestFailedCheckSuitesAreClassifiedFromWhatKuayleCanRead(t *testing.T) {
	svc := NewGitHubService(nil, nil, nil, nil, nil, nil, nil, "", "", nil)
	calls := 0
	svc.ciChecks = func(context.Context, uuid.UUID, string, string) []domain.CIFailedCheck {
		calls++
		return []domain.CIFailedCheck{{
			Name: "Docker Runtime Integration", Conclusion: "failure", FailedStep: "Install packages",
			Text: "E: Failed to fetch http://azure.archive.ubuntu.com/ubuntu/dists/noble/InRelease  Temporary failure resolving 'azure.archive.ubuntu.com'",
		}}
	}
	failure := svc.ciFailure(context.Background(), uuid.New(), "acme/app", "abc123", "failure")
	require.NotNil(t, failure)
	require.Equal(t, domain.CIFailureRunner, failure.Class)
	require.Equal(t, "runner_output", failure.Rule)
	require.Contains(t, failure.Evidence, "Temporary failure resolving")
	require.Equal(t, 1, calls)

	// An unreadable check list never fails the webhook: the conclusion alone
	// still classifies the failure, as before this classification existed.
	svc.ciChecks = func(context.Context, uuid.UUID, string, string) []domain.CIFailedCheck {
		calls++
		return nil
	}
	failure = svc.ciFailure(context.Background(), uuid.New(), "acme/app", "abc123", "failure")
	require.Equal(t, domain.CIFailureTest, failure.Class)
	require.Equal(t, "default_test_failure", failure.Rule)
	require.NotEmpty(t, failure.Evidence)
	require.Equal(t, 2, calls)

	failure = svc.ciFailure(context.Background(), uuid.New(), "acme/app", "abc123", "cancelled")
	require.Equal(t, domain.CIFailureCancel, failure.Class)
	require.Equal(t, "conclusion_cancelled", failure.Rule)
	require.Equal(t, 2, calls, "a cancellation is GitHub's verdict; no check read is needed")
}

// The checks handed to the classifier keep what GitHub printed and the failed
// step of the job, so a runner step and a build's output can both decide.
func TestFailedChecksCarryTheirOutputAndFailedJobStep(t *testing.T) {
	runs := []gh.CheckRun{
		{Name: "unit", Status: "completed", Conclusion: "success"},
		{Name: "Build UI", Status: "completed", Conclusion: "failure", DetailsURL: "https://github.com/acme/app/actions/runs/9/job/42",
			Output: gh.CheckRunOutput{}},
	}
	runs[1].Output.Title = "npm run build"
	runs[1].Output.Summary = "the build stopped"
	runs[1].Output.Text = "FATAL ERROR: Reached heap limit Allocation failed - JavaScript heap out of memory"
	runs = append(runs, gh.CheckRun{Name: "integration", Status: "completed", Conclusion: "failure", DetailsURL: "https://ci.example/build/7"})

	checks := ciFailedChecks(runs)
	require.Len(t, checks, 2, "a passing check is not evidence of a failure")
	require.Equal(t, "Build UI", checks[0].check.Name)
	require.Equal(t, "failure", checks[0].check.Conclusion)
	require.Contains(t, checks[0].check.Text, "the build stopped")
	require.Contains(t, checks[0].check.Text, "out of memory")
	require.Equal(t, "integration", checks[1].check.Name)
	require.Zero(t, actionsJobID(checks[1].run.DetailsURL), "a check outside Actions has no job to read")
}

func TestActionsJobURLsNameTheirJob(t *testing.T) {
	require.EqualValues(t, 42, actionsJobID("https://github.com/acme/app/actions/runs/9/job/42"))
	require.EqualValues(t, 7, actionsJobID("https://ghe.example/acme/app/actions/runs/9/job/7?pr=12"))
	require.Zero(t, actionsJobID("https://ci.example/build/7"))
	require.Zero(t, actionsJobID(""))
}

func TestTheFirstFailedStepNamesAJobFailure(t *testing.T) {
	steps := []gh.WorkflowStep{
		{Name: "Set up job", Status: "completed", Conclusion: "success"},
		{Name: "Install packages", Status: "completed", Conclusion: "failure"},
		{Name: "Run tests", Status: "completed", Conclusion: "skipped"},
	}
	require.Equal(t, "Install packages", failedWorkflowStep(steps))
	require.Empty(t, failedWorkflowStep(nil))
	require.Empty(t, failedWorkflowStep([]gh.WorkflowStep{{Name: "Run tests", Conclusion: "success"}}))
}

func TestClassificationReadsAtMostAFixedNumberOfFailedChecks(t *testing.T) {
	runs := make([]gh.CheckRun, 0, maxCIClassifiedChecks+5)
	for range maxCIClassifiedChecks + 5 {
		runs = append(runs, gh.CheckRun{Name: "job", Status: "completed", Conclusion: "failure", Output: gh.CheckRunOutput{Text: "--- FAIL: TestX"}})
	}
	checks := ciFailedChecks(runs)
	require.Len(t, checks, maxCIClassifiedChecks)
	require.True(t, strings.HasPrefix(checks[0].check.Name, "job"))
}
