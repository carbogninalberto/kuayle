package repository

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

type automationFixture struct {
	db                        *sqlx.DB
	repo                      *AgentAutomationRepository
	ctx                       context.Context
	workspace, user, team     uuid.UUID
	label, todo, done         uuid.UUID
	labelled, other, finished uuid.UUID
}

func newAutomationFixture(t *testing.T) automationFixture {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db := isolatedPrivacyDatabase(t, url)
	base := newEnvironmentRaceFixture(t, db)
	f := automationFixture{db: db, repo: NewAgentAutomationRepository(db), workspace: base.workspaceID, user: base.userID, team: uuid.New(), label: uuid.New(), todo: uuid.New(), done: uuid.New(), labelled: uuid.New(), other: uuid.New(), finished: uuid.New()}
	f.ctx = domain.WithAccess(context.Background(), domain.Access{WorkspaceID: f.workspace, UserID: f.user})
	exec := func(query string, args ...any) {
		_, err := db.Exec(query, args...)
		require.NoError(t, err)
	}
	exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Auto','AUT')`, f.team, f.workspace)
	exec(`INSERT INTO team_statuses(id,team_id,name,slug,category) VALUES($1,$3,'Todo','todo','unstarted'),($2,$3,'Done','done','completed')`, f.todo, f.done, f.team)
	exec(`INSERT INTO labels(id,workspace_id,name,color) VALUES($1,$2,'agent','#000000')`, f.label, f.workspace)
	for i, issue := range []struct {
		id     uuid.UUID
		status uuid.UUID
	}{{f.labelled, f.todo}, {f.other, f.todo}, {f.finished, f.done}} {
		exec(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,creator_id,status_id,created_at) VALUES($1,$2,$3,$4::int,'AUT-'||$4::int,'Issue',$5,$6,now()+make_interval(secs=>$4::int))`, issue.id, f.workspace, f.team, i+1, f.user, issue.status)
	}
	exec(`INSERT INTO issue_labels(issue_id,label_id) VALUES($1,$3),($2,$3)`, f.labelled, f.finished, f.label)
	return f
}

func (f automationFixture) rule(t *testing.T, mutate func(*domain.AgentAutomation)) *domain.AgentAutomation {
	t.Helper()
	a := &domain.AgentAutomation{WorkspaceID: f.workspace, TeamID: f.team, OwnerID: f.user, Name: "Agent label", LabelID: &f.label, Instructions: "Fix it", MaxActive: 1}
	if mutate != nil {
		mutate(a)
	}
	require.NoError(t, f.repo.Create(f.ctx, a))
	return a
}

func TestAutomationsClaimEachOpenMatchOnceWithinTheBudget(t *testing.T) {
	f := newAutomationFixture(t)
	f.rule(t, func(a *domain.AgentAutomation) { a.MaxActive = 5 })
	claimed, err := f.repo.Claim(f.ctx, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1, "only the open labelled issue matches; completed issues are skipped")
	require.Equal(t, f.labelled, claimed[0].IssueID)
	require.Equal(t, "pending_approval", claimed[0].Status, "rules need approval by default")
	require.Nil(t, claimed[0].ActorID)
	require.Equal(t, "AUT-1", claimed[0].IssueIdentifier)
	again, err := f.repo.Claim(f.ctx, 10)
	require.NoError(t, err)
	require.Empty(t, again, "a match runs at most once")

	// The budget counts pending runs.
	statusRule := f.rule(t, func(a *domain.AgentAutomation) { a.Name, a.LabelID, a.StatusID = "Todo", nil, &f.todo })
	claimed, err = f.repo.Claim(f.ctx, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1, "max_active=1 admits one of the two todo issues")
	require.Equal(t, statusRule.ID, claimed[0].AutomationID)
	claimed, err = f.repo.Claim(f.ctx, 10)
	require.NoError(t, err)
	require.Empty(t, claimed, "the pending run uses the budget")

	// An issue that already has active agent work is not delegated again.
	chat, err := NewConversationRepository(f.db).OpenIssue(f.ctx, f.workspace, f.other)
	require.NoError(t, err)
	_, err = NewAgentTaskRepository(f.db).Create(f.ctx, f.workspace, chat.ID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Already running", Provider: "codex"})
	require.NoError(t, err)
	f.rule(t, func(a *domain.AgentAutomation) {
		a.Name, a.LabelID, a.StatusID, a.MaxActive = "Todo wide", nil, &f.todo, 5
	})
	claimed, err = f.repo.Claim(f.ctx, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.Equal(t, f.labelled, claimed[0].IssueID, "the issue with an active task is skipped")
}

func TestAutomationsHonourTheScheduleAndWorkspacePolicy(t *testing.T) {
	f := newAutomationFixture(t)
	other := (time.Now().UTC().Hour() + 12) % 24
	f.rule(t, func(a *domain.AgentAutomation) { a.RunHour = &other })
	claimed, err := f.repo.Claim(f.ctx, 10)
	require.NoError(t, err)
	require.Empty(t, claimed, "a scheduled rule waits for its hour")
	_, err = f.db.Exec(`UPDATE agent_automations SET run_hour=$1`, time.Now().UTC().Hour())
	require.NoError(t, err)
	_, err = f.db.Exec(`UPDATE dev_machine_workspace_policies SET enabled=false WHERE workspace_id=$1`, f.workspace)
	require.NoError(t, err)
	claimed, err = f.repo.Claim(f.ctx, 10)
	require.NoError(t, err)
	require.Empty(t, claimed, "nothing is claimed while coding agents are disabled")
	_, err = f.db.Exec(`UPDATE dev_machine_workspace_policies SET enabled=true WHERE workspace_id=$1`, f.workspace)
	require.NoError(t, err)
	claimed, err = f.repo.Claim(f.ctx, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
}

func TestUnattendedRulesStartWithoutApprovalOnlyWhenThePolicyAllowsIt(t *testing.T) {
	for _, allowed := range []bool{false, true} {
		t.Run(map[bool]string{false: "policy_off", true: "policy_on"}[allowed], func(t *testing.T) {
			f := newAutomationFixture(t)
			_, err := f.db.Exec(`UPDATE dev_machine_workspace_policies SET allow_unattended_automations=$2 WHERE workspace_id=$1`, f.workspace, allowed)
			require.NoError(t, err)
			f.rule(t, func(a *domain.AgentAutomation) { a.Unattended = true })
			claimed, err := f.repo.Claim(f.ctx, 10)
			require.NoError(t, err)
			require.Len(t, claimed, 1)
			if allowed {
				require.Equal(t, "starting", claimed[0].Status)
				require.Equal(t, f.user, *claimed[0].ActorID, "unattended runs act for the rule owner")
				starting, err := f.repo.Starting(f.ctx, 10)
				require.NoError(t, err)
				require.Len(t, starting, 1)
			} else {
				require.Equal(t, "pending_approval", claimed[0].Status)
			}
		})
	}
}

func TestOnlyTheOwnerOrAnAdminDecidesAndTheApproverActs(t *testing.T) {
	f := newAutomationFixture(t)
	f.rule(t, nil)
	claimed, err := f.repo.Claim(f.ctx, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	stranger, admin := uuid.New(), uuid.New()
	_, err = f.db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Admin','test')`, admin, admin.String()+"@example.test")
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = f.db.Exec(`DELETE FROM users WHERE id=$1`, admin) })
	_, err = f.repo.Decide(f.ctx, f.workspace, claimed[0].ID, stranger, false, true)
	require.ErrorIs(t, err, ErrAutomationNotFound)
	run, err := f.repo.Decide(f.ctx, f.workspace, claimed[0].ID, admin, true, true)
	require.NoError(t, err)
	require.Equal(t, "starting", run.Status)
	require.Equal(t, admin, *run.ActorID, "an approved run acts for the approver")
	_, err = f.repo.Decide(f.ctx, f.workspace, claimed[0].ID, f.user, false, false)
	require.ErrorIs(t, err, ErrAutomationNotFound, "a decided run cannot be decided again")
	_, err = f.repo.Decide(f.ctx, uuid.New(), claimed[0].ID, admin, true, true)
	require.ErrorIs(t, err, ErrAutomationNotFound, "runs are workspace scoped")

	chat, err := NewConversationRepository(f.db).OpenIssue(f.ctx, f.workspace, f.labelled)
	require.NoError(t, err)
	created, err := NewAgentTaskRepository(f.db).Create(f.ctx, f.workspace, chat.ID, domain.AgentTaskRequest{RequestID: run.RequestID, Prompt: "Fix it", Provider: "codex"})
	require.NoError(t, err)
	task := created.ID
	require.NoError(t, f.repo.Finish(f.ctx, run.ID, &task, ""))
	require.NoError(t, f.repo.Finish(f.ctx, run.ID, nil, "late failure"), "a settled run is not changed")
	settled, err := f.repo.Run(f.ctx, f.workspace, run.ID)
	require.NoError(t, err)
	require.Equal(t, "started", settled.Status)
	require.Equal(t, task, *settled.TaskID)
}

func TestAutomationRulesValidateTheirScope(t *testing.T) {
	f := newAutomationFixture(t)
	foreign := uuid.New()
	err := f.repo.Create(f.ctx, &domain.AgentAutomation{WorkspaceID: f.workspace, TeamID: f.team, OwnerID: f.user, Name: "x", StatusID: &foreign, Instructions: "x", MaxActive: 1})
	require.ErrorIs(t, err, ErrAutomationNotFound, "a status must belong to the rule's team")
	err = f.repo.Create(f.ctx, &domain.AgentAutomation{WorkspaceID: uuid.New(), TeamID: f.team, OwnerID: f.user, Name: "x", LabelID: &f.label, Instructions: "x", MaxActive: 1})
	require.ErrorIs(t, err, ErrAutomationNotFound, "the team must belong to the workspace")
}
