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
	Configured   bool                        `json:"configured"`
	Installed    bool                        `json:"installed"`
	GlobalApp    bool                        `json:"global_app"`
	AppSlug      string                      `json:"app_slug,omitempty"`
	Installation *GitHubInstallationResponse `json:"installation,omitempty"`
	Repos        []GitHubRepoResponse        `json:"repos"`
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

// GitHubAppSetupState says whether the GitHub App delivers everything Kuayle needs.
type GitHubAppSetupState string

const (
	// GitHubAppSetupOK means the app and its installation grant every requirement.
	GitHubAppSetupOK GitHubAppSetupState = "ok"
	// GitHubAppSetupAppMissing means the app's owner must add events or permissions.
	GitHubAppSetupAppMissing GitHubAppSetupState = "app_missing"
	// GitHubAppSetupInstallationPending means the installation must accept the app's update.
	GitHubAppSetupInstallationPending GitHubAppSetupState = "installation_pending"
	// GitHubAppSetupInstallationSuspended means the installation is suspended on GitHub.
	GitHubAppSetupInstallationSuspended GitHubAppSetupState = "installation_suspended"
	// GitHubAppSetupInstallationMissing means GitHub no longer has the recorded installation.
	GitHubAppSetupInstallationMissing GitHubAppSetupState = "installation_missing"
	// GitHubAppSetupNotConfigured means the workspace has no GitHub App.
	GitHubAppSetupNotConfigured GitHubAppSetupState = "not_configured"
	// GitHubAppSetupNotInstalled means the app is not installed for the workspace.
	GitHubAppSetupNotInstalled GitHubAppSetupState = "not_installed"
	// GitHubAppSetupUnavailable means GitHub could not be asked.
	GitHubAppSetupUnavailable GitHubAppSetupState = "unavailable"
)

type GitHubAppPermissionRequirement struct {
	Name  string `json:"name"`
	Level string `json:"level"`
}

type GitHubAppSetupResponse struct {
	State GitHubAppSetupState `json:"state"`
	// Managed is true when a shared app is configured by the Kuayle operator,
	// so workspace owners cannot change the app's own settings.
	Managed            bool                             `json:"managed"`
	AppSlug            string                           `json:"app_slug,omitempty"`
	AccountLogin       string                           `json:"account_login,omitempty"`
	MissingEvents      []string                         `json:"missing_events"`
	MissingPermissions []GitHubAppPermissionRequirement `json:"missing_permissions"`
	AppSettingsURL     string                           `json:"app_settings_url,omitempty"`
	InstallationURL    string                           `json:"installation_url,omitempty"`
}

// GitHubBotAbilityResponse is one ability toggle of the workspace's GitHub bot.
type GitHubBotAbilityResponse struct {
	Key              string `json:"key"`
	Enabled          bool   `json:"enabled"`
	EnabledByDefault bool   `json:"enabled_by_default"`
}

// GitHubBotSettingsResponse is the workspace's GitHub bot identity and abilities.
// AvatarURL is an external https image or Kuayle's public path of an upload.
type GitHubBotSettingsResponse struct {
	DisplayName    string                     `json:"display_name"`
	DefaultName    string                     `json:"default_name"`
	AvatarURL      string                     `json:"avatar_url"`
	AvatarSource   string                     `json:"avatar_source"`
	Abilities      []GitHubBotAbilityResponse `json:"abilities"`
	MentionHarness string                     `json:"mention_harness"`
	MentionModel   string                     `json:"mention_model"`
	MentionHandle  string                     `json:"mention_handle"`
	Harnesses      []string                   `json:"harnesses"`
}

// UpdateGitHubBotSettingsRequest changes only the fields it sets. An empty
// display name restores the default and an empty avatar URL removes the picture.
type UpdateGitHubBotSettingsRequest struct {
	DisplayName    *string         `json:"display_name"`
	AvatarURL      *string         `json:"avatar_url"`
	Abilities      map[string]bool `json:"abilities"`
	MentionHarness *string         `json:"mention_harness"`
	MentionModel   *string         `json:"mention_model"`
}
