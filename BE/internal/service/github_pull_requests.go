package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	gh "github.com/kuayle/kuayle-backend/pkg/github"
)

// pullRequestSettings stores the merge method chosen per repository.
type pullRequestSettings interface {
	GetMergeMethod(ctx context.Context, ws uuid.UUID, repository string) (string, error)
	SetMergeMethod(ctx context.Context, ws uuid.UUID, repository, method string) error
	ListMergeMethods(ctx context.Context, ws uuid.UUID) (map[string]string, error)
}

// SetPullRequestSettings lets merges use the merge method of the development settings.
func (s *GitHubService) SetPullRequestSettings(settings pullRequestSettings) {
	s.mergeSettings = settings
}

// MergeMethods are the methods GitHub offers, in the order a repository without
// a configured one is asked to use them.
var MergeMethods = []string{"squash", "merge", "rebase"}

// PullRequestStatus is a pull request as GitHub reports it now.
type PullRequestStatus struct {
	State          string // open, closed or merged
	Draft          bool
	Mergeable      *bool
	MergeableState string
	HeadSHA        string
	HeadRef        string
	BaseRef        string
	Ahead, Behind  int
	ChangedFiles   int
	URL            string
}

// PullRequestCheck is one CI check of the head commit.
type PullRequestCheck struct {
	Name    string `json:"name"`
	State   string `json:"state"` // passed, failed or pending
	URL     string `json:"url,omitempty"`
	Summary string `json:"summary,omitempty"`
}

// PullRequestComment is an inline review comment with its thread.
type PullRequestComment struct {
	ID       int64  `json:"id"`
	Author   string `json:"author"`
	Bot      bool   `json:"bot"`
	Path     string `json:"path"`
	Line     int    `json:"line,omitempty"`
	Body     string `json:"body"`
	URL      string `json:"url"`
	ReplyTo  int64  `json:"in_reply_to,omitempty"`
	ThreadID string `json:"thread_id,omitempty"`
	Resolved bool   `json:"resolved"`
	Outdated bool   `json:"outdated,omitempty"`
}

// pullRequestReader is the GitHub side of the pull request tools.
type pullRequestReader interface {
	PullRequestStatus(ctx context.Context, ws uuid.UUID, repository string, number int) (*PullRequestStatus, error)
	PullRequestChecks(ctx context.Context, ws uuid.UUID, repository string, number int) (string, []PullRequestCheck, error)
	PullRequestComments(ctx context.Context, ws uuid.UUID, repository string, number int) ([]PullRequestComment, error)
	ReplyToReviewComment(ctx context.Context, ws uuid.UUID, repository string, number int, commentID int64, body string) error
	ResolveReviewThread(ctx context.Context, ws uuid.UUID, repository string, number int, threadID string) error
	UpdatePullRequestBranch(ctx context.Context, ws uuid.UUID, repository string, number int, expectedHead string) error
	SetPullRequestDraft(ctx context.Context, ws uuid.UUID, repository string, number int, draft bool) error
	ResolveMergeMethod(ctx context.Context, ws uuid.UUID, repository string) (string, error)
}

var _ pullRequestReader = (*GitHubService)(nil)

func (s *GitHubService) pullClient(ctx context.Context, ws uuid.UUID, repository string) (*gh.Client, string, string, string, error) {
	return s.repositoryClient(ctx, ws, repository)
}

func (s *GitHubService) PullRequestStatus(ctx context.Context, ws uuid.UUID, repository string, number int) (*PullRequestStatus, error) {
	client, token, owner, name, err := s.pullClient(ctx, ws, repository)
	if err != nil {
		return nil, err
	}
	pull, err := client.GetPullRequest(token, owner, name, number)
	if err != nil {
		return nil, err
	}
	status := &PullRequestStatus{
		State: pull.State, Draft: pull.Draft, Mergeable: pull.Mergeable, MergeableState: pull.MergeableState,
		HeadSHA: pull.Head.SHA, HeadRef: pull.Head.Ref, BaseRef: pull.Base.Ref, ChangedFiles: pull.ChangedFiles, URL: pull.HTMLURL,
	}
	if pull.Merged {
		status.State = "merged"
	}
	// Ahead and behind are counted from the base branch; a failed comparison
	// only leaves them unknown (-1) rather than failing the whole read.
	status.Ahead, status.Behind = -1, -1
	if status.State == "open" && status.HeadSHA != "" {
		if comparison, err := client.CompareCommits(token, owner, name, pull.Base.Ref, status.HeadSHA); err == nil {
			status.Ahead, status.Behind = comparison.AheadBy, comparison.BehindBy
		}
	}
	return status, nil
}

