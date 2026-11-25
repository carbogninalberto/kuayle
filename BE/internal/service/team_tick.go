package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	log "github.com/sirupsen/logrus"
)

// The tick runs a task pinned to a pipeline (KUA-252) in place of the graph
// runner. The root task is the writer: its turns are the task's iterations,
// and between them it waits in awaiting_review, where a pass runs whenever an
// event (a member's turn ending, a webhook, a person's decision) or the
// tick's own poll wakes it. One pass:
//
//  1. reads the facts of the task's pull request (readTaskFacts), the
//     verdicts and the approval that hold for its head, and the room;
//  2. posts each fact whose value changed since it last posted it;
//  3. takes the first phase with an unmet required check as the current one
//     and posts it when it changes, or, with a lead, the phase the lead
//     moved to (team_lead.go);
//  4. runs the platform actions the facts call for, each through the code
//     that already does it: publishing the writer's turn, asking the roles
//     whose verdict an unmet required check needs, bringing a branch behind
//     its base up to date, running failed CI jobs again once, waiting for
//     the owner's approval, and merging through the merge gate;
//  5. starts a turn for every member whose unread posts call for one;
//  6. when nothing is running, pending or awaited, sends the unmet checks
//     with their reasons to the lead, or to the owner of the current phase
//     when there is none, and after tickStallLimit such passes on the same
//     head and checks pauses for a person;
//  7. charges the turns it starts and the working time to the budget;
//  8. waits for the next event, or polls while something changes by itself.
//
// Nothing here knows a particular failure: a new situation is a fact with a
// reason, which reaches whoever owns the phase. The facts reader
// (task_facts.go) says what an action's outcome means: a script check's run,
// the merge (readMergeable), an action that cannot run now or at all
// (actionFact), a publication that left no pull request. A fact that could
// not be read is waited for, never acted on. A person's decisions on the task
// are in workflow_pipeline.go.

const (
	// tickPoll is how soon a pass runs again while something changes by
	// itself: CI is running, a fact could not be read, a branch update or a
	// merge is under way.
	tickPoll = time.Minute
	// tickStallLimit is how many passes in a row may find nobody working on
	// the same head and unmet checks before the task pauses for a person.
	tickStallLimit = 3
	// tickSaveReserve is the part of the worker's step a pass keeps for its
	// own writes after the reads and writes on GitHub.
	tickSaveReserve = 5 * time.Second
)

// tickGitHubTimeout bounds what one pass asks of GitHub. A read that runs
// out is a fact that could not be read: the pass waits and reads again.
var tickGitHubTimeout = 12 * time.Second

// tickState is the tick's memory between passes (agent_tasks.pipeline_state).
type tickState struct {
	// Pass counts the passes that saved; it keys the room posts of a pass,
	// so a pass that runs again after a crash posts nothing twice.
	Pass int `json:"pass"`
	// Facts are the values last posted to the room, by fact.
	Facts map[string]json.RawMessage `json:"facts,omitempty"`
	// Read is the room read so far, Head the head the room last said; a
	// person's waiver holds for the head it was given on (Waivers).
	Read    int64             `json:"read,omitempty"`
	Head    string            `json:"head,omitempty"`
	Waivers map[string]string `json:"waivers,omitempty"`
	// Asked is the head each role was last asked to review.
	Asked map[string]string `json:"asked,omitempty"`
	// Approved is the owner's approval to merge a head.
	Approved *tickApproval `json:"approved,omitempty"`
	// Published is the writer's run Kuayle last published.
	Published string `json:"published,omitempty"`
	// Updates counts Kuayle's updates of the branch since it published
	// UpdatesOf; Rerun is the head whose failed CI jobs it ran again.
	Updates   int    `json:"updates,omitempty"`
	UpdatesOf string `json:"updates_of,omitempty"`
	Rerun     string `json:"rerun,omitempty"`
	// Merge is the merge the guarded phase carries out.
	Merge *domain.MergeWait `json:"merge,omitempty"`
	Stall tickStall         `json:"stall,omitzero"`
	// Wait is since when the current phase waits on the same checks that
	// change by themselves (CI running, a fact not readable).
	Wait tickWait `json:"wait,omitzero"`
	// Scripts are the runs of the script checks, by check ID.
	Scripts map[string]*scriptRun `json:"scripts,omitempty"`
	// Writer holds the write lock (KUA-289), and Moves are when it moved:
	// the repository records both when a writer's turn starts.
	Writer string          `json:"writer,omitempty"`
	Moves  json.RawMessage `json:"moves,omitempty"`
}

type tickWait struct {
	Key   string    `json:"key,omitempty"`
	Since time.Time `json:"since,omitzero"`
}

type tickApproval struct {
	Head string    `json:"head"`
	By   uuid.UUID `json:"by"`
}

// tickStall counts the passes in a row that found nobody working on Head
// with the same unmet checks (Key).
type tickStall struct {
	Key   string `json:"key,omitempty"`
	Head  string `json:"head,omitempty"`
	Count int    `json:"count,omitempty"`
}

func readTickState(task domain.AgentTask) tickState {
	var st tickState
	_ = json.Unmarshal(task.PipelineState, &st)
	return st
}

