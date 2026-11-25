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
	for _, action := range []string{"merge", "publish", "split"} {
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

func TestTaskRoomCreatorGrantNamesAuthority(t *testing.T) {
	for _, tc := range []struct {
		name    string
		data    TaskRoomData
		allowed bool
	}{
		{"legacy check grant", TaskRoomData{Check: "ci"}, true},
		{"waive one check", TaskRoomData{Role: "lead", Action: "waive", Check: "ci"}, true},
		{"approve merge", TaskRoomData{Role: "lead", Action: "approve"}, true},
		{"unknown check", TaskRoomData{Action: "waive", Check: "missing"}, false},
		{"no check", TaskRoomData{Action: "waive"}, false},
		{"mixed grants", TaskRoomData{Action: "approve", Check: "ci"}, false},
		{"unknown action", TaskRoomData{Action: "publish", Check: "ci"}, false},
		{"wrong role", TaskRoomData{Role: "reviewer", Check: "ci"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			post := TaskRoomPost{AuthorKind: RoomAuthorHuman, Kind: RoomGrant, Data: tc.data}
			require.Equal(t, tc.allowed, TaskRoomRefusal(roomPipeline(true), post, false) == "")
			require.NotEmpty(t, TaskRoomRefusal(roomPipeline(false), post, false))
		})
	}
}

func TestTaskRoomAgentGrantRefused(t *testing.T) {
	p := roomPipeline(true)
	for _, role := range []string{"implementer", "reviewer", PipelineLeadRole} {
		post := TaskRoomPost{AuthorKind: RoomAuthorMember, AuthorRole: role, Kind: RoomGrant,
			Data: TaskRoomData{Role: PipelineLeadRole, Action: "approve"}}
		require.NotEmpty(t, TaskRoomRefusal(p, post, true))
	}
}

func TestTaskRoomLeadApprovesOnlyWithinGrant(t *testing.T) {
	p := roomPipeline(true)
	approval := TaskRoomPost{AuthorKind: RoomAuthorMember, AuthorRole: PipelineLeadRole, Kind: RoomRequest,
		Data: TaskRoomData{Action: "approve", Commit: strings.Repeat("a", 40)}}
	require.NotEmpty(t, TaskRoomRefusal(p, approval, false), "a grant to waive CI cannot approve")
	require.Empty(t, TaskRoomRefusal(p, approval, true), "runtime approval grant")
	p.Autonomy.Approve = PipelineLeadRole
	require.Empty(t, TaskRoomRefusal(p, approval, false), "pinned approval grant")
	for _, role := range []string{"implementer", "reviewer"} {
		other := approval
		other.AuthorRole = role
		require.NotEmpty(t, TaskRoomRefusal(p, other, true))
	}
	owner := approval
	owner.AuthorKind, owner.AuthorRole = RoomAuthorHuman, ""
	require.NotEmpty(t, TaskRoomRefusal(p, owner, false), "the creator already has a dedicated approval route")
	approval.Data.Commit = "abc1234"
	require.NotEmpty(t, TaskRoomRefusal(p, approval, true), "approval names the full head")
	waiver := TaskRoomPost{AuthorKind: RoomAuthorMember, AuthorRole: PipelineLeadRole, Kind: RoomWaiver,
		Body: "Manually verified", Data: TaskRoomData{Check: "reviewer"}}
	require.NotEmpty(t, TaskRoomRefusal(p, waiver, false), "approval cannot waive review")
}

func TestTaskRoomWaiverRequiresReason(t *testing.T) {
	post := TaskRoomPost{AuthorKind: RoomAuthorHuman, Kind: RoomWaiver, Body: "  ", Data: TaskRoomData{Check: "ci"}}
	require.NotEmpty(t, TaskRoomRefusal(roomPipeline(true), post, false))
	post.Body = "Ran the suite locally"
	require.Empty(t, TaskRoomRefusal(roomPipeline(true), post, false))
}

func TestTaskRoomDecisionScopesReachMemberPrompt(t *testing.T) {
	head := strings.Repeat("b", 40)
	for _, tc := range []struct {
		kind, data string
		fragments  []string
	}{
		{RoomGrant, `{"action":"approve"}`, []string{"lead", "approve"}},
		{RoomGrant, `{"check":"ci"}`, []string{"lead", "waive", "ci"}},
		{RoomRequest, `{"action":"approve","commit":"` + head + `"}`, []string{"approve", head}},
		{RoomWaiver, `{"check":"ci","standing":true}`, []string{"ci", "standing"}},
		{RoomWaiver, `{"check":"ci","commit":"` + head + `"}`, []string{"ci", head}},
	} {
		var data TaskRoomData
		require.NoError(t, json.Unmarshal([]byte(tc.data), &data))
		line := roomDataLine(TaskMessage{Kind: tc.kind, Data: data})
		for _, fragment := range tc.fragments {
			require.Contains(t, line, fragment)
		}
	}
}
