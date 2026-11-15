package domain

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// ForgeProvider identifies a source-control forge. Bitbucket is reserved for a
// later phase: the abstraction and the publishing interface already leave room
// for it, but no provider implements it yet.
type ForgeProvider string

const (
	ForgeGitHub  ForgeProvider = "github"
	ForgeGitLab  ForgeProvider = "gitlab"
	ForgeGitea   ForgeProvider = "gitea"
	ForgeForgejo ForgeProvider = "forgejo"
	// ForgeBitbucket is reserved; no integration ships with it yet.
	ForgeBitbucket ForgeProvider = "bitbucket"
)

// ForgeEventKind is the normalized kind of a webhook event.
type ForgeEventKind string

const (
	ForgeEventMergeRequest  ForgeEventKind = "merge_request"
	ForgeEventPush          ForgeEventKind = "push"
	ForgeEventBranchCreated ForgeEventKind = "branch_created"
	ForgeEventIgnored       ForgeEventKind = "ignored"
)

// ForgeRepoRef identifies the repository a webhook payload belongs to.
type ForgeRepoRef struct {
	ExternalID int64
	FullName   string
	WebURL     string
}

// ForgeMergeRequest is a provider-neutral merge/pull request.
type ForgeMergeRequest struct {
	ExternalID   int64
	Number       int
	Title        string
	Description  string
	SourceBranch string
	TargetBranch string
	Author       string
	WebURL       string
	// State is one of open, draft, closed or merged.
	State string
	// Action is one of opened, reopened, ready, updated, closed or merged.
	Action   string
	MergedAt *time.Time
	ClosedAt *time.Time
}

// ForgeCommit is a provider-neutral commit.
type ForgeCommit struct {
	SHA         string
	Message     string
	URL         string
	Author      string
	CommittedAt time.Time
}

// ForgeBranch is a provider-neutral branch reference.
type ForgeBranch struct {
	Name   string
	WebURL string
}

// ForgeEvent is the normalized result of parsing a webhook payload. A push
// that also created a branch carries both the commits and the branch.
type ForgeEvent struct {
	Kind         ForgeEventKind
	DeliveryID   string
	Repo         ForgeRepoRef
	MergeRequest *ForgeMergeRequest
	Commits      []ForgeCommit
	Branch       *ForgeBranch
}

// ForgeWebhook verifies and parses a provider's webhook requests.
type ForgeWebhook interface {
	Provider() ForgeProvider
	VerifyWebhook(header http.Header, body []byte, secret string) error
	DeliveryID(header http.Header) string
	ParseWebhook(header http.Header, body []byte) (*ForgeEvent, error)
}

// ForgeCommenter posts a comment on a merge/pull request. Implementations must
// dial the admin-supplied base URL through the SSRF-safe client.
type ForgeCommenter interface {
	CommentOnMergeRequest(ctx context.Context, conn *ForgeConnection, repo *ForgeRepo, number int, body string) error
}

// ForgeNewMergeRequest describes a merge request an agent would open.
type ForgeNewMergeRequest struct {
	Title        string
	Description  string
	SourceBranch string
	TargetBranch string
}

// ForgePublisher opens and merges merge/pull requests. It is the extension
// point for agent publishing on non-GitHub forges (and for Bitbucket); nothing
// implements it yet.
type ForgePublisher interface {
	OpenMergeRequest(ctx context.Context, conn *ForgeConnection, repo *ForgeRepo, in ForgeNewMergeRequest) (*ForgeMergeRequest, error)
	MergeMergeRequest(ctx context.Context, conn *ForgeConnection, repo *ForgeRepo, number int) error
}

