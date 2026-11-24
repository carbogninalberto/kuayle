package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	gh "github.com/kuayle/kuayle-backend/pkg/github"
)

// A workflow's CI check step judges the CI of the commit the task published
// (KUA-215). It records what it judged on the step (domain.WorkflowCIRecord),
// so a later CI step on the same commit knows which failures it already saw:
//
//   - CI that failed on a commit is a result once. When the work comes back to
//     a CI step and the published commit is the same (the agent pushed nothing
//     new), the old failure is not judged again as if it were fresh: Kuayle
//     asks GitHub to run the failed jobs again, once per commit, and waits for
//     that result. If CI passes, the step passes. If it still fails, the
//     workflow pauses for a person instead of sending the same failure back to
//     an agent that changed nothing: continuing sends it back, stopping fails
//     the task.
//   - A failed check run that no webhook reported (GitHub's check list is read
//     directly) gets the same details as a failed suite (KUA-206): Kuayle reads
//     the commit's failed checks, their failed steps, annotations and log ends
//     in the background, and the step waits for that read within
//     domain.CIFailureDetailsWait.
//
// A CI step that would wait for checks first reads its pull request's branch
// (readBranchFacts, KUA-260). CI never starts on a branch that conflicts with
// its base, and CI on a branch behind its base never ran with the base's
// newest commits:
//
//   - A conflict, or a migration version the branch shares with its base,
//     goes back to the first step that changes code at once
//     (domain.WorkflowBranchConflict), as from the ship approval, instead of
//     the step waiting until it times out.
//   - A branch behind its base that merges cleanly is brought up to date with
//     GitHub's update-branch, pinned to the commit the step judges, at most
//     maxMergeUpdates times. The step then judges CI on the new head once it
//     is verified as merges of the base into the published commit
//     (verifiedUpdates); so it does when another step or a person updated the
//     branch that way. A head someone else pushed is not judged: the step
//     goes on judging its commit, as before.
//   - A fact GitHub has not reported changes nothing: the step keeps waiting
//     and reads it again on its next pass. Neither does the branch change CI
//     that already passed or failed: the ship approval and the merge gate
//     check the branch again.

// ciRerunStartWait bounds how long a CI step waits for GitHub to start the
// jobs Kuayle asked it to run again, while the old failures are all it lists.
const ciRerunStartWait = 10 * time.Minute

// ciNoChanges starts the reason a CI step pauses the workflow with.
const ciNoChanges = "No changes were made since CI failed on "

// ciRerunner runs a commit's failed GitHub Actions jobs again
// (GitHubService.RerunFailedJobs).
type ciRerunner interface {
	RerunFailedJobs(ctx context.Context, ws uuid.UUID, repository string, runs []int64) error
}

// ciVerdict is what a CI step decides now.
type ciVerdict struct {
	outcome, detail, waiting string
	wake                     *time.Time
	// record is what the step knows about the commit; save says it changed
	// while the step keeps waiting, so it is stored with the wake-up, and
	// after runs once it is stored.
	record *domain.WorkflowCIRecord
	save   bool
	after  func()
	// stop is set when nothing changed since CI failed on the commit and it
	// still fails: the workflow pauses for a person with this reason.
	stop string
	// failing is set while the step waits only to read a failure's details.
	failing bool
}

// ciPrior is what earlier CI steps of the task judged on the same commit.
type ciPrior struct {
	// failed: an earlier CI step failed on the commit.
	failed bool
	// judged are the failures already judged on it.
	judged map[string]bool
	// rerunAt is when Kuayle asked GitHub to run its failed jobs again, and
	// rerunHere whether the step being judged asked.
	rerunAt   *time.Time
	rerunHere bool
}

// ciFailed is one failed check or suite Kuayle knows of on the commit.
type ciFailed struct {
	id, name string
	run      int64
}

// ciOutcome decides a CI check node without what earlier steps judged; see
// judgeCI.
func (w *WorkflowRunner) ciOutcome(ctx context.Context, task domain.AgentTask, ci *repository.WorkflowCI, now time.Time) (outcome, detail, waiting string, wake *time.Time, err error) {
	v, err := w.judgeCI(ctx, task, nil, ci, now)
	return v.outcome, v.detail, v.waiting, v.wake, err
}

