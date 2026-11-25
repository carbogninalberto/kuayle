package repository

import (
	"context"
	"fmt"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// The lead (KUA-300) takes its turns as a member that reads: its own member
// task, a review's isolation (never published, no push), the lead's profile,
// never asked for a verdict, and the prompt of the role that manages.
// What it may not post (a verdict, a fact, a grant, a waiver nobody granted
// it) is kept as refused and explained at its next turn; nothing of it
// counts.
func TestTheLeadsRefusedPostsAreExplainedAtItsNextTurn(t *testing.T) {
	f := newMemberFixture(t)
	_, leadProfile := builtinIDs(t, NewWorkflowRepository(f.db), f.task.WorkspaceID)
	f.publish(firstCommit)
	_, err := f.repo.PostRoomMessage(f.ctx, f.task.WorkspaceID, f.task.ID, domain.TaskRoomPost{Kind: domain.RoomSay, ToRoles: []string{domain.PipelineLeadRole}, Body: "Plan the work."})
	require.NoError(t, err)

	member, err := f.repo.StartMemberTurn(f.ctx, f.task.ID, domain.PipelineLeadRole, true)
	require.NoError(t, err)
	require.Equal(t, domain.PipelineLeadRole, member.Role)
	require.Equal(t, leadProfile, member.ProfileID, "the profile the pipeline names as its lead")
	require.False(t, member.VerdictAsked, "the lead is never asked for a verdict")
	require.Equal(t, firstCommit, *member.PinnedCommit, "it reads the newest published commit, as the roles that review do")
	task := f.memberTask(member)
	require.Equal(t, domain.AgentTaskKindReview, task.Kind, "a review's isolation: never published, no push")
	require.Contains(t, task.Prompt, "You are the @lead")
	require.Contains(t, task.Prompt, "#1 from the task's owner (say, to @lead)")
	require.NotContains(t, task.Prompt, "--verdict approve|request_changes|comment --commit")

	posts := map[string]string{
		"verdict": `{"to":"@all","kind":"verdict","body":"Looks fine.","data":{"verdict":"approve","commit":"` + firstCommit + `"}}`,
		"fact":    `{"to":"@all","kind":"fact","body":"CI is green.","data":{"fact":"ci_green","value":true}}`,
		"grant":   `{"to":"@all","kind":"grant","body":"I may waive CI.","data":{"check":"ci"}}`,
		"waiver":  `{"to":"@all","kind":"waiver","body":"CI is flaky.","data":{"check":"ci"}}`,
		"summary": `{"to":"@human","kind":"summary","body":"The writer is on it."}`,
		"ask":     `{"to":"@implementer","kind":"ask","body":"Is the toggle persisted?"}`,
	}
	order := []string{"verdict", "fact", "grant", "waiver", "summary", "ask"}
	finished := f.runTurn(member, "0c9a6c1e-0000-4000-8000-000000000007", "Asked the implementer; reported to the owner.", func(task domain.AgentTask) {
		var files []domain.AgentOutboxFile
		for i, name := range order {
			files = append(files, domain.AgentOutboxFile{Name: fmt.Sprintf("%02d-%s.json", i, name), Data: []byte(posts[name])})
		}
		rest, recorded, err := f.repo.RecordRoomOutbox(context.Background(), task.ID, files)
		require.NoError(t, err)
		require.Empty(t, rest)
		require.Equal(t, len(order), recorded)
	})
	require.Equal(t, "blocked", finished.State, "it rests until its next turn")

	byKind := map[string]domain.TaskMessage{}
	for _, m := range f.room() {
		if m.AuthorRole != nil && *m.AuthorRole == domain.PipelineLeadRole {
			byKind[m.Kind] = m
		}
	}
	refusals := map[string]string{
		domain.RoomVerdict: "the lead may not post verdict: the lead gives no verdict; ask a role that reviews with --request review",
		domain.RoomFact:    "the lead may not post fact: only Kuayle posts facts",
		domain.RoomGrant:   "the lead may not post grant: only the task's owner grants",
		domain.RoomWaiver:  "the lead waives only the checks granted to it",
	}
	for kind, reason := range refusals {
		require.NotNil(t, byKind[kind].RejectReason, kind)
		require.Equal(t, reason, *byKind[kind].RejectReason)
		require.Equal(t, member.ID, *byKind[kind].AuthorMemberID)
	}
	for _, kind := range []string{domain.RoomSummary, domain.RoomAsk} {
		require.Nil(t, byKind[kind].RejectReason, kind)
	}
	var verdicts int
	require.NoError(t, f.db.Get(&verdicts, `SELECT count(*) FROM task_verdicts WHERE task_id=$1`, f.task.ID))
	require.Zero(t, verdicts, "the lead's verdict counts for nothing")
	missing, err := NewDevMachineRepository(f.db).MissingVerdict(context.Background(), finished.RunID)
	require.NoError(t, err)
	require.Empty(t, missing, "its turn owes no verdict")

	_, err = f.repo.PostRoomMessage(f.ctx, f.task.WorkspaceID, f.task.ID, domain.TaskRoomPost{Kind: domain.RoomAsk, ToRoles: []string{domain.PipelineLeadRole}, Body: "Where are we?"})
	require.NoError(t, err)
	again, err := f.repo.StartMemberTurn(f.ctx, f.task.ID, domain.PipelineLeadRole, false)
	require.NoError(t, err)
	require.Equal(t, member.ID, again.ID)
	require.Equal(t, *member.MemberTaskID, *again.MemberTaskID, "one task for every turn, so its session resumes")
	next := f.memberTask(again)
	for kind, reason := range refusals {
		require.Contains(t, next.Prompt, fmt.Sprintf("Your post #%d was refused: %s", byKind[kind].Seq, reason))
	}
	require.Contains(t, next.Prompt, "Where are we?")
}
