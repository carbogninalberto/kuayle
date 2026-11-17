package service

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	gh "github.com/kuayle/kuayle-backend/pkg/github"
	log "github.com/sirupsen/logrus"
)

const (
	// ciClassifyTimeout bounds what Kuayle reads to classify one failed check
	// suite; the webhook classifies from the conclusion if it runs out.
	ciClassifyTimeout = 8 * time.Second
	// maxCIClassifiedChecks bounds the failed checks kept from one suite, and
	// with them the job reads made for their failed steps.
	maxCIClassifiedChecks = 10
	// maxCIClassifiedLogs bounds the job logs read for one suite: the reason
	// is at the end of a possibly large log, and three are enough to prove
	// that every failed job in a normal suite is the runner's.
	maxCIClassifiedLogs = 3
	// ciDetailsTimeout bounds the read of a failed suite's checks for its
	// summary (KUA-206) when the webhook's read was not enough. It runs in
	// the background, never in the webhook or the task worker.
	ciDetailsTimeout = 60 * time.Second
	// maxCIDetailLogs bounds the job logs that read fetches.
	maxCIDetailLogs = 5
	// ciDetailsReads bounds the details reads running at once.
	ciDetailsReads = 4
	// maxCIAnnotationDetails bounds the annotations kept per check for the
	// summary.
	maxCIAnnotationDetails = 10
)

// actionsJobPath finds the job in an Actions check run's details URL, such as
// https://github.com/acme/app/actions/runs/9/job/42.
var actionsJobPath = regexp.MustCompile(`/actions/runs/\d+/job/(\d+)`)

// ciOutcomeFailed reports whether a normalized check suite conclusion is a
// failure Kuayle classifies.
func ciOutcomeFailed(outcome string) bool {
	switch outcome {
	case "failure", "timed_out", "cancelled", "action_required", "startup_failure":
		return true
	}
	return false
}

// ciFailureDetails is what the webhook read about a failed suite's checks
// for the agent (KUA-206): the summary, and whether it is complete or a
// longer read outside the webhook could add to it.
type ciFailureDetails struct {
	Summary  string
	Complete bool
}

// ciFailure classifies a completed check suite. It reads the suite's failed
// checks, their annotations and the end of their job logs when it can; a read
// that fails leaves the classification to the conclusion and whatever was
// read, so a webhook never fails because classification could not reach GitHub.
// What it read also becomes the failure's summary; the summary is complete
// when every failed check's log was read (a timed-out suite is not read here,
// and a cancelled one has nothing worth reading).
func (s *GitHubService) ciFailure(ctx context.Context, ws uuid.UUID, repository, headSHA, conclusion string) (*domain.CIFailure, ciFailureDetails) {
	var checks []domain.CIFailedCheck
	more := false
	details := ciFailureDetails{Complete: conclusion != "failure" && conclusion != "timed_out"}
	if conclusion == "failure" && headSHA != "" {
		var truncated bool
		if read := s.ciChecks; read != nil {
			checks, truncated = read(ctx, ws, repository, headSHA)
		} else {
			checks, truncated = s.readCIChecks(ctx, ws, repository, headSHA)
		}
		more = truncated || len(checks) > maxCIClassifiedLogs
		details.Summary = domain.CIFailureSummary(checks, truncated)
		details.Complete = len(checks) > 0 && !more
		for _, check := range checks {
			if check.LogTail == "" {
				details.Complete = false
			}
		}
	}
	failure := domain.ClassifyCIFailure(domain.CIFailureEvidence{Conclusion: conclusion, Checks: checks, MoreChecks: more})
	return &failure, details
}

// ciDetailReads tracks the background reads of failed suites' details: one
// per commit at a time, at most ciDetailsReads at once. Copies of a
// service share it.
type ciDetailReads struct {
	mu      sync.Mutex
	reading map[string]bool
	slots   chan struct{}
}

func newCIDetailReads() *ciDetailReads {
	return &ciDetailReads{reading: map[string]bool{}, slots: make(chan struct{}, ciDetailsReads)}
}

// start claims key, or reports that a read of it is already running.
func (r *ciDetailReads) start(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.reading[key] {
		return false
	}
	r.reading[key] = true
	return true
}

func (r *ciDetailReads) finish(key string) {
	r.mu.Lock()
	delete(r.reading, key)
	r.mu.Unlock()
}

