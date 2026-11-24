package machine

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The shared agent CLI volume outlives releases. These tests run the helper
// script (agentCLIRefreshScript) against a volume on disk, a fake npm
// registry and a stand-in for the helper image's global packages, with the
// real flag contract (devmachine/common/check-agent-cli-flags.sh). A machine
// finds a CLI the way its PATH does: the volume's current bin directory
// first, then the image's.

// agentCLITestImage are the image's pinned versions in these tests.
var agentCLITestImage = map[string]string{
	"@anthropic-ai/claude-code": "2.1.330",
	"@opencode/cli":             "2.3.2",
	"@openai/codex":             "0.171.0",
	"@qwen-code/qwen-code":      "0.28.4",
}

type cliVolumeFixture struct {
	t        *testing.T
	root     string // the kuayle-agent-clis volume
	modules  string // the helper image's global node_modules
	imageBin string // the image's CLIs, after the volume on PATH
	registry string
	bin      string // fake npm and timeout
	tmp      string
	node     string
	help     map[string]string // a passing --help output per command
}

func newCLIVolumeFixture(t *testing.T) *cliVolumeFixture {
	t.Helper()
	node, err := exec.LookPath("node")
	require.NoError(t, err)
	npmRoot, err := exec.Command("npm", "root", "--global").Output()
	require.NoError(t, err, "the resolver and validator use npm's semver, as in the image")
	base := t.TempDir()
	volume := &cliVolumeFixture{
		t: t, root: filepath.Join(base, "agent-clis"), modules: filepath.Join(base, "image", "node_modules"),
		imageBin: filepath.Join(base, "image", "bin"), registry: filepath.Join(base, "registry"), bin: filepath.Join(base, "bin"),
		tmp: filepath.Join(base, "tmp"), node: node, help: agentCLIContractHelp(t),
	}
	for _, dir := range []string{volume.root, volume.modules, volume.imageBin, volume.registry, volume.bin, volume.tmp} {
		require.NoError(t, os.MkdirAll(dir, 0o755))
	}
	require.NoError(t, os.Symlink(filepath.Join(strings.TrimSpace(string(npmRoot)), "npm"), filepath.Join(volume.modules, "npm")))
	for _, item := range agentCLIPackages {
		version := agentCLITestImage[item.name]
		writeAgentCLIPackage(t, filepath.Join(volume.modules, item.name), item.command, version, volume.help[item.command])
		require.NoError(t, os.Symlink(filepath.Join(volume.modules, item.name, "bin", item.command), filepath.Join(volume.imageBin, item.command)))
	}
	// npm view prints the registry's view.json; npm install copies packages
	// from the registry and links their commands into node_modules/.bin.
	require.NoError(t, os.WriteFile(filepath.Join(volume.bin, "npm"), []byte(`#!/bin/sh
set -eu
registry='`+volume.registry+`'
case "$1" in
  view) cat "$registry/$2/view.json" ;;
  install)
    shift
    prefix= specs=
    while [ "$#" -gt 0 ]; do
      case "$1" in
        --prefix) prefix=$2; shift 2 ;;
        -*) shift ;;
        *) specs="$specs $1"; shift ;;
      esac
    done
    mkdir -p "$prefix/node_modules/.bin"
    for spec in $specs; do
      name=${spec%@*}
      mkdir -p "$prefix/node_modules/$name"
      cp -R "$registry/$name/${spec##*@}/." "$prefix/node_modules/$name/"
      for command in "$prefix/node_modules/$name/bin/"*; do
        ln -sf "../$name/bin/${command##*/}" "$prefix/node_modules/.bin/${command##*/}"
      done
    done ;;
  *) exit 2 ;;
esac
`), 0o755))
	// macOS has no timeout(1); the limits are not what these tests check.
	require.NoError(t, os.WriteFile(filepath.Join(volume.bin, "timeout"), []byte("#!/bin/sh\nshift\nexec \"$@\"\n"), 0o755))
	return volume
}

