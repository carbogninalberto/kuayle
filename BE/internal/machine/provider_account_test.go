package machine

import (
	"testing"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// Production (KUA-218): an OpenCode run stopped on "APIError: Insufficient
// balance" with a billing link naming the workspace. The run records the
// account's state in the form the task worker pauses on, without the link.
func TestOutOfCreditAgentRunRecordsTheAccountState(t *testing.T) {
	stdout := `{"type":"step_start","part":{"type":"step-start"}}
{"type":"error","error":{"name":"APIError","data":{"message":"Insufficient balance. Top up at https://opencode.ai/console/wrk_01JTEST0WORKSPACE/billing?credits=1"}}}`
	stderr := `timestamp=2026-11-17T13:30:15.525Z level=ERROR run=ses_01 message=failed error="APIError: Insufficient balance. Top up at https://opencode.ai/console/wrk_01JTEST0WORKSPACE/billing?credits=1"`
	store := runOutOfMemoryAgent(t, &AgentExecution{ContainerID: "agent", ExitCode: 1, Stdout: stdout, Stderr: stderr}, true)
	run := store.completedRun
	require.Equal(t, domain.DevMachineAgentRunStatusFailed, run.Status)
	require.NotNil(t, run.ErrorMessage)
	message, ok := domain.ParseProviderCreditError(*run.ErrorMessage)
	require.True(t, ok, *run.ErrorMessage)
	require.Equal(t, "APIError: Insufficient balance.", message)
	require.NotContains(t, *run.ErrorMessage, "wrk_01JTEST0WORKSPACE")
	require.Equal(t, string(domain.FailureProvider), *run.FailureClass)
	require.Equal(t, domain.FailureRuleProviderCredit, *run.FailureRule)
	require.Contains(t, *run.FailureCause, "The provider account has no credit left")
}

// Production (KUA-218): OpenCode's error event said only "UnknownError:
// Unexpected server error"; its log named ProviderModelNotFoundError and the
// model. The run records the model in the form the task worker fails on.
func TestModelNotFoundAgentRunNamesTheModel(t *testing.T) {
	stdout := `{"type":"error","error":{"name":"UnknownError","data":{"message":"Unexpected server error. Check server logs for details"}}}`
	stderr := `timestamp=2026-11-17T15:02:44.120Z level=ERROR run=ses_02 message=failed ref=err_01JTEST error="ProviderModelNotFoundError: Model not found: opencode-go/kimi-k9"`
	store := runOutOfMemoryAgent(t, &AgentExecution{ContainerID: "agent", ExitCode: 1, Stdout: stdout, Stderr: stderr}, true)
	run := store.completedRun
	require.Equal(t, domain.DevMachineAgentRunStatusFailed, run.Status)
	require.NotNil(t, run.ErrorMessage)
	model, ok := domain.ParseModelNotFoundError(*run.ErrorMessage)
	require.True(t, ok, *run.ErrorMessage)
	require.Equal(t, "opencode-go/kimi-k9", model)
	require.Equal(t, string(domain.FailureProvider), *run.FailureClass)
	require.Equal(t, domain.FailureRuleModelNotFound, *run.FailureRule)
	require.Contains(t, *run.FailureCause, `The provider has no model "opencode-go/kimi-k9"`)
}

// An ordinary harness error is neither: it keeps the harness's message.
func TestOrdinaryHarnessErrorIsNotAProviderAccountFailure(t *testing.T) {
	stdout := `{"type":"error","error":{"name":"APIError","data":{"message":"429 Too Many Requests"}}}`
	store := runOutOfMemoryAgent(t, &AgentExecution{ContainerID: "agent", ExitCode: 1, Stdout: stdout}, true)
	run := store.completedRun
	require.NotNil(t, run.ErrorMessage)
	_, credit := domain.ParseProviderCreditError(*run.ErrorMessage)
	_, model := domain.ParseModelNotFoundError(*run.ErrorMessage)
	require.False(t, credit)
	require.False(t, model)
	require.Equal(t, "provider_error", *run.FailureRule)
}
