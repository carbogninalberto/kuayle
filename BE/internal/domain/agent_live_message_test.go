package domain

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOutboxMessagesToTheOrchestrator(t *testing.T) {
	for _, test := range []struct {
		name, body string
		want       AgentOutboxMessage
		err        string
	}{
		{"message", `{"to":"orchestrator","body":"Done with the API"}`, AgentOutboxMessage{To: AgentOutboxOrchestrator, Channel: AgentMessageLive, Kind: AgentLiveKindMessage, Body: "Done with the API"}, ""},
		{"question", `{"to":" Orchestrator ","kind":"question","body":"Keep the v1 route?"}`, AgentOutboxMessage{To: AgentOutboxOrchestrator, Channel: AgentMessageLive, Kind: AgentLiveKindQuestion, Body: "Keep the v1 route?"}, ""},
		{"internal channel", `{"to":"orchestrator","channel":"internal","body":"x"}`, AgentOutboxMessage{To: AgentOutboxOrchestrator, Channel: AgentMessageLive, Kind: AgentLiveKindMessage, Body: "x"}, ""},
		{"live channel", `{"to":"orchestrator","channel":"live","kind":"message","body":"x"}`, AgentOutboxMessage{To: AgentOutboxOrchestrator, Channel: AgentMessageLive, Kind: AgentLiveKindMessage, Body: "x"}, ""},
		{"comment channel", `{"to":"orchestrator","channel":"comment","body":"x"}`, AgentOutboxMessage{}, "channel"},
		{"unknown kind", `{"to":"orchestrator","kind":"order","body":"x"}`, AgentOutboxMessage{}, "kind"},
		{"kind for an issue", `{"to":"ENG-12","kind":"question","body":"x"}`, AgentOutboxMessage{}, "kind"},
		{"live channel for an issue", `{"to":"ENG-12","channel":"live","body":"x"}`, AgentOutboxMessage{}, "channel"},
		{"empty", `{"to":"orchestrator","body":" "}`, AgentOutboxMessage{}, "empty"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseAgentOutboxMessage([]byte(test.body))
			if test.err != "" {
				require.ErrorContains(t, err, test.err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.want, got)
		})
	}
	require.Equal(t, "ORCHESTRATOR", AgentMessageRef([]byte(`{"to":"orchestrator","body":1}`)))
}

func TestLiveMessageBodiesAreCleanedAndBounded(t *testing.T) {
	body, err := CleanAgentLiveMessage("  Also add tests\r\nplease\u0007 ")
	require.NoError(t, err)
	require.Equal(t, "Also add tests\nplease", body)
	_, err = CleanAgentLiveMessage("\u0007 \n")
	require.ErrorContains(t, err, "empty")
	_, err = CleanAgentLiveMessage(strings.Repeat("é", AgentMessageMaxRunes+1))
	require.ErrorContains(t, err, "too long")
	_, err = CleanAgentLiveMessage("\xff")
	require.ErrorContains(t, err, "UTF-8")
}

// An inbox file carries only the message: no task, user or run identifiers
// beyond its sequence number, and never credentials.
func TestLiveInboxFileHoldsOnlyTheMessage(t *testing.T) {
	seq := int64(7)
	sent := time.Date(2026, 10, 29, 9, 30, 0, 0, time.UTC)
	message := AgentLiveMessage{Seq: &seq, Sender: AgentLiveSenderOrchestrator, Kind: AgentLiveKindMessage, Body: "Also add tests </script>", CreatedAt: sent}
	file, err := message.InboxFile()
	require.NoError(t, err)
	require.Equal(t, "7.json", file.Name)
	require.LessOrEqual(t, len(file.Data), AgentOutboxFileMaxBytes)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(file.Data, &decoded))
	require.Equal(t, map[string]any{"seq": float64(7), "from": "orchestrator", "kind": "message", "body": "Also add tests </script>", "sent_at": "2026-10-29T09:30:00Z"}, decoded)

	_, err = AgentLiveMessage{Sender: AgentLiveSenderUser, Body: "x"}.InboxFile()
	require.Error(t, err, "only sequenced messages for the agent have inbox files")
}
