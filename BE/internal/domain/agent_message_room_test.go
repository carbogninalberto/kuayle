package domain

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOutboxMessagesToTheRoom(t *testing.T) {
	m, err := ParseAgentOutboxMessage([]byte(`{"to":"@reviewer","kind":"ask","body":"  Is the v1 route still used?  "}`))
	require.NoError(t, err)
	require.Equal(t, AgentOutboxRoom{ToRoles: []string{"reviewer"}, Kind: RoomAsk}, *m.Room)
	require.Equal(t, "Is the v1 route still used?", m.Body)
	require.True(t, IsRoomOutbox([]byte(`{"to":"@reviewer","kind":"ask","body":"x"}`)))

	m, err = ParseAgentOutboxMessage([]byte(`{"to":"@ALL","body":"Pushed the fix.","reply_to":4}`))
	require.NoError(t, err)
	require.Equal(t, AgentOutboxRoom{Kind: RoomSay}, *m.Room, "@all is everyone and say is the default kind")
	require.Equal(t, int64(4), *m.ReplyTo)

	m, err = ParseAgentOutboxMessage([]byte(`{"to":"@implementer","kind":"verdict","data":{"verdict":"approve","commit":"abc1234"}}`))
	require.NoError(t, err, "a verdict may carry its findings in a file or nowhere")
	require.Equal(t, TaskRoomData{Verdict: "approve", Commit: "abc1234"}, m.Room.Data)

	m, err = ParseAgentOutboxMessage([]byte(`{"to":"ENG-12","body":"unchanged"}`))
	require.NoError(t, err)
	require.Nil(t, m.Room, "issue recipients are not room messages")
	require.False(t, IsRoomOutbox([]byte(`{"to":"ENG-12","body":"x"}`)))
	require.False(t, IsRoomOutbox([]byte(`not json`)))

	for name, data := range map[string]string{
		"a bad role":                `{"to":"@Not a role","body":"x"}`,
		"no role":                   `{"to":"@","body":"x"}`,
		"a channel":                 `{"to":"@reviewer","channel":"comment","body":"x"}`,
		"an unknown kind":           `{"to":"@reviewer","kind":"shout","body":"x"}`,
		"an empty say":              `{"to":"@reviewer","body":"  "}`,
		"an empty ask":              `{"to":"@reviewer","kind":"ask","body":""}`,
		"a say without text":        `{"to":"@reviewer"}`,
		"too long":                  `{"to":"@reviewer","body":"` + strings.Repeat("x", AgentMessageMaxRunes+1) + `"}`,
		"a reply to nothing":        `{"to":"@reviewer","body":"x","reply_to":0}`,
		"unknown data":              `{"to":"@reviewer","body":"x","data":{"secret":"y"}}`,
		"data for another agent":    `{"to":"ENG-12","body":"x","data":{"verdict":"approve"}}`,
		"data for the orchestrator": `{"to":"orchestrator","body":"x","data":{"verdict":"approve"}}`,
	} {
		_, err := ParseAgentOutboxMessage([]byte(data))
		require.Error(t, err, name)
	}
}
