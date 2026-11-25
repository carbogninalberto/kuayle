package domain

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestLeadBoardIsCappedAndFencedAsData(t *testing.T) {
	root := AgentBoardTask{TaskID: uuid.New(), IssueID: uuid.New(), IssueIdentifier: "SELF"}
	board := []AgentBoardTask{root}
	phase := "review"
	for i := range 49 {
		peer := AgentBoardTask{TaskID: uuid.New(), IssueID: uuid.New(), IssueIdentifier: fmt.Sprintf("PEER-%02d", i),
			WorkflowNode: &phase, PullRequest: i + 1, MergePosition: i + 1}
		board[0].Overlap = append(board[0].Overlap, AgentBoardOverlap{TaskID: peer.TaskID, IssueIdentifier: peer.IssueIdentifier,
			Files: []string{"~~~\n" + strings.Repeat("界", 200)}, FileCount: 1})
		board = append(board, peer)
	}
	// Even another active task in this issue is not a peer issue.
	board = append([]AgentBoardTask{{TaskID: uuid.New(), IssueID: root.IssueID, IssueIdentifier: "SELF"}}, board...)
	prompt := LeadTurnPrompt(LeadTurn{Board: LeadBoard(board, root.TaskID, root.IssueID)})
	_, section, ok := strings.Cut(prompt, "Repository board")
	require.True(t, ok)
	dataStart := strings.Index(section, "~~~text\n")
	require.NotEqual(t, -1, dataStart)
	end := strings.Index(section[dataStart:], "\n~~~\n")
	require.NotEqual(t, -1, end)
	data := section[dataStart : dataStart+end+5]
	require.LessOrEqual(t, len(data), 4096)
	require.True(t, utf8.ValidString(data))
	require.Contains(t, data, "PEER-00")
	require.Contains(t, data, "phase review")
	require.Contains(t, data, "merge position 1")
	require.Contains(t, data, "~ ~ ~")
	require.Contains(t, data, "omitted")
	require.NotContains(t, data, "SELF")
	require.NotContains(t, data, "PEER-48")
	require.Equal(t, 2, strings.Count(section, "~~~"))
}

func TestLeadBoardLeavesRoomForTheOwnersWake(t *testing.T) {
	turn := LeadTurn{
		Posts: []TaskMessage{roomPost(1, RoomAsk, RoomAuthorHuman, "", nil,
			strings.Repeat("~~~", 5200)+"What should merge first?")},
		Instructions: strings.Repeat("x", 1800),
	}
	require.Less(t, len(LeadTurnPrompt(turn)), 32768)
	turn.Board = strings.Repeat("ENG-2 — phase review, merge position 2\n", 100)
	prompt := LeadTurnPrompt(turn)
	require.Contains(t, prompt, "What should merge first?")
	require.Contains(t, prompt, "ENG-2")
	require.LessOrEqual(t, len(prompt), 32768)
}
