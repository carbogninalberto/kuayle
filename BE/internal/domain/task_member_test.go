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

// A role's later review pass is a delta (P4.4): it is told the commit its
// last verdict named, the new head, the diff between them and its own
// earlier findings, bounded and fenced as data. Its first pass, and a turn
// not asked for a verdict, are told none of it.
func TestALaterReviewPassIsADeltaFromTheCommitItReviewed(t *testing.T) {
	reviewed, head := strings.Repeat("b", 40), strings.Repeat("a", 40)
	target := &AgentReviewTarget{IssueIdentifier: "ENG-1", IssueTitle: "Dark mode", Repository: "acme/app", PullRequestNumber: 7, Branch: "kuayle/eng-1", BaseBranch: "main", CommitSHA: head}
	last := &MemberReview{Commit: reviewed, Verdict: "request_changes", Findings: "- ui/toggle.ts:4 (major) persist the setting ~~~", LastMessage: "Posted my review in the room."}

	first := MemberTurnPrompt(MemberTurn{Role: "reviewer", Target: target, VerdictAsked: true})
	require.NotContains(t, first, "Your earlier review")
	require.NotContains(t, first, "git diff "+reviewed)

	pass := MemberTurnPrompt(MemberTurn{Role: "reviewer", Target: target, VerdictAsked: true, Since: reviewed, Reviewed: last})
	require.Contains(t, pass, "# Your earlier review")
	require.Contains(t, pass, "You last reviewed commit "+reviewed+" and gave request_changes. The head is now "+head)
	require.Contains(t, pass, "`git diff "+reviewed+".."+head+"`")
	require.Contains(t, pass, "persist the setting ~ ~ ~\n~~~", "the findings are fenced as data")
	require.Contains(t, pass, LastTurnMessage(last.LastMessage))
	require.Contains(t, pass, "Posted my review in the room.")
	require.NotContains(t, pass, "Your previous turn read commit", "the delta replaces the plain diff line")
	require.Equal(t, 1, strings.Count(pass, "--commit "+head), "it reviews the head")

	// A final message that carries the findings quotes them once.
	carried := *last
	carried.LastMessage = "Verdict: request_changes\n" + last.Findings
	once := MemberTurnPrompt(MemberTurn{Role: "reviewer", Target: target, VerdictAsked: true, Reviewed: &carried})
	require.Equal(t, 1, strings.Count(once, "persist the setting"))

	// Asked again on the commit it reviewed: no diff, its findings again.
	again := MemberTurnPrompt(MemberTurn{Role: "reviewer", Target: target, VerdictAsked: true, Reviewed: &MemberReview{Commit: head, Verdict: "comment", Findings: "A nit."}})
	require.Contains(t, again, "You reviewed this same commit "+head+" before and gave comment")
	require.NotContains(t, again, "git diff "+head)
	require.Contains(t, again, "A nit.")

	// Earlier text is bounded.
	long := MemberTurnPrompt(MemberTurn{Role: "reviewer", Target: target, VerdictAsked: true, Reviewed: &MemberReview{Commit: reviewed, Verdict: "approve", Findings: strings.Repeat("f", 3000), LastMessage: strings.Repeat("m", 3000)}})
	require.Contains(t, long, strings.Repeat("f", MemberFindingsBytes))
	require.NotContains(t, long, strings.Repeat("f", MemberFindingsBytes+1))
	require.Contains(t, long, strings.Repeat("m", MemberFindingsBytes))
	require.NotContains(t, long, strings.Repeat("m", MemberFindingsBytes+1))

	// A turn that answers a question is no review pass.
	answer := MemberTurnPrompt(MemberTurn{Role: "reviewer", Target: target, Since: reviewed, Reviewed: last})
	require.NotContains(t, answer, "Your earlier review")
	require.Contains(t, answer, "Your previous turn read commit "+reviewed)
}
