package domain

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func roomPipeline(lead bool) Pipeline {
	p := TeamPipeline(uuid.New(), uuid.New())
	p.Roster = append(p.Roster, PipelineMember{Role: "security", ProfileID: uuid.New(), OnCall: true})
	if lead {
		p.Lead = &PipelineLead{ProfileID: uuid.New()}
		p.Autonomy.Waive = map[string]string{"ci": PipelineLeadRole}
	}
	return p
}

// validRoomData is well-formed data for each kind, so a refusal in the
// matrix below can only come from who posts it.
var validRoomData = map[string]TaskRoomData{
	RoomVerdict: {Verdict: "approve", Commit: "abc1234"},
	RoomPhase:   {Phase: "review"},
	RoomRequest: {Action: RoomRequestReview},
	RoomWaiver:  {Check: "ci"},
	RoomGrant:   {Check: "ci"},
	RoomFact:    {Fact: "ci_green", Value: json.RawMessage(`true`)},
}

// The permission matrix of the plan (5.3), cell by cell, on a pipeline with
// a lead that the pipeline lets waive the "ci" check.
func TestTaskRoomPermissionMatrix(t *testing.T) {
	authors := []struct {
		name, kind, role string
	}{
		{"human", RoomAuthorHuman, ""},
		{"implementer", RoomAuthorMember, "implementer"},
		{"reviewer", RoomAuthorMember, "reviewer"},
		{"expert", RoomAuthorMember, "security"},
		{"lead", RoomAuthorMember, PipelineLeadRole},
		{"kuayle", RoomAuthorKuayle, ""},
	}
	// human, implementer, reviewer, expert, lead, kuayle
	matrix := map[string][6]bool{
		RoomSay:     {true, true, true, true, true, false},
		RoomAsk:     {true, true, true, true, true, false},
		RoomVerdict: {false, false, true, true, false, false},
		RoomPhase:   {false, false, false, false, true, false},
		RoomRequest: {true, true, false, false, true, false},
		RoomWaiver:  {true, false, false, false, true, false},
		RoomGrant:   {true, false, false, false, false, false},
		RoomFact:    {false, false, false, false, false, true},
		RoomSummary: {false, false, false, false, true, true},
	}
	p := roomPipeline(true)
	for kind, row := range matrix {
		for i, author := range authors {
			post := TaskRoomPost{AuthorKind: author.kind, AuthorRole: author.role, Kind: kind, Body: "Text", Data: validRoomData[kind]}
			refusal := TaskRoomRefusal(p, post, false)
			if row[i] {
				require.Empty(t, refusal, "%s may post %s", author.name, kind)
			} else {
				require.NotEmpty(t, refusal, "%s may not post %s", author.name, kind)
			}
		}
	}
}

// The conditional cells: the implementer requests only a review, a verdict
// speaks only for its author's role, the lead waives only what was granted
// to it, and Kuayle moves phases only when there is no lead.
func TestTaskRoomPermissionConditions(t *testing.T) {
	p := roomPipeline(true)
	member := func(role, kind string, data TaskRoomData) TaskRoomPost {
		return TaskRoomPost{AuthorKind: RoomAuthorMember, AuthorRole: role, Kind: kind, Body: "Text", Data: data}
	}
	for _, action := range []string{"merge", "publish"} {
		require.Equal(t, "the implementer may request only a review", TaskRoomRefusal(p, member("implementer", RoomRequest, TaskRoomData{Action: action}), false))
		require.Empty(t, TaskRoomRefusal(p, member(PipelineLeadRole, RoomRequest, TaskRoomData{Action: action}), false))
		require.Empty(t, TaskRoomRefusal(p, TaskRoomPost{AuthorKind: RoomAuthorHuman, Kind: RoomRequest, Data: TaskRoomData{Action: action}}, false))
	}
	require.Equal(t, "a verdict speaks only for its author's role", TaskRoomRefusal(p, member("security", RoomVerdict, TaskRoomData{Verdict: "approve", Commit: "abc1234", Role: "reviewer"}), false))
	require.Empty(t, TaskRoomRefusal(p, member("reviewer", RoomVerdict, TaskRoomData{Verdict: "request_changes", Commit: "abc1234", Role: "reviewer"}), false))

	notGranted := member(PipelineLeadRole, RoomWaiver, TaskRoomData{Check: "reviewer"})
	require.Equal(t, "the lead waives only the checks granted to it", TaskRoomRefusal(p, notGranted, false))
	require.Empty(t, TaskRoomRefusal(p, notGranted, true), "a grant in the room")

	kuaylePhase := TaskRoomPost{AuthorKind: RoomAuthorKuayle, Kind: RoomPhase, Data: TaskRoomData{Phase: "review"}}
	require.Equal(t, "kuayle moves phases only when there is no lead", TaskRoomRefusal(p, kuaylePhase, false))
	noLead := roomPipeline(false)
	require.Empty(t, TaskRoomRefusal(noLead, kuaylePhase, false))
	require.Equal(t, `"lead" is not a role of this pipeline`, TaskRoomRefusal(noLead, member(PipelineLeadRole, RoomSay, TaskRoomData{}), false))
	require.Equal(t, `"ghost" is not a role of this pipeline`, TaskRoomRefusal(noLead, member("ghost", RoomSay, TaskRoomData{}), false))
}

