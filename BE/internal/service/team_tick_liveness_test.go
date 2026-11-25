package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	gh "github.com/kuayle/kuayle-backend/pkg/github"
	"github.com/stretchr/testify/require"
)

// Liveness of the pipeline tick (KUA-278). Seeded pseudo-random sequences of
// what happens around a pipeline task (pushes, CI results, base moves and
// conflicts, verdicts, the owner's approvals, decisions and posts, member
// turns that finish, fail or are cancelled, publications under way for a
// pass or two or failing, GitHub failing or answering too slowly, script
// checks, the budget running out) play through the scenario tests' world
// (tickWorld): the real worker, tick, repository and room, with GitHub and
// the machines faked. After every step the worker takes, the task must be
//
//   - working: the writer's turn, or a member's, is under way;
//   - waiting on a fact that changes by itself, named in its reason, with a
//     wake no further away than the tick's poll;
//   - waiting on a person: paused for a decision, or a person awaited (the
//     owner's approval, a phase a person owns);
//   - or ended, with no member's turn left running;
//
// never idle without a pending wake; the room never told of a publication
// still under way; and the writer never started more than tickStallLimit
// turns in a row with nothing changed. After the sequence everyone cooperates
// (GitHub answers, CI passes, the roles commit and approve, the owner
// approves and continues) and the task must end within tickLivenessSettle
// rounds, with at most one Continue beyond its budget's: a task that only
// polls forever, or moves only when a person pushes it, is not live either.
//
// TICK_LIVENESS_SEED=<seed> replays one sequence; TICK_LIVENESS_SEEDS=<n>
// plays n seeds from TICK_LIVENESS_FROM (1 by default). A failure names its
// seed and lists the events that led to it; TICK_LIVENESS_TRACE=1 lists
// them for every seed.

const (
	// tickLivenessSeeds and tickLivenessEvents keep the default run under a
	// minute on a developer machine.
	tickLivenessSeeds  = 10
	tickLivenessEvents = 30
	// tickLivenessSettle bounds the cooperative rounds a task may take to end.
	tickLivenessSettle = 16
)

func TestTickLiveness(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL is not set")
	}
	for _, seed := range tickLivenessSeedList(t) {
		t.Run(fmt.Sprintf("seed=%d", seed), func(t *testing.T) { playTickLiveness(t, seed) })
	}
}

func tickLivenessSeedList(t *testing.T) []uint64 {
	t.Helper()
	env := func(name string, fallback uint64) uint64 {
		value := os.Getenv(name)
		if value == "" {
			return fallback
		}
		n, err := strconv.ParseUint(value, 10, 64)
		require.NoError(t, err, name)
		return n
	}
	if os.Getenv("TICK_LIVENESS_SEED") != "" {
		return []uint64{env("TICK_LIVENESS_SEED", 0)}
	}
	from, n := env("TICK_LIVENESS_FROM", 1), env("TICK_LIVENESS_SEEDS", tickLivenessSeeds)
	seeds := make([]uint64, 0, n)
	for i := range n {
		seeds = append(seeds, from+i)
	}
	return seeds
}

// liveWorld is one seeded sequence over a tickWorld.
type liveWorld struct {
	*tickWorld
	seed uint64
	// rng picks the events; each turn draws from its own (turnRand), so a
	// turn plays the same whatever order the worker takes the tasks in.
	rng    *rand.Rand
	github *liveGitHub
	p      domain.Pipeline
	writer string
	// offset is how far the tick's clock runs ahead of the real one.
	offset time.Duration
	// cooperative: every role, GitHub and the owner move the task on.
	cooperative bool
	commits     int
	steps       int
	events      []string
	// playing is the event under way.
	playing string
	// checkoutFor counts the events left while the task's worktree is not on
	// a running machine (its script checks cannot run).
	checkoutFor int
	// stuck counts the writer's turns in a row that started on the same
	// head and unmet checks (the tick's stall key) since a person decided.
	stuck    int
	stuckKey string
}

