package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
)

// Kuayle reads what is true about a task's pull request without a model, once,
// into TaskFacts (team workflows, KUA-252). The merge gate decides from them,
// and so will the workflow steps and the task's tick. unmetChecks turns the
// facts and the checks a step requires into what still does not hold and why,
// worded for the person or agent who acts on it.
//
// A fact whose source could not be read is unknown, never false: whoever waits
// on it reads it again, and nobody is sent to fix it.

// Fact is a yes/no fact Kuayle read, or FactUnknown when it could not.
type Fact string

const (
	FactUnknown Fact = "unknown"
	FactYes     Fact = "yes"
	FactNo      Fact = "no"
)

func factOf(value bool) Fact {
	if value {
		return FactYes
	}
	return FactNo
}

// TaskFacts is what Kuayle knows about a task's pull request at one moment.
// The facts that need an open pull request stay unknown on a merged or
// closed one.
type TaskFacts struct {
	Repository  string `json:"repository,omitempty"`
	PullRequest int    `json:"pull_request,omitempty"`
	PRExists    Fact   `json:"pr_exists"`
	PRDraft     Fact   `json:"pr_draft"`
	Merged      Fact   `json:"merged"`
	// Closed is a pull request closed without merging.
	Closed      Fact   `json:"closed"`
	HeadSHA     string `json:"head_sha,omitempty"`
	BaseRef     string `json:"base_ref,omitempty"`
	MergeCommit string `json:"merge_commit,omitempty"`
	// Behind counts the base's commits the head lacks; -1 when GitHub did
	// not compare them.
	Behind           int  `json:"behind"`
	HeadContainsBase Fact `json:"head_contains_base"`
	Conflict         Fact `json:"conflict"`
	// MigrationCollision is a migration version the branch shares with a
	// different migration of the base; MigrationRepair tells the task agent
	// how to renumber it.
	MigrationCollision Fact    `json:"migration_collision"`
	MigrationRepair    string  `json:"migration_repair,omitempty"`
	CI                 CIFacts `json:"ci"`
	// Verdicts (each reviewing role's latest) and Approved (a person's
	// approval to merge the head) come from Kuayle's records, not GitHub, so
	// the caller that has them sets them. A verdict's Commit is the commit it
	// covers: the head only when it is that commit, or the caller verified
	// that the head merely merged the base into it (verifyUpdate). Nil
	// Verdicts were not read.
	Verdicts map[string]TaskVerdict `json:"verdicts"`
	Approved Fact                   `json:"approved"`
	// Overlap lists the repository's other active tasks whose branches
	// change some of the same files, and whether each merges first, from the
	// board (boardOverlap); the caller that read the board sets it. Nil: not
	// read.
	Overlap []domain.AgentBoardOverlap `json:"overlap"`
	// Scripts are the runs of a pipeline's script checks for the head, by
	// check ID; the tick that runs them sets them.
	Scripts map[string]ScriptFact `json:"scripts,omitempty"`
	// Err is why the pull request could not be read.
	Err error `json:"-"`
}

// TaskVerdict is a reviewing role's verdict on a commit: approve,
// request_changes or comment.
type TaskVerdict struct {
	Commit  string `json:"commit"`
	Verdict string `json:"verdict"`
}

// ScriptFact is a script check's run for the head: "pending" (not run
// yet), "running", "passed", "failed" (its exit code and the end of its
// output), or "unavailable" (it could not run, and why).
type ScriptFact struct {
	State  string `json:"state"`
	Exit   int    `json:"exit,omitempty"`
	Output string `json:"output,omitempty"`
	Why    string `json:"why,omitempty"`
}

// CIFacts is CI on the pull request's head.
type CIFacts struct {
	// Green is yes when every check on Head passed, or when the repository
	// has no CI at all; no when a check failed or CI is still awaited (State
	// says which); unknown when the checks could not all be read, or were
	// read for another head than TaskFacts.HeadSHA (a push landed between
	// the reads).
	Green Fact `json:"green"`
	// State is summarizeChecks' state: passing, failing, pending,
	// incomplete or none; empty when the checks were not read.
	State    string             `json:"state,omitempty"`
	Head     string             `json:"head,omitempty"`
	Checks   []PullRequestCheck `json:"checks,omitempty"`
	Coverage ChecksCoverage     `json:"-"`
	// Unchecked says why CI is still awaited on a head that reports no
	// checks; empty when the repository has no CI.
	Unchecked string `json:"unchecked,omitempty"`
	Err       error  `json:"-"`
}

