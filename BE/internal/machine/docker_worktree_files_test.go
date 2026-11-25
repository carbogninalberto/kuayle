package machine

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// stdout runs a scan script as the manager does and keeps its stdout only,
// untrimmed: a file's bytes are part of it.
func (f *scriptFixture) stdout(body string, args ...string) (string, error) {
	command := exec.Command("sh", append([]string{"-c", body, "kuayle-worktree-scan"}, args...)...)
	command.Env = append(os.Environ(), "PATH="+f.wrapper+":"+os.Getenv("PATH"), "HOME="+f.root)
	output, err := command.Output()
	return string(output), err
}

func (f *scriptFixture) tree(ref string) *domain.WorktreeScanResult {
	f.t.Helper()
	job := WorktreeScanJob{Request: domain.WorktreeScanRequest{Kind: domain.WorktreeScanTree, Ref: ref}, Path: f.checkout, Branch: "kuayle/rec-1"}
	command, err := worktreeScanCommand(job)
	require.NoError(f.t, err)
	output, err := f.stdout(command[2], command[4:]...)
	require.NoError(f.t, err, output)
	result, err := parseWorktreeScan(job, output, false)
	require.NoError(f.t, err)
	return result
}

func (f *scriptFixture) treeWithin(ref string, limit int) *domain.WorktreeScanResult {
	f.t.Helper()
	job := WorktreeScanJob{Request: domain.WorktreeScanRequest{Kind: domain.WorktreeScanTree, Ref: ref, Limit: limit}, Path: f.checkout, Branch: "kuayle/rec-1"}
	command, err := worktreeScanCommand(job)
	require.NoError(f.t, err)
	output, err := f.stdout(command[2], command[4:]...)
	require.NoError(f.t, err, output)
	result, err := parseWorktreeScan(job, output, false)
	require.NoError(f.t, err)
	return result
}

func (f *scriptFixture) show(ref, name string, start, end int) *domain.WorktreeScanResult {
	f.t.Helper()
	job := WorktreeScanJob{Request: domain.WorktreeScanRequest{Kind: domain.WorktreeScanShow, Ref: ref, Path: name, StartLine: start, EndLine: end}, Path: f.checkout, Branch: "kuayle/rec-1"}
	command, err := worktreeScanCommand(job)
	require.NoError(f.t, err)
	output, err := f.stdout(command[2], command[4:]...)
	require.NoError(f.t, err, output)
	result, err := parseWorktreeScan(job, output, false)
	require.NoError(f.t, err)
	return result
}