func playTickLiveness(t *testing.T, seed uint64) {
	rng := rand.New(rand.NewPCG(seed, 0x6b7561796c65))
	var p domain.Pipeline
	w := newTickWorld(t, func(pipeline *domain.Pipeline) {
		tickLivenessVariant(rng, pipeline)
		p = *pipeline
	})
	writer, _ := p.Writer()
	l := &liveWorld{tickWorld: w, seed: seed, rng: rng, p: p, writer: writer.Role}
	l.github = &liveGitHub{tickGitHub: w.github, l: l, tip: tickBase, ci: map[string]string{}, resolves: map[string]bool{}, ours: map[string]bool{}}
	l.github.parents, l.github.onBase, l.github.changes = map[string][]string{}, map[string]bool{tickBase: true}, map[string][]gh.CompareFile{}
	// The base's tip has CI: a head that reports no checks waits for them.
	l.github.commitChecks = map[string][]PullRequestCheck{tickBase: {{Name: "unit", State: "passed"}}}
	w.f.tasks.Workflow().SetPullRequests(l.github, l.github)
	w.f.tasks.Workflow().SetCIChecks(l.github)
	w.f.tasks.Workflow().now = func() time.Time { return time.Now().Add(l.offset) }
	w.agent = l.agent
	w.afterStep = func(domain.AgentTask) {
		l.steps++
		if l.task().State == "cancelling" && l.pendingCheck() {
			l.dropCheck()
		}
		l.check(false)
	}
	t.Cleanup(func() {
		if t.Failed() || os.Getenv("TICK_LIVENESS_TRACE") != "" {
			t.Logf("seed %d, events:\n%s\n     (under way: %s)\nreplay: TICK_LIVENESS_SEED=%d go test ./internal/service -run 'TestTickLiveness' -count=1 -v", seed, strings.Join(l.events, "\n"), l.playing, seed)
		}
	})
	l.note("pipeline: %s", tickLivenessDescribe(p))

	l.drain()
	l.check(true)
	for i := 0; i < tickLivenessEvents && !l.ended(); i++ {
		l.play()
		l.check(true)
	}
	l.settle()
}

// tickLivenessVariant varies the team pipeline by seed: its budget, a
// second reviewing role, script checks, and who owns a phase.
func tickLivenessVariant(rng *rand.Rand, p *domain.Pipeline) {
	p.Budget.MaxTurns = []int{6, 12, 40}[rng.IntN(3)]
	if rng.IntN(5) == 0 {
		p.Budget.OnExhausted = domain.WorkflowOnLimitFail
	}
	if rng.IntN(3) == 0 {
		// A second reviewer with the reviewer's profile.
		p.Roster = append(p.Roster, domain.PipelineMember{Role: "security", ProfileID: p.Roster[1].ProfileID})
		p.Phases[2].Checks = append(p.Phases[2].Checks, domain.PipelineCheck{ID: "security", Fact: domain.PipelineFactVerdict, Role: "security", Required: true})
	}
	switch rng.IntN(5) {
	case 0, 1:
		p.Phases[3].Checks = append(p.Phases[3].Checks, domain.PipelineCheck{ID: "tests", Fact: domain.PipelineFactScript, Script: "make test", Required: true})
	case 2:
		p.Phases[3].Checks = append(p.Phases[3].Checks, domain.PipelineCheck{ID: "lint", Fact: domain.PipelineFactScript, Script: "make lint"})
	}
	switch rng.IntN(8) {
	case 0:
		p.Phases[2].Owner = "reviewer"
	case 1:
		p.Phases[3].Owner = domain.PipelineOwnerHuman
	}
}

func tickLivenessDescribe(p domain.Pipeline) string {
	var phases []string
	for _, phase := range p.Phases {
		var checks []string
		for _, check := range phase.Checks {
			checks = append(checks, check.ID+map[bool]string{true: "", false: "?"}[check.Required])
		}
		phases = append(phases, fmt.Sprintf("%s(@%s %s)", phase.ID, phase.Owner, strings.Join(checks, ",")))
	}
	return fmt.Sprintf("%s; budget %d turns, %s", strings.Join(phases, " "), p.Budget.MaxTurns, p.Budget.Workflow().Policy())
}

func (l *liveWorld) note(format string, args ...any) {
	l.events = append(l.events, fmt.Sprintf("%3d  ", len(l.events))+fmt.Sprintf(format, args...))
}

// sha is a new commit name of a kind: the writer's, a person's push,
// Kuayle's update, the base's tip, a merge.
func (l *liveWorld) sha(kind int) string {
	l.commits++
	return sha(kind<<16 | l.commits)
}

// turnRand is the randomness of a role's turn.
func (l *liveWorld) turnRand(role string, turn int) *rand.Rand {
	h := fnv.New64a()
	_, _ = h.Write([]byte(role))
	return rand.New(rand.NewPCG(l.seed, h.Sum64()+uint64(turn)))
}

// commit is a commit the writer makes; resolves: it merges the base in and
// fixes what collides with it.
func (l *liveWorld) commit(resolves bool) string {
	commit := l.sha(0x1000)
	l.github.ours[commit] = true
	l.github.resolves[commit] = resolves
	return commit
}