// tickPass is one pass over a claimed task.
type tickPass struct {
	s        *AgentTaskService
	w        *WorkflowRunner
	task     domain.AgentTask
	p        domain.Pipeline
	profiles map[uuid.UUID]domain.AgentProfile
	st       tickState
	now      time.Time
	target   *domain.AgentReviewTarget
	facts    TaskFacts
	writer   domain.PipelineMember
	members  map[string]domain.AgentTask
	turns    int
	readSeq  int64
	// current is the phase the task is in, as the lead last moved it.
	current string
	// gh bounds the pass's calls to GitHub (tickGitHubTimeout).
	gh context.Context
	// stop ends the pass when the budget has no turn left for a member.
	stop *repository.WorkflowEntry
	// busy: a member works or a turn just started; pending: an action is
	// under way; poll: something changes by itself and is read again soon;
	// person: the owner's approval is awaited.
	busy, pending, poll, person bool
}

// pipelineTurnEnded is the worker's report that the writer's turn ended,
// failed when failure is set: its tick decides what follows.
func (s *AgentTaskService) pipelineTurnEnded(ctx context.Context, task domain.AgentTask, failure string) error {
	if task.WorkflowPauseRequest != nil {
		return s.Repo.ParkWorkflow(ctx, task)
	}
	if failure != "" {
		if err := s.postTurnFailed(ctx, task, "", task.RunID, failure); err != nil {
			return err
		}
	}
	// A turn that changed the work is published at once, as any task's.
	st := readTickState(task)
	entry, due, err := s.publishTurn(ctx, task, &st)
	if err != nil || !due {
		if err != nil {
			return err
		}
		return s.pipelineWait(ctx, task, "the writer's turn ended")
	}
	state, err := json.Marshal(st)
	if err != nil {
		return err
	}
	_, err = s.Repo.ApplyPipeline(ctx, task, repository.PipelineApply{Sequence: task.WorkflowSequence, Phase: deref(task.WorkflowNode), State: state, ClaimedWake: task.WorkflowWakeAt, Entry: entry})
	return err
}

// pipelinePublished is the worker's report of the publication of the
// writer's turn: anything but a pull request at a commit is a fact.
func (s *AgentTaskService) pipelinePublished(ctx context.Context, task domain.AgentTask, result *domain.DevMachineAgentRunStep) error {
	if why := publicationFact(result); why != "" {
		value, _ := json.Marshal(map[string]string{"status": string(result.Status), "run": task.RunID.String()})
		if err := s.kuaylePost(ctx, task, domain.TaskRoomPost{Kind: domain.RoomFact, Body: why,
			Data: domain.TaskRoomData{Fact: "publication", Value: value}, DedupeKey: "publication:" + task.RunID.String()}); err != nil {
			return err
		}
	} else {
		s.notePublished(ctx, task)
	}
	return s.pipelineWait(ctx, task, "Kuayle published the writer's turn")
}

// pipelineWait moves the task to its tick, which runs at once.
func (s *AgentTaskService) pipelineWait(ctx context.Context, task domain.AgentTask, why string) error {
	now := time.Now()
	_, err := s.Repo.ApplyPipeline(ctx, task, repository.PipelineApply{Sequence: task.WorkflowSequence, Phase: deref(task.WorkflowNode), State: task.PipelineState, ClaimedWake: task.WorkflowWakeAt,
		Entry: repository.WorkflowEntry{Kind: repository.WorkflowEnterWait, WakeAt: &now, Reason: domain.WorkflowReasonPrefix + upperFirst(why) + "; Kuayle reads the facts"}})
	return err
}

// postTurnFailed posts that a role's turn (run) failed and why: a fact like
// any other, which reaches the owner of the phase. role "" is the writer.
func (s *AgentTaskService) postTurnFailed(ctx context.Context, root domain.AgentTask, role string, run uuid.UUID, reason string) error {
	value, _ := json.Marshal(map[string]string{"role": role, "run": run.String(), "state": "failed"})
	who := "the writer's"
	if role != "" {
		who = "the @" + role + "'s"
	}
	return s.kuaylePost(ctx, root, domain.TaskRoomPost{Kind: domain.RoomFact, Body: upperFirst(who) + " turn failed: " + oneLine(reason, 1000),
		Data: domain.TaskRoomData{Fact: "turn", Value: value}, DedupeKey: "turn:" + run.String()})
}

func (s *AgentTaskService) kuaylePost(ctx context.Context, root domain.AgentTask, post domain.TaskRoomPost) error {
	post.AuthorKind = domain.RoomAuthorKuayle
	_, err := s.Repo.RecordRoomMessage(ctx, root.ID, post)
	return err
}

