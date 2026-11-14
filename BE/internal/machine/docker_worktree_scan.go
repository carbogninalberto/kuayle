package machine

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	pathpkg "path"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/client"
)

// The scan scripts run in the developer container. Their command lines are
// fixed; the request reaches them only as positional arguments, so a pattern
// or path is never parsed as shell syntax. Every git command goes through the
// publication preamble's g(), which never loads the agent-writable checkout's
// configuration, hooks, fsmonitor or attribute programs, and rg runs with
// --no-config so no configuration file can add a preprocessor (--pre).
const worktreeScanHeader = `printf '%s\n' "$branch"
printf '%s\n' "$(g rev-parse --verify HEAD 2>/dev/null || true)"
changed=$(g status --porcelain=v1 --untracked-files=normal 2>/dev/null || true)
case $changed in
  "") printf '0\n' ;;
  *) printf '1\n' ;;
esac
`

const worktreeGrepScript = `set -eu
path=$1 branch=$2 step=scan fixed=$3 ignorecase=$4 pattern=$5
` + publishCheckoutScript + worktreeScanHeader + `cd -- "$path"
set -- rg --no-config --json --no-messages --max-columns 300 --max-columns-preview --max-filesize 4M
if [ "$fixed" = 1 ]; then set -- "$@" -F; fi
if [ "$ignorecase" = 1 ]; then set -- "$@" -i; fi
set -- "$@" -e "$pattern" -- .
set +e
"$@"
status=$?
set -e
[ "$status" -le 1 ] || exit "$status"
`

// rg's -g pattern is an override that includes a file even when .gitignore
// excludes it, so the glob is applied in Go after rg has respected the ignore
// files.
const worktreeGlobScript = `set -eu
path=$1 branch=$2 step=scan
` + publishCheckoutScript + worktreeScanHeader + `cd -- "$path"
command -v rg >/dev/null 2>&1 || { echo "rg is unavailable" >&2; exit 1; }
rg --no-config --files --no-messages -- . > "$work/files" || exit $?
LC_ALL=C sort "$work/files"
`

const worktreeReadScript = `set -eu
path=$1 branch=$2 step=scan target=$3 start=$4 end=$5
` + publishCheckoutScript + worktreeScanHeader + `root=$(cd -P -- "$path" && pwd -P)
resolved=$(readlink -f -- "$path/$target" 2>/dev/null || true)
if [ -z "$resolved" ]; then resolved=$(realpath -- "$path/$target" 2>/dev/null || true); fi
if [ -z "$resolved" ]; then echo "file is unavailable" >&2; exit 3; fi
case $resolved in
  "$root"/*) ;;
  *) echo "path is outside the checkout" >&2; exit 3 ;;
esac
if [ ! -f "$resolved" ]; then echo "not a regular file" >&2; exit 3; fi
size=$(wc -c < "$resolved" | tr -d ' ')
if [ "$size" -gt 4194304 ]; then echo "file is too large" >&2; exit 3; fi
total=$(awk 'END{print NR}' "$resolved")
printf '@kuayle total %s\n' "$total"
printf '@kuayle content\n'
awk -v start="$start" -v end="$end" 'NR>=start && NR<=end {print}' "$resolved"
`

const worktreeStatusScript = `set -eu
path=$1 branch=$2 step=scan base=$3
` + publishCheckoutScript + `changed=$(g status --porcelain=v1 --untracked-files=normal 2>/dev/null || true)
printf '%s\n' "$branch"
printf '%s\n' "$(g rev-parse --verify HEAD 2>/dev/null || true)"
case $changed in
  "") printf '0\n' ;;
  *) printf '1\n' ;;
esac
printf '%s\n' "$changed"
if g rev-parse -q --verify "refs/remotes/origin/$base^{commit}" >/dev/null 2>&1; then
  printf '@kuayle ahead-behind %s\n' "$(g rev-list --left-right --count "refs/remotes/origin/$base...HEAD" 2>/dev/null || true)"
fi
printf '@kuayle subject %s\n' "$(g log -1 --format=%s 2>/dev/null || true)"
`