// judgeCI decides the CI check step the task waits on. A failure reported by
// a webhook fails it at once, with what GitHub reported about the failed
// checks; while Kuayle still reads them after the webhook, the step waits for
// that read, at most domain.CIFailureDetailsWait. Passing needs more than
// completed suites, because GitHub only reports suites once they complete and
// a fast one (lint) finishes before a slow one (tests) has reported anything:
// CI passes only when GitHub lists at least one check for the published
// commit, none failed, none is still running and the whole list could be
// read. Otherwise the step keeps waiting (waiting says why) and polls again,
// unless its pull request's branch decides first (ciBranch). On a commit an
// earlier CI step of the task failed on, ciAgain decides.
func (w *WorkflowRunner) judgeCI(ctx context.Context, task domain.AgentTask, step *domain.WorkflowStep, ci *repository.WorkflowCI, now time.Time) (ciVerdict, error) {
	published := ci.CommitSHA
	record := &domain.WorkflowCIRecord{Repository: ci.Repository, Head: published}
	if step != nil {
		if stored := step.CIRecord(); stored != nil && (stored.Head == published || stored.UpdateOf != "" && stored.UpdateOf == published) {
			record = stored
		}
	}
	if record.UpdateOf != "" {
		// The step judges Kuayle's update of the published commit; the
		// suites reported for the published commit say nothing about it.
		ci = &repository.WorkflowCI{Published: ci.Published, Repository: ci.Repository, CommitSHA: record.Head}
	}
	v, err := w.judgeChecks(ctx, task, step, ci, record, now)
	if err != nil || v.outcome != "" || v.stop != "" || v.failing {
		return v, err
	}
	return w.ciBranch(ctx, task, published, v, now)
}

// judgeChecks is judgeCI on the checks of the commit the step judges.
func (w *WorkflowRunner) judgeChecks(ctx context.Context, task domain.AgentTask, step *domain.WorkflowStep, ci *repository.WorkflowCI, record *domain.WorkflowCIRecord, now time.Time) (ciVerdict, error) {
	var prior ciPrior
	if step != nil {
		var err error
		if prior, err = w.ciPrior(ctx, task, *step, ci.CommitSHA); err != nil {
			return ciVerdict{}, err
		}
	}
	v := ciVerdict{record: record}
	if prior.failed {
		return w.ciAgain(ctx, task, ci, prior, v, now)
	}
	if len(ci.Failed) > 0 {
		if w.awaitSignalDetails(task, ci, &v, now) {
			return v, nil
		}
		v.outcome, v.detail = domain.WorkflowChecksFailed, ciSignalsDetail(ci.Failed)
		record.Failed = signalIDs(ci.Failed)
		return v, nil
	}
	idle := now.Add(ciIdlePoll)
	if w.checks == nil || ci.Repository == "" || ci.CommitSHA == "" {
		// Completed suites alone never prove that CI passed.
		v.waiting = "Kuayle cannot read this commit's checks from GitHub"
		return v, nil
	}
	checks, coverage, err := w.checks.CommitChecks(ctx, task.WorkspaceID, ci.Repository, ci.CommitSHA)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return ciVerdict{}, err
		}
		v.waiting, v.wake = "GitHub's checks could not be read", &idle
		return v, nil
	}
	failed, pending := failedChecks(checks)
	switch {
	case len(failed) > 0:
		if w.awaitCheckDetails(task, ci, &v, now) {
			return v, nil
		}
		v.outcome, v.detail = domain.WorkflowChecksFailed, ciChecksDetail(failed, record.Details)
		record.Failed = checkIDs(failed)
	case coverage.incomplete():
		v.waiting, v.wake = "not every check of the commit could be read", &idle
	case pending > 0:
		poll := now.Add(ciPoll)
		v.waiting, v.wake = fmt.Sprintf("%d of %d CI checks still running", pending, len(checks)), &poll
	case len(checks) == 0:
		v.waiting, v.wake = "no CI check has started on the commit", &idle
	default:
		v.outcome, v.detail = domain.WorkflowChecksPassed, fmt.Sprintf("All %d CI checks passed", len(checks))
	}
	return v, nil
}

