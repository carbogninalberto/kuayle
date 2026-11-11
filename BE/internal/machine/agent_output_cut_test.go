package machine

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func openCodeToolLine(t *testing.T, command, output string) string {
	t.Helper()
	line, err := json.Marshal(map[string]any{
		"type": "tool_use", "timestamp": 1762855201000, "sessionID": "ses_1",
		"part": map[string]any{"id": "prt_2", "type": "tool", "callID": "call_1", "tool": "bash",
			"state": map[string]any{"status": "completed", "input": map[string]any{"command": command}, "output": output,
				"metadata": map[string]any{"exit": 0, "output": output}, "time": map[string]any{"start": 1762855200500, "end": 1762855201000}}},
	})
	require.NoError(t, err)
	return string(line) + "\n"
}

// Production (KUA-136): OpenCode writes a tool call's input and output on one
// line, so 22 of 128 calls were stored only as "[Oversized output line
// omitted]". The output is cut instead and the event stays an event.
func TestOversizedToolEventKeepsItsCallAndACutOutput(t *testing.T) {
	output, records := collectAgentOutput(nil)
	big := "FIRST-LINE\n" + strings.Repeat("0123456789abcdef\n", 100<<10/17) + "LAST-LINE"
	require.NoError(t, output.Write("stdout", []byte(openCodeToolLine(t, "go test ./...", big))))
	require.NoError(t, output.Finish())
	require.Len(t, *records, 1)
	stored := (*records)[0]
	require.False(t, stored.truncated, "the line is still an event the transcript can show")
	require.LessOrEqual(t, len(stored.content), maxAgentOutputLine)
	require.True(t, strings.HasSuffix(stored.content, "\n"))

	var event struct {
		Type      string
		Timestamp json.Number
		Part      struct {
			Tool   string
			CallID string
			State  struct {
				Status string
				Input  struct{ Command string }
				Output string
				Time   struct{ Start json.Number }
			}
		}
	}
	require.NoError(t, json.Unmarshal([]byte(stored.content), &event))
	require.Equal(t, "tool_use", event.Type)
	require.Equal(t, "1762855201000", event.Timestamp.String(), "numbers are not rewritten")
	require.Equal(t, "1762855200500", event.Part.State.Time.Start.String())
	require.Equal(t, "bash", event.Part.Tool)
	require.Equal(t, "call_1", event.Part.CallID)
	require.Equal(t, "completed", event.Part.State.Status)
	require.Equal(t, "go test ./...", event.Part.State.Input.Command)
	require.True(t, strings.HasPrefix(event.Part.State.Output, "FIRST-LINE\n"))
	require.True(t, strings.HasSuffix(event.Part.State.Output, "LAST-LINE"), "the end of a test run's output is kept too")
	require.Contains(t, event.Part.State.Output, "cut by Kuayle")
	require.Less(t, len(event.Part.State.Output), len(big))

	// The provider reads it as the same single event.
	events := agent.NewOpenCodeProvider("").ParseEvents([]byte(stored.content))
	require.Len(t, events, 1)
	require.Equal(t, "tool_use", events[0].Type)
}

// Claude Code and Codex lines are cut the same way; a split write and one
// through Docker's timestamped log stream store the same record.
func TestOversizedEventsOfOtherHarnessesAreCutAcrossFrames(t *testing.T) {
	big := strings.Repeat("line of build output\n", 4000)
	for name, line := range map[string]string{
		"claude-code": `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":` + mustJSON(t, big) + `}]}}` + "\n",
		"codex":       `{"type":"item.completed","item":{"id":"item_1","type":"command_execution","command":"npm test","aggregated_output":` + mustJSON(t, big) + `,"exit_code":0,"status":"completed"}}` + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			whole, a := collectAgentOutput(nil)
			require.NoError(t, whole.Write("stdout", []byte(line)))
			require.NoError(t, whole.Finish())
			split, b := collectAgentOutput(nil)
			for start := 0; start < len(line); start += 4093 {
				require.NoError(t, split.Write("stdout", []byte(line[start:min(start+4093, len(line))])))
			}
			require.NoError(t, split.Finish())
			docker, c := collectAgentOutput(nil)
			require.NoError(t, docker.WriteDocker("stdout", []byte("2026-11-11T10:00:00.000000001Z "+line)))
			require.NoError(t, docker.Finish())
			require.Equal(t, *a, *b)
			require.Equal(t, (*a)[0].content, (*c)[0].content)
			require.Len(t, *a, 1)
			require.False(t, (*a)[0].truncated)
			require.LessOrEqual(t, len((*a)[0].content), maxAgentOutputLine)
			require.True(t, json.Valid([]byte((*a)[0].content)))
			require.Contains(t, (*a)[0].content, "cut by Kuayle")
			if name == "codex" {
				require.Contains(t, (*a)[0].content, `"command":"npm test"`)
			} else {
				require.Contains(t, (*a)[0].content, `"tool_use_id":"toolu_1"`)
			}
		})
	}
}