// pipelineTick runs one pass of the task's tick.
func (s *AgentTaskService) pipelineTick(ctx context.Context, task domain.AgentTask) error {
	w := s.workflow
	if w == nil {
		return ErrInvalidOperation
	}
	p, profiles, err := w.pipeline(ctx, task)
	if err != nil {
		return err
	}
	t := &tickPass{s: s, w: w, task: task, p: *p, profiles: profiles, st: readTickState(task), now: w.now(), current: deref(task.WorkflowNode)}
	t.writer = p.Holder(t.st.Writer)
	deadline := time.Now().Add(tickGitHubTimeout)
	if step, ok := ctx.Deadline(); ok && step.Add(-tickSaveReserve).Before(deadline) {
		deadline = step.Add(-tickSaveReserve)
	}
	gh, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	t.gh = gh
	if err = t.read(ctx); err != nil {
		return err
	}
	if t.facts.Err != nil {
		// The pull request could not be read: nothing is posted, nothing is
		// sent anywhere; the pass runs again shortly.
		log.WithField("task_id", task.ID).WithError(t.facts.Err).Warn("A pipeline tick could not read its pull request; it reads it again shortly")
		return t.save(ctx, t.wait("Kuayle could not read the pull request from GitHub and reads it again shortly", true), t.current)
	}
	if err = t.postFacts(ctx); err != nil {
		return err
	}
	if err = t.postScripts(ctx); err != nil {
		return err
	}
	index, unmet := t.phase()
	phase := p.Phases[index]
	if err = t.postChecks(ctx); err != nil {
		return err
	}
	// Without a lead Kuayle names the phase; with one, the lead does.
	if phase.ID != deref(task.WorkflowNode) && p.Lead == nil {
		if err = s.kuaylePost(ctx, task, domain.TaskRoomPost{Kind: domain.RoomPhase, Body: phase.Label, Data: domain.TaskRoomData{Phase: phase.ID},
			DedupeKey: fmt.Sprintf("phase:%d:%s", t.st.Pass, phase.ID)}); err != nil {
			return err
		}
	}
	if t.facts.Merged == FactYes {
		return t.save(ctx, repository.WorkflowEntry{Kind: repository.WorkflowEnterComplete, Reason: fmt.Sprintf("%sPull request #%d was merged", domain.WorkflowReasonPrefix, t.facts.PullRequest)}, phase.ID)
	}
	// The writer's turn is published before anything reads its work.
	if entry, ok, err := s.publishTurn(ctx, task, &t.st); err != nil || ok {
		if err != nil {
			return err
		}
		return t.save(ctx, entry, phase.ID)
	}
	if entry, ok := t.exhausted(0); ok {
		return t.save(ctx, entry, phase.ID)
	}
	if t.target != nil && t.facts.PRExists == FactYes {
		if t.st.Scripts == nil {
			t.st.Scripts = map[string]*scriptRun{}
		}
		if ran, err := s.runScriptChecks(ctx, task, pipelineScripts(t.p, index), t.facts.Scripts, t.st.Scripts, t.target.CommitSHA); err != nil {
			return err
		} else if ran {
			_, unmet = t.phase()
		}
	}
	if err = t.act(ctx, unmet); err != nil {
		return err
	}
	if entry, done, err := t.mergeGuarded(ctx, index, unmet); err != nil || done {
		if err != nil {
			return err
		}
		return t.save(ctx, entry, phase.ID)
	}
	if err = t.deliver(ctx, unmet); err != nil {
		return err
	}
	writerDue, err := t.writerDue(ctx)
	if err != nil {
		return err
	}
	if !writerDue && t.stop == nil {
		// What is sent to a role other than the writer starts its turn now;
		// what is sent to a person waits for the person.
		entry, paused, err := t.stall(ctx, phase, unmet)
		if err != nil || paused {
			if err != nil {
				return err
			}
			return t.save(ctx, entry, phase.ID)
		}
		if err = t.deliver(ctx, unmet); err != nil {
			return err
		}
		if writerDue, err = t.writerDue(ctx); err != nil {
			return err
		}
	}
	if t.stop != nil {
		return t.save(ctx, *t.stop, phase.ID)
	}
	if writerDue {
		entry, err := t.writerTurn(ctx, phase)
		if err != nil {
			return err
		}
		err = t.save(ctx, entry, phase.ID)
		if actionFact(err) != CheckWaiting || entry.Kind != repository.WorkflowEnterIteration {
			return err
		}
		// The writer's last attempt is still being wound down: its turn
		// waits for the next poll.
		t.turns, t.readSeq = t.turns-1, 0
		return t.save(ctx, t.wait(phase.Label+": the writer's next turn waits until its last attempt has stopped", true), phase.ID)
	}
	return t.save(ctx, t.wait(t.reason(phase, unmet), t.poll || t.pending), phase.ID)
}