// ciBranch decides a CI step that would wait (v) from its pull request's
// branch: a conflict or a migration collision goes back at once, and a
// branch behind its base is brought up to date; see the comment at the top
// of the file. Anything else leaves v as it is.
func (w *WorkflowRunner) ciBranch(ctx context.Context, task domain.AgentTask, published string, v ciVerdict, now time.Time) (ciVerdict, error) {
	if w.pulls == nil || published == "" {
		return v, nil
	}
	target, err := w.tasks.WorkflowPullRequest(ctx, task)
	if errors.Is(err, repository.ErrNoReviewTarget) {
		return v, nil
	}
	if err != nil {
		return ciVerdict{}, err
	}
	facts, _ := readBranchFacts(ctx, w.pulls, task.WorkspaceID, target.Repository, target.PullRequestNumber)
	if errors.Is(facts.Err, context.Canceled) || errors.Is(facts.Err, context.DeadlineExceeded) {
		return ciVerdict{}, facts.Err
	}
	if facts.PRExists != FactYes || facts.Merged != FactNo || facts.Closed != FactNo {
		return v, nil
	}
	base := facts.BaseRef
	if base == "" {
		base = target.BaseBranch
	}
	sendBack := func(why string) (ciVerdict, error) {
		return ciVerdict{outcome: domain.WorkflowBranchConflict, detail: why, record: v.record}, nil
	}
	record := v.record
	switch {
	case facts.MigrationCollision == FactYes:
		return sendBack(facts.MigrationRepair)
	case facts.Conflict == FactYes:
		return sendBack(rebaseInstruction(base))
	case facts.HeadSHA != record.Head:
		if moved, ok := w.ciAdopt(ctx, task, target, base, facts.HeadSHA, published, record, now); ok {
			return moved, nil
		}
		return v, nil
	case facts.HeadContainsBase != FactNo || facts.Conflict != FactNo || facts.MigrationCollision != FactNo || record.Updates >= maxMergeUpdates:
		// Up to date, not known yet, or updated as often as a merge would.
		return v, nil
	}
	// Behind its base, and it merges cleanly: every request counts.
	record.Updates++
	v.save = true
	head, updated, conflicts := w.updateBranch(ctx, task, target, record.Head, facts.Behind)
	switch {
	case conflicts:
		return sendBack(rebaseInstruction(base))
	case !updated:
		return v, nil
	case head != "" && head != record.Head:
		if moved, ok := w.ciAdopt(ctx, task, target, base, head, published, record, now); ok {
			return moved, nil
		}
	}
	// GitHub has not made the new head yet, or it is not only the update:
	// the next pass reads the branch again.
	poll := now.Add(ciPoll)
	v.waiting, v.wake = fmt.Sprintf("Kuayle asked GitHub to bring the branch up to date with %s", base), &poll
	return v, nil
}

// ciAdopt makes the step judge head, its pull request's new head, when it
// is the published commit or merges of the base into it, each verified
// (verifiedUpdates). It returns false for any other head, or one GitHub
// could not show yet: the step goes on judging its commit.
func (w *WorkflowRunner) ciAdopt(ctx context.Context, task domain.AgentTask, target *domain.AgentReviewTarget, base, head, published string, from *domain.WorkflowCIRecord, now time.Time) (ciVerdict, bool) {
	if head == "" {
		return ciVerdict{}, false
	}
	updates, why, err := w.verifiedUpdates(ctx, task, target, base, head, published)
	if err != nil || why != "" {
		return ciVerdict{}, false
	}
	record := &domain.WorkflowCIRecord{Repository: from.Repository, Head: head, Updates: max(from.Updates, updates)}
	if head != published {
		record.UpdateOf = published
	}
	poll := now.Add(ciPoll)
	return ciVerdict{record: record, save: true, wake: &poll,
		waiting: fmt.Sprintf("waiting for CI on %s, the branch brought up to date with %s", shortSHA(head), base)}, true
}

// ciPrior reads what the task's earlier CI steps judged on the commit.
func (w *WorkflowRunner) ciPrior(ctx context.Context, task domain.AgentTask, step domain.WorkflowStep, head string) (ciPrior, error) {
	prior := ciPrior{judged: map[string]bool{}}
	if head == "" {
		return prior, nil
	}
	history, err := w.tasks.WorkflowCIHistory(ctx, task, head)
	if err != nil {
		return prior, err
	}
	for _, earlier := range history {
		record := earlier.CIRecord()
		if record == nil || record.Head != head {
			continue
		}
		if earlier.Sequence != step.Sequence && earlier.FinishedAt != nil && earlier.Outcome != nil && *earlier.Outcome == domain.WorkflowChecksFailed {
			prior.failed = true
			for _, id := range record.Failed {
				prior.judged[id] = true
			}
		}
		if record.RerunAt != nil {
			for _, id := range record.Rerun {
				prior.judged[id] = true
			}
			if prior.rerunAt == nil || record.RerunAt.After(*prior.rerunAt) {
				prior.rerunAt, prior.rerunHere = record.RerunAt, earlier.Sequence == step.Sequence
			}
		}
	}
	return prior, nil
}

