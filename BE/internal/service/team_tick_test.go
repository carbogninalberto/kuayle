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
	"github.com/lib/pq"
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
	// slow makes reading the pull request take that long, unless the
	// caller gives up first.
	slow   time.Duration
	merged []string
	reruns [][]int64
}

func (g *tickGitHub) PullRequestStatus(ctx context.Context, ws uuid.UUID, repository string, number int) (*PullRequestStatus, error) {
	if g.onStatus != nil {
		g.onStatus()
	}
	if g.slow > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(g.slow):
		}
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
	// hold keeps the turn's agent working until the test finishes it.
	hold bool
}

type tickWorld struct {
	t       *testing.T
	f       profileFixture
	github  *tickGitHub
	runtime *tickRuntime
	root    uuid.UUID
	machine uuid.UUID
	// step is the time the worker gives each step (20 s, as the server).
	step time.Duration
	// agent plays each turn of a role ("implementer" writes); turn counts
	// that role's turns from 1.
	agent func(role string, turn int, task domain.AgentTask) tickTurn
	// afterStep, when set, runs after each step the worker takes.
	afterStep func(task domain.AgentTask)
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
	w := &tickWorld{t: t, f: f, github: github, machine: uuid.New(), step: 20 * time.Second}
	w.runtime = &tickRuntime{w: w, runs: map[uuid.UUID]*domain.DevMachineAgentRun{}, commits: map[uuid.UUID]string{}, turns: map[string]int{}, held: map[uuid.UUID]string{}}
	f.tasks.runtime = w.runtime
	suffix := strings.ReplaceAll(w.machine.String(), "-", "")
	w.exec(`INSERT INTO dev_machines(id,workspace_id,created_by_user_id,routing_key,name,status,desired_status,generation,repo_url,repo_provider,repo_owner,repo_name,base_branch,working_branch,machine_size,cpu_millis,memory_mb,disk_gb)
 VALUES($1,$2,$3,$4,$5,'running','running',1,'','github','','','','','small',1000,2048,20)`, w.machine, f.ws, f.user, suffix[:16], "machine-"+suffix)
	task, err := f.tasks.Create(f.ctx, f.ws, chat.ID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Add a dark mode toggle", Provider: "codex", WorkflowID: &created.ID})
	require.NoError(t, err)
	w.root = task.ID
	// The root task's worktree on the machine, where its checks run.
	installation, repo := uuid.New(), uuid.New()
	w.exec(`INSERT INTO dev_machine_workspace_policies(workspace_id,enabled) VALUES($1,true) ON CONFLICT (workspace_id) DO NOTHING`, f.ws)
	w.exec(`INSERT INTO github_installations(id,workspace_id,installation_id,account_login,account_type,installed_by) VALUES($1,$2,1,'acme','Organization',$3)`, installation, f.ws, f.user)
	w.exec(`INSERT INTO github_repos(id,installation_id,workspace_id,github_repo_id,full_name,default_branch) VALUES($1,$2,$3,1,$4,'main')`, repo, installation, f.ws, tickRepository)
	w.exec(`INSERT INTO dev_machine_checkouts(id,workspace_id,machine_id,issue_id,github_repo_id,repository_full_name,base_branch,working_branch,workspace_path,status,scope_key) VALUES($1,$2,$3,$4,$5,$6,'main','kuayle/eng-1','/workspace/tasks/eng-1','ready',$7)`,
		task.CheckoutID, f.ws, w.machine, f.issue, repo, tickRepository, task.ID.String())
	return w
}

// runCheck plays the machine running the check Kuayle queued: it ends with
// exit and output, through the manual check runner's own records.
func (w *tickWorld) runCheck(exit int, output string) {
	w.t.Helper()
	store := repository.NewDevMachineRepository(w.f.db)
	ctx := context.Background()
	ops, err := store.LeaseOperations(ctx, "tick-check-fixture", 1, time.Minute)
	require.NoError(w.t, err)
	require.Len(w.t, ops, 1)
	op := ops[0]
	require.Equal(w.t, domain.DevMachineOpRunCheck, op.Action)
	step := uuid.MustParse(strings.TrimPrefix(op.IdempotencyKey, "task-check:"))
	require.NoError(w.t, store.CreateRuntimeService(ctx, &domain.DevMachineService{ID: step, WorkspaceID: op.WorkspaceID, MachineID: op.MachineID, AgentRunID: op.AgentRunID, ServiceType: "agent",
		ServiceKey: "check-" + step.String(), ContainerName: "check", ImageRef: "fixture", InternalHost: "check", InternalPort: 8080, Status: "pending", HealthStatus: "unknown"}))
	require.NoError(w.t, store.StartManualCheck(ctx, op, "container"))
	require.NoError(w.t, store.CreateManualCheckOutput(ctx, op, &domain.DevMachineLogChunk{WorkspaceID: op.WorkspaceID, MachineID: op.MachineID, AgentRunID: op.AgentRunID, ServiceID: &step, Stream: "stdout", Sequence: 1, Content: output, SourceKey: "check"}))
	require.NoError(w.t, store.CompleteManualCheck(ctx, op, exit))
	require.NoError(w.t, store.CompleteOperation(ctx, op.ID, *op.LeaseOwner))
}

// checks are the script checks Kuayle queued for the root task, oldest first.
func (w *tickWorld) checks() []string {
	w.t.Helper()
	var argv []string
	require.NoError(w.t, w.f.db.Select(&argv, `SELECT s.command_argv::text FROM dev_machine_agent_run_steps s JOIN dev_machine_agent_runs r ON r.id=s.agent_run_id WHERE r.task_id=$1 AND s.step_type='manual_check' ORDER BY s.created_at`, w.root))
	return argv
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
		// A turn the test holds is left alone while its agent works.
		held := []string{}
		for run := range w.runtime.held {
			held = append(held, run.String())
		}
		w.exec(`UPDATE agent_tasks SET next_poll_at=CASE WHEN run_id::text=ANY($1) AND state<>'cancelling' THEN now()+interval '1 hour' ELSE now() END`, pq.Array(held))
		ctx, cancel := context.WithTimeout(context.Background(), w.step)
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
		if w.afterStep != nil {
			w.afterStep(*task)
		}
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
	// held are the runs whose agent still works, by role.
	held map[uuid.UUID]string
}

// finish ends the held turn of a role as turn says.
func (w *tickWorld) finish(role string, turn tickTurn) {
	w.t.Helper()
	for run, held := range w.runtime.held {
		if held != role {
			continue
		}
		status := domain.DevMachineAgentRunStatusSucceeded
		if turn.fail != "" {
			status = domain.DevMachineAgentRunStatusFailed
			w.runtime.runs[run].FailureCause = &turn.fail
		}
		w.exec(`UPDATE dev_machine_agent_runs SET status=$2,summary=$3,completed_at=now() WHERE id=$1`, run, status, turn.final)
		w.runtime.runs[run].Status = status
		delete(w.runtime.held, run)
		return
	}
	w.t.Fatalf("no turn of the @%s is held", role)
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
	if turn.hold {
		status = domain.DevMachineAgentRunStatusRunning
		r.held[task.RunID] = role
	}
	r.w.exec(`INSERT INTO dev_machine_agent_runs(id,workspace_id,machine_id,task_id,requested_by_user_id,provider_id,mode,prompt,command_argv,max_runtime_seconds,status,started_at,completed_at,summary,worktree_changed,encrypted_output_secrets)
 VALUES($1,$2,$3,$4,$5,$6,'autonomous',$7,'["agent"]',60,$8,now(),now(),$9,$10,'sealed')`, task.RunID, task.WorkspaceID, r.w.machine, task.ID, task.CreatorID, task.Provider, task.Prompt, status, turn.final, turn.commit != "")
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
	if _, ok := r.held[task.RunID]; ok {
		delete(r.held, task.RunID)
		r.runs[task.RunID].Status = domain.DevMachineAgentRunStatusCancelled
		r.w.exec(`UPDATE dev_machine_agent_runs SET status='cancelled',completed_at=now() WHERE id=$1`, task.RunID)
	}
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
	require.Equal(t, []string{"review", "integrate", "approval", "merge"}, w.phases(), "published at once, the work skips the pull request phase")
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
		if role == "implementer" && turn > 1 {
			return tickTurn{final: "Renumbering."}
		}
		return w.writesAndApproves(role, turn, task)
	}
	// Another task merged a migration with the same number first.
	const repair = "Migration 000208 of this branch has the number of 000208_other_feature on main. Next step: renumber it to 000209."
	w.github.status.migrationRepair = repair
	w.drain()
	w.github.ci("passed", "")
	w.later()
	collision := w.facts("migration_collision")
	require.Len(t, collision, 1)
	require.JSONEq(t, `true`, string(collision[0].Data.Value))
	require.Contains(t, collision[0].Body, repair)
	// The pipeline requires no collision: the reason reaches the writer, and
	// nothing is merged.
	require.Equal(t, "integrate", w.phase())
	require.Contains(t, w.prompt("implementer", 2), repair)
	require.Empty(t, w.github.merged)
}

