package machine

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// fakeCodexSignIn is shaped like the Codex sign-in file (auth.json) a run
// receives as KUAYLE_CODEX_AUTH, pretty-printed as Codex writes it. Every
// value is fake.
const fakeCodexSignIn = `{
  "OPENAI_API_KEY": null,
  "tokens": {
    "id_token": "fake-id-token-0001",
    "access_token": "fake-access-token-0001",
    "refresh_token": "fake-refresh-token-0001",
    "account_id": "fake-account-id-0001"
  },
  "last_refresh": "2026-11-18T09:00:00.000000Z"
}
`

// codexReviewStream is a `codex exec --json` review run as Codex prints it.
// AUTH_FILE is replaced by the sign-in file, JSON-escaped, as if the agent had
// printed it.
const codexReviewStream = `{"type":"thread.started","thread_id":"019a8f2e-1c4b-7d10-9e55-3f2a1b0c4d5e"}
{"type":"turn.started"}
{"type":"item.completed","item":{"id":"item_0","type":"reasoning","text":"**Reviewing the change**\n\nI'll check out the commit and read the diff."}}
{"type":"item.started","item":{"id":"item_1","type":"command_execution","command":"/bin/bash -lc 'git diff origin/main...HEAD'","aggregated_output":"","exit_code":null,"status":"in_progress"}}
{"type":"item.completed","item":{"id":"item_1","type":"command_execution","command":"/bin/bash -lc 'git diff origin/main...HEAD'","aggregated_output":"diff --git a/app.go b/app.go\n--- a/app.go\n+++ b/app.go\n@@ -9,6 +9,9 @@ func run() error {\n \tcfg, err := load()\n+\tif err != nil {\n+\t\treturn err\n+\t}\n }\n","exit_code":0,"status":"completed"}}
{"type":"item.completed","item":{"id":"item_2","type":"command_execution","command":"/bin/bash -lc 'cat ~/.codex/auth.json'","aggregated_output":"AUTH_FILE","exit_code":0,"status":"completed"}}
{"type":"item.completed","item":{"id":"item_3","type":"agent_message","text":"Verdict: request_changes\n\n- ` + "`app.go:12`" + ` major: wrap the error with the file name.\n- ` + "`.codex/auth.json`" + ` blocker: the sign-in holds fake-access-token-0001; never print it."}}
{"type":"turn.completed","usage":{"input_tokens":24763,"cached_input_tokens":24448,"output_tokens":122}}
`

// Production (KUA-227): both Codex reviews of a pull request ended "Review
// finished: unknown". The run's Codex sign-in is a multiline JSON secret, and
// its lines "{" and "}" were redaction keys, so every brace of the stream was
// redacted before the final message was parsed. The stream runs through the
// real redaction and the Codex parser here, with the secrets a sign-in run
// gets (agentSignInSecrets).
func TestCodexReviewKeepsItsVerdictWhenTheSignInIsRedacted(t *testing.T) {
	secrets := map[string]string{"KUAYLE_CODEX_AUTH": fakeCodexSignIn}
	for index, token := range signInTokens(fakeCodexSignIn) {
		secrets[fmt.Sprintf("KUAYLE_SIGN_IN_SECRET_%d", index)] = token
	}
	escaped, err := json.Marshal(fakeCodexSignIn)
	require.NoError(t, err)
	stream := strings.Replace(codexReviewStream, "AUTH_FILE", string(escaped[1:len(escaped)-1]), 1)
	output, records := collectAgentOutput(secrets)
	for len(stream) > 0 {
		size := min(7, len(stream))
		require.NoError(t, output.Write("stdout", []byte(stream[:size])))
		stream = stream[size:]
	}
	require.NoError(t, output.Finish())

	require.Len(t, *records, 8)
	tokens := []string{"fake-id-token-0001", "fake-access-token-0001", "fake-refresh-token-0001", "fake-account-id-0001"}
	for _, record := range *records {
		require.True(t, json.Valid([]byte(record.content)), "redaction keeps every event JSON: %s", record.content)
		for _, token := range tokens {
			require.NotContains(t, record.content, token)
		}
	}
	require.Contains(t, (*records)[4].content, `+\tif err != nil {\n+\t\treturn err\n+\t}\n }`, "braces in the output are not redacted")
	require.Contains(t, (*records)[5].content, `"aggregated_output":"[REDACTED]"`)

	result := agent.NewCodexProvider("").ParseResult(output.ResultText("stdout"), output.ResultText("stderr"), 0)
	require.Equal(t, "succeeded", result.Status)
	require.True(t, result.FinalMessage)
	verdict, findings := domain.ParseAgentReview(result.Summary)
	require.Equal(t, "request_changes", verdict)
	require.Equal(t, "- `app.go:12` major: wrap the error with the file name.\n- `.codex/auth.json` blocker: the sign-in holds [REDACTED]; never print it.", findings)
}
