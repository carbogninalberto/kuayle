package domain

import (
	"time"

	"github.com/google/uuid"
)

type GitHubInstallation struct {
	ID             uuid.UUID  `json:"id" db:"id"`
	WorkspaceID    uuid.UUID  `json:"workspace_id" db:"workspace_id"`
	InstallationID int64      `json:"installation_id" db:"installation_id"`
	AccountLogin   string     `json:"account_login" db:"account_login"`
	AccountType    string     `json:"account_type" db:"account_type"`
	AccessToken    string     `json:"-" db:"access_token"`
	TokenExpiresAt *time.Time `json:"-" db:"token_expires_at"`
	InstalledBy    uuid.UUID  `json:"installed_by" db:"installed_by"`
	CreatedAt      time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at" db:"updated_at"`
}

type GitHubRepoModel struct {
	ID             uuid.UUID `json:"id" db:"id"`
	InstallationID uuid.UUID `json:"installation_id" db:"installation_id"`
	WorkspaceID    uuid.UUID `json:"workspace_id" db:"workspace_id"`
	GitHubRepoID   int64     `json:"github_repo_id" db:"github_repo_id"`
	FullName       string    `json:"full_name" db:"full_name"`
	DefaultBranch  string    `json:"default_branch" db:"default_branch"`
	IsActive       bool      `json:"is_active" db:"is_active"`
	CreatedAt      time.Time `json:"created_at" db:"created_at"`
}

type GitHubPullRequest struct {
	ID              uuid.UUID  `json:"id" db:"id"`
	WorkspaceID     uuid.UUID  `json:"workspace_id" db:"workspace_id"`
	IssueID         *uuid.UUID `json:"issue_id" db:"issue_id"`
	GitHubRepoID    uuid.UUID  `json:"github_repo_id" db:"github_repo_id"`
	GitHubPRID      int64      `json:"github_pr_id" db:"github_pr_id"`
	Number          int        `json:"number" db:"number"`
	Title           string     `json:"title" db:"title"`
	State           string     `json:"state" db:"state"`
	AuthorLogin     string     `json:"author_login" db:"author_login"`
	AuthorAvatarURL *string    `json:"author_avatar_url" db:"author_avatar_url"`
	HTMLURL         string     `json:"html_url" db:"html_url"`
	HeadBranch      *string    `json:"head_branch" db:"head_branch"`
	BaseBranch      *string    `json:"base_branch" db:"base_branch"`
	Additions       int        `json:"additions" db:"additions"`
	Deletions       int        `json:"deletions" db:"deletions"`
	MergedAt        *time.Time `json:"merged_at" db:"merged_at"`
	ClosedAt        *time.Time `json:"closed_at" db:"closed_at"`
	CreatedAt       time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at" db:"updated_at"`
}

type GitHubBranch struct {
	ID           uuid.UUID  `json:"id" db:"id"`
	WorkspaceID  uuid.UUID  `json:"workspace_id" db:"workspace_id"`
	IssueID      *uuid.UUID `json:"issue_id" db:"issue_id"`
	GitHubRepoID uuid.UUID  `json:"github_repo_id" db:"github_repo_id"`
	Name         string     `json:"name" db:"name"`
	HTMLURL      *string    `json:"html_url" db:"html_url"`
	CreatedAt    time.Time  `json:"created_at" db:"created_at"`
}

type GitHubCommit struct {
	ID              uuid.UUID  `json:"id" db:"id"`
	WorkspaceID     uuid.UUID  `json:"workspace_id" db:"workspace_id"`
	IssueID         *uuid.UUID `json:"issue_id" db:"issue_id"`
	GitHubRepoID    uuid.UUID  `json:"github_repo_id" db:"github_repo_id"`
	PRID            *uuid.UUID `json:"pr_id" db:"pr_id"`
	SHA             string     `json:"sha" db:"sha"`
	Message         string     `json:"message" db:"message"`
	AuthorLogin     *string    `json:"author_login" db:"author_login"`
	AuthorAvatarURL *string    `json:"author_avatar_url" db:"author_avatar_url"`
	HTMLURL         string     `json:"html_url" db:"html_url"`
	CommittedAt     time.Time  `json:"committed_at" db:"committed_at"`
	CreatedAt       time.Time  `json:"created_at" db:"created_at"`
}

type GitHubAutoTransition struct {
	ID             uuid.UUID  `json:"id" db:"id"`
	WorkspaceID    uuid.UUID  `json:"workspace_id" db:"workspace_id"`
	Event          string     `json:"event" db:"event"`
	TargetStatus   string     `json:"target_status" db:"target_status"`
	TargetStatusID *uuid.UUID `json:"target_status_id" db:"target_status_id"`
	IsActive       bool       `json:"is_active" db:"is_active"`
}
