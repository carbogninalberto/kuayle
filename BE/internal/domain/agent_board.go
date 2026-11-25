package domain

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

// AgentBoardTask is one active implementing task of a repository on the
// board (team workflows, KUA-262): where it stands, what its branch changes
// and where it is in the merge order. Everything comes from Kuayle's own
// records, never a live GitHub read, so it can lag GitHub; how far the branch
// is behind its base is not recorded and is left to the task's facts.
type AgentBoardTask struct {
	IssueID         uuid.UUID `json:"-"`
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

// boardRank is a task's group in the merge order: approved with CI passing,
// approved, any other pull request, and no pull request (not in line).
func boardRank(task AgentBoardTask) int {
	switch {
	case task.PullRequest <= 0:
		return 3
	case task.ApprovedAt == nil:
		return 2
	case task.CI != "passing":
		return 1
	}
	return 0
}

// ArrangeBoard sorts the tasks into merge order and derives each one's
// position and overlap. Approved tasks go by approval time, earliest first;
// other pull requests by number, oldest first. Overlap is computed on every
// changed file before the lists are cut for the response.
func ArrangeBoard(tasks []AgentBoardTask) []AgentBoardTask {
	slices.SortStableFunc(tasks, func(a, b AgentBoardTask) int {
		if order := cmp.Compare(boardRank(a), boardRank(b)); order != 0 {
			return order
		}
		if a.ApprovedAt != nil && b.ApprovedAt != nil {
			if order := a.ApprovedAt.Compare(*b.ApprovedAt); order != 0 {
				return order
			}
		}
		return cmp.Or(cmp.Compare(a.PullRequest, b.PullRequest), strings.Compare(a.IssueIdentifier, b.IssueIdentifier))
	})
	files := make([]map[string]bool, len(tasks))
	for i := range tasks {
		if tasks[i].PullRequest > 0 {
			tasks[i].MergePosition = i + 1
		}
		files[i] = make(map[string]bool, len(tasks[i].ChangedFiles))
		for _, file := range tasks[i].ChangedFiles {
			files[i][file] = true
		}
	}
	for i := range tasks {
		tasks[i].Overlap = []AgentBoardOverlap{}
		for j, other := range tasks {
			if i == j {
				continue
			}
			var shared []string
			for file := range files[i] {
				if files[j][file] {
					shared = append(shared, file)
				}
			}
			if len(shared) == 0 {
				continue
			}
			slices.Sort(shared)
			tasks[i].Overlap = append(tasks[i].Overlap, AgentBoardOverlap{
				TaskID: other.TaskID, IssueIdentifier: other.IssueIdentifier, PullRequest: other.PullRequest,
				Files: shared[:min(len(shared), MaxAgentBoardOverlapFiles)], FileCount: len(shared),
				// Sorted, so a task in line before this one merges first.
				Ahead: other.MergePosition > 0 && j < i,
			})
		}
	}
	for i := range tasks {
		tasks[i].ChangedFileCount = len(tasks[i].ChangedFiles)
		tasks[i].ChangedFiles = tasks[i].ChangedFiles[:min(len(tasks[i].ChangedFiles), MaxAgentBoardFiles)]
		if tasks[i].ChangedFiles == nil {
			tasks[i].ChangedFiles = []string{}
		}
	}
	return tasks
}

// SharedChanges names the overlapping issue, its PR and up to three shared
// paths. The board in a prompt and the overlap fact use the same description.
func (o AgentBoardOverlap) SharedChanges() string {
	name := o.IssueIdentifier
	if o.PullRequest > 0 {
		name += fmt.Sprintf(" (pull request #%d)", o.PullRequest)
	}
	files := o.Files[:min(len(o.Files), 3)]
	list := strings.Join(files, ", ")
	if rest := o.FileCount - len(files); rest > 0 {
		list += fmt.Sprintf(" and %d more file(s)", rest)
	}
	return name + " also changes " + list
}