// failing lists the failed checks with their links and summaries.
func (c CIFacts) failing() []PullRequestCheck {
	var failed []PullRequestCheck
	for _, check := range c.Checks {
		if check.State == "failed" {
			failed = append(failed, check)
		}
	}
	return failed
}

// mergeGraceWindow is how long a head without checks is assumed to be still
// waiting for CI to register them.
const mergeGraceWindow = 2 * time.Minute

var errNoPullRequestReader = errors.New("Kuayle cannot read pull requests from GitHub here")

// readTaskFacts reads the facts of a task's pull request from GitHub. It
// does not fail: a read that failed leaves its facts unknown, with the error
// in Err or CI.Err. A repository or number that is not set means no pull
// request. now is read only to judge a head that reports no checks.
func readTaskFacts(ctx context.Context, pulls pullRequestReader, now func() time.Time, ws uuid.UUID, repository string, number int) TaskFacts {
	f, status := readBranchFacts(ctx, pulls, ws, repository, number)
	if status != nil && status.State == "open" {
		f.CI = readCIFacts(ctx, pulls, now, ws, repository, number, status)
	}
	return f
}

// readBranchFacts is readTaskFacts without CI, which stays unknown, for a
// caller that judges CI on a commit of its own (a workflow's CI step). It
// also returns the pull request it read, or nil.
func readBranchFacts(ctx context.Context, pulls pullRequestReader, ws uuid.UUID, repository string, number int) (TaskFacts, *PullRequestStatus) {
	f := TaskFacts{
		Repository: repository, PullRequest: number, Behind: -1,
		PRExists: FactUnknown, PRDraft: FactUnknown, Merged: FactUnknown, Closed: FactUnknown,
		HeadContainsBase: FactUnknown, Conflict: FactUnknown, MigrationCollision: FactUnknown,
		Approved: FactUnknown, CI: CIFacts{Green: FactUnknown},
	}
	switch {
	case repository == "" || number <= 0:
		f.PRExists = FactNo
		return f, nil
	case pulls == nil:
		f.Err = errNoPullRequestReader
		return f, nil
	}
	status, err := pulls.PullRequestStatus(ctx, ws, repository, number)
	if err != nil {
		f.Err = err
		return f, nil
	}
	f.PRExists, f.PRDraft = FactYes, factOf(status.Draft)
	f.Merged, f.Closed = factOf(status.State == "merged"), factOf(status.State != "open" && status.State != "merged")
	f.HeadSHA, f.BaseRef, f.MergeCommit = status.HeadSHA, status.BaseRef, status.MergeCommit
	if status.State != "open" {
		return f, status
	}
	f.Behind = status.Behind
	if status.Behind >= 0 {
		f.HeadContainsBase = factOf(status.Behind == 0)
	}
	switch {
	case status.MergeableState == "dirty" || (status.Mergeable != nil && !*status.Mergeable):
		f.Conflict = FactYes
	case status.Mergeable != nil || status.Behind == 0:
		// A head that contains the base's tip merges without conflict.
		f.Conflict = FactNo
	}
	switch {
	case status.migrationRepair != "":
		f.MigrationCollision, f.MigrationRepair = FactYes, status.migrationRepair
	case !status.migrationCheckUnavailable:
		f.MigrationCollision = FactNo
	}
	return f, status
}

// readCIFacts reads the checks of an open pull request's head.
func readCIFacts(ctx context.Context, pulls pullRequestReader, now func() time.Time, ws uuid.UUID, repository string, number int, status *PullRequestStatus) CIFacts {
	ci := CIFacts{Green: FactUnknown}
	head, checks, coverage, err := pulls.PullRequestChecks(ctx, ws, repository, number)
	if err != nil {
		ci.Err = err
		return ci
	}
	ci.Head, ci.Checks, ci.Coverage = head, checks, coverage
	_, ci.State, _ = summarizeChecks(checks, coverage)
	if head != status.HeadSHA {
		return ci
	}
	switch ci.State {
	case "passing":
		ci.Green = FactYes
	case "failing", "pending":
		ci.Green = FactNo
	case "none":
		ci.Unchecked, ci.Err = uncheckedHead(ctx, pulls, now, ws, repository, status)
		if ci.Err == nil {
			ci.Green = factOf(ci.Unchecked == "")
		}
	}
	return ci
}

