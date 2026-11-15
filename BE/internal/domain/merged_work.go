package domain

import "fmt"

// MergedPullRequest is a merged pull request (GitHub) or merge request
// (GitLab, Gitea, Forgejo) linked to an issue.
type MergedPullRequest struct {
	Number     int    `json:"number" db:"number"`
	URL        string `json:"url" db:"url"`
	Repository string `json:"repository" db:"repository"`
}

// MergedWorkError refuses to start another agent run (a retry, a follow-up,
// "send and start now") on an issue whose work was already merged until the
// person confirms it (KUA-193). It names the newest merged pull request.
type MergedWorkError struct {
	PullRequest MergedPullRequest
}

func (e *MergedWorkError) Error() string {
	return fmt.Sprintf("This issue's work was merged in #%d. Retry anyway?", e.PullRequest.Number)
}