// agent plays a role's turn: at random, or cooperating.
func (l *liveWorld) agent(role string, turn int, _ domain.AgentTask) tickTurn {
	if role == l.writer {
		l.writerStarts()
	}
	if l.cooperative {
		return l.cooperate(role)
	}
	r := l.turnRand(role, turn)
	var played tickTurn
	if role == l.writer {
		switch x := r.IntN(10); {
		case x < 5:
			played = tickTurn{commit: l.commit(r.IntN(10) < 6), final: "Committed."}
		case x < 7:
			played = tickTurn{final: "Nothing to change."}
		case x < 8:
			played = tickTurn{fail: "The writer's harness crashed"}
		default:
			played = tickTurn{hold: true}
			if r.IntN(2) == 0 {
				played.commit = l.commit(r.IntN(10) < 6)
			}
		}
		if r.IntN(7) == 0 {
			played.posts = []string{`{"to":"@reviewer","kind":"ask","body":"Is the toggle in the right place?"}`}
		}
		// Its publication is under way for a pass or two before it ends,
		// and sometimes fails (KUA-297). A randomness of its own keeps the
		// seeds' turns as they were.
		p := l.turnRand(role+" publication", turn)
		if p.IntN(3) == 0 {
			queued, running := domain.DevMachineAgentRunStepStatusQueued, domain.DevMachineAgentRunStepStatusRunning
			played.publishing = [][]domain.DevMachineAgentRunStepStatus{{queued}, {running}, {queued, running}}[p.IntN(3)]
		}
		played.unpublished = p.IntN(6) == 0
	} else {
		switch x := r.IntN(10); {
		case x < 4:
			played = tickTurn{final: "Verdict: approve\nLooks right."}
		case x < 6:
			played = tickTurn{final: "Verdict: request_changes\nPersist the setting."}
		case x < 7:
			played = tickTurn{final: "Verdict: comment\nA nit."}
		case x < 8:
			played = tickTurn{final: "I looked at it."}
		case x < 9:
			played = tickTurn{fail: "The " + role + "'s harness crashed"}
		default:
			played = tickTurn{hold: true}
		}
		if r.IntN(7) == 0 {
			played.posts = []string{`{"to":"@implementer","body":"Mind the system theme."}`}
		}
	}
	l.note("     @%s turn %d: %s", role, turn, describeTurn(played))
	return played
}

func (l *liveWorld) cooperate(role string) tickTurn {
	if role == l.writer {
		return tickTurn{commit: l.commit(true), final: "Committed, with the base merged in."}
	}
	return tickTurn{final: "Verdict: approve\nLooks right."}
}

func describeTurn(turn tickTurn) string {
	var parts []string
	switch {
	case turn.hold:
		parts = append(parts, "keeps working")
	case turn.fail != "":
		parts = append(parts, "fails")
	default:
		parts = append(parts, strings.SplitN(turn.final, "\n", 2)[0])
	}
	if turn.commit != "" {
		parts = append(parts, "commits "+shortSHA(turn.commit))
	}
	if len(turn.posts) > 0 {
		parts = append(parts, "posts")
	}
	if len(turn.publishing) > 0 {
		parts = append(parts, fmt.Sprintf("its publication is under way for %d pass(es)", len(turn.publishing)))
	}
	if turn.unpublished {
		parts = append(parts, "its publication fails")
	}
	return strings.Join(parts, ", ")
}

// apply puts GitHub and the machine in the state the event left them, then
// drain runs the worker until nothing is due.
func (l *liveWorld) apply() {
	g := l.github
	g.readErr, g.tickGitHub.slow, l.step = nil, 0, 20*time.Second
	if g.downFor > 0 {
		g.readErr = errLiveGitHubDown
	}
	if g.slowFor > 0 {
		// GitHub answers after the worker's step gave up.
		g.tickGitHub.slow, l.step = time.Minute, 2*time.Second
	}
	status := "ready"
	if l.checkoutFor > 0 {
		status = "queued"
	}
	l.exec(`UPDATE dev_machine_checkouts SET status=$2 WHERE id=(SELECT checkout_id FROM agent_tasks WHERE id=$1)`, l.root, status)
}

func (l *liveWorld) drain() {
	l.apply()
	l.tickWorld.drain()
}

// elapse lets d pass on the tick's clock: whatever waits until then is due.
func (l *liveWorld) elapse(d time.Duration) {
	l.offset += d
	l.exec(`UPDATE agent_tasks SET workflow_wake_at=now() WHERE workflow_wake_at<=$1`, time.Now().Add(l.offset))
	l.drain()
}

// wake is the webhook or event that wakes the task's tick.
func (l *liveWorld) wake() {
	l.exec(`UPDATE agent_tasks SET workflow_wake_at=now() WHERE id=$1 AND state='awaiting_review' AND workflow_status='waiting'`, l.root)
}

func (l *liveWorld) ended() bool {
	switch l.task().State {
	case "completed", "failed", "cancelled":
		return true
	}
	return false
}

// memberTasks are the members' tasks by role, roles sorted.
func (l *liveWorld) memberTasks() ([]string, map[string]domain.AgentTask) {
	tasks, err := l.f.tasks.Repo.MemberTasks(context.Background(), l.task())
	require.NoError(l.t, err)
	roles := make([]string, 0, len(tasks))
	for role := range tasks {
		roles = append(roles, role)
	}
	slices.Sort(roles)
	return roles, tasks
}

// heldRoles are the roles whose turn the test keeps working, sorted.
func (l *liveWorld) heldRoles() []string {
	var roles []string
	for _, role := range l.runtime.held {
		roles = append(roles, role)
	}
	slices.Sort(roles)
	return roles
}

