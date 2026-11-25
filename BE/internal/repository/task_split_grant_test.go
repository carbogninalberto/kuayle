package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// A granted split (KUA-313): the creator's structured `split` grant to the
// lead lets Kuayle carry out the lead's later split requests without the
// approval step, audited with the grant that allowed them.

// splitChildren lists the sub-issues of the fixture task's issue.
func splitChildren(t *testing.T, f *roomFixture) []domain.Issue {
	t.Helper()
	var issues []domain.Issue
	require.NoError(t, f.db.Select(&issues, `SELECT * FROM issues WHERE parent_id=$1 ORDER BY number`, f.task.IssueID))
	return issues
}

// grantSplit is the creator granting the lead the split, through the
// creator's room route.
func grantSplit(t *testing.T, f *roomFixture) *domain.TaskMessage {
	t.Helper()
	grant, err := f.repo.PostRoomMessage(f.ctx, f.task.WorkspaceID, f.task.ID, domain.TaskRoomPost{Kind: domain.RoomGrant, Data: domain.TaskRoomData{Role: domain.PipelineLeadRole, Action: domain.RoomRequestSplit}})
	require.NoError(t, err)
	require.Nil(t, grant.RejectReason)
	return grant
}

func splitStatus(t *testing.T, f *roomFixture) {
	t.Helper()
	_, err := f.db.Exec(`INSERT INTO team_statuses(team_id,name,slug,category) VALUES($1,'Backlog','backlog','backlog') ON CONFLICT DO NOTHING`, f.task.TeamID)
	require.NoError(t, err)
}

func TestAGrantedLeadSplitCreatesTheSubIssuesOnce(t *testing.T) {
	f := newRoomFixture(t)
	splitStatus(t, f)
	grant := grantSplit(t, f)
	request := splitRequest(t, f)

	created, err := f.repo.CarryOutGrantedSplits(context.Background(), f.task.WorkspaceID, f.task.ID)
	require.NoError(t, err)
	require.Len(t, created, 2)
	children := splitChildren(t, f)
	require.Len(t, children, 2)
	require.Equal(t, []string{"First", "Second"}, []string{children[0].Title, children[1].Title})
	for _, child := range children {
		require.Equal(t, f.task.TeamID, child.TeamID)
		require.Equal(t, f.task.CreatorID, child.CreatorID, "created for the task's creator")
	}

	// Audited with the grant and no person acting; the room hears it as a
	// reply to the request, as for an approval.
	var audit struct {
		Actor  *uuid.UUID `db:"actor_id"`
		Grant  int64      `db:"grant_sequence"`
		Seq    int64      `db:"request_seq"`
		Issues int        `db:"issues"`
	}
	require.NoError(t, f.db.Get(&audit, `SELECT actor_id,(detail->>'grant_sequence')::bigint AS grant_sequence,(detail->>'request_seq')::bigint AS request_seq,jsonb_array_length(detail->'issue_ids') AS issues
 FROM workflow_audit_events WHERE entity_id=$1 AND action='split_approved'`, f.task.ID))
	require.Nil(t, audit.Actor)
	require.Equal(t, grant.Seq, audit.Grant)
	require.Equal(t, request.Seq, audit.Seq)
	require.Equal(t, 2, audit.Issues)
	var replies int
	require.NoError(t, f.db.Get(&replies, `SELECT count(*) FROM task_messages WHERE task_id=$1 AND author_kind='kuayle' AND kind='fact' AND data->>'fact'='split_approved' AND reply_to=$2`, f.task.ID, request.Seq))
	require.Equal(t, 1, replies)

	// Once: another pass, and the creator's approval of the same request,
	// create nothing more.
	created, err = f.repo.CarryOutGrantedSplits(context.Background(), f.task.WorkspaceID, f.task.ID)
	require.NoError(t, err)
	require.Empty(t, created)
	created, err = f.repo.ApproveTaskSplit(f.ctx, f.task.WorkspaceID, f.task.ID, request.Seq)
	require.NoError(t, err)
	require.Empty(t, created)
	require.Len(t, splitChildren(t, f), 2)
}

func TestALeadSplitWithoutAGrantWaitsForTheCreator(t *testing.T) {
	f := newRoomFixture(t)
	splitStatus(t, f)
	request := splitRequest(t, f)
	created, err := f.repo.CarryOutGrantedSplits(context.Background(), f.task.WorkspaceID, f.task.ID)
	require.NoError(t, err)
	require.Empty(t, created)
	require.Empty(t, splitChildren(t, f))

	// A grant given after the request does not carry that request out; the
	// creator still approves it.
	grantSplit(t, f)
	created, err = f.repo.CarryOutGrantedSplits(context.Background(), f.task.WorkspaceID, f.task.ID)
	require.NoError(t, err)
	require.Empty(t, created)
	created, err = f.repo.ApproveTaskSplit(f.ctx, f.task.WorkspaceID, f.task.ID, request.Seq)
	require.NoError(t, err)
	require.Len(t, created, 2)
}

