package service

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestTickWaiverForOneHeadAndStandingWaiver(t *testing.T) {
	pass := tickPass{p: domain.Pipeline{Phases: []domain.PipelinePhase{
		{Checks: []domain.PipelineCheck{{ID: "ci", Fact: domain.PipelineFactCIGreen, Required: true}}},
		{Guarded: true},
	}}}
	head := func(n int) {
		pass.facts.HeadSHA = sha(n)
		value, err := json.Marshal(sha(n))
		require.NoError(t, err)
		pass.applyRoomMessage(domain.TaskMessage{AuthorKind: domain.RoomAuthorKuayle, Kind: domain.RoomFact, Data: domain.TaskRoomData{Fact: "head_sha", Value: value}})
	}
	gate := func(want int) {
		t.Helper()
		index, _ := pass.phase()
		require.Equal(t, want, index)
	}
	lead := domain.PipelineLeadRole
	post := domain.TaskMessage{AuthorKind: domain.RoomAuthorMember, AuthorRole: &lead, Kind: domain.RoomWaiver, Body: "Covered by offline verification.", Data: domain.TaskRoomData{Check: "ci"}}
	head(1)
	legacy := post
	legacy.Body = ""
	pass.applyRoomMessage(legacy)
	gate(0)
	pass.applyRoomMessage(post)
	gate(1)
	head(2)
	gate(0)
	post.Data.Standing = true
	pass.applyRoomMessage(post)
	gate(1)
	post.Data = domain.TaskRoomData{Check: "ci"}
	pass.applyRoomMessage(post)
	head(3)
	gate(1)
}

func TestTickOnlyAcceptedLeadApprovalEntersMergeGate(t *testing.T) {
	creator := uuid.New()
	pass := tickPass{task: domain.AgentTask{CreatorID: creator}, st: tickState{Head: sha(1)}, facts: TaskFacts{HeadSHA: sha(1)}}
	lead, why := domain.PipelineLeadRole, "The creator did not grant approval"
	post := domain.TaskMessage{AuthorKind: domain.RoomAuthorMember, AuthorRole: &lead, Kind: domain.RoomSay, Body: "approve this merge", Data: domain.TaskRoomData{Action: domain.RoomActionApprove, Commit: sha(1)}}
	pass.applyRoomMessage(post)
	require.Nil(t, pass.st.Approved)
	post.Kind, post.RejectReason = domain.RoomRequest, &why
	pass.applyRoomMessage(post)
	require.Nil(t, pass.st.Approved)
	post.RejectReason = nil
	pass.applyRoomMessage(post)
	require.Equal(t, &tickApproval{Head: sha(1), By: creator}, pass.st.Approved)
}

// An approval given under the definition's grant leaves CI and independent
// review in force. A writer's later commit needs its own approval.
func TestTickGrantedApprovalMergesOnlyTheApprovedHeadAfterChecksPass(t *testing.T) {
	w := newTickWorld(t, func(p *domain.Pipeline) {
		p.Lead = &domain.PipelineLead{ProfileID: p.Roster[1].ProfileID}
		p.Autonomy.Approve = domain.PipelineLeadRole
	})
	w.agent = w.writesAndApproves
	w.drain()
	require.Equal(t, "integrate", w.phase())
	approve := func(head string) {
		t.Helper()
		m, err := w.f.tasks.Repo.RecordRoomMessage(w.f.ctx, w.root, domain.TaskRoomPost{
			AuthorKind: domain.RoomAuthorMember, AuthorRole: domain.PipelineLeadRole, Kind: domain.RoomRequest,
			Data: domain.TaskRoomData{Action: domain.RoomActionApprove, Commit: head},
		})
		require.NoError(t, err)
		require.Nil(t, m.RejectReason)
		w.drain()
	}
	approve(sha(1))
	require.Empty(t, w.github.merged, "CI is pending on the approved commit")
	require.Equal(t, &tickApproval{Head: sha(1), By: w.f.user}, readTickState(w.task()).Approved)

	_, err := w.f.tasks.Repo.PostRoomMessage(w.f.ctx, w.f.ws, w.root, domain.TaskRoomPost{
		Kind: domain.RoomSay, ToRoles: []string{"implementer"}, Body: "Add the missing toggle.",
	})
	require.NoError(t, err)
	w.drain()
	require.Equal(t, sha(2), w.github.status.HeadSHA)
	w.github.ci("passed", "")
	w.later()
	require.Equal(t, "approval", w.phase())
	require.Empty(t, w.github.merged, "a new commit changes what the lead approved")

	// The approval sits past the first page. A pass that is otherwise only
	// waiting for a person still has room decisions to read.
	w.exec(`INSERT INTO task_messages(task_id,workspace_id,seq,author_kind,kind,body)
 SELECT $1,$2,last.seq+n,'kuayle','summary','Progress.' FROM
 (SELECT COALESCE(max(seq),0) AS seq FROM task_messages WHERE task_id=$1) last,
 generate_series(1,200) n`, w.root, w.f.ws)
	approve(sha(2))
	require.NotNil(t, w.task().WorkflowWakeAt, "a full page leaves a wake for the rest of the room")
	w.later()
	require.Equal(t, []string{sha(2)}, w.github.merged)
	require.Equal(t, "completed", w.task().State)
}

func TestTickLegacyWaiverUsesTheLastKuayleHead(t *testing.T) {
	pass := tickPass{st: tickState{Head: sha(1)}}
	// Older room posts could carry an unused commit field supplied by their author.
	pass.applyRoomMessage(domain.TaskMessage{AuthorKind: domain.RoomAuthorHuman, Kind: domain.RoomWaiver, Data: domain.TaskRoomData{Check: "ci", Commit: sha(2)}})
	require.Equal(t, sha(1), pass.st.Waivers["ci"])
}