const worktreeListScript = `set -eu
root=$1
step=list
count=0
for dir in "$root"/*; do
  [ -d "$dir" ] || continue
  count=$((count + 1))
  if [ "$count" -gt 100 ]; then printf '@kuayle truncated\n'; break; fi
  path=$dir
  branch=
  headline=
  gitdir=$dir/.git
  if [ -f "$gitdir" ]; then
    link=$(head -n 1 "$gitdir" 2>/dev/null || true)
    case $link in
      "gitdir: "*) gitdir=${link#gitdir: } ;;
      *) gitdir= ;;
    esac
    case $gitdir in
      "") ;;
      /*) ;;
      *) gitdir=$dir/$gitdir ;;
    esac
  fi
  if [ -n "$gitdir" ] && [ -f "$gitdir/HEAD" ]; then headline=$(head -n 1 "$gitdir/HEAD" 2>/dev/null || true); fi
  case $headline in
    "ref: refs/heads/"*) branch=${headline#ref: refs/heads/} ;;
  esac
  (
` + publishCheckoutScript + `
    if [ -z "$branch" ]; then
      case $headline in
        [0-9a-f]*) printf '%s\n' "$headline" > "$work/checkout/HEAD" ;;
      esac
    fi
    commit=$(g rev-parse --verify HEAD 2>/dev/null || true)
    changed=$(g status --porcelain=v1 --untracked-files=normal 2>/dev/null || true)
    case $changed in
      "") dirty=0 ;;
      *) dirty=1 ;;
    esac
    mtime=$(stat -c %Y -- "$path" 2>/dev/null || stat -f %m -- "$path" 2>/dev/null || printf '0')
    printf '%s\t%s\t%s\t%s\t%s\n' "$path" "$branch" "$commit" "$dirty" "$mtime"
  ) || continue
done
`

// worktreeScanKill stops a scan in the container shortly after the exec's own
// deadline; detaching alone would leave a long rg running there.
var worktreeScanKill = []string{"timeout", "-s", "KILL", "12"}

// ScanWorktree reads one worktree with rg and git in the developer container.
// The whole exec has a fixed deadline and its stdout a fixed cap; a scan that
// reaches either reports Truncated.
func (r *DockerRuntime) ScanWorktree(ctx context.Context, _ *domain.DevMachine, services []domain.DevMachineService, job WorktreeScanJob) (*domain.WorktreeScanResult, error) {
	developerContainer := developerContainerID(services)
	if developerContainer == "" {
		return nil, fmt.Errorf("developer container is unavailable")
	}
	if !strings.HasPrefix(job.Path, "/workspace") || strings.ContainsRune(job.Path, 0) {
		return nil, fmt.Errorf("invalid worktree path")
	}
	command, err := worktreeScanCommand(job)
	if err != nil {
		return nil, err
	}
	scanCtx, cancel := context.WithTimeout(ctx, domain.WorktreeScanTimeout)
	defer cancel()
	output, truncated, err := r.execWorktreeScan(scanCtx, developerContainer, append(append([]string{}, worktreeScanKill...), command...))
	if err != nil {
		return nil, err
	}
	return parseWorktreeScan(job, output, truncated)
}

func worktreeScanCommand(job WorktreeScanJob) ([]string, error) {
	switch job.Request.Kind {
	case domain.WorktreeScanGrep:
		return []string{"sh", "-c", worktreeGrepScript, "kuayle-worktree-scan", job.Path, job.Branch,
			scanBool(job.Request.Fixed), scanBool(job.Request.CaseInsensitive), job.Request.Pattern}, nil
	case domain.WorktreeScanGlob:
		return []string{"sh", "-c", worktreeGlobScript, "kuayle-worktree-scan", job.Path, job.Branch}, nil
	case domain.WorktreeScanRead:
		return []string{"sh", "-c", worktreeReadScript, "kuayle-worktree-scan", job.Path, job.Branch,
			job.Request.Path, strconv.Itoa(job.Request.StartLine), strconv.Itoa(job.Request.EndLine)}, nil
	case domain.WorktreeScanStatus:
		return []string{"sh", "-c", worktreeStatusScript, "kuayle-worktree-scan", job.Path, job.Branch, job.BaseBranch}, nil
	case domain.WorktreeScanList:
		return []string{"sh", "-c", worktreeListScript, "kuayle-worktree-scan", job.Path}, nil
	default:
		return nil, fmt.Errorf("unsupported worktree scan kind")
	}
}

