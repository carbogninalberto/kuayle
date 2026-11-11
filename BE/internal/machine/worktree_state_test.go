package machine

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// The fingerprint Kuayle takes before and after a planning step sees every
// way an agent can change a worktree (an edit, a new file, a staged change,
// a commit) and nothing else: Kuayle's own .kuayle/ files and ignored files
// leave it as it was. It never touches the checkout's index.
func TestWorktreeStateSeesEveryChangeAnAgentCanMake(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root, "-c", "user.name=t", "-c", "user.email=t@example.invalid", "-c", "commit.gpgsign=false"}, args...)...)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
		return string(out)
	}
	write := func(name, content string) {
		t.Helper()
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(content), 0o644))
	}
	state := func() string {
		t.Helper()
		out, err := exec.Command("/bin/sh", "-c", worktreeStateScript, "kuayle-worktree", root).CombinedOutput()
		require.NoError(t, err, string(out))
		parsed := parseWorktreeState(string(out))
		require.NotEmpty(t, parsed, string(out))
		return parsed
	}
	files := func(before, after string) []string {
		t.Helper()
		changed, from, to := worktreeChange(before, after)
		require.True(t, changed)
		return nonEmptyLines(git("diff", "--name-only", "--no-renames", from, to))
	}
	git("init", "-q")
	write("a.go", "package a\n")
	write(".gitignore", "node_modules/\n")
	git("add", "-A")
	git("commit", "-q", "-m", "base")
	out, err := exec.Command("/bin/sh", "-c", excludeKuayleScript, "kuayle-exclude", root).CombinedOutput()
	require.NoError(t, err, string(out))
	// The worktree already holds uncommitted work of an earlier step.
	write("pending.go", "package a // wip\n")
	write("node_modules/x/index.js", "x")

	before := state()
	require.Equal(t, before, state(), "reading the state changes nothing")
	write(".kuayle/outbox/m.json", "{}")
	write("node_modules/x/more.js", "x")
	require.Equal(t, before, state(), "Kuayle's files and work inside an ignored directory are not the agent's changes")
	changed, _, _ := worktreeChange(before, state())
	require.False(t, changed)
	require.Empty(t, strings.TrimSpace(git("diff", "--cached", "--name-only")), "the checkout's index is untouched")

	write("a.go", "package a // edited\n")
	edited := state()
	require.Equal(t, []string{"a.go"}, files(before, edited))
	write("a.go", "package a\n")
	require.Equal(t, before, state())

	write("docs/plan.md", "# Plan\n")
	created := state()
	require.Equal(t, []string{"docs/plan.md"}, files(before, created), "an untracked file counts")
	git("add", "docs/plan.md")
	require.Equal(t, created, state(), "staging it changes nothing more")
	require.NoError(t, os.Remove(filepath.Join(root, "docs/plan.md")))
	git("rm", "-q", "--cached", "docs/plan.md")
	require.Equal(t, before, state())

	// Committing what was pending leaves the files alone but is a change.
	git("add", "pending.go")
	git("commit", "-q", "-m", "wip")
	committed := state()
	changed, from, to := worktreeChange(before, committed)
	require.True(t, changed)
	require.Equal(t, from, to, "same files, new commit")

	// Hiding a new file from git does not hide it from Kuayle: neither a
	// pattern the agent adds to the repository's exclude file, nor a
	// directory that ignores itself.
	hidden := state()
	exclude := filepath.Join(root, ".git", "info", "exclude")
	old, err := os.ReadFile(exclude)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(exclude, append(old, []byte("\nsecret.go\n")...), 0o644))
	write("secret.go", "package a // hidden\n")
	changed, _, _ = worktreeChange(hidden, state())
	require.True(t, changed, "a file excluded during the run is still a change")
	require.NoError(t, os.Remove(filepath.Join(root, "secret.go")))
	require.Equal(t, hidden, state(), "the pattern alone changes nothing that could be published")
	write("stash/.gitignore", "*\n")
	write("stash/impl.go", "package stash\n")
	changed, _, _ = worktreeChange(hidden, state())
	require.True(t, changed, "a directory that ignores itself is still a change")
	require.NoError(t, os.RemoveAll(filepath.Join(root, "stash")))
	write("node_modules/x/other.js", "y")
	require.Equal(t, hidden, state(), "work inside a directory that was ignored before is not")

	// Kuayle's own files never count, also when the repository does not
	// exclude .kuayle/ (its outbox preparation failed).
	bare := t.TempDir()
	root = bare
	git("init", "-q")
	write("a.go", "package a\n")
	git("add", "-A")
	git("commit", "-q", "-m", "base")
	clean := state()
	write(".kuayle/outbox/m.json", "{}")
	write(".kuayle/validation.json", "{}")
	require.Equal(t, clean, state())
	write("b.go", "package a\n")
	require.Equal(t, []string{"b.go"}, files(clean, state()))

	require.Empty(t, parseWorktreeState("fatal: not a git repository\n"))
	require.Empty(t, parseWorktreeState(""))
}

