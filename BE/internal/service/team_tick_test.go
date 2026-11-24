package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	gh "github.com/kuayle/kuayle-backend/pkg/github"
	"github.com/stretchr/testify/require"
)

// Scenario tests of the pipeline tick (KUA-252, the plan's "Scenario tests
// for Phase 2"): a task on the team pipeline runs through the real worker
// step, repository and room, on a database of its own, with GitHub
// (tickGitHub) and the machines that run the turns (tickRuntime) faked.

const tickRepository = "acme/app"

// sha is a full commit name.
func sha(n int) string { return fmt.Sprintf("%040x", n) }

var tickBase = sha(0xba5e)

// tickTemplate is a migrated database each scenario clones, made once per
// test process.
var tickTemplate struct {
	once sync.Once
	name string
	err  error
}

func tickDatabase(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL is not set")
	}
	admin, err := sqlx.Connect("pgx", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { admin.Close() })
	parsed, err := url.Parse(dsn)
	require.NoError(t, err)
	tickTemplate.once.Do(func() {
		name := "tick_template_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		if _, tickTemplate.err = admin.Exec(`CREATE DATABASE "` + name + `"`); tickTemplate.err != nil {
			return
		}
		target := *parsed
		target.Path = "/" + name
		migrator, err := migrate.New("file://../../migrations", target.String())
		if err != nil {
			tickTemplate.err = err
			return
		}
		if err = migrator.Up(); err != nil {
			tickTemplate.err = err
		}
		migrator.Close()
		tickTemplate.name = name
	})
	require.NoError(t, tickTemplate.err)
	clone := "tick_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = admin.Exec(`CREATE DATABASE "` + clone + `" TEMPLATE "` + tickTemplate.name + `"`)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = admin.Exec(`DROP DATABASE IF EXISTS "` + clone + `" WITH (FORCE)`) })
	parsed.Path = "/" + clone
	return parsed.String()
}

// tickGitHub is GitHub for a scenario: one pull request, its checks, the
// update check's answers (prGitHub), the merges and CI re-runs Kuayle asks
// for, and onStatus, which runs while a pass reads the pull request.
type tickGitHub struct {
	*prGitHub
	onStatus func()
	merged   []string
	reruns   [][]int64
}

func (g *tickGitHub) PullRequestStatus(ctx context.Context, ws uuid.UUID, repository string, number int) (*PullRequestStatus, error) {
	if g.onStatus != nil {
		g.onStatus()
	}
	return g.prGitHub.PullRequestStatus(ctx, ws, repository, number)
}

func (g *tickGitHub) MergeTaskPullRequest(_ context.Context, _ uuid.UUID, _ string, _ int, head, _ string) error {
	g.merged = append(g.merged, head)
	g.status.State = "merged"
	return nil
}

func (g *tickGitHub) RerunFailedJobs(_ context.Context, _ uuid.UUID, _ string, runs []int64) error {
	g.reruns = append(g.reruns, runs)
	g.ci("pending", "")
	return nil
}

// ci sets the head's one check: passed, failed (with summary) or pending.
func (g *tickGitHub) ci(state, summary string) {
	g.checks = []PullRequestCheck{{Name: "unit", State: state, URL: "https://github.com/acme/app/actions/runs/77/job/9", Summary: summary}}
}

// tickTurn is what an agent does in a turn: the head the writer commits, its
// final message, a failure, and the room messages it writes (outbox files).
type tickTurn struct {
	commit, final, fail string
	posts               []string
}

type tickWorld struct {
	t       *testing.T
	f       profileFixture
	github  *tickGitHub
	runtime *tickRuntime
	root    uuid.UUID
	machine uuid.UUID
	// agent plays each turn of a role ("implementer" writes); turn counts
	// that role's turns from 1.
	agent func(role string, turn int, task domain.AgentTask) tickTurn
}

