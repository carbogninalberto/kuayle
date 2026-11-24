package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
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
//     (no lead decides it in this version) and posts it when it changes;
//  4. runs the platform actions the facts call for, each through the code
//     that already does it: publishing the writer's turn, asking the current
//     phase's reviewers for a verdict on the head, bringing a branch behind
//     its base up to date, running failed CI jobs again once, waiting for
//     the owner's approval, and merging through the merge gate;
//  5. starts a turn for every member whose unread posts call for one;
//  6. when nothing is running, pending or awaited, sends the unmet checks
//     with their reasons to the owner of the current phase, and after
//     tickStallLimit such passes on the same head and checks pauses for a
//     person;
//  7. charges the turns it starts and the working time to the budget;
//  8. waits for the next event, or polls while something changes by itself.
//
// Nothing here knows a particular failure: a new situation is a fact with a
// reason, which reaches whoever owns the phase. A fact that could not be read
// is waited for, never acted on.

const (
	// tickPoll is how soon a pass runs again while something changes by
	// itself: CI is running, a fact could not be read, a branch update or a
	// merge is under way.
	tickPoll = time.Minute
	// tickStallLimit is how many passes in a row may find nobody working on
	// the same head and unmet checks before the task pauses for a person.
	tickStallLimit = 3
)

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

// pipeline is the pipeline a task runs and the profiles of the workspace
// (archived ones too, for the roles that pinned them).
func (w *WorkflowRunner) pipeline(ctx context.Context, task domain.AgentTask) (*domain.Pipeline, map[uuid.UUID]domain.AgentProfile, error) {
	if !task.Pipeline || task.WorkflowVersionID == nil {
		return nil, nil, ErrInvalidOperation
	}
	w.mu.Lock()
	p := w.pipelines[*task.WorkflowVersionID]
	w.mu.Unlock()
	if p == nil {
		version, err := w.definitions.Version(ctx, task.WorkspaceID, *task.WorkflowVersionID)
		if err != nil {
			return nil, nil, err
		}
		if p, err = domain.ParsePipeline(version.Graph); err != nil {
			return nil, nil, err
		}
		w.mu.Lock()
		w.pipelines[*task.WorkflowVersionID] = p
		w.mu.Unlock()
	}
	profiles, err := w.definitions.AllProfiles(ctx, task.WorkspaceID)
	return p, profiles, err
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
	return s.pipelineWait(ctx, task, "the writer's turn ended")
}

