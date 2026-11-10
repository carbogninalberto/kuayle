package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/realtime"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// profileTasks creates tasks as AgentTaskService.Create does for an agent
// identity, without a Dev Machine service.
type profileTasks struct {
	repo    *repository.AgentTaskRepository
	creates int
}

func (p *profileTasks) Create(ctx context.Context, ws, conversation uuid.UUID, input domain.AgentTaskRequest) (*domain.AgentTask, error) {
	p.creates++
	applyAgentProfile(&input)
	if accepted, err := p.repo.ReplayCreate(ctx, ws, conversation, input); err != nil || accepted != nil {
		return accepted, err
	}
	return p.repo.Create(ctx, ws, conversation, input)
}

type assigneeFixture struct {
	toolFixture
	svc     *AgentAssigneeService
	repo    *repository.AgentAssigneeRepository
	tasks   *profileTasks
	team    uuid.UUID
	issue   uuid.UUID
	profile uuid.UUID
}

// newAssigneeFixture has one ENG issue and one agent enabled for its team.
func newAssigneeFixture(t *testing.T) assigneeFixture {
	t.Helper()
	f := assigneeFixture{toolFixture: newToolFixture(t), issue: uuid.New()}
	require.NoError(t, f.db.Get(&f.team, `SELECT id FROM teams WHERE workspace_id=$1`, f.ws))
	_, err := f.db.Exec(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,creator_id,status_id) VALUES($1,$2,$3,1,'ENG-1','Flaky import',$4,(SELECT id FROM team_statuses WHERE team_id=$3 LIMIT 1))`, f.issue, f.ws, f.team, f.user)
	require.NoError(t, err)
	require.NoError(t, f.db.Get(&f.profile, `INSERT INTO agent_profiles(workspace_id,name,role,harness,model,effort,instructions) VALUES($1,'Fixer','implementer','goose','','','Always add tests.') RETURNING id`, f.ws))
	_, err = f.db.Exec(`INSERT INTO agent_team_policies(profile_id,team_id,workspace_id) VALUES($1,$2,$3)`, f.profile, f.team, f.ws)
	require.NoError(t, err)
	taskRepo := repository.NewAgentTaskRepository(f.db)
	f.repo, f.tasks = repository.NewAgentAssigneeRepository(f.db), &profileTasks{repo: taskRepo}
	f.svc = NewAgentAssigneeService(f.repo, f.toolFixture.repo, f.tasks, taskRepo, &AgentTaskService{Repo: taskRepo}, realtime.NewHub())
	return f
}

func (f assigneeFixture) member(t *testing.T, role string) (uuid.UUID, context.Context) {
	t.Helper()
	user := uuid.New()
	_, err := f.db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Second member','hash')`, user, user.String()+"@test.invalid")
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = f.db.Exec(`DELETE FROM users WHERE id=$1`, user) })
	_, err = f.db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,$3)`, f.ws, user, role)
	require.NoError(t, err)
	return user, domain.WithAccess(context.Background(), domain.Access{WorkspaceID: f.ws, UserID: user})
}

func (f assigneeFixture) counts(t *testing.T) (assignments, tasks int) {
	t.Helper()
	require.NoError(t, f.db.Get(&assignments, `SELECT count(*) FROM issue_agent_assignees WHERE workspace_id=$1`, f.ws))
	require.NoError(t, f.db.Get(&tasks, `SELECT count(*) FROM agent_tasks WHERE workspace_id=$1`, f.ws))
	return
}

func TestAssigningAnIssueToAnAgentStartsATaskAndShowsItsStatus(t *testing.T) {
	f := newAssigneeFixture(t)
	agents, err := f.svc.SetIssueAgents(f.ctx, f.ws, f.issue, []uuid.UUID{f.profile})
	require.NoError(t, err)
	require.Len(t, agents, 1)
	require.Equal(t, "Fixer", agents[0].Name)
	require.Equal(t, f.user, *agents[0].AssignedBy)
	require.NotNil(t, agents[0].TaskID)
	require.Equal(t, "queued", *agents[0].TaskState, "the issue shows the agent with its task's live state")

	var task domain.AgentTask
	require.NoError(t, f.db.Get(&task, `SELECT * FROM agent_tasks WHERE id=$1`, *agents[0].TaskID))
	require.Equal(t, f.user, task.CreatorID, "the agent acts for the member who assigned it")
	require.Equal(t, f.profile, *task.AgentProfileID)
	require.Equal(t, "goose", task.Provider)
	require.Nil(t, task.WorkflowVersionID)
	require.Contains(t, task.AgentInstructions, "Always add tests.")
	bundle, err := repository.NewAgentTaskRepository(f.db).ProfileBundle(f.ctx, task)
	require.NoError(t, err)
	require.Equal(t, "Fixer", bundle.Name, "the attempt gets the agent's profile files and skills")

	// Assigning again changes nothing and starts nothing.
	_, err = f.svc.SetIssueAgents(f.ctx, f.ws, f.issue, []uuid.UUID{f.profile})
	require.NoError(t, err)
	assignments, tasks := f.counts(t)
	require.Equal(t, 1, assignments)
	require.Equal(t, 1, tasks)

	var history struct {
		UserID   uuid.UUID `db:"user_id"`
		NewValue string    `db:"new_value"`
	}
	require.NoError(t, f.db.Get(&history, `SELECT user_id,new_value FROM issue_history WHERE issue_id=$1 AND field='agent_assignees'`, f.issue))
	require.Equal(t, f.user, history.UserID)
	require.Equal(t, f.profile.String(), history.NewValue)

	// People stay in issue_assignees; the agent is found by the same filter.
	issues := repository.NewIssueRepository(f.db)
	require.NoError(t, issues.SetAssignees(f.ctx, f.issue, []uuid.UUID{f.user}))
	for _, assignee := range []string{f.profile.String(), f.user.String()} {
		params := dto.IssueFilterParams{AssigneeID: assignee, GroupBy: "assignee"}
		params.Defaults()
		found, total, err := issues.List(f.ctx, f.ws, params)
		require.NoError(t, err)
		require.Equal(t, 1, total, assignee)
		require.Equal(t, f.issue, found[0].ID)
	}
	for _, tc := range []struct {
		params dto.IssueFilterParams
		want   int
	}{{dto.IssueFilterParams{AssigneeID: "none"}, 0}, {dto.IssueFilterParams{AgentAssignedBy: f.user.String()}, 1}, {dto.IssueFilterParams{AgentAssignedBy: uuid.NewString()}, 0}} {
		tc.params.Defaults()
		_, total, err := issues.List(f.ctx, f.ws, tc.params)
		require.NoError(t, err)
		require.Equal(t, tc.want, total)
	}
	require.NoError(t, issues.SetAssignees(f.ctx, f.issue, nil))
	params := dto.IssueFilterParams{AssigneeID: "none"}
	params.Defaults()
	_, total, err := issues.List(f.ctx, f.ws, params)
	require.NoError(t, err)
	require.Zero(t, total, "an issue assigned only to an agent is not unassigned")
}

func TestAnAgentCannotBeAssignedWhereItsTeamPolicyIsOff(t *testing.T) {
	f := newAssigneeFixture(t)
	_, err := f.db.Exec(`UPDATE agent_team_policies SET assignable=false WHERE profile_id=$1`, f.profile)
	require.NoError(t, err)
	_, err = f.svc.SetIssueAgents(f.ctx, f.ws, f.issue, []uuid.UUID{f.profile})
	require.ErrorIs(t, err, repository.ErrAgentNotAssignable)

	// No policy row, an archived agent and another workspace's agent alike.
	_, err = f.db.Exec(`DELETE FROM agent_team_policies WHERE profile_id=$1`, f.profile)
	require.NoError(t, err)
	_, err = f.svc.SetIssueAgents(f.ctx, f.ws, f.issue, []uuid.UUID{f.profile})
	require.ErrorIs(t, err, repository.ErrAgentNotAssignable)
	_, err = f.db.Exec(`INSERT INTO agent_team_policies(profile_id,team_id,workspace_id) VALUES($1,$2,$3)`, f.profile, f.team, f.ws)
	require.NoError(t, err)
	_, err = f.db.Exec(`UPDATE agent_profiles SET archived_at=now() WHERE id=$1`, f.profile)
	require.NoError(t, err)
	_, err = f.svc.SetIssueAgents(f.ctx, f.ws, f.issue, []uuid.UUID{f.profile})
	require.ErrorIs(t, err, repository.ErrAgentNotAssignable)

	other := newToolFixture(t)
	var foreign uuid.UUID
	require.NoError(t, other.db.Get(&foreign, `INSERT INTO agent_profiles(workspace_id,name,role,harness) VALUES($1,'Foreign','implementer','goose') RETURNING id`, other.ws))
	_, err = f.svc.SetIssueAgents(f.ctx, f.ws, f.issue, []uuid.UUID{foreign})
	require.ErrorIs(t, err, repository.ErrAgentNotAssignable)

	assignments, tasks := f.counts(t)
	require.Zero(t, assignments)
	require.Zero(t, tasks)
	require.Zero(t, f.tasks.creates)
}

func TestAGuestOrOutsiderCannotAssignAgents(t *testing.T) {
	f := newAssigneeFixture(t)
	_, guest := f.member(t, "guest")
	_, err := f.svc.SetIssueAgents(guest, f.ws, f.issue, []uuid.UUID{f.profile})
	require.ErrorIs(t, err, repository.ErrAgentAssignForbidden)

	outsider := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: f.ws, UserID: uuid.New()})
	_, err = f.svc.SetIssueAgents(outsider, f.ws, f.issue, []uuid.UUID{f.profile})
	require.ErrorIs(t, err, repository.ErrAgentAssignForbidden)

	// A guest cannot unassign (and so cancel) either.
	_, err = f.svc.SetIssueAgents(f.ctx, f.ws, f.issue, []uuid.UUID{f.profile})
	require.NoError(t, err)
	_, err = f.svc.SetIssueAgents(guest, f.ws, f.issue, nil)
	require.ErrorIs(t, err, repository.ErrAgentAssignForbidden)
	assignments, tasks := f.counts(t)
	require.Equal(t, 1, assignments)
	require.Equal(t, 1, tasks)

	// A guest's mention reaches no agent.
	guestID, _ := f.member(t, "guest")
	require.NoError(t, f.svc.HandleComment(context.Background(), f.ws, f.issue, mentionComment(guestID, f.profile)))
	_, tasks = f.counts(t)
	require.Equal(t, 1, tasks)
}

func TestAMemberWhoCannotSeeAPrivateTeamCannotAssignThere(t *testing.T) {
	f := newAssigneeFixture(t)
	_, err := f.db.Exec(`UPDATE teams SET is_private=true WHERE id=$1`, f.team)
	if err != nil {
		t.Skip("teams have no is_private column")
	}
	_, member := f.member(t, "member")
	_, err = f.svc.SetIssueAgents(member, f.ws, f.issue, []uuid.UUID{f.profile})
	require.ErrorIs(t, err, repository.ErrAgentIssueNotFound)
	assigned, err := f.svc.ForIssues(member, []uuid.UUID{f.issue})
	require.NoError(t, err)
	require.Empty(t, assigned)
}

func TestUnassigningCancelsAWorkingTaskAndKeepsOneAwaitingReview(t *testing.T) {
	f := newAssigneeFixture(t)
	assign := func() uuid.UUID {
		agents, err := f.svc.SetIssueAgents(f.ctx, f.ws, f.issue, []uuid.UUID{f.profile})
		require.NoError(t, err)
		return *agents[0].TaskID
	}
	state := func(id uuid.UUID) (state string) {
		require.NoError(t, f.db.Get(&state, `SELECT state FROM agent_tasks WHERE id=$1`, id))
		return
	}
	working := assign()
	other, otherCtx := f.member(t, "member")
	agents, err := f.svc.SetIssueAgents(otherCtx, f.ws, f.issue, nil)
	require.NoError(t, err)
	require.Empty(t, agents)
	require.Equal(t, "cancelling", state(working))
	var actor uuid.UUID
	require.NoError(t, f.db.Get(&actor, `SELECT actor_id FROM agent_task_transitions WHERE task_id=$1 AND to_state='cancelling'`, working))
	require.Equal(t, other, actor, "the transition names the member who unassigned the agent")

	// A new assignment starts a new task; one awaiting review is kept.
	_, err = f.db.Exec(`UPDATE agent_tasks SET state='cancelled' WHERE id=$1`, working)
	require.NoError(t, err)
	review := assign()
	require.NotEqual(t, working, review)
	_, err = f.db.Exec(`UPDATE agent_tasks SET state='awaiting_review' WHERE id=$1`, review)
	require.NoError(t, err)
	_, err = f.svc.SetIssueAgents(f.ctx, f.ws, f.issue, nil)
	require.NoError(t, err)
	require.Equal(t, "awaiting_review", state(review), "the published pull request stays reviewable")
}

func TestMentioningAnAgentStartsOrMessagesItsTask(t *testing.T) {
	f := newAssigneeFixture(t)
	comment := mentionComment(f.user, f.profile)
	require.NoError(t, f.svc.HandleComment(context.Background(), f.ws, f.issue, comment))
	require.NoError(t, f.svc.HandleComment(context.Background(), f.ws, f.issue, comment))
	var task domain.AgentTask
	require.NoError(t, f.db.Get(&task, `SELECT * FROM agent_tasks WHERE workspace_id=$1`, f.ws), "one task for one comment")
	require.Equal(t, f.user, task.CreatorID)
	require.Equal(t, f.profile, *task.AgentProfileID)
	require.Contains(t, task.Prompt, "why does the import fail")
	assignments, _ := f.counts(t)
	require.Zero(t, assignments, "a mention does not assign the agent")

	// A later mention by the same member goes to that open task.
	require.NoError(t, f.svc.HandleComment(context.Background(), f.ws, f.issue, mentionComment(f.user, f.profile)))
	var bodies []string
	require.NoError(t, f.db.Select(&bodies, `SELECT body FROM agent_live_messages WHERE task_id=$1 AND direction='to_agent'`, task.ID))
	require.Len(t, bodies, 1)
	require.Contains(t, bodies[0], "why does the import fail")

	// Another member's mention neither messages that task nor starts one.
	other, _ := f.member(t, "member")
	require.NoError(t, f.svc.HandleComment(context.Background(), f.ws, f.issue, mentionComment(other, f.profile)))
	require.NoError(t, f.db.Select(&bodies, `SELECT body FROM agent_live_messages WHERE task_id=$1 AND direction='to_agent'`, task.ID))
	require.Len(t, bodies, 1)
	_, tasks := f.counts(t)
	require.Equal(t, 1, tasks)

	// An agent switched off for the team is no longer reachable by mention.
	_, err := f.db.Exec(`UPDATE agent_team_policies SET assignable=false WHERE profile_id=$1`, f.profile)
	require.NoError(t, err)
	require.NoError(t, f.svc.HandleComment(context.Background(), f.ws, f.issue, mentionComment(f.user, f.profile)))
	require.NoError(t, f.db.Select(&bodies, `SELECT body FROM agent_live_messages WHERE task_id=$1 AND direction='to_agent'`, task.ID))
	require.Len(t, bodies, 1)
}

func TestOnlyAdministratorsChooseWhereAnAgentCanBeAssigned(t *testing.T) {
	f := newAssigneeFixture(t)
	color := "#3366ff"
	none := []uuid.UUID{}
	// The fixture's user is a member.
	require.ErrorIs(t, f.svc.UpdateIdentity(f.ctx, f.ws, f.profile, AgentIdentityInput{TeamIDs: &none}), repository.ErrAgentIdentityDenied)

	_, admin := f.member(t, "admin")
	require.NoError(t, f.svc.UpdateIdentity(admin, f.ws, f.profile, AgentIdentityInput{AvatarColor: &color, TeamIDs: &none}))
	agents, err := f.svc.Identities(admin, f.ws)
	require.NoError(t, err)
	var fixer *domain.AgentIdentity
	for i := range agents {
		if agents[i].ID == f.profile {
			fixer = &agents[i]
		}
	}
	require.NotNil(t, fixer)
	require.Equal(t, color, fixer.AvatarColor)
	require.Empty(t, fixer.TeamIDs)
	_, err = f.svc.SetIssueAgents(f.ctx, f.ws, f.issue, []uuid.UUID{f.profile})
	require.ErrorIs(t, err, repository.ErrAgentNotAssignable)

	teams := []uuid.UUID{f.team}
	require.NoError(t, f.svc.UpdateIdentity(admin, f.ws, f.profile, AgentIdentityInput{TeamIDs: &teams}))
	_, err = f.svc.SetIssueAgents(f.ctx, f.ws, f.issue, []uuid.UUID{f.profile})
	require.NoError(t, err)

	// A team of another workspace is refused and changes nothing.
	other := newToolFixture(t)
	var foreignTeam uuid.UUID
	require.NoError(t, other.db.Get(&foreignTeam, `SELECT id FROM teams WHERE workspace_id=$1`, other.ws))
	mixed := []uuid.UUID{f.team, foreignTeam}
	require.ErrorIs(t, f.svc.UpdateIdentity(admin, f.ws, f.profile, AgentIdentityInput{TeamIDs: &mixed}), repository.ErrAgentIdentityInvalid)
	var foreign int
	require.NoError(t, f.db.Get(&foreign, `SELECT count(*) FROM agent_team_policies WHERE team_id=$1`, foreignTeam))
	require.Zero(t, foreign)
}

// A task run as an agent identity writes its history rows with the profile
// (AgentTaskRepository.Start); the issue history then names the agent, and
// the row's user stays the member it acted for.
func TestHistoryNamesTheAgentIdentityThatActedForAMember(t *testing.T) {
	f := newAssigneeFixture(t)
	_, err := f.db.Exec(`INSERT INTO issue_history(issue_id,user_id,field,old_value,new_value,agent,agent_profile_id) VALUES($1,$2,'status','Backlog','In Progress','goose',$3)`, f.issue, f.user, f.profile)
	require.NoError(t, err)
	_, err = f.db.Exec(`INSERT INTO issue_history(issue_id,user_id,field,old_value,new_value,agent) VALUES($1,$2,'priority','0','1','codex')`, f.issue, f.user)
	require.NoError(t, err)
	history, err := repository.NewIssueHistoryRepository(f.db).ListByIssue(f.ctx, f.issue)
	require.NoError(t, err)
	require.Len(t, history, 2)
	for _, entry := range history {
		require.Equal(t, f.user, entry.UserID)
		if entry.Field == "status" {
			require.Equal(t, "Fixer", *entry.AgentName)
		} else {
			require.Nil(t, entry.AgentName, "a delegated harness keeps its own name")
		}
	}
}

// The agent is unassigned between the creation of its task and the moment
// the assignment would record it. Unassigning found no task to cancel, so the
// start cancels the task itself rather than leave it running unassigned.
func TestAnAgentUnassignedWhileItsTaskStartsLeavesNoRunningTask(t *testing.T) {
	f := newAssigneeFixture(t)
	added, _, err := f.repo.Set(f.ctx, f.ws, f.issue, f.user, []uuid.UUID{f.profile})
	require.NoError(t, err)
	require.Len(t, added, 1)
	_, removed, err := f.repo.Set(f.ctx, f.ws, f.issue, f.user, nil)
	require.NoError(t, err)
	require.Nil(t, removed[0].TaskID, "unassigning saw no task to cancel")

	require.False(t, f.svc.startAssignment(context.Background(), added[0]))
	var state string
	require.NoError(t, f.db.Get(&state, `SELECT state FROM agent_tasks WHERE workspace_id=$1`, f.ws))
	require.Equal(t, "cancelling", state)

	// A second start of a recorded assignment (worker and request racing)
	// replays the same task and never cancels it.
	agents, err := f.svc.SetIssueAgents(f.ctx, f.ws, f.issue, []uuid.UUID{f.profile})
	require.NoError(t, err)
	var assignment repository.AgentAssignment
	require.NoError(t, f.db.Get(&assignment, `SELECT issue_id,profile_id,workspace_id,assigned_by,request_id FROM issue_agent_assignees WHERE issue_id=$1`, f.issue))
	require.False(t, f.svc.startAssignment(context.Background(), assignment))
	require.NoError(t, f.db.Get(&state, `SELECT state FROM agent_tasks WHERE id=$1`, *agents[0].TaskID))
	require.Equal(t, "queued", state)
}

// A start that keeps failing is retried only within the start window; after
// it the assignment records an error and is no longer picked up, so stuck
// rows cannot crowd out newer ones.
func TestAStartThatNeverSucceedsStopsBeingRetried(t *testing.T) {
	f := newAssigneeFixture(t)
	_, _, err := f.repo.Set(f.ctx, f.ws, f.issue, f.user, []uuid.UUID{f.profile})
	require.NoError(t, err)
	mine := func() (n int) {
		pending, err := f.repo.Pending(f.ctx, 1000)
		require.NoError(t, err)
		for _, assignment := range pending {
			if assignment.IssueID == f.issue {
				n++
			}
		}
		return
	}
	require.Zero(t, mine(), "a start made just now is still the request's own")
	_, err = f.db.Exec(`UPDATE issue_agent_assignees SET created_at=now()-interval '1 minute' WHERE issue_id=$1`, f.issue)
	require.NoError(t, err)
	require.Equal(t, 1, mine(), "an interrupted start is resumed")

	_, err = f.db.Exec(`UPDATE issue_agent_assignees SET created_at=now()-interval '16 minutes' WHERE issue_id=$1`, f.issue)
	require.NoError(t, err)
	require.Zero(t, mine())
	f.svc.resume(context.Background())
	agents, err := f.svc.ForIssues(f.ctx, []uuid.UUID{f.issue})
	require.NoError(t, err)
	require.Nil(t, agents[f.issue][0].TaskID)
	require.Equal(t, "The task could not be started; assign the agent again to retry", *agents[f.issue][0].Error)
	_, tasks := f.counts(t)
	require.Zero(t, tasks)
	require.Zero(t, f.tasks.creates)
}

// Switching a team off for an agent only refuses new assignments and
// mentions: agents already assigned stay assigned and their tasks keep
// running until someone unassigns them.
func TestSwitchingATeamOffKeepsExistingAssignmentsAndTasks(t *testing.T) {
	f := newAssigneeFixture(t)
	agents, err := f.svc.SetIssueAgents(f.ctx, f.ws, f.issue, []uuid.UUID{f.profile})
	require.NoError(t, err)
	_, admin := f.member(t, "admin")
	none := []uuid.UUID{}
	require.NoError(t, f.svc.UpdateIdentity(admin, f.ws, f.profile, AgentIdentityInput{TeamIDs: &none}))

	still, err := f.svc.ForIssues(f.ctx, []uuid.UUID{f.issue})
	require.NoError(t, err)
	require.Len(t, still[f.issue], 1)
	require.Equal(t, "queued", *still[f.issue][0].TaskState)
	// Keeping the same agents is not a new assignment, so it is not refused;
	// unassigning still works and cancels the task.
	_, err = f.svc.SetIssueAgents(f.ctx, f.ws, f.issue, []uuid.UUID{f.profile})
	require.NoError(t, err)
	_, err = f.svc.SetIssueAgents(f.ctx, f.ws, f.issue, nil)
	require.NoError(t, err)
	var state string
	require.NoError(t, f.db.Get(&state, `SELECT state FROM agent_tasks WHERE id=$1`, *agents[0].TaskID))
	require.Equal(t, "cancelling", state)
	_, err = f.svc.SetIssueAgents(f.ctx, f.ws, f.issue, []uuid.UUID{f.profile})
	require.ErrorIs(t, err, repository.ErrAgentNotAssignable)
}