// ciAgain decides a CI step on a commit an earlier CI step of the task failed
// on: the work came back without a new commit. GitHub's whole check list is
// read; what passes passes, and what still runs is waited for. A failure is
// never sent back to an agent by itself: the first time, Kuayle runs the
// failed jobs again and waits for them; after that, CI that still fails
// pauses the workflow for a person (stop).
func (w *WorkflowRunner) ciAgain(ctx context.Context, task domain.AgentTask, ci *repository.WorkflowCI, prior ciPrior, v ciVerdict, now time.Time) (ciVerdict, error) {
	record := v.record
	idle, poll := now.Add(ciIdlePoll), now.Add(ciPoll)
	if w.checks == nil || ci.Repository == "" || ci.CommitSHA == "" {
		v.waiting = "Kuayle cannot read this commit's checks from GitHub"
		return v, nil
	}
	checks, coverage, err := w.checks.CommitChecks(ctx, task.WorkspaceID, ci.Repository, ci.CommitSHA)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return ciVerdict{}, err
		}
		v.waiting, v.wake = "GitHub's checks could not be read", &idle
		return v, nil
	}
	failedRuns, pending := failedChecks(checks)
	var failures []ciFailed
	for _, signal := range ci.Failed {
		failures = append(failures, ciFailed{id: signalID(signal), name: signalLabel(signal), run: actionsRunID(signal.URL)})
	}
	for _, check := range failedRuns {
		failures = append(failures, ciFailed{id: checkID(check), name: check.Name, run: actionsRunID(check.URL)})
	}
	short := shortSHA(ci.CommitSHA)
	switch {
	case pending > 0:
		v.waiting, v.wake = fmt.Sprintf("%d of %d CI checks still running on %s, which already failed CI once", pending, len(checks), short), &poll
		return v, nil
	case len(failures) == 0 && coverage.incomplete():
		v.waiting, v.wake = "not every check of the commit could be read", &idle
		return v, nil
	case len(failures) == 0 && len(checks) == 0:
		v.waiting, v.wake = "no CI check has started on the commit", &idle
		return v, nil
	case len(failures) == 0:
		v.outcome, v.detail = domain.WorkflowChecksPassed, fmt.Sprintf("All %d CI checks passed on %s, which failed CI before", len(checks), short)
		return v, nil
	}
	var fresh, names []string
	runs := []int64{}
	for _, failure := range failures {
		names = append(names, failure.name)
		if !prior.judged[failure.id] {
			fresh = append(fresh, failure.name)
		}
		if failure.run > 0 && !slices.Contains(runs, failure.run) {
			runs = append(runs, failure.run)
		}
	}
	checksText := oneLine(strings.Join(names, ", "), 300)
	if prior.rerunAt == nil {
		// Nothing new ran: the same failures are no new result. Their jobs
		// run again, once for this commit.
		why := ""
		rerunner, ok := w.checks.(ciRerunner)
		switch {
		case len(runs) == 0:
			why = "Kuayle can only run GitHub Actions jobs again"
		case !ok:
			why = "Kuayle cannot run CI jobs again here"
		default:
			if err := rerunner.RerunFailedJobs(ctx, task.WorkspaceID, ci.Repository, runs); err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return ciVerdict{}, err
				}
				if why = rerunRefusal(err); why == "" {
					v.waiting, v.wake = "GitHub could not be reached to run the failed jobs again", &idle
					return v, nil
				}
			}
		}
		if why != "" {
			v.stop = fmt.Sprintf("%s%s, and CI still fails on %s; %s. Run the failed jobs again on GitHub, then continue, or stop the workflow", ciNoChanges, short, checksText, lowerFirst(why))
			v.outcome, v.detail = domain.WorkflowChecksFailed, ciFailuresDetail(ci.Failed, failedRuns, record.Details)
			record.Failed = failureIDs(failures)
			return v, nil
		}
		at := now
		record.RerunAt, record.Rerun, record.RerunRuns = &at, failureIDs(failures), runs
		// The details read before belong to the jobs that ran before.
		record.DetailsSince, record.DetailsRead, record.Details = nil, false, ""
		v.save, v.waiting, v.wake = true, fmt.Sprintf("no changes were made since CI failed on %s; Kuayle asked GitHub to run the failed jobs again (%s)", short, checksText), &poll
		return v, nil
	}
	if len(fresh) == 0 && prior.rerunHere && now.Before(prior.rerunAt.Add(ciRerunStartWait)) {
		// GitHub still lists the jobs that failed before it started them again.
		v.waiting, v.wake = fmt.Sprintf("waiting for GitHub to start the failed jobs on %s again", short), &poll
		return v, nil
	}
	if len(failedRuns) > 0 && w.awaitCheckDetails(task, ci, &v, now) {
		return v, nil
	}
	if w.awaitSignalDetails(task, ci, &v, now) {
		return v, nil
	}
	again := " after Kuayle ran the failed jobs again"
	if !prior.rerunHere {
		again = ""
	}
	v.stop = fmt.Sprintf("%s%s, and CI still fails on %s%s", ciNoChanges, short, checksText, again)
	v.outcome, v.detail = domain.WorkflowChecksFailed, ciFailuresDetail(ci.Failed, failedRuns, record.Details)
	record.Failed = failureIDs(failures)
	return v, nil
}

