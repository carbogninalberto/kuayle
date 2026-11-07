package domain

import (
	"regexp"
	"time"

	"github.com/google/uuid"
)

// GitHubBotAbility is one thing the workspace's Kuayle GitHub bot may do on
// GitHub. Each is a per-workspace toggle; a disabled ability is refused where
// the GitHub write happens, whoever asked for it.
type GitHubBotAbility string

const (
	// GitHubBotLinkIssues comments the linked Kuayle issue on new pull requests.
	GitHubBotLinkIssues GitHubBotAbility = "link_issues"
	// GitHubBotTaskFeedback turns CI failures and change requests on a task's
	// pull request into follow-up attempts.
	GitHubBotTaskFeedback GitHubBotAbility = "task_feedback"
	// GitHubBotReviewComments posts agent review findings on pull requests.
	GitHubBotReviewComments GitHubBotAbility = "review_comments"
	// GitHubBotPullRequestActions replies to and resolves review threads,
	// updates branches and marks pull requests ready, after approval.
	GitHubBotPullRequestActions GitHubBotAbility = "pull_request_actions"
	// GitHubBotMergePullRequests merges approved pull requests.
	GitHubBotMergePullRequests GitHubBotAbility = "merge_pull_requests"
	// GitHubBotReleases publishes approved GitHub releases.
	GitHubBotReleases GitHubBotAbility = "releases"
	// GitHubBotMentionReviews starts an agent review when a trusted commenter
	// mentions the bot on a pull request and asks for a review.
	GitHubBotMentionReviews GitHubBotAbility = "mention_reviews"
)

// GitHubBotAbilities lists every ability in display order.
var GitHubBotAbilities = []GitHubBotAbility{
	GitHubBotLinkIssues, GitHubBotTaskFeedback, GitHubBotReviewComments, GitHubBotPullRequestActions,
	GitHubBotMergePullRequests, GitHubBotReleases, GitHubBotMentionReviews,
}

// DefaultGitHubBotName is the bot's name until a workspace chooses another.
const DefaultGitHubBotName = "Kuayle"

// GitHubBotNameMaxRunes bounds the display name.
const GitHubBotNameMaxRunes = 64

// GitHubBotAvatarMaxBytes bounds an uploaded or linked profile picture.
const GitHubBotAvatarMaxBytes = 2 * 1024 * 1024

// AvatarImageTypes maps the raster formats accepted for profile pictures to
// their file extension. SVG is excluded because it can carry script.
var AvatarImageTypes = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/webp": ".webp",
	"image/gif":  ".gif",
}

// GitHubBotAvatarFilePattern matches a stored bot avatar file name.
var GitHubBotAvatarFilePattern = regexp.MustCompile(`^[0-9a-f-]{36}\.(png|jpg|webp|gif)$`)

// GitHubBotAbilityEnabledByDefault keeps everything the bot already did on
// for existing workspaces; the mention trigger, which starts work without an
// approval card, is opt-in.
func GitHubBotAbilityEnabledByDefault(ability GitHubBotAbility) bool {
	return ability != GitHubBotMentionReviews
}

// GitHubBotSettings is a workspace's bot identity and abilities. At most one
// of AvatarURL (an external https image) and AvatarFile (an uploaded image)
// is set. Abilities holds only explicit choices; a missing key is the default.
type GitHubBotSettings struct {
	WorkspaceID    uuid.UUID       `db:"workspace_id"`
	DisplayName    *string         `db:"display_name"`
	AvatarURL      *string         `db:"avatar_url"`
	AvatarFile     *string         `db:"avatar_file"`
	Abilities      map[string]bool `db:"-"`
	MentionHarness string          `db:"mention_harness"`
	MentionModel   string          `db:"mention_model"`
	UpdatedAt      *time.Time      `db:"updated_at"`
}

// Name is the configured display name, or the default.
func (s *GitHubBotSettings) Name() string {
	if s == nil || s.DisplayName == nil || *s.DisplayName == "" {
		return DefaultGitHubBotName
	}
	return *s.DisplayName
}

// Enabled reports whether the workspace allows the ability.
func (s *GitHubBotSettings) Enabled(ability GitHubBotAbility) bool {
	if s != nil {
		if enabled, ok := s.Abilities[string(ability)]; ok {
			return enabled
		}
	}
	return GitHubBotAbilityEnabledByDefault(ability)
}

// IsGitHubBotAbility reports whether name is a known ability.
func IsGitHubBotAbility(name string) bool {
	for _, ability := range GitHubBotAbilities {
		if string(ability) == name {
			return true
		}
	}
	return false
}