// pendingCheck reports whether a script check waits for the machine.
func (l *liveWorld) pendingCheck() bool {
	var pending bool
	require.NoError(l.t, l.f.db.Get(&pending, `SELECT EXISTS(SELECT 1 FROM dev_machine_operations WHERE machine_id=$1 AND action=$2 AND status='pending')`, l.machine, domain.DevMachineOpRunCheck))
	return pending
}

// dropCheck plays the machine taking a script check queued for a task
// that is being stopped: the check may no longer run, and the machine drops
// it, which lets the stop be confirmed.
func (l *liveWorld) dropCheck() {
	store := repository.NewDevMachineRepository(l.f.db)
	ctx := context.Background()
	ops, err := store.LeaseOperations(ctx, "tick-liveness-machine", 1, time.Minute)
	require.NoError(l.t, err)
	require.Len(l.t, ops, 1)
	op := ops[0]
	require.Equal(l.t, domain.DevMachineOpRunCheck, op.Action)
	_, err = store.GetManualCheck(ctx, op)
	require.Error(l.t, err, "a task being stopped runs no check")
	_, err = store.FailOperation(ctx, op.ID, *op.LeaseOwner, "unauthorized", err.Error(), false)
	require.NoError(l.t, err)
}

// check is the invariant: the task is working, waiting on a named fact
// with a wake due within a poll, waiting on a person, or ended (and, once
// the worker settled, none of its members still working).
func (l *liveWorld) check(settled bool) {
	l.t.Helper()
	// The room hears what a publication did once it ended, never while
	// it is under way: the task waits in publishing for it (KUA-297).
	for _, m := range l.facts("publication") {
		var value struct {
			Status domain.DevMachineAgentRunStepStatus `json:"status"`
		}
		require.NoError(l.t, json.Unmarshal(m.Data.Value, &value))
		if value.Status == domain.DevMachineAgentRunStepStatusQueued || value.Status == domain.DevMachineAgentRunStepStatusRunning {
			l.violation("the room was told of a publication still %s (#%d: %s)", value.Status, m.Seq, m.Body)
		}
	}
	root := l.task()
	roles, members := l.memberTasks()
	busy := ""
	for _, role := range roles {
		if domain.MemberState(members[role].State) != domain.MemberIdle {
			busy = role
		}
	}
	status := deref(root.WorkflowStatus)
	now := time.Now().Add(l.offset)
	switch root.State {
	case "completed", "failed", "cancelled":
		if settled && busy != "" {
			l.violation("the task ended, but the @%s's turn is still %s", busy, members[busy].State)
		}
		return
	case "queued", "preparing", "waiting_for_disk", "running", "waiting_input", "validating", "publishing", "cancelling":
		return // the writer's turn, or its publication
	case "blocked":
		if status == domain.WorkflowStatusPaused {
			return // a person decides
		}
		l.violation("the task is blocked without a decision to take")
	case "awaiting_review":
		switch {
		case busy != "":
			return
		case root.WorkflowHeldSince != nil:
			return // a person is awaited
		case root.WorkflowWakeAt == nil:
			l.violation("nobody works, nobody is awaited and nothing wakes the task")
		case root.WorkflowWakeAt.After(now.Add(tickPoll + time.Second)):
			l.violation("nobody works and nobody is awaited; the next wake is %s away", root.WorkflowWakeAt.Sub(now).Round(time.Second))
		case strings.TrimSpace(strings.TrimPrefix(root.Reason, domain.WorkflowReasonPrefix)) == "":
			l.violation("the task waits without saying on what")
		}
		return
	}
	l.violation("the task is in state %s", root.State)
}

func (l *liveWorld) violation(format string, args ...any) {
	l.t.Helper()
	root := l.task()
	roles, members := l.memberTasks()
	var states []string
	for _, role := range roles {
		states = append(states, fmt.Sprintf("@%s %s (%q)", role, members[role].State, members[role].Reason))
	}
	var posts []string
	room := l.room()
	for _, m := range room[max(len(room)-10, 0):] {
		author := m.AuthorKind
		if m.AuthorRole != nil {
			author += " @" + *m.AuthorRole
		}
		posts = append(posts, fmt.Sprintf("  #%d %s %s %s to %v: %s", m.Seq, author, m.Kind, m.Data.Fact, []string(m.ToRoles), oneLine(m.Body, 160)))
	}
	wake := "none"
	if root.WorkflowWakeAt != nil {
		wake = root.WorkflowWakeAt.Sub(time.Now().Add(l.offset)).Round(time.Second).String() + " from now"
	}
	l.t.Fatalf("seed %d: not live: %s\n\ntask: %s, workflow %s, phase %s, wake %s, person awaited: %t\nreason: %s\nmembers: %s\nroom (last posts):\n%s",
		l.seed, fmt.Sprintf(format, args...), root.State, deref(root.WorkflowStatus), deref(root.WorkflowNode), wake, root.WorkflowHeldSince != nil,
		root.Reason, strings.Join(states, "; "), strings.Join(posts, "\n"))
}

