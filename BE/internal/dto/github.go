package dto

import "time"

// --- Requests ---

type LinkGitHubReposRequest struct {
	GitHubRepoIDs []int64 `json:"github_repo_ids" validate:"required,min=1"`
}

type UpdateAutoTransitionsRequest struct {
	Transitions []AutoTransitionRule `json:"transitions" validate:"required"`
}

type AutoTransitionRule struct {
	Event          string  `json:"event" validate:"required"`
	TargetStatus   string  `json:"target_status" validate:"required"`
	TargetStatusID *string `json:"target_status_id"`
	IsActive       bool    `json:"is_active"`
}

// --- Responses ---

type GitHubStatusResponse struct {
	Configured   bool                         `json:"configured"`
	Installed    bool                         `json:"installed"`
	GlobalApp    bool                         `json:"global_app"`
	AppSlug      string                       `json:"app_slug,omitempty"`
	Installation *GitHubInstallationResponse  `json:"installation,omitempty"`
	Repos        []GitHubRepoResponse         `json:"repos"`
}

type GitHubInstallationResponse struct {
	ID             string `json:"id"`
	InstallationID int64  `json:"installation_id"`
	AccountLogin   string `json:"account_login"`
	AccountType    string `json:"account_type"`
	CreatedAt      string `json:"created_at"`
}

type GitHubInstallURLResponse struct {
	URL string `json:"url"`
}

type GitHubRepoResponse struct {
	ID            string `json:"id"`
	GitHubRepoID  int64  `json:"github_repo_id"`
	FullName      string `json:"full_name"`
	DefaultBranch string `json:"default_branch"`
	IsActive      bool   `json:"is_active"`
}

type GitHubAvailableRepoResponse struct {
	GitHubRepoID  int64  `json:"github_repo_id"`
	FullName      string `json:"full_name"`
	DefaultBranch string `json:"default_branch"`
	Private       bool   `json:"private"`
	Linked        bool   `json:"linked"`
}

type GitHubPullRequestResponse struct {
	ID              string     `json:"id"`
	Number          int        `json:"number"`
	Title           string     `json:"title"`
	State           string     `json:"state"`
	AuthorLogin     string     `json:"author_login"`
	AuthorAvatarURL string     `json:"author_avatar_url"`
	HTMLURL         string     `json:"html_url"`
	HeadBranch      string     `json:"head_branch"`
	BaseBranch      string     `json:"base_branch"`
	Additions       int        `json:"additions"`
	Deletions       int        `json:"deletions"`
	RepoFullName    string     `json:"repo_full_name"`
	MergedAt        *time.Time `json:"merged_at"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type GitHubBranchResponse struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	HTMLURL      string `json:"html_url"`
	RepoFullName string `json:"repo_full_name"`
}

type GitHubCommitResponse struct {
	ID              string    `json:"id"`
	SHA             string    `json:"sha"`
	ShortSHA        string    `json:"short_sha"`
	Message         string    `json:"message"`
	AuthorLogin     string    `json:"author_login"`
	AuthorAvatarURL string    `json:"author_avatar_url"`
	HTMLURL         string    `json:"html_url"`
	RepoFullName    string    `json:"repo_full_name"`
	CommittedAt     time.Time `json:"committed_at"`
}

type GitHubIssueActivityResponse struct {
	PullRequests []GitHubPullRequestResponse `json:"pull_requests"`
	Branches     []GitHubBranchResponse      `json:"branches"`
	Commits      []GitHubCommitResponse      `json:"commits"`
}

type GitHubAutoTransitionResponse struct {
	Event          string  `json:"event"`
	TargetStatus   string  `json:"target_status"`
	TargetStatusID *string `json:"target_status_id"`
	IsActive       bool    `json:"is_active"`
}
