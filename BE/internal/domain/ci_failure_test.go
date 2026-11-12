package domain

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The CI rules, one case each. The runner cases are the two days of
// production failures: an Ubuntu mirror that did not resolve and a build
// that ran out of memory.
func TestClassifyCIFailure(t *testing.T) {
	check := func(name, conclusion, step, text string) []CIFailedCheck {
		return []CIFailedCheck{{Name: name, Conclusion: conclusion, FailedStep: step, Text: text}}
	}
	for name, tc := range map[string]struct {
		evidence CIFailureEvidence
		class    string
		rule     string
	}{
		"nothing but a failure": {CIFailureEvidence{Conclusion: "failure"}, CIFailureTest, "default_test_failure"},
		"a failing test": {
			CIFailureEvidence{Conclusion: "failure", Checks: check("Backend tests", "failure", "Run go test", "--- FAIL: TestRetry (0.02s)\nProcess completed with exit code 1.")},
			CIFailureTest, "default_test_failure",
		},
		"cancelled":       {CIFailureEvidence{Conclusion: "cancelled"}, CIFailureCancel, "conclusion_cancelled"},
		"suite timed out": {CIFailureEvidence{Conclusion: "timed_out"}, CIFailureTimeout, "conclusion_timed_out"},
		"job exceeded its time": {
			CIFailureEvidence{Conclusion: "failure", Checks: check("e2e", "failure", "Run tests", "The job running on runner GitHub Actions 7 has exceeded the maximum execution time of 30 minutes.")},
			CIFailureTimeout, "conclusion_timed_out",
		},
		"startup failure": {CIFailureEvidence{Conclusion: "startup_failure"}, CIFailureRunner, "conclusion_startup_failure"},
		"package mirror unreachable": {
			CIFailureEvidence{Conclusion: "failure", Checks: check("Docker Runtime Integration", "failure", "Install packages", "E: Failed to fetch http://azure.archive.ubuntu.com/ubuntu/dists/noble/InRelease  Temporary failure resolving 'azure.archive.ubuntu.com'")},
			CIFailureRunner, "runner_output",
		},
		"build out of memory": {
			CIFailureEvidence{Conclusion: "failure", Checks: check("Build UI", "failure", "npm run build", "FATAL ERROR: Reached heap limit Allocation failed - JavaScript heap out of memory")},
			CIFailureRunner, "runner_output",
		},
		"runner shut down": {
			CIFailureEvidence{Conclusion: "failure", Checks: check("unit", "failure", "", "The runner has received a shutdown signal. This can happen when the runner service is stopped.")},
			CIFailureRunner, "runner_output",
		},
		"the runner's own step": {
			CIFailureEvidence{Conclusion: "failure", Checks: check("unit", "failure", "Initialize containers", "")},
			CIFailureRunner, "runner_step",
		},
		"compilation error": {
			CIFailureEvidence{Conclusion: "failure", Checks: check("Backend tests", "failure", "Run go test", "./service.go:12:2: undefined: missingSymbol\nFAIL build failed")},
			CIFailureBuild, "build_output",
		},
		"a build step": {
			CIFailureEvidence{Conclusion: "failure", Checks: check("CI", "failure", "Build image", "Process completed with exit code 1.")},
			CIFailureBuild, "build_step",
		},
		"a build check without its step": {
			CIFailureEvidence{Conclusion: "failure", Checks: check("Build", "failure", "", "")},
			CIFailureBuild, "build_step",
		},
		"a check named for build and test is a test failure": {
			CIFailureEvidence{Conclusion: "failure", Checks: check("Build and test", "failure", "", "")},
			CIFailureTest, "default_test_failure",
		},
		"a runner failure beside a failing test is the test's": {
			CIFailureEvidence{Conclusion: "failure", Checks: []CIFailedCheck{
				{Name: "integration", Conclusion: "failure", FailedStep: "Set up job"},
				{Name: "unit", Conclusion: "failure", FailedStep: "Run tests", Text: "--- FAIL: TestX"},
			}},
			CIFailureTest, "default_test_failure",
		},
		"two runner failures": {
			CIFailureEvidence{Conclusion: "failure", Checks: []CIFailedCheck{
				{Name: "integration", Conclusion: "failure", FailedStep: "Set up job"},
				{Name: "unit", Conclusion: "failure", Text: "Could not resolve host: github.com"},
			}},
			CIFailureRunner, "runner_step",
		},
	} {
		t.Run(name, func(t *testing.T) {
			got := ClassifyCIFailure(tc.evidence)
			require.Equal(t, tc.class, got.Class)
			require.Equal(t, tc.rule, got.Rule)
			require.NotEmpty(t, got.Evidence)
			require.Contains(t, CIFailureClasses, got.Class)
		})
	}
}

func TestOnlyTheChangesFailuresStartAFollowUp(t *testing.T) {
	require.True(t, CIFailureStartsFollowUp(CIFailureTest))
	require.True(t, CIFailureStartsFollowUp(CIFailureBuild))
	require.True(t, CIFailureStartsFollowUp(CIFailureTimeout))
	require.False(t, CIFailureStartsFollowUp(CIFailureRunner))
	require.False(t, CIFailureStartsFollowUp(CIFailureCancel))
}
