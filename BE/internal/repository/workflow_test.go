package repository

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func workflowIssueCodes(err error) []string {
	var invalid *domain.WorkflowInvalidError
	if !errors.As(err, &invalid) {
		return nil
	}
	codes := []string{}
	for _, issue := range invalid.Issues {
		codes = append(codes, issue.Code)
	}
	return codes
}

func addWorkflowMember(t *testing.T, db *sqlx.DB, ws uuid.UUID, role string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Workflow test','hash')`, id, id.String()+"@test.invalid")
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,$3)`, ws, id, role)
	require.NoError(t, err)
	return id
}

func builtinIDs(t *testing.T, repo *WorkflowRepository, ws uuid.UUID) (uuid.UUID, uuid.UUID) {
	t.Helper()
	require.NoError(t, repo.EnsureBuiltinProfiles(context.Background(), ws))
	profiles, err := repo.Profiles(context.Background(), ws)
	require.NoError(t, err)
	var implementer, reviewer uuid.UUID
	for _, p := range profiles {
		if p.BuiltinKey != nil && *p.BuiltinKey == domain.AgentRoleImplementer {
			implementer = p.ID
		}
		if p.BuiltinKey != nil && *p.BuiltinKey == domain.AgentRoleReviewer {
			reviewer = p.ID
		}
	}
	require.NotEqual(t, uuid.Nil, implementer)
	require.NotEqual(t, uuid.Nil, reviewer)
	return implementer, reviewer
}

func TestWorkflowSettingsPermissionsValidationAndDefaults(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db := isolatedPrivacyDatabase(t, url)
	ctx := context.Background()
	owner := uuid.New()
	ws, team, project := uuid.New(), uuid.New(), uuid.New()
	_, err := db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Owner','hash')`, owner, owner.String()+"@test.invalid")
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Workflows',$2,$3)`, ws, ws.String(), owner)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner')`, ws, owner)
	require.NoError(t, err)
	admin := addWorkflowMember(t, db, ws, "admin")
	member := addWorkflowMember(t, db, ws, "member")
	guest := addWorkflowMember(t, db, ws, "guest")
	_, err = db.Exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Team','WF')`, team, ws)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO team_statuses(team_id,name,slug,category) VALUES($1,'Todo','todo','unstarted')`, team)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO projects(id,workspace_id,name) VALUES($1,$2,'Project')`, project, ws)
	require.NoError(t, err)
	inProject, plain := uuid.New(), uuid.New()
	_, err = db.Exec(`INSERT INTO issues(id,workspace_id,team_id,project_id,number,identifier_text,title,creator_id,status_id) VALUES
 ($1,$3,$4,$5,1,'WF-1','In project',$6,(SELECT id FROM team_statuses WHERE team_id=$4 LIMIT 1)),
 ($2,$3,$4,NULL,2,'WF-2','No project',$6,(SELECT id FROM team_statuses WHERE team_id=$4 LIMIT 1))`, inProject, plain, ws, team, project, owner)
	require.NoError(t, err)

	repo := NewWorkflowRepository(db)
	implementer, reviewer := builtinIDs(t, repo, ws)
	require.NoError(t, repo.EnsureBuiltinProfiles(ctx, ws))
	profiles, err := repo.Profiles(ctx, ws)
	require.NoError(t, err)
	require.Len(t, profiles, 2, "built-in profiles are created once")

	// Owners and admins edit; members and guests cannot.
	input := domain.AgentProfileInput{Name: "Docs writer", Role: domain.AgentRoleDocsWriter, Harness: "codex"}
	for _, denied := range []uuid.UUID{member, guest, uuid.New()} {
		_, err = repo.CreateProfile(ctx, ws, denied, input)
		require.ErrorIs(t, err, ErrWorkflowDenied)
	}
	docs, err := repo.CreateProfile(ctx, ws, admin, input)
	require.NoError(t, err)
	_, err = repo.CreateProfile(ctx, ws, owner, input)
	require.ErrorIs(t, err, ErrAgentProfileNameTaken)

	graph := domain.ReviewLoopWorkflowGraph(implementer, reviewer)
	raw, err := json.Marshal(graph)
	require.NoError(t, err)
	_, err = repo.CreateWorkflow(ctx, ws, member, "Review loop", "", raw, "created")
	require.ErrorIs(t, err, ErrWorkflowDenied)

	// Invalid workflows cannot be saved.
	invalid := map[string]func(*domain.WorkflowGraph){
		"unreachable": func(g *domain.WorkflowGraph) {
			g.Nodes = append(g.Nodes, domain.WorkflowNode{ID: "orphan", Type: domain.WorkflowNodeEnd, Label: "Orphan", Outcome: domain.WorkflowEndDone})
		},
		"unguarded_loop": func(g *domain.WorkflowGraph) { g.Nodes[3].MaxVisits = 0 },
		"missing_profile": func(g *domain.WorkflowGraph) {
			missing := uuid.New()
			g.Nodes[1].ProfileID = &missing
		},
	}
	for code, mutate := range invalid {
		broken := domain.ReviewLoopWorkflowGraph(implementer, reviewer)
		mutate(&broken)
		brokenRaw, err := json.Marshal(broken)
		require.NoError(t, err)
		_, err = repo.CreateWorkflow(ctx, ws, admin, "Broken "+code, "", brokenRaw, "created")
		require.Contains(t, workflowIssueCodes(err), code)
	}
	// A profile of another workspace is a missing profile too.
	otherWorkspaceProfile := uuid.New()
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Other',$2,$3)`, otherWorkspaceProfile, otherWorkspaceProfile.String(), owner)
	require.NoError(t, err)
	foreign, err := repo.CreateProfile(ctx, otherWorkspaceProfile, owner, input)
	require.ErrorIs(t, err, ErrWorkflowDenied, "the owner is not a member there")
	require.Nil(t, foreign)
	var stored int
	require.NoError(t, db.Get(&stored, `SELECT count(*) FROM workflows WHERE workspace_id=$1`, ws))
	require.Zero(t, stored)

	created, err := repo.CreateWorkflow(ctx, ws, admin, "Implement with review loop", "Example", raw, "created")
	require.NoError(t, err)
	require.Equal(t, 1, created.LatestVersion)
	saved, err := repo.SaveVersion(ctx, ws, created.ID, owner, 1, "Implement with review loop", "Example v2", raw)
	require.NoError(t, err)
	require.Equal(t, 2, saved.LatestVersion)
	_, err = repo.SaveVersion(ctx, ws, created.ID, owner, 1, "Implement with review loop", "", raw)
	require.ErrorIs(t, err, ErrWorkflowStale, "saving over a newer version is refused")
	_, err = repo.SaveVersion(ctx, ws, created.ID, member, 2, "Implement with review loop", "", raw)
	require.ErrorIs(t, err, ErrWorkflowDenied)
	_, err = db.Exec(`UPDATE workflow_versions SET graph='{}' WHERE workflow_id=$1`, created.ID)
	require.Error(t, err, "versions are immutable")
	versions, err := repo.Versions(ctx, ws, created.ID)
	require.NoError(t, err)
	require.Len(t, versions, 2)

	// A profile the workflow uses cannot be archived; an unused one can.
	require.ErrorIs(t, repo.ArchiveProfile(ctx, ws, reviewer, owner), ErrAgentProfileInUse)

	// A saved version's semantics never change under it: the role of a
	// profile any version uses is fixed (a reviewer turned implementer would
	// edit code, and running tasks would find no transition). Other fields
	// and unused profiles stay editable.
	toImplementer := domain.AgentProfileInput{Name: "Reviewer", Role: domain.AgentRoleImplementer, Harness: "codex"}
	_, err = repo.UpdateProfile(ctx, ws, reviewer, owner, toImplementer)
	require.ErrorIs(t, err, ErrAgentProfileRoleInUse)
	var role string
	require.NoError(t, db.Get(&role, `SELECT role FROM agent_profiles WHERE id=$1`, reviewer))
	require.Equal(t, domain.AgentRoleReviewer, role)
	renamed, err := repo.UpdateProfile(ctx, ws, reviewer, owner, domain.AgentProfileInput{Name: "Strict reviewer", Role: domain.AgentRoleReviewer, Harness: "codex", Model: "gpt-5.5"})
	require.NoError(t, err, "the same role with another model is fine")
	require.Equal(t, "gpt-5.5", renamed.Model)
	docsAsTester := input
	docsAsTester.Role = domain.AgentRoleTester
	_, err = repo.UpdateProfile(ctx, ws, docs.ID, owner, docsAsTester)
	require.NoError(t, err, "an unused profile may change role")
	// An archived workflow keeps its versions, which running tasks may pin.
	_, err = db.Exec(`UPDATE workflows SET archived_at=now() WHERE id=$1`, created.ID)
	require.NoError(t, err)
	_, err = repo.UpdateProfile(ctx, ws, reviewer, owner, toImplementer)
	require.ErrorIs(t, err, ErrAgentProfileRoleInUse)
	_, err = db.Exec(`UPDATE workflows SET archived_at=NULL WHERE id=$1`, created.ID)
	require.NoError(t, err)
	require.NoError(t, repo.ArchiveProfile(ctx, ws, docs.ID, owner))

	// Defaults: project, then team, then workspace; none is Standard.
	resolve := func(issue uuid.UUID, chosen *string) *domain.WorkflowVersion {
		t.Helper()
		version, err := repo.ResolveWorkflow(ctx, ws, issue, chosen)
		require.NoError(t, err)
		return version
	}
	require.Nil(t, resolve(plain, nil), "without defaults tasks run Standard")
	require.ErrorIs(t, repo.SetDefault(ctx, ws, member, domain.WorkflowScopeWorkspace, ws, &created.ID, false), ErrWorkflowDenied)
	require.NoError(t, repo.SetDefault(ctx, ws, admin, domain.WorkflowScopeWorkspace, ws, &created.ID, false))
	version := resolve(plain, nil)
	require.NotNil(t, version)
	require.Equal(t, 2, version.Version, "a new task pins the latest version")
	require.NoError(t, repo.SetDefault(ctx, ws, admin, domain.WorkflowScopeTeam, team, nil, false))
	require.Nil(t, resolve(plain, nil), "the team chose Standard")
	require.NoError(t, repo.SetDefault(ctx, ws, admin, domain.WorkflowScopeProject, project, &created.ID, false))
	require.NotNil(t, resolve(inProject, nil), "the project overrides its team")
	require.NoError(t, repo.SetDefault(ctx, ws, admin, domain.WorkflowScopeTeam, team, nil, true))
	require.NotNil(t, resolve(plain, nil), "the team inherits the workspace default again")
	standard := domain.StandardWorkflowID
	require.Nil(t, resolve(plain, &standard), "a task can choose Standard")
	require.ErrorIs(t, repo.SetDefault(ctx, ws, admin, domain.WorkflowScopeTeam, uuid.New(), nil, false), ErrWorkflowScopeNotFound)

	// Archiving clears the defaults that chose it; it can no longer be chosen.
	require.NoError(t, repo.ArchiveWorkflow(ctx, ws, created.ID, owner))
	require.Nil(t, resolve(plain, nil))
	chosen := created.ID.String()
	_, err = repo.ResolveWorkflow(ctx, ws, plain, &chosen)
	require.ErrorIs(t, err, ErrWorkflowNotFound)

	// Every change is audited with its actor.
	var audited int
	require.NoError(t, db.Get(&audited, `SELECT count(*) FROM workflow_audit_events WHERE workspace_id=$1 AND entity='workflow' AND entity_id=$2`, ws, created.ID))
	require.Equal(t, 3, audited, "created, version saved, archived")
	require.NoError(t, db.Get(&audited, `SELECT count(*) FROM workflow_audit_events WHERE workspace_id=$1 AND entity='workflow_default' AND actor_id=$2`, ws, admin))
	require.Equal(t, 4, audited)
}

