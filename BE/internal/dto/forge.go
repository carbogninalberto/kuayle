package dto

import "time"

// --- Requests ---

type CreateForgeConnectionRequest struct {
	Provider      string `json:"provider" validate:"required"`
	Name          string `json:"name"`
	BaseURL       string `json:"base_url" validate:"required"`
	WebhookSecret string `json:"webhook_secret" validate:"required"`
	AccessToken   string `json:"access_token"`
}

// UpdateForgeConnectionRequest changes only the fields it sets. An empty
// webhook_secret is rejected; an empty access_token removes the token.
type UpdateForgeConnectionRequest struct {
	Name          *string `json:"name"`
	BaseURL       *string `json:"base_url"`
	WebhookSecret *string `json:"webhook_secret"`
	AccessToken   *string `json:"access_token"`
}

type LinkForgeRepoRequest struct {
	ExternalRepoID int64  `json:"external_repo_id" validate:"required"`
	FullName       string `json:"full_name" validate:"required"`
	WebURL         string `json:"web_url"`
	DefaultBranch  string `json:"default_branch"`
}

// --- Responses ---

type ForgeConnectionResponse struct {
	ID               string    `json:"id"`
	Provider         string    `json:"provider"`
	Name             string    `json:"name"`
	BaseURL          string    `json:"base_url"`
	HasAccessToken   bool      `json:"has_access_token"`
	HasWebhookSecret bool      `json:"has_webhook_secret"`
	WebhookURL       string    `json:"webhook_url"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type ForgeRepoResponse struct {
	ID             string `json:"id"`
	ConnectionID   string `json:"connection_id"`
	ExternalRepoID int64  `json:"external_repo_id"`
	FullName       string `json:"full_name"`
	WebURL         string `json:"web_url"`
	DefaultBranch  string `json:"default_branch"`
	IsActive       bool   `json:"is_active"`
}

type ForgeMergeRequestResponse struct {
	ID           string     `json:"id"`
	Provider     string     `json:"provider"`
	Number       int        `json:"number"`
	Title        string     `json:"title"`
	State        string     `json:"state"`
	AuthorLogin  string     `json:"author_login"`
	WebURL       string     `json:"web_url"`
	SourceBranch string     `json:"source_branch"`
	TargetBranch string     `json:"target_branch"`
	RepoFullName string     `json:"repo_full_name"`
	MergedAt     *time.Time `json:"merged_at"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type ForgeBranchResponse struct {
	ID           string `json:"id"`
	Provider     string `json:"provider"`
	Name         string `json:"name"`
	WebURL       string `json:"web_url"`
	RepoFullName string `json:"repo_full_name"`
}

type ForgeCommitResponse struct {
	ID           string    `json:"id"`
	Provider     string    `json:"provider"`
	SHA          string    `json:"sha"`
	ShortSHA     string    `json:"short_sha"`
	Message      string    `json:"message"`
	AuthorLogin  string    `json:"author_login"`
	WebURL       string    `json:"web_url"`
	RepoFullName string    `json:"repo_full_name"`
	CommittedAt  time.Time `json:"committed_at"`
}

type ForgeIssueActivityResponse struct {
	MergeRequests []ForgeMergeRequestResponse `json:"merge_requests"`
	Branches      []ForgeBranchResponse       `json:"branches"`
	Commits       []ForgeCommitResponse       `json:"commits"`
}
