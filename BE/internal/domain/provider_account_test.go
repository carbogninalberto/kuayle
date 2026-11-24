package domain

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Production (KUA-211, 2026-11-17): OpenCode stopped six times in a row on
// its provider's error event. The harness error is what the run recorded; the
// stderr line is OpenCode's own log of the same failure.
const (
	openCodeCreditStdout = `{"type":"step_start","part":{"type":"step-start"}}
{"type":"error","error":{"name":"APIError","data":{"message":"Insufficient balance. Top up at https://opencode.ai/console/wrk_01JTEST0WORKSPACE/billing?credits=1"}}}`
	openCodeCreditHarnessError = "The harness reported an error: APIError: Insufficient balance. Top up at https://opencode.ai/console/wrk_01JTEST0WORKSPACE/billing?credits=1"
	openCodeCreditStderr       = `timestamp=2026-11-17T13:30:15.525Z level=ERROR run=ses_01 message=failed error="APIError: Insufficient balance. Top up at https://opencode.ai/console/wrk_01JTEST0WORKSPACE/billing?credits=1"`

	// Production (KUA-211): agent-2's model did not exist. OpenCode's error
	// event only said UnknownError; its log named the model.
	openCodeModelHarnessError = "The harness reported an error: UnknownError: Unexpected server error. Check server logs for details"
	openCodeModelStderr       = `timestamp=2026-11-17T15:02:44.118Z level=WARN message="slow request"
timestamp=2026-11-17T15:02:44.120Z level=ERROR run=ses_02 message=failed ref=err_01JTEST error="ProviderModelNotFoundError: Model not found: opencode-go/kimi-k9"`
)

func TestDetectProviderCreditFromTheProductionOpenCodeError(t *testing.T) {
	message, ok := DetectProviderCredit(openCodeCreditHarnessError, openCodeCreditStdout)
	require.True(t, ok)
	require.Equal(t, "APIError: Insufficient balance.", message, "the billing URL and its workspace ID are dropped")

	// The structured event alone is enough (a harness error cut elsewhere).
	message, ok = DetectProviderCredit("", openCodeCreditStdout)
	require.True(t, ok)
	require.Equal(t, "APIError: Insufficient balance.", message)
}

func TestDetectProviderCreditFromTheOtherHarnesses(t *testing.T) {
	for name, tc := range map[string]struct {
		harnessError, stdout, message string
	}{
		"Claude Code with an Anthropic API key": {
			stdout:  `{"type":"result","subtype":"success","is_error":true,"result":"Credit balance is too low"}`,
			message: "Credit balance is too low",
		},
		"the Anthropic API's own message": {
			harnessError: `API Error: 400 {"type":"error","error":{"type":"invalid_request_error","message":"Your credit balance is too low to access the Anthropic API. Please go to Plans & Billing to upgrade or purchase credits."}}`,
			message:      "Your credit balance is too low to access the Anthropic API.",
		},
		"Codex with an API key": {
			stdout:  `{"type":"error","message":"Quota exceeded. Check your plan and billing details."}`,
			message: "Quota exceeded. Check your plan and billing details.",
		},
		"the OpenAI API's insufficient_quota": {
			stdout:  `{"type":"turn.failed","error":{"message":"You exceeded your current quota, please check your plan and billing details. For more information on this error, read the docs: https://platform.openai.com/docs/guides/error-codes/api-errors."}}`,
			message: "You exceeded your current quota, please check your plan and billing details.",
		},
		"only the OpenAI error code": {
			harnessError: `unexpected status 429 Too Many Requests: {"error":{"type":"insufficient_quota","code":"insufficient_quota"}}`,
			message:      "insufficient_quota",
		},
	} {
		t.Run(name, func(t *testing.T) {
			message, ok := DetectProviderCredit(tc.harnessError, tc.stdout)
			require.True(t, ok)
			require.Equal(t, tc.message, message)
			require.NotContains(t, message, "http")
		})
	}
}