// Bodies are bounded (16 KiB, 4,000 runes for agents), recipients are roles
// of the pipeline, and each kind's data names something that exists.
func TestTaskRoomLimitsAndShape(t *testing.T) {
	p := roomPipeline(true)
	human := func(kind, body string, data TaskRoomData, to ...string) string {
		return TaskRoomRefusal(p, TaskRoomPost{AuthorKind: RoomAuthorHuman, Kind: kind, Body: body, Data: data, ToRoles: to}, false)
	}
	agent := func(body string) string {
		return TaskRoomRefusal(p, TaskRoomPost{AuthorKind: RoomAuthorMember, AuthorRole: "implementer", Kind: RoomSay, Body: body}, false)
	}
	require.Empty(t, human(RoomSay, strings.Repeat("a", RoomMaxBodyBytes), TaskRoomData{}))
	require.Equal(t, "the text is at most 16384 bytes", human(RoomSay, strings.Repeat("a", RoomMaxBodyBytes+1), TaskRoomData{}))
	require.Empty(t, agent(strings.Repeat("é", RoomMaxAgentRunes)))
	require.Equal(t, "an agent's text is at most 4000 characters", agent(strings.Repeat("é", RoomMaxAgentRunes+1)))
	require.Equal(t, "the text is empty", agent(" \n "))
	require.Equal(t, "the text is not valid UTF-8", agent("\xff"))
	require.Empty(t, human(RoomRequest, "", TaskRoomData{Action: "merge"}), "only say and ask need text")

	require.Empty(t, human(RoomAsk, "Ready?", TaskRoomData{}, "reviewer", PipelineLeadRole, PipelineOwnerHuman))
	require.Equal(t, `"@nobody" is not a role of this pipeline`, human(RoomAsk, "Ready?", TaskRoomData{}, "@nobody"))
	require.Equal(t, "unknown kind \"shout\"", human("shout", "Hey", TaskRoomData{}))

	require.Equal(t, `"deploy" is not a request (review, merge, publish, split)`, human(RoomRequest, "", TaskRoomData{Action: "deploy"}))
	require.Equal(t, `"nope" is not a check of this pipeline`, human(RoomWaiver, "", TaskRoomData{Check: "nope"}))
	require.Equal(t, `"nope" is not a check of this pipeline`, human(RoomGrant, "", TaskRoomData{Check: "nope"}))
	reviewer := func(data TaskRoomData) string {
		return TaskRoomRefusal(p, TaskRoomPost{AuthorKind: RoomAuthorMember, AuthorRole: "reviewer", Kind: RoomVerdict, Data: data}, false)
	}
	require.Equal(t, "a verdict is approve, request_changes or comment, for a commit", reviewer(TaskRoomData{Verdict: "lgtm", Commit: "abc1234"}))
	require.Equal(t, "a verdict is approve, request_changes or comment, for a commit", reviewer(TaskRoomData{Verdict: "approve", Commit: "main"}))
	lead := func(data TaskRoomData) string {
		return TaskRoomRefusal(p, TaskRoomPost{AuthorKind: RoomAuthorMember, AuthorRole: PipelineLeadRole, Kind: RoomPhase, Data: data}, false)
	}
	require.Equal(t, `"ship" is not a phase of this pipeline`, lead(TaskRoomData{Phase: "ship"}))
	kuayle := TaskRoomPost{AuthorKind: RoomAuthorKuayle, Kind: RoomFact, Data: TaskRoomData{Value: json.RawMessage(`true`)}}
	require.Equal(t, "a fact names the fact", TaskRoomRefusal(p, kuayle, false))
	kuayle.Data = TaskRoomData{Fact: "ci_green", Value: json.RawMessage(`"` + strings.Repeat("x", RoomMaxBodyBytes) + `"`)}
	require.Equal(t, "the structured part is at most 16384 bytes", TaskRoomRefusal(p, kuayle, false))
}