// liveEvent is something that happens around the task. play returns what
// happened, or "" when it cannot happen now.
type liveEvent struct {
	name   string
	weight int
	play   func(l *liveWorld) string
}

var liveEvents = []liveEvent{
	{"time passes", 12, (*liveWorld).timePasses},
	{"CI reports", 12, (*liveWorld).ciReports},
	{"the base moves", 9, (*liveWorld).baseMoves},
	{"someone pushes", 3, (*liveWorld).someonePushes},
	{"a turn ends", 12, (*liveWorld).turnEnds},
	{"a turn is cancelled", 3, (*liveWorld).turnCancelled},
	{"the owner approves", 6, (*liveWorld).ownerApproves},
	{"the owner continues", 6, (*liveWorld).ownerContinues},
	{"the owner says something", 6, (*liveWorld).ownerSays},
	{"the owner waives a check", 3, (*liveWorld).ownerWaives},
	{"the owner stops", 1, (*liveWorld).ownerStops},
	{"GitHub fails", 6, (*liveWorld).githubFails},
	{"GitHub is slow", 3, (*liveWorld).githubSlow},
	{"a script check ends", 9, (*liveWorld).scriptCheckEnds},
	{"the worktree is away", 3, (*liveWorld).checkoutAway},
	{"the time budget passes", 2, (*liveWorld).budgetPasses},
	{"merges are refused", 3, (*liveWorld).mergeRefused},
	{"a migration collides", 3, (*liveWorld).migrationCollides},
	{"the pull request ends", 1, (*liveWorld).pullRequestEnds},
}

// play picks an event that can happen now and plays it.
func (l *liveWorld) play() {
	total := 0
	for _, event := range liveEvents {
		total += event.weight
	}
	for {
		pick := l.rng.IntN(total)
		for _, event := range liveEvents {
			if pick -= event.weight; pick < 0 {
				l.playing = event.name
				if happened := event.play(l); happened != "" {
					l.note("%s", happened)
					g := l.github
					g.downFor, g.slowFor, g.refuseFor, g.methodFor = max(g.downFor-1, 0), max(g.slowFor-1, 0), max(g.refuseFor-1, 0), max(g.methodFor-1, 0)
					l.checkoutFor = max(l.checkoutFor-1, 0)
					return
				}
				break
			}
		}
	}
}

func (l *liveWorld) timePasses() string {
	d := []time.Duration{20 * time.Second, 70 * time.Second, 3 * time.Minute, 30 * time.Minute, mergeWaitTimeout + time.Minute}[l.rng.IntN(5)]
	l.elapse(d)
	return "time passes: " + d.String()
}

func (l *liveWorld) ciReports() string {
	head := l.github.status.HeadSHA
	if head == "" {
		return ""
	}
	state := []string{"passed", "passed", "passed", "failed", "failed", "pending", "none"}[l.rng.IntN(7)]
	l.github.ci[head] = state
	webhook := l.rng.IntN(10) < 7
	if webhook {
		l.wake()
	}
	l.drain()
	return fmt.Sprintf("CI on %s: %s (webhook %t)", shortSHA(head), state, webhook)
}

func (l *liveWorld) baseMoves() string {
	g := l.github
	g.tip = l.sha(0x4000)
	g.onBase[g.tip] = true
	g.behind += 1 + l.rng.IntN(2)
	g.conflict = g.conflict || l.rng.IntN(10) < 3
	g.unknown = l.rng.IntN(5) == 0
	webhook := l.rng.IntN(10) < 8
	if webhook {
		_, err := l.f.tasks.Repo.WakeBaseBranchWaits(context.Background(), l.f.ws, tickRepository, "main")
		require.NoError(l.t, err)
	}
	l.drain()
	return fmt.Sprintf("the base moves: %d behind, conflict %t, mergeability unknown %t (webhook %t)", g.behind, g.conflict, g.unknown, webhook)
}

func (l *liveWorld) someonePushes() string {
	g := l.github
	if g.status.HeadSHA == "" || g.status.State != "open" {
		return ""
	}
	head := l.sha(0x2000)
	g.resolves[head] = l.rng.IntN(2) == 0
	g.status.HeadSHA = head
	webhook := l.rng.IntN(10) < 3
	if webhook {
		l.wake()
	}
	l.drain()
	return fmt.Sprintf("someone pushes %s, base merged in %t (webhook %t)", shortSHA(head), g.resolves[head], webhook)
}

func (l *liveWorld) turnEnds() string {
	roles := l.heldRoles()
	if len(roles) == 0 {
		return ""
	}
	role := roles[l.rng.IntN(len(roles))]
	var turn tickTurn
	switch x := l.rng.IntN(10); {
	case l.cooperative:
		turn = l.cooperate(role)
	case x < 2:
		turn = tickTurn{fail: "The " + role + "'s run was lost"}
	case role == l.writer:
		turn = tickTurn{final: "Done."}
	case x < 6:
		turn = tickTurn{final: "Verdict: approve\nGood."}
	case x < 8:
		turn = tickTurn{final: "Verdict: request_changes\nNot yet."}
	default:
		turn = tickTurn{final: "Still reading."}
	}
	l.finish(role, turn)
	l.drain()
	return fmt.Sprintf("the @%s's turn ends: %s", role, describeTurn(turn))
}

