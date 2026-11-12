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
// checks and the failed step of their jobs when it can; a read that fails
// leaves the classification to the conclusion and whatever was read, so a
// webhook never fails because classification could not reach GitHub.
func (s *GitHubService) ciFailure(ctx context.Context, ws uuid.UUID, repository, headSHA, conclusion string) *domain.CIFailure {
	var checks []domain.CIFailedCheck
	if conclusion == "failure" && headSHA != "" {
		read := s.ciChecks
		if read == nil {
			read = s.readCIChecks
		}
		checks = read(ctx, ws, repository, headSHA)
	}
	failure := domain.ClassifyCIFailure(domain.CIFailureEvidence{Conclusion: conclusion, Checks: checks})
	return &failure
}

// readCIChecks reads a failed suite's checks within ciClassifyTimeout. The
// GitHub read continues in the background if it overruns; its result is only
// ever used to classify this signal.
func (s *GitHubService) readCIChecks(ctx context.Context, ws uuid.UUID, repository, headSHA string) []domain.CIFailedCheck {
	read := make(chan []domain.CIFailedCheck, 1)
	go func() {
		read <- s.readCIChecksFromGitHub(ctx, ws, repository, headSHA)
	}()
	select {
	case checks := <-read:
		return checks
	case <-ctx.Done():
		return nil
	case <-time.After(ciClassifyTimeout):
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
	return ciFailedChecks(runs, func(run gh.CheckRun) string {
		id := actionsJobID(run.DetailsURL)
		if id <= 0 {
			return ""
		}
		job, err := client.GetWorkflowJob(token, owner, name, id)
		if err != nil {
			return ""
		}
		return failedWorkflowStep(job.Steps)
	})
}

// ciFailedChecks keeps the completed checks that did not pass, with what they
// printed and the failed step of their job when there is one.
func ciFailedChecks(runs []gh.CheckRun, failedStep func(gh.CheckRun) string) []domain.CIFailedCheck {
	checks := make([]domain.CIFailedCheck, 0, len(runs))
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
		checks = append(checks, domain.CIFailedCheck{
			Name: run.Name, Conclusion: run.Conclusion, FailedStep: failedStep(run), Text: ciCheckText(run),
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
