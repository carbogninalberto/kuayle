package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// WorktreeScanKind is what a worktree scan reads on a dev machine. Scans are
// read-only: they run rg and git in the developer container without
// credentials and never change the checkout.
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
)

// DevMachineOpScanWorktree runs one worktree scan. It is read-only, so the
// manager runs it beside agent work on the same machine.
const DevMachineOpScanWorktree DevMachineOperationAction = "scan_worktree"

// Worktree scan limits, the same as the GitHub repository scans.
const (
	WorktreeScanTimeout    = 10 * time.Second
	WorktreeScanOutputMax  = 64 << 20 // bytes of rg/git output read
	WorktreeScanLineRunes  = 300      // a match or read line is cut here
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
	Kind            WorktreeScanKind `json:"kind"`
	Pattern         string           `json:"pattern,omitempty"`
	Glob            string           `json:"glob,omitempty"`
	Fixed           bool             `json:"fixed,omitempty"`
	CaseInsensitive bool             `json:"case_insensitive,omitempty"`
	Path            string           `json:"path,omitempty"`
	StartLine       int              `json:"start_line,omitempty"`
	EndLine         int              `json:"end_line,omitempty"`
	Limit           int              `json:"limit,omitempty"`
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

// WorktreeScanTarget names what a scan reads: a task's checkout (TaskID), or
// with only MachineID one of the requester's own machines, which only a list
// scan accepts.
type WorktreeScanTarget struct {
	TaskID    *uuid.UUID
	MachineID *uuid.UUID
}