func (l *liveWorld) turnCancelled() string {
	roles, members := l.memberTasks()
	if len(roles) == 0 {
		return ""
	}
	role := roles[l.rng.IntN(len(roles))]
	err := l.f.tasks.Repo.RequestCancel(l.f.ctx, l.f.ws, members[role].ID)
	l.drain()
	return fmt.Sprintf("the owner cancels the @%s's turn (%s): %v", role, members[role].State, err)
}

// writerStarts is the invariant on the writer's turns: a turn that leaves
// the head and the unmet checks as they were makes no progress, however it
// was asked for, and tickStallLimit of them pause for a person (KUA-297). So
// the writer never starts more than tickStallLimit turns in a row on the
// same ones unless a person decided in between.
func (l *liveWorld) writerStarts() {
	l.t.Helper()
	key := readTickState(l.task()).Stall.Key
	if key == "" || key != l.stuckKey || strings.HasSuffix(key, "|") {
		l.stuck, l.stuckKey = 0, key
		return
	}
	if l.stuck++; l.stuck >= tickStallLimit {
		l.violation("the writer starts its turn %d in a row with nothing changed (%s)", l.stuck+1, key)
	}
}

// decide is a person's decision, which starts the writer's count again.
func (l *liveWorld) decide(decision string) error {
	l.stuck, l.stuckKey = 0, ""
	return l.tickWorld.decide(decision)
}

func (l *liveWorld) decision(decision string) string {
	err := l.decide(decision)
	if err != nil && !errors.Is(err, ErrWorkflowDecision) {
		require.NoError(l.t, err, "decision %s", decision)
	}
	l.drain()
	return fmt.Sprintf("the owner decides %s: %v", decision, err)
}

func (l *liveWorld) ownerApproves() string  { return l.decision(WorkflowDecisionApprove) }
func (l *liveWorld) ownerContinues() string { return l.decision(WorkflowDecisionContinue) }
func (l *liveWorld) ownerStops() string     { return l.decision(WorkflowDecisionStop) }

func (l *liveWorld) ownerSays() string {
	roles := []string{l.writer}
	for _, member := range l.p.Roster {
		if !member.Writer {
			roles = append(roles, member.Role)
		}
	}
	role := roles[l.rng.IntN(len(roles))]
	_, err := l.f.tasks.Repo.PostRoomMessage(l.f.ctx, l.f.ws, l.root, domain.TaskRoomPost{Kind: domain.RoomSay, Body: "Also follow the system theme.", ToRoles: []string{role}})
	l.drain()
	return fmt.Sprintf("the owner tells the @%s something: %v", role, err)
}

func (l *liveWorld) ownerWaives() string {
	var checks []string
	for _, phase := range l.p.Phases {
		for _, check := range phase.Checks {
			if check.Required {
				checks = append(checks, check.ID)
			}
		}
	}
	check := checks[l.rng.IntN(len(checks))]
	_, err := l.f.tasks.Repo.PostRoomMessage(l.f.ctx, l.f.ws, l.root, domain.TaskRoomPost{Kind: domain.RoomWaiver, Data: domain.TaskRoomData{Check: check}})
	l.drain()
	return fmt.Sprintf("the owner waives %s: %v", check, err)
}

func (l *liveWorld) githubFails() string {
	l.github.downFor = 1 + l.rng.IntN(3)
	l.elapse(70 * time.Second)
	return fmt.Sprintf("GitHub fails for %d event(s)", l.github.downFor)
}

func (l *liveWorld) githubSlow() string {
	l.github.slowFor = 1 + l.rng.IntN(2)
	l.elapse(70 * time.Second)
	return fmt.Sprintf("GitHub answers too slowly for %d event(s)", l.github.slowFor)
}

func (l *liveWorld) scriptCheckEnds() string {
	if !l.pendingCheck() {
		return ""
	}
	exit := []int{0, 0, 1, 2}[l.rng.IntN(4)]
	l.runCheck(exit, fmt.Sprintf("exit %d\n", exit))
	l.drain()
	return fmt.Sprintf("a script check exits %d", exit)
}

func (l *liveWorld) checkoutAway() string {
	l.checkoutFor = 1 + l.rng.IntN(3)
	l.elapse(70 * time.Second)
	return fmt.Sprintf("the worktree is not on a running machine for %d event(s)", l.checkoutFor)
}