func scanBool(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

func (r *DockerRuntime) execWorktreeScan(ctx context.Context, containerID string, command []string) (string, bool, error) {
	created, err := r.client.ExecCreate(ctx, containerID, client.ExecCreateOptions{
		Cmd: command, User: "1000:1000", AttachStdin: true, AttachStdout: true, AttachStderr: true,
	})
	if err != nil {
		return "", false, err
	}
	attached, err := r.client.ExecAttach(ctx, created.ID, client.ExecAttachOptions{})
	if err != nil {
		return "", false, err
	}
	defer attached.Close()
	defer context.AfterFunc(ctx, attached.Close)()
	if err := attached.CloseWrite(); err != nil {
		return "", false, err
	}
	stdout := newCappedBuffer(domain.WorktreeScanOutputMax)
	stderr := newCappedBuffer(64 << 10)
	if _, err := stdcopy.StdCopy(&stdout, &stderr, attached.Reader); err != nil && !errors.Is(err, io.EOF) {
		return "", stdout.truncated, err
	}
	inspection, err := r.client.ExecInspect(ctx, created.ID, client.ExecInspectOptions{})
	if err != nil {
		return "", stdout.truncated, err
	}
	if inspection.ExitCode != 0 {
		return "", stdout.truncated, fmt.Errorf("worktree scan failed with exit code %d: %s", inspection.ExitCode, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), stdout.truncated, nil
}

type worktreeScanHeaderValues struct {
	Branch string
	Commit string
	Dirty  bool
	Body   string
}

func parseWorktreeHeader(output string) (worktreeScanHeaderValues, error) {
	parts := strings.SplitN(output, "\n", 4)
	if len(parts) < 3 {
		return worktreeScanHeaderValues{}, fmt.Errorf("worktree scan returned an incomplete header")
	}
	header := worktreeScanHeaderValues{Branch: parts[0], Commit: parts[1], Dirty: parts[2] == "1"}
	if len(parts) == 4 {
		header.Body = parts[3]
	}
	return header, nil
}

func parseWorktreeScan(job WorktreeScanJob, output string, truncated bool) (*domain.WorktreeScanResult, error) {
	if job.Request.Kind == domain.WorktreeScanList {
		worktrees, more := parseWorktreeList(output, domain.WorktreeScanMaxList)
		return &domain.WorktreeScanResult{Worktrees: worktrees, Truncated: truncated || more}, nil
	}
	header, err := parseWorktreeHeader(output)
	if err != nil {
		return nil, err
	}
	result := &domain.WorktreeScanResult{Branch: header.Branch, Commit: header.Commit, Dirty: header.Dirty}
	switch job.Request.Kind {
	case domain.WorktreeScanGrep:
		result.Matches, result.Truncated = parseRgJSON(header.Body, job.Request.Glob, worktreeScanLimit(job.Request.Limit, 50, domain.WorktreeScanMaxMatches))
		result.Truncated = result.Truncated || truncated
	case domain.WorktreeScanGlob:
		result.Files, result.Truncated = parseRgFiles(header.Body, job.Request.Glob, worktreeScanLimit(job.Request.Limit, 200, domain.WorktreeScanMaxFiles))
		result.Truncated = result.Truncated || truncated
	case domain.WorktreeScanRead:
		result.Content, result.TotalLines, result.EndLine, result.Truncated = parseReadOutput(header.Body, job.Request.StartLine, job.Request.EndLine)
		result.StartLine = job.Request.StartLine
		result.Truncated = result.Truncated || truncated
	case domain.WorktreeScanStatus:
		result.Changed, result.Ahead, result.Behind, result.LastSubject, result.Truncated = parseStatusOutput(header.Body, domain.WorktreeScanMaxChanged)
		result.Truncated = result.Truncated || truncated
	default:
		return nil, fmt.Errorf("unsupported worktree scan kind")
	}
	return result, nil
}

func worktreeScanLimit(requested, fallback, maximum int) int {
	if requested <= 0 {
		return fallback
	}
	return min(requested, maximum)
}

type rgPath struct {
	Text  *string `json:"text"`
	Bytes *string `json:"bytes"`
}

func (p rgPath) value() string {
	if p.Text != nil {
		return *p.Text
	}
	if p.Bytes != nil {
		if decoded, err := base64.StdEncoding.DecodeString(*p.Bytes); err == nil {
			return string(decoded)
		}
	}
	return ""
}

// parseRgJSON reads rg's JSON events. A malformed or cut trailing line is
// skipped; the exec cap already marks that output as truncated. The glob is
// applied after rg, which respected the checkout's ignore files.
func parseRgJSON(body, glob string, limit int) ([]domain.WorktreeMatch, bool) {
	matches := []domain.WorktreeMatch{}
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event struct {
			Type string `json:"type"`
			Data struct {
				Path  rgPath `json:"path"`
				Lines struct {
					Text *string `json:"text"`
				} `json:"lines"`
				LineNumber int `json:"line_number"`
			} `json:"data"`
		}
		if json.Unmarshal([]byte(line), &event) != nil || event.Type != "match" {
			continue
		}
		path := scanRelativePath(event.Data.Path.value())
		if !worktreeGlobMatches(glob, path) {
			continue
		}
		if len(matches) >= limit {
			return matches, true
		}
		text := ""
		if event.Data.Lines.Text != nil {
			text = strings.TrimSuffix(*event.Data.Lines.Text, "\n")
		}
		matches = append(matches, domain.WorktreeMatch{
			Path: path,
			Line: event.Data.LineNumber,
			Text: scanLine(text),
		})
	}
	return matches, false
}

func parseRgFiles(body, glob string, limit int) ([]string, bool) {
	files := []string{}
	for _, line := range strings.Split(body, "\n") {
		line = scanRelativePath(strings.TrimSpace(line))
		if line == "" || !worktreeGlobMatches(glob, line) {
			continue
		}
		if len(files) >= limit {
			return files, true
		}
		files = append(files, line)
	}
	return files, false
}

// worktreeGlobMatches applies rg's glob shape: a pattern without a slash
// matches the basename at any depth, and ** spans directories.
func worktreeGlobMatches(pattern, path string) bool {
	if pattern == "" {
		return true
	}
	if !strings.Contains(pattern, "/") {
		return worktreeGlobSegments([]string{pattern}, []string{pathpkg.Base(path)})
	}
	return worktreeGlobSegments(strings.Split(pattern, "/"), strings.Split(path, "/"))
}

func worktreeGlobSegments(pattern, path []string) bool {
	if len(pattern) == 0 {
		return len(path) == 0
	}
	if pattern[0] == "**" {
		for i := 0; i <= len(path); i++ {
			if worktreeGlobSegments(pattern[1:], path[i:]) {
				return true
			}
		}
		return false
	}
	if len(path) == 0 {
		return false
	}
	matched, err := pathpkg.Match(pattern[0], path[0])
	if err != nil || !matched {
		return false
	}
	return worktreeGlobSegments(pattern[1:], path[1:])
}

func scanRelativePath(path string) string {
	return strings.TrimPrefix(path, "./")
}

func scanLine(line string) string {
	return truncateRunes(line, domain.WorktreeScanLineRunes)
}

func truncateRunes(value string, maximum int) string {
	if utf8.RuneCountInString(value) <= maximum {
		return value
	}
	return string([]rune(value)[:maximum])
}

// parseReadOutput reads the total and content markers. The content is the rest
// of the output, so a line that looks like a marker inside the file is data.
func parseReadOutput(body string, start, end int) (string, int, int, bool) {
	total := 0
	contentStart := -1
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if rest, ok := strings.CutPrefix(line, "@kuayle total "); ok {
			total, _ = strconv.Atoi(strings.TrimSpace(rest))
		}
		if line == "@kuayle content" {
			contentStart = i + 1
			break
		}
	}
	if contentStart < 0 || contentStart >= len(lines) {
		return "", total, 0, end < total
	}
	contentLines := lines[contentStart:]
	if len(contentLines) > 0 && contentLines[len(contentLines)-1] == "" {
		contentLines = contentLines[:len(contentLines)-1]
	}
	var builder strings.Builder
	written, returned, truncated := 0, 0, end < total
	for _, line := range contentLines {
		if returned >= domain.WorktreeScanReadLines {
			truncated = true
			break
		}
		line = scanLine(line)
		needed := len(line)
		if written > 0 {
			needed++
		}
		if written+needed > domain.WorktreeScanReadBytes {
			truncated = true
			break
		}
		if written > 0 {
			builder.WriteByte('\n')
			written++
		}
		builder.WriteString(line)
		written += len(line)
		returned++
	}
	endLine := 0
	if returned > 0 {
		endLine = start + returned - 1
	}
	return builder.String(), total, endLine, truncated
}

