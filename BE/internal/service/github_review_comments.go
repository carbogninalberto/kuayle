package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	log "github.com/sirupsen/logrus"
)

type reviewCommentStore interface {
	ClaimReviewComment(ctx context.Context, lease time.Duration) (*repository.ReviewComment, error)
	FinishReviewComment(ctx context.Context, review domain.AgentReview, posted bool) error
}

// SetReviewComments lets finished agent reviews post the comment their
// requester approved on the pull request.
func (s *GitHubService) SetReviewComments(store reviewCommentStore) {
	s.reviewComments = store
}

// RunReviewComments posts due agent review comments every interval.
func (s *GitHubService) RunReviewComments(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := s.PostReviewComments(ctx); err != nil && ctx.Err() == nil {
				log.WithError(err).Warn("Agent review comments could not be posted")
			}
		}
	}
}

// PostReviewComments posts up to ten due review comments, each once: the
// comment carries a marker of the review, and GitHub is checked for it before
// posting, so a retried or lost response never duplicates it. Only comments
// the requester asked for are ever due; Kuayle never approves or requests
// changes on GitHub. It returns the number posted.
func (s *GitHubService) PostReviewComments(ctx context.Context) (int, error) {
	if s.reviewComments == nil {
		return 0, nil
	}
	post := s.postReviewComment
	if post == nil {
		post = s.githubPostReviewComment
	}
	posted := 0
	for range 10 {
		claimed, err := s.reviewComments.ClaimReviewComment(ctx, 2*time.Minute)
		if err != nil || claimed == nil {
			return posted, err
		}
		review := claimed.Review
		err = post(ctx, review.WorkspaceID, review.Repository, review.PullRequestNumber, reviewCommentMarker(review.TaskID), reviewCommentBody(*claimed))
		if err != nil {
			log.WithError(err).WithField("review_task_id", review.TaskID).Warn("Could not post agent review comment")
		}
		if finishErr := s.reviewComments.FinishReviewComment(ctx, review, err == nil); finishErr != nil {
			return posted, finishErr
		}
		if err == nil {
			posted++
		}
	}
	return posted, nil
}

func reviewCommentMarker(review uuid.UUID) string {
	return "<!-- kuayle-agent-review:" + review.String() + " -->"
}

// reviewCommentBody presents the reviewer's untrusted findings as quoted
// Markdown, with mentions neutralized so the comment cannot notify anyone.
func reviewCommentBody(comment repository.ReviewComment) string {
	review := comment.Review
	verdict := "unknown"
	if review.Verdict != nil {
		verdict = *review.Verdict
	}
	findings := ""
	if review.Findings != nil {
		findings = *review.Findings
	}
	findings = strings.ReplaceAll(findings, "@", "@​")
	short := review.CommitSHA
	if len(short) > 12 {
		short = short[:12]
	}
	name := agentDisplayNames[review.Provider]
	if name == "" {
		name = review.Provider
	}
	var b strings.Builder
	fmt.Fprintf(&b, "**Agent review by %s** of `%s` for %s, requested in Kuayle.\n\n", name, short, comment.IssueIdentifier)
	fmt.Fprintf(&b, "Verdict: **%s**\n\n", verdict)
	for _, line := range strings.Split(findings, "\n") {
		b.WriteString("> " + line + "\n")
	}
	b.WriteString("\n_Written by an AI reviewer; this comment is not a GitHub review approval or change request._\n")
	return b.String()
}

func (s *GitHubService) githubPostReviewComment(ctx context.Context, workspaceID uuid.UUID, repository string, number int, marker, body string) error {
	_, name, ok := strings.Cut(repository, "/")
	if !ok {
		return fmt.Errorf("invalid repository %q", repository)
	}
	inst, err := s.ghRepo.GetInstallationByWorkspace(ctx, workspaceID)
	if err != nil || inst == nil {
		return fmt.Errorf("GitHub not connected")
	}
	client, _, err := s.getClient(ctx, workspaceID)
	if err != nil {
		return err
	}
	token, _, err := client.GetRepositoryInstallationToken(inst.InstallationID, name)
	if err != nil {
		return err
	}
	return client.EnsurePullRequestComment(ctx, token, repository, number, marker, body)
}