// agentCLIContractHelp is, per command, a help text listing every flag and
// value of the contract, which the real flag check accepts.
func agentCLIContractHelp(t *testing.T) map[string]string {
	t.Helper()
	script, err := os.ReadFile(filepath.Join(repoRootFromCaller(t), "devmachine", "common", "check-agent-cli-flags.sh"))
	require.NoError(t, err)
	_, list, found := strings.Cut(string(script), "<<'FLAGS'\n")
	require.True(t, found)
	list, _, found = strings.Cut(list, "\nFLAGS\n")
	require.True(t, found)
	help := map[string]string{}
	for _, line := range strings.Split(list, "\n") {
		command, tokens, found := strings.Cut(line, ":")
		require.True(t, found, line)
		cli := strings.Fields(command)[0]
		help[cli] += tokens + "\n"
	}
	return help
}

func writeAgentCLIPackage(t *testing.T, dir, command, version, help string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "bin"), 0o755))
	manifest, err := json.Marshal(map[string]any{"version": version, "bin": map[string]string{command: "bin/" + command}})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), manifest, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bin", command),
		[]byte("#!/bin/sh\nif [ \"${1:-}\" = --version ]; then echo '"+version+"'; exit 0; fi\ncat <<'HELP'\n"+help+"HELP\n"), 0o755))
}

// publish adds a version to the fake registry, published at a time; a
// dropped flag is left out of its help.
func (v *cliVolumeFixture) publish(name, command, version string, published time.Time, dropped string) {
	v.t.Helper()
	help := v.help[command]
	if dropped != "" {
		require.Contains(v.t, help, " "+dropped+" ")
		help = strings.ReplaceAll(help, " "+dropped+" ", " ")
	}
	writeAgentCLIPackage(v.t, filepath.Join(v.registry, name, version), command, version, help)
	path := filepath.Join(v.registry, name, "view.json")
	view := struct {
		Time     map[string]string `json:"time"`
		DistTags map[string]string `json:"dist-tags"`
	}{Time: map[string]string{}, DistTags: map[string]string{}}
	if data, err := os.ReadFile(path); err == nil {
		require.NoError(v.t, json.Unmarshal(data, &view))
	}
	view.Time[version] = published.UTC().Format(time.RFC3339)
	view.DistTags["latest"] = version
	data, err := json.Marshal(view)
	require.NoError(v.t, err)
	require.NoError(v.t, os.WriteFile(path, data, 0o644))
}

// release writes a release an earlier Kuayle installed, from published
// packages (name@version), and makes it current.
func (v *cliVolumeFixture) release(stamp string, specs ...string) string {
	v.t.Helper()
	dir := filepath.Join(v.root, "releases", stamp)
	require.NoError(v.t, os.MkdirAll(filepath.Join(dir, "node_modules", ".bin"), 0o755))
	var versions []string
	for _, spec := range specs {
		at := strings.LastIndex(spec, "@")
		name, version := spec[:at], spec[at+1:]
		require.NoError(v.t, os.CopyFS(filepath.Join(dir, "node_modules", name), os.DirFS(filepath.Join(v.registry, name, version))))
		entries, err := os.ReadDir(filepath.Join(dir, "node_modules", name, "bin"))
		require.NoError(v.t, err)
		for _, entry := range entries {
			require.NoError(v.t, os.Symlink(filepath.Join("..", name, "bin", entry.Name()), filepath.Join(dir, "node_modules", ".bin", entry.Name())))
		}
		versions = append(versions, spec[:at]+"="+version)
	}
	require.NoError(v.t, os.WriteFile(filepath.Join(dir, "versions.txt"), []byte(strings.Join(versions, "\n")+"\n"), 0o644))
	_ = os.Remove(filepath.Join(v.root, "current"))
	require.NoError(v.t, os.Symlink(filepath.Join("releases", stamp), filepath.Join(v.root, "current")))
	return dir
}