// A final answer longer than a stored line used to be dropped before the
// result was parsed, so the run had no final message. The stored line is
// cut; the result is read from the whole message.
func TestOversizedFinalTextIsStillTheRunsFinalMessage(t *testing.T) {
	output, records := collectAgentOutput(nil)
	answer := "Summary of the change.\n" + strings.Repeat("- changed a file\n", 2000) + "END OF SUMMARY"
	require.Greater(t, len(answer), maxAgentOutputLine)
	require.NoError(t, output.Write("stdout", []byte(openCodeToolLine(t, "ls", "ok"))))
	require.NoError(t, output.Write("stdout", []byte(`{"type":"text","timestamp":1762855204000,"sessionID":"ses_1","part":{"id":"prt_4","type":"text","text":`+mustJSON(t, answer)+`,"time":{"start":1,"end":2}}}`+"\n")))
	require.NoError(t, output.Finish())
	require.Len(t, *records, 2)
	require.False(t, (*records)[1].truncated)
	require.LessOrEqual(t, len((*records)[1].content), maxAgentOutputLine)
	require.True(t, json.Valid([]byte((*records)[1].content)))

	result := agent.NewOpenCodeProvider("").ParseResult(output.Text("stdout"), output.Text("stderr"), 0)
	require.Equal(t, "succeeded", result.Status)
	require.True(t, result.FinalMessage)
	require.True(t, result.SummaryTruncated, "a summary over the limit says it was cut")
	require.Equal(t, answer[:agent.MaxSummaryBytes], result.Summary)
	require.NotContains(t, result.Summary, "cut by Kuayle")
}

// Redaction runs on the whole line before anything is cut, and again on the
// decoded values, so a secret is stored neither whole, nor escaped another
// way, nor as the fragment a cut would leave.
func TestCutEventsAreRedactedBeforeTheyAreStored(t *testing.T) {
	secrets := map[string]string{"TOKEN": "ghp_supersecrettoken123", "PATH_KEY": "key/with/slashes"}
	filler := strings.Repeat("y", 6000)
	// The token sits where the head of the cut value ends; the second secret
	// is escaped in a way the raw line does not match (\/).
	out := strings.Repeat("x", 6130) + "ghp_supersecrettoken123" + strings.Repeat("z", 60000) + " ghp_supersecrettoken123 tail"
	line := `{"type":"tool_use","part":{"type":"tool","tool":"bash","callID":"c1","state":{"status":"completed","input":{"command":"cat .env ` + filler + `"},"output":` + mustJSON(t, out) + `,"title":"key\/with\/slashes"}}}` + "\n"
	output, records := collectAgentOutput(secrets)
	require.NoError(t, output.Write("stdout", []byte(line)))
	require.NoError(t, output.Finish())
	require.Len(t, *records, 1)
	stored := (*records)[0].content
	require.False(t, (*records)[0].truncated)
	require.True(t, json.Valid([]byte(stored)))
	require.Contains(t, stored, "[REDACTED]")
	for _, text := range []string{stored, output.Text("stdout")} {
		require.NotContains(t, text, "ghp_supersecrettoken123")
		require.NotContains(t, text, "key/with/slashes")
		require.NotContains(t, text, `key\/with\/slashes`)
		require.NotContains(t, text, "ghp_supers", "no fragment of the secret is left at a cut")
	}
}