// uncheckedHead says why CI is still awaited on an up-to-date head that
// reports no checks, or "" when the repository has no CI. A branch update or
// push registers its checks only after a moment, so such a head counts as
// checked only when the base's tip it contains has no checks either and it
// was committed longer than mergeGraceWindow ago.
func uncheckedHead(ctx context.Context, pulls pullRequestReader, now func() time.Time, ws uuid.UUID, repository string, status *PullRequestStatus) (string, error) {
	base := status.BaseRef
	if status.MergeBase == "" {
		return fmt.Sprintf("and the tip of %s it contains is unknown, so it is not known whether the repository has CI", base), nil
	}
	checks, coverage, err := pulls.CommitChecks(ctx, ws, repository, status.MergeBase)
	if err != nil {
		return "", err
	}
	if len(checks) > 0 {
		return fmt.Sprintf("though %s's tip has %d, so CI has not run on it", base, len(checks)), nil
	}
	if coverage.incomplete() {
		return fmt.Sprintf("and the checks of %s's tip could not all be read, so it is not known whether the repository has CI", base), nil
	}
	committed, err := pulls.CommitDate(ctx, ws, repository, status.HeadSHA)
	if err != nil {
		return "", err
	}
	if now().Sub(committed) < mergeGraceWindow {
		return fmt.Sprintf("and it was committed less than %s ago, so CI may not have started", mergeGraceWindow), nil
	}
	return "", nil
}

// FactCheck is a check a step requires: a fact that must hold, and for a
// verdict the role that gives it. A check holds when its fact is yes for
// pr_exists, head_contains_base, ci_green, approved and merged; no for
// pr_draft, conflict, migration_collision and closed; for verdict when the
// role approved the head; and for overlap when no task ahead in the board's
// merge order changes the same files.
type FactCheck struct {
	ID   string `json:"id"`
	Fact string `json:"fact"`
	Role string `json:"role,omitempty"`
}

// The facts a check can name; the list is closed.
const (
	factPRExists           = "pr_exists"
	factPRDraft            = "pr_draft"
	factHeadContainsBase   = "head_contains_base"
	factConflict           = "conflict"
	factMigrationCollision = "migration_collision"
	factCIGreen            = "ci_green"
	factVerdict            = "verdict"
	factApproved           = "approved"
	factMerged             = "merged"
	factClosed             = "closed"
	factOverlap            = "overlap"
	factScript             = "script"
)

// What a check that does not hold waits for.
const (
	// CheckUnmet needs someone to act; the reason says what to do.
	CheckUnmet = "unmet"
	// CheckWaiting changes by itself (CI is running): wait for it.
	CheckWaiting = "waiting"
	// CheckUnknown could not be read: read it again, never act on it.
	CheckUnknown = "unknown"
)

// UnmetCheck is a required check that does not hold, and why.
type UnmetCheck struct {
	FactCheck
	State  string `json:"state"`
	Reason string `json:"reason"`
}

// unmetChecks lists, in order, the checks the facts do not satisfy, each
// with a reason a person or an agent can act on.
func unmetChecks(f TaskFacts, checks []FactCheck) []UnmetCheck {
	var unmet []UnmetCheck
	for _, check := range checks {
		if state, reason := f.judge(check); state != "" {
			unmet = append(unmet, UnmetCheck{FactCheck: check, State: state, Reason: reason})
		}
	}
	return unmet
}

