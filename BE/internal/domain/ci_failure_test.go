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
		"the real runner failure shape": {
			CIFailureEvidence{Conclusion: "failure", Checks: []CIFailedCheck{{
				Name: "Docker Runtime Integration", Conclusion: "failure",
				FailedStep:  "Build, smoke test and scan native architecture image",
				Annotations: []string{`buildx failed with: ERROR: failed to build: failed to solve: process "/bin/sh -c apt-get update && apt-get install -y ..." did not complete successfully`},
				LogTail:     "#7 [build 2/3] RUN apt-get update\nCould not resolve 'archive.ubuntu.com'\nTemporary failure resolving 'azure.archive.ubuntu.com'",
			}}},
			CIFailureRunner, "runner_output",
		},
		"an annotation alone names the runner": {
			CIFailureEvidence{Conclusion: "failure", Checks: []CIFailedCheck{{
				Name: "Docker Runtime Integration", Conclusion: "failure", FailedStep: "Install packages",
				Annotations: []string{"E: Failed to fetch http://archive.ubuntu.com/... Temporary failure resolving 'archive.ubuntu.com'"},
			}}},
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

// Each strong signature is a real runner or network failure seen on GitHub
// Actions. The evidence may be an annotation, a job log tail or the check's
// own output; the failed step must not be a test run.
func TestEachRunnerSignatureDecidesTheSuite(t *testing.T) {
	for name, tc := range map[string]struct {
		step string
		text string
	}{
		"dns resolution":            {"Install packages", "Could not resolve 'archive.ubuntu.com'"},
		"temporary dns failure":     {"Build image", "Temporary failure resolving 'azure.archive.ubuntu.com'"},
		"package mirror 404":        {"Install packages", "E: Failed to fetch http://archive.ubuntu.com/ubuntu/pool/main/x/y.deb  404  Not Found"},
		"package mirror 503":        {"Install packages", "Failed to fetch http://mirror.example/deb: 503 Service Unavailable"},
		"registry rate limit":       {"Build image", "Error response from daemon: toomanyrequests: You have reached your pull rate limit"},
		"registry 429":              {"Build image", "docker: failed to pull image: 429 Too Many Requests"},
		"download connection reset": {"Build image", "go: downloading example.com/mod\nread: connection reset by peer"},
		"download timed out":        {"Build image", "npm ERR! network request failed: connection timed out while downloading dependencies"},
		"no space left":             {"Build image", "failed to write layer: no space left on device"},
		"runner shutdown":           {"", "The runner has received a shutdown signal. This can happen when the runner service is stopped."},
		"lost communication":        {"", "lost communication with the server"},
		"heap out of memory":        {"Build, smoke test and scan native architecture image", "FATAL ERROR: Reached heap limit Allocation failed - JavaScript heap out of memory"},
		"process killed for memory": {"Build image", "Killed process 4242 (node) total-vm:1kB, oom-kill"},
	} {
		t.Run(name, func(t *testing.T) {
			got := ClassifyCIFailure(CIFailureEvidence{Conclusion: "failure", Checks: []CIFailedCheck{{
				Name: "CI", Conclusion: "failure", FailedStep: tc.step, Text: tc.text,
			}}})
			require.Equal(t, CIFailureRunner, got.Class, got.Evidence)
			require.Equal(t, "runner_output", got.Rule)
			require.NotEmpty(t, got.Evidence)
		})
	}
}

// Ordinary test output can quote every word a network failure uses; it is
// never the runner's, and a test step is never the runner's.
func TestOrdinaryTestOutputNeverMakesTheSuiteTheRunners(t *testing.T) {
	for name, check := range map[string]CIFailedCheck{
		"a dial that timed out": {Name: "Backend tests", Conclusion: "failure", FailedStep: "Run go test",
			Text: "--- FAIL: TestDatabase\n    dial tcp 127.0.0.1:5432: i/o timeout"},
		"connection refused": {Name: "Backend tests", Conclusion: "failure", FailedStep: "Run go test",
			Text: "--- FAIL: TestServer\n    connect: connection refused\n    timeout after 5s"},
		"a status the test expected": {Name: "Backend tests", Conclusion: "failure", FailedStep: "Run go test",
			Text: "--- FAIL: TestBilling\n    expected 429 Too Many Requests, got 200\n    billing is wrong"},
		"a killed child": {Name: "Backend tests", Conclusion: "failure", FailedStep: "Run go test",
			Text: "--- FAIL: TestCrash\n    signal: killed\n    exit code 137"},
		"a test about disk space": {Name: "Backend tests", Conclusion: "failure", FailedStep: "Run go test",
			Text: "--- FAIL: TestDiskFull\n    got \"no space left on device\" from the fake"},
		"the runner's signature in a test step": {Name: "Integration", Conclusion: "failure", FailedStep: "Run tests",
			Text: "Could not resolve host: example.com"},
		"a network word without a download": {Name: "Integration", Conclusion: "failure", FailedStep: "Run service",
			Text: "connection refused after a timeout"},
	} {
		t.Run(name, func(t *testing.T) {
			got := ClassifyCIFailure(CIFailureEvidence{Conclusion: "failure", Checks: []CIFailedCheck{check}})
			require.Equal(t, CIFailureTest, got.Class, got.Evidence)
			require.Equal(t, "default_test_failure", got.Rule)
		})
	}
}

// A failure beside a passing or a failing test keeps the suite the change's.
func TestOneFailingTestBesideTheRunnerIsStillTheChanges(t *testing.T) {
	got := ClassifyCIFailure(CIFailureEvidence{Conclusion: "failure", Checks: []CIFailedCheck{
		{Name: "build", Conclusion: "failure", FailedStep: "Build image", Text: "no space left on device"},
		{Name: "unit", Conclusion: "failure", FailedStep: "Run tests", Text: "--- FAIL: TestX"},
	}})
	require.Equal(t, CIFailureTest, got.Class)
}

// When more checks failed than Kuayle read, the runner cannot be blamed.
func TestATruncatedFailedCheckListIsNotTheRunners(t *testing.T) {
	got := ClassifyCIFailure(CIFailureEvidence{Conclusion: "failure", MoreChecks: true, Checks: []CIFailedCheck{
		{Name: "build", Conclusion: "failure", FailedStep: "Build image", Text: "no space left on device"},
	}})
	require.NotEqual(t, CIFailureRunner, got.Class)
}
