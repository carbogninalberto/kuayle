package repository

import (
	"context"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestDeclinedSplitCannotBeCreatedFromAStaleCard(t *testing.T) {
	f := newRoomFixture(t)
	proposal := splitRequest(t, f)
	answer := domain.TaskRoomPost{Kind: domain.RoomSay, ReplyTo: &proposal.Seq, Body: "Keep one issue", Data: domain.TaskRoomData{Answer: "decline"}}
	_, err := f.repo.PostRoomMessage(f.ctx, f.task.WorkspaceID, f.task.ID, answer)
	require.NoError(t, err)
	_, err = f.repo.PostRoomMessage(f.ctx, f.task.WorkspaceID, f.task.ID, answer)
	require.Error(t, err)
	_, err = f.repo.ApproveTaskSplit(f.ctx, f.task.WorkspaceID, f.task.ID, proposal.Seq)
	require.Error(t, err)
	require.Empty(t, splitChildren(t, f))
}

func TestWorkflowOwnerDecisions(t *testing.T) {
	f := newRoomFixture(t)
	read := func() domain.OwnerDecisions {
		t.Helper()
		task, err := f.repo.Get(f.ctx, f.task.WorkspaceID, f.task.ID)
		require.NoError(t, err)
		return task.PendingDecisions
	}
	update := func(fields string) {
		t.Helper()
		_, err := f.db.Exec("UPDATE agent_tasks SET "+fields+" WHERE id=$1", f.task.ID)
		require.NoError(t, err)
	}
	update(`pipeline=true, workflow_status='waiting',state='awaiting_review',workflow_node='approval',pipeline_state='{"facts":{"head_sha":"abcde123"}}'`)
	decisions := read()
	require.Len(t, decisions, 1)
	require.Equal(t, "approval", decisions[0].Kind)
	require.Equal(t, "abcde123", decisions[0].Data.Commit)
	update(`pipeline_state=jsonb_set(pipeline_state,'{approved}','{"head":"abcde123"}')`)
	require.Empty(t, read())
	update(`workflow_status='paused',state='blocked',reason='Workflow · The budget is used up'`)
	proposal := splitRequest(t, f)
	require.Len(t, read(), 2, "a budget pause is independent of a split")
	_, err := f.repo.PostRoomMessage(f.ctx, f.task.WorkspaceID, f.task.ID, domain.TaskRoomPost{Kind: domain.RoomSay, ReplyTo: &proposal.Seq, Body: "No", Data: domain.TaskRoomData{Answer: "decline"}})
	require.NoError(t, err)
	require.Len(t, read(), 1)
	require.Equal(t, "pause", read()[0].Kind)
	update(`workflow_status='running',state='running'`)
	require.Empty(t, read())
}

func TestPendingOwnerDecisions(t *testing.T) {
	f := newRoomFixture(t)
	ws, id := f.task.WorkspaceID, f.task.ID
	_, err := f.db.Exec("UPDATE agent_tasks SET pipeline=true WHERE id=$1", id)
	require.NoError(t, err)
	pending := func(n int) {
		t.Helper()
		task, err := f.repo.Get(f.ctx, ws, id)
		require.NoError(t, err)
		require.Len(t, task.PendingDecisions, n)
		items, err := f.repo.NeedsMe(f.ctx, ws, "")
		require.NoError(t, err)
		if n == 0 {
			require.Empty(t, items)
		} else {
			require.Len(t, items, 1)
			require.Equal(t, n, items[0].PendingDecisionCount)
			missing, err := f.repo.NeedsMe(f.ctx, ws, "REC-999")
			require.NoError(t, err)
			require.Empty(t, missing)
		}
	}
	split := splitRequest(t, f)
	pending(1)
	_, err = f.db.Exec(`UPDATE agent_tasks SET state='blocked',workflow_status='paused',reason='Workflow · Nobody made progress on this head',pipeline_state='{"stall":{"count":3}}' WHERE id=$1`, id)
	require.NoError(t, err)
	pending(1) // The stall and the unanswered proposal are one decision.
	splitStatus(t, f)
	_, err = f.repo.ApproveTaskSplit(f.ctx, ws, id, split.Seq)
	require.NoError(t, err)
	pending(0)
	resumed, err := f.repo.Get(f.ctx, ws, id)
	require.NoError(t, err)
	require.Equal(t, "awaiting_review", resumed.State)
	require.Nil(t, resumed.WorkflowPausedAt)
	for _, kind := range []string{domain.RoomAsk, domain.RoomRequest} {
		p := f.member("lead", kind, "Which search should ship?", domain.TaskRoomData{})
		if kind == domain.RoomRequest {
			p.Data.Action = "publish"
			p.Body = ""
		}
		p.ToRoles = []string{"reviewer"}
		_, err = f.repo.RecordRoomMessage(context.Background(), id, p)
		require.NoError(t, err)
		pending(0)
		p.ToRoles = []string{"human"}
		post, err := f.repo.RecordRoomMessage(context.Background(), id, p)
		require.NoError(t, err)
		require.NotEmpty(t, post.Body)
		pending(1)
		notice, err := NewNotificationRepository(f.db).GetByID(f.ctx, post.ID)
		require.NoError(t, err)
		require.Contains(t, notice.Title, post.Body)
		claimed, err := f.repo.ClaimDecisionNotices(context.Background())
		require.NoError(t, err)
		require.Contains(t, claimed, DecisionNotice{AgentTask: domain.AgentTask{ID: id, CreatorID: f.task.CreatorID, IssueID: f.task.IssueID, WorkspaceID: ws}, NoticeID: post.ID})
		again, err := f.repo.ClaimDecisionNotices(context.Background())
		require.NoError(t, err)
		require.Empty(t, again)
		reply := f.member("reviewer", domain.RoomSay, "I agree", domain.TaskRoomData{})
		reply.ReplyTo = &post.Seq
		_, err = f.repo.RecordRoomMessage(context.Background(), id, reply)
		require.NoError(t, err)
		pending(1)
		_, err = f.repo.PostRoomMessage(f.ctx, ws, id, domain.TaskRoomPost{Kind: domain.RoomSay, Body: "Start with titles", ReplyTo: &post.Seq})
		require.NoError(t, err)
		pending(0)
	}
}