type worktreeRuntimeFake struct {
	*runtimeFake
	states []string
	files  []string
}

func (f *worktreeRuntimeFake) WorktreeState(context.Context, *domain.DevMachine, []domain.DevMachineService, *domain.DevMachineCheckout) (string, error) {
	if len(f.states) == 0 {
		return "", errors.New("developer container is unavailable")
	}
	state := f.states[0]
	f.states = f.states[1:]
	return state, nil
}

func (f *worktreeRuntimeFake) WorktreeChangedFiles(context.Context, []domain.DevMachineService, *domain.DevMachineCheckout, string, string) ([]string, error) {
	return f.files, nil
}

// The run records whether its agent changed the worktree, which files, or
// that it could not tell; an interactive run is never compared.
func TestRunRecordsWhetherTheAgentChangedTheWorktree(t *testing.T) {
	const clean, edited, committed, hid = "aaaa 1111 eeee", "aaaa 2222 eeee", "bbbb 1111 eeee", "aaaa 1111 ffff"
	record := func(mode string, files []string, states ...string) *domain.DevMachineAgentRun {
		runtime := &worktreeRuntimeFake{runtimeFake: &runtimeFake{}, states: states, files: files}
		m := &Manager{runtime: runtime}
		run := &domain.DevMachineAgentRun{ID: uuid.New(), Mode: mode}
		before := m.worktreeBefore(context.Background(), &domain.DevMachine{}, run, nil, nil)
		m.recordWorktreeChange(context.Background(), &domain.DevMachine{}, run, nil, nil, before)
		return run
	}
	run := record("autonomous", nil, clean, clean)
	require.NotNil(t, run.WorktreeChanged)
	require.False(t, *run.WorktreeChanged)
	require.JSONEq(t, `[]`, string(run.WorktreeChangedFiles))

	run = record("autonomous", []string{"a.go", "b.go"}, clean, edited)
	require.True(t, *run.WorktreeChanged)
	require.JSONEq(t, `["a.go","b.go"]`, string(run.WorktreeChangedFiles))

	run = record("autonomous", []string{"never listed"}, clean, committed)
	require.True(t, *run.WorktreeChanged, "a commit with the same files is a change")
	require.JSONEq(t, `[]`, string(run.WorktreeChangedFiles))

	run = record("autonomous", []string{"never listed"}, clean, hid)
	require.True(t, *run.WorktreeChanged, "new files that git ignores are a change")
	require.JSONEq(t, `["files that git ignores"]`, string(run.WorktreeChangedFiles))

	require.Nil(t, record("autonomous", nil, clean).WorktreeChanged, "unknown when the worktree cannot be read afterwards")
	require.Nil(t, record("autonomous", nil).WorktreeChanged, "or before")
	require.Nil(t, record("interactive", nil, clean, edited).WorktreeChanged)
	plain := &Manager{runtime: &runtimeFake{}}
	other := &domain.DevMachineAgentRun{Mode: "autonomous"}
	plain.recordWorktreeChange(context.Background(), &domain.DevMachine{}, other, nil, nil, plain.worktreeBefore(context.Background(), &domain.DevMachine{}, other, nil, nil))
	require.Nil(t, other.WorktreeChanged, "a runtime that cannot fingerprint leaves it unknown")
}
