package machine

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

// Both developer images ship the same passwordless sudoers rule, validated at
// build time, which only takes effect in containers the runtime relaxes
// (TestSudoMachinesRelaxOnlyTheDeveloperContainers). They also point global
// npm installs at the workspace volume, ahead of the bundled CLIs on PATH, so
// upgrading Claude Code needs no sudo. Agent images have no sudo at all.
func TestDeveloperImagesShipInertSudoAndAWorkspaceNpmPrefix(t *testing.T) {
	root := repoRootFromCaller(t)
	for _, image := range []string{"headless", "ide"} {
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
	agent, err := os.ReadFile(filepath.Join(root, "devmachine", "agent", "Dockerfile"))
	require.NoError(t, err)
	require.NotRegexp(t, `\bsudo\b`, string(agent), "agent attempts never get sudo")
}
