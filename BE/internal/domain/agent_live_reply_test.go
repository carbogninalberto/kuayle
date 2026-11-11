package domain

import (
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

func TestUnansweredMessageReasonNamesTheMessages(t *testing.T) {
	one := UnansweredMessageReason([]int64{3})
	require.Equal(t, "The agent ended without replying to message 3 from the person who owns the task", one)
	require.True(t, IsUnansweredMessageReason(one))
	require.Equal(t, "The agent ended without replying to messages 3, 5 from the person who owns the task", UnansweredMessageReason([]int64{3, 5}))
	require.False(t, IsUnansweredMessageReason(NoFinalMessageReason("")))
	require.False(t, IsNoFinalMessageReason(one), "the two reasons stay apart")
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