// Lines that are not one JSON object, that stay too large after cutting, or
// that exceed what Kuayle buffers keep the marker.
func TestOversizedLinesThatCannotBeCutKeepTheMarker(t *testing.T) {
	many := make([]string, 4000)
	for i := range many {
		many[i] = "value"
	}
	manyValues, _ := json.Marshal(map[string]any{"type": "tool_use", "values": many})
	for name, line := range map[string]string{
		"plain text":        strings.Repeat("x", maxAgentOutputLine+20),
		"broken JSON":       `{"type":"tool_use","output":"` + strings.Repeat("x", maxAgentOutputLine),
		"JSON array":        `["` + strings.Repeat("x", maxAgentOutputLine) + `"]`,
		"two objects":       `{"a":"` + strings.Repeat("x", maxAgentOutputLine) + `"} {"b":1}`,
		"many small values": string(manyValues),
		"beyond the buffer": `{"type":"tool_use","output":"` + strings.Repeat("x", maxAgentOutputEventLine) + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			output, records := collectAgentOutput(nil)
			require.NoError(t, output.Write("stdout", []byte("before\n"+line+"\nafter\n")))
			require.NoError(t, output.Finish())
			require.Len(t, *records, 3)
			require.True(t, (*records)[1].truncated)
			require.Equal(t, "[Oversized output line omitted]\n", (*records)[1].content)
			require.Equal(t, "after\n", (*records)[2].content)
			require.Equal(t, "before\n[Oversized output line omitted]\nafter\n", output.Text("stdout"))
		})
	}
}

func mustJSON(t *testing.T, value string) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	return string(encoded)
}

// The whole path, as in production: an OpenCode run of tool calls only (one
// with a 100 KB output) exits 0. It is recorded as failed for that reason,
// with an empty summary, and a run ending in a provider error names it.
func TestOpenCodeRunWithoutAFinalMessageIsRecordedAsFailed(t *testing.T) {
	step := `{"type":"step_start","timestamp":1762855200000,"sessionID":"ses_1","part":{"id":"prt_1","type":"step-start"}}` + "\n"
	finish := `{"type":"step_finish","timestamp":1762855202000,"sessionID":"ses_1","part":{"id":"prt_3","type":"step-finish","reason":"tool-calls"}}` + "\n"
	stdout := step + openCodeToolLine(t, "go test ./...", strings.Repeat("ok  \tpackage\t0.2s\n", 6000)) + finish
	store := runOutOfMemoryAgent(t, &AgentExecution{ContainerID: "agent", ExitCode: 0, Stdout: stdout}, true)
	require.Equal(t, domain.DevMachineAgentRunStatusFailed, store.completedRun.Status)
	require.Equal(t, "", *store.completedRun.Summary, "no stored summary starts with event JSON")
	require.Equal(t, "The agent ended without a final message, so there is no summary of what it did", *store.completedRun.ErrorMessage)
	require.True(t, domain.IsNoFinalMessageReason(*store.completedRun.ErrorMessage))

	failure := `{"type":"error","timestamp":1762855203000,"sessionID":"ses_1","error":{"name":"APIError","data":{"message":"429 The model is overloaded","statusCode":429}}}` + "\n"
	store = runOutOfMemoryAgent(t, &AgentExecution{ContainerID: "agent", ExitCode: 0, Stdout: stdout + failure}, true)
	require.Equal(t, domain.DevMachineAgentRunStatusFailed, store.completedRun.Status)
	require.Equal(t, "", *store.completedRun.Summary)
	require.Equal(t, "The agent ended without a final message, so there is no summary of what it did. The harness reported an error: APIError: 429 The model is overloaded", *store.completedRun.ErrorMessage)

	text := `{"type":"text","timestamp":1762855204000,"sessionID":"ses_1","part":{"id":"prt_4","type":"text","text":"Added the column; go test passes.","time":{"start":1,"end":2}}}` + "\n"
	store = runOutOfMemoryAgent(t, &AgentExecution{ContainerID: "agent", ExitCode: 0, Stdout: stdout + text}, true)
	require.Equal(t, domain.DevMachineAgentRunStatusSucceeded, store.completedRun.Status)
	require.Equal(t, "Added the column; go test passes.", *store.completedRun.Summary)
	require.Nil(t, store.completedRun.ErrorMessage)
}
