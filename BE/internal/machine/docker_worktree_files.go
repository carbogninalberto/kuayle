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

// worktreeTreeScript prints the commit, then, for one directory, its git
// tree's SHA and the entries git ls-tree lists in it (NUL-separated, with
// sizes) without recursing, or "@kuayle missing" when the commit has no
// directory there. A search ($search is 1) prints every tracked path instead
// and the parser filters them. The directory is read as <commit>:<dir>, so
// only the commit's objects are named and no path can lead out of the checkout.
const worktreeTreeScript = `set -eu
path=$1 branch=$2 step=scan ref=$3 dir=$4 search=$5
` + publishCheckoutScript + `commit=$(g rev-parse --verify --quiet "$ref^{commit}" 2>/dev/null) || { echo "commit is unavailable" >&2; exit 3; }
printf '%s\n' "$commit"
if [ "$search" = 1 ]; then
  g ls-tree -r -z --full-tree "$commit"
  exit 0
fi
spec="$commit^{tree}"
if [ -n "$dir" ]; then spec="$commit:$dir"; fi
tree=$(g rev-parse --verify --quiet "$spec" 2>/dev/null || true)
if [ -z "$tree" ] || [ "$(g cat-file -t "$tree" 2>/dev/null)" != tree ]; then printf '@kuayle missing\n'; exit 0; fi
printf '@kuayle tree %s\n' "$tree"
g ls-tree -z -l "$tree"
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
		if !domain.ValidTaskDirPath(job.Request.Path) || (job.Request.Pattern != "" && job.Request.Path != "") {
			return nil, errors.New("invalid path")
		}
		return []string{"sh", "-c", worktreeTreeScript, "kuayle-worktree-scan", job.Path, job.Branch, job.Request.Ref,
			job.Request.Path, scanBool(job.Request.Pattern != "")}, nil
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

// parseWorktreeTree reads a tree scan's output: a directory's entries, or,
// for a search, the matching files.
func parseWorktreeTree(req domain.WorktreeScanRequest, output string, truncated bool) (*domain.WorktreeScanResult, error) {
	commit, body, err := parseWorktreeCommit(output)
	if err != nil {
		return nil, err
	}
	limit := req.Limit
	if limit <= 0 || limit > domain.TaskFilesMax {
		limit = domain.TaskFilesMax
	}
	// When the exec cut its output, the last entry may be partial: drop it.
	split := func(text string) []string {
		entries := strings.Split(text, "\x00")
		if truncated {
			entries = entries[:len(entries)-1]
		}
		return entries
	}
	if req.Pattern != "" {
		return parseWorktreeSearch(commit, split(body), strings.ToLower(req.Pattern), limit, truncated), nil
	}
	if body == "@kuayle missing\n" {
		return &domain.WorktreeScanResult{Commit: commit, Missing: true}, nil
	}
	line, rest, _ := strings.Cut(body, "\n")
	tree, ok := strings.CutPrefix(line, "@kuayle tree ")
	if !ok || !domain.ValidCommitID(tree) {
		return nil, errors.New("worktree scan returned no tree")
	}
	entries := []domain.TreeEntry{}
	for _, entry := range split(rest) {
		meta, name, ok := strings.Cut(entry, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 4 || !domain.ValidCommitID(fields[2]) || !domain.ValidTaskFilePath(name) || strings.Contains(name, "/") {
			continue
		}
		switch {
		case fields[0] == "040000" && fields[1] == "tree":
			entries = append(entries, domain.TreeEntry{Name: name, Kind: domain.TreeEntryTree, SHA: fields[2]})
		case (fields[0] == "100644" || fields[0] == "100755") && fields[1] == "blob":
			item := domain.TreeEntry{Name: name, Kind: domain.TreeEntryBlob, SHA: fields[2]}
			if size, err := strconv.ParseInt(fields[3], 10, 64); err == nil && size >= 0 {
				item.Size = &size
			}
			entries = append(entries, item)
		}
	}
	domain.SortTreeEntries(entries)
	used := 0
	for i, entry := range entries {
		used += domain.JSONTextLen([]byte(entry.Name)) + len(entry.SHA) + worktreeEntryOverhead
		if i >= limit || used > worktreeTreeBytes {
			entries, truncated = entries[:i], true
			break
		}
	}
	return &domain.WorktreeScanResult{Commit: commit, Tree: tree, Entries: entries, Truncated: truncated}, nil
}

// worktreeEntryOverhead is what a directory entry's JSON takes besides its
// name and SHA: the keys, the kind, a size and the punctuation, counted
// generously as jsonb stores them.
const worktreeEntryOverhead = 72

// parseWorktreeSearch keeps the regular files (modes 100644 and 100755)
// whose paths a task file can have and contain pattern, sorted, up to limit
// and the stored bound, and counts all of them.
func parseWorktreeSearch(commit string, entries []string, pattern string, limit int, truncated bool) *domain.WorktreeScanResult {
	files := []string{}
	for _, entry := range entries {
		meta, name, ok := strings.Cut(entry, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 3 || (fields[0] != "100644" && fields[0] != "100755") || fields[1] != "blob" || !domain.ValidTaskFilePath(name) ||
			!strings.Contains(strings.ToLower(name), pattern) {
			continue
		}
		files = append(files, name)
	}
	sort.Strings(files)
	matches := len(files)
	used := 0
	for i, name := range files {
		used += domain.JSONTextLen([]byte(name)) + 4 // quotes, comma and space
		if i >= limit || used > worktreeTreeBytes {
			files, truncated = files[:i], true
			break
		}
	}
	return &domain.WorktreeScanResult{Commit: commit, Files: files, FileMatches: matches, Truncated: truncated}
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