func TestTickScenario07QuestionAndAnswerResumeSessions(t *testing.T) {
	w := newTickWorld(t, nil)
	asked := map[int]int64{}
	w.agent = func(role string, turn int, task domain.AgentTask) tickTurn {
		switch {
		case role == "implementer" && turn == 1:
			return tickTurn{commit: sha(1), final: "Added the toggle.", posts: []string{`{"to":"@reviewer","kind":"ask","body":"Should the toggle persist per user or per device?"}`}}
		case role == "implementer" && turn == 2:
			return tickTurn{final: "Stored it per user.", posts: []string{`{"to":"@reviewer","kind":"ask","body":"Is the profile the right store?"}`}}
		case role == "reviewer":
			for _, m := range w.room() {
				if m.Kind == domain.RoomAsk {
					asked[turn] = m.Seq
				}
			}
			answer := map[int]string{1: "Per user: store it in the profile.", 2: "Yes, the profile."}[turn]
			return tickTurn{final: "Verdict: approve\nFine.", posts: []string{fmt.Sprintf(`{"to":"@implementer","body":%q,"reply_to":%d}`, answer, asked[turn])}}
		}
		return tickTurn{final: "Done."}
	}
	w.drain()
	require.Contains(t, w.prompt("reviewer", 1), "Should the toggle persist per user or per device?")
	answer := w.prompt("implementer", 2)
	require.Contains(t, answer, "Per user: store it in the profile.")
	require.Contains(t, answer, fmt.Sprintf("replying to #%d", asked[1]))
	require.Contains(t, w.prompt("reviewer", 2), "Is the profile the right store?")
	require.Contains(t, w.prompt("implementer", 3), "Yes, the profile.")

	// Each of these next turns continues its session: launched again on
	// Claude Code, Codex and OpenCode, the reviewer's second turn and the
	// writer's third carry the harness's resume flag and the session of
	// the turn before.
	reviewer := w.member("reviewer")
	implementer, _, err := w.f.workflows.builtinProfileIDs(w.f.ctx, w.f.ws)
	require.NoError(t, err)
	w.exec(`UPDATE agent_profiles SET instructions='Keep every commit small.' WHERE id=$1`, implementer)
	for provider, resume := range map[string][]string{
		"claude-code": {"--resume", "0c9a6c1e-0000-4000-8000-000000000001"},
		"codex":       {"resume", "0199a213-81c0-7800-8aa1-bbab2a035a53"},
		"opencode":    {"--session", "ses_df21986abffefn2FxDuH6uZGLA"},
	} {
		for role, id := range map[string]uuid.UUID{"reviewer": *reviewer.MemberTaskID, "implementer": w.root} {
			run := w.relaunch(id, provider, resume[1])
			var argv []string
			require.NoError(t, json.Unmarshal(run.CommandArgv, &argv))
			require.Subset(t, argv, resume, "the @%s's next turn on %s resumes its session", role, provider)
			require.Contains(t, run.Prompt, "this continues your previous session")
			if role == "implementer" && provider == "claude-code" {
				require.Contains(t, string(run.ProfileFiles), "Keep every commit small.", "the writer's profile is its instructions file")
			}
		}
	}
}

