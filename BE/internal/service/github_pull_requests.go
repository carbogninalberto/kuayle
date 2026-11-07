package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
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
	// MergeCommit is the commit the merge created on the base branch, once merged.
	MergeCommit string
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
	PullRequestChecks(ctx context.Context, ws uuid.UUID, repository string, number int) (string, []PullRequestCheck, ChecksCoverage, error)
	PullRequestComments(ctx context.Context, ws uuid.UUID, repository string, number int) (*PullRequestCommentList, error)
	ReplyToReviewComment(ctx context.Context, ws uuid.UUID, repository string, number int, commentID int64, body string) error
	ResolveReviewThread(ctx context.Context, ws uuid.UUID, repository string, number int, threadID string) error
	UpdatePullRequestBranch(ctx context.Context, ws uuid.UUID, repository string, number int, expectedHead string) (string, error)
	SetPullRequestDraft(ctx context.Context, ws uuid.UUID, repository string, number int, draft bool) error
	ResolveMergeMethod(ctx context.Context, ws uuid.UUID, repository string) (string, error)
}

var _ pullRequestReader = (*GitHubService)(nil)

func (s *GitHubService) pullClient(ctx context.Context, ws uuid.UUID, repository string) (*gh.Client, string, string, string, error) {
	return s.repositoryClient(ctx, ws, repository)
}