// pipelinePublished is the worker's report of the publication of the
// writer's turn: anything but a pull request at a commit is a fact.
func (s *AgentTaskService) pipelinePublished(ctx context.Context, task domain.AgentTask, result *domain.DevMachineAgentRunStep) error {
	if result.Status != domain.DevMachineAgentRunStepStatusSucceeded {
		summary := publicationFailureReason(result, "Publishing failed; check the repository connection")
		if reason := domain.PublicationCheckoutBlockReason(result.Summary); reason != "" {
			summary = reason
		} else if result.Status == domain.DevMachineAgentRunStepStatusSkipped {
			summary = publicationSkippedReason(result)
		}
		value, _ := json.Marshal(map[string]string{"status": string(result.Status), "run": task.RunID.String()})
		if err := s.kuaylePost(ctx, task, domain.TaskRoomPost{Kind: domain.RoomFact, Body: summary,
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
	t := &tickPass{s: s, w: w, task: task, p: *p, profiles: profiles, st: readTickState(task), now: w.now()}
	t.writer, _ = p.Writer()
	if err = t.read(ctx); err != nil {
		return err
	}
	if t.facts.Err != nil {
		// The pull request could not be read: nothing is posted, nothing is
		// sent anywhere; the pass runs again shortly.
		log.WithField("task_id", task.ID).WithError(t.facts.Err).Warn("A pipeline tick could not read its pull request; it reads it again shortly")
		return t.save(ctx, t.wait("Kuayle could not read the pull request from GitHub and reads it again shortly", true), deref(task.WorkflowNode))
	}
	if err = t.postFacts(ctx); err != nil {
		return err
	}
	index, unmet := t.phase()
	phase := p.Phases[index]
	if phase.ID != deref(task.WorkflowNode) {
		if err = s.kuaylePost(ctx, task, domain.TaskRoomPost{Kind: domain.RoomPhase, Body: phase.Label, Data: domain.TaskRoomData{Phase: phase.ID},
			DedupeKey: fmt.Sprintf("phase:%d:%s", t.st.Pass, phase.ID)}); err != nil {
			return err
		}
	}
	if t.facts.Merged == FactYes {
		return t.save(ctx, repository.WorkflowEntry{Kind: repository.WorkflowEnterComplete, Reason: fmt.Sprintf("%sPull request #%d was merged", domain.WorkflowReasonPrefix, t.facts.PullRequest)}, phase.ID)
	}
	// The writer's turn is published before anything reads its work.
	if entry, ok, err := t.publish(ctx); err != nil || ok {
		if err != nil {
			return err
		}
		return t.save(ctx, entry, phase.ID)
	}
	if entry, ok := t.exhausted(0); ok {
		return t.save(ctx, entry, phase.ID)
	}
	if err = t.act(ctx, index, unmet); err != nil {
		return err
	}
	if entry, done, err := t.mergeGuarded(ctx, index, unmet); err != nil || done {
		if err != nil {
			return err
		}
		return t.save(ctx, entry, phase.ID)
	}
	// A merge the gate refused is what the guarded phase waits on.
	if merge := t.st.Merge; merge != nil && merge.Failure != "" && len(unmet) == 0 && index == len(p.Phases)-1 {
		unmet = []UnmetCheck{{FactCheck: FactCheck{ID: "merge", Fact: "merge"}, State: CheckUnmet, Reason: merge.Failure}}
	}
	if err = t.deliver(ctx); err != nil {
		return err
	}
	if t.stop != nil {
		return t.save(ctx, *t.stop, phase.ID)
	}
	writerDue, err := t.due(ctx, t.writer.Role)
	if err != nil {
		return err
	}
	if !writerDue {
		entry, paused, err := t.stall(ctx, phase, unmet)
		if err != nil || paused {
			if err != nil {
				return err
			}
			return t.save(ctx, entry, phase.ID)
		}
		if writerDue, err = t.due(ctx, t.writer.Role); err != nil {
			return err
		}
	}
	if writerDue {
		entry, err := t.writerTurn(ctx, phase)
		if err != nil {
			return err
		}
		return t.save(ctx, entry, phase.ID)
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
		t.facts = readTaskFacts(ctx, t.w.pulls, t.w.now, task.WorkspaceID, "", 0)
	} else {
		t.facts = readTaskFacts(ctx, t.w.pulls, t.w.now, task.WorkspaceID, target.Repository, target.PullRequestNumber)
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
	verdicts, err := s.Repo.LatestVerdicts(ctx, task.ID, "")
	if err != nil {
		return err
	}
	onHead, err := s.Repo.LatestVerdicts(ctx, task.ID, head)
	if err != nil {
		return err
	}
	t.facts.Verdicts = map[string]TaskVerdict{}
	for role, verdict := range verdicts {
		if v, ok := onHead[role]; ok {
			verdict = v
		}
		commit, err := t.covered(ctx, verdict.CommitSHA)
		if err != nil {
			t.facts.Verdicts = nil // transient: the verdict checks wait
			break
		}
		t.facts.Verdicts[role] = TaskVerdict{Commit: commit, Verdict: verdict.Verdict}
	}
	t.facts.Approved = FactNo
	if t.st.Approved != nil {
		if commit, err := t.covered(ctx, t.st.Approved.Head); err != nil {
			t.facts.Approved = FactUnknown
		} else {
			t.facts.Approved = factOf(commit == head && head != "")
		}
	}
	if target != nil {
		if board, err := s.Board(ctx, task.WorkspaceID, target.Repository); err == nil {
			t.facts.Overlap = boardOverlap(board, task.ID)
		}
	}
	return t.readRoom(ctx)
}

// covered is the head when it is commit, or commit plus only merges of the
// base that Kuayle verified (verifiedUpdates); commit otherwise.
func (t *tickPass) covered(ctx context.Context, commit string) (string, error) {
	head := t.facts.HeadSHA
	if commit == head || head == "" || t.target == nil || t.w.pulls == nil {
		return commit, nil
	}
	_, why, err := t.w.verifiedUpdates(ctx, t.task, t.target, t.facts.BaseRef, head, commit)
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
		case m.AuthorKind == domain.RoomAuthorHuman && m.Kind == domain.RoomWaiver:
			if t.st.Waivers == nil {
				t.st.Waivers = map[string]string{}
			}
			t.st.Waivers[m.Data.Check] = t.st.Head
		}
	}
	return nil
}

// tickFacts are the facts a pass posts, in order.
var tickFacts = []string{factPRExists, "head_sha", "base_ref", factPRDraft, factHeadContainsBase, factConflict, factMigrationCollision, factCIGreen, factApproved, factOverlap, factMerged, factClosed}

// factValue is a fact's value as the room records it; false when unknown.
func factValue(f TaskFacts, name string) (any, bool) {
	known := func(fact Fact) (any, bool) { return fact == FactYes, fact != FactUnknown }
	switch name {
	case "head_sha":
		return f.HeadSHA, f.HeadSHA != ""
	case "base_ref":
		return f.BaseRef, f.BaseRef != ""
	case factPRExists:
		return known(f.PRExists)
	case factPRDraft:
		return known(f.PRDraft)
	case factHeadContainsBase:
		return known(f.HeadContainsBase)
	case factConflict:
		return known(f.Conflict)
	case factMigrationCollision:
		return known(f.MigrationCollision)
	case factApproved:
		return known(f.Approved)
	case factMerged:
		return known(f.Merged)
	case factClosed:
		return known(f.Closed)
	case factCIGreen:
		if f.CI.State == "" || f.CI.Head != f.HeadSHA || f.CI.Green == FactUnknown {
			return nil, false
		}
		failing := []string{}
		for _, check := range f.CI.failing() {
			failing = append(failing, check.Name)
		}
		return map[string]any{"state": f.CI.State, "head": f.CI.Head, "failing": failing}, true
	case factOverlap:
		if f.Overlap == nil {
			return nil, false
		}
		ahead := []string{}
		for _, other := range f.Overlap {
			if other.Ahead {
				ahead = append(ahead, other.IssueIdentifier)
			}
		}
		return ahead, true
	}
	return nil, false
}

// postFacts posts every fact whose value changed since the room last heard
// it, with what it means for the task when it blocks something.
func (t *tickPass) postFacts(ctx context.Context) error {
	for _, name := range tickFacts {
		value, ok := factValue(t.facts, name)
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
func (t *tickPass) phase() (int, []UnmetCheck) {
	for i, phase := range t.p.Phases {
		var checks []FactCheck
		for _, check := range phase.Checks {
			if waived, ok := t.st.Waivers[check.ID]; check.Required && (!ok || waived != t.facts.HeadSHA) {
				checks = append(checks, FactCheck{ID: check.ID, Fact: check.Fact, Role: check.Role})
			}
		}
		if unmet := unmetChecks(t.facts, checks); len(unmet) > 0 {
			return i, unmet
		}
	}
	return len(t.p.Phases) - 1, nil
}

// publish sends the writer's finished turn to publication, once per turn,
// when it changed the work or nothing is published yet.
func (t *tickPass) publish(ctx context.Context) (repository.WorkflowEntry, bool, error) {
	run, err := t.s.Repo.WorkflowRun(ctx, t.task)
	if err != nil {
		return repository.WorkflowEntry{}, false, err
	}
	changed := run.WorktreeChanged == nil || *run.WorktreeChanged || t.target == nil
	if run.Status != string(domain.DevMachineAgentRunStatusSucceeded) || !changed || t.st.Published == t.task.RunID.String() {
		return repository.WorkflowEntry{}, false, nil
	}
	t.st.Published = t.task.RunID.String()
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
func (t *tickPass) act(ctx context.Context, index int, unmet []UnmetCheck) error {
	f := t.facts
	for _, check := range unmet {
		if check.State != CheckUnmet {
			t.poll = true // it changes by itself, or is read again
		}
		if check.Fact == factApproved && check.State == CheckUnmet {
			t.person = true // the owner approves through the decision route
		}
	}
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
	// as often as the merge gate would.
	if t.st.UpdatesOf != t.target.CommitSHA {
		t.st.Updates, t.st.UpdatesOf = 0, t.target.CommitSHA
	}
	if f.HeadContainsBase == FactNo && f.Conflict == FactNo && f.MigrationCollision == FactNo && t.st.Updates < maxMergeUpdates {
		t.st.Updates++
		if _, updated, conflicts := t.w.updateBranch(ctx, t.task, t.target, f.HeadSHA, f.Behind); updated || conflicts {
			t.pending = true
		}
	}
	// CI that fails again on a head the writer was sent and did not change
	// runs its failed jobs once more (as ciAgain does).
	if f.CI.Green == FactNo && f.CI.State == "failing" && t.st.Stall.Head == f.HeadSHA && t.st.Stall.Count > 0 && t.st.Rerun != f.HeadSHA && !t.busy {
		if done, err := t.rerun(ctx); err != nil || done {
			return err
		}
	}
	return t.askReviewers(ctx, index)
}

// rerun asks GitHub to run the head's failed Actions jobs again.
func (t *tickPass) rerun(ctx context.Context) (bool, error) {
	rerunner, ok := t.w.checks.(ciRerunner)
	runs := []int64{}
	for _, check := range t.facts.CI.failing() {
		if run := actionsRunID(check.URL); run > 0 && !slices.Contains(runs, run) {
			runs = append(runs, run)
		}
	}
	if !ok || len(runs) == 0 {
		return false, nil
	}
	err := rerunner.RerunFailedJobs(ctx, t.task.WorkspaceID, t.target.Repository, runs)
	if err != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
		return false, err
	}
	if err != nil && rerunRefusal(err) == "" {
		return false, nil // GitHub could not be reached: next pass
	}
	t.st.Rerun = t.facts.HeadSHA
	if err == nil {
		t.pending = true
	}
	return err == nil, nil
}

// askReviewers asks each role whose verdict the current phase requires and
// that has none for the head, once per head.
func (t *tickPass) askReviewers(ctx context.Context, index int) error {
	head := t.facts.HeadSHA
	for _, check := range t.p.Phases[index].Checks {
		verdict, given := t.facts.Verdicts[check.Role]
		if check.Fact != domain.PipelineFactVerdict || !check.Required || t.facts.Verdicts == nil || (given && verdict.Commit == head) || t.st.Asked[check.Role] == head {
			continue
		}
		if err := t.startMember(ctx, check.Role, true); err != nil {
			return err
		}
		if t.st.Asked == nil {
			t.st.Asked = map[string]string{}
		}
		t.st.Asked[check.Role] = head
	}
	return nil
}

// startMember starts a turn of a role that does not write, unless it is
// busy or the budget has no turn left (the next pass stops the task then).
func (t *tickPass) startMember(ctx context.Context, role string, askVerdict bool) error {
	if task, ok := t.members[role]; ok && domain.MemberState(task.State) != domain.MemberIdle {
		t.busy = true
		return nil
	}
	if entry, out := t.exhausted(1); out {
		t.stop = &entry
		return nil
	}
	member, err := t.s.Repo.StartMemberTurn(ctx, t.task.ID, role, askVerdict)
	if errors.Is(err, repository.ErrMemberBusy) {
		t.busy = true
		return nil
	}
	if err != nil {
		return err
	}
	t.members[role] = domain.AgentTask{ID: *member.MemberTaskID, State: "queued"}
	t.turns++
	t.busy = true
	return nil
}

// mergeGuarded merges the pull request through the merge gate once the
// guarded phase is reached: every required check before it holds for the
// head. A merge the gate refuses is a fact, and is not tried again on the
// same head. A pipeline that guards no merge is done once every check holds.
func (t *tickPass) mergeGuarded(ctx context.Context, index int, unmet []UnmetCheck) (repository.WorkflowEntry, bool, error) {
	last := len(t.p.Phases) - 1
	head := t.facts.HeadSHA
	if index != last || len(unmet) > 0 || t.facts.PRExists != FactYes {
		return repository.WorkflowEntry{}, false, nil
	}
	if !t.p.Phases[last].Guarded {
		return repository.WorkflowEntry{Kind: repository.WorkflowEnterComplete, Reason: domain.WorkflowReasonPrefix + "Every required check holds for " + shortSHA(head)}, true, nil
	}
	wait := t.st.Merge
	if wait == nil || (wait.Head != head && !wait.Updating) {
		wait = newMergeWait(head, "", t.now)
		t.st.Merge = wait
	}
	if wait.Failure != "" {
		return repository.WorkflowEntry{}, false, nil
	}
	refuse := func(reason string) error {
		wait.Failure = truncateRunes(reason, 1000)
		value, _ := json.Marshal(map[string]string{"head": head, "state": "refused"})
		return t.s.kuaylePost(ctx, t.task, domain.TaskRoomPost{Kind: domain.RoomFact, Body: wait.Failure, Data: domain.TaskRoomData{Fact: "merge", Value: value}, DedupeKey: "merge:" + head})
	}
	if t.w.merger == nil {
		return repository.WorkflowEntry{}, false, refuse("Kuayle cannot merge pull requests here")
	}
	if wait.Method == "" {
		method, err := t.w.pulls.ResolveMergeMethod(ctx, t.task.WorkspaceID, t.target.Repository)
		if errors.Is(err, ErrMergeMethodNotAllowed) {
			return repository.WorkflowEntry{}, false, refuse(upperFirst(err.Error()))
		}
		if err != nil {
			t.pending = true
			return repository.WorkflowEntry{}, false, nil
		}
		wait.Method = method
	}
	// The merge is the approver's, who must still be allowed to decide.
	actor := t.task.CreatorID
	if t.st.Approved != nil {
		actor = t.st.Approved.By
		access := domain.WithAccess(ctx, domain.Access{WorkspaceID: t.task.WorkspaceID, UserID: actor})
		if _, err := t.s.Repo.WorkflowDecisionTask(access, t.task.WorkspaceID, t.task.ID); errors.Is(err, repository.ErrConversationNotFound) || errors.Is(err, repository.ErrAgentTaskUnauthorized) {
			return repository.WorkflowEntry{}, false, refuse("The person who approved can no longer merge this task's pull request")
		} else if err != nil {
			return repository.WorkflowEntry{}, false, err
		}
	}
	hold, err := t.w.gate().merge(ctx, t.task.WorkspaceID, actor, t.target.Repository, t.target.PullRequestNumber, wait, t.w.merger)
	switch {
	case err != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)):
		return repository.WorkflowEntry{}, false, err
	case err != nil:
		t.pending = true // GitHub could not be reached: next pass
	case hold.merged || hold.mergedElsewhere:
		return repository.WorkflowEntry{Kind: repository.WorkflowEnterComplete, Reason: fmt.Sprintf("%sMerged pull request #%d at %s", domain.WorkflowReasonPrefix, t.target.PullRequestNumber, shortSHA(wait.Head))}, true, nil
	case hold.waits(wait):
		wait.Waiting, t.pending = truncateRunes(hold.reason, 600), true
	default:
		return repository.WorkflowEntry{}, false, refuse(hold.reason)
	}
	return repository.WorkflowEntry{}, false, nil
}

// due reports whether a role's unread posts call for a turn.
func (t *tickPass) due(ctx context.Context, role string) (bool, error) {
	inbox, err := t.s.Repo.MemberInbox(ctx, t.task.ID, role)
	return err == nil && domain.RoomWakes(role, inbox), err
}

// deliver starts a turn of every idle member, other than the writer, whose
// unread posts call for one.
func (t *tickPass) deliver(ctx context.Context) error {
	for _, member := range t.p.Roster {
		if member.Writer {
			continue
		}
		if due, err := t.due(ctx, member.Role); err != nil || !due {
			if err != nil {
				return err
			}
			continue
		}
		if err := t.startMember(ctx, member.Role, false); err != nil {
			return err
		}
	}
	return nil
}

// stall sends the current phase's unmet checks to its owner when nobody
// works, nothing is under way and nothing is awaited; after tickStallLimit
// such passes in a row on the same head and checks it pauses for a person.
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
	owner := phase.Owner
	if _, role := t.p.RosterRole(owner); !role && owner != domain.PipelineOwnerHuman {
		owner = t.writer.Role // Kuayle's own phase: the writer changes what blocks it
	}
	body := fmt.Sprintf("Nobody is working, and the phase %s waits on:\n%s", phase.Label, strings.Join(reasons, "\n"))
	return repository.WorkflowEntry{}, false, t.s.kuaylePost(ctx, t.task, domain.TaskRoomPost{Kind: domain.RoomSummary, ToRoles: []string{owner},
		Body: truncateUTF8Bytes(body, domain.RoomMaxBodyBytes), DedupeKey: fmt.Sprintf("stall:%d", t.st.Pass)})
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
	// The time budget also ends a wait.
	if deadline := (domain.WorkflowGraph{Budget: t.p.Budget.Workflow()}).BudgetDeadline(t.task.Cursor()); deadline != nil && !t.person && (wake == nil || deadline.Before(*wake)) {
		wake = deadline
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
	apply := repository.PipelineApply{Sequence: t.task.WorkflowSequence, Phase: phase, State: state, Entry: entry, Turns: t.turns, ReadSeq: t.readSeq, ClaimedWake: t.task.WorkflowWakeAt}
	if entry.Kind == repository.WorkflowEnterPause {
		apply.Notes = []domain.NotebookNote{{Event: "Paused for a person's decision: " + strings.TrimPrefix(entry.Reason, domain.WorkflowReasonPrefix)}}
	}
	_, err = t.s.Repo.ApplyPipeline(ctx, t.task, apply)
	return err
}

// decidePipeline applies a person's decision on a pipeline task: approving
// the head where a phase waits for the owner's approval, or continuing or
// stopping where the tick paused.
func (s *AgentTaskService) decidePipeline(ctx context.Context, ws uuid.UUID, task domain.AgentTask, sequence int, decision string) (*domain.AgentTask, error) {
	access, ok := domain.AccessFromContext(ctx)
	if !ok || s.workflow == nil || task.WorkflowSequence != sequence || task.WorkflowStatus == nil || task.WorkflowPausedAt != nil {
		return nil, ErrWorkflowDecision
	}
	p, _, err := s.workflow.pipeline(ctx, task)
	if err != nil {
		return nil, err
	}
	st := readTickState(task)
	now := s.workflow.now()
	apply := repository.PipelineApply{Sequence: sequence, Phase: deref(task.WorkflowNode), Entry: repository.WorkflowEntry{Kind: repository.WorkflowEnterWait, WakeAt: &now}}
	phase := p.Phases[max(slices.IndexFunc(p.Phases, func(phase domain.PipelinePhase) bool { return phase.ID == apply.Phase }), 0)]
	switch {
	case *task.WorkflowStatus == domain.WorkflowStatusWaiting && decision == WorkflowDecisionApprove && task.State == "awaiting_review":
		var head string
		_ = json.Unmarshal(st.Facts["head_sha"], &head)
		if head == "" || !slices.ContainsFunc(phase.Checks, func(c domain.PipelineCheck) bool { return c.Fact == domain.PipelineFactApproved }) {
			return nil, ErrWorkflowDecision
		}
		st.Approved = &tickApproval{Head: head, By: access.UserID}
		apply.Entry.Reason = fmt.Sprintf("%s%s: approved %s; Kuayle reads the facts", domain.WorkflowReasonPrefix, phase.Label, shortSHA(head))
		apply.Notes = []domain.NotebookNote{{Event: fmt.Sprintf("A person approved merging %s at %s", shortSHA(head), phase.Label)}}
	case *task.WorkflowStatus == domain.WorkflowStatusPaused && decision == WorkflowDecisionContinue:
		// Every role may be asked again, and the stall counts anew; a budget
		// that is used up grants another of the same size.
		st.Stall, st.Asked, st.Merge = tickStall{}, nil, nil
		t := &tickPass{task: task, p: *p, now: now}
		_, apply.ExtendBudget = t.exhausted(1)
		apply.Entry.Reason = domain.WorkflowReasonPrefix + "Continued by a person; Kuayle reads the facts"
		apply.Notes = []domain.NotebookNote{{Event: fmt.Sprintf("A person chose %q at %s", decision, phase.Label)}}
	case *task.WorkflowStatus == domain.WorkflowStatusPaused && decision == WorkflowDecisionStop:
		apply.Entry = repository.WorkflowEntry{Kind: repository.WorkflowEnterFail, Reason: domain.WorkflowReasonPrefix + "stopped by a person where it paused"}
	default:
		return nil, ErrWorkflowDecision
	}
	if apply.State, err = json.Marshal(st); err != nil {
		return nil, err
	}
	decided, err := s.Repo.DecidePipeline(ctx, ws, task.ID, apply)
	if errors.Is(err, repository.ErrAgentTaskConflict) {
		return nil, ErrWorkflowDecision
	}
	return decided, err
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
