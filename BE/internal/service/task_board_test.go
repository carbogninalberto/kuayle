package service

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func boardTask(issue string, pr int, ci string, approved *time.Time, files ...string) domain.AgentBoardTask {
	return domain.AgentBoardTask{TaskID: uuid.New(), IssueIdentifier: issue, PullRequest: pr, CI: ci, ApprovedAt: approved, ChangedFiles: files, ChangedFileCount: len(files)}
}

func TestBoardMergeOrderPutsApprovedGreenTasksFirst(t *testing.T) {
	at := func(minutes int) *time.Time {
		when := time.Date(2026, 11, 24, 9, minutes, 0, 0, time.UTC)
		return &when
	}
	// Listed in the database's order (most recently updated first).
	board := arrangeBoard([]domain.AgentBoardTask{
		boardTask("KUA-6", 0, "", nil),
		boardTask("KUA-5", 2, "", nil),
		boardTask("KUA-4", 1, "passing", nil),
		boardTask("KUA-3", 4, "failing", at(0)),
		boardTask("KUA-2", 5, "passing", at(20)),
		boardTask("KUA-1", 3, "passing", at(10)),
	})
	var order []string
	for _, task := range board {
		order = append(order, fmt.Sprintf("%s:%d", task.IssueIdentifier, task.MergePosition))
	}
	require.Equal(t, []string{
		"KUA-1:1", "KUA-2:2", // approved and green, earliest approval first
		"KUA-3:3",            // approved, CI not green
		"KUA-4:4", "KUA-5:5", // open pull requests, oldest first
		"KUA-6:0", // no pull request: not in the merge order
	}, order)
}

func TestBoardOverlapNamesSharedFilesAndWhoIsAhead(t *testing.T) {
	approved := time.Date(2026, 11, 24, 9, 0, 0, 0, time.UTC)
	migration := "BE/migrations/000201_board.up.sql"
	first := boardTask("KUA-1", 3, "passing", &approved, migration, "BE/internal/service/a.go")
	second := boardTask("KUA-2", 7, "", nil, "BE/internal/service/a.go", migration, "UI/x.ts")
	alone := boardTask("KUA-3", 9, "", nil, "WEB/y.svelte")
	draft := boardTask("KUA-4", 0, "", nil, migration)
	board := arrangeBoard([]domain.AgentBoardTask{draft, alone, second, first})

	overlap := func(issue string) []domain.AgentBoardOverlap {
		for _, task := range board {
			if task.IssueIdentifier == issue {
				require.NotNil(t, task.Overlap, issue)
				return task.Overlap
			}
		}
		t.Fatalf("%s is not on the board", issue)
		return nil
	}
	shared := []string{"BE/internal/service/a.go", migration}
	require.Equal(t, []domain.AgentBoardOverlap{
		{TaskID: second.TaskID, IssueIdentifier: "KUA-2", PullRequest: 7, Files: shared, FileCount: 2},
		{TaskID: draft.TaskID, IssueIdentifier: "KUA-4", Files: []string{migration}, FileCount: 1},
	}, overlap("KUA-1"), "the first in line has nobody ahead of it")
	require.Equal(t, []domain.AgentBoardOverlap{
		{TaskID: first.TaskID, IssueIdentifier: "KUA-1", PullRequest: 3, Files: shared, FileCount: 2, Ahead: true},
		{TaskID: draft.TaskID, IssueIdentifier: "KUA-4", Files: []string{migration}, FileCount: 1},
	}, overlap("KUA-2"))
	require.Empty(t, overlap("KUA-3"), "a task sharing no file overlaps nothing")
	ahead := overlap("KUA-4")
	require.True(t, ahead[0].Ahead && ahead[1].Ahead, "a task without a pull request comes after every one with one")

	require.Equal(t, overlap("KUA-2"), boardOverlap(board, second.TaskID), "the facts take a task's overlap from the board")
	require.NotNil(t, boardOverlap(board, uuid.New()), "a task the board does not list overlaps nothing that was read")
	require.Empty(t, boardOverlap(board, uuid.New()))
}

func TestBoardCutsLongFileLists(t *testing.T) {
	var many []string
	for i := range domain.MaxAgentBoardFiles + 5 {
		many = append(many, fmt.Sprintf("src/file%03d.go", i))
	}
	big := boardTask("KUA-1", 1, "", nil, many...)
	same := boardTask("KUA-2", 2, "", nil, many...)
	board := arrangeBoard([]domain.AgentBoardTask{big, same})
	require.Len(t, board[0].ChangedFiles, domain.MaxAgentBoardFiles)
	require.Equal(t, len(many), board[0].ChangedFileCount)
	require.Len(t, board[0].Overlap[0].Files, domain.MaxAgentBoardOverlapFiles)
	require.Equal(t, len(many), board[0].Overlap[0].FileCount, "the overlap is computed on every file, not the cut list")
}
