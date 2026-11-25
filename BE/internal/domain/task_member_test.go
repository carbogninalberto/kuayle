package domain

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Only a review request the room took asks a member for a verdict.
func TestOnlyATakenReviewRequestAsksForAVerdict(t *testing.T) {
	request := roomPost(3, RoomRequest, RoomAuthorMember, "implementer", []string{"reviewer"}, "")
	request.Data.Action = RoomRequestReview
	merge := roomPost(4, RoomRequest, RoomAuthorHuman, "", nil, "")
	merge.Data.Action = "merge"
	refused := request
	reason := "refused"
	refused.RejectReason = &reason
	reviewer := PipelineMember{Role: "reviewer"}
	require.True(t, RoomReviewRequested(reviewer, []TaskMessage{merge, request}))
	require.False(t, RoomReviewRequested(reviewer, []TaskMessage{merge, refused, roomPost(5, RoomAsk, RoomAuthorHuman, "", nil, "review?")}))
}

// A turn asked for a verdict is a review of the pinned commit that may also
// answer in the room; its role's instructions are its focus, bounded.
func TestAMemberTurnAskedForAVerdictIsAReviewOfItsCommit(t *testing.T) {
	target := &AgentReviewTarget{IssueIdentifier: "ENG-1", IssueTitle: "Dark mode", Repository: "acme/app", PullRequestNumber: 7, Branch: "kuayle/eng-1", BaseBranch: "main", CommitSHA: strings.Repeat("a", 40)}
	ask := roomPost(2, RoomAsk, RoomAuthorMember, "implementer", []string{"reviewer"}, "Is the toggle covered?")
	prompt := MemberTurnPrompt(MemberTurn{Role: "reviewer", Target: target, Instructions: strings.Repeat("x", 5000), VerdictAsked: true, Posts: []TaskMessage{ask}})
	require.Contains(t, prompt, "You are the @reviewer")
	require.Contains(t, prompt, "git checkout --detach "+target.CommitSHA)
	require.Contains(t, prompt, "Verdict: approve | request_changes | comment")
	require.Contains(t, prompt, "--commit "+target.CommitSHA)
	require.Contains(t, prompt, strings.Repeat("x", MemberInstructionsBytes))
	require.NotContains(t, prompt, strings.Repeat("x", MemberInstructionsBytes+1))
	require.Contains(t, prompt, "#2 from the implementer (ask, to @reviewer)")

	answer := MemberTurnPrompt(MemberTurn{Role: "reviewer", Target: target, Posts: []TaskMessage{ask}})
	require.NotContains(t, answer, "Verdict: approve")
	require.Contains(t, answer, "Nobody asked you for a verdict")
}