// helper runs the refresh helper's script as the manager starts it.
func (v *cliVolumeFixture) helper(mode string, before time.Time) (AgentCLIRefresh, string, error) {
	v.t.Helper()
	cmd := exec.Command("sh", agentCLIHelperArgs(mode, before)...)
	cmd.Env = append(agentCLIHelperEnv(),
		"PATH="+v.bin+string(os.PathListSeparator)+filepath.Dir(v.node)+string(os.PathListSeparator)+os.Getenv("PATH"),
		"TMPDIR="+v.tmp, "KUAYLE_AGENT_CLI_ROOT="+v.root, "KUAYLE_IMAGE_NODE_MODULES="+v.modules,
		"KUAYLE_AGENT_CLI_FLAG_CHECK="+filepath.Join(repoRootFromCaller(v.t), "devmachine", "common", "check-agent-cli-flags.sh"))
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	result, parseErr := parseAgentCLIRefresh(stdout.String())
	require.NoError(v.t, parseErr, "stdout %q, stderr %s", stdout.String(), stderr.String())
	return result, stderr.String(), err
}

// machineRuns is the version a machine starts for a command: the volume's
// current bin directory comes first on PATH, then the image's CLIs.
func (v *cliVolumeFixture) machineRuns(command string) string {
	v.t.Helper()
	cmd := exec.Command("sh", "-c", `"$1" --version`, "machine", command)
	cmd.Env = []string{"PATH=" + filepath.Join(v.root, "current", "node_modules", ".bin") + string(os.PathListSeparator) + v.imageBin + string(os.PathListSeparator) + "/usr/bin:/bin"}
	output, err := cmd.Output()
	require.NoError(v.t, err)
	return strings.TrimSpace(string(output))
}

func (v *cliVolumeFixture) releases() []string {
	v.t.Helper()
	entries, err := os.ReadDir(filepath.Join(v.root, "releases"))
	require.NoError(v.t, err)
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func (v *cliVolumeFixture) link(name string) string {
	target, err := os.Readlink(filepath.Join(v.root, name))
	if err != nil {
		return ""
	}
	return target
}

var agentCLITestNow = time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)

// publishImageVersions publishes every image version, so an earlier Kuayle's
// release and the refresh can install them.
func (v *cliVolumeFixture) publishImageVersions() {
	for _, item := range agentCLIPackages {
		v.publish(item.name, item.command, agentCLITestImage[item.name], agentCLITestNow.Add(-30*24*time.Hour), "")
	}
}

// After a deploy the volume still holds what the previous release's refresh
// installed. The manager checks it before it starts work; what does not fit
// the new image is set aside, so machines run the image's CLIs, and its files
// stay for runs that already started from it.
func TestAgentCLICheckSetsAsideAVolumeThatDoesNotFitTheImage(t *testing.T) {
	for _, scenario := range []struct {
		name    string
		publish func(v *cliVolumeFixture)
		release []string
		command string
		reason  string
	}{
		{
			name:    "older than the image",
			release: []string{"@anthropic-ai/claude-code@2.1.300", "@opencode/cli@2.3.2", "@openai/codex@0.171.0", "@qwen-code/qwen-code@0.28.4"},
			publish: func(v *cliVolumeFixture) {
				v.publish("@anthropic-ai/claude-code", "claude", "2.1.300", agentCLITestNow.Add(-60*24*time.Hour), "")
			},
			command: "claude", reason: "@anthropic-ai/claude-code 2.1.300 is older than the image's 2.1.330",
		},
		{
			name:    "the package was renamed",
			release: []string{"@anthropic-ai/claude-code@2.1.330", "opencode-ai@1.19.6", "@openai/codex@0.171.0", "@qwen-code/qwen-code@0.28.4"},
			publish: func(v *cliVolumeFixture) {
				v.publish("opencode-ai", "opencode", "1.19.6", agentCLITestNow.Add(-60*24*time.Hour), "")
			},
			command: "opencode", reason: "@opencode/cli is not installed",
		},
		{
			name:    "a newer version fails the flag contract",
			release: []string{"@anthropic-ai/claude-code@2.1.330", "@opencode/cli@2.4.0", "@openai/codex@0.171.0", "@qwen-code/qwen-code@0.28.4"},
			publish: func(v *cliVolumeFixture) {
				v.publish("@opencode/cli", "opencode", "2.4.0", agentCLITestNow.Add(-3*24*time.Hour), "--auto")
			},
			command: "opencode", reason: "opencode run --help: missing --auto",
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			volume := newCLIVolumeFixture(t)
			volume.publishImageVersions()
			scenario.publish(volume)
			old := volume.release("20261101000000", scenario.release...)

			result, stderr, err := volume.helper("check", agentCLITestNow)
			require.NoError(t, err, stderr)
			require.Equal(t, "rejected", result.State)
			require.False(t, result.Usable())
			require.Empty(t, result.Versions)
			require.Contains(t, stderr, scenario.reason)
			require.Empty(t, volume.link("current"), "PATH falls through to the image's CLIs")
			require.Equal(t, agentCLITestImage, map[string]string{
				"@anthropic-ai/claude-code": volume.machineRuns("claude"), "@opencode/cli": volume.machineRuns("opencode"),
				"@openai/codex": volume.machineRuns("codex"), "@qwen-code/qwen-code": volume.machineRuns("qwen"),
			})
			require.Equal(t, "releases/20261101000000", volume.link("rejected"))
			require.DirExists(t, old, "a run that started from it keeps its files")

			again, stderr, err := volume.helper("check", agentCLITestNow)
			require.NoError(t, err, stderr)
			require.Equal(t, "none", again.State)
		})
	}
}