func parseStatusOutput(body string, limit int) (changed []string, ahead, behind *int, subject string, truncated bool) {
	changed = []string{}
	seenMarker := false
	for _, line := range strings.Split(body, "\n") {
		if line == "" {
			continue
		}
		if rest, ok := strings.CutPrefix(line, "@kuayle ahead-behind "); ok {
			seenMarker = true
			fields := strings.Fields(rest)
			if len(fields) == 2 {
				if count, err := strconv.Atoi(fields[0]); err == nil {
					behind = &count
				}
				if count, err := strconv.Atoi(fields[1]); err == nil {
					ahead = &count
				}
			}
			continue
		}
		if rest, ok := strings.CutPrefix(line, "@kuayle subject "); ok {
			seenMarker = true
			subject = rest
			continue
		}
		if seenMarker {
			continue
		}
		if len(changed) >= limit {
			truncated = true
			continue
		}
		changed = append(changed, line)
	}
	return changed, ahead, behind, subject, truncated
}

func parseWorktreeList(body string, limit int) ([]domain.WorktreeSummary, bool) {
	worktrees := []domain.WorktreeSummary{}
	truncated := false
	for _, line := range strings.Split(body, "\n") {
		if line == "" {
			continue
		}
		if line == "@kuayle truncated" {
			truncated = true
			continue
		}
		fields := strings.SplitN(line, "\t", 5)
		if len(fields) != 5 {
			continue
		}
		if len(worktrees) >= limit {
			return worktrees, true
		}
		seconds, _ := strconv.ParseInt(fields[4], 10, 64)
		worktrees = append(worktrees, domain.WorktreeSummary{
			Path: fields[0], Branch: fields[1], Commit: fields[2], Dirty: fields[3] == "1",
			ModifiedAt: time.Unix(seconds, 0).UTC(),
		})
	}
	return worktrees, truncated
}
