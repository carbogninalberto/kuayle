package machine

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const procListen = "  sl  local_address rem_address   st\n" +
	"   0: 00000000:0BB8 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000 0 1\n" +
	"   1: 00000000:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000 0 2\n" +
	"   2: 0100007F:0BB8 0100007F:A1B2 01 00000000:00000000 00:00000000 00000000  1000 0 3\n" +
	"   3: 00000000:1E01 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000 0 4\n" +
	"   4: 0100007F:1388 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000 0 5\n" +
	"   5: 0B00007F:89FD 00000000:0000 0A 00000000:00000000 00:00000000 00000000  0 0 6\n"

type portsScript struct {
	t     *testing.T
	ports string
	proc  string
}

func newPortsScript(t *testing.T) *portsScript {
	t.Helper()
	root := t.TempDir()
	script := &portsScript{t: t, ports: filepath.Join(root, "ports"), proc: filepath.Join(root, "net")}
	require.NoError(t, os.MkdirAll(script.proc, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(script.proc, "tcp"), []byte(procListen), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(script.proc, "tcp6"), []byte("  sl  local_address\n"), 0o600))
	return script
}

func (s *portsScript) writeState(state string) {
	s.t.Helper()
	require.NoError(s.t, os.MkdirAll(s.ports, 0o700))
	require.NoError(s.t, os.WriteFile(filepath.Join(s.ports, "state.json"), []byte(state), 0o600))
}

func (s *portsScript) run(args ...string) (string, string, int) {
	s.t.Helper()
	cmd := exec.Command("sh", append([]string{filepath.Join(repoRootFromCaller(s.t), "devmachine", "common", "kuayle-ports")}, args...)...)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + s.t.TempDir(), "KUAYLE_PORTS_DIR=" + s.ports, "KUAYLE_PROC_NET=" + s.proc, "KUAYLE_PORTS_TIMEOUT=5"}
	cmd.Dir = s.t.TempDir()
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		code = exit.ExitCode()
	} else {
		require.NoError(s.t, err)
	}
	return stdout.String(), stderr.String(), code
}

// answer plays the manager: it takes the named request and writes state.
func (s *portsScript) answer(name, state string) chan []byte {
	taken := make(chan []byte, 1)
	go func() {
		path := filepath.Join(s.ports, "requests", name)
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
			if body, err := os.ReadFile(path); err == nil {
				_ = os.Remove(path)
				_ = os.WriteFile(filepath.Join(s.ports, "state.json"), []byte(state), 0o600)
				taken <- body
				return
			}
		}
		close(taken)
	}()
	return taken
}

func TestKuaylePortsListsListeningPortsWithTheirURLs(t *testing.T) {
	script := newPortsScript(t)
	script.writeState(`{"ports":[{"port":3000,"label":"web","url":"https://abc-3000.machines.test","source":"agent"},{"port":5173,"url":"https://abc-5173.machines.test","source":"user"}],"listening":[3000],"reserved":[7681],"refused":[],"updated_at":"2026-10-28T10:00:00Z"}`)
	out, _, code := script.run()
	require.Zero(t, code)
	require.Contains(t, out, "3000    https://abc-3000.machines.test (agent)")
	require.Contains(t, out, "8080    listening, not exposed")
	require.NotContains(t, out, "7681", "the machine's own services are not offered")
	require.Contains(t, out, "5000    localhost only: bind to 0.0.0.0 to expose it")
	require.NotContains(t, out, "35325", "Docker's DNS resolver is not an app")
	require.Contains(t, out, "5173    https://abc-5173.machines.test (not listening)")
	require.Equal(t, 1, strings.Count(out, "3000 "), "the established connection is not a listener")
}

// Ports published by containers of the machine's Docker daemon listen there,
// not in this container; kuayle-ports lists them from Kuayle's state and does
// not warn that nothing listens when one is exposed.
func TestKuaylePortsListsPortsPublishedByTheDockerDaemon(t *testing.T) {
	script := newPortsScript(t)
	script.writeState(`{"ports":[{"port":5432,"url":"https://abc-5432.machines.test","source":"agent"}],"listening":[3000,5432,8025],"docker":[5432,8025],"reserved":[2376,7681],"refused":[],"updated_at":"2026-10-28T10:00:00Z"}`)
	out, _, code := script.run()
	require.Zero(t, code)
	require.Contains(t, out, "5432    https://abc-5432.machines.test (agent, docker)")
	require.Contains(t, out, "8025    published by a Docker container, not exposed")
	require.NotContains(t, out, "(not listening)")

	taken := script.answer("expose-8025.json", `{"ports":[{"port":8025,"url":"https://abc-8025.machines.test","source":"agent"}],"listening":[8025],"docker":[8025],"reserved":[2376],"refused":[],"updated_at":"2026-10-28T10:00:15Z"}`)
	out, stderr, code := script.run("expose", "8025")
	<-taken
	require.Zero(t, code)
	require.Equal(t, "https://abc-8025.machines.test\n", out)
	require.NotContains(t, stderr, "nothing listens")
}

