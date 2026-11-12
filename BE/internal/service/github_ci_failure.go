package service

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	gh "github.com/kuayle/kuayle-backend/pkg/github"
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

// ciFailure classifies a completed check suite. It reads the suite's failed
// checks, their annotations and the end of their job logs when it can; a read
// that fails leaves the classification to the conclusion and whatever was
// read, so a webhook never fails because classification could not reach GitHub.
func (s *GitHubService) ciFailure(ctx context.Context, ws uuid.UUID, repository, headSHA, conclusion string) *domain.CIFailure {
	var checks []domain.CIFailedCheck
	more := false
	if conclusion == "failure" && headSHA != "" {
		read := s.ciChecks
		if read == nil {
			read = s.readCIChecks
		}
		checks = read(ctx, ws, repository, headSHA)
		more = len(checks) > maxCIClassifiedLogs
	}
	failure := domain.ClassifyCIFailure(domain.CIFailureEvidence{Conclusion: conclusion, Checks: checks, MoreChecks: more})
	return &failure
}

// readCIChecks reads a failed suite's checks within ciClassifyTimeout. The
// GitHub read continues in the background if it overruns; its result is only
// ever used to classify this signal.
func (s *GitHubService) readCIChecks(ctx context.Context, ws uuid.UUID, repository, headSHA string) []domain.CIFailedCheck {
	readCtx, cancel := context.WithTimeout(ctx, ciClassifyTimeout)
	defer cancel()
	read := make(chan []domain.CIFailedCheck, 1)
	go func() {
		read <- s.readCIChecksFromGitHub(readCtx, ws, repository, headSHA)
	}()
	select {
	case checks := <-read:
		return checks
	case <-readCtx.Done():
		return nil
	}
}

func (s *GitHubService) readCIChecksFromGitHub(ctx context.Context, ws uuid.UUID, repository, headSHA string) []domain.CIFailedCheck {
	client, token, owner, name, err := s.repositoryClient(ctx, ws, repository)
	if err != nil {
		return nil
	}
	runs, _, err := client.ListCheckRuns(token, owner, name, headSHA)
	if err != nil {
		return nil
	}
	checks := ciFailedChecks(runs)
	logs := 0
	for i := range checks {
		run := checks[i].run
		id := actionsJobID(run.DetailsURL)
		if id > 0 {
			if job, err := client.GetWorkflowJob(token, owner, name, id); err == nil {
				checks[i].check.FailedStep = failedWorkflowStep(job.Steps)
			}
		}
		if run.ID > 0 && run.AnnotationsCount > 0 {
			if annotations, err := client.ListCheckRunAnnotations(token, owner, name, run.ID); err == nil {
				for _, annotation := range annotations {
					if message := strings.TrimSpace(annotation.Message); message != "" {
						checks[i].check.Annotations = append(checks[i].check.Annotations, message)
					}
				}
			}
		}
		if id > 0 && logs < maxCIClassifiedLogs {
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
	return failed
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
