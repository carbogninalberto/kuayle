package machine

import (
	"testing"
	"time"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// Production (KUA-98): Claude Code exited 1 at its 5-hour limit. The run
// error says so in the form the task worker pauses on.
func TestUsageLimitedAgentRunRecordsTheLimit(t *testing.T) {
	stdout := `{"type":"rate_limit_event","rate_limit_info":{"status":"rejected","resetsAt":1794283800,"rateLimitType":"five_hour"}}
{"type":"assistant","message":{"content":[{"type":"text","text":"You've hit your 5-hour limit · resets 4:10am (UTC)"}]},"api_error":"usage_limit_reached"}
{"type":"result","is_error":true,"result":"You've hit your 5-hour limit · resets 4:10am (UTC)"}`
	store := runOutOfMemoryAgent(t, &AgentExecution{ContainerID: "agent", ExitCode: 1, Stdout: stdout}, true)
	require.Equal(t, domain.DevMachineAgentRunStatusFailed, store.completedRun.Status)
	require.NotNil(t, store.completedRun.ErrorMessage)
	limit, ok := domain.ParseUsageLimitError(*store.completedRun.ErrorMessage)
	require.True(t, ok, *store.completedRun.ErrorMessage)
	require.Equal(t, "You've hit your 5-hour limit · resets 4:10am (UTC)", limit.Message)
	require.True(t, limit.ResetsAt.Equal(time.Unix(1794283800, 0)))
}

func TestOrdinaryAgentFailureIsNotAUsageLimit(t *testing.T) {
	store := runOutOfMemoryAgent(t, &AgentExecution{ContainerID: "agent", ExitCode: 1, Stdout: `{"type":"result","is_error":true,"result":"2 tests failed"}`}, true)
	if store.completedRun.ErrorMessage != nil {
		_, ok := domain.ParseUsageLimitError(*store.completedRun.ErrorMessage)
		require.False(t, ok)
	}
}
