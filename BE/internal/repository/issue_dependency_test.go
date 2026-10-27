package repository

import (
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func (f automationFixture) block(t *testing.T, blocked, blocker uuid.UUID, fromBlockerSide bool) {
	t.Helper()
	query := `INSERT INTO issue_relations(issue_id,related_issue_id,type) VALUES($1,$2,'blocked_by')`
	if fromBlockerSide {
		// Only the inverse row exists, as when the best-effort inverse failed.
		blocked, blocker = blocker, blocked
		query = `INSERT INTO issue_relations(issue_id,related_issue_id,type) VALUES($1,$2,'blocking')`
	}
	_, err := f.db.Exec(query, blocked, blocker)
	require.NoError(t, err)
}

func (f automationFixture) task(t *testing.T, issue uuid.UUID) *domain.AgentTask {
	t.Helper()
	chat, err := NewConversationRepository(f.db).OpenIssue(f.ctx, f.workspace, issue)
	require.NoError(t, err)
	task, err := NewAgentTaskRepository(f.db).Create(f.ctx, f.workspace, chat.ID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Implement", Provider: "codex"})
	require.NoError(t, err)
	return task
}

func TestDelegatedWorkWaitsForUnfinishedBlockers(t *testing.T) {
	for _, side := range []bool{false, true} {
		t.Run(map[bool]string{false: "blocked_by_row", true: "blocking_row_only"}[side], func(t *testing.T) {
			f := newAutomationFixture(t)
			// other (AUT-2) is blocked by labelled (AUT-1), and by a finished issue.
			f.block(t, f.other, f.labelled, side)
			f.block(t, f.other, f.finished, false)
			tasks := NewAgentTaskRepository(f.db)
			task := f.task(t, f.other)
			require.Equal(t, "queued", task.State)
			require.Equal(t, "Waiting for blocking issues to finish: AUT-1", task.Reason, "finished blockers are not waited for")
			claimed, err := tasks.Claim(f.ctx)
			require.NoError(t, err)
			require.Nil(t, claimed, "a blocked task is not started")

			_, err = f.db.Exec(`UPDATE issues SET status_id=$2 WHERE id=$1`, f.labelled, f.done)
			require.NoError(t, err)
			claimed, err = tasks.Claim(f.ctx)
			require.NoError(t, err)
			require.NotNil(t, claimed, "the task starts once its blocker is finished")
			require.Equal(t, task.ID, claimed.ID)
		})
	}
}

func TestWaitingReasonOnlyNamesSameTeamBlockers(t *testing.T) {
	f := newAutomationFixture(t)
	otherTeam, hidden := uuid.New(), uuid.New()
	_, err := f.db.Exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Private','PRV')`, otherTeam, f.workspace)
	require.NoError(t, err)
	_, err = f.db.Exec(`INSERT INTO team_statuses(team_id,name,slug,category) VALUES($1,'Todo','todo','unstarted')`, otherTeam)
	require.NoError(t, err)
	_, err = f.db.Exec(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,creator_id,status_id) VALUES($1,$2,$3,1,'PRV-1','Secret plan',$4,(SELECT id FROM team_statuses WHERE team_id=$3))`, hidden, f.workspace, otherTeam, f.user)
	require.NoError(t, err)
	f.block(t, f.other, hidden, false)
	task := f.task(t, f.other)
	require.Equal(t, "Waiting for blocking issues to finish: 1 other issue", task.Reason)
	require.NotContains(t, task.Reason, "PRV")
}

func TestAutomationsSkipBlockedIssues(t *testing.T) {
	f := newAutomationFixture(t)
	f.block(t, f.labelled, f.other, false)
	f.rule(t, func(a *domain.AgentAutomation) { a.MaxActive = 5 })
	claimed, err := f.repo.Claim(f.ctx, 10)
	require.NoError(t, err)
	require.Empty(t, claimed, "a blocked match waits")
	_, err = f.db.Exec(`UPDATE issues SET status_id=$2 WHERE id=$1`, f.other, f.done)
	require.NoError(t, err)
	claimed, err = f.repo.Claim(f.ctx, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1, "it is delegated once unblocked")
}

func TestDependencyCyclesAreDetectedTransitively(t *testing.T) {
	f := newAutomationFixture(t)
	deps := NewIssueDependencyRepository(f.db)
	f.block(t, f.other, f.labelled, false) // AUT-2 blocked by AUT-1
	f.block(t, f.finished, f.other, true)  // AUT-3 blocked by AUT-2 (stored from the blocker side)
	cycle, err := deps.Blocks(f.ctx, f.labelled, f.finished)
	require.NoError(t, err)
	require.True(t, cycle, "AUT-1 blocked by AUT-3 would close a cycle")
	cycle, err = deps.Blocks(f.ctx, f.finished, f.labelled)
	require.NoError(t, err)
	require.False(t, cycle)
}

func TestRelatedWorkListsSameTeamNeighboursWithTheirAgentWork(t *testing.T) {
	f := newAutomationFixture(t)
	parent, sibling := uuid.New(), uuid.New()
	for i, id := range []uuid.UUID{parent, sibling} {
		_, err := f.db.Exec(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,creator_id,status_id) VALUES($1,$2,$3,$4::int,'AUT-'||$4::int,$5,$6,$7)`, id, f.workspace, f.team, 10+i, []string{"Epic", "Backend half"}[i], f.user, f.todo)
		require.NoError(t, err)
	}
	_, err := f.db.Exec(`UPDATE issues SET parent_id=$1 WHERE id IN ($2,$3)`, parent, f.labelled, sibling)
	require.NoError(t, err)
	f.block(t, f.labelled, f.other, false)
	f.task(t, sibling)
	work, err := NewIssueDependencyRepository(f.db).RelatedWork(f.ctx, f.labelled)
	require.NoError(t, err)
	byID := map[string]domain.RelatedWork{}
	for _, item := range work {
		byID[item.Identifier] = item
	}
	require.Len(t, byID, 3)
	require.Equal(t, "parent", byID["AUT-10"].Relation)
	require.Equal(t, "sibling", byID["AUT-11"].Relation)
	require.Equal(t, "queued", *byID["AUT-11"].TaskState)
	require.Equal(t, "blocker", byID["AUT-2"].Relation)
	require.Equal(t, "Todo", byID["AUT-2"].Status)
}