// judge returns "" when the check holds, otherwise its state and reason.
func (f TaskFacts) judge(check FactCheck) (string, string) {
	switch check.Fact {
	case factPRExists, factPRDraft, factHeadContainsBase, factConflict, factMigrationCollision, factCIGreen, factVerdict, factApproved, factMerged, factClosed, factOverlap, factScript:
	default:
		return CheckUnmet, fmt.Sprintf("Kuayle does not know the fact %q. Next step: fix the workflow definition.", check.Fact)
	}
	pr := fmt.Sprintf("pull request #%d", f.PullRequest)
	unreadable := fmt.Sprintf("Kuayle could not read %s from GitHub. It reads it again shortly.", pr)
	switch f.PRExists {
	case FactYes:
	case FactNo:
		return CheckUnmet, "No pull request is published for this task yet. Next step: commit the work so Kuayle publishes it."
	default:
		return CheckUnknown, unreadable
	}
	head := shortSHA(f.HeadSHA)
	switch check.Fact {
	case factPRExists:
		return "", ""
	case factVerdict:
		return f.judgeVerdict(check, head)
	case factScript:
		return f.judgeScript(check.ID, head)
	case factApproved:
		return judgeFact(f.Approved, FactYes, fmt.Sprintf("Nobody has approved merging %s yet. Next step: ask the owner to approve it.", head),
			"Kuayle could not read the approvals. It reads them again shortly.")
	case factMerged:
		if f.Merged == FactYes {
			return "", ""
		}
	}
	// The other facts need an open pull request.
	switch {
	case f.Merged == FactYes:
		return CheckUnmet, fmt.Sprintf("Pull request #%d is already merged.", f.PullRequest)
	case f.Closed == FactYes:
		return CheckUnmet, fmt.Sprintf("Pull request #%d was closed without merging. Next step: reopen it on GitHub, or publish the work again.", f.PullRequest)
	}
	base := f.BaseRef
	switch check.Fact {
	case factMerged:
		return CheckUnmet, fmt.Sprintf("Pull request #%d is not merged yet.", f.PullRequest)
	case factPRDraft:
		return judgeFact(f.PRDraft, FactNo, fmt.Sprintf("Pull request #%d is still a draft. Next step: mark it ready for review.", f.PullRequest), unreadable)
	case factHeadContainsBase:
		return judgeFact(f.HeadContainsBase, FactYes,
			fmt.Sprintf("Pull request #%d is %d commit(s) behind %s, so its CI has not run with %s's latest commits. Next step: bring the branch up to date with %s (GitHub's update-branch, when it does not conflict), then wait for CI on the new head.", f.PullRequest, f.Behind, base, base, base),
			fmt.Sprintf("GitHub did not report whether %s contains the latest %s. Kuayle reads it again shortly.", pr, base))
	case factConflict:
		return judgeFact(f.Conflict, FactNo,
			fmt.Sprintf("Pull request #%d conflicts with %s. Next step: merge %s into the branch, resolve the conflicts and push.", f.PullRequest, base, base),
			fmt.Sprintf("GitHub has not yet said whether %s conflicts with %s. Kuayle reads it again shortly.", pr, base))
	case factMigrationCollision:
		return judgeFact(f.MigrationCollision, FactNo, f.MigrationRepair, migrationCheckUnavailableReason+". It checks again shortly.")
	case factCIGreen:
		return f.judgeCI(pr, head)
	case factOverlap:
		// A task ahead changes the base under this one when it merges; once
		// merged it leaves the board, so the check waits for it.
		if f.Overlap == nil {
			return CheckUnknown, "Kuayle has not read the repository's other active tasks. It reads them again shortly."
		}
		if reason := overlapReason(f.Overlap, base); reason != "" {
			return CheckWaiting, reason
		}
		return "", ""
	}
	return judgeFact(f.Closed, FactNo, "", unreadable)
}

// judgeFact judges a fact that must be want: the opposite is unmet, anything
// else unknown.
func judgeFact(fact, want Fact, unmet, unknown string) (string, string) {
	switch {
	case fact == want:
		return "", ""
	case fact == FactYes || fact == FactNo:
		return CheckUnmet, unmet
	}
	return CheckUnknown, unknown
}

func (f TaskFacts) judgeCI(pr, head string) (string, string) {
	ci := f.CI
	switch {
	case ci.Green == FactYes:
		return "", ""
	case ci.Err != nil || ci.State == "":
		return CheckUnknown, fmt.Sprintf("Kuayle could not read the checks of %s from GitHub. It reads them again shortly.", pr)
	case ci.Head != f.HeadSHA:
		return CheckUnknown, fmt.Sprintf("The head of %s moved while Kuayle read its checks. It reads them again shortly.", pr)
	}
	switch ci.State {
	case "failing":
		var names []string
		for _, check := range ci.failing() {
			names = append(names, check.Name)
		}
		return CheckUnmet, fmt.Sprintf("%d check(s) failed on %s: %s. Next step: read their output, fix the cause and push; CI runs again on the new head.", len(names), head, truncateRunes(strings.Join(names, ", "), 300))
	case "pending":
		pending := 0
		for _, check := range ci.Checks {
			if check.State == "pending" {
				pending++
			}
		}
		return CheckWaiting, fmt.Sprintf("%d of %d check(s) are still running on %s. Kuayle waits for them.", pending, len(ci.Checks), head)
	case "none":
		return CheckWaiting, fmt.Sprintf("No checks have reported on %s yet, %s. Kuayle waits for them.", head, ci.Unchecked)
	}
	return CheckUnknown, fmt.Sprintf("Not every check on %s could be read, so it is not known that CI passed. Kuayle reads them again shortly.", head)
}