// pullWriteClient is pullClient for actions that change a pull request,
// which the workspace may turn off.
func (s *GitHubService) pullWriteClient(ctx context.Context, ws uuid.UUID, repository string) (*gh.Client, string, string, string, error) {
	if err := s.requireAbility(ctx, ws, domain.GitHubBotPullRequestActions); err != nil {
		return nil, "", "", "", err
	}
	return s.pullClient(ctx, ws, repository)
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
		status.State, status.MergeCommit = "merged", pull.MergeCommitSHA
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

// ChecksCoverage says how much of a commit's CI was read. When Truncated or
// StatusesUnavailable, failures may be unseen, so the checks must never be
// reported as passing.
type ChecksCoverage struct {
	Truncated           bool
	StatusesUnavailable bool
}

func (c ChecksCoverage) incomplete() bool { return c.Truncated || c.StatusesUnavailable }

func (s *GitHubService) PullRequestChecks(ctx context.Context, ws uuid.UUID, repository string, number int) (string, []PullRequestCheck, ChecksCoverage, error) {
	var coverage ChecksCoverage
	client, token, owner, name, err := s.pullClient(ctx, ws, repository)
	if err != nil {
		return "", nil, coverage, err
	}
	pull, err := client.GetPullRequest(token, owner, name, number)
	if err != nil {
		return "", nil, coverage, err
	}
	head := pull.Head.SHA
	checks, coverage, err := commitChecks(client, token, owner, name, head)
	if err != nil {
		return "", nil, coverage, err
	}
	return head, checks, coverage, nil
}

// commitChecks reads a commit's check runs and legacy statuses. A failed read
// of the statuses is flagged rather than fatal, so it never looks like green CI.
func commitChecks(client *gh.Client, token, owner, name, sha string) ([]PullRequestCheck, ChecksCoverage, error) {
	var coverage ChecksCoverage
	runs, truncated, err := client.ListCheckRuns(token, owner, name, sha)
	if err != nil {
		return nil, coverage, err
	}
	coverage.Truncated = truncated
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
	statuses, statusesTruncated, statusErr := client.ListCommitStatuses(token, owner, name, sha)
	if statusErr != nil {
		coverage.StatusesUnavailable = true
	}
	coverage.Truncated = coverage.Truncated || statusesTruncated
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
	return checks, coverage, nil
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

func (s *GitHubService) PullRequestComments(ctx context.Context, ws uuid.UUID, repository string, number int) (*PullRequestCommentList, error) {
	client, token, owner, name, err := s.pullClient(ctx, ws, repository)
	if err != nil {
		return nil, err
	}
	comments, truncated, err := client.ListReviewComments(token, owner, name, number)
	if err != nil {
		return nil, err
	}
	// Thread state needs GraphQL; without it the comments are still listed but
	// the list says their state is unknown instead of calling them unresolved.
	list := &PullRequestCommentList{Truncated: truncated}
	threadOf := map[int64]gh.ReviewThread{}
	threads, threadsTruncated, threadErr := client.ListReviewThreads(token, owner, name, number)
	if threadErr != nil {
		list.ThreadsUnavailable = true
	}
	if threadsTruncated {
		list.Truncated = true
	}
	for _, thread := range threads {
		for _, id := range thread.CommentIDs {
			threadOf[id] = thread
		}
	}
	list.Comments = make([]PullRequestComment, 0, len(comments))
	for _, comment := range comments {
		thread := threadOf[comment.ID]
		list.Comments = append(list.Comments, PullRequestComment{
			ID: comment.ID, Author: comment.User.Login, Bot: strings.EqualFold(comment.User.Type, "Bot"), Path: comment.Path, Line: comment.Line,
			Body: comment.Body, URL: comment.HTMLURL, ReplyTo: comment.InReplyToID, ThreadID: thread.ID, Resolved: thread.IsResolved, Outdated: thread.IsOutdated,
		})
	}
	return list, nil
}

func (s *GitHubService) ReplyToReviewComment(ctx context.Context, ws uuid.UUID, repository string, number int, commentID int64, body string) error {
	client, token, owner, name, err := s.pullWriteClient(ctx, ws, repository)
	if err != nil {
		return err
	}
	_, err = client.ReplyToReviewComment(token, owner, name, number, commentID, body)
	return err
}

// ResolveReviewThread resolves a thread only if it belongs to the pull request.
func (s *GitHubService) ResolveReviewThread(ctx context.Context, ws uuid.UUID, repository string, number int, threadID string) error {
	client, token, owner, name, err := s.pullWriteClient(ctx, ws, repository)
	if err != nil {
		return err
	}
	threads, truncated, err := client.ListReviewThreads(token, owner, name, number)
	if err != nil {
		return err
	}
	if truncated {
		return &gh.APIError{Status: 422, Message: "this pull request has too many review threads to look the thread up"}
	}
	for _, thread := range threads {
		if thread.ID == threadID {
			return client.ResolveReviewThread(token, threadID)
		}
	}
	return &gh.APIError{Status: 404, Message: "review thread not found on this pull request"}
}

// HeadSyncError means GitHub accepted a branch update but Kuayle could not
// read and record the new head, so the task's checkout is not known to follow.
type HeadSyncError struct{ Err error }

func (e *HeadSyncError) Error() string { return "could not record the updated head: " + e.Err.Error() }
func (e *HeadSyncError) Unwrap() error { return e.Err }

// UpdatePullRequestBranch asks GitHub to merge the base branch into the head
// branch while the head is still expectedHead. It then records the new head
// through ReadPullRequestHead, the path head tracking uses, so the checkout of
// the task is synced to it on its next publication (see SyncCheckout) exactly
// as for any commit others add to the branch. GitHub merges asynchronously, so
// it waits briefly for the head to move; if it has not, the pull_request
// webhook records it later and the returned head is still expectedHead.
func (s *GitHubService) UpdatePullRequestBranch(ctx context.Context, ws uuid.UUID, repository string, number int, expectedHead string) (string, error) {
	client, token, owner, name, err := s.pullWriteClient(ctx, ws, repository)
	if err != nil {
		return "", err
	}
	if err = client.UpdatePullRequestBranch(token, owner, name, number, expectedHead); err != nil {
		return "", err
	}
	waits := s.headPollWaits
	if waits == nil {
		waits = []time.Duration{time.Second, 2 * time.Second, 3 * time.Second, 4 * time.Second}
	}
	head := expectedHead
	for attempt := 0; ; attempt++ {
		if head, err = s.ReadPullRequestHead(ctx, ws, repository, number); err != nil {
			return "", &HeadSyncError{Err: err}
		}
		if head != expectedHead || attempt >= len(waits) {
			return head, nil
		}
		select {
		case <-ctx.Done():
			return head, nil
		case <-time.After(waits[attempt]):
		}
	}
}

// SetPullRequestDraft marks a pull request ready for review or back to draft.
func (s *GitHubService) SetPullRequestDraft(ctx context.Context, ws uuid.UUID, repository string, number int, draft bool) error {
	client, token, owner, name, err := s.pullWriteClient(ctx, ws, repository)
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

// PullRequestCommentList is the inline review comments of a pull request.
// ThreadsUnavailable means the resolved state of every thread is unknown
// (GraphQL failed or the App lacks read access); Truncated that a page limit
// cut the comments or threads.
type PullRequestCommentList struct {
	Comments           []PullRequestComment
	ThreadsUnavailable bool
	Truncated          bool
}
