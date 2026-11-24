package machine

import (
	"encoding/json"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

const verdictCommit = "0123456789abcdef0123456789abcdef01234567"

// A pipeline member's turn that was asked for a verdict and ends without
// one is asked once more, in its own session, to post it in the room; when
// it does, nothing more is asked.
func TestATurnThatOwesAVerdictIsAskedOnceForIt(t *testing.T) {
	completed, store, runtime := notebookRun(t, notebookScenario{provider: "claude-code", session: "0c9a6c1e-session", continuation: resumeCommand,
		stdout:         `{"type":"result","subtype":"success","is_error":false,"result":"I looked at the change."}`,
		missingVerdict: verdictCommit, afterAsk: func(store *notebookStoreFake) { store.missingVerdict = "" }})
	require.Equal(t, domain.DevMachineAgentRunStatusSucceeded, completed.Status)
	require.Len(t, runtime.runs, 2, "the turn and one continuation")
	require.Equal(t, 1, store.asked)
	var argv []string
	require.NoError(t, json.Unmarshal(runtime.runs[1].CommandArgv, &argv))
	require.Equal(t, []string{"harness", "resume", "0c9a6c1e-session", "-p"}, argv[:4], "its own session")
	require.Contains(t, argv[4], "gave no verdict")
	require.Contains(t, argv[4], "kuayle-say --to @all --verdict approve|request_changes|comment --commit "+verdictCommit)
	require.Contains(t, argv[4], "Do nothing else")
	require.Equal(t, "I looked at the change.", *completed.Summary, "the continuation's reply is not the turn's summary")
}

// A turn whose final message carries the verdict (the Verdict: line) is not
// asked: Kuayle records that one when the turn ends.
func TestATurnWhoseFinalMessageHasTheVerdictIsNotAsked(t *testing.T) {
	completed, store, runtime := notebookRun(t, notebookScenario{provider: "claude-code", session: "0c9a6c1e-session", continuation: resumeCommand,
		stdout: `{"type":"result","subtype":"success","is_error":false,"result":"Verdict: approve\n\nNo findings."}`, missingVerdict: verdictCommit})
	require.Equal(t, domain.DevMachineAgentRunStatusSucceeded, completed.Status)
	require.Len(t, runtime.runs, 1)
	require.Zero(t, store.asked)
}