// awaitSignalDetails keeps the step waiting while Kuayle reads a failed
// suite's checks after its webhook (KUA-206), at most
// domain.CIFailureDetailsWait; a read lost to a restart starts again.
func (w *WorkflowRunner) awaitSignalDetails(task domain.AgentTask, ci *repository.WorkflowCI, v *ciVerdict, now time.Time) bool {
	for _, signal := range ci.Failed {
		if !signal.CIDetailsAwaited(now) {
			continue
		}
		if reader, ok := w.checks.(ciFailureDetailsReader); ok {
			head := signal.HeadSHA
			if head == "" {
				head = ci.CommitSHA
			}
			reader.ensureCIFailureDetails(task.WorkspaceID, ci.Repository, head)
		}
		poll := now.Add(ciDetailsPoll)
		if until := signal.CreatedAt.Add(domain.CIFailureDetailsWait); until.Before(poll) {
			poll = until
		}
		v.waiting, v.wake, v.failing = "reading the failed checks from GitHub", &poll, true
		return true
	}
	return false
}

// awaitCheckDetails asks Kuayle to read the commit's failed check runs that
// no webhook reported, with the same read as a failed suite's (KUA-206), and
// keeps the step waiting for it, at most domain.CIFailureDetailsWait. The
// read starts once the step recorded that it waits (after), so its result
// always finds the step. Without a reader there is nothing to wait for.
func (w *WorkflowRunner) awaitCheckDetails(task domain.AgentTask, ci *repository.WorkflowCI, v *ciVerdict, now time.Time) bool {
	reader, ok := w.checks.(ciFailureDetailsReader)
	record := v.record
	if !ok || record.DetailsRead {
		return false
	}
	ensure := func() { reader.ensureCIFailureDetails(task.WorkspaceID, ci.Repository, ci.CommitSHA) }
	if record.DetailsSince == nil {
		at := now
		record.DetailsSince = &at
		v.save, v.after = true, ensure
	} else if record.DetailsAwaited(now) {
		// A read lost to a restart starts again; one running is left alone.
		ensure()
	} else {
		return false
	}
	poll := now.Add(ciDetailsPoll)
	if until := record.DetailsSince.Add(domain.CIFailureDetailsWait); until.Before(poll) {
		poll = until
	}
	v.waiting, v.wake, v.failing = "reading the failed checks from GitHub", &poll, true
	return true
}

// rerunRefusal words why GitHub will not run the jobs again, or returns ""
// for a failure that may pass on its own (GitHub unreachable).
func rerunRefusal(err error) string {
	if message, disabled := disabledAbilityMessage(err); disabled {
		return message
	}
	var api *gh.APIError
	if errors.As(err, &api) {
		switch {
		case api.Status == 401 || api.Status == 403 || api.Status == 422 && strings.Contains(strings.ToLower(api.Message), "permission"):
			return "the Kuayle GitHub App lacks the Actions write permission needed to run CI jobs again"
		case api.Status >= 500 || api.Status == 429:
			return ""
		}
		return "GitHub refused to run the failed jobs again: " + oneLine(api.Message, 200)
	}
	return ""
}

