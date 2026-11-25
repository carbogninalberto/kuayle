package domain

import (
	"bytes"
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// A task's files at a commit (KUA-280): the files one commit of the task's
// branch tracks, read for the task's creator. The listing is the commit's
// tree, never the worktree, so untracked and ignored files (a .env) are
// never listed or served.
const (
	// TaskFilesMax bounds a listing, as the repository scans do; it is the
	// default and the largest value of a workspace's setting (its Dev Machine
	// policy's task_files_max, migration 210), and MinTaskFilesMax the
	// smallest.
	TaskFilesMax    = 30000
	MinTaskFilesMax = 100
	// WorktreeTreeResultMax bounds a stored tree scan result (migration 210):
	// 30,000 paths averaging about 135 bytes. Other scan results keep 64 KiB.
	WorktreeTreeResultMax = 4 << 20
	// TaskFileLinesMax and TaskFileBytesMax bound one page of a file; bytes
	// are counted as the JSON answer encodes them.
	TaskFileLinesMax = 5000
	TaskFileBytesMax = 512 << 10
	// TaskFileReadableMax: a larger file is listed, but its content is not
	// served (the repository scans' per-file limit).
	TaskFileReadableMax = 2 << 20
	// taskFileSniffBytes: a NUL among a file's first bytes makes it binary.
	taskFileSniffBytes = 8000
)

// Entry kinds of a directory listing: a subdirectory (a git tree) or a
// tracked regular file (a blob).
const (
	TreeEntryTree = "tree"
	TreeEntryBlob = "blob"
)

const (
	// TaskFileSearchMax bounds the paths one search answers: a filter is
	// refined, not scrolled.
	TaskFileSearchMax = 500
	// TaskFileQueryMax bounds a search text, in bytes.
	TaskFileQueryMax = 200
)

// TreeEntry is one entry of a git tree: the name in its directory, its kind,
// its object id (the tree's or blob's SHA) and a blob's size in bytes. It is
// what is cached by tree SHA, so it names nothing about a commit or a path.
type TreeEntry struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	SHA  string `json:"sha,omitempty"`
	Size *int64 `json:"size,omitempty"`
}

// TaskFileEntry is one entry of a directory a commit tracks, or one file a
// search found (Name is then the file's own name and Path locates it).
// Changed: the task changed the file, by the files its newest attempt that
// recorded any reported, or, for a directory, a file below it.
type TaskFileEntry struct {
	TreeEntry
	Path    string `json:"path"`
	Changed bool   `json:"changed"`
}

// TaskFiles is one directory of the files a commit of a task tracks (Path,
// "" for the root; Tree its git tree SHA when known), directories first and
// each group by name, or, when Query is set, the files whose path contains it.
// Truncated: the listing stops at a bound and later entries are missing.
// Matches is a search's count of all matching files.
type TaskFiles struct {
	Commit    string          `json:"commit"`
	Path      string          `json:"path"`
	Tree      string          `json:"tree,omitempty"`
	Query     string          `json:"query,omitempty"`
	Entries   []TaskFileEntry `json:"entries"`
	Matches   int             `json:"matches,omitempty"`
	Truncated bool            `json:"truncated"`
}

// TaskFileContent is lines Start to End of one tracked file at Commit.
// Truncated: the file goes on after End (read on from End+1), or a line was
// cut. A binary or too large file carries no content.
type TaskFileContent struct {
	Path       string `json:"path"`
	Commit     string `json:"commit"`
	Content    string `json:"content"`
	Start      int    `json:"start"`
	End        int    `json:"end"`
	TotalLines int    `json:"total_lines"`
	Truncated  bool   `json:"truncated"`
	Binary     bool   `json:"binary"`
	TooLarge   bool   `json:"too_large"`
}

// TaskFileSource is what Kuayle recorded about where a task's files are:
// the repository and head it published ("" when nothing is), whether a
// requested commit is one it published or one of the task's attempts
// recorded, the files the task changed and the workspace's listing limit.
type TaskFileSource struct {
	Repository   string
	Head         string
	RefPublished bool
	RefRecorded  bool
	Changed      []string
	// FilesMax is how many files the workspace lets a listing hold.
	FilesMax int
}

var commitID = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)