// ForgeConnection is a workspace's configuration for one forge instance.
// Secret fields never leave the server.
type ForgeConnection struct {
	ID            uuid.UUID     `json:"id" db:"id"`
	WorkspaceID   uuid.UUID     `json:"workspace_id" db:"workspace_id"`
	Provider      ForgeProvider `json:"provider" db:"provider"`
	Name          string        `json:"name" db:"name"`
	BaseURL       string        `json:"base_url" db:"base_url"`
	WebhookSecret string        `json:"-" db:"webhook_secret"`
	AccessToken   *string       `json:"-" db:"access_token"`
	CreatedBy     uuid.UUID     `json:"created_by" db:"created_by"`
	CreatedAt     time.Time     `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time     `json:"updated_at" db:"updated_at"`
}

// ForgeRepo is a repository linked to a forge connection.
type ForgeRepo struct {
	ID             uuid.UUID `json:"id" db:"id"`
	ConnectionID   uuid.UUID `json:"connection_id" db:"connection_id"`
	WorkspaceID    uuid.UUID `json:"workspace_id" db:"workspace_id"`
	ExternalRepoID int64     `json:"external_repo_id" db:"external_repo_id"`
	FullName       string    `json:"full_name" db:"full_name"`
	WebURL         string    `json:"web_url" db:"web_url"`
	DefaultBranch  string    `json:"default_branch" db:"default_branch"`
	IsActive       bool      `json:"is_active" db:"is_active"`
	CreatedAt      time.Time `json:"created_at" db:"created_at"`
}

// ForgeMergeRequestModel is a stored merge request.
type ForgeMergeRequestModel struct {
	ID              uuid.UUID  `json:"id" db:"id"`
	WorkspaceID     uuid.UUID  `json:"workspace_id" db:"workspace_id"`
	IssueID         *uuid.UUID `json:"issue_id" db:"issue_id"`
	ForgeRepoID     uuid.UUID  `json:"forge_repo_id" db:"forge_repo_id"`
	ExternalID      int64      `json:"external_id" db:"external_id"`
	Number          int        `json:"number" db:"number"`
	Title           string     `json:"title" db:"title"`
	State           string     `json:"state" db:"state"`
	AuthorLogin     string     `json:"author_login" db:"author_login"`
	AuthorAvatarURL *string    `json:"author_avatar_url" db:"author_avatar_url"`
	WebURL          string     `json:"web_url" db:"web_url"`
	SourceBranch    *string    `json:"source_branch" db:"source_branch"`
	TargetBranch    *string    `json:"target_branch" db:"target_branch"`
	Additions       int        `json:"additions" db:"additions"`
	Deletions       int        `json:"deletions" db:"deletions"`
	MergedAt        *time.Time `json:"merged_at" db:"merged_at"`
	ClosedAt        *time.Time `json:"closed_at" db:"closed_at"`
	CreatedAt       time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at" db:"updated_at"`
}

// ForgeBranchModel is a stored branch.
type ForgeBranchModel struct {
	ID          uuid.UUID  `json:"id" db:"id"`
	WorkspaceID uuid.UUID  `json:"workspace_id" db:"workspace_id"`
	IssueID     *uuid.UUID `json:"issue_id" db:"issue_id"`
	ForgeRepoID uuid.UUID  `json:"forge_repo_id" db:"forge_repo_id"`
	Name        string     `json:"name" db:"name"`
	WebURL      *string    `json:"web_url" db:"web_url"`
	CreatedAt   time.Time  `json:"created_at" db:"created_at"`
}

// ForgeCommitModel is a stored commit.
type ForgeCommitModel struct {
	ID              uuid.UUID  `json:"id" db:"id"`
	WorkspaceID     uuid.UUID  `json:"workspace_id" db:"workspace_id"`
	IssueID         *uuid.UUID `json:"issue_id" db:"issue_id"`
	ForgeRepoID     uuid.UUID  `json:"forge_repo_id" db:"forge_repo_id"`
	SHA             string     `json:"sha" db:"sha"`
	Message         string     `json:"message" db:"message"`
	AuthorLogin     *string    `json:"author_login" db:"author_login"`
	AuthorAvatarURL *string    `json:"author_avatar_url" db:"author_avatar_url"`
	WebURL          string     `json:"web_url" db:"web_url"`
	CommittedAt     time.Time  `json:"committed_at" db:"committed_at"`
	CreatedAt       time.Time  `json:"created_at" db:"created_at"`
}
