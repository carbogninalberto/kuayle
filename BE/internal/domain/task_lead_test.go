package domain

import (
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// The lead of a pipeline (KUA-300) manages: it is the one member whose
// behaviour is manages. No profile stores that behaviour; the pipeline gives
// it to the profile it names as its lead.
func TestThePipelineGivesItsLeadTheBehaviourManages(t *testing.T) {
	implementer, reviewer, _ := exampleProfiles()
	p := TeamPipeline(implementer, reviewer)
	require.Equal(t, AgentBehaviourChangesCode, p.Behaviour("implementer"))
	require.Equal(t, AgentBehaviourReviews, p.Behaviour("reviewer"))
	require.Empty(t, p.Behaviour(PipelineLeadRole), "a pipeline without a lead has no manager")
	require.Empty(t, p.Behaviour("ghost"))
	_, ok := p.Member(PipelineLeadRole)
	require.False(t, ok)

	lead := uuid.New()
	p.Lead = &PipelineLead{ProfileID: lead}
	require.Equal(t, AgentBehaviourManages, p.Behaviour(PipelineLeadRole))
	member, ok := p.Member(PipelineLeadRole)
	require.True(t, ok)
	require.Equal(t, PipelineMember{Role: PipelineLeadRole, ProfileID: lead}, member, "the lead never writes and is never on call")
	reviewing, ok := p.Member("reviewer")
	require.True(t, ok)
	require.Equal(t, "reviewer", reviewing.Role)
	require.Equal(t, []string{"implementer", "reviewer", PipelineLeadRole}, memberRoles(p.Members()))

	require.False(t, ValidAgentBehaviour(AgentBehaviourManages), "a profile never stores it")
	require.NotContains(t, AgentBehaviours(), AgentBehaviourManages, "the profile editor never offers it")
}

func memberRoles(members []PipelineMember) []string {
	roles := []string{}
	for _, member := range members {
		roles = append(roles, member.Role)
	}
	return roles
}

// A roster has at most one lead: the pipeline's lead, never a role of the
// roster called lead. Any profile may lead, one that also plays a role too.
func TestAPipelineHasAtMostOneLead(t *testing.T) {
	implementer, reviewer, profiles := exampleProfiles()
	p := TeamPipeline(implementer, reviewer)
	p.Lead = &PipelineLead{ProfileID: reviewer}
	require.Empty(t, ValidatePipeline(p, profiles), "the reviewer's profile leads too")

	p.Roster = append(p.Roster, PipelineMember{Role: PipelineLeadRole, ProfileID: reviewer})
	require.Equal(t, []string{"invalid_role"}, issueCodes(ValidatePipeline(p, profiles)), "a second lead in the roster")
}

// The lead may say, ask, request, summarise and move phases; Kuayle refuses
// a verdict, a fact, a grant and an ungranted waiver from it, and says why
// in words the lead can act on.
func TestTheRoomRefusesWhatTheLeadMayNotPostAndSaysWhy(t *testing.T) {
	p := roomPipeline(true)
	lead := func(kind string, data TaskRoomData) string {
		return TaskRoomRefusal(p, TaskRoomPost{AuthorKind: RoomAuthorMember, AuthorRole: PipelineLeadRole, Kind: kind, Body: "Text", Data: data}, false)
	}
	for kind, why := range map[string]string{
		RoomVerdict: "the lead may not post verdict: the lead gives no verdict; ask a role that reviews with --request review",
		RoomFact:    "the lead may not post fact: only Kuayle posts facts",
		RoomGrant:   "the lead may not post grant: only the task's owner grants",
		RoomWaiver:  "the lead waives only the checks granted to it",
	} {
		data := validRoomData[kind]
		if kind == RoomWaiver {
			data = TaskRoomData{Check: "reviewer"}
		}
		require.Equal(t, why, lead(kind, data), kind)
	}
	for _, kind := range []string{RoomSay, RoomAsk, RoomRequest, RoomSummary, RoomPhase} {
		require.Empty(t, lead(kind, validRoomData[kind]), kind)
	}
	require.Empty(t, lead(RoomWaiver, TaskRoomData{Check: "ci"}), "the pipeline granted it the ci check")
	require.Equal(t, "the implementer may not post verdict", TaskRoomRefusal(p, TaskRoomPost{AuthorKind: RoomAuthorMember, AuthorRole: "implementer", Kind: RoomVerdict, Data: validRoomData[RoomVerdict]}, false),
		"other roles' refusals are unchanged")
}

// The lead's turn says what it is: the manager, which reads the published
// work, writes no code and gives no verdict; with the team, the phases and
// their checks, how to post, and its room.
func TestALeadTurnPromptSaysWhatTheLeadDoes(t *testing.T) {
	implementer, reviewer, _ := exampleProfiles()
	p := TeamPipeline(implementer, reviewer)
	p.Lead = &PipelineLead{ProfileID: uuid.New()}
	p.Roster = append(p.Roster, PipelineMember{Role: "uiux", ProfileID: reviewer, OnCall: true})
	target := &AgentReviewTarget{Repository: "acme/app", PullRequestNumber: 12, Branch: "kuayle/eng-1", BaseBranch: "main", CommitSHA: strings.Repeat("a", 40)}
	prompt := LeadTurnPrompt(LeadTurn{Pipeline: p, Phase: "review", Target: target, Instructions: "Keep the owner informed.",
		Posts: []TaskMessage{roomPost(4, RoomSay, RoomAuthorHuman, "", nil, "Ship it by Friday.")}})
	for _, want := range []string{
		"You are the @lead",
		"you write no code and give no verdict",
		"no repository credentials",
		"Keep the task notebook",
		"plan, what is done and what remains",
		"@implementer writes the code",
		"@reviewer reviews",
		"@uiux reviews, on call",
		"review (Review), the current phase",
		"reviewer: a verdict of the @reviewer, required",
		"merge (Merge), guarded",
		"kuayle-say --to @human --kind summary",
		"Keep the owner informed.",
		"#4 from the task's owner (say)",
		target.CommitSHA,
	} {
		require.Contains(t, prompt, want)
	}
	require.False(t, slices.ContainsFunc(strings.Split(prompt, "\n"), func(line string) bool { return strings.HasPrefix(line, "Verdict:") }))

	nothing := LeadTurnPrompt(LeadTurn{Pipeline: p, Phase: "implement"})
	require.Contains(t, nothing, "Nothing is published yet")
	require.NotContains(t, nothing, "# Task room")
}