// failedChecks splits GitHub's check list into the failed checks and the
// number still running.
func failedChecks(checks []PullRequestCheck) ([]PullRequestCheck, int) {
	var failed []PullRequestCheck
	pending := 0
	for _, check := range checks {
		switch check.State {
		case "failed":
			failed = append(failed, check)
		case "pending":
			pending++
		}
	}
	return failed, pending
}

// signalID identifies a suite's result: a suite run again is reported again.
func signalID(signal domain.AgentTaskSignal) string {
	return "suite:" + signal.ExternalID + "@" + signal.UpdatedAt.UTC().Format(time.RFC3339Nano)
}

// checkID identifies a check's result: an Actions job run again has a new
// link; a check without one is known by its name.
func checkID(check PullRequestCheck) string {
	if check.URL != "" {
		return check.URL
	}
	return "check:" + check.Name
}

func signalIDs(signals []domain.AgentTaskSignal) []string {
	ids := make([]string, 0, len(signals))
	for _, signal := range signals {
		ids = append(ids, signalID(signal))
	}
	return ids
}

func checkIDs(checks []PullRequestCheck) []string {
	ids := make([]string, 0, len(checks))
	for _, check := range checks {
		ids = append(ids, checkID(check))
	}
	return ids
}

func failureIDs(failures []ciFailed) []string {
	ids := make([]string, 0, len(failures))
	for _, failure := range failures {
		ids = append(ids, failure.id)
	}
	return ids
}

func signalLabel(signal domain.AgentTaskSignal) string {
	if label := strings.TrimSpace(signal.FailureEvidence); label != "" {
		return oneLine(label, 200)
	}
	return signal.Name
}

// ciFailuresDetail describes the failed suites, or else the failed checks.
func ciFailuresDetail(signals []domain.AgentTaskSignal, checks []PullRequestCheck, details string) string {
	if len(signals) > 0 {
		return ciSignalsDetail(signals)
	}
	return ciChecksDetail(checks, details)
}

// ciSignalsDetail describes failed suites as webhooks reported them, each
// with its fenced summary, within one notebook finding. A note says when
// every failed suite was the runner's or the network's and may need a re-run.
func ciSignalsDetail(signals []domain.AgentTaskSignal) string {
	var names []string
	everyRunner := true
	for _, signal := range signals {
		if signal.FailureClass == nil || *signal.FailureClass != domain.CIFailureRunner {
			everyRunner = false
		}
		names = append(names, signalLabel(signal))
	}
	note := ""
	if everyRunner {
		note = "\n\nCI failed on the runner; it may need to be run again: " + strings.Join(names, "; ")
	}
	budget := (domain.NotebookFindingBytes - len(note) - 2*(len(signals)-1)) / len(signals)
	lines := make([]string, 0, len(signals))
	for _, signal := range signals {
		lines = append(lines, ciSuiteDetail(fmt.Sprintf("CI check suite %q concluded %q", signal.Name, signal.Outcome), signal.URL, signal.Outcome, signal.Summary, budget))
	}
	return strings.Join(lines, "\n\n") + note
}

// ciChecksDetail describes failed check runs GitHub listed, within one
// notebook finding: each check and its link, then what Kuayle read about the
// commit's failed checks (their failed steps, annotations and log ends), or
// that it could not read them.
func ciChecksDetail(checks []PullRequestCheck, details string) string {
	block := fencedCIOutput(details, domain.NotebookFindingBytes/2)
	room := domain.NotebookFindingBytes - len(block) - 2
	budget := (room - 2*(len(checks)-1)) / max(len(checks), 1)
	lines := make([]string, 0, len(checks)+1)
	for _, check := range checks {
		outcome := "failure"
		if block != "" {
			// The block below says what failed; no check says it cannot.
			outcome = ""
		}
		lines = append(lines, ciSuiteDetail(fmt.Sprintf("CI check %q failed", check.Name), check.URL, outcome, check.Summary, budget))
	}
	if block != "" {
		lines = append(lines, block)
	}
	return strings.Join(lines, "\n\n")
}