func TestKuaylePortsExposeWritesAStrictRequestAndPrintsTheURL(t *testing.T) {
	script := newPortsScript(t)
	script.writeState(`{"ports":[],"listening":[],"refused":[],"updated_at":"2026-10-28T10:00:00Z"}`)
	taken := script.answer("expose-3000.json", `{"ports":[{"port":3000,"label":"web app","url":"https://abc-3000.machines.test","source":"agent"}],"listening":[3000],"refused":[],"updated_at":"2026-10-28T10:00:15Z"}`)
	out, _, code := script.run("expose", "3000", "--label", "web app")
	require.Zero(t, code)
	require.Equal(t, "https://abc-3000.machines.test\n", out)
	body := <-taken
	var request map[string]any
	require.NoError(t, json.Unmarshal(body, &request), string(body))
	require.Equal(t, map[string]any{"port": float64(3000), "label": "web app"}, request)
	action, err := parsePortRequest(PortRequest{Name: "expose-3000.json", Body: body})
	require.NoError(t, err, "the manager accepts what the script writes")
	require.Equal(t, 3000, action.port)

	taken = script.answer("expose-8080.json", `{"ports":[],"listening":[3000],"refused":[{"port":8080,"reason":"port 8080 belongs to the machine's ide service"}],"updated_at":"2026-10-28T10:00:30Z"}`)
	_, stderr, code := script.run("expose", "8080")
	<-taken
	require.Equal(t, 4, code)
	require.Contains(t, stderr, "belongs to the machine's ide service")

	for _, args := range [][]string{{"expose", "80"}, {"expose", "03000"}, {"expose", "3000", "--label", `a"b`}, {"expose", "3000", "--label", strings.Repeat("a", 41)}, {"unexpose", "x"}, {"bogus"}} {
		_, _, code = script.run(args...)
		require.Equal(t, 2, code, args)
	}
	require.NoFileExists(t, filepath.Join(script.ports, "requests", "expose-80.json"))
}

func TestKuaylePortsUnexposeAndTimeout(t *testing.T) {
	script := newPortsScript(t)
	script.writeState(`{"ports":[{"port":3000,"url":"https://abc-3000.machines.test","source":"agent"}],"listening":[],"refused":[],"updated_at":"2026-10-28T10:00:00Z"}`)
	taken := script.answer("unexpose-3000", `{"ports":[],"listening":[],"refused":[],"updated_at":"2026-10-28T10:00:15Z"}`)
	out, _, code := script.run("unexpose", "3000")
	<-taken
	require.Zero(t, code)
	require.Contains(t, out, "no longer exposed")

	start := time.Now()
	_, stderr, code := script.run("expose", "4000")
	require.Equal(t, 3, code, "no manager answered")
	require.Contains(t, stderr, "did not answer")
	require.Less(t, time.Since(start), 15*time.Second)
	require.NoFileExists(t, filepath.Join(script.ports, "requests", "expose-4000.json"), "an unanswered request is withdrawn")
}

func TestStartCommonInstallsTheQASkillForBothAgents(t *testing.T) {
	repoRoot := repoRootFromCaller(t)
	home := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".claude", "skills"), 0o700))
	outside := t.TempDir()
	require.NoError(t, os.Symlink(outside, filepath.Join(home, ".claude", "skills", "kuayle-qa")))
	cmd := exec.Command("sh", "-c", `. "$1"; install_kuayle_qa`, "test", filepath.Join(repoRoot, "devmachine", "common", "start-common.sh"))
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "KUAYLE_QA_SKILL=" + filepath.Join(repoRoot, "devmachine", "common", "kuayle-qa.md")}
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	expected, err := os.ReadFile(filepath.Join(repoRoot, "devmachine", "common", "kuayle-qa.md"))
	require.NoError(t, err)
	for _, agent := range []string{".claude", ".codex"} {
		installed, err := os.ReadFile(filepath.Join(home, agent, "skills", "kuayle-qa", "SKILL.md"))
		require.NoError(t, err, agent)
		require.Equal(t, string(expected), string(installed))
	}
	entries, err := os.ReadDir(outside)
	require.NoError(t, err)
	require.Empty(t, entries, "a symlinked skill directory is replaced, not written through")
	require.DirExists(t, filepath.Join(home, ".kuayle", "ports", "requests"))
	require.Contains(t, string(expected), "name: kuayle-qa")
}

func TestAgentRunnerInstallsTheMessagingSkillUnderDash(t *testing.T) {
	repoRoot := repoRootFromCaller(t)
	runner, err := os.ReadFile(filepath.Join(repoRoot, "devmachine", "common", "kuayle-agent-runner"))
	require.NoError(t, err)
	require.Contains(t, string(runner), "\ninstall_kuayle_agents\n", "autonomous attempts get the skill")
	for _, shell := range outboxShells(t) {
		home := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(home, ".codex", "skills"), 0o700))
		outside := t.TempDir()
		require.NoError(t, os.Symlink(outside, filepath.Join(home, ".codex", "skills", "kuayle-agents")))
		cmd := exec.Command(shell, "-c", `. "$1"; install_kuayle_agents; install_kuayle_qa`, "test", filepath.Join(repoRoot, "devmachine", "common", "start-common.sh"))
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "KUAYLE_AGENTS_SKILL=" + filepath.Join(repoRoot, "devmachine", "common", "kuayle-agents.md"), "KUAYLE_QA_SKILL=" + filepath.Join(repoRoot, "devmachine", "common", "kuayle-qa.md")}
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, string(output))
		expected, err := os.ReadFile(filepath.Join(repoRoot, "devmachine", "common", "kuayle-agents.md"))
		require.NoError(t, err)
		require.Contains(t, string(expected), "name: kuayle-agents")
		require.Contains(t, string(expected), ".kuayle/outbox/")
		for _, agent := range []string{".claude", ".codex"} {
			installed, err := os.ReadFile(filepath.Join(home, agent, "skills", "kuayle-agents", "SKILL.md"))
			require.NoError(t, err, agent)
			require.Equal(t, string(expected), string(installed))
			require.FileExists(t, filepath.Join(home, agent, "skills", "kuayle-qa", "SKILL.md"))
		}
		entries, err := os.ReadDir(outside)
		require.NoError(t, err)
		require.Empty(t, entries, "a symlinked skill directory is replaced, not written through")
	}
}