// ciDetailsRecorder stores what a later read found about a failed suite and
// requests the follow-ups that waited for it (AgentTaskFeedback).
type ciDetailsRecorder interface {
	RecordCIDetails(ctx context.Context, ws uuid.UUID, repository, headSHA, summary string) error
}

// ensureCIFailureDetails reads a failed suite's checks again, with their job
// logs, outside the webhook and the task worker, and records the summary on
// the commit's waiting signals; an empty read is recorded too (the fuller
// summary is kept), so nothing waits for it longer than it takes. One read
// per commit runs at a time, at most ciDetailsReads in all, each within
// ciDetailsTimeout. A read lost to a restart is started again by the
// workflow's CI step; a follow-up waits for it at most
// domain.CIFailureDetailsWait.
func (s *GitHubService) ensureCIFailureDetails(ws uuid.UUID, repository, headSHA string) {
	recorder, ok := s.taskSignals.(ciDetailsRecorder)
	reads := s.ciDetailReads
	if !ok || reads == nil || repository == "" || headSHA == "" {
		return
	}
	key := ws.String() + "/" + strings.ToLower(repository) + "@" + headSHA
	if !reads.start(key) {
		return
	}
	go func() {
		defer reads.finish(key)
		// Outside a request nothing else recovers: a failure here must not
		// stop the server, and the wait it leaves ends on its own.
		defer func() {
			if recovered := recover(); recovered != nil {
				log.WithFields(log.Fields{"workspace_id": ws, "head_sha": headSHA, "panic": recovered}).Error("Reading a failed CI suite's details panicked")
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), ciDetailsTimeout)
		defer cancel()
		summary := ""
		select {
		case reads.slots <- struct{}{}:
			summary = func() string {
				defer func() { <-reads.slots }()
				return s.readCIFailureSummary(ctx, ws, repository, headSHA)
			}()
		case <-ctx.Done():
		}
		write, done := context.WithTimeout(context.Background(), signalWriteTimeout)
		defer done()
		if err := recorder.RecordCIDetails(write, ws, repository, headSHA, summary); err != nil {
			log.WithFields(log.Fields{"workspace_id": ws, "head_sha": headSHA}).WithError(err).Warn("Could not record a failed CI suite's details")
		}
	}()
}

// readCIFailureSummary reads a failed suite's checks, their annotations and
// up to maxCIDetailLogs job logs for its summary; empty when nothing could be
// read in time.
func (s *GitHubService) readCIFailureSummary(ctx context.Context, ws uuid.UUID, repository, headSHA string) string {
	var checks []domain.CIFailedCheck
	var truncated bool
	if read := s.ciDetails; read != nil {
		checks, truncated = read(ctx, ws, repository, headSHA)
	} else {
		checks, truncated = s.readCIChecksFromGitHub(ctx, ws, repository, headSHA, maxCIDetailLogs)
	}
	return domain.CIFailureSummary(checks, truncated)
}

// readCIChecks reads a failed suite's checks within ciClassifyTimeout. The
// GitHub read continues in the background if it overruns; its result is only
// ever used to classify this signal.
func (s *GitHubService) readCIChecks(ctx context.Context, ws uuid.UUID, repository, headSHA string) ([]domain.CIFailedCheck, bool) {
	readCtx, cancel := context.WithTimeout(ctx, ciClassifyTimeout)
	defer cancel()
	type answer struct {
		checks    []domain.CIFailedCheck
		truncated bool
	}
	read := make(chan answer, 1)
	go func() {
		checks, truncated := s.readCIChecksFromGitHub(readCtx, ws, repository, headSHA, maxCIClassifiedLogs)
		read <- answer{checks, truncated}
	}()
	select {
	case got := <-read:
		return got.checks, got.truncated
	case <-readCtx.Done():
		return nil, false
	}
}