const maxChecksListed = 40

func (s *GitHubService) PullRequestChecks(ctx context.Context, ws uuid.UUID, repository string, number int) (string, []PullRequestCheck, error) {
	client, token, owner, name, err := s.pullClient(ctx, ws, repository)
	if err != nil {
		return "", nil, err
	}
	pull, err := client.GetPullRequest(token, owner, name, number)
	if err != nil {
		return "", nil, err
	}
	head := pull.Head.SHA
	runs, err := client.ListCheckRuns(token, owner, name, head)
	if err != nil {
		return "", nil, err
	}
	// Failing checks first, so a long list never hides the one that matters.
	var checks []PullRequestCheck
	for _, run := range runs {
		check := PullRequestCheck{Name: truncateRunes(run.Name, 120), State: checkRunState(run), URL: run.HTMLURL}
		if run.DetailsURL != "" {
			check.URL = run.DetailsURL
		}
		if check.State == "failed" {
			check.Summary = truncateRunes(strings.TrimSpace(run.Output.Title+"\n"+run.Output.Summary), 500)
		}
		checks = append(checks, check)
	}
	// Legacy commit statuses are best effort: checks alone are still reported.
	if statuses, err := client.ListCommitStatuses(token, owner, name, head); err == nil {
		for _, status := range statuses {
			state := map[string]string{"success": "passed", "pending": "pending"}[status.State]
			if state == "" {
				state = "failed"
			}
			check := PullRequestCheck{Name: truncateRunes(status.Context, 120), State: state, URL: status.TargetURL}
			if state == "failed" {
				check.Summary = truncateRunes(status.Description, 500)
			}
			checks = append(checks, check)
		}
	}
	return head, checks, nil
}

// checkRunState maps a check run to passed, failed or pending.
func checkRunState(run gh.CheckRun) string {
	if run.Status != "completed" {
		return "pending"
	}
	switch run.Conclusion {
	case "success", "neutral", "skipped":
		return "passed"
	}
	return "failed"
}

func (s *GitHubService) PullRequestComments(ctx context.Context, ws uuid.UUID, repository string, number int) ([]PullRequestComment, error) {
	client, token, owner, name, err := s.pullClient(ctx, ws, repository)
	if err != nil {
		return nil, err
	}
	comments, err := client.ListReviewComments(token, owner, name, number)
	if err != nil {
		return nil, err
	}
	// Thread state needs GraphQL; without it the comments are still listed,
	// every thread reported as unresolved.
	threadOf := map[int64]gh.ReviewThread{}
	if threads, err := client.ListReviewThreads(token, owner, name, number); err == nil {
		for _, thread := range threads {
			for _, id := range thread.CommentIDs {
				threadOf[id] = thread
			}
		}
	}
	result := make([]PullRequestComment, 0, len(comments))
	for _, comment := range comments {
		thread := threadOf[comment.ID]
		result = append(result, PullRequestComment{
			ID: comment.ID, Author: comment.User.Login, Bot: strings.EqualFold(comment.User.Type, "Bot"), Path: comment.Path, Line: comment.Line,
			Body: comment.Body, URL: comment.HTMLURL, ReplyTo: comment.InReplyToID, ThreadID: thread.ID, Resolved: thread.IsResolved, Outdated: thread.IsOutdated,
		})
	}
	return result, nil
}

func (s *GitHubService) ReplyToReviewComment(ctx context.Context, ws uuid.UUID, repository string, number int, commentID int64, body string) error {
	client, token, owner, name, err := s.pullClient(ctx, ws, repository)
	if err != nil {
		return err
	}
	_, err = client.ReplyToReviewComment(token, owner, name, number, commentID, body)
	return err
}

// ResolveReviewThread resolves a thread only if it belongs to the pull request.
func (s *GitHubService) ResolveReviewThread(ctx context.Context, ws uuid.UUID, repository string, number int, threadID string) error {
	client, token, owner, name, err := s.pullClient(ctx, ws, repository)
	if err != nil {
		return err
	}
	threads, err := client.ListReviewThreads(token, owner, name, number)
	if err != nil {
		return err
	}
	for _, thread := range threads {
		if thread.ID == threadID {
			return client.ResolveReviewThread(token, threadID)
		}
	}
	return &gh.APIError{Status: 404, Message: "review thread not found on this pull request"}
}

