package domain

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// A reply is an outbox file to the orchestrator that names the message it
// answers by sequence number; nothing else carries reply_to.
func TestAnOutboxReplyNamesTheMessageItAnswers(t *testing.T) {
	reply, err := ParseAgentOutboxMessage([]byte(`{"to":"orchestrator","reply_to":3,"body":"Understood: adding the empty-list tests first."}`))
	require.NoError(t, err)
	require.Equal(t, AgentOutboxOrchestrator, reply.To)
	require.NotNil(t, reply.ReplyTo)
	require.Equal(t, int64(3), *reply.ReplyTo)
	require.Equal(t, AgentLiveKindMessage, reply.Kind)

	plain, err := ParseAgentOutboxMessage([]byte(`{"to":"orchestrator","body":"Tests added"}`))
	require.NoError(t, err)
	require.Nil(t, plain.ReplyTo, "a report is not a reply")

	for name, file := range map[string]string{
		"zero":            `{"to":"orchestrator","reply_to":0,"body":"x"}`,
		"negative":        `{"to":"orchestrator","reply_to":-1,"body":"x"}`,
		"text":            `{"to":"orchestrator","reply_to":"3","body":"x"}`,
		"fraction":        `{"to":"orchestrator","reply_to":3.5,"body":"x"}`,
		"another agent":   `{"to":"ENG-12","reply_to":3,"body":"x"}`,
		"the registry":    `{"to":"registry","reply_to":3,"body":"x"}`,
		"an unknown name": `{"to":"orchestrator","in_reply_to":3,"body":"x"}`,
	} {
		_, err := ParseAgentOutboxMessage([]byte(file))
		require.Error(t, err, name)
	}
}

// A missing reply is a note on the attempt, never a reason it fails.
func TestAMissingReplyIsANoteNotAFailure(t *testing.T) {
	note := UnansweredMessageNote([]int64{3})
	require.Contains(t, note, "did not reply to message 3 from the person who owns the task")
	require.Contains(t, note, "the work is kept")
	require.Contains(t, UnansweredMessageNote([]int64{3, 5}), "messages 3, 5")
	require.False(t, IsNoFinalMessageReason(note))
	require.False(t, IsResourceLimitReason(note))
	require.False(t, IsWorkflowReason(note))

	ask := ReplyClosingAsk([]int64{3, 5})
	require.Contains(t, ask.Missing, "you did not reply to messages 3, 5")
	require.Contains(t, ask.Do, `{"to":"orchestrator","reply_to":<message number>,"body":"..."}`)
	require.Contains(t, ReplyClosingAsk([]int64{3}).Missing, "message 3 ")
}

// One closing continuation asks for everything that is missing: a reply and
// a notebook update are two numbered items of the same prompt.
func TestOneClosingPromptAsksForTheReplyAndTheNotebook(t *testing.T) {
	reply, notebook := ReplyClosingAsk([]int64{3}), NotebookClosingAsk("")
	both := ClosingPrompt([]ClosingAsk{reply, notebook})
	require.Contains(t, both, "2 things are missing before this step can close:")
	require.Contains(t, both, "1. "+reply.Missing)
	require.Contains(t, both, "2. "+notebook.Missing)
	require.Contains(t, both, "reply_to")
	only := ClosingPrompt([]ClosingAsk{reply})
	require.Contains(t, only, "One thing is missing before this step can close: you did not reply to message 3")
	require.NotContains(t, only, AgentNotebookFile)
	require.Contains(t, only, "Do nothing else")
	require.Empty(t, ClosingPrompt(nil))
}

// An attempt stopped to deliver a message is requeued, not cancelled; the
// task says it is being interrupted, so views do not read it as cancelling.
func TestAnInterruptedAttemptIsRequeued(t *testing.T) {
	require.True(t, ValidAgentTaskTransition("cancelling", "queued"))
	require.False(t, ValidAgentTaskTransition("cancelling", "running"))
	next := int64(3)
	task := AgentTask{State: "cancelling", Generation: 2, LiveResumeGeneration: &next}
	require.True(t, task.LiveInterrupting())
	encoded, err := json.Marshal(task)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"interrupting":true`)
	for name, other := range map[string]AgentTask{
		"a cancellation":          {State: "cancelling", Generation: 2},
		"a continued attempt":     {State: "queued", Generation: 3, LiveResumeGeneration: &next},
		"an earlier interruption": {State: "cancelling", Generation: 3, LiveResumeGeneration: &next},
	} {
		require.False(t, other.LiveInterrupting(), name)
		encoded, err = json.Marshal(other)
		require.NoError(t, err)
		require.NotContains(t, string(encoded), "interrupting", name)
	}
}

// Only Claude Code can be handed a message in the middle of a turn; the
// others receive it by being interrupted and resumed.
func TestOnlyClaudeCodeReadsItsInboxMidTurn(t *testing.T) {
	require.True(t, HarnessReadsLiveInbox("claude-code"))
	for _, provider := range []string{"codex", "opencode", ""} {
		require.False(t, HarnessReadsLiveInbox(provider), provider)
	}
}

func TestLiveResumedIsOnlyTheAttemptStartedForTheMessage(t *testing.T) {
	two, three := int64(2), int64(3)
	require.False(t, AgentTask{Generation: 2}.LiveResumed())
	require.True(t, AgentTask{Generation: 2, LiveResumeGeneration: &two}.LiveResumed())
	require.False(t, AgentTask{Generation: 2, LiveResumeGeneration: &three}.LiveResumed(), "the interrupted attempt is still being stopped")
	require.False(t, AgentTask{Generation: 3, LiveResumeGeneration: &two}.LiveResumed(), "a later attempt is an ordinary one")
}