// judgeScript judges a script check by its run for the head; the end of a
// failed run's output is fenced as data.
func (f TaskFacts) judgeScript(id, head string) (string, string) {
	result, ok := f.Scripts[id]
	switch {
	case f.Scripts == nil:
		return CheckUnknown, "Kuayle has not read the checks' runs. It reads them again shortly."
	case !ok || result.State == "pending" || result.State == "running":
		return CheckWaiting, fmt.Sprintf("Kuayle runs check %s on %s in the task's worktree and waits for it.", id, head)
	case result.State == "unavailable":
		return CheckUnknown, fmt.Sprintf("Kuayle could not run check %s on %s: %s. It tries again shortly.", id, head, lowerFirst(strings.TrimSuffix(result.Why, ".")))
	case result.State == "failed":
		return CheckUnmet, fmt.Sprintf("Check %s exited %d on %s. The end of its output:\n~~~text\n%s\n~~~\nNext step: fix what it reports and push.", id, result.Exit, head, strings.ReplaceAll(strings.TrimSpace(result.Output), "~~~", "~ ~ ~"))
	}
	return "", ""
}

func (f TaskFacts) judgeVerdict(check FactCheck, head string) (string, string) {
	role := check.Role
	if role == "" {
		return CheckUnmet, fmt.Sprintf("Check %s names no reviewing role. Next step: fix the workflow definition.", check.ID)
	}
	if f.Verdicts == nil {
		return CheckUnknown, "Kuayle has not read the review verdicts. It reads them again shortly."
	}
	verdict, ok := f.Verdicts[role]
	switch {
	case !ok:
		return CheckUnmet, fmt.Sprintf("%s has not reviewed %s yet. Next step: ask %s to review it.", role, head, role)
	case verdict.Commit != f.HeadSHA:
		return CheckUnmet, fmt.Sprintf("%s's verdict is for %s, not the head %s. Next step: ask %s to review %s.", role, shortSHA(verdict.Commit), head, role, head)
	case verdict.Verdict != "approve":
		return CheckUnmet, fmt.Sprintf("%s did not approve %s (%s). Next step: address %s's findings, push, and ask %s to review again.", role, head, verdict.Verdict, role, role)
	}
	return "", ""
}

// roomFacts are the facts a pipeline's tick posts to the task's room when
// they change, in order.
var roomFacts = []string{factPRExists, "head_sha", "base_ref", factPRDraft, factHeadContainsBase, factConflict, factMigrationCollision, factCIGreen, factApproved, factOverlap, factMerged, factClosed}

// roomValue is a fact's value as the room records it; false when unknown.
func (f TaskFacts) roomValue(name string) (any, bool) {
	yesNo := map[string]Fact{factPRExists: f.PRExists, factPRDraft: f.PRDraft, factHeadContainsBase: f.HeadContainsBase, factConflict: f.Conflict,
		factMigrationCollision: f.MigrationCollision, factApproved: f.Approved, factMerged: f.Merged, factClosed: f.Closed}
	if fact, ok := yesNo[name]; ok {
		return fact == FactYes, fact != FactUnknown
	}
	switch name {
	case "head_sha":
		return f.HeadSHA, f.HeadSHA != ""
	case "base_ref":
		return f.BaseRef, f.BaseRef != ""
	case factCIGreen:
		failing := []string{}
		for _, check := range f.CI.failing() {
			failing = append(failing, check.Name)
		}
		return map[string]any{"state": f.CI.State, "head": f.CI.Head, "failing": failing}, f.CI.State != "" && f.CI.Head == f.HeadSHA && f.CI.Green != FactUnknown
	case factOverlap:
		ahead := []string{}
		for _, other := range f.Overlap {
			if other.Ahead {
				ahead = append(ahead, other.IssueIdentifier)
			}
		}
		return ahead, f.Overlap != nil
	}
	return nil, false
}