// A volume that fits the image (each CLI at the image's version or newer,
// from the expected package, passing the contract) is kept as it is.
func TestAgentCLICheckKeepsAVolumeThatFitsTheImage(t *testing.T) {
	volume := newCLIVolumeFixture(t)
	volume.publishImageVersions()
	volume.publish("@openai/codex", "codex", "0.172.1", agentCLITestNow.Add(-3*24*time.Hour), "")
	volume.release("20261120000000", "@anthropic-ai/claude-code@2.1.330", "@opencode/cli@2.3.2", "@openai/codex@0.172.1", "@qwen-code/qwen-code@0.28.4")

	result, stderr, err := volume.helper("check", agentCLITestNow)
	require.NoError(t, err, stderr)
	require.Equal(t, "kept", result.State)
	require.True(t, result.Usable())
	require.Equal(t, "0.172.1", result.Versions["@openai/codex"])
	require.Equal(t, "releases/20261120000000", volume.link("current"))
	require.Equal(t, "0.172.1", volume.machineRuns("codex"))

	empty := newCLIVolumeFixture(t)
	result, stderr, err = empty.helper("check", agentCLITestNow)
	require.NoError(t, err, stderr)
	require.Equal(t, "none", result.State, "a new volume has no CLIs yet")
}

// The first refresh after a deploy replaces an old volume (an older version
// and the old OpenCode package) with the newest eligible versions; the old
// release is kept until the next switch, for runs that started from it, and
// then removed with the old package.
func TestAgentCLIRefreshUpgradesAnOldVolume(t *testing.T) {
	volume := newCLIVolumeFixture(t)
	volume.publishImageVersions()
	volume.publish("@anthropic-ai/claude-code", "claude", "2.1.300", agentCLITestNow.Add(-60*24*time.Hour), "")
	volume.publish("opencode-ai", "opencode", "1.19.6", agentCLITestNow.Add(-60*24*time.Hour), "")
	old := volume.release("20261101000000", "@anthropic-ai/claude-code@2.1.300", "opencode-ai@1.19.6", "@openai/codex@0.171.0", "@qwen-code/qwen-code@0.28.4")
	volume.publish("@opencode/cli", "opencode", "2.3.5", agentCLITestNow.Add(-3*24*time.Hour), "")
	volume.publish("@opencode/cli", "opencode", "2.3.6", agentCLITestNow.Add(-time.Hour), "") // too new

	result, stderr, err := volume.helper("refresh", agentCLITestNow.Add(-48*time.Hour))
	require.NoError(t, err, stderr)
	require.Equal(t, "changed", result.State)
	require.Equal(t, map[string]string{
		"@anthropic-ai/claude-code": "2.1.330", "@opencode/cli": "2.3.5", "@openai/codex": "0.171.0", "@qwen-code/qwen-code": "0.28.4",
	}, result.Versions)
	require.Equal(t, "2.3.5", volume.machineRuns("opencode"))
	require.Equal(t, "2.1.330", volume.machineRuns("claude"))
	require.Empty(t, volume.link("rejected"))
	info, err := os.Stat(filepath.Join(volume.root, "current"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o755), info.Mode().Perm(), "machines read it as their own user")
	require.DirExists(t, old, "kept until the next switch")
	require.Len(t, volume.releases(), 2)

	result, stderr, err = volume.helper("refresh", agentCLITestNow.Add(-48*time.Hour))
	require.NoError(t, err, stderr)
	require.Equal(t, "unchanged", result.State)
	require.Len(t, volume.releases(), 2, "an unchanged refresh leaves nothing behind")

	volume.publish("@openai/codex", "codex", "0.172.0", agentCLITestNow.Add(-3*24*time.Hour), "")
	result, stderr, err = volume.helper("refresh", agentCLITestNow.Add(-48*time.Hour))
	require.NoError(t, err, stderr)
	require.Equal(t, "changed", result.State)
	require.Equal(t, "0.172.0", volume.machineRuns("codex"))
	require.NoDirExists(t, old, "the old package goes with its release")
	require.Len(t, volume.releases(), 2)
	_, err = os.Stat(filepath.Join(volume.root, "current", "node_modules", "opencode-ai"))
	require.True(t, os.IsNotExist(err))
}

