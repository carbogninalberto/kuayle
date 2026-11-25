package service

import (
	"testing"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// Without a lead, the board goes to the writer holding the write lock
// (KUA-289): with several writers, the one whose turn is next on the
// task's worktree plans the merge order, not merely the first in the
// roster.
func TestTickPostsTheBoardToTheWriterHoldingTheWriteLock(t *testing.T) {
	w := newTickWorld(t, withSecondWriter)
	w.agent = func(role string, turn int, task domain.AgentTask) tickTurn {
		switch role {
		case "implementer":
			return tickTurn{commit: sha(1), final: "Added the toggle."}
		case "frontend":
			return tickTurn{commit: sha(2), final: "Styled the toggle."}
		}
		return tickTurn{final: "Verdict: approve\nFine."}
	}
	w.drain()
	_, err := w.f.tasks.Repo.PostRoomMessage(w.f.ctx, w.f.ws, w.root, domain.TaskRoomPost{Kind: domain.RoomAsk, ToRoles: []string{"frontend"}, Body: "Can you style the toggle?"})
	require.NoError(t, err)
	w.drain()
	require.Equal(t, 1, w.runtime.turns["frontend"])
	require.Equal(t, "frontend", w.holder())

	b := w.board(`["ui/toggle.ts"]`)
	w.boardTask(b, 2, 13, `["ui/toggle.ts"]`)
	w.pass()
	board := w.facts(domain.RoomFactBoard)
	require.Len(t, board, 1)
	require.Equal(t, []string{"frontend"}, []string(board[0].ToRoles), "the writer holding the lock gets it")
}
