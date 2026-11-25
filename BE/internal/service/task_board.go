package service

import (
	"context"
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
