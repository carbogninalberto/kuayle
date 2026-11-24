package domain

import (
	"time"

	"github.com/google/uuid"
)

// AgentBoardTask is one active implementing task of a repository on the
// board (team workflows, KUA-262): where it stands, what its branch changes
// and where it is in the merge order. Everything comes from Kuayle's own
// records, never a live GitHub read, so it can lag GitHub; how far the branch
// is behind its base is not recorded and is left to the task's facts.
type AgentBoardTask struct {
	TaskID          uuid.UUID `json:"task_id"`
	IssueIdentifier string    `json:"issue_identifier"`
	IssueTitle      string    `json:"issue_title"`
	State           string    `json:"state"`
	WorkflowNode    *string   `json:"workflow_node,omitempty"`
	WorkflowStatus  *string   `json:"workflow_status,omitempty"`
	Repository      string    `json:"repository"`
	// PullRequest is the newest pull request Kuayle published for the task;
	// 0 when none.
	PullRequest int `json:"pull_request,omitempty"`
	// Head is the head GitHub last reported for it, or the commit Kuayle
	// published when that is newer; Base is the checkout's base branch.
	Head string `json:"head,omitempty"`
	Base string `json:"base,omitempty"`
	// ChangedFiles are the files the branch changes against its base, as the
	// latest attempt that recorded any saw them, cut to
	// MaxAgentBoardFiles; ChangedFileCount counts them all.
	ChangedFiles     []string `json:"changed_files"`
	ChangedFileCount int      `json:"changed_file_count"`
	// CI is "failing" when a check suite reported on Head failed, "passing"
	// when at least one passed and none failed, and empty when none
	// reported. Suites still running report nothing, so passing is
	// approximate.
	CI string `json:"ci,omitempty"`
	// ApprovedAt is when a person approved the merge Kuayle is carrying out
	// now (a workflow step's merge, or an assistant merge waiting for
	// checks); nil when no approved merge is in progress.
	ApprovedAt *time.Time `json:"approved_at,omitempty"`
	UpdatedAt  time.Time  `json:"updated_at"`
	// MergePosition is the task's place in the merge order, from 1; 0 for a
	// task without a pull request. Overlap lists the other tasks on the
	// board whose branches change some of the same files.
	MergePosition int                 `json:"merge_position,omitempty"`
	Overlap       []AgentBoardOverlap `json:"overlap"`
}

// AgentBoardOverlap is another active task whose branch changes some of the
// same files.
type AgentBoardOverlap struct {
	TaskID          uuid.UUID `json:"task_id"`
	IssueIdentifier string    `json:"issue_identifier"`
	PullRequest     int       `json:"pull_request,omitempty"`
	// Files are the shared files, sorted and cut to MaxAgentBoardOverlapFiles;
	// FileCount counts them all.
	Files     []string `json:"files"`
	FileCount int      `json:"file_count"`
	// Ahead: the other task comes first in the merge order.
	Ahead bool `json:"ahead"`
}

// Bounds of the board's file lists.
const (
	MaxAgentBoardFiles        = 200
	MaxAgentBoardOverlapFiles = 20
)