// budgetPasses spends the task's time budget. The task's clock is moved
// back rather than the tick's forward: the repository counts the budget a
// person grants from the real time, which would otherwise lag behind.
func (l *liveWorld) budgetPasses() string {
	d := time.Duration(l.p.Budget.MaxMinutes+60) * time.Minute
	l.exec(`UPDATE agent_tasks SET workflow_started_at=workflow_started_at-$2::interval,workflow_budget_from=workflow_budget_from-$2::interval WHERE id=$1`, l.root, fmt.Sprintf("%d seconds", int(d.Seconds())))
	l.wake()
	l.drain()
	return "the time budget passes: " + d.String()
}

func (l *liveWorld) mergeRefused() string {
	if l.rng.IntN(2) == 0 {
		l.github.refuseFor = 1 + l.rng.IntN(3)
		l.elapse(70 * time.Second)
		return fmt.Sprintf("GitHub refuses merges for %d event(s)", l.github.refuseFor)
	}
	l.github.methodFor = 1 + l.rng.IntN(3)
	l.elapse(70 * time.Second)
	return fmt.Sprintf("the repository allows no merge method for %d event(s)", l.github.methodFor)
}

func (l *liveWorld) migrationCollides() string {
	l.github.collision = !l.github.collision
	l.wake()
	l.drain()
	return fmt.Sprintf("a migration collides with the base: %t", l.github.collision)
}

func (l *liveWorld) pullRequestEnds() string {
	g := l.github
	if g.status.HeadSHA == "" || g.status.State != "open" {
		return ""
	}
	if l.rng.IntN(2) == 0 {
		g.status.State = "closed"
		l.wake()
		l.drain()
		return "someone closes the pull request"
	}
	g.status.State, g.status.MergeCommit = "merged", l.sha(0x5000)
	webhook := l.rng.IntN(10) < 7
	if webhook {
		_, err := l.f.tasks.Repo.CompleteMergedPullRequest(context.Background(), l.f.ws, tickRepository, 12)
		require.NoError(l.t, err)
	}
	l.drain()
	return fmt.Sprintf("someone merges the pull request on GitHub (webhook %t)", webhook)
}

// settle has everyone cooperate until the task ends. A pause the sequence
// left takes one Continue; cooperating, the task never pauses again but for
// its budget: a task that does only makes progress when a person pushes it.
func (l *liveWorld) settle() {
	l.t.Helper()
	l.cooperative = true
	continued := 0
	g := l.github
	g.downFor, g.slowFor, g.refuseFor, g.methodFor, g.unknown, l.checkoutFor = 0, 0, 0, 0, false, 0
	for round := 1; round <= tickLivenessSettle; round++ {
		if l.ended() {
			root := l.task()
			l.t.Logf("seed %d: %s after %d events and %d cooperative rounds, %d worker steps, %d head(s) merged: %s", l.seed, root.State, len(l.events), round-1, l.steps, len(g.merged), oneLine(root.Reason, 200))
			return
		}
		l.note("cooperative round %d", round)
		for _, role := range l.heldRoles() {
			l.finish(role, l.cooperate(role))
		}
		if head := g.status.HeadSHA; head != "" {
			g.ci[head] = "passed"
		}
		for l.pendingCheck() {
			l.runCheck(0, "ok\n")
		}
		root := l.task()
		switch {
		case root.State == "blocked" && deref(root.WorkflowStatus) == domain.WorkflowStatusPaused:
			if !strings.Contains(root.Reason, "budget of") {
				if continued++; continued > 1 {
					l.violation("cooperating, the task paused again and needs a person's Continue each time")
				}
			}
			l.note("     the owner continues: %v", l.decide(WorkflowDecisionContinue))
		case root.State == "awaiting_review" && root.WorkflowHeldSince != nil:
			if err := l.decide(WorkflowDecisionApprove); err != nil {
				_, err = l.f.tasks.Repo.PostRoomMessage(l.f.ctx, l.f.ws, l.root, domain.TaskRoomPost{Kind: domain.RoomSay, Body: "Go on: do what the room asks.", ToRoles: []string{l.writer}})
				l.note("     the owner tells the writer to go on: %v", err)
			} else {
				l.note("     the owner approves")
			}
		}
		l.elapse(3 * time.Minute)
		l.check(true)
	}
	root := l.task()
	l.violation("the task did not end in %d cooperative rounds (merged: %v; state %s)", tickLivenessSettle, g.merged, root.State)
}

var errLiveGitHubDown = errors.New("GitHub answered 502")

// liveGitHub is GitHub for a liveness sequence. The pull request's head
// moves with the writer's publications, Kuayle's updates and other people's
// pushes; the base moves under it, possibly into a conflict; CI reports per
// head; and GitHub fails, answers too slowly, or refuses merges for a few
// events at a time.
type liveGitHub struct {
	*tickGitHub
	l *liveWorld
	// tip is the base's tip; the head is behind commits behind it.
	tip    string
	behind int
	// unknown: GitHub has not computed whether the head merges cleanly.
	conflict, unknown, collision bool
	// ci is the state of CI per head: passed, failed, pending or none.
	ci map[string]string
	// resolves are the commits that merge the base in (and fix what
	// collides with it); ours those the writer committed for Kuayle to publish.
	resolves, ours map[string]bool
	// seen is the head GitHub last reported.
	seen string
	// The events left while GitHub fails, answers too slowly, refuses
	// merges, or allows no merge method.
	downFor, slowFor, refuseFor, methodFor int
	merged                                 []string
}

