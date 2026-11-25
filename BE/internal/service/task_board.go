package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// The board (team workflows, KUA-262) shows a repository's active tasks side
// by side: which merges first, and which of them change the same files. It
// is arranged by domain.ArrangeBoard from the rows the repository reads; a task's
// overlap then reaches its facts through boardOverlap.

// Board lists the active tasks on a repository that GET /agent-tasks would
// show the actor, in merge order, each with the tasks it overlaps.
func (s *AgentTaskService) Board(ctx context.Context, ws uuid.UUID, repository string) ([]domain.AgentBoardTask, error) {
	tasks, err := s.Repo.Board(ctx, ws, repository)
	if err != nil {
		return nil, err
	}
	return domain.ArrangeBoard(tasks), nil
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
		ahead = append(ahead, other.SharedChanges()+" and is ahead of you in the merge order")
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

// boardRoom is a task's place on its repository's board as its room hears
// it (the board fact, P4.6): the other active tasks that change the same
// files, in merge order, each ahead of the task or not, with the files they
// share. Tasks that share nothing are left out, so their moves post nothing.
// It names at most boardRoomTasks tasks and boardRoomFiles files of each
// (More counts the tasks left out), which keeps the post within the room's
// bounds.
type boardRoom struct {
	Overlap []boardRoomTask `json:"overlap"`
	More    int             `json:"more,omitempty"`
}

type boardRoomTask struct {
	Issue       string   `json:"issue"`
	PullRequest int      `json:"pull_request,omitempty"`
	Ahead       bool     `json:"ahead"`
	Files       []string `json:"files"`
	FileCount   int      `json:"file_count"`
}

const (
	boardRoomTasks     = 10
	boardRoomFiles     = 5
	boardRoomPathBytes = 200
)

func newBoardRoom(overlap []domain.AgentBoardOverlap) boardRoom {
	room := boardRoom{Overlap: []boardRoomTask{}, More: max(len(overlap)-boardRoomTasks, 0)}
	for _, other := range overlap[:min(len(overlap), boardRoomTasks)] {
		files := []string{}
		for _, file := range other.Files[:min(len(other.Files), boardRoomFiles)] {
			files = append(files, truncateUTF8Bytes(file, boardRoomPathBytes))
		}
		room.Overlap = append(room.Overlap, boardRoomTask{Issue: other.IssueIdentifier, PullRequest: other.PullRequest, Ahead: other.Ahead, Files: files, FileCount: other.FileCount})
	}
	return room
}

// text says the board fact in words.
func (b boardRoom) text(repository string) string {
	if len(b.Overlap) == 0 {
		return "No other active task on " + repository + " changes the same files."
	}
	parts := make([]string, 0, len(b.Overlap)+1)
	for _, other := range b.Overlap {
		order := "has no pull request yet"
		switch {
		case other.Ahead:
			order = "is ahead of this task in the merge order"
		case other.PullRequest > 0:
			order = "comes after this task in the merge order"
		}
		shared := domain.AgentBoardOverlap{IssueIdentifier: other.Issue, PullRequest: other.PullRequest, Files: other.Files, FileCount: other.FileCount}
		parts = append(parts, shared.SharedChanges()+" and "+order)
	}
	if b.More > 0 {
		parts = append(parts, fmt.Sprintf("%d more task(s) change some of the same files", b.More))
	}
	return strings.Join(parts, "; ") + "."
}
