package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// agentCLIFlagContract reads the help commands and tokens that the images
// check each built-in CLI's help for (devmachine/common/check-agent-cli-flags.sh),
// keyed by the CLI's binary.
func agentCLIFlagContract(t *testing.T) map[string][]string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	script, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "..", "devmachine", "common", "check-agent-cli-flags.sh"))
	require.NoError(t, err)
	_, list, found := strings.Cut(string(script), "<<'FLAGS'\n")
	require.True(t, found, "the contract starts with <<'FLAGS'")
	list, _, found = strings.Cut(list, "\nFLAGS\n")
	require.True(t, found, "the contract ends with FLAGS")
	contract := map[string][]string{}
	for _, line := range strings.Split(list, "\n") {
		command, tokens, found := strings.Cut(line, ":")
		require.True(t, found, line)
		cli := strings.Fields(command)[0]
		contract[cli] = append(contract[cli], strings.Fields(tokens)...)
	}
	return contract
}

// The flags each built-in adapter passes, keyed by binary, over every mode,
// a model with an effort, and a new and a resumed session.
func adapterFlags(t *testing.T) (map[string]map[string]bool, map[string]map[string]bool) {
	t.Helper()
	flags, words := map[string]map[string]bool{}, map[string]map[string]bool{}
	inputs := []RunInput{
		{Prompt: "PROMPT"},
		{Prompt: "PROMPT", Model: "provider/MODEL", Thinking: "high"},
		{Prompt: "PROMPT", Session: &SessionInput{Dir: "/workspace/.kuayle/agent-sessions/00000000-0000-0000-0000-000000000000", NewID: "00000000-0000-0000-0000-000000000001"}},
		{Prompt: "PROMPT", Session: &SessionInput{Dir: "/workspace/.kuayle/agent-sessions/00000000-0000-0000-0000-000000000000", ResumeID: "00000000-0000-0000-0000-000000000002"}},
	}
	for _, provider := range []Provider{
		NewClaudeCodeProvider(""), NewOpenCodeProvider(""), NewCodexProvider(""),
		NewQwenCodeProvider(""), NewCrushProvider(""), NewKimiProvider(""), NewGooseProvider(""),
	} {
		for _, mode := range []Mode{ModeAutonomous, ModeInteractive} {
			for _, input := range inputs {
				input.Mode = mode
				invocation, err := provider.BuildInvocation(input)
				require.NoError(t, err)
				argv := invocation.Argv
				// The session wrapper runs the CLI as "$@".
				if argv[0] == "/bin/sh" {
					argv = argv[5:]
				}
				cli := argv[0]
				if flags[cli] == nil {
					flags[cli], words[cli] = map[string]bool{}, map[string]bool{}
				}
				for _, arg := range argv[1:] {
					if strings.HasPrefix(arg, "-") && arg != "--" {
						flags[cli][arg] = true
					} else {
						words[cli][arg] = true
					}
				}
			}
		}
	}
	return flags, words
}

// The images fail when a CLI's help lacks a flag in the contract, so the
// contract must name every flag an adapter passes, and only those; its fixed
// values (stream-json, bypassPermissions) must be ones an adapter passes.
func TestAgentCLIFlagContractMatchesTheAdapters(t *testing.T) {
	contract := agentCLIFlagContract(t)
	flags, words := adapterFlags(t)
	require.ElementsMatch(t, sortedKeys(flags), sortedKeys(contract), "every built-in CLI has a contract")
	for cli, tokens := range contract {
		listed := map[string]bool{}
		for _, token := range tokens {
			if strings.HasPrefix(token, "-") {
				listed[token] = true
				continue
			}
			require.True(t, words[cli][token], "%s: the contract's %q is not something the adapter passes", cli, token)
		}
		require.Equal(t, sortedKeys(flags[cli]), sortedKeys(listed), "%s: the contract lists exactly the adapter's flags", cli)
	}
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