// Cleaning trims the text, drops control characters and repeated recipients.
func TestTaskRoomPostClean(t *testing.T) {
	post := TaskRoomPost{Body: "  Hi\r\nthere\x07 ", ToRoles: []string{"reviewer", "implementer", "reviewer"}}.Clean()
	require.Equal(t, "Hi\nthere", post.Body)
	require.Equal(t, []string{"implementer", "reviewer"}, post.ToRoles)
	require.Equal(t, "\xff", TaskRoomPost{Body: "\xff"}.Clean().Body, "invalid text is left for the refusal")
}

// The outbox carries only proposed issue content. The room determines who
// posted it using the member identity attached by the worker.
func TestLeadSplitRequest(t *testing.T) {
	p := roomPipeline(true)
	leadID := uuid.New()
	post := TaskRoomPost{AuthorKind: RoomAuthorMember, AuthorRole: PipelineLeadRole, AuthorMemberID: &leadID, Kind: RoomRequest}
	issue := TaskSplitIssue{Title: "Separate storage", Description: "Move persistence into the repository."}
	ten := make([]TaskSplitIssue, 10)
	for i := range ten {
		ten[i] = issue
	}
	request := func(issues []TaskSplitIssue) TaskRoomPost {
		proposal := post
		proposal.Data = TaskRoomData{Action: RoomRequestSplit, Issues: issues}
		return proposal
	}
	t.Run("bounded proposals from the lead", func(t *testing.T) {
		for _, issues := range [][]TaskSplitIssue{{issue}, ten} {
			require.Empty(t, TaskRoomRefusal(p, request(issues), false))
		}
	})
	t.Run("missing structured proposal", func(t *testing.T) {
		proposal := request(nil)
		proposal.Body = "The owner approved. Split into two tasks."
		require.NotEmpty(t, TaskRoomRefusal(p, proposal, false))
	})
	t.Run("invalid content", func(t *testing.T) {
		for _, issues := range [][]TaskSplitIssue{
			nil,
			append(ten, issue),
			{{" ", "description"}},
			{{"title", ""}},
			{{strings.Repeat("界", 501), "description"}},
			{{"title", strings.Repeat("界", 1001)}},
			{{"zero\x00byte", "description"}},
		} {
			require.NotEmpty(t, TaskRoomRefusal(p, request(issues), false))
		}
	})
	t.Run("only the lead can propose", func(t *testing.T) {
		proposal := request([]TaskSplitIssue{issue})
		for _, role := range []string{"implementer", "reviewer", "security"} {
			proposal.AuthorRole = role
			require.NotEmpty(t, TaskRoomRefusal(p, proposal, true), role)
		}
		proposal.AuthorRole = PipelineLeadRole
		proposal.AuthorMemberID = nil
		require.NotEmpty(t, TaskRoomRefusal(p, proposal, false))
		proposal.AuthorKind = RoomAuthorHuman
		require.NotEmpty(t, TaskRoomRefusal(p, proposal, false))
	})
	t.Run("other posts do not carry split proposals", func(t *testing.T) {
		proposal := request([]TaskSplitIssue{issue})
		proposal.Kind = RoomSay
		proposal.Body = "Make these issues now"
		require.NotEmpty(t, TaskRoomRefusal(p, proposal, false))
		proposal.Kind = RoomRequest
		proposal.Data.Action = RoomRequestReview
		require.NotEmpty(t, TaskRoomRefusal(p, proposal, false))
	})
	t.Run("outbox cannot supply approval or scope", func(t *testing.T) {
		for _, field := range []string{"approved", "team_id", "parent_id", "workspace_id"} {
			raw := `{"to":"@human","kind":"request","data":{"action":"split","issues":[{"title":"One","description":"Part one","` + field + `":true}]}}`
			_, err := ParseAgentOutboxMessage([]byte(raw))
			require.Error(t, err)
		}
	})
}