// readCIChecksFromGitHub reads a commit's failed checks: each one's failed
// step, its annotations and the end of at most maxLogs job logs.
func (s *GitHubService) readCIChecksFromGitHub(ctx context.Context, ws uuid.UUID, repository, headSHA string, maxLogs int) ([]domain.CIFailedCheck, bool) {
	client, token, owner, name, err := s.repositoryClient(ctx, ws, repository)
	if err != nil {
		return nil, false
	}
	runs, truncated, err := client.ListCheckRunsContext(ctx, token, owner, name, headSHA)
	if err != nil {
		return nil, false
	}
	checks := ciFailedChecks(runs)
	logs := 0
	for i := range checks {
		run := checks[i].run
		id := actionsJobID(run.DetailsURL)
		if id > 0 {
			if job, err := client.GetWorkflowJobContext(ctx, token, owner, name, id); err == nil {
				checks[i].check.FailedStep = failedWorkflowStep(job.Steps)
			}
		}
		if run.ID > 0 && run.AnnotationsCount > 0 {
			if annotations, err := client.ListCheckRunAnnotationsContext(ctx, token, owner, name, run.ID); err == nil {
				for _, annotation := range annotations {
					if message := strings.TrimSpace(annotation.Message); message != "" {
						checks[i].check.Annotations = append(checks[i].check.Annotations, message)
					}
				}
				checks[i].check.AnnotationDetails = ciAnnotationDetails(annotations)
			}
		}
		if id > 0 && logs < maxLogs {
			logs++
			if tail, err := client.GetWorkflowJobLog(ctx, token, owner, name, id, gh.MaxWorkflowJobLogBytes); err == nil {
				checks[i].check.LogTail = tail
			}
		}
	}
	failed := make([]domain.CIFailedCheck, len(checks))
	for i := range checks {
		failed[i] = checks[i].check
	}
	return failed, truncated
}

// ciAnnotationDetails are a check's annotations as a person reads them:
// "path:line: title: message", failures before warnings and notices.
func ciAnnotationDetails(annotations []gh.CheckRunAnnotation) []string {
	var failures, others []string
	for _, annotation := range annotations {
		message := strings.TrimSpace(annotation.Message)
		if message == "" {
			continue
		}
		line := message
		if title := strings.TrimSpace(annotation.Title); title != "" && !strings.Contains(message, title) {
			line = title + ": " + line
		}
		// GitHub files a step's own error under ".github".
		if path := strings.TrimSpace(annotation.Path); path != "" && path != ".github" {
			location := path
			if annotation.StartLine > 0 {
				location = fmt.Sprintf("%s:%d", path, annotation.StartLine)
			}
			line = location + ": " + line
		}
		if annotation.Level == "failure" {
			failures = append(failures, line)
		} else {
			others = append(others, line)
		}
	}
	details := append(failures, others...)
	if len(details) > maxCIAnnotationDetails {
		details = details[:maxCIAnnotationDetails]
	}
	return details
}

// failedCheckRun pairs a failed check with the run it came from, whose ID
// names its annotations and whose details URL names its job.
type failedCheckRun struct {
	run   gh.CheckRun
	check domain.CIFailedCheck
}

// ciFailedChecks keeps the completed checks that did not pass, with what they
// printed. It reads no annotations or logs itself; the caller enriches at
// most maxCIClassifiedChecks of them.
func ciFailedChecks(runs []gh.CheckRun) []failedCheckRun {
	checks := make([]failedCheckRun, 0, len(runs))
	for _, run := range runs {
		if len(checks) >= maxCIClassifiedChecks {
			break
		}
		if run.Status != "" && run.Status != "completed" {
			continue
		}
		switch run.Conclusion {
		case "success", "neutral", "skipped":
			continue
		}
		checks = append(checks, failedCheckRun{
			run:   run,
			check: domain.CIFailedCheck{Name: run.Name, Conclusion: run.Conclusion, Text: ciCheckText(run)},
		})
	}
	return checks
}

func ciCheckText(run gh.CheckRun) string {
	parts := make([]string, 0, 3)
	for _, part := range []string{run.Output.Title, run.Output.Summary, run.Output.Text} {
		if part = strings.TrimSpace(part); part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, "\n")
}

// actionsJobID returns the Actions job named by a check run's details URL, or
// 0 for a check run from another CI.
func actionsJobID(detailsURL string) int64 {
	match := actionsJobPath.FindStringSubmatch(detailsURL)
	if len(match) != 2 {
		return 0
	}
	id, err := strconv.ParseInt(match[1], 10, 64)
	if err != nil {
		return 0
	}
	return id
}

// failedWorkflowStep is the first step that did not succeed.
func failedWorkflowStep(steps []gh.WorkflowStep) string {
	for _, step := range steps {
		switch step.Conclusion {
		case "failure", "timed_out", "cancelled":
			return step.Name
		}
	}
	return ""
}
