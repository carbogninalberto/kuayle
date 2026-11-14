package machine

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func requireRipgrep(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg is not on PATH")
	}
}

func (f *scriptFixture) scan(body string, args ...string) (string, error) {
	return f.script(body, "", "kuayle-worktree-scan", args...)
}

func (f *scriptFixture) grep(pattern string) (*domain.WorktreeScanResult, error) {
	output, err := f.scan(worktreeGrepScript, f.checkout, "kuayle/rec-1", "0", "0", pattern)
	if err != nil {
		return nil, err
	}
	return parseWorktreeScan(WorktreeScanJob{Request: domain.WorktreeScanRequest{Kind: domain.WorktreeScanGrep, Pattern: pattern}}, output, false)
}

func TestWorktreeGrepFindsUncommittedTextAndReportsDirty(t *testing.T) {
	requireRipgrep(t)
	f := newScriptFixture(t)
	require.NoError(t, os.MkdirAll(filepath.Join(f.checkout, "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(f.checkout, "src", "app.go"), []byte("package main\n// needle here\n"), 0o644))

	result, err := f.grep("needle")
	require.NoError(t, err)
	require.Equal(t, "kuayle/rec-1", result.Branch)
	require.NotEmpty(t, result.Commit)
	require.True(t, result.Dirty, "an untracked file makes the worktree dirty")
	require.Len(t, result.Matches, 1)
	require.Equal(t, "src/app.go", result.Matches[0].Path)
	require.Equal(t, 2, result.Matches[0].Line)
	require.Contains(t, result.Matches[0].Text, "needle here")
	require.False(t, result.Truncated)
}

func TestWorktreeGrepAndGlobSkipGitIgnoredFiles(t *testing.T) {
	requireRipgrep(t)
	f := newScriptFixture(t)
	require.NoError(t, os.WriteFile(filepath.Join(f.checkout, ".gitignore"), []byte("secret.txt\nbuild/\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(f.checkout, "visible.txt"), []byte("needle visible\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(f.checkout, "secret.txt"), []byte("needle hidden\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(f.checkout, "build"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(f.checkout, "build", "out.txt"), []byte("needle hidden\n"), 0o644))

	result, err := f.grep("needle")
	require.NoError(t, err)
	require.Len(t, result.Matches, 1)
	require.Equal(t, "visible.txt", result.Matches[0].Path)

	output, err := f.scan(worktreeGlobScript, f.checkout, "kuayle/rec-1")
	require.NoError(t, err)
	files, err := parseWorktreeScan(WorktreeScanJob{Request: domain.WorktreeScanRequest{Kind: domain.WorktreeScanGlob, Glob: "*.txt"}}, output, false)
	require.NoError(t, err)
	require.Equal(t, []string{"visible.txt"}, files.Files)
}

func TestWorktreeReadRefusesEscapingPathsAndSymlinks(t *testing.T) {
	f := newScriptFixture(t)
	outside := filepath.Join(f.root, "outside.txt")
	require.NoError(t, os.WriteFile(outside, []byte("outside\n"), 0o644))

	output, err := f.scan(worktreeReadScript, f.checkout, "kuayle/rec-1", "../outside.txt", "1", "1")
	require.Error(t, err, output)
	require.Contains(t, output, "outside the checkout")

	require.NoError(t, os.Symlink(outside, filepath.Join(f.checkout, "escape.txt")))
	output, err = f.scan(worktreeReadScript, f.checkout, "kuayle/rec-1", "escape.txt", "1", "1")
	require.Error(t, err, output)
	require.Contains(t, output, "outside the checkout")

	require.NoError(t, os.MkdirAll(filepath.Join(f.checkout, "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(f.checkout, "src", "read.txt"), []byte("one\ntwo\nthree\n"), 0o644))
	output, err = f.scan(worktreeReadScript, f.checkout, "kuayle/rec-1", "src/read.txt", "2", "3")
	require.NoError(t, err, output)
	result, err := parseWorktreeScan(WorktreeScanJob{Request: domain.WorktreeScanRequest{Kind: domain.WorktreeScanRead, Path: "src/read.txt", StartLine: 2, EndLine: 3}}, output, false)
	require.NoError(t, err)
	require.Equal(t, "two\nthree", result.Content)
	require.Equal(t, 3, result.TotalLines)
	require.Equal(t, 2, result.StartLine)
	require.Equal(t, 3, result.EndLine)
	require.False(t, result.Truncated)
}

func TestWorktreeStatusReportsChangesAndDivergence(t *testing.T) {
	f := newScriptFixture(t)
	local := f.commitFile(f.checkout, "feature.txt", fixtureIdentity)
	require.NoError(t, os.WriteFile(filepath.Join(f.checkout, "README.md"), []byte("changed\n"), 0o644))

	maintainer := filepath.Join(f.root, "maintainer")
	f.run(f.root, nil, "clone", "-q", f.origin, maintainer)
	f.commitFile(maintainer, "base.txt", fixtureIdentity)
	f.run(maintainer, nil, "push", "-q", f.origin, "main")
	f.run(f.checkout, nil, "fetch", "-q", "origin")

	output, err := f.scan(worktreeStatusScript, f.checkout, "kuayle/rec-1", "main")
	require.NoError(t, err, output)
	result, err := parseWorktreeScan(WorktreeScanJob{Request: domain.WorktreeScanRequest{Kind: domain.WorktreeScanStatus}, BaseBranch: "main"}, output, false)
	require.NoError(t, err)
	require.Equal(t, "kuayle/rec-1", result.Branch)
	require.Equal(t, local, result.Commit)
	require.True(t, result.Dirty)
	require.Contains(t, result.Changed, " M README.md")
	require.Equal(t, "feature.txt", result.LastSubject)
	require.NotNil(t, result.Ahead)
	require.Equal(t, 1, *result.Ahead)
	require.NotNil(t, result.Behind)
	require.Equal(t, 1, *result.Behind)
}

func TestWorktreeScanNeverRunsACheckoutsConfiguredPrograms(t *testing.T) {
	f := newScriptFixture(t)
	marker := filepath.Join(f.root, "configured-program-ran")
	program := filepath.Join(f.root, "configured-program.sh")
	require.NoError(t, os.WriteFile(program, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o755))
	f.run(f.checkout, nil, "config", "core.fsmonitor", program)
	hooks := filepath.Join(f.root, "hooks")
	require.NoError(t, os.MkdirAll(hooks, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(hooks, "post-checkout"), []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o755))
	f.run(f.checkout, nil, "config", "core.hooksPath", hooks)
	require.NoError(t, os.WriteFile(filepath.Join(f.checkout, ".gitattributes"), []byte("* filter=spy\n"), 0o644))
	f.run(f.checkout, nil, "config", "filter.spy.smudge", program)

	output, err := f.scan(worktreeStatusScript, f.checkout, "kuayle/rec-1", "main")
	require.NoError(t, err, output)
	_, err = os.Stat(marker)
	require.True(t, os.IsNotExist(err), "a program named by the checkout's configuration ran")
}

func TestWorktreeListReportsEachTaskWorktree(t *testing.T) {
	f := newScriptFixture(t)
	output, err := f.scan(worktreeListScript, f.root)
	require.NoError(t, err, output)
	result, err := parseWorktreeScan(WorktreeScanJob{Request: domain.WorktreeScanRequest{Kind: domain.WorktreeScanList}}, output, false)
	require.NoError(t, err)
	require.NotEmpty(t, result.Worktrees)
	var found *domain.WorktreeSummary
	for i := range result.Worktrees {
		if result.Worktrees[i].Path == f.checkout {
			found = &result.Worktrees[i]
		}
	}
	require.NotNil(t, found, "the checkout is listed")
	require.Equal(t, "kuayle/rec-1", found.Branch)
	require.NotEmpty(t, found.Commit)
	require.False(t, found.ModifiedAt.IsZero())
}

func TestParseReadOutputCapsLines(t *testing.T) {
	var body strings.Builder
	body.WriteString("@kuayle total 500\n@kuayle content\n")
	for range 500 {
		body.WriteString("line\n")
	}
	content, total, endLine, truncated := parseReadOutput(body.String(), 1, 500)
	require.True(t, truncated)
	require.Equal(t, 500, total)
	require.Equal(t, domain.WorktreeScanReadLines, endLine)
	require.Len(t, strings.Split(content, "\n"), domain.WorktreeScanReadLines)
}

func TestParseRgJSONStopsAtTheLimitAndCutsLongLines(t *testing.T) {
	body := strings.Join([]string{
		`{"type":"begin","data":{}}`,
		`{"type":"match","data":{"path":{"text":"./a.go"},"lines":{"text":"` + strings.Repeat("x", 400) + `\n"},"line_number":3}}`,
		`not json`,
		`{"type":"match","data":{"path":{"text":"./b.go"},"lines":{"text":"second\n"},"line_number":8}}`,
		`{"type":"match","data":{"path":{"text":"./c.go"},"lines":{"text":"third\n"},"line_number":9}}`,
	}, "\n")
	matches, truncated := parseRgJSON(body, "", 2)
	require.True(t, truncated)
	require.Len(t, matches, 2)
	require.Equal(t, "a.go", matches[0].Path)
	require.Equal(t, 3, matches[0].Line)
	require.Len(t, []rune(matches[0].Text), domain.WorktreeScanLineRunes)
	require.Equal(t, "b.go", matches[1].Path)
}

// An rg configuration file can name a preprocessor (--pre) that rg runs on
// every file; a scan must never load one, whatever the environment says.
func TestWorktreeGrepIgnoresRipgrepConfiguration(t *testing.T) {
	requireRipgrep(t)
	f := newScriptFixture(t)
	marker := filepath.Join(f.root, "preprocessor-ran")
	preprocessor := filepath.Join(f.root, "pre.sh")
	require.NoError(t, os.WriteFile(preprocessor, []byte("#!/bin/sh\ntouch '"+marker+"'\ncat \"$1\"\n"), 0o755))
	config := filepath.Join(f.root, "ripgreprc")
	require.NoError(t, os.WriteFile(config, []byte("--pre="+preprocessor+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(f.checkout, "app.txt"), []byte("needle\n"), 0o644))
	t.Setenv("RIPGREP_CONFIG_PATH", config)

	result, err := f.grep("needle")
	require.NoError(t, err)
	require.Len(t, result.Matches, 1)
	_, statErr := os.Stat(marker)
	require.True(t, os.IsNotExist(statErr), "the configured preprocessor must not run")
}
