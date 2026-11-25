package domain

import (
	"fmt"
	"strings"
)

// The lead of a pipeline (KUA-300, plan P5): the member that manages. Its
// turns run as any member's that does not write code (a member task, read
// only, without repository credentials), and the room's matrix
// (TaskRoomRefusal) lets it talk, request, summarise and move phases, but
// never give a verdict, post a fact, grant, or waive what was not granted
// to it.

// Behaviour is what a role of the pipeline does: a role that writes changes
// code, the lead manages and every other role reviews; "" for a role the
// pipeline does not have.
func (p Pipeline) Behaviour(role string) string {
	member, ok := p.Member(role)
	switch {
	case !ok:
		return ""
	case member.Writer:
		return AgentBehaviourChangesCode
	case role == PipelineLeadRole:
		return AgentBehaviourManages
	}
	return AgentBehaviourReviews
}

// Member is the member that plays a role: a role of the roster, or the
// lead, played by the profile the pipeline names. The roster never has a
// role called lead (ValidatePipeline), so a pipeline has at most one.
func (p Pipeline) Member(role string) (PipelineMember, bool) {
	if member, ok := p.role(role); ok {
		return member, true
	}
	if role == PipelineLeadRole && p.Lead != nil {
		return PipelineMember{Role: PipelineLeadRole, ProfileID: p.Lead.ProfileID}, true
	}
	return PipelineMember{}, false
}

// Members are the roles of the roster, then the lead when there is one.
func (p Pipeline) Members() []PipelineMember {
	members := append([]PipelineMember(nil), p.Roster...)
	if lead, ok := p.Member(PipelineLeadRole); ok {
		members = append(members, lead)
	}
	return members
}

// leadRefusals say why the lead may not post a kind, in words it can act on.
var leadRefusals = map[string]string{
	RoomVerdict: "the lead gives no verdict; ask a role that reviews with --request review",
	RoomFact:    "only Kuayle posts facts",
	RoomGrant:   "only the task's owner grants",
}

// LeadTurn is what a turn of the lead reads: the pipeline, the phase the
// task is in, the published work (nil before anything is published), the
// lead profile's instructions and its unread posts.
type LeadTurn struct {
	Pipeline     Pipeline
	Phase        string
	Target       *AgentReviewTarget
	Instructions string
	Posts        []TaskMessage
}

// LeadTurnPrompt is the prompt of a turn of the lead: the standing contract
// of the role that manages, the team and the phases with their checks, how
// to post, then its room (RoomTurnPrompt).
func LeadTurnPrompt(turn LeadTurn) string {
	var b strings.Builder
	b.WriteString("You are the @lead of the team working on this task: you manage it. Kuayle continues your session across your turns where your harness can, and otherwise ends this prompt with what your previous turn did.\n\n")
	b.WriteString("Keep the work moving toward the owner's request: decide what happens next, ask the right role for it, and tell the owner what matters. As the lead, you write no code and give no verdict: the roles below do. Kuayle keeps the facts, publishes the writer's work, keeps the branch up to date and merges once every required check holds.\n")
	if t := turn.Target; t != nil {
		fmt.Fprintf(&b, "Kuayle published commit %s on branch %s (pull request #%d in %s); the base branch is %s. To read it, run `git checkout --detach %s` in this worktree, then `git diff origin/%s...HEAD`. ", t.CommitSHA, t.Branch, t.PullRequestNumber, t.Repository, t.BaseBranch, t.CommitSHA, t.BaseBranch)
	} else {
		b.WriteString("Nothing is published yet. ")
	}
	b.WriteString("You read the work read-only and have no repository credentials: do not edit, commit or push anything; Kuayle discards any change.\n")

	b.WriteString("\nThe team:\n")
	for _, member := range turn.Pipeline.Roster {
		switch {
		case member.Writer:
			fmt.Fprintf(&b, "- @%s writes the code; its turns are published.\n", member.Role)
		case member.OnCall:
			fmt.Fprintf(&b, "- @%s reviews, on call: it works only when addressed by name.\n", member.Role)
		default:
			fmt.Fprintf(&b, "- @%s reviews and answers what it is asked.\n", member.Role)
		}
	}
	b.WriteString("\nThe phases, in order:\n")
	for _, phase := range turn.Pipeline.Phases {
		fmt.Fprintf(&b, "- %s (%s)", phase.ID, phase.Label)
		if phase.ID == turn.Phase {
			b.WriteString(", the current phase")
		}
		switch {
		case phase.Guarded:
			b.WriteString(", guarded: Kuayle merges only once every required check before it holds")
		case phase.Owner == PipelineOwnerKuayle:
			b.WriteString(", run by Kuayle")
		case phase.Owner == PipelineOwnerHuman:
			b.WriteString(", owned by the task's owner")
		default:
			fmt.Fprintf(&b, ", owned by @%s", phase.Owner)
		}
		for i, check := range phase.Checks {
			sep := "; "
			if i == 0 {
				sep = ". Checks: "
			}
			fmt.Fprintf(&b, "%s%s: %s, %s", sep, check.ID, leadCheck(check), map[bool]string{true: "required", false: "advisory"}[check.Required])
		}
		b.WriteString("\n")
	}

	b.WriteString("\nYou choose the phase (KUA-301): move the task with kuayle-say --to @all --phase <id> \"why\", forward, back or past a phase. Kuayle runs what the phase you choose needs (it asks a role for a verdict a required check is missing, waits for the owner's approval, merges at the guarded phase), and every required check of the phases up to the merge must hold, or be waived by the owner, before it merges, whatever you move. Kuayle tells you when the checks change, and what the current phase waits on when nobody works.\n")
	b.WriteString("\nPost in the room with kuayle-say (see \"The task room\" in kuayle-agents):\n")
	b.WriteString("- kuayle-say --to @implementer --ask \"...\": a question or a message to a role, @human or @all;\n")
	b.WriteString("- kuayle-say --to @reviewer --request review: a request (review, merge, publish or split);\n")
	b.WriteString("- kuayle-say --to @human --kind summary \"...\": a short report for the owner.\n")
	b.WriteString("Kuayle refuses a verdict, a fact or a grant from you, and a waiver of a check the owner did not grant you, and tells you why at your next turn.\n")
	instructions := strings.TrimSpace(turn.Instructions)
	if len(instructions) > MemberInstructionsBytes {
		instructions = strings.ToValidUTF8(instructions[:MemberInstructionsBytes], "")
	}
	if instructions != "" {
		fmt.Fprintf(&b, "\nYour instructions:\n%s\n", instructions)
	}
	b.WriteString("\nEnd your turn with a short final message: what you decided and why.\n")
	if room := RoomTurnPrompt(PipelineLeadRole, turn.Posts); room != "" {
		b.WriteString("\n" + room)
	}
	return b.String()
}

// leadCheck says what a check requires, for the lead.
func leadCheck(check PipelineCheck) string {
	switch check.Fact {
	case PipelineFactVerdict:
		return "a verdict of the @" + check.Role
	case PipelineFactScript:
		return "a command in the worktree exits 0"
	}
	return check.Fact
}