// actionFact is what an error of a platform action a pipeline's tick runs
// (starting a turn or a script check) means for the check waiting on it:
// CheckWaiting when the action can run at a later pass (the last turn is
// still winding down, the machine is busy or its worktree not ready, no
// task slot is free for the turn), CheckUnmet when it cannot run as asked
// (a role whose profile was archived, a prompt or command Kuayle refuses),
// which a person decides; "" for no error or any other, which is the pass's
// own.
func actionFact(err error) string {
	switch {
	case errors.Is(err, repository.ErrMemberBusy), errors.Is(err, repository.ErrAgentTaskConflict), errors.Is(err, repository.ErrMachineBusy), errors.Is(err, repository.ErrCheckUnavailable),
		errors.Is(err, repository.ErrNoTaskSlot):
		return CheckWaiting
	case errors.Is(err, repository.ErrMemberUnavailable), errors.Is(err, repository.ErrConversationInvalid):
		return CheckUnmet
	}
	return ""
}

// publicationFact is why the publication of a pipeline writer's turn left no
// pull request at a commit, as the room hears it; "" when it did.
func publicationFact(result *domain.DevMachineAgentRunStep) string {
	switch {
	case result.Status == domain.DevMachineAgentRunStepStatusSucceeded:
		return ""
	case domain.PublicationCheckoutBlockReason(result.Summary) != "":
		return domain.PublicationCheckoutBlockReason(result.Summary)
	case result.Status == domain.DevMachineAgentRunStepStatusSkipped:
		return publicationSkippedReason(result)
	}
	return publicationFailureReason(result, "Publishing failed; check the repository connection")
}

// scriptRun is a pipeline script check's latest run, kept in the tick's
// memory: the published commit it checks in the task's worktree, the check
// run in flight, its exit code and the end of its output once it ended, why
// it could not run, and whether the room heard the result.
type scriptRun struct {
	Commit string     `json:"commit"`
	Step   *uuid.UUID `json:"step,omitempty"`
	Exit   *int       `json:"exit,omitempty"`
	Output string     `json:"output,omitempty"`
	Why    string     `json:"why,omitempty"`
	Posted bool       `json:"posted,omitempty"`
}

// scriptOutputBytes bounds the end of a check's output the room keeps.
const scriptOutputBytes = 2000

// pipelineScripts are the script checks of a pipeline's phases up to the
// one at upto.
func pipelineScripts(p domain.Pipeline, upto int) []domain.PipelineCheck {
	var checks []domain.PipelineCheck
	for _, phase := range p.Phases[:upto+1] {
		for _, check := range phase.Checks {
			if check.Fact == domain.PipelineFactScript {
				checks = append(checks, check)
			}
		}
	}
	return checks
}

// readScriptFacts records the runs of the checks that ended, and returns
// each check's fact for the head: its run, when that run's commit is the head
// or one the head merely merged the base into (covered, as for verdicts).
func (s *AgentTaskService) readScriptFacts(ctx context.Context, task domain.AgentTask, checks []domain.PipelineCheck, runs map[string]*scriptRun, head string, covered func(string) (string, error)) (map[string]ScriptFact, error) {
	facts := map[string]ScriptFact{}
	for _, check := range checks {
		run := runs[check.ID]
		if run == nil {
			facts[check.ID] = ScriptFact{State: "pending"}
			continue
		}
		if run.Step != nil {
			status, exit, output, err := s.Repo.PipelineCheckResult(ctx, task, *run.Step, scriptOutputBytes)
			if err != nil {
				return nil, err
			}
			if status == domain.DevMachineAgentRunStepStatusSucceeded || status == domain.DevMachineAgentRunStepStatusFailed {
				run.Step, run.Exit, run.Output, run.Posted = nil, exit, output, false
				if exit == nil {
					run.Why = "the check could not finish"
				}
			}
		}
		fact := ScriptFact{State: "pending"}
		switch commit, err := covered(run.Commit); {
		case err != nil:
			fact = ScriptFact{State: "unavailable", Why: "GitHub did not say whether the head is the commit it checked"}
		case commit != head:
		case run.Step != nil:
			fact.State = "running"
		case run.Exit != nil && *run.Exit == 0:
			fact.State = "passed"
		case run.Exit != nil:
			fact = ScriptFact{State: "failed", Exit: *run.Exit, Output: run.Output}
		case run.Why != "":
			fact = ScriptFact{State: "unavailable", Why: run.Why}
		}
		facts[check.ID] = fact
	}
	return facts, nil
}

