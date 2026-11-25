package service

import (
	"encoding/json"
	"fmt"
	"strings"
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
	board := domain.ArrangeBoard([]domain.AgentBoardTask{
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
	board := domain.ArrangeBoard([]domain.AgentBoardTask{draft, alone, second, first})

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
	board := domain.ArrangeBoard([]domain.AgentBoardTask{big, same})
	require.Len(t, board[0].ChangedFiles, domain.MaxAgentBoardFiles)
	require.Equal(t, len(many), board[0].ChangedFileCount)
	require.Len(t, board[0].Overlap[0].Files, domain.MaxAgentBoardOverlapFiles)
	require.Equal(t, len(many), board[0].Overlap[0].FileCount, "the overlap is computed on every file, not the cut list")
}

// The board fact (P4.6) stays within the room's bounds however many tasks
// overlap, and says each one's place in words.
func TestTheBoardFactIsBounded(t *testing.T) {
	var overlap []domain.AgentBoardOverlap
	for i := range 12 {
		files := []string{}
		for j := range 8 {
			files = append(files, fmt.Sprintf("dir/%s%d.go", string(rune('a'+j)), i))
		}
		overlap = append(overlap, domain.AgentBoardOverlap{IssueIdentifier: fmt.Sprintf("KUA-%d", i+1), PullRequest: i + 1, Files: files, FileCount: 30, Ahead: i < 3})
	}
	overlap[11].Files[0] = "deep/" + strings.Repeat("x", 400) + ".go"
	overlap[4].PullRequest = 0
	room := newBoardRoom(overlap)
	require.Len(t, room.Overlap, boardRoomTasks)
	require.Equal(t, 2, room.More)
	for _, task := range room.Overlap {
		require.Len(t, task.Files, boardRoomFiles)
		require.Equal(t, 30, task.FileCount)
	}
	text := room.text("acme/app")
	require.Contains(t, text, "KUA-1 (pull request #1) also changes dir/a0.go, dir/b0.go, dir/c0.go and 27 more file(s) and is ahead of this task in the merge order")
	require.Contains(t, text, "KUA-4 (pull request #4) also changes")
	require.Contains(t, text, "comes after this task in the merge order")
	require.Contains(t, text, "KUA-5 also changes dir/a4.go, dir/b4.go, dir/c4.go and 27 more file(s) and has no pull request yet")
	require.Contains(t, text, "2 more task(s) change some of the same files.")
	require.Equal(t, "No other active task on acme/app changes the same files.", newBoardRoom([]domain.AgentBoardOverlap{}).text("acme/app"))

	// A long path is cut, and the fact stays far below the room's limit.
	long := newBoardRoom(overlap[10:])
	require.LessOrEqual(t, len(long.Overlap[1].Files[0]), boardRoomPathBytes)
	raw, err := json.Marshal(newBoardRoom(overlap))
	require.NoError(t, err)
	require.Less(t, len(raw), domain.RoomMaxBodyBytes)
}