// A refresh whose new versions fail the flag contract is discarded: machines
// keep the current release when it fits, and the image's CLIs when nothing on
// the volume does. A failing CLI is never put on PATH.
func TestAgentCLIRefreshDiscardsVersionsThatFailTheFlagContract(t *testing.T) {
	t.Run("keeps the current release", func(t *testing.T) {
		volume := newCLIVolumeFixture(t)
		volume.publishImageVersions()
		volume.publish("@opencode/cli", "opencode", "2.3.5", agentCLITestNow.Add(-5*24*time.Hour), "")
		current := volume.release("20261120000000", "@anthropic-ai/claude-code@2.1.330", "@opencode/cli@2.3.5", "@openai/codex@0.171.0", "@qwen-code/qwen-code@0.28.4")
		volume.publish("@opencode/cli", "opencode", "2.5.0", agentCLITestNow.Add(-3*24*time.Hour), "--auto")

		result, stderr, err := volume.helper("refresh", agentCLITestNow.Add(-48*time.Hour))
		require.Error(t, err)
		require.Contains(t, stderr, "opencode run --help: missing --auto")
		require.Equal(t, "kept", result.State, "the report names what machines keep using")
		require.Equal(t, "2.3.5", result.Versions["@opencode/cli"])
		require.Equal(t, "releases/20261120000000", volume.link("current"))
		require.Equal(t, "2.3.5", volume.machineRuns("opencode"))
		require.Equal(t, []string{filepath.Base(current)}, volume.releases(), "the failed install is removed")
	})
	t.Run("falls back to the image", func(t *testing.T) {
		volume := newCLIVolumeFixture(t)
		volume.publishImageVersions()
		volume.publish("opencode-ai", "opencode", "1.19.6", agentCLITestNow.Add(-60*24*time.Hour), "")
		volume.release("20261101000000", "@anthropic-ai/claude-code@2.1.330", "opencode-ai@1.19.6", "@openai/codex@0.171.0", "@qwen-code/qwen-code@0.28.4")
		volume.publish("@opencode/cli", "opencode", "2.5.0", agentCLITestNow.Add(-3*24*time.Hour), "--auto")

		result, stderr, err := volume.helper("refresh", agentCLITestNow.Add(-48*time.Hour))
		require.Error(t, err)
		require.Contains(t, stderr, "opencode run --help: missing --auto")
		require.Equal(t, "rejected", result.State)
		require.Empty(t, volume.link("current"))
		require.Equal(t, "2.3.2", volume.machineRuns("opencode"), "the image's CLI, not 1.19.6 or 2.5.0")
		require.Equal(t, []string{"20261101000000"}, volume.releases())
	})
}