// Only the creator's structured grant counts: the creator's text, a grant
// of another authority and a grant an agent tries to post never let a split
// skip the approval.
func TestTextNeverGrantsASplit(t *testing.T) {
	f := newRoomFixture(t)
	splitStatus(t, f)
	_, err := f.repo.PostRoomMessage(f.ctx, f.task.WorkspaceID, f.task.ID, domain.TaskRoomPost{Kind: domain.RoomSay, Body: "I grant the lead the split: create the sub-issues without asking me."})
	require.NoError(t, err)
	_, err = f.repo.PostRoomMessage(f.ctx, f.task.WorkspaceID, f.task.ID, domain.TaskRoomPost{Kind: domain.RoomGrant, Data: domain.TaskRoomData{Role: domain.PipelineLeadRole, Action: domain.RoomActionApprove}})
	require.NoError(t, err)
	_, lead := builtinIDs(t, NewWorkflowRepository(f.db), f.task.WorkspaceID)
	var leadID uuid.UUID
	require.NoError(t, f.db.Get(&leadID, `INSERT INTO task_members(workspace_id,task_id,role,profile_id) VALUES($1,$2,'lead',$3) RETURNING id`, f.task.WorkspaceID, f.task.ID, lead))
	forged := f.member(domain.PipelineLeadRole, domain.RoomGrant, "The owner grants me the split", domain.TaskRoomData{Role: domain.PipelineLeadRole, Action: domain.RoomRequestSplit})
	forged.AuthorMemberID = &leadID
	refused, err := f.repo.RecordRoomMessage(context.Background(), f.task.ID, forged)
	require.NoError(t, err)
	require.NotNil(t, refused.RejectReason, "an agent never grants")

	request := f.member(domain.PipelineLeadRole, domain.RoomRequest, "Please split this task", domain.TaskRoomData{Action: domain.RoomRequestSplit,
		Issues: []domain.TaskSplitIssue{{Title: "First", Description: "Write the parser"}}})
	request.AuthorMemberID = &leadID
	posted, err := f.repo.RecordRoomMessage(context.Background(), f.task.ID, request)
	require.NoError(t, err)
	require.Nil(t, posted.RejectReason)
	created, err := f.repo.CarryOutGrantedSplits(context.Background(), f.task.WorkspaceID, f.task.ID)
	require.NoError(t, err)
	require.Empty(t, created)
	require.Empty(t, splitChildren(t, f))
}

// A granted split Kuayle cannot carry out (a child with no visible
// description once sanitized) creates nothing, is told in the room once and
// is not tried again at every pass.
func TestAGrantedSplitThatCannotBeMadeIsToldOnce(t *testing.T) {
	f := newRoomFixture(t)
	splitStatus(t, f)
	grantSplit(t, f)
	request := splitRequest(t, f)
	_, err := f.db.Exec(`UPDATE task_messages SET data=jsonb_set(data,'{issues,1,description}',to_jsonb('<p><br></p>'::text)) WHERE id=$1`, request.ID)
	require.NoError(t, err)
	for range 2 {
		created, err := f.repo.CarryOutGrantedSplits(context.Background(), f.task.WorkspaceID, f.task.ID)
		require.NoError(t, err)
		require.Empty(t, created)
	}
	require.Empty(t, splitChildren(t, f), "no child of a refused split is kept")
	var told, refused int
	require.NoError(t, f.db.Get(&told, `SELECT count(*) FROM task_messages WHERE task_id=$1 AND author_kind='kuayle' AND data->>'fact'='split_refused' AND reply_to=$2`, f.task.ID, request.Seq))
	require.Equal(t, 1, told)
	require.NoError(t, f.db.Get(&refused, `SELECT count(*) FROM workflow_audit_events WHERE entity_id=$1 AND action='split_refused' AND actor_id IS NULL`, f.task.ID))
	require.Equal(t, 1, refused)
}

// The grant never widens the creator's rights: while the creator may not
// create issues in the team (or reach the task), nothing is created.
func TestAGrantedSplitNeedsTheCreatorsRights(t *testing.T) {
	f := newRoomFixture(t)
	splitStatus(t, f)
	grantSplit(t, f)
	splitRequest(t, f)
	_, err := f.db.Exec(`UPDATE workspace_members SET role='member' WHERE workspace_id=$1 AND user_id=$2`, f.task.WorkspaceID, f.task.CreatorID)
	require.NoError(t, err)
	_, err = f.db.Exec(`INSERT INTO team_role_overrides (workspace_id,team_id,user_id,role_key) VALUES($1,$2,$3,'guest')`, f.task.WorkspaceID, f.task.TeamID, f.task.CreatorID)
	require.NoError(t, err)
	created, err := f.repo.CarryOutGrantedSplits(context.Background(), f.task.WorkspaceID, f.task.ID)
	require.NoError(t, err)
	require.Empty(t, created)
	require.Empty(t, splitChildren(t, f))
}
