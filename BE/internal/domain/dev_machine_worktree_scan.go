package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// WorktreeScanKind selects a read-only checkout or machine Docker operation.
type WorktreeScanKind string

const (
	// WorktreeScanGrep searches a task checkout's files with rg.
	WorktreeScanGrep WorktreeScanKind = "grep"
	// WorktreeScanGlob lists a task checkout's files matching a glob.
	WorktreeScanGlob WorktreeScanKind = "glob"
	// WorktreeScanRead reads a line range of one file of a task checkout.
	WorktreeScanRead WorktreeScanKind = "read"
	// WorktreeScanStatus reports a task checkout's git state.
	WorktreeScanStatus WorktreeScanKind = "status"
	// WorktreeScanList lists the task worktrees under /workspace/tasks.
	WorktreeScanList WorktreeScanKind = "list"
	// WorktreeScanTree lists the files one commit of a task checkout
	// tracks, and WorktreeScanShow reads a line range of one of them. Both
	// read the commit with git, never the worktree (KUA-280).
	WorktreeScanTree WorktreeScanKind = "tree"
	WorktreeScanShow WorktreeScanKind = "show"
	// Machine-only Docker reads use the same bounded, read-only scan queue.
	MachineScanContainers    WorktreeScanKind = "containers"
	MachineScanContainerLogs WorktreeScanKind = "container_logs"
)

func (kind WorktreeScanKind) ReadsDocker() bool {
	return kind == MachineScanContainers || kind == MachineScanContainerLogs
}

// DevMachineOpScanWorktree runs one worktree scan. It is read-only, so the
// manager runs it beside agent work on the same machine.
const DevMachineOpScanWorktree DevMachineOperationAction = "scan_worktree"

// Worktree scan limits, the same as the GitHub repository scans.
const (
	WorktreeScanTimeout    = 10 * time.Second
	WorktreeScanOutputMax  = 8 << 20 // bytes of rg/git output read
	WorktreeScanLineRunes  = 300     // a match or read line is cut here
	WorktreeScanMaxMatches = 100
	WorktreeScanMaxFiles   = 500
	WorktreeScanReadLines  = 400
	WorktreeScanReadBytes  = 14 << 10
	WorktreeScanMaxChanged = 200 // changed paths reported by a status scan
	WorktreeScanMaxList    = 100 // worktrees reported by a list scan
)

// WorktreeScanRequest is what one scan reads. Only the fields of its kind are
// used; Path and Glob are relative to the checkout and never leave it.
type WorktreeScanRequest struct {
	Container       string           `json:"container,omitempty"`
	Kind            WorktreeScanKind `json:"kind"`
	Pattern         string           `json:"pattern,omitempty"`
	Glob            string           `json:"glob,omitempty"`
	Fixed           bool             `json:"fixed,omitempty"`
	CaseInsensitive bool             `json:"case_insensitive,omitempty"`
	Path            string           `json:"path,omitempty"`
	StartLine       int              `json:"start_line,omitempty"`
	EndLine         int              `json:"end_line,omitempty"`
	Limit           int              `json:"limit,omitempty"`
	// Ref is the commit a tree or show scan reads (HEAD, the task branch's
	// tip, or a full commit id). A tree scan lists the one directory Path
	// (the root when empty) without recursion (KUA-319); with a Pattern it
	// searches every path of the commit for that text instead.
	Ref string `json:"ref,omitempty"`
}

// WorktreeMatch is one rg match.
type WorktreeMatch struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

// WorktreeSummary is one worktree found by a list scan.
type WorktreeSummary struct {
	Path       string    `json:"path"`
	Branch     string    `json:"branch,omitempty"`
	Commit     string    `json:"commit,omitempty"`
	Dirty      bool      `json:"dirty"`
	ModifiedAt time.Time `json:"modified_at"`
}

// WorktreeScanResult is what the manager stores for a finished scan. Every
// scan of a checkout reports the branch, the HEAD commit and whether the
// worktree has uncommitted changes, so a transcript shows what was read.
type WorktreeScanResult struct {
	Branch      string            `json:"branch,omitempty"`
	Commit      string            `json:"commit,omitempty"`
	Dirty       bool              `json:"dirty"`
	Matches     []WorktreeMatch   `json:"matches,omitempty"`
	Files       []string          `json:"files,omitempty"`
	Entries     []TreeEntry       `json:"entries,omitempty"`      // a tree scan's directory
	Tree        string            `json:"tree,omitempty"`         // that directory's tree SHA
	FileMatches int               `json:"file_matches,omitempty"` // a tree search's matching files
	Content     string            `json:"content,omitempty"`
	StartLine   int               `json:"start_line,omitempty"`
	EndLine     int               `json:"end_line,omitempty"`
	TotalLines  int               `json:"total_lines,omitempty"`
	Changed     []string          `json:"changed,omitempty"` // git status --porcelain lines
	Ahead       *int              `json:"ahead,omitempty"`
	Behind      *int              `json:"behind,omitempty"`
	LastSubject string            `json:"last_subject,omitempty"`
	Worktrees   []WorktreeSummary `json:"worktrees,omitempty"`
	Truncated   bool              `json:"truncated"`
	// A show scan's file or a tree scan's directory: Missing means the
	// commit tracks nothing at that path; a Binary or TooLarge file carries no content.
	Missing  bool `json:"missing,omitempty"`
	Binary   bool `json:"binary,omitempty"`
	TooLarge bool `json:"too_large,omitempty"`
}

// WorktreeScan is one queued scan and, once the manager ran it, its result or
// error. Its ID is also its operation's ID.
type WorktreeScan struct {
	ID          uuid.UUID       `json:"id" db:"id"`
	WorkspaceID uuid.UUID       `json:"workspace_id" db:"workspace_id"`
	MachineID   uuid.UUID       `json:"machine_id" db:"machine_id"`
	CheckoutID  *uuid.UUID      `json:"checkout_id,omitempty" db:"checkout_id"`
	TaskID      *uuid.UUID      `json:"task_id,omitempty" db:"task_id"`
	RequestedBy uuid.UUID       `json:"requested_by" db:"requested_by"`
	Request     json.RawMessage `json:"request" db:"request"`
	Result      json.RawMessage `json:"result,omitempty" db:"result"`
	Error       *string         `json:"error,omitempty" db:"error"`
	CreatedAt   time.Time       `json:"created_at" db:"created_at"`
	CompletedAt *time.Time      `json:"completed_at,omitempty" db:"completed_at"`
	// Read with the scan for its result header: the machine's name, and for a
	// checkout scan its repository, working branch and path.
	MachineName   string `json:"machine_name" db:"machine_name"`
	Repository    string `json:"repository,omitempty" db:"repository"`
	WorkingBranch string `json:"working_branch,omitempty" db:"working_branch"`
	BaseBranch    string `json:"base_branch,omitempty" db:"base_branch"`
	WorkspacePath string `json:"workspace_path,omitempty" db:"workspace_path"`
}

// WorktreeScanTarget selects a task checkout, or an owned machine for a
// worktree listing or Docker read.
type WorktreeScanTarget struct {
	TaskID    *uuid.UUID
	MachineID *uuid.UUID
}