// sync brings the pull request GitHub reports in line with the world.
func (g *liveGitHub) sync() {
	s := &g.status
	if head := s.HeadSHA; head != g.seen {
		if g.resolves[head] {
			g.behind, g.conflict, g.unknown, g.collision = 0, false, false, false
		}
		if g.ours[head] && s.State == "closed" {
			s.State = "open" // publishing again reopens it
		}
		g.seen = head
	}
	yes, no := true, false
	s.Behind = g.behind
	switch {
	case g.unknown:
		s.Mergeable, s.MergeableState = nil, "unknown"
	case g.conflict:
		s.Mergeable, s.MergeableState = &no, "dirty"
	default:
		s.Mergeable, s.MergeableState = &yes, "clean"
	}
	s.migrationRepair = ""
	if g.collision {
		s.migrationRepair = "Migration 000208 of this branch has the number of 000208_other_feature on main. Next step: renumber it to 000209."
	}
	g.checksHead = s.HeadSHA
	switch g.ci[s.HeadSHA] {
	case "passed":
		g.checks = []PullRequestCheck{{Name: "unit", State: "passed"}}
	case "failed":
		g.checks = []PullRequestCheck{{Name: "unit", State: "failed", URL: "https://github.com/acme/app/actions/runs/77/job/9", Summary: "TestDarkMode failed"}}
	case "none":
		g.checks = nil
	default:
		g.checks = []PullRequestCheck{{Name: "unit", State: "pending"}}
	}
}

func (g *liveGitHub) failure(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if g.downFor > 0 {
		return errLiveGitHubDown
	}
	return nil
}

func (g *liveGitHub) PullRequestStatus(ctx context.Context, ws uuid.UUID, repository string, number int) (*PullRequestStatus, error) {
	g.sync()
	return g.tickGitHub.PullRequestStatus(ctx, ws, repository, number)
}

func (g *liveGitHub) PullRequestChecks(ctx context.Context, ws uuid.UUID, repository string, number int) (string, []PullRequestCheck, ChecksCoverage, error) {
	g.sync()
	return g.prGitHub.PullRequestChecks(ctx, ws, repository, number)
}

// UpdatePullRequestBranch merges the base's tip into the head, as GitHub's
// update-branch does: a commit Kuayle can verify as only that.
func (g *liveGitHub) UpdatePullRequestBranch(ctx context.Context, _ uuid.UUID, _ string, _ int, expected string) (string, error) {
	g.calls = append(g.calls, "update:"+expected)
	g.sync()
	if err := g.failure(ctx); err != nil {
		return "", err
	}
	switch {
	case g.status.State != "open":
		return "", &gh.APIError{Status: http.StatusUnprocessableEntity, Message: "pull request is not open"}
	case g.conflict:
		return "", &gh.APIError{Status: http.StatusUnprocessableEntity, Message: "merge conflict between base and head"}
	case expected != g.status.HeadSHA:
		return "", &gh.APIError{Status: http.StatusUnprocessableEntity, Message: "expected head sha didn't match current head ref"}
	}
	merge := g.l.sha(0x3000)
	g.parents[merge] = []string{expected, g.tip}
	g.status.HeadSHA, g.seen, g.behind, g.unknown = merge, merge, 0, false
	return merge, nil
}

func (g *liveGitHub) MergeTaskPullRequest(ctx context.Context, _ uuid.UUID, _ string, _ int, head, _ string) error {
	g.sync()
	if err := g.failure(ctx); err != nil {
		return err
	}
	switch {
	case g.refuseFor > 0:
		return &gh.ErrMergeRefused{Reason: `Required status check "deploy" is expected.`}
	case g.status.State != "open":
		return &gh.ErrMergeRefused{Reason: "Pull Request is not mergeable"}
	case head != g.status.HeadSHA:
		return &gh.ErrMergeRefused{Reason: "Head branch was modified. Review and try the merge again."}
	}
	g.status.State, g.status.MergeCommit = "merged", g.l.sha(0x5000)
	g.merged = append(g.merged, head)
	return nil
}

func (g *liveGitHub) ResolveMergeMethod(ctx context.Context, _ uuid.UUID, _ string) (string, error) {
	if err := g.failure(ctx); err != nil {
		return "", err
	}
	if g.methodFor > 0 {
		return "", ErrMergeMethodNotAllowed
	}
	return "squash", nil
}

func (g *liveGitHub) RerunFailedJobs(ctx context.Context, _ uuid.UUID, _ string, runs []int64) error {
	if err := g.failure(ctx); err != nil {
		return err
	}
	g.reruns = append(g.reruns, runs)
	g.ci[g.status.HeadSHA] = "pending"
	return nil
}