// UpdatePullRequestBranch asks GitHub to merge the base branch into the head
// branch while the head is still expectedHead, then records the head GitHub
// reports so the checkout of the task is synced to it on its next publication
// (see SyncCheckout), exactly as for any commit others add to the branch.
func (s *GitHubService) UpdatePullRequestBranch(ctx context.Context, ws uuid.UUID, repository string, number int, expectedHead string) error {
	client, token, owner, name, err := s.pullClient(ctx, ws, repository)
	if err != nil {
		return err
	}
	if err = client.UpdatePullRequestBranch(token, owner, name, number, expectedHead); err != nil {
		return err
	}
	// GitHub completes the merge asynchronously and the pull_request webhook
	// records the new head; reading now only helps when it is already done.
	_, _ = s.ReadPullRequestHead(ctx, ws, repository, number)
	return nil
}

// SetPullRequestDraft marks a pull request ready for review or back to draft.
func (s *GitHubService) SetPullRequestDraft(ctx context.Context, ws uuid.UUID, repository string, number int, draft bool) error {
	client, token, owner, name, err := s.pullClient(ctx, ws, repository)
	if err != nil {
		return err
	}
	pull, err := client.GetPullRequest(token, owner, name, number)
	if err != nil {
		return err
	}
	if pull.Draft == draft {
		return nil
	}
	if draft {
		return client.ConvertPullRequestToDraft(token, pull.NodeID)
	}
	return client.MarkPullRequestReadyForReview(token, pull.NodeID)
}

// ErrMergeMethodNotAllowed means the repository's configured method is disabled on GitHub.
var ErrMergeMethodNotAllowed = errors.New("merge method not allowed by the repository")

// ResolveMergeMethod is the method a merge of the repository uses: the one
// chosen in the development settings, which must be allowed by the repository,
// otherwise the first allowed one (squash, merge, rebase).
func (s *GitHubService) ResolveMergeMethod(ctx context.Context, ws uuid.UUID, repository string) (string, error) {
	configured := ""
	if s.mergeSettings != nil {
		var err error
		if configured, err = s.mergeSettings.GetMergeMethod(ctx, ws, repository); err != nil {
			return "", err
		}
	}
	read := s.repositoryMergeSettings
	if read == nil {
		read = s.githubRepositoryMergeSettings
	}
	allowed, err := read(ctx, ws, repository)
	if err != nil {
		// Without the repository's rules the configured method, or squash, is
		// tried and GitHub refuses it with its own reason if it is not allowed.
		if configured != "" {
			return configured, nil
		}
		return "squash", nil
	}
	if configured != "" {
		if !allowed.Allows(configured) {
			return "", fmt.Errorf("%w: %s", ErrMergeMethodNotAllowed, configured)
		}
		return configured, nil
	}
	for _, method := range MergeMethods {
		if allowed.Allows(method) {
			return method, nil
		}
	}
	return "", fmt.Errorf("%w: the repository allows no merge method", ErrMergeMethodNotAllowed)
}

func (s *GitHubService) githubRepositoryMergeSettings(ctx context.Context, ws uuid.UUID, repository string) (*gh.RepositoryMergeSettings, error) {
	client, token, owner, name, err := s.pullClient(ctx, ws, repository)
	if err != nil {
		return nil, err
	}
	return client.GetRepositoryMergeSettings(token, owner, name)
}

// SetMergeMethod records a repository's merge method from the development
// settings; "" restores the default. The method is checked against the
// repository's rules when it is next used.
func (s *GitHubService) SetMergeMethod(ctx context.Context, ws uuid.UUID, repository, method string) error {
	if s.mergeSettings == nil {
		return fmt.Errorf("merge settings are unavailable")
	}
	linked, err := s.ghRepo.ListReposByWorkspace(ctx, ws)
	if err != nil {
		return err
	}
	for _, repo := range linked {
		if strings.EqualFold(repo.FullName, repository) {
			// Stored under the linked name, which merges look up.
			return s.mergeSettings.SetMergeMethod(ctx, ws, repo.FullName, method)
		}
	}
	return ErrRepositoryNotLinked
}

// MergeMethodSettings lists the configured merge method of every repository.
func (s *GitHubService) MergeMethodSettings(ctx context.Context, ws uuid.UUID) (map[string]string, error) {
	if s.mergeSettings == nil {
		return map[string]string{}, nil
	}
	return s.mergeSettings.ListMergeMethods(ctx, ws)
}

// ErrRepositoryNotLinked means the repository is not linked to the workspace.
var ErrRepositoryNotLinked = errors.New("repository is not linked to the workspace")