// The files of a commit are the files it tracks. The worktree's ignored
// .env, an untracked file, a staged file and an uncommitted edit are never
// listed or read; links, directories and nested repositories are not files;
// and an earlier commit shows its own tree.
func TestWorktreeTreeAndShowReadOnlyTheFilesACommitTracks(t *testing.T) {
	f := newScriptFixture(t)
	first := f.head()
	f.writeAndCommit(f.checkout, ".gitignore", ".env\n", 0o644, nil)
	require.NoError(t, os.MkdirAll(filepath.Join(f.checkout, "src"), 0o755))
	f.writeAndCommit(f.checkout, "src/app.go", "package app\n", 0o644, nil)
	outside := filepath.Join(f.root, "outside.txt")
	require.NoError(t, os.WriteFile(outside, []byte("outside\n"), 0o644))
	require.NoError(t, os.Symlink(outside, filepath.Join(f.checkout, "link")))
	f.run(f.checkout, nil, "add", "link")
	f.run(f.checkout, nil, "commit", "-q", "-m", "link")
	commit := f.head()

	require.NoError(t, os.WriteFile(filepath.Join(f.checkout, ".env"), []byte("LOCAL_ONLY=1\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(f.checkout, "notes.txt"), []byte("untracked\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(f.checkout, "staged.txt"), []byte("staged\n"), 0o644))
	f.run(f.checkout, nil, "add", "staged.txt")
	require.NoError(t, os.WriteFile(filepath.Join(f.checkout, "src", "app.go"), []byte("package app // edited\n"), 0o644))

	listed := f.tree("HEAD")
	require.Equal(t, commit, listed.Commit)
	require.Equal(t, []string{".gitignore", "README.md", "src/app.go"}, listed.Files)
	require.False(t, listed.Truncated)

	for _, name := range []string{".env", "notes.txt", "staged.txt", "link", "src", "missing.txt"} {
		shown := f.show("HEAD", name, 1, 100)
		require.True(t, shown.Missing, name)
		require.Empty(t, shown.Content, name)
	}
	shown := f.show("HEAD", "src/app.go", 1, 100)
	require.False(t, shown.Missing)
	require.Equal(t, commit, shown.Commit)
	require.Equal(t, "package app", shown.Content, "the committed file, not the edited worktree")
	require.Equal(t, 1, shown.TotalLines)

	earlier := f.tree(first)
	require.Equal(t, first, earlier.Commit)
	require.Equal(t, []string{"README.md"}, earlier.Files)
	require.True(t, f.show(first, "src/app.go", 1, 100).Missing)

	// A commit the checkout does not have is an error, not an empty tree.
	job := WorktreeScanJob{Request: domain.WorktreeScanRequest{Kind: domain.WorktreeScanTree, Ref: strings.Repeat("f", 40)}, Path: f.checkout, Branch: "kuayle/rec-1"}
	command, err := worktreeScanCommand(job)
	require.NoError(t, err)
	_, err = f.stdout(command[2], command[4:]...)
	require.Error(t, err)
}

// A shown file is paged by lines, a binary file carries no bytes, and a file
// over the readable size is named too large.
func TestWorktreeShowPagesTextAndWithholdsBinaryAndLargeFiles(t *testing.T) {
	f := newScriptFixture(t)
	var lines []string
	for i := 1; i <= 10; i++ {
		lines = append(lines, "line "+strconv.Itoa(i))
	}
	f.writeAndCommit(f.checkout, "ten.txt", strings.Join(lines, "\n")+"\n", 0o644, nil)
	f.writeAndCommit(f.checkout, "image.bin", "PNG\x00\x01\x02", 0o644, nil)
	f.writeAndCommit(f.checkout, "large.txt", strings.Repeat("0123456789abcdef\n", domain.TaskFileReadableMax/16), 0o644, nil)

	page := f.show("HEAD", "ten.txt", 3, 4)
	require.Equal(t, "line 3\nline 4", page.Content)
	require.Equal(t, 3, page.StartLine)
	require.Equal(t, 4, page.EndLine)
	require.Equal(t, 10, page.TotalLines)
	require.True(t, page.Truncated)

	binary := f.show("HEAD", "image.bin", 1, 100)
	require.True(t, binary.Binary)
	require.Empty(t, binary.Content)

	large := f.show("HEAD", "large.txt", 1, 100)
	require.True(t, large.TooLarge)
	require.Empty(t, large.Content)
}

// A listing larger than the 64 KiB every scan result had is kept whole up to
// the workspace's limit (migration 210): a commit of 2,000 files lists them
// all, and a limit of 500 stops there and says so.
func TestWorktreeTreeListsMoreThanTheFormerBoundUpToTheLimit(t *testing.T) {
	f := newScriptFixture(t)
	dir := filepath.Join(f.checkout, "src", "a-fairly-long-directory-name")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	for i := range 2000 {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "file-"+strconv.Itoa(i)+".go"), []byte("package x\n"), 0o644))
	}
	f.run(f.checkout, nil, "add", "src")
	f.run(f.checkout, nil, "commit", "-q", "-m", "many")

	listed := f.treeWithin("HEAD", domain.TaskFilesMax)
	require.Len(t, listed.Files, 2001, "every file and the README")
	require.False(t, listed.Truncated)
	payload, err := json.Marshal(listed)
	require.NoError(t, err)
	require.Greater(t, len(payload), worktreeScanResultMax, "more than a scan result held before")
	stored, err := marshalWorktreeScanResult(listed, worktreeScanResultBound(domain.WorktreeScanTree))
	require.NoError(t, err)
	require.Equal(t, payload, []byte(stored), "stored whole, not trimmed")

	limited := f.treeWithin("HEAD", 500)
	require.Len(t, limited.Files, 500)
	require.True(t, limited.Truncated)
	require.Equal(t, listed.Files[:500], limited.Files)
}

