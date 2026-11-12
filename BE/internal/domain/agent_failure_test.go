package domain

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func exitCode(code int) *int { return &code }

// The classification table, rule by rule. The first six cases are the runs
// measured in production that stored an exit code and nothing else.
func TestClassifyRunFailure(t *testing.T) {
	for name, tc := range map[string]struct {
		evidence RunFailureEvidence
		class    FailureClass
		rule     string
		cause    []string
	}{
		"exit 139 under the PID limit": {
			RunFailureEvidence{Status: "failed", ExitCode: exitCode(139), PidsLimit: 1024, PidsPeak: 1024},
			FailurePlatform, "pid_limit", []string{"limit of 1024 processes and threads", "exit code 139, SIGSEGV"},
		},
		"the runtime says it could not start a thread": {
			RunFailureEvidence{Status: "failed", ExitCode: exitCode(2), Stderr: "runtime: failed to create new OS thread (have 12 already; errno=11)\nfatal error: newosproc"},
			FailurePlatform, "pid_limit", []string{"PID limit", "exit code 2", "failed to create new OS thread"},
		},
		"exit 137 out of memory": {
			RunFailureEvidence{Status: "failed", ExitCode: exitCode(137), OOMKilled: true, MemoryLimit: MemoryLimitMessage("agent", 1503238553, true)},
			FailurePlatform, "out_of_memory", []string{"Out of memory: the agent container reached its 1.4 GiB memory limit", "exit code 137, SIGKILL"},
		},
		"exit 137 without an out-of-memory event": {
			RunFailureEvidence{Status: "failed", ExitCode: exitCode(137)},
			FailureUnknown, "killed", []string{"SIGKILL", "nothing recorded says what sent it"},
		},
		"exit 139 below the PID limit": {
			RunFailureEvidence{Status: "failed", ExitCode: exitCode(139), PidsLimit: 1024, PidsPeak: 80},
			FailureUnknown, "killed", []string{"SIGSEGV"},
		},
		"exit 1 with no message": {
			RunFailureEvidence{Status: "failed", ExitCode: exitCode(1)},
			FailureUnknown, "harness_exit_silent", []string{"without reporting an error", "exit code 1"},
		},
		"exit 1 with standard error": {
			RunFailureEvidence{Status: "failed", ExitCode: exitCode(1), Stderr: "\nError: config file is not valid JSON\n"},
			FailureAgent, "harness_exit_with_output", []string{"exit code 1", "Error: config file is not valid JSON"},
		},
		"usage limit": {
			RunFailureEvidence{Status: "failed", ExitCode: exitCode(1), UsageLimit: "You've hit your usage limit · resets 4pm (UTC)", HarnessError: "You've hit your usage limit"},
			FailureProvider, "usage_limit", []string{"usage limit was reached", "resets 4pm"},
		},
		"overloaded": {
			RunFailureEvidence{Status: "failed", ExitCode: exitCode(1), HarnessError: `API Error: 529 {"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`},
			FailureProvider, "provider_error", []string{"The provider reported an error", "Overloaded"},
		},
		"no final message after a provider's 429": {
			RunFailureEvidence{Status: "failed", ExitCode: exitCode(0), NoFinalMessage: true, HarnessError: "The harness reported an error: APIError: 429 Too Many Requests"},
			FailureProvider, "provider_error", []string{"429"},
		},
		"no final message": {
			RunFailureEvidence{Status: "failed", ExitCode: exitCode(0), NoFinalMessage: true},
			FailureAgent, "no_final_message", []string{"The agent ended without a final message", "exit code 0"},
		},
		"the harness reported an error": {
			RunFailureEvidence{Status: "failed", ExitCode: exitCode(1), HarnessError: "error_max_turns"},
			FailureAgent, "harness_error", []string{"The harness reported an error: error_max_turns"},
		},
		"sign-in required": {
			RunFailureEvidence{Status: "failed", ExitCode: exitCode(1), HarnessError: "sign_in_required: Claude Code needs you to sign in"},
			FailureProvider, "sign_in_required", []string{"needs a sign-in"},
		},
		"time limit": {
			RunFailureEvidence{Status: "timeout", ExitCode: exitCode(-1), TimeLimitSeconds: 10800, HarnessError: "still working"},
			FailurePlatform, "time_limit", []string{"time limit of 180 minutes"},
		},
		"cleanup timeout": {
			RunFailureEvidence{Status: "failed", ExitCode: exitCode(-1), ExecutionError: "agent_timeout_cleanup_failed: context deadline exceeded"},
			FailurePlatform, "platform_error", []string{"could not clean up the run after its time limit", "context deadline exceeded"},
		},
		"persistence failure": {
			RunFailureEvidence{Status: "failed", ExitCode: exitCode(0), ExecutionError: "agent_output_persistence_failed: context deadline exceeded", HarnessError: "interrupted"},
			FailurePlatform, "platform_error", []string{"could not store the agent's output"},
		},
		"a person's cancel": {
			RunFailureEvidence{Status: "cancelled", ExitCode: exitCode(-1), CancelledBy: CancelledByPerson},
			FailurePerson, "cancelled_by_person", []string{"A person stopped the run"},
		},
		"a person's cancel wins over the kill it caused": {
			RunFailureEvidence{Status: "cancelled", ExitCode: exitCode(137), CancelledBy: CancelledByPerson, Stderr: "Killed"},
			FailurePerson, "cancelled_by_person", []string{"A person stopped the run"},
		},
		"a policy refusal": {
			RunFailureEvidence{Status: "cancelled", ExitCode: exitCode(-1), CancelledBy: CancelledByPolicy},
			FailurePolicy, "cancelled_by_policy", []string{"policy no longer allows it"},
		},
		"stopped with its machine": {
			RunFailureEvidence{Status: "cancelled", ExitCode: exitCode(-1)},
			FailurePlatform, "cancelled_by_platform", []string{"Kuayle stopped the run"},
		},
		"no evidence at all": {
			RunFailureEvidence{Status: "failed"},
			FailureUnknown, "no_evidence", []string{"nothing recorded says why"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			failure := ClassifyRunFailure(tc.evidence)
			require.Equal(t, tc.class, failure.Class)
			require.Equal(t, tc.rule, failure.Rule)
			for _, part := range tc.cause {
				require.Contains(t, failure.Cause, part)
			}
			require.NotEmpty(t, failure.Cause, "no failed run is stored without a cause")
			require.LessOrEqual(t, len(failure.Cause), FailureCauseMaxBytes)
			require.True(t, ValidFailureClass(string(failure.Class)))
		})
	}
}

