package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
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
	// Err is why the pull request could not be read.
	Err error `json:"-"`
}

// TaskVerdict is a reviewing role's verdict on a commit: approve,
// request_changes or comment.
type TaskVerdict struct {
	Commit  string `json:"commit"`
	Verdict string `json:"verdict"`
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
	f := TaskFacts{
		Repository: repository, PullRequest: number, Behind: -1,
		PRExists: FactUnknown, PRDraft: FactUnknown, Merged: FactUnknown, Closed: FactUnknown,
		HeadContainsBase: FactUnknown, Conflict: FactUnknown, MigrationCollision: FactUnknown,
		Approved: FactUnknown, CI: CIFacts{Green: FactUnknown},
	}
	switch {
	case repository == "" || number <= 0:
		f.PRExists = FactNo
		return f
	case pulls == nil:
		f.Err = errNoPullRequestReader
		return f
	}
	status, err := pulls.PullRequestStatus(ctx, ws, repository, number)
	if err != nil {
		f.Err = err
		return f
	}
	f.PRExists, f.PRDraft = FactYes, factOf(status.Draft)
	f.Merged, f.Closed = factOf(status.State == "merged"), factOf(status.State != "open" && status.State != "merged")
	f.HeadSHA, f.BaseRef, f.MergeCommit = status.HeadSHA, status.BaseRef, status.MergeCommit
	if status.State != "open" {
		return f
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
	f.CI = readCIFacts(ctx, pulls, now, ws, repository, number, status)
	return f
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
// pr_draft, conflict, migration_collision and closed; and for verdict when the
// role approved the head.
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
	case factPRExists, factPRDraft, factHeadContainsBase, factConflict, factMigrationCollision, factCIGreen, factVerdict, factApproved, factMerged, factClosed:
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