func newTickWorld(t *testing.T, edit func(*domain.Pipeline)) *tickWorld {
	t.Helper()
	f := profileFixtureOn(t, tickDatabase(t))
	f.tasks.SetPipelines(true)
	implementer, reviewer, err := f.workflows.builtinProfileIDs(f.ctx, f.ws)
	require.NoError(t, err)
	p := domain.TeamPipeline(implementer, reviewer)
	if edit != nil {
		edit(&p)
	}
	created, err := f.workflows.Create(f.ctx, f.ws, WorkflowRequest{Name: "Team", Graph: mustJSON(t, p)})
	require.NoError(t, err)
	chat, err := repository.NewConversationRepository(f.db).OpenIssue(f.ctx, f.ws, f.issue)
	require.NoError(t, err)
	yes := true
	github := &tickGitHub{prGitHub: &prGitHub{method: "squash", status: PullRequestStatus{State: "open", BaseRef: "main", MergeBase: tickBase, Mergeable: &yes, MergeableState: "clean"}}}
	github.ci("pending", "")
	f.tasks.Workflow().SetPullRequests(github, github)
	f.tasks.Workflow().SetCIChecks(github)
	w := &tickWorld{t: t, f: f, github: github, machine: uuid.New()}
	w.runtime = &tickRuntime{w: w, runs: map[uuid.UUID]*domain.DevMachineAgentRun{}, commits: map[uuid.UUID]string{}, turns: map[string]int{}}
	f.tasks.runtime = w.runtime
	suffix := strings.ReplaceAll(w.machine.String(), "-", "")
	w.exec(`INSERT INTO dev_machines(id,workspace_id,created_by_user_id,routing_key,name,status,desired_status,generation,repo_url,repo_provider,repo_owner,repo_name,base_branch,working_branch,machine_size,cpu_millis,memory_mb,disk_gb)
 VALUES($1,$2,$3,$4,$5,'running','running',1,'','github','','','','','small',1000,2048,20)`, w.machine, f.ws, f.user, suffix[:16], "machine-"+suffix)
	task, err := f.tasks.Create(f.ctx, f.ws, chat.ID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Add a dark mode toggle", Provider: "codex", WorkflowID: &created.ID})
	require.NoError(t, err)
	w.root = task.ID
	return w
}

func (w *tickWorld) exec(query string, args ...any) {
	w.t.Helper()
	_, err := w.f.db.Exec(query, args...)
	require.NoError(w.t, err)
}

// drain runs the worker until no task has anything to do now.
func (w *tickWorld) drain() {
	w.t.Helper()
	for range 300 {
		w.exec(`UPDATE agent_tasks SET next_poll_at=now()`)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		task, err := w.f.tasks.Repo.Claim(ctx)
		require.NoError(w.t, err)
		if task == nil {
			cancel()
			return
		}
		if os.Getenv("TICK_TRACE") != "" {
			w.t.Logf("step %s %s %s %q", task.Kind, task.State, deref(task.WorkflowStatus), task.Reason)
		}
		if err = w.f.tasks.step(ctx, *task); err != nil && !errors.Is(err, repository.ErrAgentTaskConflict) {
			require.NoError(w.t, err, "the step of a %s task in %s", task.Kind, task.State)
		}
		_ = w.f.tasks.store.Release(ctx, *task)
		cancel()
	}
	w.t.Fatal("the tasks did not settle")
}

// later lets the time pass that the waiting tasks wait for, and drains.
func (w *tickWorld) later() {
	w.t.Helper()
	w.exec(`UPDATE agent_tasks SET workflow_wake_at=now() WHERE workflow_wake_at IS NOT NULL`)
	w.drain()
}

func (w *tickWorld) task() domain.AgentTask {
	w.t.Helper()
	var task domain.AgentTask
	require.NoError(w.t, w.f.db.Get(&task, `SELECT * FROM agent_tasks WHERE id=$1`, w.root))
	return task
}

func (w *tickWorld) phase() string { return deref(w.task().WorkflowNode) }

func (w *tickWorld) decide(decision string) error {
	_, err := w.f.workflows.Decide(w.f.ctx, w.f.ws, w.root, w.task().WorkflowSequence, decision)
	return err
}

func (w *tickWorld) room() []domain.TaskMessage {
	w.t.Helper()
	messages, err := w.f.tasks.Repo.RoomMessages(w.f.ctx, w.f.ws, w.root, 0)
	require.NoError(w.t, err)
	return messages
}

// posts are the room's posts of a kind, oldest first.
func (w *tickWorld) posts(kind string) []domain.TaskMessage {
	var out []domain.TaskMessage
	for _, m := range w.room() {
		if m.Kind == kind {
			out = append(out, m)
		}
	}
	return out
}

func (w *tickWorld) facts(name string) []domain.TaskMessage {
	var out []domain.TaskMessage
	for _, m := range w.posts(domain.RoomFact) {
		if m.Data.Fact == name {
			out = append(out, m)
		}
	}
	return out
}

func (w *tickWorld) phases() []string {
	var out []string
	for _, m := range w.posts(domain.RoomPhase) {
		out = append(out, m.Data.Phase)
	}
	return out
}

// prompt is the prompt of a role's turn (1-based) as the agent got it.
func (w *tickWorld) prompt(role string, turn int) string {
	w.t.Helper()
	prompts := w.runtime.prompts[role]
	require.GreaterOrEqual(w.t, len(prompts), turn, "the @%s took %d turns", role, len(prompts))
	return prompts[turn-1]
}

func (w *tickWorld) member(role string) domain.TaskMember {
	w.t.Helper()
	var member domain.TaskMember
	require.NoError(w.t, w.f.db.Get(&member, `SELECT * FROM task_members WHERE task_id=$1 AND role=$2`, w.root, role))
	return member
}

// publish records that Kuayle published head from a run of the root task.
func (w *tickWorld) publish(run uuid.UUID, head string) {
	w.t.Helper()
	w.exec(`INSERT INTO dev_machine_git_refs(workspace_id,machine_id,agent_run_id,ref_type,repository_full_name,ref_name,commit_sha,pull_request_number,url,source) VALUES
 ($1,$2,$3,'pull_request',$4,'kuayle/eng-1',NULL,12,'https://github.com/acme/app/pull/12','published'),
 ($1,$2,$3,'commit',$4,'kuayle/eng-1',$5,NULL,NULL,'published')`, w.f.ws, w.machine, run, tickRepository, head)
	w.head(head)
}

// head moves the pull request's head on GitHub.
func (w *tickWorld) head(head string) {
	w.github.status.HeadSHA, w.github.checksHead = head, head
}

// update makes merge the head GitHub's update-branch makes of from: the
// base's tip merged in, changing nothing else.
func (w *tickWorld) update(from, merge string) {
	tip := sha(0x71b)
	files := []gh.CompareFile{{Filename: "ui/toggle.ts", Status: "modified", Patch: "@@ -1 +1 @@\n-light\n+dark"}}
	if w.github.parents == nil {
		w.github.parents, w.github.onBase, w.github.changes = map[string][]string{}, map[string]bool{}, map[string][]gh.CompareFile{}
	}
	w.github.parents[merge] = []string{from, tip}
	w.github.onBase[tip] = true
	w.github.changes[tip+"..."+merge], w.github.changes[tip+"..."+from] = files, files
}

type tickRuntime struct {
	w       *tickWorld
	runs    map[uuid.UUID]*domain.DevMachineAgentRun
	commits map[uuid.UUID]string
	turns   map[string]int
	prompts map[string][]string
}

func (r *tickRuntime) role(task domain.AgentTask) string {
	if task.Kind != domain.AgentTaskKindReview {
		return "implementer"
	}
	var role string
	require.NoError(r.w.t, r.w.f.db.Get(&role, `SELECT role FROM task_members WHERE member_task_id=$1`, task.ID))
	return role
}

func (r *tickRuntime) Prepare(_ context.Context, task *domain.AgentTask) (bool, error) {
	if task.MachineID == nil {
		r.w.exec(`UPDATE agent_tasks SET machine_id=$2 WHERE id=$1`, task.ID, r.w.machine)
		task.MachineID = &r.w.machine
	}
	return true, nil
}

// Attempt plays the turn: the agent writes to the room while it works, and
// its run ends as it says.
func (r *tickRuntime) Attempt(ctx context.Context, task domain.AgentTask) (*domain.DevMachineAgentRun, error) {
	if run, ok := r.runs[task.RunID]; ok {
		return run, nil
	}
	role := r.role(task)
	r.turns[role]++
	if r.prompts == nil {
		r.prompts = map[string][]string{}
	}
	r.prompts[role] = append(r.prompts[role], task.Prompt)
	turn := r.w.agent(role, r.turns[role], task)
	var files []domain.AgentOutboxFile
	for i, post := range turn.posts {
		files = append(files, domain.AgentOutboxFile{Name: fmt.Sprintf("%02d.json", i), Data: []byte(post)})
	}
	if len(files) > 0 {
		_, _, err := r.w.f.tasks.Repo.RecordRoomOutbox(ctx, task.ID, files)
		require.NoError(r.w.t, err)
	}
	status := domain.DevMachineAgentRunStatusSucceeded
	if turn.fail != "" {
		status = domain.DevMachineAgentRunStatusFailed
	}
	r.w.exec(`INSERT INTO dev_machine_agent_runs(id,workspace_id,machine_id,task_id,requested_by_user_id,provider_id,mode,prompt,command_argv,max_runtime_seconds,status,started_at,completed_at,summary,worktree_changed)
 VALUES($1,$2,$3,$4,$5,$6,'autonomous',$7,'["agent"]',60,$8,now(),now(),$9,$10)`, task.RunID, task.WorkspaceID, r.w.machine, task.ID, task.CreatorID, task.Provider, task.Prompt, status, turn.final, turn.commit != "")
	r.commits[task.RunID] = turn.commit
	now := time.Now()
	run := &domain.DevMachineAgentRun{ID: task.RunID, TaskID: &task.ID, Status: status, StartedAt: &now, Prompt: task.Prompt}
	if turn.fail != "" {
		run.FailureCause = &turn.fail
	}
	r.runs[task.RunID] = run
	return run, nil
}

// Publish pushes the writer's commit and records it, as Kuayle's
// publication does; a turn that committed nothing publishes nothing.
func (r *tickRuntime) Publish(_ context.Context, task domain.AgentTask) (*domain.DevMachineAgentRunStep, error) {
	commit := r.commits[task.RunID]
	if commit == "" {
		summary := domain.NoChangesReason("main")
		return &domain.DevMachineAgentRunStep{Status: domain.DevMachineAgentRunStepStatusSkipped, Summary: &summary}, nil
	}
	r.w.publish(task.RunID, commit)
	return &domain.DevMachineAgentRunStep{Status: domain.DevMachineAgentRunStepStatusSucceeded}, nil
}

// Cancel stops the task's run, as the machine confirms it.
func (r *tickRuntime) Cancel(_ context.Context, task domain.AgentTask) error {
	r.w.exec(`INSERT INTO dev_machine_operations(workspace_id,machine_id,agent_run_id,action,status,generation,idempotency_key)
 SELECT workspace_id,machine_id,id,'cancel_agent','completed',1,$2 FROM dev_machine_agent_runs WHERE id=$1 ON CONFLICT DO NOTHING`, task.RunID, "task-cancel:"+task.ID.String()+":"+task.RunID.String())
	return nil
}

// The roles of the team pipeline as most scenarios play them: the writer
// commits a new head each turn, the reviewer approves.
func (w *tickWorld) writesAndApproves(role string, turn int, _ domain.AgentTask) tickTurn {
	if role == "reviewer" {
		return tickTurn{final: "Verdict: approve\nThe toggle works."}
	}
	return tickTurn{commit: sha(turn), final: "Added the toggle."}
}

// throughReview runs the task until the reviewer approved the first head and
// CI passed on it: it then waits for the owner's approval.
func (w *tickWorld) throughReview() {
	w.t.Helper()
	w.drain()
	require.Equal(w.t, "integrate", w.phase(), "CI still runs once the reviewer approved")
	w.github.ci("passed", "")
	w.later()
	require.Equal(w.t, "approval", w.phase())
}

func TestTickScenario01HappyPath(t *testing.T) {
	w := newTickWorld(t, nil)
	w.agent = w.writesAndApproves
	w.throughReview()
	waiting := w.task()
	require.Equal(t, "awaiting_review", waiting.State)
	require.NotNil(t, waiting.WorkflowHeldSince, "the time budget stops while the owner is awaited")
	require.Nil(t, waiting.WorkflowWakeAt, "nothing polls while a person is awaited")
	require.Contains(t, waiting.Reason, "Nobody has approved merging")

	require.NoError(t, w.decide(WorkflowDecisionApprove))
	w.drain()
	done := w.task()
	require.Equal(t, "completed", done.State)
	require.Equal(t, domain.WorkflowStatusDone, *done.WorkflowStatus)
	require.Equal(t, []string{sha(1)}, w.github.merged)
	require.Equal(t, []string{"pr", "review", "integrate", "approval", "merge"}, w.phases())
	require.Equal(t, 2, done.WorkflowIterations, "the writer's turn and the reviewer's")
	require.Len(t, w.posts(domain.RoomVerdict), 1)
	require.NotEmpty(t, w.facts("pr_exists"))
	require.Len(t, w.facts("approved"), 2, "not approved, then approved")
	require.Equal(t, 1, w.member("reviewer").Turns)
}

func TestTickScenario02ChangesRequestedThenReviewedAgain(t *testing.T) {
	w := newTickWorld(t, nil)
	w.agent = func(role string, turn int, task domain.AgentTask) tickTurn {
		if role == "reviewer" && turn == 1 {
			return tickTurn{final: "Verdict: request_changes\nThe toggle is not persisted across reloads."}
		}
		return w.writesAndApproves(role, turn, task)
	}
	w.drain()
	// The findings reached the writer, whose next turn resumed its task;
	// the reviewer's next turn is the same task, on the new head, and is
	// told what changed since the commit it reviewed.
	require.Contains(t, w.prompt("implementer", 2), "The toggle is not persisted across reloads.")
	require.Contains(t, w.prompt("implementer", 2), "@implementer")
	root := w.task()
	require.EqualValues(t, 2, root.Generation, "one task: the writer's second turn is its next generation")
	reviewer := w.member("reviewer")
	require.Equal(t, 2, reviewer.Turns)
	var generation int64
	require.NoError(t, w.f.db.Get(&generation, `SELECT generation FROM agent_tasks WHERE id=$1`, *reviewer.MemberTaskID))
	require.EqualValues(t, 2, generation, "the reviewer's task is reused, so its session resumes")
	require.Contains(t, w.prompt("reviewer", 2), fmt.Sprintf("git diff %s %s", sha(1), sha(2)))
	require.Equal(t, sha(2), *reviewer.PinnedCommit)
	require.Equal(t, "integrate", w.phase())
}

func TestTickScenario03FailingCIReachesTheWriterWithDetails(t *testing.T) {
	w := newTickWorld(t, nil)
	w.agent = func(role string, turn int, task domain.AgentTask) tickTurn {
		if role == "implementer" && turn == 2 {
			return tickTurn{final: "Looked at the failure."}
		}
		return w.writesAndApproves(role, turn, task)
	}
	w.drain()
	w.github.ci("failed", "TestDarkMode: expected the dark palette, got light")
	w.later()
	ci := w.facts("ci_green")
	require.Contains(t, ci[len(ci)-1].Body, "TestDarkMode: expected the dark palette")
	prompt := w.prompt("implementer", 2)
	require.Contains(t, prompt, "TestDarkMode: expected the dark palette", "the failing check's details")
	require.Contains(t, prompt, "1 check(s) failed on", "and the unmet check with its next step")
}

func TestTickScenario04BaseMovesAfterApprovalThenMerges(t *testing.T) {
	w := newTickWorld(t, nil)
	w.agent = w.writesAndApproves
	w.throughReview()
	require.NoError(t, w.decide(WorkflowDecisionApprove))
	// The base moved before Kuayle merged: the branch is behind, cleanly.
	merge := sha(0x3e)
	w.update(sha(1), merge)
	w.github.status.Behind = 2
	w.github.updateHead = merge
	w.github.onUpdate = func() {
		w.github.status.Behind = 0
		w.head(merge)
		w.github.ci("pending", "")
	}
	w.drain()
	require.Contains(t, w.github.calls, "update:"+sha(1))
	require.Empty(t, w.github.merged, "CI runs on the updated head first")
	require.Equal(t, "integrate", w.phase())
	w.github.ci("passed", "")
	w.later()
	require.Equal(t, []string{merge}, w.github.merged, "the reviewer's verdict and the approval still hold for the base merged in")
	require.Equal(t, "completed", w.task().State)
	require.Equal(t, 1, w.member("reviewer").Turns, "nobody reviews the base again")
}

func TestTickScenario05ConflictReachesTheWriter(t *testing.T) {
	w := newTickWorld(t, nil)
	w.agent = func(role string, turn int, task domain.AgentTask) tickTurn {
		if role == "implementer" && turn == 2 {
			return tickTurn{final: "Merging main."}
		}
		return w.writesAndApproves(role, turn, task)
	}
	w.throughReview()
	no := false
	w.github.status.Behind, w.github.status.Mergeable, w.github.status.MergeableState = 3, &no, "dirty"
	w.github.onStatus = nil
	w.exec(`UPDATE agent_tasks SET workflow_wake_at=now() WHERE id=$1`, w.root) // the base's push wakes it
	w.drain()
	require.NotContains(t, strings.Join(w.github.calls, " "), "update:", "a conflicting branch is not updated")
	conflict := w.facts("conflict")
	require.Contains(t, conflict[len(conflict)-1].Body, "conflicts with main")
	require.Contains(t, w.prompt("implementer", 2), "conflicts with main")
}

func TestTickScenario06MigrationCollisionIsAFact(t *testing.T) {
	w := newTickWorld(t, nil)
	w.agent = func(role string, turn int, task domain.AgentTask) tickTurn {
		if role == "implementer" && turn == 2 {
			return tickTurn{final: "Renumbering."}
		}
		return w.writesAndApproves(role, turn, task)
	}
	// Another task merged a migration with the same number first.
	const repair = "Migration 000208 of this branch has the number of 000208_other_feature on main. Next step: renumber it to 000209."
	w.github.status.migrationRepair = repair
	w.throughReview()
	collision := w.facts("migration_collision")
	require.Len(t, collision, 1)
	require.JSONEq(t, `true`, string(collision[0].Data.Value))
	require.Contains(t, collision[0].Body, repair)
	// The merge gate refuses it, and the reason reaches the writer.
	require.NoError(t, w.decide(WorkflowDecisionApprove))
	w.drain()
	require.Empty(t, w.github.merged)
	require.Contains(t, w.prompt("implementer", 2), repair)
}

func TestTickScenario07QuestionAndAnswerResumeSessions(t *testing.T) {
	w := newTickWorld(t, nil)
	var asked int64
	w.agent = func(role string, turn int, task domain.AgentTask) tickTurn {
		switch {
		case role == "implementer" && turn == 1:
			return tickTurn{commit: sha(1), final: "Added the toggle.", posts: []string{`{"to":"@reviewer","kind":"ask","body":"Should the toggle persist per user or per device?"}`}}
		case role == "reviewer":
			for _, m := range w.room() {
				if m.Kind == domain.RoomAsk {
					asked = m.Seq
				}
			}
			return tickTurn{final: "Verdict: approve\nFine.", posts: []string{fmt.Sprintf(`{"to":"@implementer","body":"Per user: store it in the profile.","reply_to":%d}`, asked)}}
		}
		return tickTurn{final: "Stored it per user."}
	}
	w.drain()
	require.Contains(t, w.prompt("reviewer", 1), "Should the toggle persist per user or per device?")
	answer := w.prompt("implementer", 2)
	require.Contains(t, answer, "Per user: store it in the profile.")
	require.Contains(t, answer, fmt.Sprintf("replying to #%d", asked))

	// Each next turn continues its session, on every harness that writes or
	// reviews in a team (the launch carries its resume flag).
	for provider, resume := range map[string][]string{
		"claude-code": {"--resume", "0c9a6c1e-0000-4000-8000-000000000001"},
		"codex":       {"resume", "0199a213-81c0-7800-8aa1-bbab2a035a53"},
		"opencode":    {"--session", "ses_df21986abffefn2FxDuH6uZGLA"},
	} {
		for _, member := range []bool{true, false} {
			run := turnRun(t, provider, resume[1], member)
			var argv []string
			require.NoError(t, json.Unmarshal(run.CommandArgv, &argv))
			require.Subset(t, argv, resume, "%s (member %v) launches with its resume flag", provider, member)
		}
	}
}

// turnRun is the run Kuayle launches for a task's second turn on provider,
// after a first one in session: a member's turn, or the writer's.
func turnRun(t *testing.T, provider, session string, member bool) domain.DevMachineAgentRun {
	t.Helper()
	if member {
		run, _ := reviewTurn(t, provider, session, true)
		return run
	}
	ws, machine, user, checkout := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	store := &taskPolicyStore{devMachineStoreFake: &devMachineStoreFake{
		policy:        testPolicy(ws),
		machine:       &domain.DevMachine{ID: machine, WorkspaceID: ws, CreatedByUserID: &user, Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning},
		agentProvider: &domain.DevMachineAgentProvider{ProviderID: provider, Enabled: true, RequiredSecrets: json.RawMessage(`[]`), Config: json.RawMessage(`{}`)},
		checkouts:     []domain.DevMachineCheckout{{ID: checkout, WorkspaceID: ws, MachineID: machine, Status: "ready", WorkspacePath: "/workspace/tasks/eng-1", RepositoryFullName: "example/test", BaseBranch: "main", WorkingBranch: "kuayle/eng-1"}},
	}}
	store.policy.AllowedProviders = json.RawMessage(`["opencode","codex","claude-code"]`)
	machines := newTestDevMachineService(store)
	machines.agents = agent.NewRegistry(agent.NewOpenCodeProvider(""), agent.NewCodexProvider(""), agent.NewClaudeCodeProvider(""))
	tasks := &taskRuntimeStoreFake{previous: &domain.AgentHandoff{AttemptID: uuid.New(), Status: "succeeded", MachineID: machine, NativeSessionID: session}}
	task := domain.AgentTask{ID: uuid.New(), WorkspaceID: ws, CreatorID: user, MachineID: &machine, CheckoutID: checkout, RunID: uuid.New(), State: "preparing", Provider: provider,
		Prompt: "Add a dark mode toggle", Kind: domain.AgentTaskKindImplement, Generation: 2, Pipeline: true}
	_, err := (&devMachineTaskRuntime{machines: machines, tasks: tasks}).Attempt(context.Background(), task)
	require.NoError(t, err)
	require.Len(t, store.agentRuns, 1)
	return store.agentRuns[0]
}

func TestTickScenario08NoProgressPausesForTheOwner(t *testing.T) {
	w := newTickWorld(t, nil)
	w.agent = func(role string, turn int, task domain.AgentTask) tickTurn {
		switch {
		case role == "reviewer":
			return tickTurn{final: "Verdict: request_changes\nHandle the system theme."}
		case turn == 1:
			return tickTurn{commit: sha(1), final: "Added the toggle."}
		}
		return tickTurn{final: "I am not sure what to change."} // no new commit
	}
	w.drain()
	paused := w.task()
	require.Equal(t, "blocked", paused.State)
	require.Equal(t, domain.WorkflowStatusPaused, *paused.WorkflowStatus)
	require.Contains(t, paused.Reason, "Nobody made progress on")
	require.Contains(t, paused.Reason, "Continue or stop the workflow")
	summaries := w.posts(domain.RoomSummary)
	require.Len(t, summaries, 2, "the writer was sent the unmet checks twice")
	for _, summary := range summaries {
		require.Equal(t, []string{"implementer"}, []string(summary.ToRoles))
		require.Contains(t, summary.Body, "did not approve")
	}
	require.Equal(t, 3, w.runtime.turns["implementer"])

	// Continuing counts again from nothing: two more sends, then a pause.
	require.NoError(t, w.decide(WorkflowDecisionContinue))
	w.drain()
	require.Equal(t, 5, w.runtime.turns["implementer"])
	require.Len(t, w.posts(domain.RoomSummary), 4)
	require.Equal(t, domain.WorkflowStatusPaused, *w.task().WorkflowStatus)
}

func TestTickScenario09RefusedPostIsToldOnTheNextTurn(t *testing.T) {
	w := newTickWorld(t, nil)
	w.agent = func(role string, turn int, task domain.AgentTask) tickTurn {
		if role == "reviewer" && turn == 1 {
			// Only Kuayle (or a lead) moves phases.
			return tickTurn{final: "Verdict: request_changes\nPersist it.", posts: []string{`{"to":"@all","kind":"phase","data":{"phase":"merge"}}`}}
		}
		return w.writesAndApproves(role, turn, task)
	}
	w.drain()
	var refused *domain.TaskMessage
	for _, m := range w.room() {
		if m.RejectReason != nil {
			refused = &m
		}
	}
	require.NotNil(t, refused)
	require.Equal(t, "the reviewer may not post phase", *refused.RejectReason)
	require.NotContains(t, w.prompt("implementer", 2), "may not post phase", "a refused post reaches nobody else")
	require.Contains(t, w.prompt("reviewer", 2), fmt.Sprintf("Your post #%d was refused: the reviewer may not post phase", refused.Seq))
}

func TestTickScenario10OlderVerdictHoldsOnlyAcrossMergesOfTheBase(t *testing.T) {
	w := newTickWorld(t, nil)
	w.agent = w.writesAndApproves
	w.drain()
	require.Equal(t, "integrate", w.phase())
	// Kuayle's verified update of the reviewed commit: the verdict holds.
	merge := sha(0x3e)
	w.update(sha(1), merge)
	w.head(merge)
	w.later()
	require.Equal(t, "integrate", w.phase())
	require.Equal(t, 1, w.member("reviewer").Turns)
	// The owner asks for more; the writer's new commit is not a merge of the
	// base, so the verdict on the older one no longer holds and the
	// reviewer is asked for the new head.
	_, err := w.f.tasks.Repo.PostRoomMessage(w.f.ctx, w.f.ws, w.root, domain.TaskRoomPost{Kind: domain.RoomSay, Body: "Also follow the system theme.", ToRoles: []string{"implementer"}})
	require.NoError(t, err)
	w.drain()
	require.Contains(t, w.prompt("implementer", 2), "Also follow the system theme.")
	reviewer := w.member("reviewer")
	require.Equal(t, 2, reviewer.Turns)
	require.Equal(t, sha(2), *reviewer.PinnedCommit)
	require.Contains(t, w.prompt("reviewer", 2), "commit "+sha(2))
}

func TestTickScenario11BudgetAsksOrFails(t *testing.T) {
	for _, policy := range []string{domain.WorkflowOnLimitAsk, domain.WorkflowOnLimitFail} {
		t.Run(policy, func(t *testing.T) {
			w := newTickWorld(t, func(p *domain.Pipeline) {
				p.Budget = domain.PipelineBudget{MaxTurns: 1, MaxMinutes: 60, OnExhausted: policy}
			})
			w.agent = w.writesAndApproves
			w.drain()
			task := w.task()
			require.Contains(t, task.Reason, "budget of 1 turns is used up")
			require.Zero(t, w.runtime.turns["reviewer"], "no turn beyond the budget")
			if policy == domain.WorkflowOnLimitFail {
				require.Equal(t, "failed", task.State)
				// A person retrying it starts the writer again, on another budget.
				_, err := w.f.tasks.Repo.Retry(w.f.ctx, w.f.ws, w.root, domain.AgentTaskRetryRequest{RequestID: uuid.New(), Generation: task.Generation, Prompt: "Try again"})
				require.NoError(t, err)
				w.drain()
				require.Equal(t, 2, w.runtime.turns["implementer"])
				require.Equal(t, 1, w.task().WorkflowExtensions)
				return
			}
			require.Equal(t, "blocked", task.State)
			require.Equal(t, domain.WorkflowStatusPaused, *task.WorkflowStatus)
			// Continuing grants another budget of the same size.
			require.NoError(t, w.decide(WorkflowDecisionContinue))
			w.drain()
			require.Equal(t, 1, w.runtime.turns["reviewer"])
			require.Equal(t, 1, w.task().WorkflowExtensions)
		})
	}
}

func TestTickScenario12UnreadableFactWaits(t *testing.T) {
	w := newTickWorld(t, nil)
	w.agent = w.writesAndApproves
	// GitHub fails once the pull request exists.
	w.github.onStatus = func() {
		if w.github.status.HeadSHA != "" {
			w.github.readErr = errors.New("GitHub answered 502")
		}
	}
	w.drain()
	waiting := w.task()
	require.Equal(t, "awaiting_review", waiting.State)
	require.NotNil(t, waiting.WorkflowWakeAt, "the tick reads it again shortly")
	require.Empty(t, w.facts("head_sha"), "nothing is posted")
	require.Empty(t, w.posts(domain.RoomSummary), "nothing is sent anywhere")
	require.Zero(t, w.runtime.turns["reviewer"], "nobody is asked")
	require.Equal(t, 1, w.runtime.turns["implementer"])

	w.github.onStatus, w.github.readErr = nil, nil
	w.later()
	require.Len(t, w.facts("head_sha"), 1)
	require.Equal(t, 1, w.runtime.turns["reviewer"])
}

// A wake that lands while a pass runs (a webhook, a member's turn ending)
// is not lost to the wait that pass schedules: the task is looked at again.
func TestTickKeepsAWakeThatLandsDuringAPass(t *testing.T) {
	w := newTickWorld(t, nil)
	w.agent = w.writesAndApproves
	w.throughReview()
	require.Nil(t, w.task().WorkflowWakeAt, "waiting for the owner, nothing polls")
	passes := 0
	w.github.onStatus = func() {
		passes++
		if passes == 1 {
			w.exec(`UPDATE agent_tasks SET workflow_wake_at=now() WHERE id=$1`, w.root) // the webhook
		}
	}
	w.exec(`UPDATE agent_tasks SET workflow_wake_at=now()-interval '1 second' WHERE id=$1`, w.root)
	w.drain()
	require.Equal(t, 2, passes, "the wake that landed during the first pass ran a second one")
	require.Nil(t, w.task().WorkflowWakeAt)
}

// A member's turn that fails does not hold the task up: the failure is a
// fact the owner of the phase is sent, and the member takes its next turn
// once a person continues.
func TestTickAFailedMemberTurnIsAFact(t *testing.T) {
	w := newTickWorld(t, nil)
	w.agent = func(role string, turn int, task domain.AgentTask) tickTurn {
		switch {
		case role == "reviewer" && turn == 1:
			return tickTurn{fail: "The reviewer's harness crashed"}
		case role == "implementer" && turn > 1:
			return tickTurn{final: "Nothing to change."}
		}
		return w.writesAndApproves(role, turn, task)
	}
	w.drain()
	failed := w.facts("turn")
	require.Len(t, failed, 1)
	require.Contains(t, failed[0].Body, "The reviewer's harness crashed")
	require.Contains(t, w.prompt("implementer", 2), "The reviewer's harness crashed")
	require.Equal(t, domain.WorkflowStatusPaused, *w.task().WorkflowStatus, "nobody made progress")
	require.NoError(t, w.decide(WorkflowDecisionContinue))
	w.drain()
	require.Equal(t, 2, w.runtime.turns["reviewer"], "the reviewer is asked again")
	require.Equal(t, "integrate", w.phase())
}

// A pull request merged on GitHub completes its pipeline task through the
// merge webhook, as it does any task waiting for review.
func TestTickPullRequestMergedOnGitHubCompletesTheTask(t *testing.T) {
	w := newTickWorld(t, nil)
	w.agent = w.writesAndApproves
	w.throughReview()
	completed, err := w.f.tasks.Repo.CompleteMergedPullRequest(context.Background(), w.f.ws, tickRepository, 12)
	require.NoError(t, err)
	require.Equal(t, 1, completed)
	task := w.task()
	require.Equal(t, "completed", task.State)
	require.Equal(t, domain.WorkflowStatusDone, *task.WorkflowStatus)
}
