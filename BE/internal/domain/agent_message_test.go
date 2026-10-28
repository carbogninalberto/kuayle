package domain

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOutboxMessagesAreStrictAndBounded(t *testing.T) {
	for _, test := range []struct {
		name, body string
		want       AgentOutboxMessage
		err        string
	}{
		{"minimal", `{"to":"ENG-12","body":"API is ready"}`, AgentOutboxMessage{To: "ENG-12", Channel: AgentMessageInternal, Body: "API is ready"}, ""},
		{"comment", `{"to":"eng-12","channel":"comment","body":"  Merged the schema\n"}`, AgentOutboxMessage{To: "ENG-12", Channel: AgentMessageComment, Body: "Merged the schema"}, ""},
		{"controls", "{\"to\":\"ENG-12\",\"body\":\"a\\u0007b\\tc\\r\\nd\"}", AgentOutboxMessage{To: "ENG-12", Channel: AgentMessageInternal, Body: "ab\tc\nd"}, ""},
		{"unknown field", `{"to":"ENG-12","body":"x","from":"ENG-1"}`, AgentOutboxMessage{}, "invalid JSON"},
		{"two values", `{"to":"ENG-12","body":"x"}{"to":"ENG-13","body":"y"}`, AgentOutboxMessage{}, "invalid JSON"},
		{"array", `[{"to":"ENG-12","body":"x"}]`, AgentOutboxMessage{}, "invalid JSON"},
		{"channel", `{"to":"ENG-12","channel":"email","body":"x"}`, AgentOutboxMessage{}, "channel"},
		{"recipient", `{"to":"ENG 12; DROP","body":"x"}`, AgentOutboxMessage{}, "recipient"},
		{"empty", `{"to":"ENG-12","body":"   "}`, AgentOutboxMessage{}, "empty"},
		{"long", `{"to":"ENG-12","body":"` + strings.Repeat("é", AgentMessageMaxRunes+1) + `"}`, AgentOutboxMessage{}, "too long"},
		{"invalid utf8", "{\"to\":\"ENG-12\",\"body\":\"\xff\"}", AgentOutboxMessage{}, "UTF-8"},
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
	_, err := ParseAgentOutboxMessage([]byte(strings.Repeat(" ", AgentOutboxFileMaxBytes) + `{}`))
	require.ErrorContains(t, err, "too large")
}

func TestAgentMessageRecipientRefIsBoundedForRefusals(t *testing.T) {
	require.Equal(t, "ENG-12", AgentMessageRef([]byte(`{"to":"eng-12","body":1}`)))
	require.Equal(t, "?", AgentMessageRef([]byte(`not json`)))
	require.Len(t, []rune(AgentMessageRef([]byte(`{"to":"`+strings.Repeat("x", 500)+`"}`))), 64)
}
