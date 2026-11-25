package machine

import (
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/kuayle/kuayle-backend/internal/domain"
)

// The tree and show scans read one commit of a task checkout with git, never
// its worktree (KUA-280): untracked, ignored, staged and edited files are not
// part of a commit's tree, so a .env beside the code is never listed or read.
// Only the commit's objects are read, so no path can lead out of the
// checkout. Like the other scans they run under the publication preamble's
// throwaway git directory, where HEAD is the task branch.

// worktreeTreeScript prints the commit, then git ls-tree's NUL-separated
// entries of every file it tracks.
const worktreeTreeScript = `set -eu
path=$1 branch=$2 step=scan ref=$3
` + publishCheckoutScript + `commit=$(g rev-parse --verify --quiet "$ref^{commit}" 2>/dev/null) || { echo "commit is unavailable" >&2; exit 3; }
printf '%s\n' "$commit"
g ls-tree -r -z --full-tree "$commit"
`

// worktreeShowScript prints the commit and the file's size, then its bytes
// when it is at most max. The path must name a regular file of the commit:
// the blob <commit>:<path> resolves to, listed with a file mode. Directories,
// links and nested repositories are "missing".
const worktreeShowScript = `set -eu
path=$1 branch=$2 step=scan ref=$3 target=$4 max=$5
` + publishCheckoutScript + `export GIT_LITERAL_PATHSPECS=1
tab=$(printf '\t')
commit=$(g rev-parse --verify --quiet "$ref^{commit}" 2>/dev/null) || { echo "commit is unavailable" >&2; exit 3; }
printf '%s\n' "$commit"
blob=$(g rev-parse --verify --quiet "$commit:$target" 2>/dev/null || true)
entry=$(g ls-tree --full-tree "$commit" -- "$target" 2>/dev/null | head -n 1 || true)
case $entry in
  "100644 blob $blob$tab"*|"100755 blob $blob$tab"*) ;;
  *) blob= ;;
esac
if [ -z "$blob" ]; then printf '@kuayle missing\n'; exit 0; fi
size=$(g cat-file -s "$blob")
printf '@kuayle size %s\n' "$size"
if [ "$size" -gt "$max" ]; then exit 0; fi
printf '@kuayle content\n'
g cat-file blob "$blob"
`

// worktreeFilesBytes bounds the content a show result keeps, as JSON encodes
// it, so the stored result stays within its 64 KiB without being trimmed.
// worktreeTreeBytes bounds a tree result's paths the same way within its
// 4 MiB (migration 210), each counted with 4 bytes of quotes and separator as
// jsonb stores it.
const (
	worktreeFilesBytes = 60 << 10
	worktreeTreeBytes  = domain.WorktreeTreeResultMax - 8<<10
)

// worktreeFilesCommand checks the ref and path again, as the scripts' git
// commands take them: an option-like or evaluated ref never reaches git.
func worktreeFilesCommand(job WorktreeScanJob) ([]string, error) {
	if job.Request.Ref != "HEAD" && !domain.ValidCommitID(job.Request.Ref) {
		return nil, errors.New("invalid commit")
	}
	if job.Request.Kind == domain.WorktreeScanTree {
		return []string{"sh", "-c", worktreeTreeScript, "kuayle-worktree-scan", job.Path, job.Branch, job.Request.Ref}, nil
	}
	if !domain.ValidTaskFilePath(job.Request.Path) {
		return nil, errors.New("invalid path")
	}
	return []string{"sh", "-c", worktreeShowScript, "kuayle-worktree-scan", job.Path, job.Branch, job.Request.Ref,
		job.Request.Path, strconv.Itoa(domain.TaskFileReadableMax)}, nil
}

// parseWorktreeCommit reads the commit line the tree and show scans start with.
func parseWorktreeCommit(output string) (string, string, error) {
	commit, body, ok := strings.Cut(output, "\n")
	if !ok || !domain.ValidCommitID(commit) {
		return "", "", errors.New("worktree scan returned no commit")
	}
	return commit, body, nil
}

// parseWorktreeTree keeps the regular files (modes 100644 and 100755) whose
// paths a task file can have, sorted, up to limit (TaskFilesMax when 0) and
// the stored bound. When the exec cut its output, the last entry may be
// partial and is dropped.
func parseWorktreeTree(output string, limit int, truncated bool) (*domain.WorktreeScanResult, error) {
	commit, body, err := parseWorktreeCommit(output)
	if err != nil {
		return nil, err
	}
	entries := strings.Split(body, "\x00")
	if truncated {
		entries = entries[:len(entries)-1]
	}
	files := []string{}
	for _, entry := range entries {
		meta, name, ok := strings.Cut(entry, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 3 || (fields[0] != "100644" && fields[0] != "100755") || fields[1] != "blob" || !domain.ValidTaskFilePath(name) {
			continue
		}
		files = append(files, name)
	}
	sort.Strings(files)
	if limit <= 0 || limit > domain.TaskFilesMax {
		limit = domain.TaskFilesMax
	}
	used := 0
	for i, name := range files {
		used += domain.JSONTextLen([]byte(name)) + 4 // quotes, comma and space
		if i >= limit || used > worktreeTreeBytes {
			files, truncated = files[:i], true
			break
		}
	}
	return &domain.WorktreeScanResult{Commit: commit, Files: files, Truncated: truncated}, nil
}

// parseWorktreeShow returns the requested page of the file, or says it is
// missing, binary or too large. A file must arrive whole.
func parseWorktreeShow(req domain.WorktreeScanRequest, output string, truncated bool) (*domain.WorktreeScanResult, error) {
	commit, body, err := parseWorktreeCommit(output)
	if err != nil {
		return nil, err
	}
	result := &domain.WorktreeScanResult{Commit: commit, StartLine: req.StartLine}
	if body == "@kuayle missing\n" {
		result.Missing = true
		return result, nil
	}
	sizeLine, rest, _ := strings.Cut(body, "\n")
	sizeText, ok := strings.CutPrefix(sizeLine, "@kuayle size ")
	size, err := strconv.Atoi(sizeText)
	if !ok || err != nil || size < 0 {
		return nil, errors.New("worktree scan returned no file size")
	}
	if size > domain.TaskFileReadableMax {
		result.TooLarge = true
		return result, nil
	}
	data, ok := strings.CutPrefix(rest, "@kuayle content\n")
	if !ok || truncated || len(data) != size {
		return nil, errors.New("worktree scan did not return the whole file")
	}
	if domain.IsBinaryContent([]byte(data)) {
		result.Binary = true
		return result, nil
	}
	result.Content, result.EndLine, result.TotalLines, result.Truncated = domain.TaskFilePage([]byte(data), req.StartLine, req.EndLine, worktreeFilesBytes)
	return result, nil
}