// Only the harness's own error counts: the agent's work, a final message or
// a tool's output that mentions a balance is not the provider refusing.
func TestDetectProviderCreditIgnoresWhatTheAgentWorkedOn(t *testing.T) {
	for name, stdout := range map[string]string{
		"a successful result about a wallet":     `{"type":"result","is_error":false,"result":"Fixed the insufficient balance check in wallet.go"}`,
		"a tool's test output":                   `{"type":"tool_use","part":{"tool":"bash","state":{"status":"completed","output":"--- FAIL: TestInsufficientBalance\nexpected: insufficient balance"}}}`,
		"an assistant message quoting the error": `{"type":"assistant","message":{"content":[{"type":"text","text":"The API answers 'Credit balance is too low' when the account is empty"}]}}`,
		"a plain line":                           "insufficient balance",
		"an ordinary failure":                    `{"type":"result","is_error":true,"result":"2 tests failed"}`,
		"a usage limit":                          `{"type":"error","message":"You've hit your usage limit. Try again in 2 hours."}`,
		"another provider error":                 `{"type":"error","error":{"name":"APIError","data":{"message":"429 Too Many Requests"}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, ok := DetectProviderCredit("", stdout)
			require.False(t, ok)
		})
	}
	_, ok := DetectProviderCredit("The harness reported an error: error_max_turns", "")
	require.False(t, ok)
}

func TestProviderCreditErrorMessageRoundTrips(t *testing.T) {
	text := ProviderCreditErrorMessage("APIError: Insufficient balance.")
	require.Equal(t, "provider_credit_exhausted: APIError: Insufficient balance.", text)
	message, ok := ParseProviderCreditError(text)
	require.True(t, ok)
	require.Equal(t, "APIError: Insufficient balance.", message)
	_, ok = ParseProviderCreditError("usage_limit_reached: You've hit your limit")
	require.False(t, ok)
	_, ok = ParseProviderCreditError(openCodeCreditHarnessError)
	require.False(t, ok, "only the manager's own code is a credit pause")
}

func TestDetectModelNotFoundFromTheProductionOpenCodeLog(t *testing.T) {
	model, ok := DetectModelNotFound(openCodeModelHarnessError, "", openCodeModelStderr)
	require.True(t, ok)
	require.Equal(t, "opencode-go/kimi-k9", model)
}

func TestDetectModelNotFoundFromTheOtherHarnesses(t *testing.T) {
	for name, tc := range map[string]struct {
		harnessError, stdout, model string
	}{
		"OpenCode's error event": {
			stdout: `{"type":"error","error":{"name":"ProviderModelNotFoundError","data":{"providerID":"opencode-go","modelID":"kimi-k9","suggestions":[]}}}`,
			model:  "opencode-go/kimi-k9",
		},
		"OpenCode's error with its message": {
			harnessError: "The harness reported an error: ProviderModelNotFoundError: Model not found: opencode-go/kimi-k9.",
			model:        "opencode-go/kimi-k9",
		},
		"OpenCode 2's error event": {
			stdout: `{"type":"error","timestamp":1795529998608,"sessionID":"ses_df21281bffferMg74LvzPhProP","error":{"type":"provider.no-route","message":"Model unavailable: opencode/no-such-model-kuayle"}}`,
			model:  "opencode/no-such-model-kuayle",
		},
		"the Anthropic API": {
			stdout: `{"type":"result","is_error":true,"result":"API Error: 404 {\"type\":\"error\",\"error\":{\"type\":\"not_found_error\",\"message\":\"model: claude-sonnet-9\"}}"}`,
			model:  "claude-sonnet-9",
		},
		"Claude Code's own wording": {
			stdout: `{"type":"result","is_error":true,"result":"There's an issue with the selected model (claude-sonnet-9). It may not exist or you may not have access to it. Run --model to pick a different model."}`,
			model:  "claude-sonnet-9",
		},
		"the OpenAI API": {
			stdout: "{\"type\":\"error\",\"message\":\"unexpected status 404 Not Found: {\\\"error\\\":{\\\"message\\\":\\\"The model `gpt-9-codex` does not exist or you do not have access to it.\\\",\\\"type\\\":\\\"invalid_request_error\\\",\\\"code\\\":\\\"model_not_found\\\"}}\"}",
			model:  "gpt-9-codex",
		},
		"only the OpenAI error code": {
			harnessError: `{"error":{"code":"model_not_found"}}`,
			model:        "",
		},
	} {
		t.Run(name, func(t *testing.T) {
			model, ok := DetectModelNotFound(tc.harnessError, tc.stdout, "")
			require.True(t, ok)
			require.Equal(t, tc.model, model)
		})
	}
}

// Standard error counts only as OpenCode's own error log line: a test or a
// tool that prints the words is not the provider.
func TestDetectModelNotFoundIgnoresOrdinaryOutput(t *testing.T) {
	for name, tc := range map[string]struct{ harnessError, stdout, stderr string }{
		"a test that mentions the error":   {stderr: "--- FAIL: TestResolve\n    resolve_test.go:9: want ProviderModelNotFoundError: Model not found: x/y"},
		"a warning, not an error":          {stderr: `level=WARN message="retry" error="ProviderModelNotFoundError: Model not found: x/y"`},
		"a tool's output":                  {stdout: `{"type":"tool_use","part":{"state":{"output":"Model not found: gpt-9"}}}`},
		"a successful result":              {stdout: `{"type":"result","is_error":false,"result":"Handled model_not_found in the client"}`},
		"an unknown error without a model": {harnessError: openCodeModelHarnessError, stderr: "timestamp=2026-11-17T15:02:44.120Z level=ERROR run=ses_02 message=failed"},
		// OpenCode 2 refuses a variant the model lacks: the model exists.
		"a variant the model lacks": {stdout: `{"type":"error","timestamp":1795530065201,"sessionID":"ses_df2117c42ffeDkt9IWFB5nCr2p","error":{"type":"provider.no-route","message":"Variant unavailable for opencode/deepseek-v4.1-flash-free: ultra"}}`},
	} {
		t.Run(name, func(t *testing.T) {
			_, ok := DetectModelNotFound(tc.harnessError, tc.stdout, tc.stderr)
			require.False(t, ok)
		})
	}
}

func TestModelNotFoundErrorMessageRoundTrips(t *testing.T) {
	text := ModelNotFoundErrorMessage("opencode-go/kimi-k9")
	require.Equal(t, "model_not_found: opencode-go/kimi-k9", text)
	model, ok := ParseModelNotFoundError(text)
	require.True(t, ok)
	require.Equal(t, "opencode-go/kimi-k9", model)
	model, ok = ParseModelNotFoundError(ModelNotFoundErrorMessage(""))
	require.True(t, ok)
	require.Empty(t, model)
	_, ok = ParseModelNotFoundError("The harness reported an error: model_not_found")
	require.False(t, ok)
}

// The run's failure is classified as the provider's, with its own rules, so
// the task worker can tell both apart from an ordinary provider error.
func TestProviderAccountFailuresAreClassified(t *testing.T) {
	credit := ClassifyRunFailure(RunFailureEvidence{Status: "failed", ExitCode: exitCode(1), ProviderCredit: "APIError: Insufficient balance.",
		HarnessError: openCodeCreditHarnessError, Stderr: openCodeCreditStderr})
	require.Equal(t, FailureProvider, credit.Class)
	require.Equal(t, FailureRuleProviderCredit, credit.Rule)
	require.Contains(t, credit.Cause, "The provider account has no credit left: APIError: Insufficient balance")

	model := ClassifyRunFailure(RunFailureEvidence{Status: "failed", ExitCode: exitCode(1), ModelNotFound: true, MissingModel: "opencode-go/kimi-k9",
		HarnessError: openCodeModelHarnessError, Stderr: openCodeModelStderr})
	require.Equal(t, FailureProvider, model.Class)
	require.Equal(t, FailureRuleModelNotFound, model.Rule)
	require.Contains(t, model.Cause, `The provider has no model "opencode-go/kimi-k9"`)

	unnamed := ClassifyRunFailure(RunFailureEvidence{Status: "failed", ExitCode: exitCode(1), ModelNotFound: true})
	require.Equal(t, FailureRuleModelNotFound, unnamed.Rule)
	require.Contains(t, unnamed.Cause, "The provider does not offer the model the run asked for")

	// A usage limit still decides first; the platform's own limits too.
	limited := ClassifyRunFailure(RunFailureEvidence{Status: "failed", ExitCode: exitCode(1), UsageLimit: "You've hit your usage limit", ProviderCredit: "Credit balance is too low"})
	require.Equal(t, "usage_limit", limited.Rule)
	oom := ClassifyRunFailure(RunFailureEvidence{Status: "failed", ExitCode: exitCode(137), OOMKilled: true, ProviderCredit: "Credit balance is too low"})
	require.Equal(t, "out_of_memory", oom.Rule)
}
