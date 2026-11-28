package machine

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

// The single machine image ships a passwordless sudoers rule, validated at
// build time, which takes effect only in containers the runtime relaxes: agent
// attempts, checks, previews, tools and commands always
// (TestAgentContainersAlwaysAllowSudo), the developer terminal when the
// workspace allows sudo (TestSudoMachinesRelaxOnlyTheDeveloperContainers). It
// also points global npm installs at the workspace volume, ahead of the
// bundled CLIs on PATH, so upgrading Claude Code needs no sudo.
func TestTheMachineImageShipsSudoAndAWorkspaceNpmPrefix(t *testing.T) {
	root := repoRootFromCaller(t)
	for _, image := range []string{"headless"} {
		content, err := os.ReadFile(filepath.Join(root, "devmachine", image, "Dockerfile"))
		require.NoError(t, err)
		dockerfile := string(content)
		require.Regexp(t, `apt-get install -y --no-install-recommends[^&]*\bsudo\b`, dockerfile, image)
		require.Contains(t, dockerfile, `printf '%s ALL=(ALL) NOPASSWD:ALL\n' "$user_name" > /etc/sudoers.d/kuayle`, image)
		require.Contains(t, dockerfile, "chmod 0440 /etc/sudoers.d/kuayle", image)
		require.Contains(t, dockerfile, "visudo -cf /etc/sudoers.d/kuayle", image)
		require.Contains(t, dockerfile, `printf 'prefix=/workspace/.kuayle/npm-global\nallow-scripts=@anthropic-ai/claude-code\n' > /etc/npmrc`, image)
		require.Contains(t, dockerfile, "NPM_CONFIG_GLOBALCONFIG=/etc/npmrc", image)
		require.Regexp(t, regexp.MustCompile(`(?m)^\s+PATH="/workspace/\.kuayle/npm-global/bin:`), dockerfile, image)
		require.Regexp(t, regexp.MustCompile(`(?m)^USER 1000:1000$`), dockerfile, image)
	}
}
