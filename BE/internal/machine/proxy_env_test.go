package machine

import (
	"slices"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// Production: `sudo apt-get update` could not reach the Ubuntu mirrors. apt,
// curl and wget read only the lowercase variables for plain HTTP, so every
// machine container gets both spellings.
func TestMachineContainersGetBothProxySpellings(t *testing.T) {
	machine := &domain.DevMachine{RoutingKey: "abc"}
	for _, docker := range []bool{false, true} {
		environment := proxyEnvironment(machine, docker)
		for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy"} {
			require.Contains(t, environment, name+"=http://abc-egress:3128", name)
		}
		upper := slices.IndexFunc(environment, func(value string) bool { return len(value) > 9 && value[:9] == "NO_PROXY=" })
		lower := slices.IndexFunc(environment, func(value string) bool { return len(value) > 9 && value[:9] == "no_proxy=" })
		require.NotEqual(t, -1, upper)
		require.NotEqual(t, -1, lower)
		require.Equal(t, environment[upper][9:], environment[lower][9:])
	}
	agent := agentProxyEnvironment(machine)
	require.Contains(t, agent, "http_proxy=http://abc-egress:3128")
	require.Contains(t, agent, "no_proxy=localhost,127.0.0.1,abc-collector")
}