// relaunch builds, on provider, the launch of the task's current turn as
// Kuayle starts it on its machine, with session as the session of the
// task's turn before: the real launch path (devMachineTaskRuntime.Attempt)
// over the scenario's own records.
func (w *tickWorld) relaunch(id uuid.UUID, provider, session string) domain.DevMachineAgentRun {
	w.t.Helper()
	var task domain.AgentTask
	require.NoError(w.t, w.f.db.Get(&task, `SELECT * FROM agent_tasks WHERE id=$1`, id))
	w.exec(`UPDATE dev_machine_agent_runs SET native_session_id=$3,provider_id=$4 WHERE id=(SELECT id FROM dev_machine_agent_runs WHERE task_id=$1 AND id<>$2 ORDER BY created_at DESC,id LIMIT 1)`, id, task.RunID, session, provider)
	task.State, task.Provider = "preparing", provider
	store := &taskPolicyStore{devMachineStoreFake: &devMachineStoreFake{
		policy:        testPolicy(task.WorkspaceID),
		machine:       &domain.DevMachine{ID: w.machine, WorkspaceID: task.WorkspaceID, CreatedByUserID: &task.CreatorID, Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning},
		agentProvider: &domain.DevMachineAgentProvider{ProviderID: provider, Enabled: true, RequiredSecrets: json.RawMessage(`[]`), Config: json.RawMessage(`{}`)},
		checkouts:     []domain.DevMachineCheckout{{ID: task.CheckoutID, WorkspaceID: task.WorkspaceID, MachineID: w.machine, Status: "ready", WorkspacePath: "/workspace/tasks/eng-1", RepositoryFullName: tickRepository, BaseBranch: "main", WorkingBranch: "kuayle/eng-1"}},
	}}
	store.policy.AllowedProviders = json.RawMessage(`["opencode","codex","claude-code"]`)
	machines := newTestDevMachineService(store)
	machines.agents = agent.NewRegistry(agent.NewOpenCodeProvider(""), agent.NewCodexProvider(""), agent.NewClaudeCodeProvider(""))
	_, err := (&devMachineTaskRuntime{machines: machines, tasks: w.f.tasks.Repo}).Attempt(w.f.ctx, task)
	require.NoError(w.t, err)
	require.Len(w.t, store.agentRuns, 1)
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

	// GitHub answers too slowly for the worker's step: the read gives up in
	// time to save, and the pass waits as for an error, however often.
	w.github.slow, w.step = time.Minute, 2*time.Second
	for range 6 {
		w.later()
	}
	slow := w.task()
	require.Equal(t, "awaiting_review", slow.State)
	require.Zero(t, slow.FailureCount)
	require.NotNil(t, slow.WorkflowWakeAt)
	require.Len(t, w.facts("head_sha"), 1, "nothing more is posted")
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

// A member's turn a person cancelled is taken again: after Continue, the
// reviewer takes its next turn on its own task.
func TestTickACancelledMemberTurnIsTakenAgain(t *testing.T) {
	w := newTickWorld(t, nil)
	w.agent = func(role string, turn int, task domain.AgentTask) tickTurn {
		switch {
		case role == "reviewer" && turn == 1:
			return tickTurn{hold: true}
		case role == "implementer" && turn > 1:
			return tickTurn{final: "Waiting for the review."}
		}
		return w.writesAndApproves(role, turn, task)
	}
	w.drain()
	require.NoError(t, w.f.tasks.Repo.RequestCancel(w.f.ctx, w.f.ws, *w.member("reviewer").MemberTaskID))
	w.drain()
	require.Equal(t, domain.WorkflowStatusPaused, *w.task().WorkflowStatus, "nobody reviews")
	require.Len(t, w.facts("turn"), 1, "the cancelled turn is a fact")
	require.NoError(t, w.decide(WorkflowDecisionContinue))
	w.drain()
	require.Equal(t, 2, w.runtime.turns["reviewer"])
	require.Equal(t, "integrate", w.phase())
}

// The unmet checks of a phase a role other than the writer owns reach that
// role as a turn; a phase a person owns waits for the person.
func TestTickAStallReachesThePhaseOwner(t *testing.T) {
	t.Run("a role", func(t *testing.T) {
		w := newTickWorld(t, func(p *domain.Pipeline) { p.Phases[2].Owner = "reviewer" })
		w.agent = func(role string, turn int, task domain.AgentTask) tickTurn {
			if role == "reviewer" && turn == 1 {
				return tickTurn{final: "Looks fine to me."} // no verdict
			}
			return w.writesAndApproves(role, turn, task)
		}
		w.drain()
		require.Equal(t, 2, w.runtime.turns["reviewer"])
		require.Contains(t, w.prompt("reviewer", 2), "has not reviewed")
		require.Equal(t, 1, w.runtime.turns["implementer"])
		require.Equal(t, "integrate", w.phase())
	})
	t.Run("a person", func(t *testing.T) {
		w := newTickWorld(t, func(p *domain.Pipeline) { p.Phases[3].Owner = domain.PipelineOwnerHuman })
		w.agent = w.writesAndApproves
		w.drain()
		w.github.ci("failed", "TestDarkMode failed")
		w.later()
		waiting := w.task()
		require.Equal(t, "awaiting_review", waiting.State)
		require.NotNil(t, waiting.WorkflowHeldSince, "a person is awaited")
		summaries := w.posts(domain.RoomSummary)
		require.Len(t, summaries, 1)
		require.Equal(t, []string{"human"}, []string(summaries[0].ToRoles))
		require.Equal(t, 1, w.runtime.turns["implementer"])
	})
}

// A head the writer published while the reviewer still reviewed the one
// before is asked for once that review ends.
func TestTickAsksForAHeadPublishedDuringAReview(t *testing.T) {
	w := newTickWorld(t, nil)
	w.agent = func(role string, turn int, task domain.AgentTask) tickTurn {
		if role == "reviewer" && turn == 1 {
			return tickTurn{hold: true}
		}
		return w.writesAndApproves(role, turn, task)
	}
	w.drain()
	_, err := w.f.tasks.Repo.PostRoomMessage(w.f.ctx, w.f.ws, w.root, domain.TaskRoomPost{Kind: domain.RoomSay, Body: "Also follow the system theme.", ToRoles: []string{"implementer"}})
	require.NoError(t, err)
	w.drain()
	require.Equal(t, sha(2), w.github.status.HeadSHA)
	w.finish("reviewer", tickTurn{final: "Verdict: approve\nGood."})
	w.drain()
	reviewer := w.member("reviewer")
	require.Equal(t, 2, reviewer.Turns)
	require.Equal(t, sha(2), *reviewer.PinnedCommit)
}

// An approval holds for the head it was given on: a new commit is not
// merged until the owner approves it.
func TestTickAnApprovalOfAnOlderHeadDoesNotMerge(t *testing.T) {
	w := newTickWorld(t, nil)
	w.agent = w.writesAndApproves
	w.throughReview()
	// The writer adds a commit; the reviewer approves it and CI passes.
	_, err := w.f.tasks.Repo.PostRoomMessage(w.f.ctx, w.f.ws, w.root, domain.TaskRoomPost{Kind: domain.RoomSay, Body: "One more thing.", ToRoles: []string{"implementer"}})
	require.NoError(t, err)
	w.drain()
	require.Equal(t, sha(2), w.github.status.HeadSHA)
	require.Equal(t, "approval", w.phase())
	// The owner's approval was given on the first head.
	approved := fmt.Sprintf(`{"head":%q,"by":%q}`, sha(1), w.f.user)
	w.exec(`UPDATE agent_tasks SET pipeline_state=jsonb_set(pipeline_state,'{approved}',$2::jsonb),workflow_wake_at=now() WHERE id=$1`, w.root, approved)
	w.drain()
	require.Equal(t, "approval", w.phase())
	require.Empty(t, w.github.merged, "the approval of the first head does not merge the second")
	require.NoError(t, w.decide(WorkflowDecisionApprove))
	w.drain()
	require.Equal(t, []string{sha(2)}, w.github.merged)
}

// An approver who can no longer decide the task does not merge it: a
// person is asked.
func TestTickAnApproverWhoLostTheRightDoesNotMerge(t *testing.T) {
	w := newTickWorld(t, nil)
	w.agent = w.writesAndApproves
	w.throughReview()
	admin := uuid.New()
	w.exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Admin','hash')`, admin, admin.String()+"@test.invalid")
	w.exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'admin')`, w.f.ws, admin)
	_, err := w.f.workflows.Decide(domain.WithAccess(context.Background(), domain.Access{WorkspaceID: w.f.ws, UserID: admin}), w.f.ws, w.root, w.task().WorkflowSequence, WorkflowDecisionApprove)
	require.NoError(t, err)
	w.exec(`UPDATE workspace_members SET role='member' WHERE workspace_id=$1 AND user_id=$2`, w.f.ws, admin)
	w.drain()
	w.later()
	require.Empty(t, w.github.merged)
	paused := w.task()
	require.Equal(t, domain.WorkflowStatusPaused, *paused.WorkflowStatus)
	require.Contains(t, paused.Reason, "can no longer merge")
}

// A merge wait left from an earlier head (Kuayle was bringing it up to
// date) does not stand in the way of merging a newer head.
func TestTickAStaleMergeWaitDoesNotBlockANewHead(t *testing.T) {
	w := newTickWorld(t, nil)
	w.agent = w.writesAndApproves
	w.throughReview()
	require.NoError(t, w.decide(WorkflowDecisionApprove))
	stale := fmt.Sprintf(`{"approved":%q,"head":%q,"updating":true,"updates":1,"since":"2026-11-24T10:00:00Z","deadline":"2099-01-01T00:00:00Z"}`, sha(9), sha(9))
	w.exec(`UPDATE agent_tasks SET pipeline_state=jsonb_set(pipeline_state,'{merge}',$2::jsonb) WHERE id=$1`, w.root, stale)
	w.drain()
	require.Equal(t, []string{sha(1)}, w.github.merged)
}

// A role whose profile was archived cannot take its turn: that is a fact,
// and the task asks a person instead of failing.
func TestTickARoleThatCannotTakeATurnAsksAPerson(t *testing.T) {
	w := newTickWorld(t, nil)
	w.agent = w.writesAndApproves
	_, reviewer, err := w.f.workflows.builtinProfileIDs(w.f.ctx, w.f.ws)
	require.NoError(t, err)
	w.exec(`UPDATE agent_profiles SET archived_at=now() WHERE id=$1`, reviewer)
	w.drain()
	task := w.task()
	require.Equal(t, "blocked", task.State)
	require.Equal(t, domain.WorkflowStatusPaused, *task.WorkflowStatus)
	require.Zero(t, task.FailureCount)
	turn := w.facts("turn")
	require.Len(t, turn, 1)
	require.Contains(t, turn[0].Body, "cannot take a turn")
}

// Ending the task stops the turns its members are taking.
func TestTickEndingTheTaskStopsItsMembersTurns(t *testing.T) {
	w := newTickWorld(t, nil)
	w.agent = func(role string, turn int, task domain.AgentTask) tickTurn {
		if role == "reviewer" {
			return tickTurn{hold: true}
		}
		return w.writesAndApproves(role, turn, task)
	}
	w.drain()
	completed, err := w.f.tasks.Repo.CompleteMergedPullRequest(context.Background(), w.f.ws, tickRepository, 12)
	require.NoError(t, err)
	require.Equal(t, 1, completed)
	var state string
	require.NoError(t, w.f.db.Get(&state, `SELECT state FROM agent_tasks WHERE id=$1`, *w.member("reviewer").MemberTaskID))
	require.Equal(t, "cancelling", state)
	w.drain()
	require.NoError(t, w.f.db.Get(&state, `SELECT state FROM agent_tasks WHERE id=$1`, *w.member("reviewer").MemberTaskID))
	require.Equal(t, "cancelled", state)
}

// Checks that never report are not waited for forever: after a bound the
// phase's owner is sent them like any unmet check.
func TestTickChecksThatNeverReportAreSentOn(t *testing.T) {
	w := newTickWorld(t, nil)
	w.agent = func(role string, turn int, task domain.AgentTask) tickTurn {
		if role == "implementer" && turn > 1 {
			return tickTurn{final: "CI does not start."}
		}
		return w.writesAndApproves(role, turn, task)
	}
	// The base's tip has CI; the head reports none.
	w.github.checks = nil
	w.github.commitChecks = map[string][]PullRequestCheck{tickBase: {{Name: "unit", State: "passed"}}}
	w.drain()
	require.Equal(t, "integrate", w.phase())
	require.NotNil(t, w.task().WorkflowWakeAt, "waiting for CI")
	require.Equal(t, 1, w.runtime.turns["implementer"])
	later := time.Now().Add(3 * time.Hour)
	w.f.tasks.Workflow().now = func() time.Time { return later }
	w.later()
	require.GreaterOrEqual(t, w.runtime.turns["implementer"], 2)
	require.Contains(t, w.prompt("implementer", 2), "No checks have reported")
}

// A writer's turn that cannot start yet (its last attempt is still being
// wound down) waits for the next poll instead of passing again at once.
func TestTickAWriterTurnThatCannotStartYetWaits(t *testing.T) {
	w := newTickWorld(t, nil)
	w.agent = func(role string, turn int, task domain.AgentTask) tickTurn {
		if role == "reviewer" {
			return tickTurn{hold: true}
		}
		return w.writesAndApproves(role, turn, task)
	}
	w.drain()
	run := w.task().RunID
	w.exec(`INSERT INTO dev_machine_operations(workspace_id,machine_id,agent_run_id,action,status,generation,idempotency_key) VALUES($1,$2,$3,'cancel_agent','pending',1,'test-pending')`, w.f.ws, w.machine, run)
	_, err := w.f.tasks.Repo.PostRoomMessage(w.f.ctx, w.f.ws, w.root, domain.TaskRoomPost{Kind: domain.RoomSay, Body: "Also follow the system theme.", ToRoles: []string{"implementer"}})
	require.NoError(t, err)
	w.drain()
	waiting := w.task()
	require.Equal(t, "awaiting_review", waiting.State)
	require.NotNil(t, waiting.WorkflowWakeAt)
	require.True(t, waiting.WorkflowWakeAt.After(time.Now().Add(30*time.Second)), "the next pass is a poll away")
	w.exec(`UPDATE dev_machine_operations SET status='completed' WHERE idempotency_key='test-pending'`)
	w.later()
	require.Equal(t, 2, w.runtime.turns["implementer"])
}

// A waiver holds for the head the room last named when the task's creator
// gave it, and only from the creator: a role's waiver is refused.
func TestTickAWaiverHoldsForItsHeadAndOnlyFromTheCreator(t *testing.T) {
	w := newTickWorld(t, nil)
	w.agent = func(role string, turn int, task domain.AgentTask) tickTurn {
		switch {
		case role == "reviewer":
			return tickTurn{hold: true}
		case turn == 1:
			return tickTurn{commit: sha(1), final: "Added the toggle.", posts: []string{`{"to":"@all","kind":"waiver","data":{"check":"reviewer"}}`}}
		}
		return w.writesAndApproves(role, turn, task)
	}
	w.drain()
	require.Equal(t, "review", w.phase(), "the writer's waiver is refused")
	refused := w.posts(domain.RoomWaiver)
	require.Len(t, refused, 1)
	require.NotNil(t, refused[0].RejectReason)

	_, err := w.f.tasks.Repo.PostRoomMessage(w.f.ctx, w.f.ws, w.root, domain.TaskRoomPost{Kind: domain.RoomWaiver, Data: domain.TaskRoomData{Check: "reviewer"}})
	require.NoError(t, err)
	w.drain()
	require.Equal(t, "integrate", w.phase(), "the owner waived the review of this head")

	_, err = w.f.tasks.Repo.PostRoomMessage(w.f.ctx, w.f.ws, w.root, domain.TaskRoomPost{Kind: domain.RoomSay, Body: "Also follow the system theme.", ToRoles: []string{"implementer"}})
	require.NoError(t, err)
	w.drain()
	require.Equal(t, sha(2), w.github.status.HeadSHA)
	require.Equal(t, "review", w.phase(), "the waiver was for the head before")
}

// A required script check runs in the root task's worktree, through the
// manual check runner, once per head: exit 0 meets it, another exit sends
// its output's end to the writer, and a new head runs it again.
func TestTickRunsScriptChecksOncePerHead(t *testing.T) {
	w := newTickWorld(t, func(p *domain.Pipeline) {
		p.Phases[3].Checks = append(p.Phases[3].Checks, domain.PipelineCheck{ID: "tests", Fact: domain.PipelineFactScript, Script: "make test", Required: true})
	})
	w.agent = w.writesAndApproves
	w.drain()
	require.Equal(t, "integrate", w.phase())
	require.Equal(t, []string{`["sh", "-c", "make test"]`}, w.checks())
	require.NotNil(t, w.task().WorkflowWakeAt, "it waits for the check")

	w.runCheck(0, "ok\n")
	w.drain()
	w.later()
	require.Len(t, w.checks(), 1, "once per head")
	passed := w.facts("script")
	require.Len(t, passed, 1)
	require.Contains(t, passed[0].Body, "passed")
	w.github.ci("passed", "")
	w.later()
	require.Equal(t, "approval", w.phase())

	// A new head runs it again; a failure goes to the writer with its output.
	_, err := w.f.tasks.Repo.PostRoomMessage(w.f.ctx, w.f.ws, w.root, domain.TaskRoomPost{Kind: domain.RoomSay, Body: "Also follow the system theme.", ToRoles: []string{"implementer"}})
	require.NoError(t, err)
	w.drain()
	require.Equal(t, sha(2), w.github.status.HeadSHA)
	require.Len(t, w.checks(), 2, "a new head is checked again")
	w.runCheck(1, "--- FAIL: TestToggle\nexpected dark\n")
	w.drain()
	failed := w.facts("script")
	require.Len(t, failed, 2)
	require.Contains(t, failed[1].Body, "FAIL: TestToggle")
	prompt := w.prompt("implementer", 3)
	require.Contains(t, prompt, "Check tests exited 1")
	require.Contains(t, prompt, "expected dark")
}

// A script check that cannot run (the worktree is not on a running
// machine) is not known yet, never unmet: the task waits and tries again.
func TestTickAScriptCheckThatCannotRunWaits(t *testing.T) {
	w := newTickWorld(t, func(p *domain.Pipeline) {
		p.Phases[3].Checks = append(p.Phases[3].Checks, domain.PipelineCheck{ID: "tests", Fact: domain.PipelineFactScript, Script: "make test", Required: true})
	})
	w.agent = w.writesAndApproves
	w.github.ci("passed", "")
	w.exec(`UPDATE dev_machine_checkouts SET status='queued' WHERE id=$1`, w.task().CheckoutID)
	w.drain()
	waiting := w.task()
	require.Equal(t, "integrate", w.phase())
	require.Equal(t, "awaiting_review", waiting.State)
	require.Contains(t, waiting.Reason, "could not run check tests")
	require.NotNil(t, waiting.WorkflowWakeAt)
	require.Empty(t, w.posts(domain.RoomSummary), "nobody is sent it")
	require.Empty(t, w.checks())
	w.exec(`UPDATE dev_machine_checkouts SET status='ready' WHERE id=$1`, waiting.CheckoutID)
	w.later()
	require.Len(t, w.checks(), 1)
}

// An advisory script check never holds the task up: its failure is a fact
// sent to the owner of its phase.
func TestTickAnAdvisoryScriptCheckInformsThePhaseOwner(t *testing.T) {
	w := newTickWorld(t, func(p *domain.Pipeline) {
		p.Phases[3].Checks = append(p.Phases[3].Checks, domain.PipelineCheck{ID: "lint", Fact: domain.PipelineFactScript, Script: "make lint"})
	})
	w.agent = func(role string, turn int, task domain.AgentTask) tickTurn {
		if role == "implementer" && turn > 1 {
			return tickTurn{final: "Noted the lint warnings."}
		}
		return w.writesAndApproves(role, turn, task)
	}
	w.github.ci("passed", "")
	w.drain()
	require.Equal(t, []string{`["sh", "-c", "make lint"]`}, w.checks())
	require.Equal(t, "approval", w.phase(), "it does not wait for an advisory check")
	w.runCheck(2, "3 lint warnings\n")
	w.drain()
	lint := w.facts("script")
	require.Len(t, lint, 1)
	require.Equal(t, []string{"implementer"}, []string(lint[0].ToRoles))
	require.Contains(t, w.prompt("implementer", 2), "3 lint warnings")
	require.Equal(t, "approval", w.phase())
}

// tickBusyBase moves the base ahead of from: GitHub's update-branch of from
// makes to, on which CI then runs; a push to the base wakes the task.
func (w *tickWorld) tickBusyBase(from, to string) {
	w.t.Helper()
	w.update(from, to)
	w.github.status.Behind, w.github.updateHead = 1, to
	w.github.onUpdate = func() {
		w.github.status.Behind = 0
		w.head(to)
		w.github.ci("pending", "")
	}
	w.exec(`UPDATE agent_tasks SET workflow_wake_at=now() WHERE id=$1`, w.root)
	w.drain()
}

// toApprovalOnABusyBase runs the team pipeline to its approval while the
// base moves three times: Kuayle brings the branch up to date each time,
// every update the published commit has before the owner approves.
func (w *tickWorld) toApprovalOnABusyBase() string {
	w.t.Helper()
	w.drain()
	head := sha(1)
	for i := 1; i <= maxMergeUpdates; i++ {
		next := sha(0x300 + i)
		w.tickBusyBase(head, next)
		require.Equal(w.t, next, w.github.status.HeadSHA, "update %d before the approval", i)
		head = next
	}
	w.github.ci("passed", "")
	w.later()
	require.Equal(w.t, "approval", w.phase())
	return head
}

// KUA-277 (option B) in the tick: once the owner approved, the merge has its
// own updates, counted from the approved head.
func TestTickApprovedMergeHasItsOwnUpdates(t *testing.T) {
	w := newTickWorld(t, nil)
	w.agent = w.writesAndApproves
	head := w.toApprovalOnABusyBase()
	require.NoError(t, w.decide(WorkflowDecisionApprove))
	w.tickBusyBase(head, sha(0x401))
	require.Equal(t, sha(0x401), w.github.status.HeadSHA, "brought up to date after the approval")
	require.Empty(t, w.github.merged, "CI runs on the update first")
	w.github.ci("passed", "")
	w.later()
	require.Equal(t, []string{sha(0x401)}, w.github.merged)
	require.Equal(t, "completed", w.task().State)
	require.Equal(t, 1, w.runtime.turns["implementer"], "the writer was not sent the branch")
	require.Equal(t, 1, w.member("reviewer").Turns, "nobody reviews the base again")
}

func TestTickApprovedMergeMakesAtMostItsOwnUpdates(t *testing.T) {
	w := newTickWorld(t, nil)
	w.agent = w.writesAndApproves
	head := w.toApprovalOnABusyBase()
	require.NoError(t, w.decide(WorkflowDecisionApprove))
	for i := 1; i <= maxMergeUpdates; i++ {
		next := sha(0x400 + i)
		w.tickBusyBase(head, next)
		require.Equal(t, next, w.github.status.HeadSHA, "the merge's own update %d", i)
		head = next
	}
	w.tickBusyBase(head, sha(0x404))
	require.NotContains(t, w.github.calls, "update:"+head, "no fourth update after the approval")
	require.Empty(t, w.github.merged)
}

// KUA-257 for pipelines: a push to the base (or a pull request merged into
// it) wakes a pipeline task that waits on its pull request, also while the
// owner is awaited and when the writer's latest turn published nothing (its
// pull request is on an earlier run, as the tick reads it).
func TestTickABasePushWakesTheWaitingPipeline(t *testing.T) {
	w := newTickWorld(t, nil)
	w.agent = w.writesAndApproves
	w.throughReview()
	waiting := w.task()
	require.Equal(t, "awaiting_review", waiting.State)
	require.Equal(t, domain.WorkflowStatusWaiting, deref(waiting.WorkflowStatus))
	require.Nil(t, waiting.WorkflowWakeAt, "nothing polls while the owner is awaited")

	wake := func(branch string) int {
		t.Helper()
		n, err := w.f.tasks.Repo.WakeBaseBranchWaits(w.f.ctx, w.f.ws, tickRepository, branch)
		require.NoError(t, err)
		return n
	}
	require.Zero(t, wake("release"), "a push to another branch")
	require.Equal(t, 1, wake("main"))
	require.WithinDuration(t, time.Now(), *w.task().WorkflowWakeAt, 5*time.Second)
	w.drain()

	// The writer's latest turn changed nothing, so Kuayle did not publish it.
	quiet := uuid.New()
	w.exec(`INSERT INTO dev_machine_agent_runs SELECT (jsonb_populate_record(NULL::dev_machine_agent_runs,
 to_jsonb(r)||jsonb_build_object('id',$2::uuid))).* FROM dev_machine_agent_runs r WHERE r.id=$1`, waiting.RunID, quiet)
	w.exec(`UPDATE agent_tasks SET run_id=$2,workflow_wake_at=NULL WHERE id=$1`, w.root, quiet)
	require.Equal(t, 1, wake("main"))
	require.WithinDuration(t, time.Now(), *w.task().WorkflowWakeAt, 5*time.Second)
}