// read loads the facts, the verdicts and approval that hold for the head,
// the members' tasks and the room since the last pass.
func (t *tickPass) read(ctx context.Context) error {
	s, task := t.s, t.task
	target, err := s.Repo.PublishedWork(ctx, task)
	if err != nil {
		return err
	}
	t.target = target
	if target == nil {
		t.facts = readTaskFacts(t.gh, t.w.pulls, t.w.now, task.WorkspaceID, "", 0)
	} else {
		t.facts = readTaskFacts(t.gh, t.w.pulls, t.w.now, task.WorkspaceID, target.Repository, target.PullRequestNumber)
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if t.members, err = s.Repo.MemberTasks(ctx, task); err != nil {
		return err
	}
	if t.facts.Err != nil || t.facts.PRExists != FactYes {
		return t.readRoom(ctx)
	}
	head := t.facts.HeadSHA
	verdicts, err := s.Repo.LatestVerdicts(ctx, task, "")
	if err != nil {
		return err
	}
	onHead, err := s.Repo.LatestVerdicts(ctx, task, head)
	if err != nil {
		return err
	}
	t.facts.Verdicts = map[string]TaskVerdict{}
	for role, verdict := range verdicts {
		if v, ok := onHead[role]; ok {
			verdict = v
		}
		commit, err := t.covered(verdict.CommitSHA, maxUpdateChain)
		if err != nil {
			t.facts.Verdicts = nil // transient: the verdict checks wait
			break
		}
		t.facts.Verdicts[role] = TaskVerdict{Commit: commit, Verdict: verdict.Verdict}
	}
	t.facts.Approved = FactNo
	if t.st.Approved != nil {
		if commit, err := t.covered(t.st.Approved.Head, maxMergeUpdates); err != nil {
			t.facts.Approved = FactUnknown
		} else {
			t.facts.Approved = factOf(commit == head && head != "")
		}
	}
	covered := func(commit string) (string, error) { return t.covered(commit, maxUpdateChain) }
	if t.facts.Scripts, err = s.readScriptFacts(ctx, task, pipelineScripts(t.p, len(t.p.Phases)-1), t.st.Scripts, head, covered); err != nil {
		return err
	}
	if target != nil {
		if board, err := s.Board(ctx, task.WorkspaceID, target.Repository); err == nil {
			t.facts.Overlap = boardOverlap(board, task.ID)
		}
	}
	return t.readRoom(ctx)
}

// covered is the head when it is commit, or commit plus at most limit merges
// of the base that Kuayle verified (mergeGate.updateChain); commit otherwise.
// A verdict or a script run holds across Kuayle's updates however many
// (maxUpdateChain); the owner's approval across at most the merge's own
// (maxMergeUpdates, KUA-277).
func (t *tickPass) covered(commit string, limit int) (string, error) {
	head := t.facts.HeadSHA
	if commit == head || head == "" || t.target == nil || t.w.pulls == nil {
		return commit, nil
	}
	_, why, err := t.w.gate().updateChain(t.gh, t.task.WorkspaceID, t.target.Repository, t.facts.BaseRef, head, commit, limit)
	if err != nil || why != "" {
		return commit, err
	}
	return head, nil
}

// readRoom reads the room since the last pass: the head Kuayle last named,
// and the waivers the task's creator gave for it.
func (t *tickPass) readRoom(ctx context.Context) error {
	posts, err := t.s.Repo.RoomMessages(ctx, t.task.WorkspaceID, t.task.ID, t.st.Read)
	if err != nil {
		return err
	}
	for _, m := range posts {
		t.st.Read = m.Seq
		switch {
		case m.RejectReason != nil:
		case m.AuthorKind == domain.RoomAuthorKuayle && m.Kind == domain.RoomFact && m.Data.Fact == "head_sha":
			_ = json.Unmarshal(m.Data.Value, &t.st.Head)
		case m.AuthorKind == domain.RoomAuthorMember && m.Kind == domain.RoomPhase:
			t.current = m.Data.Phase // the room takes a member's move only from the lead
		case m.AuthorKind == domain.RoomAuthorHuman && m.Kind == domain.RoomWaiver:
			if t.st.Waivers == nil {
				t.st.Waivers = map[string]string{}
			}
			t.st.Waivers[m.Data.Check] = t.st.Head
		}
	}
	return nil
}

// postScripts posts each script check's result once: a failed advisory
// check to the owner of its phase, which it never holds up.
func (t *tickPass) postScripts(ctx context.Context) error {
	for _, phase := range t.p.Phases {
		for _, check := range phase.Checks {
			run := t.st.Scripts[check.ID]
			if check.Fact != domain.PipelineFactScript || run == nil || run.Exit == nil || run.Posted {
				continue
			}
			body := fmt.Sprintf("Check %s passed on %s.", check.ID, shortSHA(run.Commit))
			var to []string
			if *run.Exit != 0 {
				_, body = TaskFacts{Scripts: map[string]ScriptFact{check.ID: {State: "failed", Exit: *run.Exit, Output: run.Output}}}.judgeScript(check.ID, shortSHA(run.Commit))
				if !check.Required {
					to = []string{t.owner(phase)}
				}
			}
			value, _ := json.Marshal(map[string]any{"check": check.ID, "commit": run.Commit, "exit": *run.Exit})
			if err := t.s.kuaylePost(ctx, t.task, domain.TaskRoomPost{Kind: domain.RoomFact, ToRoles: to, Body: truncateUTF8Bytes(body, domain.RoomMaxBodyBytes),
				Data: domain.TaskRoomData{Fact: domain.PipelineFactScript, Value: value}, DedupeKey: fmt.Sprintf("script:%s:%s:%d", check.ID, run.Commit, t.st.Pass)}); err != nil {
				return err
			}
			run.Posted = true
		}
	}
	return nil
}

// owner is who a phase's unmet checks go to: the lead when there is one;
// otherwise the phase's role or person, and the writer for a phase Kuayle
// runs (the writer changes what blocks it).
func (t *tickPass) owner(phase domain.PipelinePhase) string {
	if t.p.Lead != nil {
		return domain.PipelineLeadRole
	}
	if _, role := t.p.RosterRole(phase.Owner); !role && phase.Owner != domain.PipelineOwnerHuman {
		return t.writer.Role
	}
	return phase.Owner
}

// postFacts posts every fact whose value changed since the room last heard
// it, with what it means for the task when it blocks something.
func (t *tickPass) postFacts(ctx context.Context) error {
	for _, name := range roomFacts {
		value, ok := t.facts.roomValue(name)
		if !ok {
			continue
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return err
		}
		if sameJSON(t.st.Facts[name], raw) {
			continue
		}
		body := ""
		if slices.Contains(domain.PipelineConditionFacts, name) && name != factMerged {
			_, body = t.facts.judge(FactCheck{ID: name, Fact: name})
		}
		if name == factCIGreen && t.facts.CI.State == "failing" {
			body += "\n\n" + ciChecksDetail(t.facts.CI.failing(), "")
		}
		if err = t.s.kuaylePost(ctx, t.task, domain.TaskRoomPost{Kind: domain.RoomFact, Body: truncateUTF8Bytes(body, domain.RoomMaxBodyBytes),
			Data: domain.TaskRoomData{Fact: name, Value: raw}, DedupeKey: fmt.Sprintf("fact:%d:%s:%x", t.st.Pass, name, sha256.Sum256(raw))}); err != nil {
			return err
		}
		if t.st.Facts == nil {
			t.st.Facts = map[string]json.RawMessage{}
		}
		t.st.Facts[name] = raw
	}
	return nil
}

// phase is the first phase with a required check that does not hold and was
// not waived for the head, with those checks; the last phase when all hold.
// With a lead, it is the lead's phase (leadPhase).
func (t *tickPass) phase() (int, []UnmetCheck) {
	if t.p.Lead != nil {
		return t.leadPhase()
	}
	for i, phase := range t.p.Phases {
		if unmet := unmetChecks(t.facts, t.required(phase)); len(unmet) > 0 {
			return i, unmet
		}
	}
	return len(t.p.Phases) - 1, nil
}

// required are a phase's required checks that were not waived for the head.
func (t *tickPass) required(phase domain.PipelinePhase) []FactCheck {
	var checks []FactCheck
	for _, check := range phase.Checks {
		if waived, ok := t.st.Waivers[check.ID]; check.Required && (!ok || waived != t.facts.HeadSHA) {
			checks = append(checks, FactCheck{ID: check.ID, Fact: check.Fact, Role: check.Role})
		}
	}
	return checks
}

// publishTurn is the publication of the task's current turn, when it is due
// (recorded in st): the turn succeeded and changed the work, or nothing is
// published yet, and it was not published already.
func (s *AgentTaskService) publishTurn(ctx context.Context, task domain.AgentTask, st *tickState) (repository.WorkflowEntry, bool, error) {
	if st.Published == task.RunID.String() {
		return repository.WorkflowEntry{}, false, nil
	}
	run, err := s.Repo.WorkflowRun(ctx, task)
	if err != nil || run.Status != string(domain.DevMachineAgentRunStatusSucceeded) {
		return repository.WorkflowEntry{}, false, err
	}
	if run.WorktreeChanged != nil && !*run.WorktreeChanged {
		if published, err := s.Repo.PublishedWork(ctx, task); err != nil || published != nil {
			return repository.WorkflowEntry{}, false, err
		}
	}
	st.Published = task.RunID.String()
	return repository.WorkflowEntry{Kind: repository.WorkflowEnterPublish, Reason: domain.WorkflowReasonPrefix + "Publishing the writer's turn"}, true, nil
}

// exhausted is the entry that stops the task when the budget is used up,
// counting turns more turns about to start.
func (t *tickPass) exhausted(turns int) (repository.WorkflowEntry, bool) {
	budget := domain.WorkflowGraph{Budget: t.p.Budget.Workflow()}
	cursor := t.task.Cursor()
	why := ""
	if deadline := budget.BudgetDeadline(cursor); deadline != nil && !t.now.Before(*deadline) {
		why = fmt.Sprintf("the pipeline's time budget of %d minutes is used up", t.p.Budget.MaxMinutes)
	} else if allowance := budget.Budget.IterationAllowance(cursor); allowance > 0 && turns > 0 && cursor.Iterations+t.turns+turns > allowance {
		why = fmt.Sprintf("the pipeline's budget of %d turns is used up", allowance)
	}
	if why == "" {
		return repository.WorkflowEntry{}, false
	}
	reason := domain.WorkflowReasonPrefix + upperFirst(why)
	if budget.Budget.Policy() == domain.WorkflowOnLimitAsk {
		return repository.WorkflowEntry{Kind: repository.WorkflowEnterPause, Reason: reason + ". Continue or stop the workflow"}, true
	}
	return repository.WorkflowEntry{Kind: repository.WorkflowEnterFail, Reason: reason}, true
}

// act runs the platform actions the facts call for in the current phase.
func (t *tickPass) act(ctx context.Context, unmet []UnmetCheck) error {
	f := t.facts
	var waiting []string
	for _, check := range unmet {
		if check.State != CheckUnmet {
			waiting = append(waiting, check.ID+":"+check.State)
		}
		if check.Fact == factApproved && check.State == CheckUnmet {
			t.person = true // the owner approves through the decision route
		}
	}
	// What changes by itself (CI running, a fact not readable) is read
	// again shortly, for as long as a merge would wait for it; then it is
	// sent on like any unmet check.
	if key := f.HeadSHA + "|" + strings.Join(waiting, ","); len(waiting) == 0 {
		t.st.Wait = tickWait{}
	} else if t.st.Wait.Key != key {
		t.st.Wait = tickWait{Key: key, Since: t.now}
	}
	t.poll = len(waiting) > 0 && t.now.Sub(t.st.Wait.Since) < mergeWaitTimeout
	for role, task := range t.members {
		if domain.MemberState(task.State) != domain.MemberIdle {
			t.busy = true
		} else if task.State == "failed" || task.State == "cancelled" {
			if err := t.s.postTurnFailed(ctx, t.task, role, task.RunID, task.Reason); err != nil {
				return err
			}
		}
	}
	if f.PRExists != FactYes || f.Merged == FactYes || f.Closed == FactYes {
		return nil
	}
	// A branch behind its base that merges cleanly is brought up to date,
	// as often as the merge gate would: maxMergeUpdates times since Kuayle
	// published, and once the owner's approval holds for the head, as many
	// again counted from the approved head (KUA-277).
	if t.st.UpdatesOf != t.target.CommitSHA {
		t.st.Updates, t.st.UpdatesOf = 0, t.target.CommitSHA
	}
	if f.HeadContainsBase == FactNo && f.Conflict == FactNo && f.MigrationCollision == FactNo && t.mayUpdate() {
		t.st.Updates++
		if _, updated, conflicts := t.w.updateBranch(t.gh, t.task, t.target, f.HeadSHA, f.Behind); updated || conflicts {
			t.pending = true
		}
	}
	// CI that fails again on a head the writer was sent and did not change
	// runs its failed jobs once more (as ciAgain does).
	if f.CI.Green == FactNo && f.CI.State == "failing" && t.st.Stall.Head == f.HeadSHA && t.st.Stall.Count > 0 && t.st.Rerun != f.HeadSHA && !t.busy {
		if ran, done := t.w.rerunFailed(t.gh, t.task.WorkspaceID, t.target.Repository, f.CI.failing()); done {
			if t.st.Rerun, t.pending = f.HeadSHA, ran; ran {
				return nil
			}
		}
	}
	return t.askReviewers(ctx, unmet)
}

// mayUpdate reports whether Kuayle may bring the head up to date once more:
// before the owner approved, while the published commit has fewer than
// maxMergeUpdates updates; once the approval holds for the head, while the
// head has fewer than maxMergeUpdates on top of the approved one (KUA-277).
func (t *tickPass) mayUpdate() bool {
	if t.st.Approved == nil || t.facts.Approved != FactYes {
		return t.st.Updates < maxMergeUpdates
	}
	since, why, err := t.w.verifiedUpdates(t.gh, t.task, t.target, t.facts.BaseRef, t.facts.HeadSHA, t.st.Approved.Head)
	return err == nil && why == "" && since < maxMergeUpdates
}

// askReviewers asks each role whose verdict an unmet required check needs
// and that has none for the head, once per head.
func (t *tickPass) askReviewers(ctx context.Context, unmet []UnmetCheck) error {
	head := t.facts.HeadSHA
	for _, check := range unmet {
		verdict, given := t.facts.Verdicts[check.Role]
		if check.Fact != factVerdict || t.facts.Verdicts == nil || (given && verdict.Commit == head) || t.st.Asked[check.Role] == head {
			continue
		}
		started, err := t.startMember(ctx, check.Role, true)
		if err != nil || !started {
			if err != nil {
				return err
			}
			continue
		}
		if t.st.Asked == nil {
			t.st.Asked = map[string]string{}
		}
		t.st.Asked[check.Role] = head
	}
	return nil
}

// owesVerdict reports whether an unmet required check needs a role's
// verdict on the head and does not have it.
func (t *tickPass) owesVerdict(unmet []UnmetCheck, role string) bool {
	verdict, given := t.facts.Verdicts[role]
	return t.facts.Verdicts != nil && (!given || verdict.Commit != t.facts.HeadSHA) && slices.ContainsFunc(unmet, func(c UnmetCheck) bool {
		return c.Fact == factVerdict && c.Role == role
	})
}

// startMember starts a turn of a role that does not write, and says
// whether it did: not while it is busy, nor when the budget has no turn left
// (the pass stops the task then). A role whose last turn is still being
// wound down is tried again at the next poll; one that cannot take a turn at
// all is a fact, and a person decides.
func (t *tickPass) startMember(ctx context.Context, role string, askVerdict bool) (bool, error) {
	if task, ok := t.members[role]; ok && domain.MemberState(task.State) != domain.MemberIdle {
		t.busy = true
		return false, nil
	}
	if entry, out := t.exhausted(1); out {
		t.stop = &entry
		return false, nil
	}
	member, err := t.s.Repo.StartMemberTurn(ctx, t.task.ID, role, askVerdict)
	switch actionFact(err) {
	case CheckWaiting:
		t.poll = true
		return false, nil
	case CheckUnmet:
		why := fmt.Sprintf("The @%s cannot take a turn: %s", role, err)
		value, _ := json.Marshal(map[string]string{"role": role, "state": "unavailable"})
		if err = t.s.kuaylePost(ctx, t.task, domain.TaskRoomPost{Kind: domain.RoomFact, Body: why, Data: domain.TaskRoomData{Fact: "turn", Value: value},
			DedupeKey: fmt.Sprintf("unavailable:%d:%s", t.st.Pass, role)}); err != nil {
			return false, err
		}
		t.stop = &repository.WorkflowEntry{Kind: repository.WorkflowEnterPause, Reason: domain.WorkflowReasonPrefix + why + ". Continue or stop the workflow"}
		return false, nil
	}
	if err != nil {
		return false, err
	}
	t.members[role] = domain.AgentTask{ID: *member.MemberTaskID, State: "queued"}
	t.turns++
	t.busy = true
	return true, nil
}

// mergeGuarded carries out the guarded phase once every required check
// before it holds for the head: the merge is a fact (readMergeable). A
// refused merge is posted and is not tried again on the same head; the next
// pass pauses for a person, unless the facts it reads send the work back. A
// pipeline that guards no merge is done once every check holds.
func (t *tickPass) mergeGuarded(ctx context.Context, index int, unmet []UnmetCheck) (repository.WorkflowEntry, bool, error) {
	last := len(t.p.Phases) - 1
	head := t.facts.HeadSHA
	if index != last || len(unmet) > 0 || t.facts.PRExists != FactYes {
		// The facts sent the work back: a merge starts afresh once they hold.
		t.st.Merge = nil
		return repository.WorkflowEntry{}, false, nil
	}
	if !t.p.Phases[last].Guarded {
		return repository.WorkflowEntry{Kind: repository.WorkflowEnterComplete, Reason: domain.WorkflowReasonPrefix + "Every required check holds for " + shortSHA(head)}, true, nil
	}
	wait := t.st.Merge
	if wait != nil && wait.Head != head {
		// Only Kuayle's own update of the head it merges carries a wait on.
		if covered, err := t.covered(wait.Head, maxMergeUpdates); err != nil || !wait.Updating || covered != head {
			wait = nil
		}
	}
	if wait == nil {
		wait = newMergeWait(head, "", t.now)
		t.st.Merge = wait
	}
	if wait.Failure != "" {
		// The gate refused, and the facts read since do not say why: a
		// person decides (Continue tries again).
		return repository.WorkflowEntry{Kind: repository.WorkflowEnterPause,
			Reason: truncateUTF8Bytes(domain.WorkflowReasonPrefix+"The merge was refused: "+wait.Failure, 1900) + ". Continue to try again, or stop the workflow"}, true, nil
	}
	var approver *uuid.UUID
	if t.st.Approved != nil && t.facts.Approved == FactYes {
		approver = &t.st.Approved.By
	}
	merge, err := t.s.readMergeable(ctx, t.gh, t.task, t.target, wait, approver, t.now)
	switch {
	case err != nil:
		return repository.WorkflowEntry{}, false, err
	case merge.State == mergeMerged:
		return repository.WorkflowEntry{Kind: repository.WorkflowEnterComplete, Reason: fmt.Sprintf("%sMerged pull request #%d at %s", domain.WorkflowReasonPrefix, t.target.PullRequestNumber, shortSHA(wait.Head))}, true, nil
	case merge.State == mergeRefused:
		wait.Failure = truncateRunes(merge.Reason, 1000)
		value, _ := json.Marshal(map[string]string{"head": head, "state": "refused"})
		if err = t.s.kuaylePost(ctx, t.task, domain.TaskRoomPost{Kind: domain.RoomFact, Body: wait.Failure, Data: domain.TaskRoomData{Fact: "merge", Value: value}, DedupeKey: "merge:" + head}); err != nil {
			return repository.WorkflowEntry{}, false, err
		}
	}
	t.pending = true
	return repository.WorkflowEntry{}, false, nil
}

// due reports whether a member's unread posts call for a turn.
func (t *tickPass) due(ctx context.Context, member domain.PipelineMember) (bool, error) {
	inbox, err := t.s.Repo.MemberInbox(ctx, t.task.ID, member.Role)
	return err == nil && domain.RoomWakes(member, inbox), err
}

// writerDue reports whether a writer's turn is due, and makes it the
// writer the pass starts: the one holding the write lock first, then the
// others in roster order. A pass runs only while no writer's turn does, so
// the lock moves only between turns.
func (t *tickPass) writerDue(ctx context.Context) (bool, error) {
	for _, member := range append([]domain.PipelineMember{t.writer}, t.p.Roster...) {
		if !member.Writer {
			continue
		}
		if due, err := t.due(ctx, member); err != nil || due {
			if due {
				t.writer = member
			}
			return due, err
		}
	}
	return false, nil
}

// deliver starts a turn of every idle member, other than the writers, whose
// unread posts call for one, the lead's too; it owes a verdict when an
// unmet required check needs it.
func (t *tickPass) deliver(ctx context.Context, unmet []UnmetCheck) error {
	for _, member := range t.p.Members() {
		if member.Writer {
			continue
		}
		if due, err := t.due(ctx, member); err != nil || !due {
			if err != nil {
				return err
			}
			continue
		}
		if _, err := t.startMember(ctx, member.Role, t.owesVerdict(unmet, member.Role)); err != nil {
			return err
		}
	}
	return nil
}

// stall sends the current phase's unmet checks to its owner when nobody
// works, nothing is under way and nothing is awaited, and to the writer too
// when only the writer can move one (writerActs); after tickStallLimit such
// passes in a row on the same head and checks it pauses for a person.
func (t *tickPass) stall(ctx context.Context, phase domain.PipelinePhase, unmet []UnmetCheck) (repository.WorkflowEntry, bool, error) {
	if t.busy || t.pending || t.poll || t.person || len(unmet) == 0 {
		return repository.WorkflowEntry{}, false, nil
	}
	keys := make([]string, 0, len(unmet))
	reasons := make([]string, 0, len(unmet))
	for _, check := range unmet {
		keys = append(keys, check.ID+":"+check.State)
		reasons = append(reasons, "- "+check.ID+": "+check.Reason)
	}
	key := t.facts.HeadSHA + "|" + phase.ID + "|" + strings.Join(keys, ",")
	if t.st.Stall.Key == key {
		t.st.Stall.Count++
	} else {
		t.st.Stall = tickStall{Key: key, Head: t.facts.HeadSHA, Count: 1}
	}
	if t.st.Stall.Count >= tickStallLimit {
		why := fmt.Sprintf("Nobody made progress on %s in %d passes; %s waits on: %s", shortSHA(t.facts.HeadSHA), t.st.Stall.Count, phase.Label, strings.Join(reasons, " "))
		return repository.WorkflowEntry{Kind: repository.WorkflowEnterPause, Reason: truncateUTF8Bytes(domain.WorkflowReasonPrefix+why, 1900) + ". Continue or stop the workflow"}, true, nil
	}
	owner := t.owner(phase)
	t.person = owner == domain.PipelineOwnerHuman
	to := []string{owner}
	if owner != t.writer.Role && slices.ContainsFunc(unmet, t.writerActs) {
		to = append(to, t.writer.Role)
	}
	body := fmt.Sprintf("Nobody is working, and the phase %s waits on:\n%s", phase.Label, strings.Join(reasons, "\n"))
	return repository.WorkflowEntry{}, false, t.s.kuaylePost(ctx, t.task, domain.TaskRoomPost{Kind: domain.RoomSummary, ToRoles: to,
		Body: truncateUTF8Bytes(body, domain.RoomMaxBodyBytes), DedupeKey: fmt.Sprintf("stall:%d", t.st.Pass)})
}

// writerActs reports whether only the writer can move an unmet verdict
// check: the role's verdict on the head did not approve it, or the role
// reviewed the newest commit Kuayle published and the head is another one
// (someone else pushed it), which no role can review.
func (t *tickPass) writerActs(check UnmetCheck) bool {
	verdict, given := t.facts.Verdicts[check.Role]
	head := t.facts.HeadSHA
	return check.Fact == factVerdict && given && (verdict.Commit == head && verdict.Verdict != "approve" ||
		verdict.Commit != head && t.target != nil && verdict.Commit == t.target.CommitSHA)
}

// writerTurn starts the writer's next turn on the task: the original
// request, the phase it works in and what it has not read in the room.
func (t *tickPass) writerTurn(ctx context.Context, phase domain.PipelinePhase) (repository.WorkflowEntry, error) {
	if entry, out := t.exhausted(1); out {
		return entry, nil
	}
	inbox, err := t.s.Repo.MemberInbox(ctx, t.task.ID, t.writer.Role)
	if err != nil {
		return repository.WorkflowEntry{}, err
	}
	scope, err := t.s.Repo.ScopeInstructions(ctx, t.task)
	if err != nil {
		return repository.WorkflowEntry{}, err
	}
	profile := t.profiles[t.writer.ProfileID]
	base, _, _ := strings.Cut(t.task.Prompt, followUpMarker)
	section := fmt.Sprintf("%sthe Kuayle pipeline, phase %s:\n", followUpMarker, phase.Label)
	if prompt := strings.TrimSpace(phase.Prompt); prompt != "" {
		section += prompt + "\n\n"
	}
	// What does not fit this turn waits for the next one.
	for len(inbox) > 1 && len(section)+len(domain.RoomTurnPrompt(t.writer.Role, inbox)) > 24000 {
		inbox = inbox[:len(inbox)-1]
	}
	section += domain.RoomTurnPrompt(t.writer.Role, inbox)
	if room := 32000 - len(section); len(base) > room {
		base = truncateRunes(base, max(room/4, 0))
	}
	read := int64(0)
	if len(inbox) > 0 {
		read = inbox[len(inbox)-1].Seq
	}
	t.turns++
	t.readSeq = read
	return repository.WorkflowEntry{Kind: repository.WorkflowEnterIteration, Reason: fmt.Sprintf("%s%s: the @%s's turn", domain.WorkflowReasonPrefix, phase.Label, t.writer.Role),
		Prompt: base + section, Instructions: workflowInstructions(scope, profile, domain.WorkflowNode{}), Provider: profile.Harness, Model: profile.Model, Thinking: profile.Effort}, nil
}

// wait is the entry that waits for the next event, or polls.
func (t *tickPass) wait(why string, poll bool) repository.WorkflowEntry {
	entry := repository.WorkflowEntry{Kind: repository.WorkflowEnterWait, Reason: domain.WorkflowReasonPrefix + why, NeedsPerson: t.person}
	var wake *time.Time
	if poll {
		wake = ptr(t.now.Add(tickPoll))
	}
	// The time budget also ends a wait; one that has passed (a pass that
	// could not read GitHub could not stop the task) is looked at again
	// after a poll, not at once.
	if deadline := (domain.WorkflowGraph{Budget: t.p.Budget.Workflow()}).BudgetDeadline(t.task.Cursor()); deadline != nil && !t.person {
		if !deadline.After(t.now) {
			deadline = ptr(t.now.Add(tickPoll))
		}
		if wake == nil || deadline.Before(*wake) {
			wake = deadline
		}
	}
	entry.WakeAt = wake
	return entry
}

// reason says what the current phase waits on, for the task's card.
func (t *tickPass) reason(phase domain.PipelinePhase, unmet []UnmetCheck) string {
	switch {
	case len(unmet) > 0:
		return phase.Label + ": " + oneLine(unmet[0].Reason, 600)
	case t.st.Merge != nil && t.st.Merge.Failure != "":
		return phase.Label + ": " + oneLine(t.st.Merge.Failure, 600)
	case t.st.Merge != nil && t.st.Merge.Waiting != "":
		return phase.Label + ": " + oneLine(t.st.Merge.Waiting, 600)
	}
	return phase.Label
}

// save stores the pass: the phase, the tick's memory, the turns charged and
// what happens to the task next.
func (t *tickPass) save(ctx context.Context, entry repository.WorkflowEntry, phase string) error {
	t.st.Pass++
	state, err := json.Marshal(t.st)
	if err != nil {
		return err
	}
	apply := repository.PipelineApply{Sequence: t.task.WorkflowSequence, Phase: phase, State: state, Entry: entry, Turns: t.turns, ReadSeq: t.readSeq, ClaimedWake: t.task.WorkflowWakeAt, Writer: t.writer}
	if entry.Kind == repository.WorkflowEnterPause {
		apply.Notes = []domain.NotebookNote{{Event: "Paused for a person's decision: " + strings.TrimPrefix(entry.Reason, domain.WorkflowReasonPrefix)}}
	}
	_, err = t.s.Repo.ApplyPipeline(ctx, t.task, apply)
	return err
}

// sameJSON reports whether two JSON values are equal, however stored.
func sameJSON(a, b json.RawMessage) bool {
	var x, y any
	return json.Unmarshal(a, &x) == nil && json.Unmarshal(b, &y) == nil && reflect.DeepEqual(x, y)
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