// runScriptChecks runs, in the task's worktree through the manual check
// runner, the checks that have no run for the head (or could not run), on
// commit. A check that cannot start now (actionFact) is unavailable and
// tried again at the next pass. It reports whether it changed any run.
func (s *AgentTaskService) runScriptChecks(ctx context.Context, task domain.AgentTask, checks []domain.PipelineCheck, facts map[string]ScriptFact, runs map[string]*scriptRun, commit string) (bool, error) {
	changed := false
	for _, check := range checks {
		if state := facts[check.ID].State; state != "pending" && state != "unavailable" {
			continue
		}
		run := &scriptRun{Commit: commit}
		step, err := s.Repo.RunPipelineCheck(ctx, task, "Pipeline check "+check.ID, []string{"sh", "-c", check.Script})
		switch {
		case actionFact(err) != "":
			run.Why = err.Error()
			facts[check.ID] = ScriptFact{State: "unavailable", Why: run.Why}
		case err != nil:
			return changed, err
		default:
			run.Step = &step.ID
			facts[check.ID] = ScriptFact{State: "running"}
		}
		runs[check.ID], changed = run, true
	}
	return changed, nil
}

// MergeFact is the guarded merge of a pipeline as a fact (KUA-279): what came
// of Kuayle merging the head through the merge gate. A merge that waits (on
// CI, its own branch update, GitHub settling or not answering in time) is
// tried again at the next pass; a refused one says why, for a person.
type MergeFact struct {
	State  string
	Reason string
}

const (
	mergeMerged  = "merged"
	mergeWaiting = "waiting"
	mergeRefused = "refused"
)

// readMergeable merges the pull request at wait.Head through the merge gate,
// as approver (the task's creator when nil, the approval was waived), who
// must still be allowed to decide the task. wait is updated in place; only
// an error reading Kuayle's own records is returned.
func (s *AgentTaskService) readMergeable(ctx, gh context.Context, task domain.AgentTask, target *domain.AgentReviewTarget, wait *domain.MergeWait, approver *uuid.UUID, now time.Time) (MergeFact, error) {
	w := s.workflow
	if w.merger == nil {
		return MergeFact{mergeRefused, "Kuayle cannot merge pull requests here"}, nil
	}
	if wait.Method == "" {
		method, err := w.pulls.ResolveMergeMethod(gh, task.WorkspaceID, target.Repository)
		if errors.Is(err, ErrMergeMethodNotAllowed) {
			return MergeFact{mergeRefused, upperFirst(err.Error())}, nil
		}
		if err != nil {
			return MergeFact{State: mergeWaiting}, nil
		}
		wait.Method = method
	}
	actor := task.CreatorID
	if approver != nil {
		actor = *approver
		access := domain.WithAccess(ctx, domain.Access{WorkspaceID: task.WorkspaceID, UserID: actor})
		if _, err := s.Repo.WorkflowDecisionTask(access, task.WorkspaceID, task.ID); errors.Is(err, repository.ErrConversationNotFound) || errors.Is(err, repository.ErrAgentTaskUnauthorized) {
			return MergeFact{mergeRefused, "The person who approved can no longer merge this task's pull request"}, nil
		} else if err != nil {
			return MergeFact{}, err
		}
	}
	hold, err := w.gate().merge(gh, task.WorkspaceID, actor, target.Repository, target.PullRequestNumber, wait, w.merger)
	switch {
	case err != nil:
		return MergeFact{State: mergeWaiting}, nil // GitHub could not be reached in time
	case hold.merged || hold.mergedElsewhere:
		return MergeFact{State: mergeMerged}, nil
	case hold.waits(wait) && now.Before(wait.Deadline):
		wait.Waiting = truncateRunes(hold.reason, 600)
		return MergeFact{mergeWaiting, wait.Waiting}, nil
	case hold.waits(wait):
		return MergeFact{mergeRefused, mergeWaitExpiredRefusal(target.PullRequestNumber, wait)}, nil
	}
	return MergeFact{mergeRefused, hold.reason}, nil
}