// ValidCommitID reports a full, lower-case SHA-1 or SHA-256 commit id.
func ValidCommitID(value string) bool { return commitID.MatchString(value) }

// ValidTaskFilePath reports a path a commit's tree can name exactly: clean,
// relative, valid UTF-8, without control characters, backslashes, "." or
// ".." segments or a .git directory. Other paths are neither listed nor read.
func ValidTaskFilePath(name string) bool {
	if name == "" || len(name) > 1024 || !utf8.ValidString(name) || strings.HasPrefix(name, "/") || path.Clean(name) != name {
		return false
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f || r == '\\' {
			return false
		}
	}
	for _, segment := range strings.Split(name, "/") {
		if segment == "." || segment == ".." || segment == ".git" {
			return false
		}
	}
	return true
}

// IsBinaryContent sniffs a file as the repository scans do: a NUL byte among
// its first 8,000 bytes.
func IsBinaryContent(data []byte) bool {
	return bytes.IndexByte(data[:min(len(data), taskFileSniffBytes)], 0) >= 0
}

// TaskFilePage returns lines start to end (1-based, inclusive; end 0 reads
// on) of a text file: at most TaskFileLinesMax lines and maxBytes bytes as
// JSON encodes the content. A first line longer than that is cut on a
// character boundary. last is the last line returned (0 for none), total the
// file's lines, truncated whether the file goes on after the page or a line
// was cut.
func TaskFilePage(data []byte, start, end, maxBytes int) (content string, last, total int, truncated bool) {
	total = bytes.Count(data, []byte{'\n'})
	if len(data) > 0 && data[len(data)-1] != '\n' {
		total++
	}
	start = max(start, 1)
	if limit := start + TaskFileLinesMax - 1; end <= 0 || end > limit {
		end = limit
	}
	var page strings.Builder
	used, line, rest := 0, 0, data
	for len(rest) > 0 && line < end {
		line++
		text := rest
		if i := bytes.IndexByte(rest, '\n'); i >= 0 {
			text, rest = rest[:i], rest[i+1:]
		} else {
			rest = nil
		}
		if line < start {
			continue
		}
		cost := JSONTextLen(text)
		if last > 0 {
			cost += 2 // the newline before the line
		}
		if used+cost > maxBytes {
			if last == 0 {
				page.Write(jsonTextPrefix(text, maxBytes))
				last = line
			}
			return page.String(), last, total, true
		}
		if last > 0 {
			page.WriteByte('\n')
		}
		page.Write(text)
		used += cost
		last = line
	}
	return page.String(), last, total, last > 0 && last < total
}

// JSONTextLen is the length of text inside a JSON string as encoding/json
// writes it, or a little more: quotes, backslashes and \n, \r, \t take two
// bytes; other control characters, <, >, &, U+2028, U+2029 and each invalid
// UTF-8 byte take six.
func JSONTextLen(text []byte) int {
	n := 0
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRune(text[i:])
		n += jsonRuneLen(r, size)
		i += size
	}
	return n
}

func jsonRuneLen(r rune, size int) int {
	switch {
	case r == utf8.RuneError && size == 1:
		return 6
	case r == '"', r == '\\', r == '\n', r == '\r', r == '\t':
		return 2
	case r < 0x20, r == '<', r == '>', r == '&', r == ' ', r == ' ':
		return 6
	}
	return size
}

// jsonTextPrefix is the longest prefix of text, in whole characters, whose
// JSON encoding takes at most budget bytes.
func jsonTextPrefix(text []byte, budget int) []byte {
	used := 0
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRune(text[i:])
		cost := jsonRuneLen(r, size)
		if used+cost > budget {
			return text[:i]
		}
		used += cost
		i += size
	}
	return text
}

// ValidTaskDirPath reports a directory a listing may name: "" (the root) or
// a path ValidTaskFilePath accepts; one trailing slash is the caller's to trim.
func ValidTaskDirPath(name string) bool { return name == "" || ValidTaskFilePath(name) }

// SortTreeEntries orders a directory as its listing shows it: directories
// first, then files, each by name.
func SortTreeEntries(entries []TreeEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if (a.Kind == TreeEntryTree) != (b.Kind == TreeEntryTree) {
			return a.Kind == TreeEntryTree
		}
		return a.Name < b.Name
	})
}