// A run stopped by the platform is never counted as the agent's, whatever
// the harness printed while it was being stopped.
func TestPlatformStopsAreNeverTheAgents(t *testing.T) {
	harness := "Error: write EPIPE"
	for _, evidence := range []RunFailureEvidence{
		{Status: "failed", ExitCode: exitCode(137), OOMKilled: true, HarnessError: harness, Stderr: harness},
		{Status: "failed", ExitCode: exitCode(139), PidsLimit: 1024, PidsPeak: 1000, HarnessError: harness},
		{Status: "timeout", ExitCode: exitCode(-1), HarnessError: harness},
		{Status: "failed", ExitCode: exitCode(1), ExecutionError: "agent_output_persistence_failed: pq: timeout", HarnessError: harness},
	} {
		require.Equal(t, FailurePlatform, ClassifyRunFailure(evidence).Class)
	}
}

// The cause quotes only the end of standard error, bounded.
func TestRunFailureCauseIsBounded(t *testing.T) {
	stderr := strings.Repeat("noise line that says nothing useful about the failure\n", 400) + "Error: the last line is what matters\n"
	failure := ClassifyRunFailure(RunFailureEvidence{Status: "failed", ExitCode: exitCode(1), Stderr: stderr})
	require.LessOrEqual(t, len(failure.Cause), FailureCauseMaxBytes)
	require.Contains(t, failure.Cause, "Error: the last line is what matters")
	require.LessOrEqual(t, strings.Count(failure.Cause, "noise line"), failureStderrLines)

	require.Equal(t, "b\nc", TailLines("a\n\nb\n  \nc\n\n", 2, 100))
	require.Equal(t, "…aéé", TailLines("aaaéé", 5, 5))
	require.Equal(t, "ab…", TruncateBytes("abcdefgh", 5))
}

func TestWorkflowOutcomeFailureClass(t *testing.T) {
	require.Equal(t, FailureAgent, WorkflowOutcomeFailureClass(WorkflowChecksFailed))
	require.Equal(t, FailurePlatform, WorkflowOutcomeFailureClass(WorkflowPublishFailed))
	require.Equal(t, FailurePolicy, WorkflowOutcomeFailureClass(WorkflowTimeout))
	require.Equal(t, FailurePerson, WorkflowOutcomeFailureClass(WorkflowApprovalDenied))
	require.Equal(t, FailureUnknown, WorkflowOutcomeFailureClass("something_else"))
}