// TestWorkflowTaskMovesThroughTheRepository applies the runner's moves to a
// real task: the version is pinned at delegation, a reviewer step creates a
// review task, its completion wakes the waiting step, CI results never queue
// Standard follow-ups, and only the creator or an owner/admin decides.
func TestWorkflowTaskMovesThroughTheRepository(t *testing.T) {
	db, repo, ctx, legacy, fixture := taskRecoveryFixture(t)
	require.Nil(t, legacy.WorkflowVersionID, "tasks run Standard unless a workflow is chosen")
	var legacySteps int
	require.NoError(t, db.Get(&legacySteps, `SELECT count(*) FROM agent_task_workflow_steps WHERE task_id=$1`, legacy.ID))
	require.Zero(t, legacySteps)
	_, err := db.Exec(`UPDATE agent_tasks SET state='completed' WHERE id=$1`, legacy.ID)
	require.NoError(t, err)

	ws := legacy.WorkspaceID
	workflows := NewWorkflowRepository(db)
	implementer, reviewer := builtinIDs(t, workflows, ws)
	graph := domain.ReviewLoopWorkflowGraph(implementer, reviewer)
	raw, err := json.Marshal(graph)
	require.NoError(t, err)
	created, err := workflows.CreateWorkflow(ctx, ws, fixture.userID, "Review loop", "", raw, "created")
	require.NoError(t, err)
	chosen := created.ID.String()
	version, err := workflows.ResolveWorkflow(ctx, ws, legacy.IssueID, &chosen)
	require.NoError(t, err)

	// The workspace's default model server speaks Claude Code's protocol:
	// implementing steps use it like the composer and reviews do.
	var server uuid.UUID
	require.NoError(t, db.Get(&server, `INSERT INTO agent_model_servers(workspace_id,name,anthropic_base_url,encrypted_api_key,default_model,is_default) VALUES($1,'Gateway','https://models.example.test','x','opus',true) RETURNING id`, ws))

	task, err := repo.Create(ctx, ws, *legacy.ConversationID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Add dark mode", Provider: "claude-code",
		Workflow: version, WorkflowInstructions: "Agent profile \"Implementer\" (implementer):\nWrite tests."})
	require.NoError(t, err)
	require.Equal(t, version.ID, *task.WorkflowVersionID)
	require.Equal(t, "implement", *task.WorkflowNode)
	require.Equal(t, 2, task.WorkflowSequence)
	require.Contains(t, task.AgentInstructions, "Write tests.")

	// The implementing step's profile gives its instructions and only the
	// skills that are active, enabled and shared with the whole workspace.
	_, err = db.Exec(`UPDATE agent_profiles SET instructions='Write tests.' WHERE id=$1`, implementer)
	require.NoError(t, err)
	var usable, disabled uuid.UUID
	require.NoError(t, db.Get(&usable, `INSERT INTO assistant_skills(workspace_id,name,description,body,status,current_version,author_kind) VALUES($1,'Review SQL','Scope queries','# SQL','active',1,'user') RETURNING id`, ws))
	require.NoError(t, db.Get(&disabled, `INSERT INTO assistant_skills(workspace_id,name,description,body,status,current_version,author_kind,enabled) VALUES($1,'Old','Off','# Old','active',1,'user',false) RETURNING id`, ws))
	_, err = db.Exec(`INSERT INTO agent_profile_skills(profile_id,skill_id,workspace_id,position) VALUES($1,$2,$4,0),($1,$3,$4,1)`, implementer, usable, disabled, ws)
	require.NoError(t, err)
	bundle, err := repo.ProfileBundle(ctx, *task)
	require.NoError(t, err)
	require.Equal(t, "Write tests.", bundle.Instructions)
	require.Equal(t, []domain.AgentProfileSkill{{Name: "Review SQL", Description: "Scope queries", Body: "# SQL"}}, bundle.Skills)
	none, err := repo.ProfileBundle(ctx, legacy)
	require.NoError(t, err)
	require.Nil(t, none, "a Standard task has no profile")
	require.NotNil(t, task.ModelServerID, "the first iteration runs on the default model server")
	require.Equal(t, server, *task.ModelServerID)
	steps, err := repo.WorkflowSteps(ctx, ws, task.ID)
	require.NoError(t, err)
	require.Len(t, steps, 2)
	require.EqualValues(t, 1, *steps[1].Generation, "the first agent step is the delegated iteration")

	claimed := prepareTaskStart(t, db, repo, ctx, *task, fixture)
	_, err = repo.Start(ctx, *claimed)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE dev_machine_agent_runs SET status='succeeded',completed_at=now(),exit_code=0,summary='Added a toggle' WHERE id=$1`, task.RunID)
	require.NoError(t, err)
	run, err := repo.WorkflowRun(ctx, *task)
	require.NoError(t, err)
	require.Equal(t, "succeeded", run.Status)

	cursor := task.Cursor()
	now := time.Now()
	toPublish := graph.Decide(cursor, domain.WorkflowAgentFinished, now, nil)
	apply := WorkflowApply{Sequence: 2, Outcome: domain.WorkflowAgentFinished, Moves: []WorkflowMove{{Decision: toPublish}}, Entry: WorkflowEntry{Kind: WorkflowEnterPublish, Reason: "Workflow · " + toPublish.Reason}}
	moved, err := repo.ApplyWorkflow(ctx, *claimed, apply)
	require.NoError(t, err)
	require.Equal(t, "publishing", moved.State, "a workflow may publish without the Standard check")
	_, err = repo.ApplyWorkflow(ctx, *claimed, apply)
	require.ErrorIs(t, err, ErrAgentTaskConflict, "the same move cannot apply twice")

	// The publication recorded a pull request and commit.
	_, err = db.Exec(`INSERT INTO dev_machine_git_refs(workspace_id,machine_id,agent_run_id,ref_type,repository_full_name,ref_name,commit_sha,pull_request_number,url,source) VALUES
 ($1,$2,$3,'pull_request','fixture/repo','kuayle/wf',NULL,7,'https://github.com/fixture/repo/pull/7','published'),
 ($1,$2,$3,'commit','fixture/repo','kuayle/wf','0123456789abcdef0123456789abcdef01234567',NULL,NULL,'published')`, ws, *claimed.MachineID, task.RunID)
	require.NoError(t, err)
	published, err := repo.HasPublishedWork(ctx, *moved)
	require.NoError(t, err)
	require.True(t, published)

	require.NoError(t, repo.Release(ctx, *claimed), "the worker releases its lease after the step")
	_, err = db.Exec(`UPDATE agent_tasks SET next_poll_at=now() WHERE id=$1`, task.ID) // skip the release back-off
	require.NoError(t, err)
	claimed, err = repo.Claim(ctx)
	require.NoError(t, err)
	require.Equal(t, task.ID, claimed.ID)
	cursor = claimed.Cursor()
	toReview := graph.Decide(cursor, domain.WorkflowPublished, now, nil)
	require.Equal(t, "review", toReview.To.ID)
	moved, err = repo.ApplyWorkflow(ctx, *claimed, WorkflowApply{Sequence: 3, Outcome: domain.WorkflowPublished, Moves: []WorkflowMove{{Decision: toReview}},
		Entry: WorkflowEntry{Kind: WorkflowEnterReview, Reason: "Workflow · " + toReview.Reason, Review: &domain.AgentReviewRequest{Provider: "codex", Model: "gpt-5", Focus: "Be strict."}}})
	require.NoError(t, err)
	require.Equal(t, "awaiting_review", moved.State)
	require.Equal(t, domain.WorkflowStatusWaiting, *moved.WorkflowStatus)
	step, err := repo.CurrentWorkflowStep(ctx, *moved)
	require.NoError(t, err)
	require.NotNil(t, step.ReviewTaskID)
	var child domain.AgentTask
	require.NoError(t, db.Get(&child, `SELECT * FROM agent_tasks WHERE id=$1`, *step.ReviewTaskID))
	require.Equal(t, domain.AgentTaskKindReview, child.Kind)
	require.Equal(t, "codex", child.Provider, "the reviewer runs its own harness")
	require.Nil(t, child.ModelServerID, "the default server does not speak Codex's protocol")
	require.Equal(t, task.ID, *child.ReviewOfTaskID)
	require.Equal(t, task.CreatorID, child.CreatorID, "it runs with the creator's permissions")
	require.Contains(t, child.Prompt, "Be strict.")
	require.NoError(t, repo.Release(ctx, *claimed))
	_, err = db.Exec(`UPDATE agent_tasks SET next_poll_at=now() WHERE id=$1`, task.ID)
	require.NoError(t, err)

	// The waiting parent is not claimed until something wakes it.
	next, err := repo.Claim(ctx)
	require.NoError(t, err)
	require.Equal(t, child.ID, next.ID)
	require.NoError(t, repo.Release(ctx, *next))

	// The review finishing wakes the step waiting for it, in its transaction.
	_, err = db.Exec(`UPDATE agent_reviews SET verdict='request_changes',findings='- Not accessible',completed_at=now() WHERE task_id=$1`, child.ID)
	require.NoError(t, err)
	tx, finished, err := repo.transaction(ctx, child.ID, false)
	require.NoError(t, err)
	require.NoError(t, tx.GetContext(ctx, finished, `UPDATE agent_tasks SET state='running' WHERE id=$1 RETURNING *`, child.ID))
	require.NoError(t, transitionAgentTask(ctx, tx, finished, "completed", "Review finished: request_changes", nil))
	require.NoError(t, tx.Commit())
	review, err := repo.WorkflowReview(ctx, *moved, child.ID)
	require.NoError(t, err)
	require.Equal(t, "completed", review.State)
	require.Equal(t, "request_changes", *review.Verdict)
	woken, err := repo.Claim(ctx)
	require.NoError(t, err)
	require.NotNil(t, woken)
	require.Equal(t, task.ID, woken.ID, "the woken parent is claimed in awaiting_review")
	require.NoError(t, repo.SetWorkflowWake(ctx, *woken, nil))
	require.NoError(t, repo.Release(ctx, *woken))
	findings, err := repo.LatestWorkflowReview(ctx, *moved)
	require.NoError(t, err)
	require.Empty(t, findings, "the reviewer step is still open until the runner leaves it")

	// CI results of a workflow task never queue Standard follow-ups.
	followUps, err := repo.RecordSignal(ctx, ws, "fixture/repo", 7, domain.AgentTaskSignalInput{Kind: "ci", ExternalID: "suite-1", Outcome: "failure", Name: "Tests", HeadSHA: "0123456789abcdef0123456789abcdef01234567"})
	require.NoError(t, err)
	require.Empty(t, followUps)
	ci, err := repo.WorkflowCI(ctx, *moved)
	require.NoError(t, err)
	require.Len(t, ci.Failed, 1)

	// Only the creator or an owner/admin who can see the issue decides.
	member := addWorkflowMember(t, db, ws, "member")
	guest := addWorkflowMember(t, db, ws, "guest")
	admin := addWorkflowMember(t, db, ws, "admin")
	toImplement := graph.Decide(moved.Cursor(), domain.WorkflowReviewChangesRequested, now, nil)
	iteration := WorkflowApply{Sequence: 4, Outcome: domain.WorkflowReviewChangesRequested, Detail: "- Not accessible", Moves: []WorkflowMove{{Decision: toImplement}}, Iteration: true,
		Entry: WorkflowEntry{Kind: WorkflowEnterIteration, Reason: "Workflow · " + toImplement.Reason, Prompt: "Add dark mode\n\n---\nFollow-up from the Kuayle workflow, step Implementer (visit 2):\nFix it", Instructions: "Write tests.", Provider: "claude-code"}}
	for _, denied := range []uuid.UUID{member, guest} {
		_, err = repo.DecideWorkflow(domain.WithAccess(ctx, domain.Access{WorkspaceID: ws, UserID: denied}), ws, task.ID, iteration)
		require.ErrorIs(t, err, ErrConversationNotFound)
	}
	adminCtx := domain.WithAccess(ctx, domain.Access{WorkspaceID: ws, UserID: admin})

	// The time budget running out during the wait holds the task on its open
	// step and pauses it; continuing waits on the same step again with
	// another budget, which starts at the decision.
	_, err = db.Exec(`UPDATE agent_tasks SET workflow_wake_at=now(),next_poll_at=now() WHERE id=$1`, task.ID)
	require.NoError(t, err)
	holder, err := repo.Claim(ctx)
	require.NoError(t, err)
	require.Equal(t, task.ID, holder.ID)
	pause := WorkflowApply{Sequence: 4, Hold: true, Entry: WorkflowEntry{Kind: WorkflowEnterPause, Reason: "Workflow · The workflow's time budget of 60 minutes is used up while Reviewer waits"}}
	withMoves := pause
	withMoves.Moves = []WorkflowMove{{Decision: toImplement}}
	_, err = repo.ApplyWorkflow(ctx, *holder, withMoves)
	require.ErrorIs(t, err, ErrAgentTaskConflict, "a held step has no moves")
	paused, err := repo.ApplyWorkflow(ctx, *holder, pause)
	require.NoError(t, err)
	require.Equal(t, "blocked", paused.State)
	require.Equal(t, domain.WorkflowStatusPaused, *paused.WorkflowStatus)
	require.Equal(t, 4, paused.WorkflowSequence)
	held, err := repo.CurrentWorkflowStep(ctx, *paused)
	require.NoError(t, err)
	require.Nil(t, held.FinishedAt, "the waiting step stays open")
	require.NoError(t, repo.Release(ctx, *holder))
	resume := WorkflowApply{Sequence: 4, Hold: true, ExtendBudget: true, Entry: WorkflowEntry{Kind: WorkflowEnterWait, Reason: "Workflow · continued past the time budget by a person; Reviewer waits again", WakeAt: &now}}
	resumed, err := repo.DecideWorkflow(adminCtx, ws, task.ID, resume)
	require.NoError(t, err)
	require.Equal(t, "awaiting_review", resumed.State)
	require.Equal(t, domain.WorkflowStatusWaiting, *resumed.WorkflowStatus)
	require.Equal(t, 1, resumed.WorkflowExtensions)
	require.NotNil(t, resumed.WorkflowBudgetFrom)
	_, err = repo.DecideWorkflow(adminCtx, ws, task.ID, resume)
	require.ErrorIs(t, err, ErrAgentTaskConflict, "a waiting task is not continued twice")
	held, err = repo.CurrentWorkflowStep(ctx, *resumed)
	require.NoError(t, err)
	require.Nil(t, held.FinishedAt)
	require.Equal(t, admin, *held.DecidedBy)

	stale := iteration
	stale.Sequence = 3
	_, err = repo.DecideWorkflow(adminCtx, ws, task.ID, stale)
	require.ErrorIs(t, err, ErrAgentTaskConflict)
	again, err := repo.DecideWorkflow(adminCtx, ws, task.ID, iteration)
	require.NoError(t, err)
	require.Equal(t, "queued", again.State)
	require.EqualValues(t, 2, again.Generation)
	require.NotNil(t, again.ModelServerID, "a later implementing iteration keeps the default model server")
	require.Equal(t, server, *again.ModelServerID)
	require.NotEqual(t, task.RunID, again.RunID)
	require.Equal(t, 2, again.WorkflowIterations)
	steps, err = repo.WorkflowSteps(ctx, ws, task.ID)
	require.NoError(t, err)
	require.Len(t, steps, 5)
	require.Equal(t, admin, *steps[3].DecidedBy)
	require.Equal(t, domain.WorkflowReviewChangesRequested, *steps[3].Outcome)
	require.Equal(t, "Reviewer → Implementer: review requested changes", steps[4].Reason)
	require.EqualValues(t, 2, *steps[4].Generation)
	var audited int
	require.NoError(t, db.Get(&audited, `SELECT count(*) FROM workflow_audit_events WHERE entity='task_workflow' AND entity_id=$1 AND actor_id=$2`, task.ID, admin))
	require.Equal(t, 2, audited, "continuing and the decision")
	findings, err = repo.LatestWorkflowReview(ctx, *again)
	require.NoError(t, err)
	require.Equal(t, "- Not accessible", findings)

	// A person's retry continues on the implementing step; cancelling closes the workflow.
	require.NoError(t, repo.RequestCancel(ctx, ws, task.ID))
	var status string
	require.NoError(t, db.Get(&status, `SELECT state FROM agent_tasks WHERE id=$1`, task.ID))
	if status == "cancelled" {
		require.NoError(t, db.Get(&status, `SELECT workflow_status FROM agent_tasks WHERE id=$1`, task.ID))
		require.Equal(t, domain.WorkflowStatusFailed, status)
	}
}

// A reviewer step the workflow leaves before its verdict (here a person
// stops the workflow) asks its review task to stop: nothing would read it.
func TestWorkflowStopCancelsTheOpenReview(t *testing.T) {
	db, repo, ctx, legacy, fixture := taskRecoveryFixture(t)
	_, err := db.Exec(`UPDATE agent_tasks SET state='completed' WHERE id=$1`, legacy.ID)
	require.NoError(t, err)
	ws := legacy.WorkspaceID
	workflows := NewWorkflowRepository(db)
	implementer, reviewer := builtinIDs(t, workflows, ws)
	graph := domain.ReviewLoopWorkflowGraph(implementer, reviewer)
	raw, err := json.Marshal(graph)
	require.NoError(t, err)
	created, err := workflows.CreateWorkflow(ctx, ws, fixture.userID, "Review loop", "", raw, "created")
	require.NoError(t, err)
	chosen := created.ID.String()
	version, err := workflows.ResolveWorkflow(ctx, ws, legacy.IssueID, &chosen)
	require.NoError(t, err)
	task, err := repo.Create(ctx, ws, *legacy.ConversationID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Add dark mode", Provider: "claude-code", Workflow: version})
	require.NoError(t, err)
	claimed := prepareTaskStart(t, db, repo, ctx, *task, fixture)
	_, err = repo.Start(ctx, *claimed)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE dev_machine_agent_runs SET status='succeeded',completed_at=now(),exit_code=0 WHERE id=$1`, task.RunID)
	require.NoError(t, err)
	now := time.Now()
	toPublish := graph.Decide(task.Cursor(), domain.WorkflowAgentFinished, now, nil)
	moved, err := repo.ApplyWorkflow(ctx, *claimed, WorkflowApply{Sequence: 2, Outcome: domain.WorkflowAgentFinished, Moves: []WorkflowMove{{Decision: toPublish}}, Entry: WorkflowEntry{Kind: WorkflowEnterPublish, Reason: "Workflow · " + toPublish.Reason}})
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO dev_machine_git_refs(workspace_id,machine_id,agent_run_id,ref_type,repository_full_name,ref_name,commit_sha,pull_request_number,url,source) VALUES
 ($1,$2,$3,'pull_request','fixture/repo','kuayle/wf',NULL,7,'https://github.com/fixture/repo/pull/7','published'),
 ($1,$2,$3,'commit','fixture/repo','kuayle/wf','0123456789abcdef0123456789abcdef01234567',NULL,NULL,'published')`, ws, *claimed.MachineID, task.RunID)
	require.NoError(t, err)
	claimed.WorkflowSequence = moved.WorkflowSequence
	toReview := graph.Decide(moved.Cursor(), domain.WorkflowPublished, now, nil)
	moved, err = repo.ApplyWorkflow(ctx, *claimed, WorkflowApply{Sequence: 3, Outcome: domain.WorkflowPublished, Moves: []WorkflowMove{{Decision: toReview}},
		Entry: WorkflowEntry{Kind: WorkflowEnterReview, Reason: "Workflow · " + toReview.Reason, Review: &domain.AgentReviewRequest{Provider: "codex", Focus: "Be strict."}}})
	require.NoError(t, err)
	step, err := repo.CurrentWorkflowStep(ctx, *moved)
	require.NoError(t, err)
	require.NotNil(t, step.ReviewTaskID)

	reviewNode, _ := graph.Node("review")
	stop := WorkflowApply{Sequence: 4, Moves: []WorkflowMove{{Decision: domain.WorkflowDecision{From: reviewNode, Reason: "Stopped by a person at the workflow's limit"}, Stop: true}},
		Entry: WorkflowEntry{Kind: WorkflowEnterFail, Reason: "Workflow · stopped by a person at its limit"}}
	claimed.WorkflowSequence = moved.WorkflowSequence
	_, err = repo.ApplyWorkflow(ctx, *claimed, stop)
	require.NoError(t, err)
	var state string
	require.NoError(t, db.Get(&state, `SELECT state FROM agent_tasks WHERE id=$1`, *step.ReviewTaskID))
	require.Equal(t, "cancelling", state, "the open review is asked to stop")
}

// A workflow that ends at Failed can be retried: the task goes back to its
// last implementing step with a new iteration, the workflow runs again from
// there, and the step history records the retry.
func TestRetryRestartsAFailedWorkflowOnItsImplementingStep(t *testing.T) {
	db, repo, ctx, legacy, fixture := taskRecoveryFixture(t)
	_, err := db.Exec(`UPDATE agent_tasks SET state='completed' WHERE id=$1`, legacy.ID)
	require.NoError(t, err)
	ws := legacy.WorkspaceID
	workflows := NewWorkflowRepository(db)
	implementer, reviewer := builtinIDs(t, workflows, ws)
	graph := domain.ReviewLoopWorkflowGraph(implementer, reviewer)
	raw, err := json.Marshal(graph)
	require.NoError(t, err)
	created, err := workflows.CreateWorkflow(ctx, ws, fixture.userID, "Retry loop", "", raw, "created")
	require.NoError(t, err)
	chosen := created.ID.String()
	version, err := workflows.ResolveWorkflow(ctx, ws, legacy.IssueID, &chosen)
	require.NoError(t, err)
	task, err := repo.Create(ctx, ws, *legacy.ConversationID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Add dark mode", Provider: "claude-code",
		Workflow: version, WorkflowInstructions: "Agent profile \"Implementer\" (implementer):\nWrite tests."})
	require.NoError(t, err)

	// The implementing agent fails and the workflow ends at Failed.
	claimed := prepareTaskStart(t, db, repo, ctx, *task, fixture)
	_, err = repo.Start(ctx, *claimed)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE dev_machine_agent_runs SET status='failed',completed_at=now(),exit_code=1 WHERE id=$1`, task.RunID)
	require.NoError(t, err)
	toFailed := graph.Decide(claimed.Cursor(), domain.WorkflowAgentFailed, time.Now(), nil)
	require.Equal(t, "failed", toFailed.To.ID)
	failed, err := repo.ApplyWorkflow(ctx, *claimed, WorkflowApply{Sequence: 2, Outcome: domain.WorkflowAgentFailed, Moves: []WorkflowMove{{Decision: toFailed}},
		Entry: WorkflowEntry{Kind: WorkflowEnterFail, Reason: "Workflow · " + toFailed.Reason}})
	require.NoError(t, err)
	require.Equal(t, "cancelling", failed.State, "a failure cleans up the runtime first")
	require.Equal(t, domain.WorkflowStatusFailed, *failed.WorkflowStatus)
	require.Equal(t, "failed", *failed.WorkflowNode)
	// The manager confirms the attempt's runtime cleanup; the task then fails.
	_, err = db.Exec(`INSERT INTO dev_machine_operations(workspace_id,machine_id,agent_run_id,action,status,generation,idempotency_key) VALUES($1,$2,$3,'cancel_agent','completed',1,$4)`,
		ws, *failed.MachineID, failed.RunID, "task-cancel:"+failed.ID.String()+":"+failed.RunID.String())
	require.NoError(t, err)
	require.NoError(t, repo.FinishCancellation(ctx, *claimed, "Cleanup confirmed"))
	failed, err = repo.Get(ctx, ws, task.ID)
	require.NoError(t, err)
	require.Equal(t, "failed", failed.State)

	retried, err := repo.Retry(ctx, ws, task.ID, domain.AgentTaskRetryRequest{RequestID: uuid.New(), Generation: failed.Generation, Prompt: "Add dark mode, and keep the toggle accessible"})
	require.NoError(t, err)
	require.Equal(t, "queued", retried.State)
	require.Equal(t, failed.Generation+1, retried.Generation)
	require.Equal(t, "implement", *retried.WorkflowNode, "the workflow continues on its implementing step")
	require.Equal(t, domain.WorkflowStatusRunning, *retried.WorkflowStatus)
	require.Equal(t, failed.WorkflowSequence+1, retried.WorkflowSequence)
	require.Equal(t, "claude-code", retried.Provider)
	require.Contains(t, retried.Prompt, "keep the toggle accessible")
	require.Contains(t, retried.AgentInstructions, "Write tests.", "the step's profile instructions still apply")
	steps, err := repo.WorkflowSteps(ctx, ws, task.ID)
	require.NoError(t, err)
	last := steps[len(steps)-1]
	require.Equal(t, "implement", last.NodeID)
	require.Equal(t, "Retried by a person", last.Reason)
	require.EqualValues(t, retried.Generation, *last.Generation)
	require.Equal(t, 2, last.Visit, "the retry is the step's second visit")
	bundle, err := repo.ProfileBundle(ctx, *retried)
	require.NoError(t, err)
	require.NotNil(t, bundle, "the retried iteration gets the implementer's profile files")
}