// Every result fits its stored bound: a tree result stops at its limit
// (30,000 by default) or at 4 MiB, whichever comes first, and a page of a
// file stays within the 64 KiB the other kinds keep.
func TestWorktreeTreeAndShowResultsFitTheStoredBound(t *testing.T) {
	commit := strings.Repeat("a", 40)
	tree := func(count int, name func(int) string) string {
		var output strings.Builder
		output.WriteString(commit + "\n")
		for i := range count {
			output.WriteString("100644 blob " + strings.Repeat("b", 40) + "\t" + name(i) + "\x00")
		}
		output.WriteString("120000 blob " + strings.Repeat("c", 40) + "\tlink\x00")
		output.WriteString("160000 commit " + strings.Repeat("d", 40) + "\tvendor/nested\x00")
		return output.String()
	}
	parse := func(output string, limit int) *domain.WorktreeScanResult {
		t.Helper()
		result, err := parseWorktreeScan(WorktreeScanJob{Request: domain.WorktreeScanRequest{Kind: domain.WorktreeScanTree, Ref: "HEAD", Limit: limit}}, output, false)
		require.NoError(t, err)
		payload, err := json.Marshal(result)
		require.NoError(t, err)
		require.LessOrEqual(t, len(payload), domain.WorktreeTreeResultMax)
		return result
	}
	short := func(i int) string { return "src/file-" + strconv.Itoa(i) + ".go" }
	result := parse(tree(domain.TaskFilesMax+10, short), 0)
	require.Len(t, result.Files, domain.TaskFilesMax, "no limit is the most a listing holds")
	require.True(t, result.Truncated)
	require.NotContains(t, result.Files, "link")
	require.NotContains(t, result.Files, "vendor/nested")
	result = parse(tree(100, short), 0)
	require.Len(t, result.Files, 100)
	require.False(t, result.Truncated)
	long := func(i int) string { return "src/" + strings.Repeat("deep/", 40) + "file-" + strconv.Itoa(i) + ".go" }
	result = parse(tree(domain.TaskFilesMax, long), 0)
	require.True(t, result.Truncated, "paths over 200 bytes long fill 4 MiB before 30,000")
	require.Greater(t, len(result.Files), 15000)

	text := strings.Repeat("\"quoted\" <markup> line\n", 20000)
	show := commit + "\n@kuayle size " + strconv.Itoa(len(text)) + "\n@kuayle content\n" + text
	page, err := parseWorktreeScan(WorktreeScanJob{Request: domain.WorktreeScanRequest{Kind: domain.WorktreeScanShow, Ref: "HEAD", Path: "a.txt", StartLine: 1, EndLine: domain.TaskFileLinesMax}}, show, false)
	require.NoError(t, err)
	require.True(t, page.Truncated)
	require.Positive(t, page.EndLine)
	payload, err := json.Marshal(page)
	require.NoError(t, err)
	require.LessOrEqual(t, len(payload), worktreeScanResultMax)
	require.Equal(t, worktreeScanResultMax, worktreeScanResultBound(domain.WorktreeScanShow))
	stored, err := marshalWorktreeScanResult(page, worktreeScanResultBound(domain.WorktreeScanShow))
	require.NoError(t, err)
	require.Equal(t, payload, []byte(stored), "the page needs no trimming that would cut a line")

	// Output cut short by the exec's cap is not a whole file.
	_, err = parseWorktreeScan(WorktreeScanJob{Request: domain.WorktreeScanRequest{Kind: domain.WorktreeScanShow, Ref: "HEAD", Path: "a.txt", StartLine: 1, EndLine: 10}}, show[:len(show)-5], true)
	require.Error(t, err)
}

// The runtime checks a scan's commit and path again before git sees them.
func TestWorktreeTreeAndShowRefuseAnOptionLikeRefOrAnEscapingPath(t *testing.T) {
	for _, req := range []domain.WorktreeScanRequest{
		{Kind: domain.WorktreeScanTree, Ref: "--output=/tmp/x"},
		{Kind: domain.WorktreeScanTree, Ref: "HEAD~1"},
		{Kind: domain.WorktreeScanShow, Ref: "HEAD", Path: "../outside.txt", StartLine: 1, EndLine: 1},
		{Kind: domain.WorktreeScanShow, Ref: "HEAD", Path: "/etc/passwd", StartLine: 1, EndLine: 1},
	} {
		_, err := worktreeScanCommand(WorktreeScanJob{Request: req, Path: "/workspace/tasks/rec-1", Branch: "kuayle/rec-1"})
		require.Error(t, err, req)
	}
}
