package service

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// The board (team workflows, KUA-262) shows a repository's active tasks side
// by side: which merges first, and which of them change the same files. It
// is derived here, without I/O, from the rows the repository reads; a task's
// overlap then reaches its facts through boardOverlap.

// Board lists the active tasks on a repository that GET /agent-tasks would
// show the actor, in merge order, each with the tasks it overlaps.
func (s *AgentTaskService) Board(ctx context.Context, ws uuid.UUID, repository string) ([]domain.AgentBoardTask, error) {
	tasks, err := s.Repo.Board(ctx, ws, repository)
	if err != nil {
		return nil, err
	}
	return arrangeBoard(tasks), nil
}

// boardRank is a task's group in the merge order: approved with CI passing,
// approved, any other pull request, and no pull request (not in line).
func boardRank(task domain.AgentBoardTask) int {
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

// arrangeBoard sorts the tasks into merge order and derives each one's
// position and overlap. Approved tasks go by approval time, earliest first;
// other pull requests by number, oldest first. Overlap is computed on every
// changed file before the lists are cut for the response.
func arrangeBoard(tasks []domain.AgentBoardTask) []domain.AgentBoardTask {
	slices.SortStableFunc(tasks, func(a, b domain.AgentBoardTask) int {
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
		tasks[i].Overlap = []domain.AgentBoardOverlap{}
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
			tasks[i].Overlap = append(tasks[i].Overlap, domain.AgentBoardOverlap{
				TaskID: other.TaskID, IssueIdentifier: other.IssueIdentifier, PullRequest: other.PullRequest,
				Files: shared[:min(len(shared), domain.MaxAgentBoardOverlapFiles)], FileCount: len(shared),
				// Sorted, so a task in line before this one merges first.
				Ahead: other.MergePosition > 0 && j < i,
			})
		}
	}
	for i := range tasks {
		tasks[i].ChangedFileCount = len(tasks[i].ChangedFiles)
		tasks[i].ChangedFiles = tasks[i].ChangedFiles[:min(len(tasks[i].ChangedFiles), domain.MaxAgentBoardFiles)]
		if tasks[i].ChangedFiles == nil {
			tasks[i].ChangedFiles = []string{}
		}
	}
	return tasks
}

// boardOverlap is a task's overlap on an arranged board, for its facts:
// empty, not nil, when the board does not list the task, because the board
// was read.
func boardOverlap(board []domain.AgentBoardTask, task uuid.UUID) []domain.AgentBoardOverlap {
	for _, entry := range board {
		if entry.TaskID == task {
			return entry.Overlap
		}
	}
	return []domain.AgentBoardOverlap{}
}

// overlapReason says which tasks ahead in the merge order change the same
// files, or "" when none does.
func overlapReason(overlap []domain.AgentBoardOverlap, base string) string {
	var ahead []string
	for _, other := range overlap {
		if !other.Ahead {
			continue
		}
		name := other.IssueIdentifier
		if other.PullRequest > 0 {
			name += fmt.Sprintf(" (pull request #%d)", other.PullRequest)
		}
		ahead = append(ahead, name+" also changes "+fileList(other.Files, other.FileCount)+" and is ahead of you in the merge order")
	}
	if len(ahead) == 0 {
		return ""
	}
	them := "it"
	if len(ahead) > 1 {
		them = "them"
	}
	return truncateRunes(strings.Join(ahead, "; "), 900) + ". Next step: let " + them + " merge first, then bring the branch up to date with " + base + " and resolve any conflicts."
}

// fileList names up to three files and counts the rest.
func fileList(files []string, count int) string {
	const named = 3
	if len(files) > named {
		files = files[:named]
	}
	list := strings.Join(files, ", ")
	if rest := count - len(files); rest > 0 {
		list += fmt.Sprintf(" and %d more file(s)", rest)
	}
	return list
}
